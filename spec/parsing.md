# Parsing

The preceding chapters define what a grammar means rule by rule. This chapter
defines a parse as a whole: how it starts, what the input and its positions are,
what a parse produces, how a syntax error is determined and reported, what
memoization can and cannot change, and the limits and modes of a parse.

## Start Rule

A parse starts at a rule, the **start rule** of the parse. It MUST be one of the
start rules of the grammar, the rules marked `@start` (see
[Start Rules](grammar-files.md#start-rules)). When no rule is named, the parse
uses the default start rule of the grammar.

| Interface | How the start rule is selected |
|:--|:--|
| Go API | The `start` argument of `pego.Compile` and `pego.CompileSource`, where an empty name selects the default start rule; `Parser.WithStart` makes a parser with another start rule for whole-input parsing; `Parser.ParsePrefix` selects a start rule for one prefix parse without changing the configured start rule |
| Command line | `pego parse -s <rule>`. It defaults to the start rule saved in a compiled grammar (`.pegoc`), and otherwise to the default start rule |
| Generated parsers | `Parse` uses the start rule selected when the parser was generated, the default start rule unless another was named; `ParseRule` and `ParseRuleAST` take the name of a start rule |

Naming a rule that is not a start rule is an error that names the rule and says
it is not a start rule; naming an undefined rule is an error that says it is not
defined. The start rule is called at the beginning of the input
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
The unit applies to node ranges, the `.Start` and `.End` members and the
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
The text of the string node is the matched input: byte mode keeps the invalid byte,
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

The value of the start rule is nil if the rule's value is an optional that did not
match. For example, `rule main = "a"?` on empty input succeeds with `nil`. A rule
whose expression has no valued element, such as `rule main = -"a"`, has the unit
`()` as its value.

### Nodes

The value of a rule without an action is a node. The properties of a node, the
Go type `pego.Node`, traversal, and the text and JSON renderings are defined in
[Nodes and Values](nodes.md).

## Prefix Parsing

`Parser.ParsePrefix(name, input, opts...)` selects the rule `name` for one call
and returns its successful endpoint without requiring the rule to consume the
whole input. It does not change the start rule that the parser was configured
with. The rule MUST be defined; an undefined name is an error. The input is
decoded as for a whole-input parse.

The result is a `PrefixResult`:

| Field | Meaning |
|:--|:--|
| `Node` | The value of the selected rule, as for a whole-input parse (a node, a value node or `nil`) |
| `Consumed` | The successful cursor in the parser's position unit (code points by default) |
| `ConsumedBytes` | The endpoint in bytes of the original UTF-8 input |

Each malformed UTF-8 byte counts as one replacement code point in `Consumed`,
while `ConsumedBytes` still refers to the original bytes. Positions inside `Node`
are local to `input`; a caller that passes a slice of a larger source adds the
slice's starting byte offset to `ConsumedBytes` to find the endpoint in the
source. Both consumption fields come from the parser's cursor and are
independent of the span of `Node`: a rule whose action returns a smaller node, or
`nil`, still reports everything it consumed. With the `Bytes` unit, node spans and
both consumption fields are in bytes. A node span in code points is converted to
bytes before a base offset is added. A zero-width success returns zero
consumption, and a nil `Node` does not imply failure. Only the runtime `Parser`
provides `ParsePrefix`; generated parsers do not.

Outcomes:

| Outcome | Result |
|:--|:--|
| The rule matches | The `PrefixResult` and a nil error |
| The rule matches after `#recover` | The `PrefixResult` and `SyntaxErrors`, as a whole-input parse returns a node with recovered errors |
| Syntax failure, including a failure after an earlier recovery, or a fatal error | The zero `PrefixResult` and the error. No speculative endpoint is exposed |

A zero result alone cannot distinguish failure from a successful zero-width rule
that returns nil; the error does.

`ParsePrefix` accepts position units, backends, recognition, tracing, nesting
limits, context and work limits. Stream diagnostic options are rejected, as for
`Parse`. A grammar that contains `#missing` is rejected, even if insertion is not
enabled; a positive `WithMissingTokens` limit is rejected for every grammar. Zero
keeps strict prefix matching. Options resolve in order. An explicit `$$` sees the
actual end of the entire supplied string, so a fragment is confined by slicing
both its beginning and its end. Cuts and left recursion keep their ordinary
meaning. Prefix parsing shares no memo entries with a `Document`, does not
shift positions to absolute coordinates and does not map escaped or noncontiguous
fragments. It can run concurrently on one parser. Compiled grammars support it
through the bytecode backends; recognition keeps the recognition requirements of
the compiled grammar.

## Incremental Documents

A `Document` owns mutable text and the reuse state of earlier parses. A parse of
a document returns what a parse of the current text from scratch returns. The
document API, versions, snapshots, interruption and change notifications are
defined in [Incremental Documents](documents.md).

## Syntax Errors

When the input does not match the grammar, the parser reports the **farthest
position** that parsing reached, together with the items that were expected at
that position: literals, character classes, `any character` (for `.`) and
anchors (`beginning of input`, `end of input`, `beginning of line`,
`end of line`).

```text
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

### Errors in Go and on the Command Line

In the Go API, a failed parse returns a `*pego.SyntaxError` with the fields `Pos`
(the position in the position unit), `Line`, `Col`, `Expected` (the sorted expected
items) and `Messages` (the messages of `@error` attributes), and the methods
`Error()`, which returns the text above with its position, and `Message()`, which
returns it without the position. A parse that recovered errors returns the node and
an error of type `pego.SyntaxErrors`, a list of `*SyntaxError`; `errors.As` tells it
apart from a fatal `*SyntaxError`, and the node is valid. The command `pego parse`
prints the tree and then each error on its own line, and exits with status 1 in
both cases. Run-time errors of actions, nesting-limit errors and interruptions are
other error values, not `SyntaxError`.

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

In the Go API, `WithContext(ctx)` supplies the context and
`WithWorkLimits(WorkLimits{MaxInputWork, MaxSteps})` the limits. Input work
counts inspected elements of the input representation; steps count backend
execution work. Charges are not rolled back by backtracking, and their values
depend on the backend, the position unit and the generated artifact. A cancelled
context returns the error of the context (`errors.Is(err, context.Canceled)` or
`context.DeadlineExceeded`); an exhausted limit returns an error that matches
`pego.ErrWorkLimit`, and `errors.As` with `*pego.WorkLimitError` gives the
details. Stream parses and documents are specified in [Streaming](streaming.md)
and [Incremental Documents](documents.md).

## Modes

The same grammar can be run in several modes. Except where stated, each mode
gives the results that an ordinary parse gives.

| Mode | Difference from an ordinary parse |
|:--|:--|
| Backends | The reference implementation has three backends (closure, recursive bytecode and iterative bytecode). They give identical values, errors and positions, and differ in speed and in the limit of nesting depth. Generated Go and TypeScript parsers implement the same matching semantics; the entry points, features and limits that differ are defined in [Generated Parsers](generated-parsers.md) |
| Recognition | The parse only tells whether the input matches. It gives the same success or failure and the same syntax errors, builds no tree, and skips actions whose values are not needed by predicates. Actions needed by predicates still run and can report run-time errors. The captures that [predicates](predicates.md) read are still built |
| Stream | A parse that reads its input incrementally and hands each element of the [`#stream`](pragmas.md#stream) repetition of the start rule to the caller as it matches. The elements are not part of the value. See [Streaming](streaming.md). Generated TypeScript parsers have no stream mode |
| Incremental | A document that is edited and reparsed reuses results of the previous parse. The result of a parse is the same as that of a parse of the whole new text. See [Incremental Documents](documents.md). Generated TypeScript parsers have no document, and generated Go parsers support a subset |
| Prefix | A named rule is parsed at the beginning of the input without requiring the whole input to be consumed. See [Prefix Parsing](#prefix-parsing) |
