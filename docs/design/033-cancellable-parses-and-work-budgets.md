# 033. Cancellable Parses and Work Budgets

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Add invocation-local cancellation and optional input-work/execution-step limits
to parsing. Start with the core Go API, including Document and streams, then
apply the same interruption contract to generated Go and portable generated
TypeScript. Existing depth limits remain independent. Ordinary calls with no
controls keep their result and callback semantics.

The limits bound counted engine work, not elapsed time, memory, compilation, or
arbitrary caller code. Cancellation is cooperative at defined engine checkpoints;
it cannot make a blocking io.Reader or callback return. Interrupted parses
publish no partial batch result and release their mutable execution state.

## Motivation

A shallow grammar can repeatedly scan long input, backtrack across alternatives,
or perform many LR/Pratt growth rounds without reaching its depth limit. Editors
also need to abandon outdated work while preserving a Document for retry.

Source inspection at `6fac14632cbf933d91fee7f8e5df9e82fb134c56` establishes the
existing boundary:

- [`ParseOptions`](../../internal/engine/input.go) carries unit, backend,
  recognition, depth, and tracing; [`public APIs`](../../pego.go) use functional
  ParseOption arguments. Document stores its construction settings; Parse has
  no per-call arguments today.
- [`run`/`recoverParse`](../../internal/engine/parse.go) distinguish completed
  syntax/recovery outcomes from fatal interruptions. Recognition skips actions
  whose values predicates do not need; predicate-dependent rules and callees
  can still execute actions. Streams synchronously call a caller-owned reader
  and emission callback.
- [`Document.Parse`](../../internal/engine/document.go) discards memo, repetition,
  and VM scratch state after an aborted parse because completed entries can depend
  on unfinished LR seeds. Ordinary failures retain reusable caches.
- [VM execution](../../internal/engine/vm.go), [iterative frames](../../internal/engine/ivm.go),
  [input loops](../../internal/engine/input.go), and
  [repetition resumption](../../internal/engine/resume.go) contain work outside a
  rule-entry depth check.
- [Generated Go](../../internal/engine/genrt/runtime.go) resolves options per
  invocation, pools private scratch, and separately supports
  [typed execution](../../internal/engine/genrt/typed.go). Depth options already
  apply to Node, recognition, and typed entry points under
  [design 022](022-generated-parser-depth-limits.md).

No overhead or latency measurements are supplied here. A state/layout change can
alter allocation and GC costs even when a new control is inactive. Static
reference and exact-parent controls are required, rather than inferring zero
cost from a disabled branch.

## Goals

- Cancel or exhaust a budget across ordinary calls, scans, actions/predicates,
  repetition, LR/Pratt, memo hits, resumption, and result conversion.
- Return identifiable interruption errors and safe zero results; consuming
  grammar recovery must not absorb an interruption.
- Reset all provisional/reusable execution state after active interruption while
  preserving Document text/options and already delivered stream elements.
- Define counter units, reset points, callback ownership, polling bounds, and
  implementation-dependent counts honestly.
- Validate default-path cost against an exact parent and a same-revision static
  reference, including GC-phase costs, before adopting the implementation.

## Non-goals

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
type WorkLimits struct {
    MaxInputWork uint64
    MaxSteps     uint64
}
func WithContext(ctx context.Context) ParseOption
func WithWorkLimits(limits WorkLimits) ParseOption

// Existing Parse/ParseStream/RecognizeOnly calls accept the same options.
func (d *Document) Parse(opts ...ParseOption) (*Node, error)

var ErrWorkLimit error

type WorkLimitKind uint8
const (
    InputWorkLimit WorkLimitKind = iota
    StepWorkLimit
)
type WorkLimitError struct {
    Kind      WorkLimitKind
    Limit     uint64
    Used      uint64
    Requested uint64
}
func (e *WorkLimitError) Error() string
func (e *WorkLimitError) Is(target error) bool // target == ErrWorkLimit
```

Nil context means context.Background; zero limits mean unlimited. Options apply
once in order, and the last context or entire WorkLimits value wins. There is no
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
until the Document is released. Edit remains outside parse controls.

Per-call Document options may override context, limits, depth, and trace using
construction defaults as their starting point. Unit/backend/recognition mode
must remain the constructed document's mode; an effective incompatible value
returns an error before execution. Existing multiple-trace composition applies
once in order. No-argument calls are unchanged. A stored method value typed as
`func() (*Node, error)` needs a wrapper around `d.Parse()` after the variadic
signature change; document this small source migration explicitly.

Generated Go emits corresponding package-local WorkLimits/errors/options and
uses one resolved control instance across Parse, ParseRule, Recognize, ParseAST,
and named ParseRuleAST where available. Named typed dispatch resolves options
once before lookup, then shares the resolved state through direct execution or
Node conversion; it must not reevaluate callbacks to establish a budget.

### What the limits count

Counters are cumulative per invocation and never rolled back on backtracking,
failed lookahead, recovery, LR growth, or repeated work. Zero disables that
counter. A charge is checked before the charged operation; limit N permits N
charged units. If a next atomic operation requests more than the remaining
allowance, abort before it, with Used unchanged and Requested explaining the
failed reservation. Use subtraction/checks rather than overflowing counters.

**Steps** are backend execution work events, not source characters or CPU
instructions. Charge VM expression/action instruction dispatches; closure and
generated expression/action operations at their emitted execution boundaries;
rule/memo dispatch and LR/Pratt selection/growth iterations; repetition/resume
iterations; and per-element work in unbounded engine loops. Include graph edges
visited during position movement, collection construction, and typed conversion,
so a memo hit or converter cannot bypass the limit by returning a large graph.
Bulk copies/materialization use bounded chunks with explicit step charges.

**Input work** counts cumulative inspected input representation elements, not
unique positions, the farthest examined offset, or the current input length.
Charge decoding/preparation, terminal scans/comparisons, lookahead rereads, text
extraction copies, offset/line-table scans, and engine text comparisons. One byte
access costs one in byte storage; one rune access costs one in decoded storage.
An EOF probe costs one. A repeated probe is charged again. Bulk comparisons
reserve their declared compared range; a fast mismatch need not refund that
reservation. Scalar/action string work and graph work also consume steps, even
when they do not read the original input. An O(1) existing substring reference
or completed table lookup does not charge the substring/table's entire size.

These are deterministic abstract charges for a fixed backend, generated artifact,
input representation, and version. Preparation may inspect raw bytes and later
matching may inspect decoded runes; both count. Therefore counts may differ
between position units, backends, optimized/reference generation, memo state,
and releases. They are not cross-backend instruction equivalence or a stable
complexity score. Publish the charging table with the implementation and test
its threshold behavior; do not claim equal budget success across implementations.
A prepared Document can need less counted input work than a new batch parse.

A resumed repetition charges the records/values actually processed, not all
historical input it skips. A constant-time memo hit consumes dispatch work but
no fictional scan of its whole examined range. Position/diagnostic updates and
actual traversal of a reused graph are still charged. Limits bound current work,
not the amount of data originally needed to construct a memo result.

### Budgeted action-error text

Action failures whose messages can include grammar-controlled names or input text
must charge the work while those fragments are constructed. Build known
engine-owned leaf messages eagerly, even if a predicate or assignment may later
discard the returned error. Charge one step per fragment during sizing and
another during writing, including empty fragments, and reserve output bytes in
bounded chunks before copying them. Apply the same accounting to the final `action in <rule>: <cause>` prefix. Do not
format an unbounded string first and charge its size afterward; that permits a
failed expression or final wrapper to bypass the work budget.

On successful construction, preserve the existing public error text, evaluation
order, and native flat-error behavior. The final action prefix contains the
cause text but does not unwrap or expose the cause identity. Do not add `%w`,
sentinels, quoting, truncation, or new location text. Release action-created
scratch before wrapping or publishing the error.

Keep arbitrary formatter behavior at a private opaque boundary. A foreign
`error` may implement `fmt.Formatter` or `Error()` with caller code, so it cannot
be made preemptible without changing semantics. Preserve native formatting for
unrecognized errors, checking cancellation immediately before and after that
call; document that the call itself is not interruptible. Known engine-owned
messages use bounded private construction and are flattened before publication.
Avoid routing every error through a new wrapper or changing generic `p.fail`
formatting.

### Guard-rejected choice alternatives

The closure backend charges one step before skipping each alternative rejected
by a first-rune guard. This charge applies even when silent lookahead or an
older failure position suppresses expectation collection. An alternative that
passes its guard retains the existing matcher-dispatch accounting, without an
extra skip charge. Bytecode backends already charge guard instructions.

Interruption in the skipped-alternative loop is fatal, including inside
negative lookahead. It cannot become a successful negation, select another
alternative, or publish ordinary diagnostics. These backend-local steps do not
promise identical budget thresholds across implementations.

### Cancellation checkpoints and long operations

Check context before execution, before/after reader and user callback calls, and
before result publication. Within controlled engine execution, target at most
256 charged steps or 4,096 charged input elements between cancellation checks,
whichever comes first. Budget checks apply to each charge/reservation regardless
of the cancellation polling interval. These initial internal constants are
measurement targets, not a configurable API or a wall-clock latency guarantee.

Chunk long literal/class scans, input decoding/copying, text operations, line/
offset tables, rollback clearing, run/memo traversal, LR/Pratt loops, and typed
conversion. Polling only at rule entry or VM dispatch is insufficient when one
operation contains a large loop. A one-time bounds check before an unbounded
host-library copy does not establish bounded cancellation latency. Split such
work where possible; allocation, GC, stack growth, first-use setup, and caller
code remain nonpreemptible intervals to measure and disclose.

At a checkpoint where several conditions are observed together, cancellation
wins, then input-work exhaustion, then step exhaustion. Otherwise the first
observed terminal condition wins. A budget can win before cancellation is next
polled; this is deterministic work accounting, not a race-resolution promise.
Cancellation/limit failures take the fatal interruption route and cannot match
an alternative or be swallowed by predicate/action error recovery or #recover.
Do not construct ordinary syntax diagnostics after the terminal interruption
merely to fill in a location; that would perform additional unbudgeted scans.

### Caller-context panic boundaries

`Context.Err` and `Done` are caller-owned methods and remain nonpreemptible.
Preserve their original panic values, including values implementing
`runtime.Error`, through initialization, execution, nested trace recovery, and
result publication. A caller panic must not become an invalid-bytecode error,
a cancellation error, or a partially returned result. Actual VM runtime faults
retain their existing invalid-bytecode classification.

Use private attribution around `Err` calls and remove it before re-panicking to
the caller. `Done` runs outside VM recovery during setup. An engine interruption
raised by a trace accessor within a context call still follows the fatal path;
already attributed panics must not accumulate additional wrappers. Preserve
cancellation-error identity and reservation precedence for returning contexts.
Release invocation scratch before a caller panic escapes, and keep earlier
results independently owned through retry.

### Results and cleanup

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

For native traced calls, retaining an event's exported metadata must not keep
the finished parser's input, reader, callback or execution references alive.
Detach these references on every terminal path: after scratch recycling for
batch calls, after reusable state transfer and abort invalidation for Documents,
and after error recovery and prefix delivery for streams. Do not mutate storage
owned by returned Nodes, live Document caches or recycled scratch. Do not close
caller-owned readers or undo delivered stream prefixes. Expectation and
recovered-error slices copied directly into events keep their existing ownership;
resetting the parser does not mutate their backing arrays.

Mandatory unwind/recycle after a terminal interruption runs to completion without
further cancellation or budget failures; otherwise a second interruption could
leave unsafe state in a pool. This cleanup is outside the exhausted budget and
is a separately measured nonpreemptible interval before the error returns.
Normal backtracking/rollback work during execution remains charged and polled.

For an active Document interruption, follow the existing abort rule: drop the
whole memo and repetition maps, clear VM/value/kid/undo scratch, and retain only
valid text/options/input tables. Completed entries can depend on unfinished LR
seeds; dropping only growing entries is insufficient. If initialization of an
input table was interrupted, discard that partial table. Stats describes the
attempt, with its existing meaning; work counters are reported on limit errors,
not silently added to rule-evaluation statistics. Pre-execution option rejection
or cancellation, before reusable state is touched, leaves the existing caches
and last stats intact. Retry with a fresh context/budget must match fresh parsing.

Document remains single-owner, and old mutable trees can still move during a
subsequent parse. Interruption is not a transactional rollback of legacy Nodes
already moved. Use Clone under its current contract to retain earlier results.

### Reader and callback ownership

Reader, trace, and emit callbacks run synchronously in the caller's goroutine.
The parser never closes the reader or starts an unmanaged goroutine to interrupt
Read. A blocked io.Reader is cancellable only through the caller's own reader/
deadline/closure arrangement; otherwise cancellation is observed after Read
returns. Likewise a callback that blocks is outside the engine's polling bound.
No further call starts after interruption has been observed.
Preserve a reader's returned non-EOF error; it wins over cancellation newly
observed after that same Read returns, just as an emit error does below.

Buffered reading may consume ahead of the parsed position. Input-work limits
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

### Portable generated equivalent

After core Go and generated Go meet their gates, extend existing TS options-aware
entry points with optional `signal`, `maxInputWork`, and `maxSteps`. Preserve the
existing plain/default functions and typed conversion path. Use an exported
structural cancellation-signal interface with readonly aborted/reason properties,
so an AbortSignal fits without requiring a browser DOM typing dependency.
Validate finite nonnegative safe-integer limits; zero means unlimited. Go uint64
limits outside the TS safe-integer range are not portable literal settings.

Return a distinct exported interruption error with cancellation/limit kind and
limit accounting; preserve an observed signal reason as cause without retaining
the signal after return. Use invocation-local counters and conversion state.
Cancellation and limits return null Node/typed value with the interruption error;
ordinary nil success still has no error. Generated recognition reports it too.

A synchronous TS parse does not yield to event-loop timers: AbortController on
the same thread cannot acquire new cancellation while a long parse monopolizes
that thread. Pre-aborted or externally changing host signals can be observed at
polls; deterministic work limits remain effective without event-loop scheduling.
Do not promise timer-driven responsiveness or add async yields implicitly.
Direct typed/Node conversion and recognition controls require their own actual
consumer/runtime tests. New helper names participate in the existing deterministic
export-name collision allocator.

### Implementation sequence and compatibility

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

## Prototype coverage

The `feat/parse-work-controls` branch starts the core batch stage. It provides
invocation-local context and overflow-safe work reservations, controlled input
preparation, scanner and execution-dispatch checkpoints, action-value checkpoints,
large character-class, batch memo/binding, detached input-text, capture-state,
and diagnostic checkpoints, and cleanup after initialization interruption or
caller panic. Recognition uses the same batch lifecycle. Individual trace-option callbacks stop before a subsequent callback
when cancellation is observed.

This foundation is not an adopted implementation of the full contract above.
Native Document and stream calls now use invocation-local controls through
reuse, text materialization and normal commit work. Known engine-owned action
errors and the final rule prefix use eager, bounded text construction.
Guard-rejected closure alternatives also consume steps independently of
diagnostic collection. Remaining matcher coverage, generated-runtime lifecycles,
copied-event ownership and first-use setup ordering need separate validation
gates. The prototype remains separate
from main until contract and performance gates pass.

The private `pego_work_controls_reference` build tag removes execution
instrumentation and rejects active controls. It retains the new state layout;
compare an exact parent as well to assess layout, compilation, and GC costs.
No timing conclusion is available while benchmark measurement is paused.

Current input charges reserve up to four raw bytes for each UTF-8 decoder call
in both sizing and storage preparation passes, one element per literal probe
or decoded-storage probe, and an EOF probe. A reservation can overcount an early
mismatch; counters do not roll back. Each scan operation also charges a step.
Backend dispatch and rule/growth charges differ between runtimes. The final
charging table must include remaining operations before these limits can be
presented as complete work bounds.

Action work consumes steps independently of input work: constructed strings and
collections need not refer to source text. Collection elements, fields, linked
locals/environments, lambda calls, ordinary action-arena resets, and runtime
concat analysis have step charges.
Explicit value copies and clears use chunks of at most 128 elements; scalar string
comparisons inspect at most 128 bytes from each operand per reservation, and
concatenation copies at most 128 bytes. Rune-length decoding reserves up to four
bytes of step work per decoder call, preserving malformed UTF-8 semantics.
Allocations, implicit buffer-growth copies, map hashing, and GC remain
nonpreemptible runtime intervals. With absent controls or the private reference
build, bulk operations retain their ordinary paths. This source-level separation
does not establish the default path's performance cost.

Large character classes charge one step before each non-ASCII range comparison,
including choice guards and value-free scans in closure and both bytecode VMs.
This bounds searches within an otherwise single matching operation. ASCII bitmap
lookups and fixed one/two-range comparisons are covered by their surrounding
scanner/dispatch charges; they do not add per-range charges. Range comparisons
consume steps only, since the input probe is already charged. Compile-time bitmap
construction remains outside parse work controls. Absent controls and the private
reference build retain uninstrumented range searches. Loaded bytecode uses the
same runtime checks; interruption is fatal through choice and recovery.

Batch memo/binding maintenance consumes steps, not input work. Key construction,
linked environment and memo-entry visits, key elements, and copied binding-prefix
nodes are charged. Each memo lookup/put or adaptive first-call operation also
charges fixed maintenance work; put reserves a final step before its atomic link
swap and retirement. String key/name comparisons use bounded byte comparisons,
while unequal string/key lengths are constant-time rejections. Interface equality
is preserved: a typed nil pointer is distinct from a nil interface. The shared
all-nil key for an absent environment and at most 16 variables performs no
fictional environment traversal.

Slot/seen-buffer copies and spare seen-word clears use chunks of at most 128
cells. Replacement prefixes and grown buffers are published only after their
explicit work completes. No checkpoint separates replacing an entry from retiring
its bookkeeping, and retirement never clears its public Node pointee. Mandatory
scratch release disables controls before resetting used memo cells, including
unpublished growth candidates. Runtime allocation/zeroing, map hashing, and GC
remain nonpreemptible; these budgets are not memory limits. Inactive/reference
calls retain the ordinary memo and binding paths and adaptive thresholds. Stream
pruning and Document edits/resumption remain for their lifecycle stages.

Detached batch input text is materialized with bounded work when no original
source substring is available, including CodePoints input containing invalid
UTF-8. Byte copying reserves at most 4096 input units and one step per chunk.
Rune conversion sizes and encodes in separate passes of at most 128 runes per
chunk, charging one input unit and one step per rune in each pass. Invalid scalar
runes retain Go's replacement-character behavior. Output sizing is checked before
allocation, and only a complete immutable string is returned; input and scratch
buffers are never borrowed by the result. Original-source substrings remain
constant-time when their offsets are ready. Allocations and builder growth remain
nonpreemptible. Trace text accessors propagate engine interruption through the
normal fatal boundary. Inactive/reference conversions retain the native paths.
Stream snapshots use the same controlled detached-text conversion, described
below.

Batch capture-state work consumes steps: one before each reverse undo assignment,
frame/scope visit, VM child assignment, cut-stack entry inspection, and failure
unwind pop. Field assignment uses controlled name comparisons and traversal.
Pratt frame snapshots and restores and projected-repetition slot clears reserve
chunks of at most 128 pointers. Undo order, nil captures, field order, self-capture
wrapping, and longest-operator tie selection retain their existing semantics.
Only private slots and containers are restored or cleared; retained public Node
pointees are never changed. A midway interruption aborts the whole batch, with
no partial result, alternative, recovery, or charged rollback during mandatory
scratch cleanup. Inactive/reference undo, copy, clear, and field paths retain
native operations.

Batch diagnostics charge expectation-bucket collisions, snapshot equality and
copying, expected-ID merge visits, and error descriptor traversal. Sorting a new
private Expected list uses bounded byte comparisons and explicit reference moves;
message encounter order and Go string byte ordering remain unchanged. Previously
published or memoized errors are never sorted or mutated. Recovered-error pointer
snapshots and replay in memo, left recursion, and Pratt parsing use bounded copies
and preserve order and pointer identity. Partial snapshots are not published.

Engine-owned recovery Error-node messages reserve sizing visits and copy text in
chunks of at most 128 bytes into an unpooled immutable builder. Empty message
parts still consume visits, and output sizing rejects integer overflow before
allocation. Recognition avoids this materialization because it builds no Error
node. Public post-return Error and Message methods remain caller work. Fatal
interruption during diagnostic construction, including a TraceEvent failure
accessor, bypasses recovery and alternatives; retained results survive scratch
reuse. Inactive/reference paths retain native equality, copying, sorting and error
formatting. Allocation, implicit slice growth, map hashing and GC remain
nonpreemptible.

Engine-owned action errors construct their text eagerly, so a failed predicate
cannot discard an uncharged large diagnostic. A private text-only error owns the
immutable leaf message; the final action prefix copies it with the same bounded
builder and publishes a flat error with no cause unwrapping. Sizing and writing
each charge one visit per fragment, including empty strings; copying reserves
steps in chunks of at most 128 bytes. These charges consume no input-work units.
Inactive paths use native string joining or formatting. Shared unary, arithmetic,
and collection helpers preserve their successful results and evaluation order.
Private opaque errors keep native `%v` formatting with cancellation checks before
and after it; arbitrary formatting code itself remains nonpreemptible.

Exact and one-short builder budgets, atomic chunk rejection, empty-fragment and
copy cancellation, and immutable text retention isolate formatting work. Public
batch tests cover all three backends and both position units, fallback bypass,
predicate-dependent recognition actions, ordinary message parity, and pooled
retry. Replacing only the bounded builder with an uncharged native join makes
the public interruption tests fail. These gates establish scoped correctness;
remaining matcher work, lifecycle, generated-output, setup, and default-cost
gates are still pending.

Guarded-choice tests isolate the rejected-alternative loop after input setup.
Exact and one-short budgets cover silent lookahead, suppressed older
expectations, and EOF; deterministic cancellation interrupts the silent loop,
and passing guards retain their matcher charges. Public tests compare successful
and ordinary failed parses with absent or generous controls on every backend
and position unit, in Node
and recognition modes. Fatal interruption bypasses negation and fallback rules,
releases scratch, and preserves retained results through pooled retry. Removing
only the skipped-alternative charge makes the isolated regression tests fail.

Caller-context panic tests cover setup, controlled input preparation, trace
prechecks and postchecks, result publication, rejected reservations, and counted
work. Every backend and position unit is exercised in Node and recognition
modes, with a public-option smoke test as well. AST-omitted VM programs are
tested for Node parsing; recognition still requires an AST.
The counted-work test arms its context after setup and asserts the polling
counters, independently of preparation and trace work. Earlier Nodes remain
unchanged through interruption and retry; observed scratch/control references
are released before the panic escapes.

Private attribution preserves exact caller panic identity through nested trace
recovery and recursive context polls. A trace accessor's work interruption inside
a context method stays fatal. Corrupting an already prepared VM still returns
its ordinary invalid-bytecode error, with cleanup and retry after restoration.
Disabling context attribution or outer unwrapping makes the corresponding
regression tests fail. Context calls remain nonpreemptible, and the active-poll
cost needs later measurement; this establishes no default-cost or full-feature
adoption claim.

Batch trace-event lifecycle tests retain exported event metadata after active
and inactive Node/recognition calls on every backend and position unit. They
check successful action fields and terminals, ordinary syntax failures, and
earlier Nodes across retries with different input. Cancellation, step exhaustion,
caller-context panic and final-publication panic detach parser references; trace
callback panic tests cover Node parsing on every backend and unit. The batch
teardown preserves the run's aborted flag, recycles scratch first, then clears
the finished traced parser without mutating separately allocated result storage.
Low-level scratch recycling remains unchanged.

Recognition fixtures deliberately keep events and Programs alive while weak
references observe the original input allocation and a trace-captured Context.
Strong-reference positive controls precede bounded forced-GC checks. Both active
and inactive calls release those fixture objects; disabling batch teardown makes
the isolated GC tests fail. This proves these objects' release, not a retained
heap-volume or throughput result. The untraced teardown nil-check and traced
fixed-size reset need later cost measurement. Copied event slices and
generated-runtime lifecycles remain separate acceptance surfaces.

Traced Document and stream calls use the same parser-reference detachment, with
owner-specific deferred ordering. Document tests preserve transferred memo state
on success and ordinary failure, invalidate provisional state after trace panic,
and compare retries with fresh parsing. Cloned historical trees survive edits.
Stream tests retain delivered records across success, syntax failure, reader and
emit errors, and trace/emit panics. They check trace panic identity and ordinary
pointer-valued emit panics, alongside caller reader ownership. Stream recovery
also attributes reader and emit panics, including runtime.Error, to caller code.
Normal caller returns clear that attribution before engine work, preserving the
invalid-bytecode classification of later VM faults. Separate regression cases
cover returned-error identity, retry, buffered-reader reuse and nested tracing.
Both matrices cover every backend and position unit.
Exported event metadata remains available after the callback; state-reading
accessors are exercised only during callbacks.

Bounded weak-GC fixtures drop the Document and its returned Node before observing
original source and captured-callback allocations. Stream fixtures drop reader
and callback locals while retaining an independently owned emitted Node. Both
keep saved events and Programs alive, with strong-reference controls first.
Disabling only Document/stream teardown retains all observed fixture targets.
These checks establish specific object release, not heap volume or throughput.
The added untraced teardown checks and traced resets remain unmeasured. Active
Document entry points now resolve construction defaults and per-call overrides
once, with fresh construction and invocation counters. Fixed unit, effective
backend and recognition mode reject incompatible values before mutation.
Constructor preparation and admission/execution/publication checks preserve
caller-context panic identity. Pre-execution interruption preserves prior caches
and statistics; execution and publication interruptions invalidate reusable state.
Temporary control and traced parser references are detached before returning an
error or rethrowing a caller panic.

Parse-time Document edit replay now reserves a step for each historical edit,
committing memo coordinates only after the complete replay remains valid.
Repetition prefix/tail reuse, binary-search probes and suffix validation are
counted, as are historical node shifts and child/field traversal. Empty-node
copies preserve shared descendants; existing mutable nodes can move before an
interruption, without transactional rollback. Historical Clone ownership remains
unchanged. Edit still uses the ordinary replay helper outside parse controls.

Explicit run/child-stack growth copies and run-state clearing use bounded chunks.
Controlled growth keeps geometric capacity to avoid copying the accumulated
prefix on every appended element. Controlled run assembly uses private storage
and publishes the run record only after every copy finishes; the inactive path
keeps its existing in-place splice and storage reuse. Successful movement-map
cleanup is counted, while mandatory fatal Document cleanup remains unbudgeted.
Allocation, runtime zeroing, map operations and GC remain nonpreemptible intervals.

Regression fixtures cover stale memo calls, search/tail reservations, provisional
run publication, growth/clear interruption, shared empty/non-empty node movement,
and partial prefix or shifted-tail cancellation on all native backends and both
units. The interrupted call returns no result, discards reusable state, preserves
text/defaults and a historical Clone, and retries against fresh parsing. The
additional controlled storage and inactive-path branch/call costs are unmeasured.
Native stream controls now use one fresh invocation counter and validate fixed
rule/backend/recognition constraints before consulting caller Context methods.
Admission, setup and execution preserve caller panic identity, including
runtime.Error values. Traced terminal paths detach reader, emit, context and
control references without changing event metadata or delivered Nodes.

Controlled reads reserve one step per staged byte/code point and check before
and after synchronous reader calls. An observer inside an invocation-local
bufio.Reader checks immediately before and after each underlying Read, including
empty-read retries and refills that complete a UTF-8 rune. Cancellation prevents
a further caller read even while ReadByte or ReadRune is still running. The
observer records the caller's returned error before buffering can defer it.
A non-EOF error returned with data therefore wins over cancellation first
observed after that read. Without cancellation, buffered data is still delivered
before its saved error. Only controlled calls add this wrapper; inactive calls
keep ordinary bufio.Reader reuse. Already-buffered caller readers can gain an
additional buffer under active controls. Reader-byte consumption and prefetch
are not bounded by the representation-based input-work counter.

Emit has pre/post checks, preserves returned errors and exact panic values, and
does not retract a delivered element when the postcheck interrupts. Normal commit
counts frame scans, trail/value/frame clearing, memo slot and entry pruning, and
leftward input/memo compaction in forward chunks of at most 128 elements.
Partially compacted invocation state is discarded; caller replay, rather than
continuation from the consumed reader, is the retry contract. Mandatory terminal
teardown remains unbudgeted. Runtime allocation/growth, GC and caller code remain
nonpreemptible intervals.

Deterministic fixtures cover admission, unlimited-control parity, data-plus-error
precedence, blocked-reader cooperation, emitted-prefix ownership, exact copy and
clear reservations, growing memo seeds, caller panic attribution and fresh replay
on every native backend and both units. Retained events and Programs are tested
with weak references and strong-reference positive controls across successful,
syntax, budget, cancellation, reader/emit-error and caller-panic exits. This
establishes release of those fixture objects, not a retained-byte or timing bound.
The controlled observer/storage/accounting costs and inactive branch/call costs
are unmeasured. Raw-reader checks add a controller pointer to the observer and
extra Context polls only on controlled caller reads. Generated-runtime lifecycles
and setup/default-cost gates remain pending before full contract adoption.


Controlled stream snapshots reserve source copying through the bounded
materialization path. CodePoints snapshots additionally charge decoder visits
while building their small position table. A new source and offset table are
private until both complete; interruption leaves the previous cache intact.
Already returned immutable substrings remain unchanged after cache replacement.
Cache hits remain constant-time and materialization does not read ahead. Tiny
loaded fragments and oversized ranges use the bounded detached-text fallback.
Invalid UTF-8 and invalid scalar values retain the existing replacement policy.

Fixtures isolate sizing, encoding and final-offset budget boundaries, decoder
cancellation, exact and one-short quotas, no read-ahead, cache reuse, malformed
input and near-limit absolute positions. Public traced streams propagate text
interruption, detach terminal state and permit fresh replay on every native
backend and position unit. Controlled CodePoints cache misses allocate a new
small offset table to avoid mutating the previous cache during interruption;
its allocation and timing costs remain unmeasured while benchmarks are paused.
Inactive snapshots retain their existing copy and offset-table reuse paths.

## Alternatives considered

- **Depth limits only.** Existing compatibility baseline, but shallow scans and
  backtracking can perform broad work. Keep depth plus optional independent limits.
- **Only context, checked at rule entry.** Small change but misses terminal/action
  loops, reused graph work, and blocked I/O. Poll long engine work and document
  the caller-code boundary instead.
- **Wall-clock timeout goroutine or forced reader Close.** Can interrupt some I/O
  but introduces reader ownership, races, and goroutine leaks for arbitrary
  readers/callbacks. Prefer caller-owned I/O cancellation and cooperative checks.
- **Check only bufio's returned error.** Avoids a controlled reader observer,
  but a caller can return data and an error together while bufio reports a
  successful ReadByte/ReadRune. A post-read cancellation would then replace the
  reader error. Observe the caller result only for controlled streams, retaining
  ordinary buffered-data/error ordering when no cancellation is observed.
- **One universal backend-independent instruction count.** Attractive for portable
  settings but would require a new normalized evaluator and charge optimized
  skipped work artificially. Use documented backend-local abstract work events.
- **Lifetime Document budget.** Can cap an editor session, but becomes exhausted
  after earlier unrelated edits and makes retry stateful. Reset per invocation;
  callers can apply their own session policy.
- **Return the best partial tree on cancellation.** Useful in some editors but
  exposes provisional LR/capture/conversion state and changes result guarantees.
  Return zero plus an identifiable interruption; preserve completed prior snapshots
  separately when that API is available.

## Testing

These are proposed gates, not completed validation:

- Pre-cancelled/deadline contexts, cancellation at deterministic checkpoints,
  nil/unlimited defaults, last-option wins, exact once/reentrant option callbacks,
  exact budget boundary/reservation failure and uint64 overflow prevention.
- Long atomic/literal/class scans, empty/nullable loops, shallow alternatives and
  backtracking, lookahead, predicates/actions and long text operations, LF/CRLF
  table scans, malformed/truncated UTF-8 and raw text, both position units.
- Every backend; recursive/indirect LR and provisional completed dependencies,
  Pratt, cuts, recovery, memo hits, resumed long repeats, position-valued actions,
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

## Performance and results

No performance promise or measured result. Pin exact parent and candidate,
backend/unit, generated controls, compiler/runtime versions, grammar/input hashes,
and normal GC settings. Compare three controls: exact parent; same-revision
static build/generation with controls and added state/layout removed; candidate
with absent controls. Then measure context-only, budgets-only, combined controls,
and cancellation latency in engine work versus caller-blocked intervals.

Use ordinary small/large batch Node, recognition, direct typed and conversion
workloads; shallow scanning/backtracking; Document no-edit/edit/reparse/resume;
streams; LR/Pratt and capture-heavy results. Separate initialization, evaluation,
conversion, cleanup, and first-use setup. Report paired time, B/op, allocations,
peak/post-GC heap, retained context/input release, and work charged. Include runs
that cross normal GC phases; an inactive parser field can change object size,
allocation cadence and scanning costs even without an executed poll. Do not
change production GC settings to mask those costs.

Test a small invocation-owned sidecar against added parser fields before choosing
storage; a nil sidecar still changes layout and access costs. Static references
are test/build or private generation controls, not public mutable switches.
Check latency at the proposed polling quanta and worst single library/allocation
intervals. Adoption needs measured default-path costs and declared tradeoffs;
record actual results with the implementation, without claiming zero overhead.

## Limitations and open questions

The proposed defaults form one coherent contract: no controls unless requested,
per-invocation counters, cooperative context/signal polling, backend-local work
charges, zero results on interruption, and caller-owned blocking operations.
No owner approval choice is required to draft or test those defaults. Measured
coverage and latency must determine internal polling/chunk sizes and state
storage; if any long operation cannot be chunked safely, document that specific
unbounded interval before claiming a completed cancellation stage.
