# 043. Generated Parser Tracing and Profiling

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Add opt-in rule-call tracing and per-rule profiling to standalone generated Go, typed Go and TypeScript parsers. Instrument generated direct-call paths as well as runtime-dispatched calls, and report optimized skips as skips rather than pretending they were calls. Generated traces and profiles observe each generated parser's execution strategy; they do not promise event-for-event equivalence with engine traces or between backends.

## Motivation

Design 014 provides `pego.WithTrace`, `pego.Profile`, `pego.WithProfile`, and the `trace`, `profile` and `explain` CLI commands for the engine. Generated Go Node, typed Go, Go Document and TypeScript batch parsers support generation-time opt-in tracing and profiling. Generated APIs have their own event/profile contracts and do not promise engine equivalence. The engine event reports rule, position, binding level, depth, match, memo use, body evaluations and examined input; callbacks are synchronous, and their panic is re-raised unchanged. Profiles accumulate per-rule call/evaluation/memo/match/failure counts, consumed positions, repeats and inclusive/self time.

Generated Go and TypeScript runtimes specialize calls into direct generated methods/functions and common call sites bypass the generic `call` path (`internal/gen/gogen/generator.go`, `internal/gen/gogen/flowrule.go`, `internal/gen/tsgen/generator.go`, `runtime/runtime.go`, `internal/gen/tsgen/tsrt/runtime.ts`). Generated Go has ordinary and typed runtimes; TypeScript has its own runtime and can convert node results to typed values. Literal/first-character dispatch can skip a call, and deferred memoization changes which calls evaluate a body. Adding a hook only to `parser.call` would therefore miss optimized calls and produce incomplete counts. The Go and TypeScript slices have focused synthetic measurements below; broader workload measurements remain unavailable.

### Go Node Batch Trace

The initial Go batch implementation is generation-time opt-in tracing for
standalone generated Go Node parsers. Generation uses
`Observability: true` in `gen.Go` or `gen.TypeScript`, or `pego gen -observability`; enabled
parsers trace `Parse`, `ParseRule` and `Recognize`. Events cover actual plain,
direct, general, memoized and left-recursive calls, plus first-character
dispatch skips only where the generated call site can identify the target.
`ExaminedKnown` is false in batch parses. Named filters include descendants; unknown
names select nothing. The start rule has depth one, and nonpositive maximum
depth is unlimited. Callback panics propagate unchanged after cleanup, while
aborted parses may omit exits. Go Node and typed batch packages also expose the
aggregate `Profile` API described below.
Typed Go batch tracing is described below; Document tracing and profiling have
their own contracts below.

### Typed Go Batch Trace Extension

With `Observability` and `Types`, generated Go packages
trace `ParseAST` and `ParseRuleAST` as well as Node `Parse`, `ParseRule` and
optional `Recognize`. This covers direct typed execution and Node-conversion
fallback. Events describe parser calls made by the selected route; converting
a Node result to typed values is not a rule call and emits no separate event.
The same event, filter, depth, interruption and callback-panic contract
applies, though Node and typed traces can differ because their execution paths
differ. The typed package also exposes `Profile`.

### TypeScript Batch Trace

Generation-time observability also supports TypeScript batch parsers, with or
without `-types`, and with `-recognize` when recognition is generated. The
generated module adds `trace`, `traceRules` and `traceMaxDepth` to
`ParseOptions`; `parseWithOptions`, `parseRuleWithOptions` and
`recognizeWithOptions` accept these options. With `-types`, the configured-start
`parseASTWithOptions` does too. The legacy positional-unit APIs do not accept a
trace callback, and TypeScript does not generate a separately named typed-rule
API.

TypeScript exports `TraceKind` (`"enter" | "exit" | "skipped"`) and a
`TraceEvent` with camel-case fields: `kind`, `rule`, `level`, `depth`, `pos`,
`matched`, `end`, `memo`, `evals`, `examined`, `examinedKnown` and
`skipReason`. Positions use the selected unit. As with generated Go,
`examinedKnown` is always false, depth starts at one, and nonpositive
`traceMaxDepth` is unlimited. An omitted or empty `traceRules` delivers all
rules; a selected rule includes its descendants, and an unknown filter selects
no rules. Events are copied snapshots, so callback
mutation cannot change parser state. Callbacks run synchronously; thrown values
are rethrown unchanged after cleanup, and aborted parses can omit exit events.
Typed-result conversion does not create a separate rule event. TypeScript
batch profiling is described below. TypeScript Document event tracing and
examined-input metrics remain open.

## Goals

- Expose explicit, opt-in trace/profile support in generated Go, typed Go and TypeScript batch parsers.
- Observe direct, generic, value-free, typed and recognition calls with one documented event contract per generated backend.
- Distinguish entered calls, memo hits, left-recursive body evaluations, and calls skipped by generated dispatch.
- Define position units, filtering, callback panic/throw, reentrancy, concurrency, interruption and profile ownership.
- Preserve parsing results and errors with instrumentation enabled; measure generation size and default/opt-in runtime costs before choosing how commonly instrumentation is available.
- State where generated traces differ from engine traces and leave existing engine dispatch and Document-resumption observability gaps as separate work.

## Non-Goals

- Making generated parsers event-equivalent to engine backends or claiming portable timing equivalence between Go and JavaScript.
- Adding stream tracing to generated parsers. Generated stream parsers are not available.
- Changing memoization, first-character dispatch, rule evaluation order, parse results, or serialized `.pegoc` format.
- Providing expression-level tracing or tracing action internals.

## Design

### Generation-Time Opt-In and Public API

Generation-time opt-in keeps ordinary generated output free of observability fields, callbacks, clock reads, or trace branches. Go Node, typed batch, and Document parsers support `gen.Go{Observability: true}` and `pego gen -observability`; TypeScript batch tracing and profiling support `gen.TypeScript{Observability: true}` and the same flag. Generated Go profiling is available for Node, typed batch, and Document parsers.

Generated Go batch API:

```go
type TraceKind uint8
const (
    TraceEnter TraceKind = iota
    TraceExit
    TraceSkipped
)

type TraceEvent struct {
    Kind TraceKind
    Rule string
    Level, Depth int
    Pos int
    Matched bool       // exit only
    End int            // exit only
    Memo bool          // exit only; true when a memo result was reused
    Evals int          // exit only; includes left-recursive growth evaluations
    Examined int       // exit only; exclusive end in this parse's position unit
    ExaminedKnown bool // false when the generated path cannot account for it
    SkipReason string  // skipped only, for example "first-character"
}

func WithTrace(func(TraceEvent)) ParseOption
func WithProfile(*Profile) ParseOption
func WithTraceFilter(rules ...string) ParseOption
func WithTraceMaxDepth(n int) ParseOption

func Parse(input string, opts ...ParseOption) (*Node, error)
func ParseRule(name, input string, opts ...ParseOption) (*Node, error)
func Recognize(input string, opts ...ParseOption) error
```

For the Go Node and typed batch slices, tracing options compose with unit and depth options. Typed packages expose `ParseAST` and `ParseRuleAST` with the same `ParseOption` values, and optional `Recognize` remains available. Positions are half-open; `Examined` is an exclusive end where the backend can report it.

The Go Node slice supports `Parse`, `ParseRule` and generated `Recognize`; the typed Go slice also supports `ParseAST` and `ParseRuleAST`. Both export `TraceEvent`, `WithTrace`, `WithTraceFilter`, `WithTraceMaxDepth` and `WithProfile`. TypeScript tracing and profiling use per-call `ParseOptions` fields instead of Go functional options; TypeScript profiles use `Profile` and `ProfileSnapshot`.

The option callback receives a copied value event containing scalar/string fields, not a pointer into parser state. Callbacks run synchronously on the parsing goroutine/JavaScript stack. There is no retained input or parser-state accessor in this API. Per-invocation trace/profile state is private; concurrent or reentrant parses of the same generated parser have independent state. A `Profile` aggregates across calls and is not safe to share across concurrent parses.

### Event Semantics

- `TraceEnter` precedes the generated call's memo lookup or body dispatch. Every enter has one matching `TraceExit` unless parsing is aborted. `Depth` starts at one for the start rule; `Level` is the Operators Rule binding level, zero for calls without one.
- `TraceExit` describes one entered call. `Memo=true` means the call reused an existing memo result without evaluating its body, so `Evals=0`. Calls that evaluate left-recursion growth attempts have `Memo=false`; their `Evals` counts their own body attempts, including growth attempts, not nested calls. Recursive calls that reuse the growing seed have `Memo=true` and `Evals=0`. `Matched` and `End` describe the rule call, not whether the whole-input parse passed its end-of-input check. `Examined` is emitted only where the generated runtime can account for a truthful range; it must not be guessed from `End` when a lookahead or failed dispatch examined farther.
- `TraceSkipped` is an unpaired event for a generated call site whose existing dispatch condition proves that the target rule was not entered. It identifies the target rule, start position, depth, level and skip reason, and has no exit, result, memo or evaluation count. It is not included in entered-call or body-evaluation totals. Only instrumented generated dispatch sites that can name the skipped target emit it. An implementation that cannot recover a truthful target and position omits the skip event rather than fabricating one.
- A deferred-memo first evaluation produces an exit without a memo hit; a later lookup reports the memo event. Recognition events report recognition work without promising all actions are skipped: predicate-dependent actions and values still execute when required, as in ordinary recognition.
- Typed direct Go and Node-conversion Go report the parser's rule execution. Typed conversion time and allocations are outside per-rule event time unless a future API explicitly adds a conversion event. TypeScript typed-result conversion has the same boundary.

Generated backends can skip different candidate calls due to first-character dispatch and other generated specialization. Their event streams must be labeled as generated-runtime traces and can differ in which `TraceSkipped` events are representable. They must never be described as equivalent to engine traces. First-character trace differences between engine backends remain a separate limitation. Generated Go Document events report calls made during that invocation; a memo hit reports the reused call without replaying nested events from prior parses. Repetition-resumption events remain out of scope because generated Documents do not resume repetitions.

### Filtering and Profiling

`WithTraceFilter` / `traceRules` select rule names, and `WithTraceMaxDepth` / `traceMaxDepth` limit delivered events by depth. Filtering suppresses callback delivery only; it must not change parsing, dispatch or parent-depth numbering. Include calls nested within a selected rule, following engine `pego trace -rule` behavior. JSON/text formatting can be supplied as small generated-independent helpers or by adapting events in user code; generated parsers do not add CLI commands.

The generated Go `Profile` zero value is ready to use. Pass `WithProfile(&profile)` to accumulate measurements across calls, then call `profile.Snapshot()` for an owned snapshot. Snapshot rule rows and per-rule skip-reason rows are sorted by name/reason, so `json.Marshal(snapshot)` is deterministic. A nil profile disables collection. The profile sink is not safe for concurrent use; reentrant parses on the same goroutine keep independent per-invocation accounting.

The snapshot contains total `Parses`, `Entered`, completed `Calls`, body `Evals`, `MemoHits`, `Skipped` and root `Time`, plus per-rule entered/completed calls, evaluations, memo hits, matches, failures, consumed positions, skips by reason, repeats and the maximum evaluation count/location. `Entered` includes calls interrupted before exit; completed-call counters do not. Skipped dispatch targets are separate from entered calls and do not depend on trace filters. `Repeats` and `MaxEvals` count completed non-memo calls with `Evals > 0` at the same `(rule, position, level)` within an invocation; `Evals` itself counts body attempts, including left-recursive growth. When maximum counts tie, `MaxEvalsAt` keeps the first observed location. In batch parses `Examined` and `Wasted` are unavailable and their `Known` flags are false; they are not inferred from matched ranges. Document parses supply them, as described below. Position counts use each invocation's selected unit and are not normalized across calls that mix units.

Root `Time` sums completed root rule calls and excludes input setup, whole-input end checks and typed-result conversion. Per-rule `Time` counts recursive calls of that rule once within each invocation; `SelfTime` subtracts child time. Both include instrumentation and synchronous enter/exit callback time. Reentrant parses have independent accounting and are included in the outer callback interval as well as their own invocation totals. A completed-exit update is published after the exit callback returns or panics, so a snapshot taken inside that callback can omit that exit until it unwinds. Callback panics still propagate unchanged after cleanup. An exit callback panic counts that completed call; an enter callback panic does not synthesize an exit. The measurements below include the overall `WithProfile` path; they do not isolate individual counter or clock costs.

### Generated Go Document Tracing and Profiling

With `-document -observability`, generated Go Documents support `Parse` and,
with `-types`, `ParseAST` tracing and profiling. `NewDocument` accepts
`WithTrace` and `WithProfile` as retained defaults; construction itself emits
no events or profile counts. Per-call options override those defaults for that
invocation only and do not change later calls. `Edit` does not parse and emits
no rule events.

Events report calls made during the current invocation, including Node calls,
direct typed calls and Node-conversion fallback calls. Conversion itself is not
a rule call. Reusing a memoized whole call reports that call with `Memo=true`
and `Evals=0`; nested calls from earlier parses are not replayed. Generated
Documents do not resume repetition bodies, so there is no `ReusedElements`
counter.

Exit events of Document parses report `ExaminedKnown=true`, with `Examined`
set to the frontier the Document keeps for edit reuse. A call that records a
memo result reports the frontier stored with it, and a memo hit reports the
stored frontier it reuses, but at least one position past its start. Each wrapper starts the call's frontier at its
position and merges it back into the caller's afterwards, so the caller's
earlier lookahead is excluded and the Document's own frontier is unchanged.
The frontier is conservative rather than the engine's exact examined range:
calls that record or reuse memo results count at least one position past their
start and end, which keeps edit reuse safe without tracking every read for
validity. Reporting this frontier makes the trace explain reuse decisions
exactly; an exact range would need a second high-water mark and an extra field
per memo entry. A recursive call that reuses a growing left-recursive seed sees
the frontier stored with an earlier seed, so any call completing in such a
provisional context reports at least its end; the raise affects only the
report, not the Document's frontier. Values can be one position past the end
of the input.

The examined frontier was measured against the previous observed source on the
same generated Document fixture with 4,096 records: node and typed drivers,
both units, batch, cold, warm and edit parses, each traced and profiled, in
eight alternating rounds of 400 iterations. Median time ratios were
0.974–1.017, except for the profiled warm byte-unit node parse, a 0.4 µs memo
hit whose ratio was 1.069 (0.97–1.48). Allocations per parse were unchanged.
An earlier version that kept unknown rules in a map measured up to 1.040 for
profiled batch parses; a flag on each rule row removed that lookup. Without
observability the same hooks are no-ops behind a nil observation. Profiles sum `Examined - Pos` per
rule over completed calls into `Examined`, and over completed failed non-memo
calls into `Wasted`; a rule's values are known only when every completed call
in the profile was a Document call.

After a completed parse, the invocation's increases in profile `Evals` and
`MemoHits` match `Document.Stats().Evaluated` and `.Reused`, respectively.
`Stats` has no call counter. Interrupted
parses can leave statistics for calls that began but did not complete. Temporary
observer and profile state is released after return or panic; constructor
defaults remain for later calls. While a Document parses, its `Parse`,
`ParseAST` and `Edit` return the generated `ErrDocumentBusy` without changing
it, so a callback cannot re-enter the same Document; callers can schedule later
work or use another Document. Every generated Document has the guard, but only
observability callbacks, the documented way to run caller code during a parse,
can trigger it, so only sources generated with observability export
`ErrDocumentBusy`. A `context.Context` passed with
`WithContext` also runs caller code, but re-entry from it is not detected. The
check precedes option validation, so a re-entrant call with invalid options
still reports `ErrDocumentBusy`.

The guard was measured against the same observed source without it, on the
generated Document benchmark fixture: 4,096 records, node and typed drivers, both
units, cold, warm and edit parses, with and without a trace, in ten alternating
rounds of 2,000 iterations. Median time ratios were 0.986–1.036; the largest
ratios came from the sub-microsecond warm byte-unit parse, whose rounds ranged
0.73–1.49. Bytes and allocations per parse were unchanged.

On the same fixture, observed source without a trace or profile was compared
with ordinary generated source. Median time ratios were 1.000–1.044, except
for the warm byte-unit parse: it is a 0.2 µs memo hit, and its ratio was 1.18
(0.83–1.28). Warm parses allocate 48 more bytes per call; cold and edit parses
allocate within a few bytes of the ordinary source. Sources generated without
observability share the runtime: each observed call path is a separate method
selected by a nil check of the parse's observation, and the other hooks return
at once when it is nil.

Generated Document race tests with Go Profile behavior, regeneration coverage,
full root, nested-module and site validation pass. The broader design remains
proposed.

### TypeScript Batch Profiling

With generation-time observability enabled, TypeScript batch parsers export
`Profile` and `ProfileSnapshot`, and add `profile?: Profile | null` to
`ParseOptions`. Pass a `Profile` to `parseWithOptions`, `parseRuleWithOptions`,
`recognizeWithOptions`, or, with `-types`, `parseASTWithOptions`. Legacy
positional-unit APIs do not accept a profile. Each invocation keeps private
accounting state; the sink aggregates completed calls across invocations and
retains no input, callback, parser history or per-call frames. A profile is not
safe for concurrent use. `snapshot()` returns independent rule rows sorted by
name, skip reasons sorted by reason, and an owned `maxEvalsAt` location for
deterministic JSON serialization.

Snapshot fields use camel case and correspond to the Go profile counters.
`entered` includes calls that began; completed-call counters and timings omit
unfinished calls. `examinedKnown` and `wastedKnown` are false because these
ranges are unavailable. `time` and `selfTime` are milliseconds from the host's
monotonic `performance.now()` clock; valid clocks with coarse resolution may
produce zero durations. Root time excludes input setup,
whole-input end checks and typed-result conversion; per-rule time counts
recursive calls of that rule once per invocation, and `selfTime` excludes child
time. Both include synchronous tracing callbacks. When the exit-time clock
read succeeds, completed-call counters are published after an exit callback
returns or throws. A snapshot made inside that callback may omit the current
exit until it unwinds. Callback exceptions retain their original thrown value
after cleanup; an enter callback exception does not synthesize an exit.

Profiling requires the host's `performance.now()`. If it is unavailable,
enabling a profile returns an `Error`; there is no `Date.now()` fallback.
Clock access or invocation failures preserve the thrown value. Non-finite or
backward clock readings fail explicitly and can leave a partial profile. If a
trace callback and a later clock read both fail, the callback's original
exception takes precedence and that exit may be omitted from the profile.
The implementation uses no DOM types. Profile timing has been measured under
Node 24.19; JavaScript allocations, retained
heap, browser timing and other hosts remain unmeasured.

### Callback Failure, Cancellation and Lifecycle

Batch callbacks may reenter the same parser; they must not access or mutate parser internals. Document callbacks MUST NOT re-enter the same Document. Go callback panics and TypeScript callback exceptions propagate with their original value after runtime cleanup; they must not be converted into syntax errors, invalid bytecode, or stack-overflow errors. This design does not add an error-returning callback, cancellation token, or asynchronous parse. A callback can deliberately abort by panicking/throwing. Event sequences can end without exit events when an action, depth limit, callback, or other runtime failure aborts parsing; this must be documented just as in design 014.

Each parse owns trace stack, counters, event filter and clock state. Generated shared rule tables remain immutable. Pooled Go parser state, if used, must clear the observer and all profile state before reuse, including after panic; observer state must never leak between invocations. TypeScript state belongs to its parser instance and is released on return or throw.

### Compatibility and Implementation Sequence

Observability types are emitted only with the generation option. Existing generated source and no-option APIs retain their source behavior; observability-enabled output is a distinct generated source form, with no `.pegoc` change. Tracing covers Go Node, typed Go, and Document calls, including recognition and typed direct/conversion paths, plus TypeScript Node and typed-result calls. Profiling covers Go Node, typed batch, and Document calls, plus TypeScript Node and typed-result batch calls. Go and TypeScript generated parsers keep their current position-unit defaults and result/error contracts.

1. **Implemented:** generation-time opt-in and event sinks for standalone Go Node and typed Go batch parsing, covering actual rule calls, memo results, typed direct/conversion paths, recognition and representable first-character skips.
2. **Implemented:** TypeScript batch events and typed-result tracing preserve exception identity and per-invocation isolation.
3. **Implemented for generated Go Node and typed batch parsers; implemented for TypeScript batch parsers:** aggregate per-rule profiles over completed calls, with owned sorted snapshots. Focused synthetic Go and TypeScript profile costs are measured below; broader workload costs remain unmeasured.
4. Resolve whether the runtime detects and rejects same-Document callback re-entry; callers must not re-enter a Document from its callback and can use another Document or schedule later work.

## Alternatives Considered

- **Add a nil-checked observer to every generated parser by default.** It gives one API regardless of generation options, but adds state and branches to all generated parsers. No measurement establishes that this overhead is acceptable; defer until it is measured.
- **Instrument only generic `call`.** This misses inlined typed/direct rules and first-character skips, so call/evaluation counts would be incomplete. Instrument generated call sites and runtime paths together.
- **Disable dispatch and direct-call optimizations whenever tracing is enabled.** This can produce a simpler stream but changes the executed strategy and hides the performance shape users need to inspect. Keep the generated strategy and report skips explicitly.
- **Emit a normal failed call for a first-character dispatch skip.** No call body ran and no memo result exists. Use a distinct unpaired `TraceSkipped` event.
- **Promise the engine's complete event stream.** Generated optimizations omit candidate calls, while engine backends already differ in such calls. Document generated-runtime observations and keep engine trace equivalence separate.
- **Generation-time opt-in.** This adds a generation flag and source variant, but lets ordinary generated code omit tracing machinery. Recommend it until default-path measurements support another choice.

## Testing

Generate fixtures for Go node, typed direct, typed conversion, recognition, TypeScript node and TypeScript typed output. For each, compare parsing values, syntax errors, recovered errors, positions and action behavior with instrumentation disabled and enabled in both position units. Assert exact small event streams for ordinary calls, memo hits, deferred memoization, left-recursion growth, failed calls, lookahead, recognition, typed dispatch, filtered nesting and each supported skipped-call reason.

Verify every emitted enter has one exit on success; verify abort cases may end with open calls and never emit fabricated exits. Test profile counts against event-derived counts where available, deterministic serialization, unknown filter names, depth boundaries, unavailable metrics and callback timing. Test callback panic/throw identity, panic after nested calls, parse-pool reuse after panic, callback reentrancy, concurrent Go parses sharing one parser, TypeScript reentrant calls, and that no observer leaks to a later no-trace parse. Test generation with and without the option, stable output, Go compilation/standalone module use and strict TypeScript checking. The Go Node, typed and Document slices have focused fixture, ordinary-reference consumer and race coverage for tracing and profiling across direct and Node-conversion routes. The TypeScript slice has focused Node and typed-result fixtures and ordinary-output parity checks for tracing and profiling. The same-Document callback re-entry enforcement choice remains unresolved.

## Performance and Results

### Node Batch Measurements

The Go Node focused measurements use Go 1.27.1 on an Apple M3 Max (darwin/arm64), `GOMAXPROCS=1`, `GOGC=100`, and no `GOMEMLIMIT`, with three alternating 200 ms samples. For JSON (64 objects), XML (64 items), and calculator (32 repetitions), generation with observability disabled produced source byte-identical to baseline commit `64e4b93`. Runtime ratios below compare ordinary and observed packages generated by the same candidate generator; each row reports its position unit.

| Fixture | Parse, no callback | Recognize, no callback | Parse, full callback | Parse, root-depth filter | Parse, unknown-name filter |
|:--|--:|--:|--:|--:|--:|
| JSON, CodePoints | 1.055× | 1.200× | 2.286× | 1.624× | 1.805× |
| JSON, Bytes | 1.046× | 1.130× | 2.207× | 1.560× | 1.751× |
| XML, CodePoints | 1.037× | 1.088× | 2.168× | 1.570× | 1.680× |
| XML, Bytes | 1.038× | 1.057× | 2.110× | 1.537× | 1.659× |
| Calculator, CodePoints | 1.016× | 1.018× | 1.535× | 1.266× | 1.305× |
| Calculator, Bytes | 1.009× | 1.016× | 1.504× | 1.257× | 1.302× |

The no-callback cases kept allocation counts unchanged and added 48 B/op.
Full and root-filter callbacks added 4–6 allocations and 896–4,017 B/op;
the unknown-name filter added two allocations and 256 B/op beyond full tracing
while delivering no events. These short synthetic fixtures do not represent
logging callbacks or establish workload-wide costs.

### Typed Batch Measurements

Typed Go tracing was measured separately against ordinary typed packages from
the same generator, with direct typed execution and forced Node-conversion
fallback across `ParseAST` and `ParseRuleAST`. Three alternating 200 ms pairs
used the same synthetic JSON (64 objects), XML (64 items) and calculator
(32 repetitions) inputs in both position units. The table gives
observed-to-ordinary median ratio ranges. No-callback ranges span
`ParseAST`/`ParseRuleAST` and both units; callback ranges cover `ParseAST` in
both units. Individual paired ratios were noisy (0.934–1.171 across
no-callback profiles), so these data do not support a significance claim.

| Fixture / path | No callback | Root-only callback | Full scalar-store callback |
|:--|--:|--:|--:|
| Calculator / direct | 1.001–1.008× | 1.316–1.336× | 1.617–1.674× |
| Calculator / conversion | 0.995–1.008× | 1.169–1.179× | 1.349–1.352× |
| JSON / direct | 1.052–1.075× | 1.964–2.117× | 2.958–3.164× |
| JSON / conversion | 1.030–1.042× | 1.389–1.430× | 1.825–1.886× |
| XML / direct | 1.021–1.062× | 1.729–1.791× | 2.418–2.601× |
| XML / conversion | 1.014–1.040× | 1.395–1.438× | 1.818–1.835× |

No-callback cases added about 48 B/op with allocation counts unchanged.
Callbacks stored scalar fields rather than logging. Relative to ordinary typed
parsing, they added about 944 B and 4 allocations for calculator, 4,016 B and
6 allocations for JSON, and 4,016–4,017 B and 6 allocations for XML. Ordinary
generated output remained byte-identical to baseline `bc0e13a`; opt-in source
grew 5.02%/4.39% for direct/conversion calculator, 9.14%/7.12% for JSON, and
10.04%/8.00% for XML. Broad workload, binary-size, retained-heap and generation
costs were not measured for this typed extension. The source is
`internal/gen/gogen/gogen_typed_observe_bench_test.go`; exported temporary fixtures
and raw measurement logs are not repository artifacts.

### Generated Go Profile Measurements

Profile costs were measured with Go 1.27.1 on an Apple M3 Max (darwin/arm64),
`GOMAXPROCS=16` and the default garbage collector, using three serial
alternating 150 ms pairs on the same JSON (64 objects), XML (64 items) and
calculator (32 repetitions) fixtures in both position units. Node sources
timed `Parse` and `Recognize`; typed sources timed `Parse`, `Recognize` and
`ParseAST`. Typed Node-conversion fallback was tested but not timed. Ordinary
output from all six Node/typed fixtures remained byte-identical to baseline
`6592f45`.

Without `WithProfile`, the current observed generator compared with the prior
trace-only generator had per-profile paired median time ratios of 0.961–1.038
for Node and 0.980–1.032 for typed parsers. Paired ranges were 0.941–1.113 and
0.906–1.077, respectively; these noisy samples do not support a significance
claim. Allocation counts were unchanged, with paired B/op differences from
−40 to +44.

For a separate trace-only comparison, a full scalar-store `WithTrace` callback
was measured on Node `Parse` against the prior trace-enabled generator. Paired
median time ratios were 1.026–1.114 for Node-generated packages and
1.011–1.160 for typed-generated packages using their Node `Parse` entry point;
individual-pair ranges were 1.022–1.160 and 0.969–1.161. Allocation counts
were unchanged and paired B/op increased by 9–25. These are six focused
fixtures/routes in both units, not a statistical or workload-wide claim, and
they do not measure typed `ParseAST` tracing.

With `WithProfile`, the table compares the profiled parser against the current
ordinary package (no `WithTrace` callback). This measures the opt-in profile
path, not the cost of an individual counter or clock operation.

| API | Profiled / ordinary time ratio |
|:--|--:|
| Node `Parse` | 3.879–9.731× |
| Node `Recognize` | 5.302–26.860× |
| Typed `Parse` | 3.821–10.212× |
| Typed `Recognize` | 5.397–25.639× |
| Typed `ParseAST` | 4.117–14.432× |

Across all profiled paths and fixtures, the measured additions ranged from
44,990 to 395,085 B/op and 25 to 50 allocations/op; these ranges are not
per-API estimates. Each benchmark reused one `Profile` per case and excluded
`Snapshot`. Generated source grew versus the prior trace-enabled generator by
0.870–4.530% for Node and 0.545–3.018% for typed output, depending on fixture.
Binary size, retained heap and generation time were not measured. The benchmark
source is `internal/gen/gogen/gogen_profile_bench_test.go`; exported fixtures and
raw results are not repository artifacts.

To reproduce the candidate measurements, export the fixtures and run each
ordinary/observed package in a separate process, alternating their order for
each fixture and unit:

```sh
PEGO_PROFILE_BENCH_DIR=/tmp/pego-profile-bench go test ./internal/gen/gogen -run '^TestExportGeneratedGoProfileBenchmarks$' -count=1
cd /tmp/pego-profile-bench/json/node/ordinary || exit 1
go test -run '^$' -bench '^(Benchmark(Parse|ParseAST|Recognize|TraceParse)|BenchmarkProfile(Parse|Recognize|ParseAST))$' -benchmem -benchtime=150ms
# Repeat from json/node/observed and the typed routes, then alternate order for xml and calc.
```

The comparison with the prior trace-enabled generator used the same exporter
with `gen.go`, `gen_observe.go`, `gen_types.go` and `runtime/trace.go` under
`internal/engine/` overlaid from `6592f45`, and `PEGO_PROFILE_BASE=1` to omit
profile-specific benchmark references.

### Node Generation Cost

Node generation cost was measured on already-parsed grammar ASTs; it includes
compile, generate and format work but excludes grammar-source parsing. Three alternating
200 ms samples gave median observed/ordinary time ratios of 1.081× (JSON),
1.038× (XML) and 1.086× (calculator). Generated source sizes grew from 257,998
to 279,914 B (JSON), 698,882 to 763,573 B (XML), and 139,068 to 146,639 B
(calculator). This is a focused candidate comparison, not a workload-wide
performance estimate. Export the generated runtime fixtures, then run each
package benchmark in separate processes, alternating ordinary and observed
order across repeated samples:

```sh
PEGO_TRACE_BENCH_DIR=/tmp/pego-trace-bench go test ./internal/gen/gogen -run '^TestExportGeneratedTraceBenchmarks$' -count=1
cd /tmp/pego-trace-bench/json/ordinary || exit 1
go test -run '^$' -bench '^(BenchmarkParse|BenchmarkRecognize|BenchmarkTrace)$' -benchmem -benchtime=200ms
# Repeat from json/observed, then alternate order for xml and calc.
```

From the repository root, measure generation separately. For paired samples,
filter the ordinary and observed subbenchmarks into separate processes and
alternate their order:

```sh
go test ./internal/gen/gogen -run '^$' -bench '^BenchmarkGeneratedObservabilityGeneration$' -benchmem -benchtime=200ms -count=3
```

The benchmark source is `internal/gen/gogen/gogen_observe_bench_test.go`; the
generated fixtures and raw results are not repository artifacts.

### TypeScript Batch Measurements

The TypeScript measurements use Node.js 24.19.0 on an Apple M3 Max
(darwin/arm64), with explicit garbage collection after warmup. Each profile used
three alternating 200 ms pairs and a 100 ms warmup. Inputs were JSON with 64
objects, XML with 64 items and a calculator expression with 32 repetitions.
All timing profiles used modules generated with `-types` and `-recognize`,
with string input in both position units. Ratios compare enabled and ordinary
packages from the same generator and workload. Source-size and default-source
comparisons additionally include Node-only modules. No-callback option-API ranges include `parseWithOptions`,
`parseRuleWithOptions`, `parseASTWithOptions` and `recognizeWithOptions`; the
legacy ranges include positional-unit `parse`, `parseAST` and `recognize`.
Callback ranges cover `parseWithOptions` and `parseASTWithOptions` only.

| Fixture | Options APIs, no callback | Legacy positional APIs, no callback | Full scalar-store callback | Root-only callback | Unknown-name filter |
|:--|--:|--:|--:|--:|--:|
| Calculator | 0.991–1.018× | 1.006–1.030× | 1.093–1.128× | 1.052–1.114× | 1.068–1.136× |
| JSON | 1.006–1.044× | 1.006–1.023× | 1.183–1.257× | 1.158–1.196× | 1.186–1.288× |
| XML | 0.996–1.018× | 1.000–1.011× | 1.139–1.187× | 1.095–1.156× | 1.141–1.188× |

These are ranges across per-profile median enabled/ordinary ratios, not a
workload-wide estimate. The paired no-callback ratios spanned 0.906–1.358,
showing substantial noise; the measurements do not establish a significant
change. Callback bodies stored scalar event fields and did not log. The six
ordinary generated Node/typed sources were byte-identical to baseline
`005ae81` with observability disabled. Generated source grew by 5.286%/5.029% for
calculator Node/typed, 7.455%/7.147% for JSON and 8.541%/8.187% for XML.
Allocations, retained memory, binary size and generation time were not measured
for this TypeScript slice. Benchmark sources are
`internal/engine/tsgen_observe_bench_test.go` and the generated
`bench.mjs`; raw results and generated fixtures are not repository artifacts.
Export the controls, then alternate ordinary and observed processes across
three pairs for each fixture:

```sh
PEGO_TS_TRACE_BENCH_DIR=/tmp/pego-ts-trace go test ./internal/engine -run '^TestExportGeneratedTSTraceBenchmarks$' -count=1
cd /tmp/pego-ts-trace/json/typed/ordinary || exit 1
node --expose-gc bench.mjs
# Repeat from json/typed/observed, then alternate order for xml and calc.
```

Each process warms every profile for 100 ms and measures it for 200 ms by
default; `PEGO_TRACE_WARM_MS` and `PEGO_TRACE_BENCH_MS` override those durations.

### TypeScript Profile Measurements

Profile timing used Node 24.19.0 on an Apple M3 Max (darwin/arm64), three
serial alternating 150 ms pairs, 75 ms warmup and explicit garbage collection
after warmup. Inputs were JSON with 64 `éclair` records, XML with 64 items and
a calculator expression with 32 repetitions; each fixture used both position
units. Ratios compare profile mode with the separately generated ordinary
package for the same grammar and workload. The table reports ranges across
fixture/route medians, not a workload-wide estimate.

| API | Profile / ordinary time ratio |
|:--|--:|
| Node `Parse` | 1.623–2.718× |
| Node `Recognize` | 1.872–4.016× |
| Typed-generated Node `Parse` | 1.742–3.193× |
| Typed `Recognize` | 1.866–4.021× |
| Typed `ParseAST` | 1.581–2.760× |

Individual paired ratios ranged from 1.492–4.313×. One `Profile` was reused
per case and `snapshot()` was outside the timed region. The runs do not measure
JavaScript allocations, retained heap, binary size, browser timing or Deno/Bun
behavior. No-profile timing was near parity with the parent observed generator
at `871e5f7`: medians were 0.965–1.020× for no-trace calls and 0.973–1.091× for trace-only
calls. Paired ranges were wide and do not support a significance claim. All
six ordinary generated sources were byte-identical to parent `871e5f7`. Source
size increased by 1.641–5.414% for Node and 1.577–5.159% for typed packages
versus the parent observed generator at `871e5f7`.

Export the ordinary and observed packages and profile driver, then run each
package in a separate Node process, alternating ordinary and observed order
for three pairs:

```sh
PEGO_TS_TRACE_BENCH_DIR=/tmp/pego-ts-profile go test ./internal/engine -run '^TestExportGeneratedTSTraceBenchmarks$' -count=1
cd /tmp/pego-ts-profile/json/node/ordinary || exit 1
node --expose-gc profile-bench.mjs
cd /tmp/pego-ts-profile/json/node/observed || exit 1
PEGO_PROFILE_MODES=none,trace,profile node --expose-gc profile-bench.mjs
```

Repeat for typed JSON, XML and calculator fixtures. The driver defaults to
75 ms warmup and 150 ms timing; `PEGO_PROFILE_WARM_MS` and
`PEGO_PROFILE_BENCH_MS` override these durations. The exporter is
`TestExportGeneratedTSTraceBenchmarks`; the benchmark source is committed in `internal/engine/tsgen_profile_bench_test.go`. Exported
sources and raw results are not repository artifacts.

## Limitations and Open Questions

- Exact `Examined` and failed-work metrics may not be available on every generated direct path. The event must expose only what the implementation can account for, with explicit availability metadata or omission.
- TypeScript profiling requires `performance.now()` and has no wall-clock fallback. Timing has been measured under Node 24.19; availability and behavior across other supported Node/browser targets still need validation.
- **Instrumentation availability:** Go and TypeScript use generation-time opt-in; always-emitted, nil-disabled instrumentation would require separate code-size and parse-time evidence.
- **Proposed skip visibility:** use explicit skip events only for call sites where code generation can identify the exact target and dispatch reason. A complete synthetic-call stream is an alternative, but would redefine call counts and risk presenting events as executed calls.
- Cross-engine trace differences and `Document` resume/profile omissions remain independent limitations; this record does not resolve them.
