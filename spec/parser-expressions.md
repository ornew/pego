# Parsing Expressions

A parsing expression is the right-hand side of a rule definition (`def`). It
matches a prefix of the input at the current position and either succeeds,
possibly consuming input and producing a value, or fails.

## Values and the concrete syntax tree

PEGO produces a predictable and consistent concrete syntax tree (CST). When a
parsing expression succeeds it produces a value, normally a node. A rule without
an action produces the value of its body.

| Expression | Value |
|:--|:--|
| Literal, character class, `.`, `@a`, `_` | A terminal: a `Match` node holding the matched text |
| `a b` | A `Seq` node whose children are the values of the elements that have a value |
| `a / b` | The value of the alternative that matched; the choice itself creates no node |
| `a*`, `a+`, `a{n,m}` | A `List` node whose children are the values of the iterations |
| `a?` | The value of `a`, or `nil` if `a` did not match |
| `label:a` | The value of `a` |
| Rule call | The value of the called rule |

The following rules determine the shape of the tree.

1. **One value per rule.** A rule produces a single value, whose kind is
   determined by the top-level expression of the rule body:
   - `def a = "x" "y"` produces a `Seq` node.
   - `def b = "x" / "y" "z"` produces the value of the alternative that matched
     (a `Match` node or a `Seq` node).
   - `def c = "x"` produces a `Match` node.
   - `def d = a` produces the value of rule `a` unchanged.
2. **Rule names.** A node that a rule without an action creates **in its own
   body** is labeled with the name of that rule. Nodes received from other rules
   keep their own label: `def d = a` produces the node of `a`, labeled `a`. The
   value of an action is final in the same way: if an action returns a node of
   its rule's body (`-> $1`), that node is not labeled by any rule.
3. **Expressions without a value.** The following expressions have no value and
   never appear as children of a `Seq` node:
   - anchors: `^^`, `$$`, `^`, `$`;
   - cut (`--`), discard (`-a`), bottom (`_|_`), lookahead (`&a`, `!a`) and
     [predicates](predicates.md).

   An optional expression `a?` that does not match does have a value, `nil`,
   which appears as a child.
4. **Suppressed construction.** Atomic (`@a`) and discard (`-a`) do not build
   the values of their operands, which saves work and keeps the tree small.
5. **Whole input.** A parse succeeds only if the [start rule](parsing.md#start-rule)
   matches the entire input; see [Parsing](parsing.md#whole-input).

The reasons for making choices transparent are recorded in
[docs/design/002](../docs/design/002-transparent-choice.md).

## Operator summary

| Syntax | Name | Consumes input |
|:--|:--|:--|
| `"..."` | literal | yes |
| `(?...)`, `(?^...)` | character class | yes |
| `.` | any character | yes |
| `_` | top | no |
| `_\|_` | bottom | no |
| `^^` / `$$` | beginning / end of input | no |
| `^` / `$` | beginning / end of line | no |
| `a b` | sequence | yes |
| `a / b` | ordered choice | yes |
| `(a)` | grouping | yes |
| `a{n,m}`, `a*`, `a+` | repetition | yes |
| `a?` | optional | yes |
| `&a` / `!a` | positive / negative lookahead | no |
| `@a` | atomic | yes |
| `-a` | discard | yes |
| `--` | cut | no |
| `label:a` | capture | yes |
| `[...]` | [predicate](predicates.md) | no |
| `#name(...)` | [attribute](attributes.md) | depends on the attribute |

"Consumes input" states whether the expression can advance the input position.

### Precedence

Operators bind in the following order, from tightest to loosest. Parentheses
override the order.

| Precedence | Operators | Kind |
|:--|:--|:--|
| 1 (tightest) | `*`, `+`, `?`, `{n,m}`, `#name(...)` | postfix |
| 2 | `label:`, `&`, `!`, `@`, `-` | prefix |
| 3 | `a b` | sequence |
| 4 (loosest) | `a / b` | ordered choice |

For example, `@(?0-9)+` is `@((?0-9)+)`, `x:a*` is `x:(a*)`, and
`"a" "b" / "c"` is `("a" "b") / "c"`. Postfix operators can be stacked
(`a*?`), and so can prefix operators (`x:@a`).

## Terminals

### Literals

A literal `"..."` matches exactly the given string and produces a terminal.

```pego
def a = "abc"
```

### Character classes

A character class `(?...)` matches a single code point that is one of the listed
characters or lies in one of the listed ranges, and produces a terminal.

```pego
def a = (?0-9)        // a decimal digit
def b = (?0-9a-zA-Z)  // a digit or an ASCII letter
def c = (?0-9abc)     // a digit, 'a', 'b' or 'c'
def d = (?\-(?\))     // '-', '(', '?' or ')'
```

A negated character class `(?^...)` matches a single code point that is **not**
one of the listed characters or ranges. It fails at the end of the input.

```pego
def string = "\"" @(?^"\\)* "\""  // characters other than " and \
```

Inside a character class:

- `a-z` denotes the inclusive range from `a` to `z`. The upper bound MUST NOT
  be less than the lower bound.
- A `-` that is not between two characters (the first item, or the last item
  before `)`) is a literal `-`. Elsewhere, write `\-`.
- `)` MUST be escaped as `\)`, and `\` as `\\`. Other characters, such as
  `(` and `?`, need no escape.
- `^` immediately after `(?` negates the class; elsewhere it is literal.
- Whitespace is literal: `(? \t)` matches a space or a tab.
- The escape sequences of [string literals](lexical.md#escape-sequences) are
  available.
- A class MUST contain at least one item and MUST NOT span lines.

### Any character

`.` matches any single code point, including a line feed, and produces a
terminal. It fails at the end of the input.

```pego
def comment = "//" @(!"\n" .)*
```

### Top

`_` always succeeds without consuming input and produces an empty terminal (a
`Match` node with empty text). It is useful as a placeholder for a rule whose
body is not yet written, or to make an empty alternative explicit.

```pego
def stub = _
def a_or_empty = "a" / _  // usually written "a"?
```

### Bottom

`_|_` always fails without consuming input. Combined with the
[`#error`](attributes.md#error) attribute, it reports a descriptive error.

```pego
def hello = "hello" / _|_ #error(message="expect 'hello'")
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
def a = "hello." $$
```

## Combinators

### Sequence

A sequence `a b` matches `a` and then `b` at the position where `a` ended. It fails
if any element fails.

```pego
def a = "a" "b"
```

### Ordered choice

An ordered choice `a / b` tries the alternatives from left to right and commits to the
first one that succeeds. Later alternatives are tried only if the earlier ones
fail; a successful alternative is never revisited, even if the expressions after
the choice fail.

```pego
def a = "a" / "b"
```

### Grouping

Parentheses `(a)` group an expression to override [precedence](#precedence). A group
has the value of the enclosed expression.

```pego
def a = "a" ("b" "c")
```

### Repetition

A repetition matches `a` repeatedly.

| Syntax | Meaning |
|:--|:--|
| `a{n}` | exactly `n` times |
| `a{n,}` | at least `n` times |
| `a{,m}` | at most `m` times |
| `a{n,m}` | at least `n` and at most `m` times |
| `a*` | shorthand for `a{0,}` |
| `a+` | shorthand for `a{1,}` |

Repetition is greedy and does not backtrack: it matches `a` as many times as
possible (up to the maximum) and fails only if fewer than the minimum matched.
`m` MUST NOT be less than `n`.

The value is a `List` node whose children are the values of the iterations.
Repetition stops at the first iteration that succeeds without consuming input,
so a repetition of an expression that can match the empty string does not loop
forever.

### Optional

`a?` matches `a` zero times or once. Its value is the value of `a`, or `nil` if
`a` did not match; it is not a `List`. In contrast, `a{0,1}` is a repetition and
produces a `List`.

### Lookahead

Lookahead tests the input that follows without consuming it, and has no value.

- `&a` (positive lookahead) succeeds if `a` matches at the current position.
- `!a` (negative lookahead) succeeds if `a` does not match at the current
  position.

```pego
// An identifier that does not begin with "if".
def ident = !"if" (?a-z)+
```

Captures and variable definitions made inside a positive lookahead remain in
effect after it; only the input position is restored. A negative lookahead MUST
NOT contain captures. Failures inside a lookahead are not included in the
expected items of a [syntax error](parsing.md#syntax-errors).

## Controlling construction

### Atomic

`@a` matches `a` and produces a single terminal holding the matched text,
without building the internal structure of `a`.

```pego
// (?0-9)+ alone produces a List of single digits; @ makes it one terminal "123".
def number = @((?0-9)+)
```

### Discard

`-a` matches `a` and discards the result: it builds neither nodes nor terminals,
and it has no value. Use it for input that the syntax requires but the tree does
not need, such as whitespace between words.

```pego
def s = " "+
def sentence = word (-s word)+
```

Because `--` is a single token, `--a` is a [cut](#cut) followed by `a`, not a
discard of a discard.

### Cut

A cut `--` commits to the current alternative: once parsing passes a cut, the
enclosing choice does not backtrack into its other alternatives. Use it to
avoid pointless backtracking where the grammar makes the choice unambiguous.

```pego
// Matches "ab" but not "ac": after "a", the cut commits to the first
// alternative, so the second alternative is never tried.
def a = "a" -- "b" / "a" -- "c"
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
def full_name = first:word " " last:word
    -> new Name{First: $first, Last: $last}
```

Captures belong to a **scope**. A scope is either a rule body or a single
iteration of a repetition (`*`, `+`, `{n,m}`).

- A capture inside a choice or an optional expression belongs to the enclosing
  scope. If it did not match, its value is `nil`.
- A capture inside the element of a repetition becomes a field of the value of
  that iteration. If the value of the element is a `Seq` node, the fields are
  added to it; otherwise the value is wrapped in a `Seq` node that carries the
  fields.

```pego
// Each element of $rest has the fields op and r.
def expr = l:term rest:(op:("+" / "-") r:term)*
```

In a rule without an action, the captures of the rule body that matched become
fields of the value of the rule; captures that did not match are omitted. If
that value was not created by the rule body itself (for example, it is the
value of a called rule, or the value of a capture), it is first wrapped in a
`Seq` node whose only child is the value. If no capture matched, the value is
unchanged.

The following restrictions apply:

- A capture made in a match that was undone by backtracking does not survive.
- Captures MUST NOT appear inside `@a`, `-a`, `!a`, the `skip` argument of
  [`#recover`](attributes.md#recover), or the [`skip`](pratt.md#skip) item of a
  Pratt expression.
- The captured expression MUST have a value: `x:&a`, `x:-a` and `x:--` are
  errors.

## Left recursion

A rule MAY be directly or indirectly left-recursive.

```pego
def expr = expr "+" term / term
```

The algorithm is described in
[docs/design/003](../docs/design/003-packrat-parsing.md).

For expressions with operator precedence and associativity, a
[Pratt expression](pratt.md) declares the operators directly instead of
requiring one left-recursive rule per precedence level.
