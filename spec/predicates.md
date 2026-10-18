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
rule deeper_indent = s:spaces { len($s) > indent } { indent := len($s) }
```

A nonempty Predicate Action block may contain statements separated by
semicolons or hard newlines. It is shorthand for adjacent Predicate Actions at
the same Rule Expression position, so these two rules behave alike:

```pego
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
sequence, including the existing CST result for a sequence of valueless
predicates. An empty block is a compile-time error.

The expression has the same operators and built-in functions as a
[Value Action](actions.md#operators-and-functions). Capture visibility follows the enclosing
grammar-expression scope. Inside a repetition element, only captures from that
iteration are visible; define a variable before the repetition to compare
against a value from outside it.

## Variables

| Syntax | Meaning |
|:--|:--|
| `{ name }`, `{ name == 1 }`, ... | Read the variable `name` |
| `{ name := value }` | Define the variable `name` |

A binding expression `name := value` makes up a single-term predicate. It
succeeds unless evaluating `value` is an error, then binds the value for the
current invocation. A block such as `{ name := value; test }` runs the binding
and test in order; the test can read the binding, and backtracking rolls it
back under the same rules as adjacent Predicate Actions.

- **Values.** A variable value is an `int`, `string`, or `bool`.
- **Definition before use.** Reading an undefined variable is an evaluation
  error and the predicate fails.
- **Scope.** Each rule invocation has its own variable scope. A rule can read
  variables from its callers; lookup resolves the nearest definition.
- **Immutability.** A new definition shadows an existing one. Definitions made
  in a called rule are not visible to its caller.
- **Backtracking.** Definitions made after a backtracked-to point are undone.

```pego
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
Definitions made inside it are undone when it returns. Memoization does not
change the result of parsing, only its cost.
