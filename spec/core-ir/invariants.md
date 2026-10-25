# Invariants

A well-formed module satisfies the following. They hold when a module is built and after every rewrite followed by
analysis, and the verifier of the IR package checks them on an analyzed module, returning the first violation.

## Structure

- Every rule has its index as its ID and a body. Every start rule, `Call` and `Nest` names an existing rule.
- A twin is value-free: its policy is NoValue and its Want is false. A rule with the finish Action has an action.
- In a typed module, a rule that is not a twin has a type.
- The call graph, its components and its cycle flags equal those computed from the bodies.
- A `Nest` of rule r occurs only in rules outside the component of r, and never inside another `Nest` of that component.
- An `OperatorsRule` is the whole body of its rule. An operator has a level of at least 1.
- A `Stream` is at the top of a start rule: its body, an item of its body sequence, or the body of a capture that is one
  of these, or the body of labels at one of these places.
- A `Missing` has a non-empty literal.

## Names and Interning

- Every `Capture` and `SlotRef` names a slot of its enclosing scope, by index and name. The scopes are those of a rule, a
  `Repeat` with a scope, a line of an Operators Rule and the rule of a `Nest`.
- Every expectation is an expectation of the module. Every class is a class of the module, and each class is in the
  module once.

## Values and Captures

- The bodies of a negative `Look`, of `Text` and of `Drop`, and the skip expression of a `Recover`, want no value and
  write no capture of the enclosing scope.
- The body of a positive `Look` wants a value only where it captures.

## Guards and Optimized Forms

- A choice guard equals the first terminal of its alternative. An alternative marked as a guarded class begins with the
  guard's class, at depth 0, as the alternative itself or as the first item of its sequence.
- Repetition and `Scan` bounds are ordered: the minimum is not negative and a non-negative maximum is not below it.
- A `Terminal` contains only literals, classes, sequences, choices without guards, options, negative lookaheads, `Text` and
  `Drop`, and wants no value. A `Scan` and a `ClassRun` want no value, and a `ClassRun` has at least one class.

## Types

- In a typed module, every term but a lambda, every operand of a `FieldCompare`, every slot and the value of every rule
  that is not a twin has a checked type.
