# Streaming

A stream parse reads its input from an `io.Reader` and hands each element of a
`#stream` repetition to the caller as soon as the element matches. The parser
then forgets the element and the input before it, so the memory a parse retains
depends on the size of one element and not on the size of the input. This
chapter defines the grammar construct, the Go entry points and their results.
The `#stream` pragma itself is listed in [Pragmas](pragmas.md#stream).

## Scope and Terminology

An **element** is the value that one iteration of a `#stream` repetition
contributes to the `List` of an ordinary parse: a struct node if the element is
a rule with an action, and a node of the concrete syntax tree (`Seq`, `Match`
and so on) otherwise. An element has no value if its expression has none, for
example `(-"a")* #stream`; the caller then receives `nil`.

A **stream parse** is a parse started with `Parser.ParseStream` or
`Parser.Iterate`, or with `pego parse -stream`. The **streaming repetition** is
the `#stream` repetition of the start rule.

## Contract

### Grammar

| Property | Definition |
|:--|:--|
| Placement | `#stream` MUST annotate a repetition (`*`, `+` or `#repeat(n,m)`) that is the body of a rule, an item of the body's top-level sequence, or the expression captured by such an item (`items:record* #stream`) |
| Arguments | `#stream` takes none |
| Multiplicity | A rule body has at most one `#stream` |
| Streaming rule | Only the start rule of the parse, at call depth 1, streams. In every other call of a rule that contains `#stream`, the repetition is an ordinary repetition |
| Suffix | After the repetition ends, the rest of the start rule MUST match as in an ordinary parse |
| Finite bounds | `#repeat(n,m)` delivers at most `m` elements and fails if fewer than `n` match. `#repeat(0)` executes no elements. Input beyond the maximum is left for the suffix: `"a" #repeat(1,2) #stream "a" $$` accepts `aaa` and delivers two elements. A later suffix or minimum failure does not retract elements already delivered |
| Empty elements | If the maximum permits an element, the first element that matches the empty string is delivered once and ends the repetition. It counts toward the minimum; if the total is still below the minimum, the parse fails without retracting delivered elements. For example, `_ #repeat(2) #stream` on empty input delivers once and fails, as batch parsing fails after one empty iteration |
| Recovery | `#recover` MUST NOT wrap the `#stream` repetition, in either pragma order, because rewinding could reach discarded input. `#recover` inside each repeated element or its rule is allowed. `@error` MAY label the streaming repetition. A compiled grammar that contains the unsafe wrapper is rejected when it is loaded, including a file without an AST |
| Ordinary parses | `Parse` and every other non-stream entry point treat `#stream` as an ordinary repetition |
| Recognition | Recognition treats `#stream` as an ordinary repetition and invokes no callbacks. Captures that element predicates read are still built; element values are not collected |

### Entry points

```go
func (p *Parser) ParseStream(r io.Reader, emit func(*Node) error, opts ...ParseOption) error
func (p *Parser) Iterate(r io.Reader, opts ...ParseOption) (*StreamIterator, error)
```

`ParseStream` calls `emit` once per element, in input order, and returns only an
error: it returns no tree. The values of whatever precedes or follows the
streaming repetition (a header, a footer) are matched and checked and then
discarded. The start rule MUST contain a streaming repetition; otherwise
`ParseStream` returns the error `rule main has no #stream repetition` (with the
start rule's name), even if a rule that the start rule calls has one. Another
rule streams when it is made the start rule with `Parser.WithStart`.

`pego parse -stream` prints each element on its own line (JSON by default, an
S-expression with `-f sexpr`) as it is delivered, then reports the final error,
if any. It cannot be combined with `-check`.

`Iterate` is the pull form. It validates the entry point without reading.
`StreamIterator.Next` advances the parse to one committed element or to the
terminal result and returns `(node, ok, err)`; `ok` distinguishes a successful
`nil` element from the end of the stream. After completion, repeated calls
return the same terminal error or a clean end. `Close` is idempotent; after an
early `Close`, `Next` returns `ErrStreamClosed`. The iterator does not close its
reader. One goroutine owns an iterator: calls to `Next` and `Close` MUST NOT
overlap, including from a callback. `Close` cannot interrupt a blocked `Read`.

### Delivery and finality

- Each element is delivered as soon as it matches, in the middle of the parse and
  before the next element is tried. The parser reads nothing from the reader while
  `emit` runs.
- An element is final. Once `emit` has seen it, the parser never backtracks to
  before it. What the caller receives is provisional until the parse returns a nil
  error, because input after the last element can still be invalid.
- The parser reads input only as far as needed. An element that ends in a fixed
  literal is delivered when its last character arrives. An element that ends in a
  repetition (`+`, `*`, `?`) is delivered after the parser has seen one further
  character, because that character shows that the repetition ended. This holds in
  both position units: decoding an ASCII character or a complete UTF-8 sequence
  does not wait for unrelated later bytes. An incomplete UTF-8 prefix waits for the
  remaining bytes or for the reader's end of input.
- PEG lookahead and buffering MAY cause the parser to read beyond the element
  that a `Next` call returns.

### Positions and text

`Node.Start`, `Node.End`, `startPos` and `endPos` in actions, and `SyntaxError.Pos`,
`Line` and `Col` count from the start of the whole input, regardless of how much
has been discarded; the parser keeps a running line and column for the discarded
part. The position unit option changes all of them to bytes.

The parser keeps no text for the caller. Terminal nodes (`Match`, terminal types,
`(...) #atomic`) carry their text in `Node.Text`; structural nodes (`Seq`, `List`)
do not. `text(...)` in an action of the start rule, which runs after elements have
been discarded, returns only the part of the node's input that is still held,
possibly the empty string.

After an element is delivered, the input before it is discarded except for the one
character before it, so `^` (beginning of line) still holds at the start of the next
element. No construct of the language looks further back.

### Options

| Option | Effect in a stream parse |
|:--|:--|
| `WithUnit`, `WithBackend`, `WithMaxDepth` | Apply |
| `WithContext`, `WithWorkLimits` | Apply to the whole stream invocation, including diagnostic callbacks and the work to copy a bounded tail |
| `RecognizeOnly` | An error |
| `WithDiagnosticCallback`, `WithDiagnosticLimit` | Stream-only; see [Recovered errors](#recovered-errors) |

A `Parser` supports any number of concurrent stream parses; each has its own state.

## Validation and Errors

The compile-time errors for `#stream`, in the order in which they are checked,
are:

1. `#stream must be attached to a repetition (*, +, or #repeat(...))`
2. `#stream is only allowed at the top level of a rule body`
3. `a rule body can have only one #stream`
4. `#stream takes no arguments`

A stream parse ends in one of the following ways.

| Cause | Result |
|:--|:--|
| The input does not match | A `*SyntaxError` with the absolute position, line and column. Elements delivered earlier stay delivered. The repetition ends where an element stops matching; the parser then expects either another element or the rest of the start rule, so a trailing `$$` makes unparsable input an error and not a silent stop |
| `emit` returns an error | Parsing stops at once, no further element is delivered and `ParseStream` returns that error unchanged (`errors.Is` and `==` both work) |
| The reader fails | A non-EOF error from `Read` is returned as it is. Elements completed before the failure are delivered; an element cut by the failure is not |
| Errors were recovered | `ParseStream` returns the recorded errors as `SyntaxErrors` after every element was delivered |
| Cancellation or a work limit | The interruption error is returned instead of a completed diagnostic result; elements already delivered remain delivered. A reader or callback blocked in caller code cannot be preempted |
| `emit` or the reader panics | The panic propagates with its original value on every backend, including values that implement `runtime.Error`. Delivered elements stay valid; the parser does not close the reader |

### Recovered errors

With `#recover` inside an element, a bad element becomes an `Error` node that is
delivered like any other element, and parsing continues. At the end the parse
returns the recorded errors as `SyntaxErrors`, a result that means success with
diagnostics and is distinguished from a fatal `*SyntaxError` by `errors.As`. By
default all recovered errors are retained until the end of the input.

`WithDiagnosticCallback(func(*SyntaxError) error)` receives each committed
recovery immediately before the element it belongs to; `Next` invokes it while
advancing to that element. Recoveries that were backtracked are not reported.
The callback receives a copy that it may keep or modify. If it returns an error,
parsing stops with that error and the element is not delivered. Recoveries in a
successful suffix are reported before the terminal result.

`WithDiagnosticLimit(n)` retains only the latest `n` committed recoveries, in
order of occurrence. A positive limit bounds the retained tail, while the callback
still receives every committed recovery for as long as it returns nil. If errors
were dropped, the terminal error is `*TruncatedSyntaxErrors` with the fields
`Errors`, `Total`, `Dropped` and `CountsSaturated`; its `Unwrap` exposes the
retained errors as `SyntaxErrors`. Counts saturate and do not wrap; when
`CountsSaturated` is true, `Total` and `Dropped` are lower bounds. If the total
does not exceed the limit, the terminal error is an ordinary `SyntaxErrors`. A
limit of zero keeps the unbounded behavior. A negative limit is invalid. The two
diagnostic options are rejected by `Parse`, `ParsePrefix` and `Document`; any
value, including zero, is a stream-only option. A limit does not bound what the
callback stores. With tracing enabled and a positive limit, the successful
start-rule exit event exposes the retained tail through `TraceEvent.Recovered()`;
only the terminal `TruncatedSyntaxErrors` has the total and dropped counts.

## Memory

An action MAY construct temporary nodes and return `nil`. The caller then receives
`nil`, and the action's construction tracker releases its references before
delivery; nodes that the action returns are unaffected. Variables hold scalar
values. Repeated assignments to a variable keep one current binding per name:
environments saved for backtracking or for a caller stay immutable, but the
current environment does not retain every earlier assignment from consumed
elements, and lookup cost depends on the number of distinct names. Large variable
values, active rollback points and memoization keys keep their own data alive.

## Compatibility and Support

Streaming is supported by the engine on all three backends. Generated Go parsers
provide `ParseStream` and `Iterate` when generated with streaming enabled, and
typed variants with typed values; see [Generated Parsers](generated-parsers.md).
Generated TypeScript parsers have no stream entry point and treat `#stream` as an
ordinary repetition. A `Document` is always parsed as a whole and does not stream.
Grammars that use `#missing` cannot be used in a stream parse.

## Conformance

The behavior is established by the stream tests of the repository
(`stream_iterator_test.go`, `stream_diagnostics_test.go`,
`stream_diagnostic_tail_test.go`) and by the backend equivalence tests, which
require identical elements and errors from every backend.
