# Rule Expressions

A Rule Expression is the right-hand side of a Rule Definition Statement. It
matches a prefix of the input at the current position and either succeeds,
possibly consuming input and producing a value, or fails.

## Evaluation

A Rule Expression is evaluated at a position of the input. If it succeeds,
the evaluation produces an end position, which is the position where the
next expression of a sequence continues, and a value (or no value). If it
fails, it has no effect: the position, the captures and the
[variables](predicates.md#variables) are as they were before it. The following
holds for every expression:

- **Determinism.** The result depends only on the grammar, the start rule, the
  input and the position unit; there is no ambiguity.
- **Order.** The elements of a sequence are evaluated from left to right, and
  the alternatives of a choice from left to right.
- **No backtracking into a success.** Once an expression has succeeded, it is
  not evaluated again with a different result to make a later expression
  succeed: a choice commits to the first alternative that succeeds, and a
  repetition takes as many iterations as it can. This is the behavior of PEG,
  as opposed to context-free grammars.
- **Recorded failures.** A failure records what was expected at its position.
  The farthest failure of a parse is reported as a
  [syntax error](parsing.md#syntax-errors).

## Values and the Concrete Syntax Tree

PEGO produces a predictable and consistent concrete syntax tree (CST). When a
Rule Expression succeeds it produces a value, normally a node. A rule without
an action produces the value of its body.

| Expression | Value |
|:--|:--|
| Literal, `[...]`, `.`, `a #atomic`, `_` | A terminal: a `Match` node holding the matched text |
| `a b` | A `Seq` node whose children are the values of the elements that have a value |
| `a / b` | The value of the alternative that matched; the choice itself creates no node |
| `a*`, `a+`, `a #repeat(n,m)` | A `List` node whose children are the values of the iterations |
| `a?` | The value of `a`, or `nil` if `a` did not match |
| `label:a` | The value of `a` |
| Rule call | The value of the called rule |

The following rules determine the shape of the tree.

1. **One value per rule.** A rule produces a single value, whose kind is
   determined by the top-level expression of the rule body:
   - `rule a = "x" "y"` produces a `Seq` node.
   - `rule b = "x" / "y" "z"` produces the value of the alternative that matched
     (a `Match` node or a `Seq` node).
   - `rule c = "x"` produces a `Match` node.
   - `rule d = a` produces the value of rule `a` unchanged.
2. **Rule names.** A node that a rule without an action creates **in its own
   body** is labeled with the name of that rule. Nodes received from other rules
   keep their own label: `rule d = a` produces the node of `a`, labeled `a`. The
   value of an action is final in the same way: if an action returns a node of
   its rule's body (`x:a => $x`), that node is not labeled by any rule.
3. **Expressions without a value.** The following expressions have no value and
   never appear as children of a `Seq` node:
   - anchors: `^^`, `$$`, `^`, `$`;
   - cut (`#cut`), discard (`-a`), bottom (`_|_`), lookahead (`&a`, `!a`) and
     [predicates](predicates.md).

   An optional expression `a?` that does not match does have a value, `nil`,
   which appears as a child.
4. **Suppressed construction.** Atomic (`a #atomic`) and discard (`-a`) do not build
   the values of their operands, which saves work and keeps the tree small.
5. **Whole input.** A parse succeeds only if the [start rule](parsing.md#start-rule)
   matches the entire input; see [Parsing](parsing.md#whole-input).

## Operator Summary

| Syntax | Name | Consumes input |
|:--|:--|:--|
| `"..."` | literal | yes |
| `[...]`, `[^...]` | character matching | yes |
| `.` | any character | yes |
| `name`, `level(name, binding)` | [rule call](#rule-calls) | depends on the rule |
| `_` | top | no |
| `_\|_` | bottom | no |
| `^^` / `$$` | beginning / end of input | no |
| `^` / `$` | beginning / end of line | no |
| `a b` | sequence | yes |
| `a / b` | ordered choice | yes |
| `(a)` | grouping | yes |
| `a #repeat(n)`, `a #repeat(n,m)`, `a*`, `a+` | repetition | yes |
| `a?` | optional | yes |
| `&a` / `!a` | positive / negative lookahead | no |
| `a #atomic` | atomic | yes |
| `-a` | discard | yes |
| `#cut` | cut | no |
| `label:a` | capture | yes |
| `{ term }` | [Predicate Action](predicates.md) | no |
| `@name(...)` | diagnostic/metadata [attribute](attributes.md) | depends on the attribute |
| `#atomic`, `#repeat(...)`, `#recover(...)`, `#missing`, `#stream` | behavioral/structural pragma | depends on the pragma |

"Consumes input" states whether the expression can advance the input position.

### Precedence

Operators bind in the following order, from tightest to loosest. Parentheses
override the order.

| Precedence | Operators | Kind |
|:--|:--|:--|
| 1 (tightest) | `*`, `+`, `?`, `#repeat(...)`, `#atomic`, `#recover(...)`, `#missing`, `#stream`, `@name(...)` | postfix |
| 2 | `label:`, `&`, `!`, `-` | prefix |
| 3 | `a b` | sequence |
| 4 (loosest) | `a / b` | ordered choice |

For example, `[0-9]+ #atomic` applies atomic construction suppression to the repeated character class,
`x:a*` captures the repeated expression, and `"a" "b" / "c"` is `("a" "b") / "c"`.
Postfix operators are applied from left to right and can be stacked (`a*?`).
For example, `a* #atomic` creates one terminal for the whole repetition, while
`(a #atomic)*` creates a list of terminals. Prefix operators can also be stacked
(`!&a`). Parentheses group an expression when an attribute must apply to a
larger expression.

## Terminals

### Literals

A literal `"..."` matches the given decoded character sequence and produces a terminal.
For public grammar ASTs, malformed UTF-8 bytes in `Literal.Value` each decode as U+FFFD;
comparison uses the same sequence in both position units. This does not rewrite the AST
or change string constants in actions. Matched text retains the input spelling according
to the selected position unit.

```pego
rule a = "abc"
```

### Character Classes

A `[...]` character class matches a single code point that is one of the listed
characters or lies in one of the listed ranges, and produces a terminal.

```pego
rule a = [0-9] // a decimal digit
rule b = [0-9a-zA-Z] // a digit or an ASCII letter
rule c = [0-9abc] // a digit, 'a', 'b' or 'c'
rule d = [\-()?] // '-', '(', '?' or ')'
```

A negated character class `[^...]` matches a single code point that is **not**
one of the listed characters or ranges. It fails at the end of the input.

```pego
rule string = "\"" [^"\\]* #atomic "\"" // characters other than " and \
```

Inside `[...]`:

- `a-z` denotes the inclusive range from `a` to `z`. The upper bound MUST NOT
  be less than the lower bound. Both endpoints MUST be Unicode scalar values;
  a range may span the surrogate interval when its endpoints are scalar values.
- A `-` that is not between two characters (the first item, or the last item
  before `]`) is a literal `-`. Elsewhere, write `\-`.
- `]` MUST be escaped as `\]`, and `\` as `\\`. Parentheses and `?` need no escape.
- `^` immediately after `[` negates the matcher; elsewhere it is literal.
- Whitespace is literal: `[ \t]` matches a space or a tab.
- The escape sequences of [string literals](lexical.md#escape-sequences) are
  available.
- A matcher MUST contain at least one item and MUST NOT contain a literal LF.
  A literal CR is allowed, as in string literals.

### Any Character

`.` matches any single code point, including a line feed, and produces a
terminal. It fails at the end of the input.

```pego
rule comment = "//" (!"\n" .)* #atomic
```

### Top

`_` always succeeds without consuming input and produces an empty terminal (a
`Match` node with empty text). It is useful as a placeholder for a rule whose
body is not yet written, or to make an empty alternative explicit.

```pego
rule stub = _
rule a_or_empty = "a" / _ // usually written "a"?
```

### Bottom

`_|_` always fails without consuming input. Combined with the
[`@error`](attributes.md#error) attribute, it reports a descriptive error.

```pego
rule hello = "hello" / _|_ @error("expect 'hello'")
```

## Anchors

An anchor matches a position without consuming input and has no value.

| Syntax | Matches |
|:--|:--|
| `^^` | at the beginning of the input |
| `$$` | at the end of the input (used to ensure that all input is consumed) |
| `^` | at the beginning of the input or immediately after a line feed |
| `$` | at the end of the input or immediately before a line feed or a carriage return |

```pego
rule a = "hello." $$
```

## Combinators

### Sequence

A sequence `a b` matches `a` and then `b` at the position where `a` ended. It fails
if any element fails.

```pego
rule a = "a" "b"
```

### Ordered Choice

An ordered choice `a / b` tries the alternatives from left to right and commits to the
first one that succeeds. Later alternatives are tried only if the earlier ones
fail; a successful alternative is never revisited, even if the expressions after
the choice fail.

```pego
rule a = "a" / "b"
```

### Grouping

Parentheses `(a)` group an expression to override [precedence](#precedence). A group
has the value of the enclosed expression.

```pego
rule a = "a" ("b" "c")
```

### Repetition

A repetition matches `a` repeatedly.

| Syntax | Meaning |
|:--|:--|
| `a #repeat(n)` | exactly `n` times |
| `a #repeat(n,)` | at least `n` times |
| `a #repeat(,m)` | at most `m` times |
| `a #repeat(n,m)` | at least `n` and at most `m` times |
| `a*` | shorthand for `a #repeat(0,)` |
| `a+` | shorthand for `a #repeat(1,)` |

Repetition is greedy and does not backtrack: it matches `a` as many times as
possible (up to the maximum) and fails only if fewer than the minimum matched.
Explicit bounds MUST be nonnegative integers, and `m` MUST NOT be less than `n`.
Omitting the second bound, rather than writing a negative value, selects an
unbounded maximum.

The value is a `List` node whose children are the values of the iterations.
Repetition stops at the first iteration that succeeds without consuming input.
That iteration counts and contributes its value, if any; the minimum is checked
after stopping. It MUST NOT run additional empty iterations to meet the minimum.
This rule applies to batch parsing, recognition, streaming, projected values,
incremental resumption and generated parsers.

For example, `_ #repeat(0)` succeeds with no iterations, `_ #repeat(0,1)` succeeds with one
empty value, and `_ #repeat(2)` and `_ #repeat(2,)` fail on empty input after one successful
iteration. A nullable body can still meet a larger minimum by consuming input:
`("a" / _) #repeat(2)` on `a` succeeds with one consuming iteration and one empty one.
The stopping rule keeps empty matches from looping forever, even with a large
minimum.

### Optional

`a?` matches `a` zero times or once. Its value is the value of `a`, or `nil` if
`a` did not match; it is not a `List`. In contrast, `a #repeat(0,1)` is a repetition and
produces a `List`.

### Lookahead

Lookahead tests the input that follows without consuming it, and has no value.

- `&a` (positive lookahead) succeeds if `a` matches at the current position.
- `!a` (negative lookahead) succeeds if `a` does not match at the current
  position.

```pego
// An identifier that does not begin with "if".
rule ident = !"if" [a-z]+
```

Captures and variable definitions made inside a positive lookahead remain in
effect after it; only the input position is restored. A negative lookahead MUST
NOT contain captures. Failures inside a lookahead are not included in the
expected items of a [syntax error](parsing.md#syntax-errors).

## Controlling Construction

### Atomic

`a #atomic` matches `a` and produces a single terminal holding the matched text,
without building the internal structure of `a`.

```pego
// [0-9]+ alone produces a List of single digits; #atomic makes it one terminal "123".
rule number = [0-9]+ #atomic
```

### Discard

`-a` matches `a` and discards the result: it builds neither nodes nor terminals,
and it has no value. Use it for input that the syntax requires but the tree does
not need, such as whitespace between words.

```pego
rule s = " "+
rule sentence = word (-s word)+
```

Because `--` is a single reserved token, `--a` is an error rather than a
discard of a discard; write `- -a` for that. A [cut](#cut) is written `#cut`.

### Cut

A cut `#cut` commits to the current alternative: once parsing passes a cut, the
enclosing choice does not backtrack into its other alternatives. Use it to
avoid pointless backtracking where the grammar makes the choice unambiguous.

```pego
// Matches "ab" but not "ac": after "a", the cut commits to the first
// alternative, so the second alternative is never tried.
rule a = "a" #cut "b" / "a" #cut "c"
```

A cut commits the innermost enclosing choice within the same rule. A repetition
or an optional expression counts as a choice between matching one more time and
stopping: if an iteration fails after passing a cut, the whole repetition (or
optional expression) fails instead of stopping. The effect of a cut never
extends beyond the rule that contains it: it does not affect choices in the
calling rule.

### Capture

A capture `label:a` records the value of `a` under the name `label` so that
[actions](actions.md) and [predicates](predicates.md) can refer to it as
`$label`. The label and the `:` MUST be adjacent.

```pego
rule full_name = first:word " " last:word => Name{First: $first, Last: $last}
```

Captures belong to a **scope**. A scope is either a rule body or a single
iteration of a repetition (`*`, `+`, `#repeat(...)`).

- A capture inside a choice or an optional expression belongs to the enclosing
  scope. If it did not match, its value is `nil`.
- A capture inside the element of a repetition, also inside a positive
  lookahead `&`, becomes a field of the value of that iteration. If the value of the element is a `Seq` node, the fields are
  added to it; otherwise the value is wrapped in a `Seq` node that carries the
  fields.

```pego
// Each element of $rest has the fields op and r.
rule expr = l:term rest:(op:("+" / "-") r:term)*
```

In a rule without an action, the captures of the rule body that matched become
fields of the value of the rule; captures that did not match are omitted. If
that value was not created by the rule body itself (for example, it is the
value of a called rule, or the value of a capture), it is first wrapped in a
`Seq` node whose only child is the value. If no capture matched, the value is
unchanged.

The following restrictions apply:

- A capture made in a match that was undone by backtracking does not survive.
- Captures MUST NOT appear inside `a #atomic`, `-a`, `!a`, the `skip` argument of
  [`#recover`](pragmas.md#recover), or the [`skip`](operators-rule.md#skip) item of a
  Operators Rule.
- The captured expression MUST have a value: `x:&a`, `x:-a` and `x:#cut` are
  errors.

## Rule Calls

A rule name in a Rule Expression calls the rule at the current position:
the body of the rule is evaluated there. If the body succeeds, so does the
call, with the end position of the body and the value of the rule (the value of
its action, if it has one). The rule MUST be defined; see
[Names](grammar-files.md#names).

A call is evaluated in a scope of its own. The captures made in the body of the
rule are visible only in that body and in the rule's action, and the variables
that the rule defines end with the call; the variables of the calling rules are
visible to the called rule (see [Variables](predicates.md#variables)). Rules
MAY call themselves recursively.

A rule defined with an [Operators Rule](operators-rule.md) can be called with the name
of a binding level using `level(name, binding)`. The keyword and opening parenthesis
must be adjacent; `name(binding)` is not a level call. See
[Level-restricted calls](operators-rule.md#level-restricted-calls).

## Left Recursion

A rule MAY be directly or indirectly left-recursive: it may call itself at the
position where it started, before consuming any input.

```pego
rule expr = expr "+" term / term
```

A call of a left-recursive rule `R` at a position `p` is evaluated by growing a
seed:

1. While `R` is being evaluated at `p`, a call of `R` at `p` fails. The body
   of `R` is evaluated under this condition. If it fails, the call fails.
2. If the body succeeded, its value is the current result (the seed). The body
   is evaluated again, with calls of `R` at `p` now succeeding with the current
   result. If the body succeeds and ends farther than the current result,
   the new value becomes the current result and step 2 is repeated. Otherwise
   the current result is the value of the call.

In an indirect left recursion, one rule of the cycle (the one where the
recursion is entered, decided by static analysis) is grown in this way, and the
others are evaluated again in each iteration. A rule that is not left-recursive
is evaluated once.

Static analysis considers calls that may run before consuming input, including
calls in `#recover(skip=...)`: recovery returns to the base expression's starting
position before trying the skip. It excludes zero-count repetition bodies and
ordered-choice suffixes when an earlier alternative is proven to succeed on every
input. Success proofs respect cut scopes; calls inside a left-recursion cycle
can read a failing seed and do not prove success. Removing unreachable calls and
refining the proofs continues until the call graph stabilizes. Unknown paths
remain in the graph, so removing an alternative whose unreachability is not
proven by this analysis can still change how an indirect cycle is entered.

Because growing tries the alternatives in order, **the recursive alternative
MUST come before the base case**: if the base case comes first, it matches again
with the same length, nothing grows, and the input after it is left over.

```pego
rule main = a $$
rule a = a "x" / "y"
```

For the input `yxx`, `pego parse -f sexpr` prints the left-associative tree

```text
(Seq (Seq (Seq "y"@a "x")@a "x")@a)@main
```

With `rule a = "y" / a "x"`, the same input fails with
`1:2: syntax error: expected end of input`.

For expressions with
operator precedence and associativity, an [Operators Rule](operators-rule.md) declares
the operators directly instead of requiring one left-recursive rule per
precedence level.
