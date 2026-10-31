# 033. Cancellable Parses and Budgets

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Add invocation-local cancellation and an optional budget limit to parsing. Start with the core Go API, including Document and streams, then
apply the same interruption contract to generated Go and portable generated
TypeScript. Existing depth limits remain independent. Ordinary calls with no
controls keep their result and callback semantics.

The limits bound the budget spent by the engine, not elapsed time, memory, compilation, or
arbitrary caller code. Cancellation is cooperative at defined engine checkpoints;
it cannot make a blocking io.Reader or callback return. Interrupted parses
publish no partial batch result and release their mutable execution state.

## Motivation

A shallow grammar can repeatedly scan long input, backtrack across alternatives,
or perform many LR/Operators Rule growth rounds without reaching its depth limit. Editors
also need to abandon outdated work while preserving a Document for retry.

Source inspection at `6fac14632cbf933d91fee7f8e5df9e82fb134c56` establishes the
existing boundary:

- [`ParseOptions`](../../../internal/engine/core/input.go) carries unit, backend,
  recognition, depth, and tracing; [`public APIs`](../../../zeroparse.go) use functional
  ParseOption arguments. Document stores its construction settings; Parse has
  no per-call arguments today.
- [`run`/`recoverParse`](../../../internal/engine/program_parse.go) distinguish completed
  syntax/recovery outcomes from fatal interruptions. Recognition skips actions
  whose values predicates do not need; predicate-dependent rules and callees
  can still execute actions. Streams synchronously call a caller-owned reader
  and emission callback.
- [`Document.Parse`](../../../internal/engine/document/document.go) discards memo, repetition,
  and VM scratch state after an aborted parse because completed entries can depend
  on unfinished LR seeds. Ordinary failures retain reusable caches.
- [VM execution](../../../internal/engine/vm/vm.go), [iterative frames](../../../internal/engine/vm/ivm.go),
  [input loops](../../../internal/engine/core/input.go), and
  [repetition resumption](../../../internal/engine/core/resume.go) contain work outside a
  rule-entry depth check.
- [Generated Go](../../../runtime/runtime.go) resolves options per
  invocation, pools private scratch, and separately supports
  [typed execution](../../../runtime/typed.go). Depth options already
  apply to Node, recognition, and typed entry points under
  [design 022](022-generated-parser-depth-limits.md).

Focused measurements for a lazy Closure setup prototype appear under
Performance and Results. They are not feature-acceptance evidence: changes to
state and setup can affect allocation, cold calls, and reachability even when a
control is inactive.

## Goals

- Cancel or exhaust a budget across ordinary calls, scans, actions/predicates,
  repetition, LR/Operators Rule, memo hits, resumption, and result conversion.
- Return identifiable interruption errors and safe zero results; consuming
  grammar recovery must not absorb an interruption.
- Reset all provisional/reusable execution state after active interruption while
  preserving Document text/options and already delivered stream elements.
- Define counter units, reset points, callback ownership, polling bounds, and
  implementation-dependent counts honestly.
- Validate default-path cost against an exact parent and a same-revision static
  reference, including GC-phase costs, before adopting the implementation.

## Non-Goals

This proposal does not impose default finite budgets, change depth defaults,
preempt arbitrary goroutines, close caller readers, add asynchronous parsing,
limit heap allocations, or time compilation/Load/serialization. It does not add
snapshot, editor transaction, or generated Document/stream APIs. Future public
snapshot conversion may reuse these controls through its own API design.

## Design

### Proposed Go API

Use the existing functional-options convention. The following additions and
Document signature change are proposals:

```go
type Limits struct {
    Budget uint64
}
func WithContext(ctx context.Context) ParseOption
func WithLimits(limits Limits) ParseOption

// Existing Parse/ParseStream/RecognizeOnly calls accept the same options.
func (d *Document) Parse(opts ...ParseOption) (*Node, error)

var ErrLimit error

type LimitKind uint8
const (
    LimitBudget LimitKind = iota
)
type LimitError struct {
    Kind      LimitKind
    Limit     uint64
    Used      uint64
    Requested uint64
}
func (e *LimitError) Error() string
func (e *LimitError) Is(target error) bool // target == ErrLimit
```

Nil context means context.Background; zero limits mean unlimited. Options apply
once in order, and the last context or entire Limits value wins. There is no
shared mutable counter, global cancellation token, deadline timer goroutine, or
runtime polling-frequency option. Unsigned limits avoid a new negative-value
interpretation. Existing option/depth validation remains unchanged.

Resolve/validate options and rule/backend availability before entering counted
execution. Check cancellation before input initialization and immediately before
publishing a completed result. Options and first-use compilation/backend setup
are outside the budgets; check cancellation before and after that setup, without
claiming it is interruptible. A pre-cancelled context prevents input reading and
engine evaluation after validation. Errors.Is identifies context.Canceled or
context.DeadlineExceeded through the returned context error; custom cancellation
causes remain available through context.Cause on the caller's context.

Document construction options become defaults for each Parse call. Construction
input initialization uses a fresh control instance; each later Parse resets both
counters and uses fresh invocation state. Prefer per-call WithContext for an
editor task so a cancelled construction-default context does not govern every
future parse. The same caller-selected default context is otherwise retained
until the Document is released. `Document.Edit` remains outside parse controls.


### Document Input Preparation and Piece Storage

Piece-backed storage is internal to `Document`; batch parsing and `ParseStream`
keep their contiguous input paths. `Document.Text()` and `Node.Text` keep their
existing meanings. Input spend counts actual inspection and copying, not a
logical source length merely because it is retained. CodePoints initialization
decodes and normalizes input and charges that spend. A large immutable Bytes
source may be retained as an original string-backed piece without copying or
scanning it, so zero construction spend is valid when no input elements
were inspected. Parsing still charges probes, literal comparisons, text
extraction and materialization across pieces. `Document.Edit` is not metered.

Completed input tables may remain available after an interruption, but a
partially built table is never published and must be discarded. A pre-execution
option error or cancellation, before reusable state is touched, leaves existing
memo/repetition caches and the last `Stats` intact. Once execution starts, an
interruption clears the whole memo and repetition maps and VM/value/kid/undo
scratch; completed input tables remain valid. Retry with a fresh context and
budget must match fresh parsing. A completed parse may retain its fully built
input tables for later calls.

Per-call Document options may override context, limits, depth, and trace using
construction defaults as their starting point. Unit/backend/recognition mode
must remain the constructed document's mode; an effective incompatible value
returns an error before execution. Existing multiple-trace composition applies
once in order. No-argument calls are unchanged. A stored method value typed as
`func() (*Node, error)` needs a wrapper around `d.Parse()` after the variadic
signature change; document this small source migration explicitly.

Generated Go emits corresponding package-local Limits/errors/options and
uses one resolved control instance across Parse, ParseRule, Recognize, ParseAST,
and named ParseRuleAST where available. Named typed dispatch resolves options
once before lookup, then shares the resolved state through direct execution or
Node conversion; it must not reevaluate callbacks to establish a budget.

### Lazy Controlled Closure Setup

The closure backend keeps the ordinary compiled program as its default. When a
parse has active cancellation or a finite budget limit, the first controlled
Closure invocation lazily builds and caches a separate metered closure program.
Later controlled invocations reuse it; ordinary invocations continue to use the
plain program. Bytecode backends use their existing invocation-control path and
do not build a second closure program.

The controlled program is derived from the same validated grammar and preserves
rule metadata needed by the original program. It does not mutate or replace the
plain graph. A controlled Document parse selects the controlled program before
execution and retains the existing Document rule/run-site/description identity
used by its caches. Cache identity and retry behavior remain correctness
requirements.

Lazy construction is setup, outside the budget. Check the
invocation context immediately before and after setup; graph construction itself
is synchronous and cannot be interrupted. Keep context method calls outside the
`sync.Once` initializer so reentrant context methods cannot deadlock setup. An
inactive context such as `context.Background()` with zero limits produces no
budget control and does not materialize the second graph.

Ordinary Closure parsers retain only the plain graph until a controlled call;
after activation, the parser may retain both graphs. Measure default parsing,
first controlled-call latency and allocations, and post-activation retained
heap separately. The lazy setup and generated-Go budget-control implementations
are implemented with the measured costs summarized under Performance and
Results; further generated-Go optimization is a separate follow-up.

### What the Limits Count

The budget is one counter, cumulative per invocation and never rolled back on
backtracking, failed lookahead, recovery, LR growth, or repeated spending. Zero
disables the limit. A charge is checked before the charged operation; limit N
permits N charged units. If a next atomic operation requests more than the
remaining allowance, abort before it, with Used unchanged and Requested
explaining the failed reservation. Use subtraction/checks rather than
overflowing counters.

Every unit adds one to the counter, whatever its kind, so a bound on the counter
bounds scanning, recursion and value construction alike:

- **Execution events**, not source characters or CPU instructions, cost one unit
  each. Charge VM expression/action instruction dispatches; closure
  expression/action operations at their execution boundaries; generated rule
  entries and action operations at their emitted execution boundaries;
  rule/memo dispatch and LR/Operators Rule selection/growth iterations;
  repetition/resume iterations; and per-element spend in unbounded engine loops.
  Include graph edges visited during position movement, collection
  construction, and typed conversion, so a memo hit or converter cannot bypass
  the limit by returning a large graph. Bulk copies/materialization use bounded
  chunks with explicit charges in proportion to their size.
- **Inspected input representation elements** cost one unit each, not unique
  positions, the farthest examined offset, or the current input length. Charge
  decoding/preparation, terminal scans/comparisons, lookahead rereads, text
  extraction copies, offset/line-table scans, and engine text comparisons. One
  byte access costs one in byte storage; one rune access costs one in decoded
  storage. An EOF probe costs one. A repeated probe is charged again. Bulk
  comparisons reserve their declared compared range; a fast mismatch need not
  refund that reservation. An input charge is the elements it inspects and adds
  nothing for the act of reading. Scalar/action string spend and graph spend are
  charged by size even when they do not read the original input. An O(1)
  existing substring reference or completed table lookup does not charge the
  substring/table's entire size.

These are deterministic abstract charges for a fixed backend, generated artifact,
input representation, and version. Preparation may inspect raw bytes and later
matching may inspect decoded runes; both count. Therefore counts may differ
between position units, backends, optimized/reference generation, memo state,
and releases. They are not cross-backend instruction equivalence or a stable
complexity score. Publish the charging table with the implementation and test
its threshold behavior; do not claim equal budget success across implementations.
A prepared Document can need less spent budget than a new batch parse.

A resumed repetition charges the records/values actually processed, not all
historical input it skips. A constant-time memo hit spends dispatch budget but
no fictional scan of its whole examined range. Position/diagnostic updates and
actual traversal of a reused graph are still charged. Limits bound current spend,
not the amount of data originally needed to construct a memo result.

### Cancellation Checkpoints and Long Operations

Check context before execution, before/after reader and user callback calls, and
before result publication. Within controlled engine execution, target at most
256 charged units between cancellation checks. Budget checks apply to each charge/reservation regardless
of the cancellation polling interval. These initial internal constants are
measurement targets, not a configurable API or a wall-clock latency guarantee.

Chunk long literal/class scans, input decoding/copying, text operations, line/
offset tables, rollback clearing, run/memo traversal, LR/Operators Rule loops, and typed
conversion. Polling only at rule entry or VM dispatch is insufficient when one
operation contains a large loop. A one-time bounds check before an unbounded
host-library copy does not establish bounded cancellation latency. Split such
work where possible; allocation, GC, stack growth, first-use setup, and caller
code remain nonpreemptible intervals to measure and disclose.

At a checkpoint where several conditions are observed together, cancellation
wins, then budget exhaustion. Otherwise the first
observed terminal condition wins. A budget can win before cancellation is next
polled; this is deterministic budget accounting, not a race-resolution promise.
Cancellation/limit failures take the fatal interruption route and cannot match
an alternative or be swallowed by predicate/action error recovery or #recover.
Do not construct ordinary syntax diagnostics after the terminal interruption
merely to fill in a location; that would perform additional unbudgeted scans.

### Results and Cleanup

Batch Node parsing returns nil on interruption; recognition returns the same
interruption error. Typed Go returns the selected entry point's established zero
value, preserving typed pointer/slice zeros and named-rule boxing semantics.
No partly converted typed object or earlier recovered tree is returned. A typed
conversion cancellation is an interruption of the whole invocation. Ordinary
completed syntax and consuming-recovery outcomes remain unchanged.

Arm cleanup immediately after acquiring scratch/control state, including input
initialization and conversion; do not rely on a normal-return recycle call.
Clear control/context references, capture frames, trails, stacks, pending
conversion maps/arenas, LR seeds and provisional memo dependencies before pool
reuse. Returned values from previous successful invocations remain independently
owned. Accounting and cancellation state must not enter saved modules, memo keys,
public Nodes, or result arenas retained by consumers.

Mandatory unwind/recycle after a terminal interruption runs to completion without
further cancellation or budget failures; otherwise a second interruption could
leave unsafe state in a pool. This cleanup is outside the exhausted budget and
is a separately measured nonpreemptible interval before the error returns.
Normal backtracking/rollback spend during execution remains charged and polled.

For an interruption after Document execution starts, drop the whole memo and
repetition maps and clear VM/value/kid/undo scratch. Completed entries can depend
on unfinished LR seeds; dropping only growing entries is insufficient. Preserve
fully completed input tables, but discard any partial table rather than publish
it. A pre-execution option rejection or cancellation, before reusable state is
touched, leaves the existing caches and last stats intact. Stats describes an
entered attempt with its existing meaning; budget counters appear on limit errors,
not in rule-evaluation statistics. Retry with a fresh context/budget must match
fresh parsing.

Document remains single-owner, and old mutable trees can still move during a
subsequent parse. Interruption is not a transactional rollback of legacy Nodes
already moved. Use Clone under its current contract to retain earlier results.

### Reader and Callback Ownership

Reader, trace, and emit callbacks run synchronously in the caller's goroutine.
The parser never closes the reader or starts an unmanaged goroutine to interrupt
Read. A blocked io.Reader is cancellable only through the caller's own reader/
deadline/closure arrangement; otherwise cancellation is observed after Read
returns. Likewise a callback that blocks is outside the engine's polling bound.
No further call starts after interruption has been observed.
Preserve a reader's returned non-EOF error; it wins over cancellation newly
observed after that same Read returns, just as an emit error does below.

Buffered reading may consume ahead of the parsed position. Input-budget limits
are not a maximum reader-byte-consumption promise, and cancellation cannot unread
prefetched data. After stream interruption, restart requires caller-managed input
replay; starting another parser at the current reader offset is not a promised
continuation mechanism.

Check immediately before emitting a completed element and after emit returns.
Previously delivered elements, including nil-valued elements, remain delivered
and caller-owned; cancellation does not roll them back or emit them again. If
emit returns an error, preserve that error rather than replace it with a newly
observed cancellation. Callback panics propagate unchanged after cleanup and
must not be mistaken for malformed-bytecode faults. Trace callbacks keep their
existing synchronous order/reentrancy ownership; no synthetic balanced exit
events are promised after an interrupted call. Option callbacks themselves are
caller code and are evaluated exactly once before execution controls apply.

### Portable Generated Equivalent

After core Go and generated Go meet their gates, extend existing TS options-aware
entry points with optional `signal` and `budget`. Preserve the
existing plain/default functions and typed conversion path. Use an exported
structural cancellation-signal interface with readonly aborted/reason properties,
so an AbortSignal fits without requiring a browser DOM typing dependency.
Validate a finite nonnegative safe-integer limit; zero means unlimited. Go uint64
limits outside the TS safe-integer range are not portable literal settings.

Return a distinct exported interruption error with cancellation/limit kind and
limit accounting; preserve an observed signal reason as cause and clear
parser-owned signal references after return. Preserved causes and host-managed
or native `Error.stack` internals may retain caller objects, so no reachability
guarantee extends through those values. Do not eagerly format or delete caller
errors or invoke caller-provided `Error.prepareStackTrace`. Use invocation-local
counters and conversion state.
Cancellation and limits return null Node/typed value with the interruption error;
ordinary nil success still has no error. Generated recognition reports it too.

A synchronous TS parse does not yield to event-loop timers: AbortController on
the same thread cannot acquire new cancellation while a long parse monopolizes
that thread. Pre-aborted or externally changing host signals can be observed at
polls; deterministic budget limits remain effective without event-loop scheduling.
Do not promise timer-driven responsiveness or add async yields implicitly.
Direct typed/Node conversion and recognition controls require their own actual
consumer/runtime tests. New helper names participate in the existing deterministic
export-name collision allocator.

### Implementation Sequence and Compatibility

1. Core Go batch controls, charging table, input/VM/closure loops, ownership and
   exact-parent/static-reference tests. Then Document invocation overrides and
   stream boundaries, with interrupted LR retry and callback tests.
2. Generated Go Node/recognition, direct typed and conversion routes; include
   selected named-rule dispatch when present. Prove option resolution once,
   typed-zero/error behavior, and private pool clearing.
3. TS portable limits/signal/errors using its existing options-aware convention;
   strict declaration consumers and actual runtimes, including synchronous
   scheduling limitations and deep/shared conversion graphs.

Each stage has a separate code/evidence review; none is implemented here. Core
Closure, recursive Bytecode and BytecodeIterative all require coverage, including
available AST-omitted bytecode parsers. Generated runtimes gain controls only
where entry points already exist; no streams/Documents are implied. Saved module
formats and grammar/action semantics stay unchanged. Update runtime, incremental,
streaming, code-generation and TypeScript guides with the applicable stage.

## Alternatives Considered

- **Depth limits only.** Existing compatibility baseline, but shallow scans and
  backtracking can perform broad work. Keep depth plus optional independent limits.
- **Only context, checked at rule entry.** Small change but misses terminal/action
  loops, reused graph spend, and blocked I/O. Poll long engine execution and document
  the caller-code boundary instead.
- **Wall-clock timeout goroutine or forced reader Close.** Can interrupt some I/O
  but introduces reader ownership, races, and goroutine leaks for arbitrary
  readers/callbacks. Prefer caller-owned I/O cancellation and cooperative checks.
- **One universal backend-independent instruction count.** Attractive for portable
  settings but would require a new normalized evaluator and charge optimized
  skipped spend artificially. Use documented backend-local abstract spend events.
- **Lifetime Document budget.** Can cap an editor session, but becomes exhausted
  after earlier unrelated edits and makes retry stateful. Reset per invocation;
  callers can apply their own session policy.
- **Return the best partial tree on cancellation.** Useful in some editors but
  exposes provisional LR/capture/conversion state and changes result guarantees.
  Return zero plus an identifiable interruption; preserve completed prior snapshots
  separately when that API is available.

## Testing

Focused validation passes for the full eight-corpus Go controls,
generated/runtime/retention checks, race checks, and the eight-corpus
TypeScript and conversion suite under Node 24.19.0 and TypeScript 5.9.3.
Reachability checks cover weak-GC release of batch/stream contexts and input,
plus TypeScript owner-signal collection and custom-context fault paths for the
exercised backends. TypeScript native `Error.stack` and preserved cause
references are outside the signal/input collection contract. The full root
suite passes across 18 packages. The validation covers the following cases:

- Pre-cancelled/deadline contexts, cancellation at deterministic checkpoints,
  nil/unlimited defaults, last-option wins, exact once/reentrant option callbacks,
  exact budget boundary/reservation failure and uint64 overflow prevention.
- Long atomic/literal/class scans, empty/nullable loops, shallow alternatives and
  backtracking, lookahead, predicates/actions and long text operations, LF/CRLF
  table scans, malformed/truncated UTF-8 and raw text, both position units.
- Every backend; recursive/indirect LR and provisional completed dependencies,
  Operators Rule, cuts, recovery, memo hits, resumed long repeats, position-valued actions,
  deep/shared graph movement/conversion. Interruption bypasses recovery and
  ordinary error translation; errors.Is/As and typed zeros are preserved.
- Document interrupt/retry and edit/retry against fresh parsing; no-edit memo-hit
  cancellation; active abort drops complete reusable state while pre-entry
  rejection leaves it untouched. Retained old Clone/results remain owned.
- Reader blocking/return/error behavior, buffered prefetch, emit errors, panics,
  cancellation inside callbacks, previously emitted ownership, nested parses,
  concurrent independent calls, pool reuse, partial initialization/conversion,
  and context/large-input release after forced GC. Race checks use one Document
  owner, not unsupported concurrent Document mutation.
- Strict generated Go/TS consumers, native context error classification, portable
  cancellation reason, invalid TS limit values, export collisions, actual Node
  runtime and same-thread signal scheduling. No timer test may claim a synchronous
  parser yields without an explicit asynchronous API.

## Performance and Results

The lazy Closure setup and generated-Go budget-control implementation are
implemented with the measured costs below. Piece-backed `Document` input is
already adopted independently. Measurements compared main `77f12f2`,
the eager-graph prototype `2ae994f`, and the lazy implementation described in
this record; source fingerprints were fixed. Five alternating one-second rounds
used Go 1.27.1 on Apple M3 Max (darwin/arm64), `GOMAXPROCS=16`, `GOGC=100`, and
no memory limit. In Closure/CodePoints no-control parses, lazy/main paired
median ratios were 1.0752 for JSON (range 1.0627–1.0934) and 1.1038 for XML
(1.0619–1.1231); allocation counts matched. Source-preparation allocation
counts also matched main (JSON 2,474; Minilang 7,464) and were lower than the
eager control (3,117; 8,897); allocated bytes were not identical. Active
controlled Closure/CodePoints parse medians were near eager (1.0047 JSON and
1.0011 XML; both paired ranges included parity). Both variants used the original
unwarmed harness, with no full parse before `b.Loop`; the measurements include
any first-use graph activation in the timed loop and are not warmed steady-state
or allocation evidence.

A later experiment split the unmetered copy, append, and clear paths from
their controlled loops; the Go 1.27.1 compiler inlined the small entry
functions. Five alternating one-second rounds compared seven
Closure/CodePoints profiles: ordinary JSON/XML parsing, Document incremental
and long-input parsing, streaming, and controlled JSON/XML parsing. Both
variants in the new controlled-call profiles performed one full parse before
`b.Loop`; the earlier eager/lazy profiles above used the same original
unwarmed harness for both. The seven inline/lazy paired medians ranged from
0.9784 to 1.0210, every paired range included parity, and allocation counts
matched. The split was rejected for lack of a consistent timing benefit. Cold
setup and retained heap were not remeasured.

Cold first controlled Closure/CodePoints parses on empty input, with grammar
compilation excluded, had median times of 7.34 to 75.63 microseconds for JSON
and 14.01 to 177.19 microseconds for Minilang from eager to lazy. These five
alternating 50-call probes include first-use graph setup, not graph-only timing.
For N=128 unparsed compiled Parser owners, pre-activation retained-heap medians
were 1.0021–1.0055 times main. After one empty-input controlled parse per owner,
retained heap was 1.1019–1.1609 times eager across the four measured grammars.

### Broader Backend and Workload Comparison

Three alternating 500 ms rounds compared main `77f12f2` with prototype
`d6e4e7f` across 131 profiles on Go 1.27.1, Apple M3 Max (darwin/arm64),
`GOMAXPROCS=16`, `GOGC=100`, with no memory limit. Paired median ratios against
exact main ranged as follows:

| Profile group | Candidate/main paired-median ratios |
|---|---:|
| Core Closure parse and recognition | 1.053–1.205× |
| Core bytecode parse and recognition | 1.074–1.160× |
| Core iterative parse and recognition | 1.061–1.133× |
| Generated Go Node parse and recognition (24 profiles) | 1.175–1.503× |
| Generated Go typed parse (8 profiles) | 1.192–1.402× |

The exact-parent comparison captures total implementation cost, including state
layout. The same-source `zp_budget_controls_reference` disables core
instrumentation checks but retains core state layout. It does not isolate
generated-Go instrumentation: the generated sources are identical across those
variants and retain `budgetControlsEnabled=true`. The generated-Go results above
are valid exact-parent comparisons, not a generated-instrumentation ablation.
Improve the generated-Go inactive path and repeat the affected comparison
before adoption.

On the same candidate, controlled core parsing across all three backends and
both position units versus the same warmed inactive paths had paired median
ratios of 1.448–1.677× for context-only, 1.533–1.799× for budget-only, and
1.535–1.791× for combined controls. These three-round measurements characterize
the tested JSON/XML profiles, not all controlled workloads.

Cancellation probes used an 8 MiB ASCII class scan per tested backend and
position unit. Input decoding had completed before the main-entry trace;
cancellation was requested 1 ms later. Across 21 probes per backend/unit,
cancel-to-return medians were 4–11 microseconds, with a maximum of 33
microseconds. In the gated-reader probe, the caller remained blocked for about
10.8–11.0 ms; after release, return latency was 9.4–20.1 microseconds median
and at most 64.1 microseconds. These are observations for the stated probes,
not universal latency bounds. Synchronous parsing cannot preempt a caller-held read.

### Generated Go Ordinary-Path Specialization

[Optimization 096](../../optimizations/096-specialize-generated-go-method-helpers.md)
records the selected nil-control method branches and ordinary allocation,
capture and expectation helpers. Three alternating 500 ms samples across all
131 profiles against `77f12f2` reduced the generated Node/recognition range to
1.039–1.332× and typed parsing to 1.082–1.234×. A same-source generation-only
ablation measured 0.760–0.969× ordinary Node/recognition time and 0.815–0.936×
typed time; eight active profiles had medians of 0.985–1.005×. Active harnesses
do not pre-parse, and noisy individual sample ranges do not establish warmed
or universal parity. A fixed-100-iteration Minilang typed-parse allocation
control found no persistent allocation increase.

The three measured JSON/XML/Minilang consumer executables grew 7.32–20.04%
against pre-feature main and 5.08–14.49% against the same-source ablation.
Generated source grew 37.61–58.71% against the ablation. This trades code size
for ordinary throughput; total feature cost remains above pre-feature main.
Broader runtime-helper and separate ordinary-method alternatives were measured
and rejected. Prescribed local checks and independent source/evidence review
passed. The residual budget-control costs are accepted; further
ordinary-path generated-Go optimization is a separate follow-up. These local
checks do not imply remote CI or deployed behavior. No zero-cost or general
speedup claim follows.

Use ordinary small/large batch Node, recognition, direct typed and conversion
workloads; shallow scanning/backtracking; Document no-edit/edit/reparse/resume;
streams; LR/Operators Rule and capture-heavy results. Separate initialization, evaluation,
conversion, cleanup, and first-use setup. Report paired time, B/op, allocations,
peak/post-GC heap, retained context/input release, and budget spent. Include runs
that cross normal GC phases; an inactive parser field can change object size,
allocation cadence and scanning costs even without an executed poll. Do not
change production GC settings to mask those costs.

Test a small invocation-owned sidecar against added parser fields before choosing
storage; a nil sidecar still changes layout and access costs. Static references
are test/build or private generation controls, not public mutable switches.
Check latency at the proposed polling quanta and worst single library/allocation
intervals. Adoption needs measured default-path costs and declared tradeoffs;
record actual results with the implementation, without claiming zero overhead.

## Limitations and Open Questions

The proposed defaults form one coherent contract: no controls unless requested,
per-invocation counters, cooperative context/signal polling, backend-local budget
charges, zero results on interruption, and caller-owned blocking operations.
No owner approval choice is required to draft or test those defaults. Measured
coverage and latency must determine internal polling/chunk sizes and state
storage; if any long operation cannot be chunked safely, document that specific
unbounded interval before claiming a completed cancellation stage.
