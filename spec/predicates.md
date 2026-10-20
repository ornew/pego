# Predicates and Variables

A Predicate Action evaluates an expression without consuming input or producing a
value. Predicate Actions and parser variables make context-sensitive grammars
possible, including indentation-sensitive languages.

## Predicate Actions

The form `{ e }` evaluates a Predicate Action without consuming input or
producing a value. A single Predicate Action keeps existing semantics: it
fails if the result is `false` or evaluation is an error, and otherwise
succeeds. It may refer to variables and named captures visible at its
position. Square brackets remain character classes; braces are predicates
only where a primary form of a Rule Expression is expected.

```pego
// Match indentation deeper than the current level, then update it.
var indent int
rule deeper_indent = s:spaces { len($s) > indent } { indent := len($s) }
```

A nonempty Predicate Action block may contain statements separated by
semicolons or hard newlines. It is shorthand for adjacent Predicate Actions at
the same Rule Expression position, so these two rules behave alike:

```pego
var count int
rule item = { count := 1; count == 1 } "a"
rule item = { count := 1 } { count == 1 } "a"
```

The statements execute in source order. An assignment updates the current
invocation's variable environment, and a term fails the predicate if it
evaluates to `false` or errors. Evaluation stops at the first failure, and an
assignment is visible to the later statements and rolled back like an adjacent
Predicate Action. The block consumes no input and adds no variable, capture,
rollback, memoization or recovery scope. In a sequence it expands to adjacent
actions; in a single-expression position it behaves like the grouped adjacent
sequence, including the unit `()` that a sequence of valueless
predicates produces. An empty block is a compile-time error.

The expression has the same operators and built-in functions as a
[Value Action](actions.md#operators-and-functions). Capture visibility follows the enclosing
grammar-expression scope. Inside a repetition element, only captures from that
iteration are visible; define a variable before the repetition to compare
against a value from outside it.

## Variables

| Syntax | Meaning |
|:--|:--|
| `var name type = value` | Declare the variable `name` with its type and initial value |
| `{ name }`, `{ name == 1 }`, ... | Read the variable `name` |
| `{ name := value }` | Bind the variable `name` |

A variable is declared by a top-level `var` declaration, which may appear among
the other top-level declarations in any order. A binding expression
`name := value` makes up a single-term predicate. It succeeds unless evaluating
`value` is an error, then binds the value for the current invocation. A block such as `{ name := value; test }` runs the binding
and test in order; the test can read the binding, and backtracking rolls it
back under the same rules as adjacent Predicate Actions.

- **Declaration.** `var name type = value` declares `name` with a type of
  `int`, `string` or `bool` and the initial value `value`. Either the type or
  the value may be omitted. Without a value, the variable starts as `0`, `""`
  or `false`; without a type, its type is the type of the value. `=` defines the
  initial value; it is not an assignment.
- **Initial value.** The value is an `int`, `string` or `bool` literal, and an
  `int` literal may be negated. `nil` and any other expression are compile-time
  errors, as is a value of another type than the declared one.
- **Types.** Reading a variable gives its declared type, so `x + 1` and
  `V{N: x}` are checked when the grammar is compiled. A binding `name := value`
  must give a value that is assignable to the declared type.
- **Declaration before use.** Reading or binding a name that is not declared is
  a compile-time error. Declaring a name twice is a compile-time error.
- **Starting state.** Every parse starts with every declared variable bound to
  its initial value, through every start rule and every API, before the start
  rule runs.
- **Scope.** Each rule invocation has its own variable scope. A rule can read
  variables from its callers; lookup resolves the nearest binding, or the
  initial value when nothing has bound the variable.
- **Immutability.** A new binding shadows an existing one. Bindings made in a
  called rule are not visible to its caller.
- **Backtracking.** Bindings made after a backtracked-to point are undone.

```pego
var x int = 0
rule a = {x := 1} b
rule b = {x == 1}
```

## Operators and Built-In Functions

Predicate Actions use the same operators and built-in functions as
[Value Actions](actions.md). Common functions include `len(x)` and `text(x)`.
Nodes created while evaluating a predicate are discarded after evaluation.

## Interaction With Memoization

A rule that reads variables in Predicate Actions or Value Actions, directly or
through a called rule, depends on their values when it is called. Memoized
results are reused only for the same relevant variable values, so such a rule,
including an Operators Rule operand or operator with an action that reads
variables, is memoized per combination of the variable values that it reads.
Bindings made inside it are undone when it returns. Memoization does not
change the result of parsing, only its cost.
