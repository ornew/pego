# 043. Generated Parser Tracing and Profiling

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Add opt-in rule-call tracing and per-rule profiling to standalone generated Go, typed Go and TypeScript parsers. Instrument generated direct-call paths as well as runtime-dispatched calls, and report optimized skips as skips rather than pretending they were calls. Generated traces are observations of each generated parser's execution strategy; they do not promise event-for-event equivalence with engine traces or between backends.

## Motivation

Design 014 provides `pego.WithTrace`, `pego.Profile`, `pego.WithProfile`, and the `trace`, `profile` and `explain` CLI commands for the engine. It explicitly says generated Go parsers lack tracing. The engine event reports rule, position, binding level, depth, match, memo use, body evaluations and examined input; callbacks are synchronous, and their panic is re-raised unchanged. Profiles accumulate per-rule call/evaluation/memo/match/failure counts, consumed and wasted positions, repeats and inclusive/self time.

Generated Go and TypeScript runtimes instead specialize calls into direct generated methods/functions and common call sites bypass the generic `call` path (`internal/engine/gen.go`, `gen_direct.go`, `gen_ts.go`, `genrt/runtime.go`, `tsrt/runtime.ts`). Generated Go has ordinary and typed runtimes; TypeScript has its own runtime and can convert node results to typed values. Literal/first-character dispatch can skip a call, and deferred memoization changes which calls evaluate a body. Adding a hook only to `parser.call` would therefore miss optimized calls and produce incomplete counts. No generated-parser timing or default-overhead measurement is available for this proposal.

## Goals

- Expose explicit, opt-in trace/profile support in generated Go, generated typed Go and TypeScript parsers.
- Observe direct, generic, value-free, typed and recognition calls with one documented event contract per generated backend.
- Distinguish entered calls, memo hits, left-recursive body evaluations, and calls skipped by generated dispatch.
- Define position units, filtering, callback panic/throw, reentrancy, concurrency, interruption and profile ownership.
- Preserve parsing results and errors with instrumentation enabled; measure generation size and default/opt-in runtime costs before choosing how commonly instrumentation is available.
- State where generated traces differ from engine traces and leave existing engine dispatch and Document-resumption observability gaps as separate work.

## Non-goals

- Making generated parsers event-equivalent to engine backends or claiming portable timing equivalence between Go and JavaScript.
- Adding Document or stream tracing to generated parsers; those generated capabilities are not present today.
- Changing memoization, first-character dispatch, rule evaluation order, parse results, or serialized `.pegoc` format.
- Providing expression-level tracing or tracing action internals.

## Design

### Generation-time opt-in and public API

Recommend generation-time opt-in so an ordinary generated parser has no observability fields, callbacks, clock reads, or trace branches. Add `pego.WithGeneratedObservability()` as a `GenOption`, accepted by `GenerateGo` and `GenerateTypeScript`; add `pego gen -observability` for each supported generation language. When enabled, both Go forms emit the observability types/options, and TypeScript emits the corresponding options and profile helpers. When disabled, generated source and the current parse hot path retain their existing shape. This generation flag is proposed and has no current implementation.

Generated Go API, available only in a parser generated with observability enabled:

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
    Memo bool          // exit only; result reused without a body evaluation
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

The generated parser's existing `ParseOption` remains intact; observability options compose with unit/depth options, including typed Go entry points such as `ParseAST` and `ParseRuleAST`. Keep no-option entry points unchanged. TypeScript adds `trace?: (event: TraceEvent) => void`, `traceRules?: string[]`, `traceMaxDepth?: number` and `profile?: Profile` to the existing `ParseOptions`, used by `parseWithOptions`, `parseRuleWithOptions`, `recognizeWithOptions` and typed-AST options-aware entry points. TypeScript's events use numeric positions in the selected `Unit`: code points by default and UTF-8 bytes in byte mode. Go uses the selected `Unit` in the same way. Positions are half-open; `Examined` is an exclusive end.

The option callback receives a copied value event containing scalar/string fields, not a pointer into parser state. Callbacks run synchronously on the parsing goroutine/JavaScript stack. There is no retained input or parser-state accessor in this API. Per-invocation trace state is private; concurrent or reentrant parses of the same generated parser have independent state. A `Profile` aggregates across calls to that object and is not safe to share across concurrent parses.

### Event semantics

- `TraceEnter` precedes the generated call's memo lookup or body dispatch. Every enter has one matching `TraceExit` unless parsing is aborted. `Depth` starts at one for the start rule; `Level` is the Pratt binding level, zero for calls without one.
- `TraceExit` describes one entered call. `Memo` is true only when the result was reused without evaluating that call's body. `Evals` counts body evaluations owned by the call, including each left-recursion growth attempt, not nested calls. `Matched` and `End` describe the final result. `Examined` is emitted only where the generated runtime can account for a truthful range; it must not be guessed from `End` when a lookahead or failed dispatch examined farther.
- `TraceSkipped` is an unpaired event for a generated call site whose existing dispatch condition proves that the target rule was not entered. It identifies the target rule, start position, depth, level and skip reason, and has no exit, result, memo or evaluation count. It is not included in entered-call or body-evaluation totals. Only instrumented generated dispatch sites that can name the skipped target emit it. An implementation that cannot recover a truthful target and position omits the skip event rather than fabricating one.
- Memo hits still produce enter and exit, with `Memo=true` and `Evals=0`. A deferred-memo first evaluation produces an ordinary non-memo exit; a later reused result produces the memo event. Recognition events report recognition work without promising all actions are skipped: predicate-dependent actions and values still execute when required, as in ordinary recognition.
- Typed direct Go and Node-conversion Go report the parser's rule execution. Typed conversion time and allocations are outside per-rule event time unless a future API explicitly adds a conversion event. TypeScript typed-result conversion has the same boundary.

Generated backends can skip different candidate calls due to first-character dispatch and other generated specialization. Their event streams must be labeled as generated-runtime traces and can differ in which `TraceSkipped` events are representable. They must never be described as equivalent to engine traces. First-character trace differences between engine backends remain a separate limitation. Generated parsers do not support `Document`, so the existing document-resume omission does not apply to these APIs. If a future generated Document feature is designed, it needs an explicit resume event contract before tracing it.

### Filtering and profiling

`WithTraceFilter` / `traceRules` select rule names, and `WithTraceMaxDepth` / `traceMaxDepth` limit delivered events by depth. Filtering suppresses callback delivery only; it must not change parsing, dispatch or parent-depth numbering. Include calls nested within a selected rule, following engine `pego trace -rule` behavior. JSON/text formatting can be supplied as small generated-independent helpers or by adapting events in user code; generated parsers do not add CLI commands.

`Profile` aggregates calls, entered calls, skipped calls by reason, body evaluations, memo hits, matches, failures, consumed positions, examined positions on failed evaluations, repeats at a `(rule, position, level)`, and inclusive/self time. Match the engine definitions where the data is available; explicitly mark fields unavailable rather than substituting estimates. In particular, a generated backend that cannot expose the engine's isolated farthest-expectation record must not claim engine-equivalent wasted-input accounting. Provide deterministic rule ordering in snapshots/JSON. Go durations are nanoseconds; TypeScript durations are milliseconds from the host monotonic clock. Times include instrumentation and are useful within one profile only.

### Callback failure, cancellation and lifecycle

Callbacks may reenter the same parser; they must not access or mutate parser internals. Go callback panics and TypeScript callback exceptions propagate with their original value after runtime cleanup; they must not be converted into syntax errors, invalid bytecode, or stack-overflow errors. This design does not add an error-returning callback, cancellation token, or asynchronous parse. A callback can deliberately abort by panicking/throwing. Event sequences can end without exit events when an action, depth limit, callback, or other runtime failure aborts parsing; this must be documented just as in design 014.

Each parse owns trace stack, counters, event filter and clock state. Generated shared rule tables remain immutable. Pooled Go parser state, if used, must clear the observer and all profile state before reuse, including after panic; observer state must never leak between invocations. TypeScript state belongs to its parser instance and is released on return or throw.

### Compatibility and implementation sequence

Observability types are emitted only with the generation option. Existing generated source and no-option APIs retain their source behavior; observability-enabled output is a distinct generated source form, with no `.pegoc` change. APIs must work in Go node, typed direct and conversion paths, recognition, and TypeScript node/typed paths. Go and TypeScript generated parsers keep their current position-unit defaults and result/error contracts.

1. Add generation-time opt-in and an event sink to the Go node runtime. Instrument every actual rule entry/exit, memo result and dispatch skip without routing ordinary calls through an always-on observer.
2. Carry the same contract through typed direct/conversion methods and recognition; add ownership/reset and panic-propagation coverage.
3. Add the corresponding TypeScript generator/runtime events and typed wrappers, preserving exception identity and reentrant/concurrent invocation isolation.
4. Add profiles and filter helpers only after event completeness is checked against instrumented generated call sites. Measure ordinary generated output, observability-enabled output with no callback, tracing, filtering and profiling before deciding whether runtime opt-in can replace generation-time opt-in.
5. Update generator/CLI guides and design 014's limitation only when implementation lands. Keep generated output freshness and standalone consumer compatibility in the gate.

## Alternatives considered

- **Add a nil-checked observer to every generated parser by default.** It gives one API regardless of generation options, but adds state and branches to all generated parsers. No measurement establishes that this overhead is acceptable; defer until it is measured.
- **Instrument only generic `call`.** This misses inlined typed/direct rules and first-character skips, so call/evaluation counts would be incomplete. Instrument generated call sites and runtime paths together.
- **Disable dispatch and direct-call optimizations whenever tracing is enabled.** This can produce a simpler stream but changes the executed strategy and hides the performance shape users need to inspect. Keep the generated strategy and report skips explicitly.
- **Emit a normal failed call for a first-character dispatch skip.** No call body ran and no memo result exists. Use a distinct unpaired `TraceSkipped` event.
- **Promise the engine's complete event stream.** Generated optimizations omit candidate calls, while engine backends already differ in such calls. Document generated-runtime observations and keep engine trace equivalence separate.
- **Generation-time opt-in.** This adds a generation flag and source variant, but lets ordinary generated code omit tracing machinery. Recommend it until default-path measurements support another choice.

## Testing

Generate fixtures for Go node, typed direct, typed conversion, recognition, TypeScript node and TypeScript typed output. For each, compare parsing values, syntax errors, recovered errors, positions and action behavior with instrumentation disabled and enabled in both position units. Assert exact small event streams for ordinary calls, memo hits, deferred memoization, left-recursion growth, failed calls, lookahead, recognition, typed dispatch, filtered nesting and each supported skipped-call reason.

Verify every emitted enter has one exit on success; verify abort cases may end with open calls and never emit fabricated exits. Test profile counts against event-derived counts where available, deterministic serialization, unknown filter names, depth boundaries and unavailable metrics. Test callback panic/throw identity, panic after nested calls, parse-pool reuse after panic, callback reentrancy, concurrent Go parses sharing one parser, TypeScript reentrant calls, and that no observer leaks to a later no-trace parse. Test generation with and without the option, stable output, Go compilation/standalone module use and strict TypeScript checking. These are proposed checks; none has been run for this design.

## Performance and results

No measurements are claimed. Use identical grammar, input, generated options and toolchain on the baseline and candidate. Measure generation time and source size separately from parse time. For Go use paired `testing.B` cases on both units for small/large grammars and direct, typed-direct, typed-conversion and recognition paths; report time, allocations and bytes. For TypeScript warm the same runtime and report Node/browser engine, version, iterations, time and memory where available. Compare: ordinary parser generated without observability; parser generated with observability but no callback; callback with no-op filter; filtered tracing; full tracing; profile. Include a no-op action and a reentrant/concurrent control where applicable. Keep raw results outside Git. Do not claim zero or negligible default overhead before measuring it.

## Limitations and open questions

- Exact `Examined` and failed-work metrics may not be available on every generated direct path. The event must expose only what the implementation can account for, with explicit availability metadata or omission.
- The smallest useful TypeScript profiling clock and its compatibility across supported Node/browser targets need verification.
- **Proposed instrumentation availability:** use generation-time opt-in for Go and TypeScript until measurements show that always-emitted, nil-disabled instrumentation is acceptable. The alternative is a single runtime API in every generated parser at a measured code-size and parse-time cost.
- **Proposed skip visibility:** use explicit skip events only for call sites where code generation can identify the exact target and dispatch reason. A complete synthetic-call stream is an alternative, but would redefine call counts and risk presenting events as executed calls.
- Cross-engine trace differences and `Document` resume/profile omissions remain independent limitations; this record does not resolve them.
