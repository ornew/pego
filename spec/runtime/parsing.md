# Parsing

The preceding chapters define what a grammar means rule by rule. This chapter
defines a parse as a whole: how it starts, what the input and its positions are,
what a parse produces, how a syntax error is determined and reported, what
memoization can and cannot change, what a parse costs in time and memory, and
the limits and modes of a parse.

## Start Rule

A parse starts at a rule, the **start rule** of the parse. It MUST be one of the
start rules of the grammar, the rules marked `@start` (see
[Start Rules](../language/grammar-files.md#start-rules)). When no rule is named, the parse
uses the default start rule of the grammar. On every interface that takes the
name of a start rule, an empty name selects the default start rule of the
grammar; it does not select the start rule a parser was compiled or generated
for.

| Interface | How the start rule is selected |
|:--|:--|
| Go API | The `start` argument of `pego.Compile` and `pego.CompileSource`, where an empty name selects the default start rule; `Parser.WithStart` makes a parser with another start rule for whole-input parsing; `Parser.ParsePrefix` selects a start rule for one prefix parse without changing the configured start rule. Both take an empty name for the default start rule |
| Command line | `pego parse -s <rule>`. It defaults to the start rule saved in a compiled grammar (`.pegoc`), and otherwise to the default start rule |
| Generated parsers | `Parse` uses the start rule selected when the parser was generated, the default start rule unless another was named; `ParseRule` and `ParseRuleAST` (`parseRule` and `parseRuleWithOptions` in TypeScript) take the name of a start rule, or an empty name for the default start rule |

Naming a rule that is not a start rule is an error that names the rule and says
it is not a start rule; naming an undefined rule is an error that says it is not
defined. The start rule is called at the beginning of the input
with no binding level, so an Operators Rule in it parses the operators of all
levels (see [Level-restricted calls](../language/operators-rule.md#level-restricted-calls)).

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
[Line terminators](../language/lexical.md#line-terminators).)

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
[anchor](../language/parser-expressions.md#anchors) `$$` inside that rule still requires the
actual end of the supplied input. The API does not change the parser's
configured start rule or shift positions to an enclosing document.

## Results

A parse has one of the following outcomes.

| Outcome | What the parse returns |
|:--|:--|
| Success | The value of the start rule, which is a [node](#nodes) or `nil` |
| Success with recovered errors | The value, together with the list of [syntax errors](#syntax-errors) that [`#recover`](../language/pragmas.md#recover) recovered from |
| Syntax error | No value; the [syntax error](#syntax-errors). If errors were recovered before, only the final syntax error is returned |
| Run-time error | No value; the error of an [action](../language/actions.md#types-and-errors) or a [limit](#limits) that was exceeded. It aborts the whole parse |

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
limits, context and budget limits. Stream diagnostic options are rejected, as for
`Parse`. A grammar that contains `#missing` is rejected, even if insertion is not
enabled; a positive `WithMissingTokens` limit is rejected for every grammar. Zero
keeps strict prefix matching. Options resolve in order. An explicit `$$` sees the
actual end of the entire supplied string, so a fragment is confined by slicing
both its beginning and its end. Cuts and left recursion keep their ordinary
meaning. Prefix parsing shares no memo entries with a `Document`, does not
shift positions to absolute coordinates and does not map escaped or noncontiguous
fragments. It can run concurrently on one parser. A compiled grammar supports it
on the bytecode backends in the mode that serves the parse: a value parse in parse
mode, recognition in recognize mode and a traced parse in document mode (see
[Compiled Grammars](compiled-grammars.md#operations-of-a-loaded-parser)).

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
- Failures inside [lookahead](../language/parser-expressions.md#lookahead) and inside the
  [`skip` of an Operators Rule](../language/operators-rule.md#skip) are not included. Predicates
  and bottom (`_|_`) contribute no expected items.
- If the start rule matches but input remains, `end of input` is expected at the
  position where the start rule stopped (see [Whole input](#whole-input)).
- If nothing was expected at the farthest position, the message is just
  `syntax error`.
- A [`@error`](../language/attributes.md#error) attribute replaces the expected items inside
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
  [Operators](../language/actions.md#operators-and-functions)).
- **Rules that read variables are memoized per combination of values.** The
  result of a rule that reads [variables](../language/predicates.md#variables) directly or
  through the rules it calls depends on their values when it is called, so a
  memoized result is reused only if the variables have the same values; see
  [Predicates and Variables](../language/predicates.md#interaction-with-memoization).
- **Actions run when a rule is evaluated.** An action is evaluated every time
  the body of its rule is evaluated and matches, even if the enclosing
  alternative later fails, and also inside a lookahead, under `#atomic` and under `-`.
  An action has no effect other than its value and its [run-time
  errors](../language/actions.md#types-and-errors), so how many times it runs is not
  observable, but whether it runs is: an error in an action aborts the parse
  even in a branch that is abandoned.
- **Left recursion** is evaluated by growing a seed, which relies on the memo
  (see [Left recursion](../language/parser-expressions.md#left-recursion)).
  While a rule grows at a position, every call of it there reads the growing
  result, whatever the values of the variables it reads.
- **The nesting limit can depend on it.** A call that is answered from the memo
  does not nest, so whether a parse that comes close to the [nesting
  limit](#limits) succeeds can depend on memoization.

## Time and Memory

The cost of a parse depends on the grammar as well as on the length n of the
input. Memoization bounds how often a rule body is evaluated at a position, but
some expressions have a cost that the memo does not share. This section defines how
that cost is measured, what holds for every grammar, and the classes of growth
that a grammar can have.

### Cost

The **cost** of a parse is the sum of the following:

- the rule bodies that are evaluated, counting each step of a growing
  left-recursive rule;
- the input that terminals, repetitions of character classes and lookaheads
  examine, counted again each time it is examined;
- the iterations of repetitions and the operators that an Operators Rule
  applies;
- the size of the values that actions and predicates build.

Time is proportional to cost. The memory that a parse allocates is at most
proportional to its cost plus the input, because each memo entry, node and
value is built by cost that is counted. The cost of a parse is measured in
budget units, which differ between backends, and the budget of
[budget limits](#cancellation-and-budget-limits) caps it.

### Termination

A parse of a finite input terminates, unless a reader or a callback that the
caller supplies does not return.

- A call of a left-recursive rule at a position where that rule is growing
  reads its current result instead of evaluating its body, whatever the values
  of variables (see [Left Recursion](../language/parser-expressions.md#left-recursion)).
  A rule therefore cannot call itself at the same position without end.
- A repetition stops at the first iteration that consumes no input, an infix
  operator applied without consuming input ends its Operators Rule expression,
  and a recovery applies only when its skip expression consumes input.
- Actions have no recursion: a lambda is only an argument of a built-in
  function, which visits a list once.

No bound that is a polynomial in n holds for every grammar.

### Evaluations of Rules

In the reference implementation and the generated parsers, a rule body is
evaluated at most twice at a position for each binding level and each
combination of the values of the variables that the rule reads, with these
exceptions.

- A rule that calls no rule, itself included, or that is called from at most
  one place in the grammar, its own body included, is not memoized in an
  ordinary parse. It is evaluated as often as its calls are evaluated. A
  call with a level restriction, `level(e, name)`, is a call of `e`; the
  parse of a right operand inside an Operators Rule is not a call.
- A rule of a left-recursion cycle is not memoized, except its leaders, the
  rules that grow (see [Conditions for Linear Cost](#conditions-for-linear-cost)).
  It is evaluated at every call, and again at every step of the growth.
- A growing rule evaluates its body once for each step of the growth, and the
  number of steps is at most two more than the length of the input it
  consumes.

### Expressions That Repeat Cost

The memo shares the result of a rule at a position. It does not share cost
inside a rule body, nor between results at different positions. The following
expressions have a cost that grows with the input they cover, and repeat it when
they are evaluated again over the same input.

| Expression | Cost of one evaluation | Repeated when |
|:--|:--|:--|
| A repetition | Its iterations, up to the input it matches | The body that contains it is evaluated again at an earlier position, so the repetition covers the same input again |
| A lookahead | The cost of its expression | It is evaluated at each of many positions and its expression examines the input after them |
| `#recover(skip=e)` | The cost of the base expression and of `e` | Its result is abandoned and parsing continues inside the skipped input |
| A growing left-recursive rule | One evaluation of its body per growth step | It is called at many positions of one input, because its result at one position is not built from its result at the next |
| An Operators Rule | One step per operator it applies | As for left recursion |
| A rule that reads variables | As for any rule, once for each combination of values | Calls at one position bind its variables to many different values |
| An action or predicate | Proportional to the sizes of the values it reads and builds | A value grows from its own earlier value, as `s + s` does |

### Classes of Growth

A grammar's cost as a function of n falls into one of these classes. A class
describes the worst input of each length.

| Class | Cost | Example grammar | Input |
|:--|:--|:--|:--|
| Linear | At most proportional to n | `main = (A / "a")* $$; A = "a" "c"` | `a` repeated n times |
| Linear given an environment bound | Proportional to n times the number of combinations of variable values that occur at one position and the size of the strings they hold | A rule that reads `indent`, bound from the length of a captured run of spaces | Lines of increasing indentation |
| Polynomial of degree k | Proportional to n^k on its worst inputs | `main = (A / "a")* $$; A = "a"* "c"` (k = 2) | `a` repeated n times |
| Exponential | Can double when n grows by a constant | `main = C $$; A = C "+" / "n"; C = "(" C ")" "y" / "(" C ")" "z" / A` | `(` n times, `n`, then `)z` n times |

The following grammars have the class shown on the inputs shown.

| Grammar | Input | Class | Cause |
|:--|:--|:--|:--|
| `main = (E "q" / "x")* $$; E = "x" E / "x"` | `x` n times | Linear | The result of `E` at a position is built from its memoized result at the next |
| `main = (E "q" / "x")* $$; E = E "x" / "x"` | `x` n times | Degree 2 | `E` grows to the end of the input at every position, and `"q"` then fails |
| `main = (E "q" / "x")* $$` with `E` an Operators Rule with the operand `"x"` and the infix operator `""` at one left-associative level | `x` n times | Degree 2 | As above, for the operators that `E` applies |
| `main = ("a" !Rest)* "a"* $$; Rest = "a"* "c"` | `a` n times | Degree 2 | The lookahead scans to the end of the input at every position |
| `main = (X / "a")* $$; X = (Y #recover(skip=[a]+)) "c"; Y = "b"` | `a` n times | Degree 2 | The skip consumes the rest of the input, `"c"` then fails, and the recovery is abandoned |
| `main = (A / "a")* $$; A = (B / "a")* "c"; B = "a"* "d"` | `a` n times | Degree 3 | A repetition that can restart inside another one that can restart |
| `main = C $$; A = C "+" / "n"; C = "(" C ")" "y" / "(" C ")" "z" / A` | `(` n times, `n`, `)z` n times | Exponential | `A` and `C` form a left-recursion cycle grown at `A`, so `C` is not memoized and each `C` evaluates the next one twice |
| `var v string`; `R = "(" {v := v + "0"} R ")" "y" / "(" {v := v + "1"} R ")" "z" / "n" {len(v) >= 0}` | As above | Exponential | The two calls of `R` at a position see different values of `v`, so neither reuses the other's result |
| `main = xs:"x"* => len(foldl("a", $xs, (acc, x) => acc + acc))` | `x` n times | Linear evaluations, exponential value size | The accumulator doubles for each element |

The same grammars with `A = "n"`, or with the bindings `{v := "0"}` and
`{v := "1"}`, are linear. The [classification](#classification) reports the
first exponential grammar as unknown, not exponential: its cause is a growth
restarted inside a recursion of rule calls, which the classifier does not
bound.

### Conditions for Linear Cost

The conditions below use these terms.

- A **copying operation** is an operation of an action, predicate or binding
  whose cost is charged in proportion to the size of what it builds: joining
  strings, taking the text of a capture, converting a value to a string, and
  building a list with `list`, `concat`, `map` or `filter`. The size of a
  string is its number of characters, and the size of a list its number of
  elements; the elements themselves are shared, not copied.
- A **left call** is a call of a rule of a left-recursion cycle, in the body
  of a rule of the same cycle, that can run before that body consumes input.
  The rules that grow are the **leaders**, fixed by the grammar: until every
  cycle of left calls passes through a leader, the rule declared first on a
  cycle that passes through none becomes one, so a cycle can have several. A
  call of a leader grows it at the position of the call, and a left call of a
  leader at that position reads the seed that the growth has reached. A call
  of another rule of the cycle evaluates its body without the memo, and its
  left calls reach a leader, which grows there. Condition 3 covers what each
  step of the growth evaluates again, and the first step of a growing unit and
  condition 3 are checked with the leaders as the rules that grow.
- A **scanning source** is one of the following: a repetition without an upper
  bound, the skip expression of a `#recover`, the growth of a left-recursion
  cycle at every call of a rule of the cycle other than a left call, the
  operators that an Operators Rule applies, and a copying operation with an
  operand, other than the value of a variable, that is not bounded in size by
  a constant of the grammar. A call of a rule of the cycle after input has
  been consumed in a body of the cycle, such as the second `E` in
  `E = E "+" (E "q" / "x") / "x"`, starts a growth of its own at a new
  position. In the same way, the right operand of an operator part is parsed
  at a new position and can apply operators itself (see
  [Parsing Rules](../language/operators-rule.md#parsing-rules)), so a unit of
  an Operators Rule contains a scanning source when its right operand can
  apply an operator. Copying the
  value of a variable costs at most L, the bound of
  [Classification](#classification), in each evaluation, which condition 4
  counts instead. An expression **contains** a scanning source when the
  source is part of it, or of the body of a rule that it calls, directly or
  indirectly.
- A **unit** is an expression after whose failure the parser continues at the
  position where the unit started: an alternative of a choice other than the
  last, one iteration of a repetition, the expression of an option, the
  expression of a lookahead, which is abandoned whether it matches or not, the
  base of a `#recover`, and, in an Operators Rule, each operator part together
  with its right operand and each `operand` item other than the last. A unit
  fails when something inside it fails, including a later part of its own
  sequence and a rule that it calls.
- A **growing unit** is a unit whose evaluation begins with a left call: the
  left call is the first expression that it evaluates, directly or through
  calls of rules of the cycle, such as the alternative `E "+" T` in
  `E = E "+" T / T`. A unit that evaluates anything before its left call, such
  as `("y")? E S "q"`, is not a growing unit. In each step of a growth after
  the first, the left call reads the seed, and the unit examines the input
  from the end of the seed. In the first step, the left call fails before the
  unit examines any input when the unit reaches the leader only through
  rules of the cycle that cannot succeed without their own left call, as
  `add` cannot in `expr = add / term; add = expr "+" term`. Otherwise, as
  through `F` in `E = F "+" X / Y; F = E / M`, the first step can match `M`
  and continue the unit after it, and the unit is also checked as an
  alternative that is not growing: from its start, against the later
  alternatives of its choice.
- The **continuations** of a unit are the expressions that the parser can
  evaluate next at the position where the unit started:
  - for a growing unit, the growing units that the same step can evaluate
    after it, which also start at the end of the seed, and what follows the
    calls that start the growth, at the end of the seed. What the step
    evaluates after the unit fails runs at the position where the rule grows,
    and condition 3 covers its cost. When the left call fails in the first
    step, that evaluation is the one of the first step after the same failure,
    except that later growing units read the seed, so unless one of them
    succeeds, the step ends where the first step ended, at the end of the
    seed. Otherwise the check as an alternative that is not growing keeps the
    later alternatives from consuming the character at the position where the
    rule grows, so the step again ends no farther than the seed. Either way
    the growth ends with the seed and the parser continues at its end;
  - for any other alternative, the later alternatives of the choice, unless
    every way in which the alternative can fail lies after a `#cut` in it;
  - for an iteration, an option or a lookahead, what follows the repetition,
    option or lookahead, through the ends of the enclosing expressions and, at
    the end of a rule body, through every call of the rule;
  - for the base of a `#recover`, its skip expression;
  - for a unit of an Operators Rule, the other operator parts and `operand`
    items that the rule can try at the same position, and what follows the
    Operators Rule expression.
- The **first characters** of an expression are the characters with which an
  evaluation of it can examine the input beyond its start position: those it
  can consume first, and those that every lookahead it can evaluate before it
  consumes input can consume first, through rule calls. The first characters
  of the continuations include, where a continuation can match without
  consuming input, the first characters of what follows it. The first
  characters of a growing unit, compared with its own continuations, and of a
  growing unit that is a continuation of another growing unit, are those of
  the part after its left call. As a continuation of a unit that is not
  growing, a growing unit examines the input from that unit's start, through
  the seed.
- A unit and a continuation **share** a call when, at the position where the
  unit starts examining the input, both evaluate the same call before
  anything else that examines the input: a call of the same rule with the
  same level restriction, or with none in both, of a rule that reads no
  variable and that is memoized or calls no rule, itself included. The call
  has the same result in both: the memo answers it the second time, or the
  rule repeats only its own scan, so the continuation pays for it at most what
  the unit paid. Calls of one rule with different level restrictions, such as
  `e` and `level(e, hi)`, are different calls with different results. A call
  of any other rule is not shared, because evaluating its body again also
  evaluates again the units inside it, at every level of a recursion.
- A unit is **committing** when, for each of its continuations, the first
  characters of the unit and of the continuation are disjoint after the calls
  that the two share are removed from their starts, one after another, each
  at the end of the one before. After
  a committing unit is abandoned, nothing that the parser evaluates next
  consumes the character at which the unit and the continuation part, so
  nothing starts again inside the input that the unit examined after its
  shared calls, and the shared calls are not repeated beyond their own cost.

A grammar's cost from a start rule is at most proportional to n, times a
constant that depends only on the grammar, when every rule that the start rule
reaches satisfies all of the following. The conditions are sufficient, not
necessary.

1. **Variables.** No binding of a variable reads, through any chain of
   bindings, captures and rule values, the variable's own earlier value, when
   the value it binds can reach a later evaluation of the same binding: when a
   sequence of the rule body has the binding in one element and a call that
   can reach its rule again in a later element, each at any depth of groups,
   options and choices, or when the binding is part of the element of a
   repetition, outside a negative lookahead. A binding and a call in
   different alternatives of one choice are not on one path of evaluation,
   because a failed alternative undoes its bindings before the next is tried.
   A binding after the recursive call binds a value that no later evaluation
   of it sees, because bindings in a called rule are not visible to its
   caller. When every binding is a literal, a constant number of combinations
   of values occurs at a position. When a binding reads a capture or a
   variable, the cost is linear given an environment bound.
2. **Repeated scans.** Every unit that contains a scanning source is
   committing.
3. **Growth.** No scanning source can be evaluated, outside the left call that
   grows, at the position where a left-recursive rule grows, in its body or in
   the body of another rule of its cycle, other than inside a call of a
   memoized rule that is not on the cycle. Each step of the growth evaluates
   these bodies again at that position.
4. **Copies.** No copying operation copies a string or list that can contain a
   capture or a value that contains a call on a recursion with the rule that
   performs the operation, nor a fold accumulator. A copying operation in the
   lambda of `map`, `filter`, `foldl` or `foldr` runs once for each element, as
   in a repetition: it copies nothing from outside the lambda that is not
   bounded in size by a constant of the grammar. A copying operation that
   copies the value of a variable is linear given an environment bound.

### Classification

`pego lint` classifies the cost of the start rule that it checks. The
classes are ordered from best to worst: each class is a weaker claim than the
classes before it. The classes linear and linear given an environment bound
produce no finding. The classes polynomial and exponential produce a
[`super-linear`](lint.md#super-linear) hint at each cause, with its witness.
The class unknown produces no finding; it is shown, with its reason, only when
the classification is requested.

| Class | Promise |
|:--|:--|
| Linear | Every parse from the start rule has a cost of at most c · (n + 1) |
| Linear given an environment bound | Every parse has a cost of at most c · K · (L + 1) · (n + 1) |
| Polynomial of degree k | None; an estimate |
| Exponential | None; an estimate |
| Unknown | None; the classifier cannot bound the cost |

The constant c depends only on the grammar and on the backend. K is the largest
number of distinct combinations of the values of the variables that memoized
rules read, among the calls at one position of the parse. L is the largest size
of a variable's value that a copying operation copies or that a memo lookup
compares; it is zero when no variable holds a string. Under the conditions, a
variable's value is built from literals, captures and other variables without
feeding back into itself, so L is at most proportional to n, and the cost of
the second class is at most proportional to K · n². A caller that needs a cost
proportional to n needs the class Linear.

The first two classes are guarantees, under the memoization of the
implementation that runs the parse: the classifier reports them only when the
conditions of [Conditions for Linear Cost](#conditions-for-linear-cost) hold,
and it over-approximates where it cannot be exact. It assumes that every
predicate can both succeed and fail, that every continuation is reachable, and
that first characters do not depend on the path that reaches an expression.

The other classes carry no promise. Deciding the cost of a grammar exactly is
impossible, because it would decide whether a grammar's language is empty, so
a class worse than linear can be pessimistic: no slow input need exist. The
classes polynomial and exponential come with a **witness**, a path of rule
calls and expressions that shows the cause, and the class unknown comes with a
reason:

- **Polynomial.** A scanning source in a unit that is not committing, the
  continuation that can start again inside it, and the enclosing repetition or
  recursion. The estimated degree is one more than the number of such sources
  nested in one another through rule calls, so that a repetition restarted
  inside a restarted repetition gives 3. A copying operation that copies a
  capture or value containing a call on its own recursion, or a fold
  accumulator, gives 2, and so does a scanning source evaluated in each step of
  a growth.
- **Exponential.** A binding that makes a variable depend on its own earlier
  value, or a copying operation that copies a value of its own recursion, or a
  fold accumulator, more than once, as `s + s` does.
- **Unknown.** A condition for linear cost fails without a witness of the
  classes above, as when sources that are not committing are nested in one
  another without bound through a recursion of rule calls. Memoized calls in
  the recursion can keep such a grammar linear, which the classifier does not
  decide. Its reason is the first unit that is not committing, or the
  condition that fails, with the expression where it fails.

### Memory

- **Memo table.** It holds at most one entry per memoized rule, position,
  binding level and combination of variable values.
- **Values.** A parse keeps the value of its start rule. Values built by
  abandoned alternatives are not kept, but their construction is cost.
- **Call stack.** It holds at most as many calls as the [nesting limit](#limits)
  allows.
- **Streams and Documents.** A [stream](streaming.md) parse discards memo entries
  and input before the elements that it has delivered. A
  [Document](documents.md) keeps memo entries and repetition records for its
  whole text between parses.

### Bounding Cost

[Budget limits](#cancellation-and-budget-limits) bound the cost of any parse,
whatever its class. `Budget` is one counter for all of it: it counts the input
that terminals, repetitions of character classes and lookaheads examine,
including input examined again, rule calls, repetition iterations, growth
steps, and the strings and lists that actions and predicates build, in
proportion to their size. Each unit adds one to the counter, so a loop that
only scans input, a deep recursion and a value that grows without reading the
input are all bounded by it.

## Limits

An implementation MAY limit the nesting depth of recursion, so that input of
unbounded nesting cannot exhaust the memory of the host. A rule is
**recursive** if it is on a cycle of the call graph: it can reach itself through
rule references, directly or through other rules (a rule that references only
itself is on a cycle). The nesting depth is the number of entries of recursive
rules that are active at once, plus the entries of Operators Rules (below),
whether or not they are recursive; the entry of any other rule does not count.
A rule outside every cycle appears at most once on the call stack of a parse, so
the rules that do not count add a bounded depth that depends only on the
grammar. The value-free twin of a rule is as recursive as the rule.

The operand of a prefix operator and the right operand of an infix operator of
an Operators Rule nest like entries of recursive rules, and so does the entry of
an Operators Rule; chains of left-associative operators do not nest. When the
limit is exceeded, the parse fails with a run-time error and no value. In the
reference implementation the error is `nesting too deep: more than N nested
recursive rule calls`; the default limit is 100,000 (10,000,000 for the iterative
bytecode VM), and the Go API changes it with `pego.WithMaxDepth`. Every backend
reaches the limit at the same nesting of the same input.

## Cancellation and Budget Limits

The reference Go API accepts invocation-local cancellation and a budget limit
through `WithContext` and `WithLimits`; generated Go exposes corresponding
package-local options.
These controls are cooperative and backend-local. A zero limit selects the
[default of that limit](#default-limits) and the value `Unlimited` disables
it. The budget counter is monotonic for one invocation and resets on the next
call. A reservation is checked before the corresponding work begins; if it
exceeds the remaining allowance, that work is not performed. Decoding is the
one exception: a reservation may follow the decode of at most one character (at
most 4 bytes) and must precede storing or using that character. Counts are
implementation-specific and are not elapsed-time, memory, or buffered-input
limits.

### Default Limits

`Limits` has two fields, and a zero value selects the default of its field:

| Field | Bounds | Default |
| --- | --- | --- |
| `Budget` | the cost of the parse | `max(2^20, 1024 * n)` |
| `MaxInput` | the length of the input | `Unlimited` |

Here `n` is the length of the input in the position unit of the parse: bytes
for `Bytes`, code points for `CodePoints`. `MaxInput` has no finite default
because the default `Budget` already grows with `n`.

Every parse has a budget. A
parse that has neither a limit nor a context therefore runs controlled. The
value `Unlimited` (the largest `uint64` in Go, `Number.MAX_SAFE_INTEGER` in
TypeScript) disables the budget; with no context either, the parse runs
without a control. `LimitError.Limit` reports the resolved limit, and the
default is `Unlimited - 1` at most. One constant disables every field of
`Limits`, and a field added to `Limits` takes the same zero and `Unlimited`
meaning.

`MaxInput` bounds the length of the input in the position unit. A batch parse,
a recognition and the creation of a Document compare the length of the input
with it before any parsing work; a Document does so again at every `Parse`, for
the text it holds after its edits. An input of exactly `MaxInput` units is
accepted. A longer input fails with a `LimitError` of kind `LimitInput`: `Limit`
is `MaxInput`, `Used` is zero and `Requested` is the length of the input. The
check costs O(1), except that `CodePoints` counts the code points of an input
whose byte length passes the limit. It needs no budget and no context: a
finite `MaxInput` alone does not make a parse controlled. It is checked before
a context is consulted, so an input over the limit fails with `LimitInput` even
if the context is already canceled. A parser that has no budget controls
accepts only a zero `MaxInput` and `Unlimited`.

A stream does not know its length. With a finite `MaxInput`, it fails when the
parser asks for a unit beyond the limit and the stream has one, and `Requested`
is the number of units the parser asked for, or the length of the stream if that
is shorter. A stream of exactly `MaxInput` units completes. As with the default
budget, the outcome does not depend on how the reader splits the input into
reads. With `MaxInput` at its default, a stream tracks no length.

The default is resolved once per entry point, from the input the entry point
receives. For a `Document`, `n` is the length of the text at each `Parse`. A
stream does not know `n` up front, so its default is `max(2^20, 1024 * d)`,
where `d` is the number of units up to the furthest position the parser has
demanded from the stream so far, and it never shrinks. `d` depends on the input
and the grammar, not on how the reader splits the input into reads. A parser generated
without budget controls spends no budget and accepts only a zero limit and
`Unlimited`.

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
`WithLimits(Limits{Budget})` the limit. The budget counter adds the
inspected elements of the input representation and the backend's execution
steps, one unit each. Charges are not rolled back by backtracking, and their values
depend on the backend, the position unit and the generated artifact. For a given
artifact, input and options they are the same on every run: the storage that an
earlier parse left in a reused parser, and when a collection releases it, never
change them. A stream also charges one unit for each call to its reader's
`Read`, so its spend depends on how the reader splits the input as well; its
default limit does not. Reading
input charges the budget counter in proportion to the elements examined, so a
loop that only scans input, such as a repeated character class, is bounded by
`Budget`. A cancelled
context returns the error of the context (`errors.Is(err, context.Canceled)` or
`context.DeadlineExceeded`); an exhausted limit returns an error that matches
`pego.ErrLimit`, and `errors.As` with `*pego.LimitError` gives the
details. Stream parses and documents are specified in [Streaming](streaming.md)
and [Incremental Documents](documents.md).

## Modes

The same grammar can be run in several modes. Except where stated, each mode
gives the results that an ordinary parse gives.

| Mode | Difference from an ordinary parse |
|:--|:--|
| Backends | The reference implementation has three backends (closure, recursive bytecode and iterative bytecode). They give identical values, errors and positions, and differ in speed and in the limit of nesting depth. Generated Go and TypeScript parsers implement the same matching semantics; the entry points, features and limits that differ are defined in [Generated Parsers](generated-parsers.md) |
| Recognition | The parse only tells whether the input matches. It gives the same success or failure and the same syntax errors, builds no tree, and skips actions whose values are not needed by predicates. Actions needed by predicates still run and can report run-time errors. The captures that [predicates](../language/predicates.md) read are still built |
| Stream | A parse that reads its input incrementally and hands each element of the [`#stream`](../language/pragmas.md#stream) repetition of the start rule to the caller as it matches. The elements are not part of the value. See [Streaming](streaming.md). Generated TypeScript parsers have no stream mode |
| Incremental | A document that is edited and reparsed reuses results of the previous parse. The result of a parse is the same as that of a parse of the whole new text. See [Incremental Documents](documents.md). Generated TypeScript parsers have no document, and generated Go parsers support a subset |
| Prefix | A named rule is parsed at the beginning of the input without requiring the whole input to be consumed. See [Prefix Parsing](#prefix-parsing) |
