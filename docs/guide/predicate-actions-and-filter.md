# Predicate Actions and List Filtering

Predicate Action blocks let a Rule Expression run ordered tests and assignments
without consuming input. `filter` selects list elements by a Boolean predicate.
See the [Actions specification](../../spec/actions.md) for the complete action
and built-in function contracts.

## Predicate Action Blocks

A nonempty block is shorthand for adjacent Predicate Actions at the same
position:

```pego
rule item = { count := 1; count == 1 } "a"
```

This behaves like:

```pego
rule item = { count := 1 } { count == 1 } "a"
```

Statements run from left to right and stop at the first false result or
error. Assignments are visible to later statements and follow the usual
backtracking and rollback behavior. A block does not create a variable or
capture scope, consume input, or add a rule boundary. In a single-expression
position, the block retains the grouped adjacent-sequence behavior, including
the existing CST result for a sequence of valueless predicates. Empty blocks
are compile-time errors.

Value Action blocks are not supported; Value Actions use the single-term form
after `=>`.

## Filtering a Node List

```pego
type Result = { Items []*Match }

rule main = word:[a-z]+ #atomic $$ => Result{
    Items: filter(list($word, nil, $word), (item) => $item != nil)
}
```

`filter` visits elements from left to right. The lambda must return an actual
Boolean: `true` keeps the original element and `false` removes it. A nil
element is passed to the lambda as nil and is kept if the predicate returns
true. A nil input behaves like an empty list and calls the lambda zero times.
The result preserves element order and identity; filtering does not clone
nodes.

Filtering does not narrow the list's static element Type. In the example,
`$item != nil` does not change the result from a list of optional `Match`
values into a list of non-optional values. A statically known non-Boolean
predicate result is a compile-time error; a dynamically non-Boolean result is
a runtime action error. An error in a Value Action aborts the parse; an error
in a Predicate Action makes that predicate fail and permits normal
backtracking.
