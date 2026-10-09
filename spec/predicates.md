# Predicates and Variables

A predicate reads and updates the internal state of the parser, its
**variables**, and decides dynamically whether parsing succeeds at that point.
Predicates make context-sensitive grammars possible, such as indentation-based
languages. A predicate never consumes input and has no value.

## Predicates

A predicate `[e]` evaluates the expression `e`. It fails if the result is
`false` or if the evaluation is an error, and succeeds otherwise.

```pego
// Matches an indentation deeper than the current one, and makes it current.
def deeper_indent = s:spaces [len($s) > indent] [indent = len($s)]
```

The expression has the same syntax as an [action](actions.md#operators). It can
refer to variables and to the captures `$label` made earlier in the same
[scope](parser-expressions.md#capture): inside the element of a repetition,
only the captures of that iteration are visible, not those of the enclosing rule
body. To compare with a value from outside the repetition, define a variable
from it first.
Positional references `$n` and `$0` MUST NOT be used in predicates.

## Variables

| Syntax | Meaning |
|:--|:--|
| `[name]`, `[name == 1]`, ... | Read the variable `name` |
| `[name = value]` | Define the variable `name` |

A definition `name = value` MUST make up the whole predicate. It always
succeeds unless evaluating `value` is an error.

- **Values.** The value of a variable MUST be an `int`, a `string` or a `bool`.
- **Definition before use.** A variable can be read once its definition has
  been executed. Reading a variable that is not defined is an error, so the
  predicate fails.
- **Scope.** Each rule invocation has its own scope. A rule can read the
  variables of the rules that (directly or indirectly) called it; a name
  resolves to the definition in the nearest scope.
- **Immutability.** Defining a variable that already exists does not modify it:
  the new definition shadows the old one. Shadowing in a called rule does not
  affect the caller, and definitions made in a called rule are not visible to the
  caller.
- **Backtracking.** When the parser backtracks, definitions made after the point
  it returns to are undone.

```pego
def a = [x = 1] b
def b = [x == 1]          // succeeds when called from a, where x = 1
```

```pego
def a = [x = 1] b [x == 1]           // the definition in b is not visible in a: x is still 1
def b = [x == 1] [x = 2] [x == 2]    // before the definition, x is a's x (1); after it, x is 2
```

## Operators and built-in functions

Predicates use the same operators and built-in functions as
[actions](actions.md). The most commonly used functions are:

| Function | Description |
|:--|:--|
| `len(x)` | The number of elements of a list, or the length of a string or a terminal in the [position unit](parsing.md#positions) |
| `text(x)` | The input text that the node `x` covers |

Nodes that a predicate creates (for example with `new`) are discarded after the
predicate is evaluated.

## Interaction with memoization

The result of a rule that reads variables, directly or through the rules it
calls, depends on the values of those variables when the rule is called
(definitions made inside the rule are undone when it returns). Such a rule is
memoized per combination of those values: a memoized result is reused only when
the variables have the same values as when it was computed. Memoization never
changes the result of parsing, only its cost.
