# Parsing

The preceding chapters define what a grammar means rule by rule. This chapter
defines a parse as a whole: how it starts, what the input and its positions are,
what a parse produces, how a syntax error is determined and reported, what
memoization can and cannot change, and the limits and modes of a parse.

## Start Rule

A parse starts at a rule, the **start rule**. The start rule is not part of the
grammar file: it is selected when the grammar is compiled or when parsing is
started.

| Interface | How the start rule is selected |
|:--|:--|
| Go API | The `start` argument of `pego.Compile` and `pego.CompileSource`; `Parser.WithStart` makes a parser with another start rule for whole-input parsing; `Parser.ParsePrefix` selects a named rule for one prefix parse without changing the configured start rule |
| Command line | `pego parse -s <rule>`. It defaults to `main`, or to the start rule saved in a compiled grammar (`.pegoc`) |

Any rule can be the start rule. It MUST be defined; parsing with an undefined
start rule is an error. The start rule is called at the beginning of the input
with no binding level, so an Operators Rule in it parses the operators of all
levels (see [Level-restricted calls](operators-rule.md#level-restricted-calls)).

## Input and Positions

The input of a parse is a sequence of Unicode characters. The input is given as
text encoded in UTF-8; a byte that is not part of a valid UTF-8 sequence is read
as U+FFFD (REPLACEMENT CHARACTER) occupying one position.

### Positions

A **position** is a number that identifies a place between two characters of the
input: 0 is the beginning, and the length of the input is the end. The **range**
of a node is a pair of positions `[start, end)`.

Positions are measured in a **position unit**, which is selected for each parse.
The unit applies to node ranges, the `startPos` and `endPos` fields and the
`len` function in actions, and the line and column of syntax errors.

| Position unit | A position is the number of ... from the beginning of the input |
|:--|:--|
| Code points (default) | Unicode code points |
| Bytes | bytes of the UTF-8 encoding |

The unit affects only how positions are counted. Matching is always performed
one code point at a time: a character class or `.` matches exactly one code
point in either unit. In bytes, a code point that is encoded in several bytes
advances the position by that many, and each byte that is not part of a valid
UTF-8 sequence counts as one position (and one character U+FFFD) in either
unit.

This decoding also applies to literals. A U+FFFD in a literal matches either
its valid UTF-8 encoding or one invalid input byte, in either position unit.
For example, `rule main = "�"` accepts the single byte `0xff`; the
matched range ends at position 1. Two invalid bytes require two replacement
characters. Position units do not change which Unicode characters match.
The terminal's text is the matched input: byte mode keeps the invalid byte,
while code-point mode substitutes U+FFFD. The literal's spelling need not have
the same byte length as the matched input.

The unit is selected with `pego.WithUnit(pego.Bytes)` in the Go API and with
`pego parse -unit bytes` on the command line.

### Lines and Columns

Lines and columns in syntax errors are 1-based. A new line starts after each
line feed (U+000A); a carriage return does not start a line. The column is
counted in the position unit. (This differs from grammar source files, where a
carriage return can end a line; see
[Line terminators](lexical.md#line-terminators).)

## Whole Input

Whole-input entry points such as `Parser.Parse` succeed only if the start rule
succeeds **and** consumes the entire input. Parsing evaluates the start rule at
position 0. If the rule fails, the parse fails with a
[syntax error](#syntax-errors). If the rule succeeds without reaching the end of
the input, `end of input` is recorded as expected at the position where it
stopped, and the parse fails with a syntax error. The error is reported at the
farthest position that parsing reached, which can be beyond where the start
rule stopped.

```pego
rule main = "a" ("b" "c")?
```

For the input `abd` a whole-input parse fails at `1:3` expecting `"c"`; for
the input `ad` it fails at `1:2` expecting `"b"` or the end of input.
`Parser.ParsePrefix(name, input, ...)` instead evaluates the named rule at
position 0 without an implicit end-of-input check. A successful prefix returns
both the rule value and the consumed cursor in the selected position unit and
original input bytes; an unconsumed suffix is allowed. An explicit
[anchor](parser-expressions.md#anchors) `$$` inside that rule still requires the
actual end of the supplied input. The API does not change the parser's
configured start rule or shift positions to an enclosing document.

## Results

A parse has one of the following outcomes.

| Outcome | What the parse returns |
|:--|:--|
| Success | The value of the start rule, which is a [node](#nodes) or `nil` |
| Success with recovered errors | The value, together with the list of [syntax errors](#syntax-errors) that [`#recover`](pragmas.md#recover) recovered from |
| Syntax error | No value; the [syntax error](#syntax-errors). If errors were recovered before, only the final syntax error is returned |
| Run-time error | No value; the error of an [action](actions.md#types-and-errors) or a [limit](#limits) that was exceeded. It aborts the whole parse |

The value of the start rule is nil if the rule has no value. For example, a
rule `rule main = -"a"` or `rule main = "a"?` on empty input succeeds with `nil`.

### Nodes

A **node** has the following properties.

| Property | Meaning |
|:--|:--|
| Type | The type name: a [reserved node type](types.md#reserved-node-types) (`Match`, `Seq`, `List`, `Operator`, `Error`, `Missing`) for the nodes of the concrete syntax tree, or the name of a type defined in the grammar |
| Rule | The name of the rule that produced the node, if it is a node that a rule without an action created in its own body (see [Values and the concrete syntax tree](parser-expressions.md#values-and-the-concrete-syntax-tree)); otherwise empty |
| Range | `[start, end)` in the position unit |
| Text | For a terminal, the matched text |
| Children | For a `Seq`, `List` or `Operator`, the child nodes; a child is `nil` where an optional expression did not match |
| Fields | The captures of a `Seq` or of a node that a rule produced, and the fields of a struct node. Each value is a node, an integer, a string, a boolean or `nil` |

The Go type `pego.Node` and its JSON form are described in
[The Node type](../docs/guide/runtime.md#the-node-type).

## Incremental Documents

A `Document` owns mutable text and parser reuse state and is not safe for concurrent use. Its position unit is fixed at construction; `Document.Unit` reports that unit. `Document.Version` returns a comparable opaque token identifying the current text revision. A new document starts at sequence 1. Parsing does not advance the version, and the token does not retain the document, text, or parse caches.

`Document.EditBatch(base, edits...)` applies replacements simultaneously to the text identified by `base`. Every range uses coordinates in the original base text and the document's position unit. Starts must be strictly increasing and ranges must not overlap; a repeated start, including multiple insertions at one position, is rejected. A token from another document or an older revision is rejected even for an empty batch. An empty valid batch changes neither text nor version. A successful nonempty batch advances the version once and does not parse. A successful `Document.Edit` also advances it once, including a no-op edit. Sequence overflow is rejected before mutation.

The implementation validates and prepares a complete batch before publishing it. A rejected batch leaves text, version, statistics, reusable state, and previously returned Nodes unchanged. In `Bytes` mode, endpoints are checked against character boundaries in the original base text; applying replacements may create or remove UTF-8 sequences at edit seams, which are decoded by the next parse. In `CodePoints` mode, invalid UTF-8 in replacement text is normalized one replacement character per invalid byte. After a successful batch, the next parse must match a fresh parse of the resulting text. Batch commit itself does not parse and conservatively clears reusable parse caches; it does not promise incremental reuse.

`Document.Parse` publishes recovered parser-produced syntax diagnostics as caller-owned values: it copies each `SyntaxError` and its `Expected` and `Messages` slices, so caller mutation cannot change memoized errors or a later parse. Ordinary parse failures are newly constructed; caller-supplied errors such as `Context.Err` retain their identity. This does not detach the mutable `Node` graph, which may share nodes with the `Document`. Parsing and diagnostic publication use the same invocation controls. If publication is interrupted, no parse result is returned and reusable execution state is cleared for retry.

### Document Snapshots

`Document.Snapshot(opts...)` captures the current text revision, parse outcome,
and value graph in one immutable `Snapshot`. It resolves invocation overrides
once over construction defaults; context, work limits, maximum depth, and tracing
may vary per call, while the Document's unit, backend, start rule, and
non-recognition mode do not. Parsing and freezing use the same cancellation
context and cumulative input-work and execution-step counters. Copying the text,
freezing nodes and fields, and copying diagnostics are part of that invocation.

A normally returned parse outcome produces a Snapshot even when the outcome is
an ordinary syntax error or recovered error. For an ordinary syntax failure the
Snapshot retains the current text and version, has a zero root as `Document.Parse`
does, and exposes a defensive `SyntaxError`. A recovered outcome retains its
root and `SyntaxErrors`. Cancellation or exhausted work limits during parsing or
freezing return a nil Snapshot and a fatal interruption error; grammar choice,
predicates, and recovery cannot absorb the interruption. No partial graph,
text, or diagnostics are published. If execution began, reusable execution
state is cleared before retry; fully completed input tables may remain, but
partial tables are discarded. Pre-entry errors preserve existing caches and
statistics. Cleanup is uncharged. A trace callback panic propagates unchanged
after cleanup. Snapshot accessors and `Materialize` run after capture and do not
inherit its context or work limits.

Snapshot freezing preserves shared node identities, cycles, nil child slots, and
nil versus allocated-empty collections. It accepts field values with dynamic
types `nil`, `int`, `string`, `bool`, and `*Node`, including a typed nil
`*Node`. Any other dynamic field type returns no Snapshot and a
`SnapshotCaptureError` containing the deterministic path and `reflect.Type`,
without retaining the unsupported value. The error also carries a defensive
copy of the ordinary parse error, if one existed. Error copies preserve
`errors.Is` and `errors.As` behavior. A present nil node field is represented by
a zero `SnapshotNode`; `Field`'s boolean distinguishes it from an absent field,
and an untyped nil remains untyped nil. Snapshot handles are immutable and safe
for concurrent reads while the Document has one editing/parsing owner.
`Materialize` returns an independent mutable `Node` graph and preserves shared
identities and cycles.

## Syntax Errors

When the input does not match the grammar, the parser reports the **farthest
position** that parsing reached, together with the items that were expected at
that position: literals, character classes, `any character` (for `.`) and
anchors (`beginning of input`, `end of input`, `beginning of line`,
`end of line`).

```
1:5: syntax error: expected "(", "-", [0-9]
```

- The position is given as a 1-based line and column. The column is counted in
  the [position unit](#positions) (code points by default).
- The expected items are listed in sorted order, without duplicates.
- Failures inside [lookahead](parser-expressions.md#lookahead) and inside the
  [`skip` of an Operators Rule](operators-rule.md#skip) are not included. Predicates
  and bottom (`_|_`) contribute no expected items.
- If the start rule matches but input remains, `end of input` is expected at the
  position where the start rule stopped (see [Whole input](#whole-input)).
- If nothing was expected at the farthest position, the message is just
  `syntax error`.
- A [`@error`](attributes.md#error) attribute replaces the expected items inside
  an expression with a message. If a message applies at the farthest position,
  the message is reported instead of the expected items.

## Memoization

An implementation memoizes the result of a rule at a position, so that the rule
is not evaluated twice there (packrat parsing). Memoization has the following
observable consequences, and no others.

- **It never changes the result of a parse.** The value, the recovered errors
  and the syntax error are the same with and without memoization. Memoization
  changes only the cost.
- **Node identity is unspecified.** Whether two calls of a rule at the same
  position return the same node, or equal nodes, depends on memoization. To
  compare what two nodes matched, compare their text or their fields (see
  [Operators](actions.md#operators-and-functions)).
- **Rules that read variables are memoized per combination of values.** The
  result of a rule that reads [variables](predicates.md#variables) directly or
  through the rules it calls depends on their values when it is called, so a
  memoized result is reused only if the variables have the same values; see
  [Predicates and Variables](predicates.md#interaction-with-memoization).
- **Actions run when a rule is evaluated.** An action is evaluated every time
  the body of its rule is evaluated and matches, even if the enclosing
  alternative later fails, and also inside a lookahead, under `#atomic` and under `-`.
  An action has no effect other than its value and its [run-time
  errors](actions.md#types-and-errors), so how many times it runs is not
  observable, but whether it runs is: an error in an action aborts the parse
  even in a branch that is abandoned.
- **Left recursion** is evaluated by growing a seed, which relies on the memo
  (see [Left recursion](parser-expressions.md#left-recursion)).
- **The nesting limit can depend on it.** A call that is answered from the memo
  does not nest, so whether a parse that comes close to the [nesting
  limit](#limits) succeeds can depend on memoization.

## Limits

An implementation MAY limit the nesting depth of rule calls, so that input of
unbounded nesting cannot exhaust the memory of the host. When the limit is
exceeded, the parse fails with a run-time error and no value. In the reference
implementation the error is `nesting too deep: more than N rule calls`; the
default limit is 100,000 nested calls (10,000,000 for the iterative bytecode
VM), and the Go API changes it with `pego.WithMaxDepth`. The operand of a prefix
operator and the right operand of an infix operator of an Operators Rule nest
like rule calls; chains of left-associative operators do not nest.

## Cancellation and Work Limits

The reference Go API accepts invocation-local cancellation and input-work and
execution-step limits through `WithContext` and `WithWorkLimits`; generated Go
exposes corresponding package-local options.
These controls are cooperative and backend-local. A zero limit disables that
counter. Each counter is monotonic for one invocation and resets on the next
call. A reservation is checked before the corresponding work begins; if it
exceeds the remaining allowance, that work is not performed. Counts are
implementation-specific and are not elapsed-time, memory, or buffered-input
limits.

Cancellation and exhausted limits are fatal interruptions, not ordinary
syntactic mismatches. Ordered choice, predicates, recovery, and other grammar
constructs cannot absorb them. An interrupted batch parse returns no partial
Node or typed value. In streaming mode, elements already delivered to the
caller remain delivered; the parser does not retract them. A blocked reader or
caller callback cannot be preempted by a synchronous parser; cancellation is
observed after the call returns.

For a `Document`, a cancellation or option error detected before execution has
touched reusable state preserves its valid caches and last statistics. Once
execution starts, an interruption clears execution memo/repetition state before
the next attempt, because entries can depend on unfinished left-recursion work.
Fully completed input tables may be retained, but incomplete tables are never
published and are discarded. A retry with fresh invocation controls must produce
the same result as parsing the current text from scratch.

The API and callback details are described in the
[Go runtime guide](../docs/guide/runtime.md), [incremental parsing guide](../docs/guide/incremental.md),
and [streaming guide](../docs/guide/streaming.md). The design record specifies
counter units, fatal cleanup and portability limits.

## Generated Go Batch Tracing

Standalone generated Go Node parsers may be generated with tracing enabled by
`pego.WithGeneratedObservability()` or `pego gen -observability`. This mode
supports batch `Parse` and `ParseRule`; it also supports `Recognize` when that
entry point is generated. With generated types, it additionally supports
`ParseAST` and `ParseRuleAST`, for both direct typed execution and Node-conversion
fallback. Typed events describe rule calls made by the selected route;
conversion itself is not a rule event, so Node and typed event streams may
differ. Typed methods use the same event fields, filter, depth, interruption
and callback-panic behavior as Node batch methods. Generated Go Node and typed
batch parsers also provide profiling through `WithProfile`; TypeScript batch
parsers provide profiling through `Profile` in per-call options.

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
`WithGeneratedObservability()` MUST expose `Profile`, `ProfileSnapshot`,
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
completed call; a panic in an enter callback does not synthesize an exit. The measured
opt-in profile path is documented in design 043; these measurements do not
isolate the cost of individual counters or clock reads.

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
`pego.WithGeneratedObservability()` or `pego gen -lang ts -observability`.
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
with `pego.WithGeneratedObservability()` or `pego gen -lang ts
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

## Modes

The same grammar can be run in several modes. Except where stated, each mode
gives the results that an ordinary parse gives.

| Mode | Difference from an ordinary parse |
|:--|:--|
| Backends | The reference implementation has three backends (closure, recursive bytecode and iterative bytecode). They give identical values, errors and positions, and differ in speed and in the limit of nesting depth. Generated Go and TypeScript parsers implement the same semantics; their differences, such as the nesting limit, are described in the guides |
| Recognition | The parse only tells whether the input matches. It gives the same success or failure and the same syntax errors, builds no tree, and skips actions whose values are not needed by predicates. Actions needed by predicates still run and can report run-time errors. The captures that [predicates](predicates.md) read are still built |
| Stream | A parse that reads its input incrementally and hands each element of the [`#stream`](pragmas.md#stream) repetition of the start rule to the caller as it matches. The elements are not part of the value |
| Incremental | A document that is edited and reparsed reuses results of the previous parse. The result of a parse is the same as that of a parse of the whole new text |

The guides describe these modes from the user's side: see
[Running parsers](../docs/guide/runtime.md), [Streaming](../docs/guide/streaming.md) and
[Incremental parsing](../docs/guide/incremental.md).
