# Tracing and Profiling

A parse can report every rule call it makes to a caller-supplied function
(**tracing**) and can accumulate the cost of each rule over many parses
(**profiling**). Observation never changes the result of a parse. This chapter
defines the engine API and the opt-in APIs of generated Go and TypeScript
parsers.

## Engine Tracing

`pego.WithTrace(f)` calls `f` at the start and at the end of every rule call of
the parse, in order, so that the events nest like the calls. With several
`WithTrace` options each function is called in order; a nil `f` is ignored.
Tracing works with every backend, with `Parse`, `ParsePrefix`, recognition,
`ParseStream` and `Document` (an option given to `NewDocument` applies to every
`Document.Parse`). Generated parsers do not provide this API; they have their own
options defined below.

The engine reports the calls that its backend makes. A call that is skipped
because the next character cannot start the rule (first-character dispatch) is
not reported, and the backends do not skip the same calls, so event streams of
different backends MAY differ in the calls that cannot match.

A `TraceEvent` has the following fields.

| Field | Meaning |
|:--|:--|
| `Kind` | `TraceEnter`, reported when a rule is called before the memo is consulted, or `TraceExit`, reported when the call returns |
| `Rule` | The name of the rule that is called |
| `Level` | The binding level the rule was called with, or 0 for a call without a level |
| `Depth` | The nesting depth of the call; the start rule has depth 1 |
| `Pos` | The position at which the call starts, in the position unit |
| `Lookahead` | True if the call is inside a lookahead, where failures record no expectations |

An exit event also has the fields below.

| Field | Meaning |
|:--|:--|
| `Matched` | Whether the rule matched |
| `End` | The position after the match, or `Pos` if the rule failed |
| `Memo` | True if the result came from the memo table without evaluating the body |
| `Evals` | The number of times this call evaluated the rule body: 0 for a memo result, usually 1, and one more for each step that a left-recursive match grows. Bodies of nested calls are not included |
| `ReusedElements` | The number of repetition elements of a `Document` reused during this call, nested calls included. It measures resumption in this invocation, not work from the parse that built a memo result |
| `Examined` | The exclusive end of the input that the call examined; looking at a character to reject it and lookahead count |

The methods `LineCol(pos)`, `Text()`, `Failure()` and `Recovered()` read the
state of the parser and MUST be called only from the trace function, while it
handles the event. `Text` returns the input the call matched. `Failure` returns
the farthest failure recorded during the call as a `*SyntaxError`, or nil.
`Recovered` returns the errors that `#recover` recovered from during the call,
which `Failure` does not include. The fields can be kept after the call. Tracing
does not change the result.

## Engine Profiling

`pego.WithProfile(prof)` adds the cost of a parse to a `pego.Profile`, which
accumulates any number of parses. It traces the parse, so it composes with
`WithTrace`; a nil `prof` is ignored. A `Profile` is not safe for concurrent use.
Times include the cost of measuring them and are comparable with each other
only.

`Profile` has the fields `Parses`, `Calls`, `Evals`, `MemoHits`,
`ReusedElements` (completed root calls only, counted once), `Examined` (the end
of the input that the start rule examined, totaled over parses), `Time` (the time
of the start rule's calls) and `Rules`, a list of `*RuleProfile` in the order of
their first call. `Profile.Hints()` returns text that points at costly rules. A
`RuleProfile` has the fields below.

| Field | Meaning |
|:--|:--|
| `Rule` | The rule name. Calls of a rule's value-free twin count as calls of the rule |
| `Calls`, `MemoHits`, `Matched`, `Failed` | Each call either evaluated the body or took the memoized result, and either matched or failed |
| `Evals` | Body evaluations: one per call not answered by the memo and one more for each growth step of a left-recursive match |
| `ReusedElements` | Repetition elements reused during the calls, nested calls included. A nested call can report the same reuse as its parent, so parent and child rows MUST NOT be summed |
| `Consumed` | The input matched by calls that matched |
| `Wasted` | The input examined by calls that evaluated the body and failed. It is inclusive of the calls they made |
| `LongestFail`, `LongestFailAt` | The most input one failed call examined, and where |
| `Repeats` | The number of calls that evaluated the body at a position and level where an earlier call of the same parse had evaluated it |
| `MaxEvals`, `MaxEvalsAt` | The most evaluating calls at one position and level, and where |
| `Time`, `SelfTime` | The time spent in the calls, nested calls included (a call within a call of the same rule counts once), and excluding nested calls |

In JSON, `Time` and `SelfTime` are in nanoseconds and the key for the reuse
counts is `reusedElements`. A `Location` has `Pos`, `Line` and `Col`; its line is 0
when the position lies in input that a stream parse has discarded.

## Generated Go Batch Tracing

Standalone generated Go Node parsers may be generated with tracing enabled by
`Observability` of `gen.Go` or `pego gen -observability`. This mode
supports batch `Parse` and `ParseRule`; it also supports `Recognize` when that
entry point is generated. With generated types, it additionally supports
`ParseAST` and `ParseRuleAST`, for both direct typed execution and Node-conversion
fallback. Typed events describe rule calls made by the selected route;
conversion itself is not a rule event, so Node and typed event streams may
differ. Typed methods use the same event fields, filter, depth, interruption
and callback-panic behavior as Node batch methods. Generated Go Node and typed
batch parsers also provide profiling through `WithProfile`; TypeScript batch
parsers provide profiling through `Profile` in per-call options.

A parser generated without observability does not observe: passing it the trace or
profile options (`WithTrace`, `WithProfile`, `WithTraceFilter`, `WithTraceMaxDepth`)
returns an error before the parse instead of delivering partial data.

The generated package defines its own `TraceEvent` and trace options. Events
observe actual generated execution, not an event-for-event replay of engine
parser calls. `TraceEnter` begins a rule call before its memo lookup or body
dispatch; `TraceExit` reports the call's result and end position. The start
rule has depth 1. An exit describes the rule call only: whole-input acceptance
can still fail because input remains after the rule returns. `TraceSkipped` is
unpaired and is emitted only for identifiable first-character dispatch skips.
A call aborted by a runtime error or callback panic may have no exit event.

Event positions use the selected position unit. `Memo=true` means the call
reused an existing memo result without evaluating its body, so `Evals` is zero.
Calls that evaluate left-recursion growth attempts have `Memo=false`; their
`Evals` counts their own body attempts, including growth attempts. Recursive
calls that reuse the growing seed have `Memo=true` and `Evals=0`. Generated
batch parses report `ExaminedKnown` as false; consumers MUST NOT treat
`Examined` as a complete examined-input range when it is false. Generated Go
Document parses report a known examined frontier, described below.

Trace callbacks receive copied scalar and string event fields synchronously.
Events do not expose parser state or input text. Callback panics propagate with
their original value after cleanup. `WithTraceFilter` limits callback delivery
to named rules and their descendants; an unknown name matches no rule, and an
empty filter selects every rule. `WithTraceMaxDepth` limits delivered events;
the start rule counts as depth 1, and zero or a negative value means no limit.
Filtering and depth limits do not change parsing. Trace state is local to one
invocation.

### Generated Go Batch Profiling

Generated Go Node and typed batch packages generated with
`Observability` MUST expose `Profile`, `ProfileSnapshot`,
`RuleProfile`, `ProfileLocation`, `SkipReasonCount` and `WithProfile`. The zero
value of `Profile` MUST be ready to use; passing a nil profile disables
collection. A profile sink is not safe for concurrent use. Reentrant parses on
one goroutine MUST keep separate per-invocation accounting. `Snapshot()` MUST
return an owned copy with rules sorted by name and each rule's skip reasons
sorted by reason; callers may marshal the snapshot for deterministic JSON.

`Parses` counts root invocations and `Entered` counts rule calls that began,
including calls interrupted before exit. `Calls`, `Evals`, `MemoHits`, match and
failure counts, consumed positions, and timing include only completed calls.
`Evals` counts body attempts, including left-recursive growth. `Repeats` and
`MaxEvals` count completed non-memo calls with `Evals > 0` at the same
`(rule, position, level)` within an invocation; tied maximum locations retain
the first observed location. `Skipped` and per-rule skip reasons count
identifiable generated dispatch skips independently of trace filters; skips
are not entered calls. Position counts use each invocation's configured unit
and MUST NOT be treated as normalized when a sink accumulates multiple units.

`Examined` and `Wasted` are unavailable for generated batch parses, so a rule
with a completed call outside a Document parse MUST report `ExaminedKnown` and
`WastedKnown` as false, and a rule with no completed call MUST also report them
as false; implementations MUST NOT infer these values from matched ranges.
Generated Go Document profiling defines them below. Root `Time` sums completed root rule calls
and excludes input setup, whole-input end checks and typed-result conversion.
Per-rule `Time` counts recursive calls of the same rule once within each
invocation, while `SelfTime` excludes child time. Both include instrumentation
and synchronous enter/exit callback time. A completed-exit update is published
after the exit callback returns or panics; a snapshot taken inside that
callback may omit its exit until it unwinds. Callback panics MUST propagate
unchanged after cleanup. A panic in an exit callback MUST still count that
completed call; a panic in an enter callback does not synthesize an exit.

## Generated Go Document Tracing and Profiling

When generated Go observability and Document support are both enabled,
`NewDocument` MUST accept `WithTrace` and `WithProfile` as retained defaults.
Construction MUST NOT emit parser events or profile counts. `Document.Parse`
and, with generated types, `Document.ParseAST` MUST accept invocation-local
overrides; an override applies only to that call and MUST NOT replace the
constructor default for later calls. `Edit` does not parse and MUST NOT emit
rule events.

Events MUST report actual rule calls in the current invocation, including Node,
direct typed and Node-conversion fallback calls. Conversion itself MUST NOT
emit a rule event. A memoized whole-call reuse MUST report that call with
`Memo=true` and `Evals=0`; nested events from earlier parses MUST NOT be
replayed. Generated Documents do not resume repetition bodies and MUST NOT
report a `ReusedElements` count.

Every exit event of a Document parse MUST report `ExaminedKnown=true`.
`Examined` is then the exclusive end of the input the Document treats the call
as having examined: the frontier against which a later edit decides whether
the call's memo result is reused. A call that records a memo result MUST report
the frontier stored with that result. A memo hit reports the stored frontier of
the result it reuses, but at least one position past its start. The frontier is conservative: it is at least one position
past the call's start and its end where the call records or reuses a memo
result, so it can exceed the input actually read and the engine's examined
range, and can be one position past the end of the input. It never includes
input examined by the caller before the call. A call that reuses a growing
left-recursive seed sees the frontier stored with an earlier seed, so a call
that completes in such a provisional context reports at least its end; these
results are never reused after an edit.

A rule's profile `Examined` sums, over completed calls, `Examined - Pos`;
`Wasted` sums the same over completed failed calls with `Memo=false`. Both, and
their `Known` flags, are known only when every completed call of the rule in the
profile reported a known frontier; a profile that also accumulates batch parses
of the rule reports them as unknown.

After a completed parse, the invocation's increases in profile `Evals` and
`MemoHits` MUST equal `Document.Stats().Evaluated` and `.Reused`, respectively.
`Stats` has no call counter. An
interrupted parse MAY leave statistics for calls that began but did not
complete. Temporary observer and profile state MUST be released after return
or panic; retained constructor defaults remain available for later calls.
While a Document generated with observability parses, its `Parse`,
`ParseAST` and `Edit` MUST return the generated `ErrDocumentBusy`, before
validating options and without changing the Document; this rejects calls a
callback makes on the same Document. The outer parse continues unaffected.
Callers SHOULD schedule later work or use another Document; parses of other
Documents and batch `Parse` calls remain allowed from callbacks.

## Generated TypeScript Batch Tracing

TypeScript batch tracing MUST be enabled at generation with
`Observability` of `gen.TypeScript` or `pego gen -lang ts -observability`.
It applies to generated `parseWithOptions`, `parseRuleWithOptions` and, when
generated, `recognizeWithOptions`. With `-types`, the configured-start
`parseASTWithOptions` also accepts trace options; the legacy positional-unit
APIs do not. TypeScript does not expose a named typed-rule entry point.

The tracing fields added to `ParseOptions` are:

```ts
trace?: (event: TraceEvent) => void;
traceRules?: readonly string[];
traceMaxDepth?: number;
```

`trace` is a synchronous callback receiving a copied `TraceEvent`;
`traceRules` selects named rules and their descendants, and `traceMaxDepth`
limits delivered events. Omitted or empty `traceRules` selects all rules;
unknown names select none. `traceMaxDepth` MUST be a safe integer. The start
rule has depth one; a nonpositive value means unlimited depth.
Callback mutation MUST NOT affect parser state or later events. If a callback
throws, the original thrown value MUST propagate after runtime cleanup, even
when it is not an `Error`; an aborted parse may omit exits for active calls.
Filtered events do not change matching. Typed-result conversion MUST NOT emit
a separate event; observed calls correspond to the selected parser path.

TypeScript events use camel-case fields. `kind` is `"enter"`, `"exit"` or
`"skipped"`; events also contain `rule`, `level`, `depth`, `pos`, `matched`,
`end`, `memo`, `evals`, `examined`, `examinedKnown` and `skipReason`. Positions
use the selected unit. `matched`, `end`, `memo` and `evals` describe exits;
`memo` indicates reuse without body evaluation, while `evals` counts body
evaluations. A skipped event is unpaired and is currently emitted only for
identifiable first-character dispatch skips. `examinedKnown` is always false
and `examined` MUST NOT be treated as a complete examined-input range. This
TypeScript batch API does not provide Document event tracing.

### Generated TypeScript Batch Profiling

TypeScript batch profiling MUST be emitted only when observability is enabled
with `Observability` of `gen.TypeScript` or `pego gen -lang ts
-observability`. The generated module MUST export `Profile` and
`ProfileSnapshot`, and add `profile?: Profile | null` to `ParseOptions`.
`parseWithOptions`, `parseRuleWithOptions`, and `recognizeWithOptions` MUST
accept this option; with `-types`, `parseASTWithOptions` MUST accept it too.
Legacy positional-unit APIs MUST NOT accept profiling options, and TypeScript
MUST NOT add a named typed-rule API for this feature.
Omitting the option or passing `null` MUST disable collection.

`Profile` MUST aggregate completed calls across invocations without retaining
input, callbacks, parser history or per-invocation frames. Each invocation MUST
keep independent accounting. A profile sink MUST NOT be shared by concurrent
calls. `snapshot()` MUST return independent rows and nested collections sorted
by rule name and skip reason, so JSON serialization is deterministic and
caller mutation cannot change the sink. Counters MUST use camel-case field
names corresponding to the generated Go profile. Entered-call counts MUST
include calls that began; completed-call counts and timings MUST exclude calls
without an exit. Profile collection MUST be independent of `traceRules` and
`traceMaxDepth`; skip counts MUST be independent of trace filters.
The snapshot's total fields are `parses`, `entered`, `calls`, `evals`,
`memoHits`, `skipped`, `time` and `rules`. Each rule row MUST expose `rule`,
`entered`, `calls`, `evals`, `memoHits`, `matched`, `failed`, `consumed`,
`skipped`, `skipReasons`, `repeats`, `maxEvals`, `maxEvalsAt`, `examined`,
`examinedKnown`, `wasted`, `wastedKnown`, `time` and `selfTime`.
Each `skipReasons` item MUST contain `reason` and `count`; `maxEvalsAt` MUST
contain `pos` and `level`. `repeats` MUST count additional completed non-memo
calls with `evals > 0` at the same `(rule, pos, level)` within an invocation.
`maxEvals` MUST be the largest such call count at a location, and ties MUST
retain the first observed location.

`examinedKnown` and `wastedKnown` MUST be false because examined and wasted
ranges are unavailable; implementations MUST NOT infer them. `time` and
`selfTime` MUST be milliseconds from the host's monotonic `performance.now()`
clock. Root time MUST exclude input setup, whole-input end checks and
typed-result conversion. Per-rule time MUST count recursive calls of the same
rule once per invocation; `selfTime` MUST exclude child time. Both MUST include
synchronous trace-callback work. When the exit-time clock read succeeds, a
completed call MUST be published after its exit callback returns or throws.
A snapshot inside that callback MAY omit the current exit until it unwinds.
An enter callback throw MUST NOT synthesize an exit.

Profiling MUST require `performance.now()` and MUST NOT fall back to
`Date.now()`. If the clock is unavailable, the profiled invocation MUST return
an `Error`. Valid clock readings MAY have coarse resolution and produce zero
durations. A thrown clock getter/call MUST preserve its original thrown value;
non-finite and backward readings MUST fail explicitly and MAY leave a partial
profile. If both a trace callback and a later clock read fail, the callback's
original thrown value MUST take precedence, and collection MAY omit that
exit. The implementation MUST NOT require DOM type declarations.
