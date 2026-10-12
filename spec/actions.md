# Actions

PEGO has two action forms. A **Predicate Action** appears in braces where a
PEG primary expression is expected and evaluates without consuming input or
producing a value. A **Value Action** follows `=>` and computes a rule's value
from the values produced by its grammar body. An ordinary rule may have a
whole-rule Value Action after its complete expression:
`rule name [Type] = expression => term`. An Operators Rule has specialized
Value Actions on its operand and operator entries; a declaration-level action
is accepted only when it selects the existing default CST behavior. Omitting
it or using the explicit `=> ...` default-CST form preserves that behavior.
Any other declaration-level action on an Operators Rule is a compile-time
error.

A single-term Predicate Action keeps its existing execution semantics.
Multi-statement Predicate Action blocks and Value Action blocks parse, format
and appear in source ASTs, but semantic analysis rejects them as unsupported.
They do not execute their statements. Inline action forms are also recognized
in source and rejected by compilation; runtime inline-action execution remains
unimplemented.

```pego
// Source syntax only; compilation rejects this inline Value Action.
(b => { x := 1; x < 10; B{} })
```

```pego
type KV struct { Key Match, Op Match, Value Match }
rule key_value KV = (k:[a-z]+ op:"=" v:[0-9]+) => KV{Key: $1, Op: $2, Value: $3}
```

The action runs after the body succeeds. Its result is a node or `nil` and is
checked against the declared or inferred rule type. The action applies to the
whole body, including all alternatives.

## Whole-Rule Value Actions

A single-term Value Action follows `=>` after the complete rule expression.
When a grammar expression spans multiple lines, parenthesize the expression;
the line break in an action block is local to that block.

## Capture References

| Syntax | Refers to |
|:--|:--|
| `$label` | A named capture visible to the whole-rule action |
| `$0` | A list of values of the rule body's valued elements |
| `$n` | The 1-based valued element in the rule body's sequence |

For a whole-rule action, positional references describe the rule body. If the
body is not a sequence, `$1` is its value and `$0` is a singleton list. `$n`
counts only elements with values. An index beyond the available values is a
type error. Capture references use ASCII decimal digits and must fit the
implementation's `int`.

Captures are visible only in their enclosing grammar-expression scope. A
capture that did not match has value `nil`.

## Variable References

An action can read parser variables visible where it is evaluated. Reading an
undefined variable is a runtime action error.

```pego
type Item struct { Depth int, Name Match }
rule item Item = {depth := 1} n:name => Item{Depth: depth, Name: $n}
```

## Values and Constructors

Action expressions operate on nodes, integers, strings, booleans, `nil`, and
functions. String and integer literals use the same escapes and decimal
syntax as grammar literals. Lists are `List` nodes, produced by repetitions
and by `list`, `map`, and `concat`.

`T{Field: value, ...}` constructs a node of struct type `T`. Unset fields read
as `nil`.

```pego
rule name Name = [a-z]+
rule ident IdentNode = n:name => IdentNode{Name: $n}
```

A node returned as the action result has the range of the rule body; a node
created inside a helper function uses the smallest range covering its node
fields, or the body range when it has no node fields. A list uses the smallest
range covering its elements.

## Field Access

`x.Name` reads field `Name` from node `x`. A field absent from a value reads as
`nil`; an undeclared field is a type error. The built-in fields `startPos`,
`endPos`, and `children` are available on every node.

## Operators and Functions

Action operators include integer/string `+`, integer `-`, `*`, `/`, `%`,
comparisons, equality, boolean `&&`, `||`, and `!`, plus unary `-`. Arithmetic
and comparison precedence is unchanged. `.` field access and function calls
bind more tightly than operators.

Built-in functions include `len`, `text`, `foldl`, `foldr`, `map`, `list`, and
`concat`. Lambdas use `(...) => term`; this same `=>` token introduces a
Value Action in its grammar position. Braced Value Action blocks, including
`=> { build($1); finish() }`, are parsed, formatted and represented in source
ASTs, but compilation rejects them as unsupported. Inline `(expression =>
{ ... })` forms are likewise source-only; no inline or multi-statement action
execution is implemented. `concat` treats `nil` and recovered `Error` values
as empty lists.

## Types and Errors

Action expressions are type-checked. A value that does not fit a struct field,
an invalid field access, or an out-of-range `$n` is a grammar error. A runtime
action error, such as division by zero, aborts parsing; it is not a syntax error
and is not subject to backtracking or recovery.

## Value and Range Details

An action result MUST be a node or `nil`. A list is a `List` node; repetitions
and the `list`, `map`, and `concat` functions produce lists. `list` arguments
MUST be nodes or `nil`, and the function passed to `map` MUST return a node or
`nil`. A missing struct field reads as `nil`. For a union-typed node, field
access is allowed when at least one member declares that field; if only some
members declare it, the static result type is optional, and reading a value
whose concrete struct lacks the field is a run-time error.

A node returned directly by the whole-rule action has the range matched by the
rule body. A node created elsewhere has the smallest range covering its node
fields, or the rule body's range when it has no node fields. A list has the
smallest range covering its elements; an empty list has an empty range at the
start of the rule match. The built-in `children` field contains the children
of `Seq`, `List`, and `Operator` nodes and is empty for other node kinds.

Action operators are `+` (integer addition or string concatenation), integer
`-`, `*`, `/`, `%`, comparisons over integers or strings, equality over any
values, boolean `&&`, `||`, and `!`, and unary integer `-`. Boolean operators
short-circuit. Integer division truncates toward zero and remainder has the
dividend's sign. Nodes compare by identity; strings compare lexicographically.
Separate calls at the same position are not guaranteed to return identical
node identities because memoization determines whether a result is reused.
Precedence from loose to tight is `||`, `&&`, non-chainable comparisons,
`+ -`, `* / %`, then unary `- !`; field access and calls bind tighter than
all of them.

`len` returns a string or terminal length in the input position unit, a
nonterminal's child count, or zero for `nil`. `text` returns covered input
text, or the empty string for `nil`. `foldl` and `foldr` fold lists in the
named direction; lambda parameters are referenced with `$`. `concat` joins
lists, treating `nil` and recovered `Error` nodes as empty lists; a union of
list types contributes elements of either member type. Runtime evaluation
errors, including division by zero and invalid concrete field access, abort
the parse and are not backtracked or handled by recovery.

An action can read variables defined by its rule body and by callers of that
rule; definitions in called rules end with those calls and are not visible in
the caller's action. Reading a variable that is undefined at the action's
position is a run-time error. Captures are visible only in the expression scope
that made them: an action cannot refer to captures in a calling rule, and a
capture that did not match has value `nil`. `$n` counts only valued elements;
when the rule body is not a sequence, `$1` is the body's value and `$0` is a
singleton list containing it (or an empty list when it has no value). An index
larger than the number of valued elements is a grammar error. Indices use
ASCII decimal digits, may have leading zeroes, and must fit `int`.
