# 043. Generated Parser Tracing and Profiling

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Add opt-in rule-call tracing and per-rule profiling to standalone generated Go, typed Go and TypeScript parsers. Instrument generated direct-call paths as well as runtime-dispatched calls, and report optimized skips as skips rather than pretending they were calls. Generated traces are observations of each generated parser's execution strategy; they do not promise event-for-event equivalence with engine traces or between backends.

## Motivation

Design 014 provides `pego.WithTrace`, `pego.Profile`, `pego.WithProfile`, and the `trace`, `profile` and `explain` CLI commands for the engine. Generated Go Node and typed batch parsing now have separate generation-time opt-in tracing; it does not provide profiles or the engine event contract. The engine event reports rule, position, binding level, depth, match, memo use, body evaluations and examined input; callbacks are synchronous, and their panic is re-raised unchanged. Profiles accumulate per-rule call/evaluation/memo/match/failure counts, consumed and wasted positions, repeats and inclusive/self time.

Generated Go and TypeScript runtimes instead specialize calls into direct generated methods/functions and common call sites bypass the generic `call` path (`internal/engine/gen.go`, `gen_direct.go`, `gen_ts.go`, `genrt/runtime.go`, `tsrt/runtime.ts`). Generated Go has ordinary and typed runtimes; TypeScript has its own runtime and can convert node results to typed values. Literal/first-character dispatch can skip a call, and deferred memoization changes which calls evaluate a body. Adding a hook only to `parser.call` would therefore miss optimized calls and produce incomplete counts. The initial slice has focused synthetic measurements below; broader workload and TypeScript measurements remain unavailable.

### Go Node Batch Trace

The initial implementation is generation-time opt-in tracing for standalone
generated Go Node batch parsers. Generation uses
`pego.WithGeneratedObservability()` or `pego gen -observability`; enabled
parsers trace `Parse`, `ParseRule` and `Recognize`. Events cover actual plain,
direct, general, memoized and left-recursive calls, plus first-character
dispatch skips only where the generated call site can identify the target.
`ExaminedKnown` is always false. Named filters include descendants; unknown
names select nothing. The start rule has depth one, and nonpositive maximum
depth is unlimited. Callback panics propagate unchanged after cleanup, while
aborted parses may omit exits. This slice does not provide profiling.
TypeScript and Node or typed `Document` combinations are rejected during
generation. Typed Go batch tracing is described below; TypeScript, Document
and profiling remain proposed.

### Typed Go Batch Trace Extension

With `WithGeneratedObservability()` and `WithTypes()`, generated Go packages
trace `ParseAST` and `ParseRuleAST` as well as Node `Parse`, `ParseRule` and
optional `Recognize`. This covers direct typed execution and Node-conversion
fallback. Events describe parser calls made by the selected route; converting
a Node result to typed values is not a rule call and emits no separate event.
The same event, filter, depth, interruption and callback-panic contract
applies, though Node and typed traces can differ because their execution paths
differ. This extension does not add `Profile`; Document and TypeScript
combinations remain unsupported.

## Goals

- Expose explicit, opt-in trace/profile support in generated Go, generated typed Go and TypeScript parsers.
- Observe direct, generic, value-free, typed and recognition calls with one documented event contract per generated backend.
- Distinguish entered calls, memo hits, left-recursive body evaluations, and calls skipped by generated dispatch.
- Define position units, filtering, callback panic/throw, reentrancy, concurrency, interruption and profile ownership.
- Preserve parsing results and errors with instrumentation enabled; measure generation size and default/opt-in runtime costs before choosing how commonly instrumentation is available.
- State where generated traces differ from engine traces and leave existing engine dispatch and Document-resumption observability gaps as separate work.

## Non-Goals

- Making generated parsers event-equivalent to engine backends or claiming portable timing equivalence between Go and JavaScript.
- Adding Document or stream tracing to generated parsers. Generated Go Document APIs exist, but their tracing contract is separate; generated stream parsers are not available.
- Changing memoization, first-character dispatch, rule evaluation order, parse results, or serialized `.pegoc` format.
- Providing expression-level tracing or tracing action internals.

## Design

### Generation-Time Opt-In and Public API

Generation-time opt-in keeps ordinary generated output free of observability fields, callbacks, clock reads, or trace branches. The implementation supports `GenerateGo` for standalone Go Node and typed batch parsers and the `pego gen -observability` CLI flag. Extending it to TypeScript and adding profiling remains proposed.

Proposed complete generated Go API, including extensions beyond the initial implementation:

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

The Go Node slice supports `Parse`, `ParseRule` and generated `Recognize`; the typed slice also supports `ParseAST` and `ParseRuleAST`. Both export `TraceEvent`, `WithTrace`, `WithTraceFilter` and `WithTraceMaxDepth`; neither exports `WithProfile`. The proposed API below adds profiling and TypeScript entry points.

The option callback receives a copied value event containing scalar/string fields, not a pointer into parser state. Callbacks run synchronously on the parsing goroutine/JavaScript stack. There is no retained input or parser-state accessor in this API. Per-invocation trace state is private; concurrent or reentrant parses of the same generated parser have independent state. A `Profile` aggregates across calls to that object and is not safe to share across concurrent parses.

### Event Semantics

- `TraceEnter` precedes the generated call's memo lookup or body dispatch. Every enter has one matching `TraceExit` unless parsing is aborted. `Depth` starts at one for the start rule; `Level` is the Operators Rule binding level, zero for calls without one.
- `TraceExit` describes one entered call. `Memo=true` means the call reused an existing memo result without evaluating its body, so `Evals=0`. Calls that evaluate left-recursion growth attempts have `Memo=false`; their `Evals` counts their own body attempts, including growth attempts, not nested calls. Recursive calls that reuse the growing seed have `Memo=true` and `Evals=0`. `Matched` and `End` describe the rule call, not whether the whole-input parse passed its end-of-input check. `Examined` is emitted only where the generated runtime can account for a truthful range; it must not be guessed from `End` when a lookahead or failed dispatch examined farther.
- `TraceSkipped` is an unpaired event for a generated call site whose existing dispatch condition proves that the target rule was not entered. It identifies the target rule, start position, depth, level and skip reason, and has no exit, result, memo or evaluation count. It is not included in entered-call or body-evaluation totals. Only instrumented generated dispatch sites that can name the skipped target emit it. An implementation that cannot recover a truthful target and position omits the skip event rather than fabricating one.
- A deferred-memo first evaluation produces an exit without a memo hit; a later lookup reports the memo event. Recognition events report recognition work without promising all actions are skipped: predicate-dependent actions and values still execute when required, as in ordinary recognition.
- Typed direct Go and Node-conversion Go report the parser's rule execution. Typed conversion time and allocations are outside per-rule event time unless a future API explicitly adds a conversion event. TypeScript typed-result conversion has the same boundary.

Generated backends can skip different candidate calls due to first-character dispatch and other generated specialization. Their event streams must be labeled as generated-runtime traces and can differ in which `TraceSkipped` events are representable. They must never be described as equivalent to engine traces. First-character trace differences between engine backends remain a separate limitation. Generated Go Node and typed `Document` APIs exist, but tracing explicitly rejects Document generation. Document parse/resume events need a separate contract before they can be traced.

### Filtering and Profiling

`WithTraceFilter` / `traceRules` select rule names, and `WithTraceMaxDepth` / `traceMaxDepth` limit delivered events by depth. Filtering suppresses callback delivery only; it must not change parsing, dispatch or parent-depth numbering. Include calls nested within a selected rule, following engine `pego trace -rule` behavior. JSON/text formatting can be supplied as small generated-independent helpers or by adapting events in user code; generated parsers do not add CLI commands.

`Profile` aggregates calls, entered calls, skipped calls by reason, body evaluations, memo hits, matches, failures, consumed positions, examined positions on failed evaluations, repeats at a `(rule, position, level)`, and inclusive/self time. Match the engine definitions where the data is available; explicitly mark fields unavailable rather than substituting estimates. In particular, a generated backend that cannot expose the engine's isolated farthest-expectation record must not claim engine-equivalent wasted-input accounting. Provide deterministic rule ordering in snapshots/JSON. Go durations are nanoseconds; TypeScript durations are milliseconds from the host monotonic clock. Times include instrumentation and are useful within one profile only.

### Callback Failure, Cancellation and Lifecycle

Callbacks may reenter the same parser; they must not access or mutate parser internals. Go callback panics and TypeScript callback exceptions propagate with their original value after runtime cleanup; they must not be converted into syntax errors, invalid bytecode, or stack-overflow errors. This design does not add an error-returning callback, cancellation token, or asynchronous parse. A callback can deliberately abort by panicking/throwing. Event sequences can end without exit events when an action, depth limit, callback, or other runtime failure aborts parsing; this must be documented just as in design 014.

Each parse owns trace stack, counters, event filter and clock state. Generated shared rule tables remain immutable. Pooled Go parser state, if used, must clear the observer and all profile state before reuse, including after panic; observer state must never leak between invocations. TypeScript state belongs to its parser instance and is released on return or throw.

### Compatibility and Implementation Sequence

Observability types are emitted only with the generation option. Existing generated source and no-option APIs retain their source behavior; observability-enabled output is a distinct generated source form, with no `.pegoc` change. Implemented batch slices cover Go Node and typed Go calls, including recognition and typed direct/conversion paths. TypeScript Node/typed paths and Document tracing remain proposed. Go and TypeScript generated parsers keep their current position-unit defaults and result/error contracts.

1. **Implemented:** generation-time opt-in and event sinks for standalone Go Node and typed Go batch parsing, covering actual rule calls, memo results, typed direct/conversion paths, recognition and representable first-character skips.
2. Add the corresponding TypeScript generator/runtime events and typed wrappers, preserving exception identity and reentrant/concurrent invocation isolation.
3. Add profiles only after event completeness is checked against instrumented generated call sites. Measure profiling separately before deciding whether runtime opt-in can replace generation-time opt-in.
4. Keep generator/CLI guides aligned with supported combinations and validate generated output freshness and standalone consumer compatibility.

## Alternatives Considered

- **Add a nil-checked observer to every generated parser by default.** It gives one API regardless of generation options, but adds state and branches to all generated parsers. No measurement establishes that this overhead is acceptable; defer until it is measured.
- **Instrument only generic `call`.** This misses inlined typed/direct rules and first-character skips, so call/evaluation counts would be incomplete. Instrument generated call sites and runtime paths together.
- **Disable dispatch and direct-call optimizations whenever tracing is enabled.** This can produce a simpler stream but changes the executed strategy and hides the performance shape users need to inspect. Keep the generated strategy and report skips explicitly.
- **Emit a normal failed call for a first-character dispatch skip.** No call body ran and no memo result exists. Use a distinct unpaired `TraceSkipped` event.
- **Promise the engine's complete event stream.** Generated optimizations omit candidate calls, while engine backends already differ in such calls. Document generated-runtime observations and keep engine trace equivalence separate.
- **Generation-time opt-in.** This adds a generation flag and source variant, but lets ordinary generated code omit tracing machinery. Recommend it until default-path measurements support another choice.

## Testing

Generate fixtures for Go node, typed direct, typed conversion, recognition, TypeScript node and TypeScript typed output. For each, compare parsing values, syntax errors, recovered errors, positions and action behavior with instrumentation disabled and enabled in both position units. Assert exact small event streams for ordinary calls, memo hits, deferred memoization, left-recursion growth, failed calls, lookahead, recognition, typed dispatch, filtered nesting and each supported skipped-call reason.

Verify every emitted enter has one exit on success; verify abort cases may end with open calls and never emit fabricated exits. Test profile counts against event-derived counts where available, deterministic serialization, unknown filter names, depth boundaries and unavailable metrics. Test callback panic/throw identity, panic after nested calls, parse-pool reuse after panic, callback reentrancy, concurrent Go parses sharing one parser, TypeScript reentrant calls, and that no observer leaks to a later no-trace parse. Test generation with and without the option, stable output, Go compilation/standalone module use and strict TypeScript checking. The Go Node and typed slices have focused fixture, ordinary-reference consumer and race coverage for direct and Node-conversion routes. TypeScript and Document tests remain proposed.

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
`internal/engine/gen_typed_observe_bench_test.go`; exported temporary fixtures
and raw measurement logs are not repository artifacts.

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
PEGO_TRACE_BENCH_DIR=/tmp/pego-trace-bench go test ./internal/engine -run '^TestExportGeneratedTraceBenchmarks$' -count=1
cd /tmp/pego-trace-bench/json/ordinary || exit 1
go test -run '^$' -bench '^(BenchmarkParse|BenchmarkRecognize|BenchmarkTrace)$' -benchmem -benchtime=200ms
# Repeat from json/observed, then alternate order for xml and calc.
```

From the repository root, measure generation separately. For paired samples,
filter the ordinary and observed subbenchmarks into separate processes and
alternate their order:

```sh
go test ./internal/engine -run '^$' -bench '^BenchmarkGeneratedObservabilityGeneration$' -benchmem -benchtime=200ms -count=3
```

The benchmark source is `internal/engine/gen_observe_bench_test.go`; the
generated fixtures and raw results are not repository artifacts.

## Limitations and Open Questions

- Exact `Examined` and failed-work metrics may not be available on every generated direct path. The event must expose only what the implementation can account for, with explicit availability metadata or omission.
- The smallest useful TypeScript profiling clock and its compatibility across supported Node/browser targets need verification.
- **Instrumentation availability:** Go uses generation-time opt-in. A future TypeScript implementation should also begin with generation-time opt-in; always-emitted, nil-disabled instrumentation would require separate code-size and parse-time evidence.
- **Proposed skip visibility:** use explicit skip events only for call sites where code generation can identify the exact target and dispatch reason. A complete synthetic-call stream is an alternative, but would redefine call counts and risk presenting events as executed calls.
- Cross-engine trace differences and `Document` resume/profile omissions remain independent limitations; this record does not resolve them.
