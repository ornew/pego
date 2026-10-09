# Parsing

The preceding chapters define what a grammar means rule by rule. This chapter
defines a parse as a whole: how it starts, what the input and its positions are,
what a parse produces, how a syntax error is determined and reported, what
memoization can and cannot change, and the limits and modes of a parse.

## Start rule

A parse starts at a rule, the **start rule**. The start rule is not part of the
grammar file: it is selected when the grammar is compiled or when parsing is
started.

| Interface | How the start rule is selected |
|:--|:--|
| Go API | The `start` argument of `pego.Compile` and `pego.CompileSource`; `Parser.WithStart` makes a parser with another start rule |
| Command line | `pego parse -s <rule>`. It defaults to `main`, or to the start rule saved in a compiled grammar (`.pegoc`) |

Any rule can be the start rule. It MUST be defined; parsing with an undefined
start rule is an error. The start rule is called at the beginning of the input
with no binding level, so a Pratt expression in it parses the operators of all
levels (see [Level-restricted calls](pratt.md#level-restricted-calls)).

## Input and positions

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

The unit is selected with `pego.WithUnit(pego.Bytes)` in the Go API and with
`pego parse -unit bytes` on the command line.

### Lines and columns

Lines and columns in syntax errors are 1-based. A new line starts after each
line feed (U+000A); a carriage return does not start a line. The column is
counted in the position unit. (This differs from grammar source files, where a
carriage return can end a line; see
[Line terminators](lexical.md#line-terminators).)

## Whole input

A parse succeeds only if the start rule succeeds **and** consumes the entire
input. Parsing evaluates the start rule at position 0. If the rule fails, the
parse fails with a [syntax error](#syntax-errors). If the rule succeeds without
reaching the end of the input, `end of input` is recorded as expected at the
position where it stopped, and the parse fails with a syntax error: the error is reported at the farthest position that parsing reached, which
can be beyond where the start rule stopped.

```pego
def main = "a" ("b" "c")?
```

For the input `abd` the parse fails at `1:3` expecting `"c"`; for the input `ad`
it fails at `1:2` expecting `"b"` or the end of input. A grammar that is meant to
parse a prefix of its input has to say so in the grammar, for example by ending
the start rule with `.*`; to make the rule itself state the requirement, end it
with the [anchor](parser-expressions.md#anchors) `$$`.

## Results

A parse has one of the following outcomes.

| Outcome | What the parse returns |
|:--|:--|
| Success | The value of the start rule, which is a [node](#nodes) or `nil` |
| Success with recovered errors | The value, together with the list of [syntax errors](#syntax-errors) that [`#recover`](attributes.md#recover) recovered from |
| Syntax error | No value; the [syntax error](#syntax-errors). If errors were recovered before, only the final syntax error is returned |
| Run-time error | No value; the error of an [action](actions.md#run-time-errors) or a [limit](#limits) that was exceeded. It aborts the whole parse |

The value of the start rule is nil if the rule has no value. For example, a
rule `def main = -"a"` or `def main = "a"?` on empty input succeeds with `nil`.

### Nodes

A **node** has the following properties.

| Property | Meaning |
|:--|:--|
| Type | The type name: a [reserved node type](types.md#reserved-node-types) (`Match`, `Seq`, `List`, `Operator`, `Error`) for the nodes of the concrete syntax tree, or the name of a type defined in the grammar |
| Rule | The name of the rule that produced the node, if it is a node that a rule without an action created in its own body (see [Values and the concrete syntax tree](parser-expressions.md#values-and-the-concrete-syntax-tree)); otherwise empty |
| Range | `[start, end)` in the position unit |
| Text | For a terminal, the matched text |
| Children | For a `Seq`, `List` or `Operator`, the child nodes; a child is `nil` where an optional expression did not match |
| Fields | The captures of a `Seq` or of a node that a rule produced, and the fields of a struct node. Each value is a node, an integer, a string, a boolean or `nil` |

The Go type `pego.Node` and its JSON form are described in
[The Node type](../docs/guide/runtime.md#the-node-type).

## Syntax errors

When the input does not match the grammar, the parser reports the **farthest
position** that parsing reached, together with the items that were expected at
that position: literals, character classes, `any character` (for `.`) and
anchors (`beginning of input`, `end of input`, `beginning of line`,
`end of line`).

```
1:5: syntax error: expected "(", "-", (?0-9)
```

- The position is given as a 1-based line and column. The column is counted in
  the [position unit](#positions) (code points by default).
- The expected items are listed in sorted order, without duplicates.
- Failures inside [lookahead](parser-expressions.md#lookahead) and inside the
  [`skip` of a Pratt expression](pratt.md#skip) are not included. Predicates
  and bottom (`_|_`) contribute no expected items.
- If the start rule matches but input remains, `end of input` is expected at the
  position where the start rule stopped (see [Whole input](#whole-input)).
- If nothing was expected at the farthest position, the message is just
  `syntax error`.
- A [`#error`](attributes.md#error) attribute replaces the expected items inside
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
  [Operators](actions.md#operators)).
- **Rules that read variables are memoized per combination of values.** The
  result of a rule that reads [variables](predicates.md#variables) directly or
  through the rules it calls depends on their values when it is called, so a
  memoized result is reused only if the variables have the same values; see
  [Predicates and Variables](predicates.md#interaction-with-memoization).
- **Actions run when a rule is evaluated.** An action is evaluated every time
  the body of its rule is evaluated and matches, even if the enclosing
  alternative later fails, and also inside a lookahead, under `@` and under `-`.
  An action has no effect other than its value and its [run-time
  errors](actions.md#run-time-errors), so how many times it runs is not
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
operator and the right operand of an infix operator of a Pratt expression nest
like rule calls; chains of left-associative operators do not nest.

## Modes

The same grammar can be run in several modes. Except where stated, each mode
gives the results that an ordinary parse gives.

| Mode | Difference from an ordinary parse |
|:--|:--|
| Backends | The reference implementation has three backends (closure, recursive bytecode and iterative bytecode). They give identical values, errors and positions, and differ in speed and in the limit of nesting depth. Generated Go and TypeScript parsers implement the same semantics; their differences, such as the nesting limit, are described in the guides |
| Recognition | The parse only tells whether the input matches. It gives the same success or failure and the same syntax errors, builds no tree, and does not evaluate actions, so run-time errors of actions are not reported. The captures that [predicates](predicates.md) read are still built |
| Stream | A parse that reads its input incrementally and hands each element of the [`#stream`](attributes.md#stream) repetition of the start rule to the caller as it matches. The elements are not part of the value |
| Incremental | A document that is edited and reparsed reuses results of the previous parse. The result of a parse is the same as that of a parse of the whole new text |

The guides describe these modes from the user's side: see
[Running parsers](../docs/guide/runtime.md) and
[Streaming and incremental parsing](../docs/guide/streaming-and-incremental.md).
