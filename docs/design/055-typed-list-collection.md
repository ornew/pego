# 055. Typed List Collection

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-14

## Summary

Add a `filter(list, predicate)` action function and a way to construct a list
with a declared element type directly from grammar results. This addresses
lists that contain useful nodes alongside `nil` or `Match` values, such as
the values produced by `(-space / comment)*`, when a struct field requires
`[]Comment`. The filter and typed-collection syntax and their exact contracts
remain proposed.

## Motivation

A repetition creates a `List` from the values of its successful iterations.
Those values can include `nil` and terminals such as `Match`; the list's
inferred type can therefore be broader than a typed field accepts. Existing
`list` and `map` can retain `nil` elements, while `concat` concatenates lists
and treats a `nil` list argument or recovered `Error` as empty. `concat` does
not filter elements within a list. These functions do not select elements by
a boolean condition or directly declare the element type of a new list.

For example, a grammar may want comment nodes from a repeated expression
while ignoring whitespace iterations that produce no value and punctuation
iterations that produce `Match`:

```pego
type Comment struct { Text string }
type File struct { Comments []Comment }

rule space = [ \t]+
rule comment Comment = "//" [^\n]* => Comment{Text: text($0)}
rule punctuation = [;,]
rule file File = comments:(-space / comment / punctuation)* #collect(Comment) => File{Comments: $comments}
```

The `#collect(Comment)` suffix is proposed syntax, not currently accepted.
A boolean filter alone does not solve the type mismatch: removing
`nil` values from a list of optional `*Comment` values does not prove to the
type checker that the remaining values are `Comment`. A general predicate
returns `bool`; it does not carry a type-narrowing result.

## Goals

- Provide an order-preserving list filter whose predicate returns `bool`.
- Provide a direct way to construct `[]T` from qualifying values produced by
  a grammar expression, without an intermediate untyped `List` action.
- Make nil handling and static element typing explicit; do not infer a narrowed
  type from an arbitrary boolean predicate.
- Preserve element values, identity, source ranges and evaluation behavior
  unless this record explicitly defines otherwise.
- Keep closure, recursive and iterative VMs, typed Go, generated Go,
  TypeScript, formatting, source AST/JSON and diagnostics consistent.

## Non-Goals

This proposal does not change `map` or `concat` semantics, infer type guards
from arbitrary lambdas, change repetition values, add mutable lists, or make
`ParseStream` a list-producing API. It does not add stream item filtering or
change the handling of recovered errors.

## Design

### Filter Function

Add a built-in action function with this schematic candidate signature (the
names are placeholders, not a complete PEGO expression):

```pego
filter(list, (item) => predicate)
```

The first argument is a `List`; the lambda returns `bool`. Evaluate the
predicate once for each input element, from first to last. Retain an element
when its predicate is true, and preserve the retained elements' order, values
and node identity. A non-boolean predicate is a compile-time type error. A
runtime error while evaluating the predicate follows the surrounding action
context's existing contract: in a Predicate Action it becomes predicate
failure and may backtrack; in a Value Action it aborts parsing. `filter` does
not override either behavior or add a recovery rule.

The result keeps the input list's static element type. In particular,
`filter($xs, (x) => $x != nil)` removes nil elements at runtime but does not
narrow `[]*Comment` to `[]Comment`. A lambda is an ordinary boolean predicate,
not a type test or flow-sensitive type guard. Whether a nil list argument is
treated as an empty list or rejected remains open; the existing `concat`
behavior is a compatibility point, not an automatic contract for `filter`.

The result range should cover the retained elements using the existing list
range rules. If no element remains, the result should have an empty range at
the input list's start. This must be validated in both position units.

### Direct Typed Collection

A separate operation must let grammar results directly produce a list with a
requested element type. One candidate is a collection pragma on a repeated
expression:

```pego
comments:(-space / comment)* #collect(Comment)
```

Under this candidate, the operation constructs a `List` whose element type is
`Comment`, allowing it to initialize `File.Comments []Comment` directly. The
name and placement are illustrative and are not selected syntax.

The inclusion rule needs an explicit type contract. The motivating use case
requires omitting `nil` and non-target terminals such as `Match`, while
retaining values assignable to the requested element type. Static validation
should reject a target type that cannot receive any possible value from the
expression. Whether another non-nil, non-assignable node is skipped or
reported as a runtime/type error remains open; silently dropping arbitrary
values could conceal a grammar mistake. Type unions, aliases, terminal types,
optional types and nested list types must follow the same assignment rules as
struct fields.

Collection evaluates the repeated expression using its existing PEG order and
short-circuit behavior. It must not rerun a match, reorder values, or alter
input consumption, captures, error expectations, recovery, memoization or
backtracking. Collection itself adds no parsing boundary. A list built by the
operation must preserve the source range of its collected elements; an empty
result needs a defined zero-width range at the expression's start.

### Relationship Between Filter and Collection

The two operations solve different problems. `filter` selects by a runtime
boolean condition but retains the input's static element type. Typed
collection selects values assignable to a declared type and constructs the
corresponding typed list. They may be composed when both predicates and type
selection are needed, but filtering must not be presented as a substitute for
type narrowing.

### Alternatives Considered

- **Only add `filter`.** This is familiar and supports arbitrary value-based
  selection, but a boolean lambda cannot generally prove a narrower static
  element type. It cannot by itself build the required `[]Comment`.
- **Only add typed collection.** This directly solves the node-type mismatch
  and may be expressible as a repetition pragma, but cannot select values by
  arbitrary runtime conditions unrelated to type assignability.
- **Add an explicit typed-list action constructor.** A constructor or literal
  could name `T` and build `[]T` from values. This is explicit and independent
  of repetition, but needs a type-argument/value syntax and rules for optional,
  nil and non-assignable elements. It may also require a new value form in the
  action language.
- **Add a type-test or type-guard predicate.** A predicate that refines its
  input type could let `filter` return a narrower list. This gives useful
  narrowing but changes lambda typing and control-flow inference throughout
  the action type checker; it is larger than an ordinary boolean filter.
- **Keep current `map` and `concat` only.** These functions already construct
  and combine lists, but `map` preserves nil results and `concat` does not
  select individual values by type. Callers must hand-build another filtering
  mechanism or accept a wider list type.

The recommended direction is to add ordinary `filter` and a separate typed
collection construct. Keep the concrete collection syntax and behavior for
unexpected non-assignable values open until the type-checking and formatter
implications are prototyped.

## Compatibility and Backend Support

Adding a built-in function and a typed collection form is additive for existing
grammars, but function-name conflicts, reserved names, source AST and JSON
representation, formatter stability and compiled grammar compatibility must
be reviewed. Existing `map` nil preservation and `concat` nil/Error handling
must remain unchanged.

The same list values and errors must be produced by the closure, recursive and
iterative VMs and generated Go and TypeScript parsers. Typed Go output must
construct the requested slice element type without unsafe assertions or
discarding values outside the specified collection rule. `ParseStream` keeps
its current per-item API: neither operation implicitly buffers a stream into a
list. If collection is usable inside a streamed rule, test each item and
recovery boundary without changing stream demand or commit behavior.

## Testing

- Compare `filter` with a simple reference over empty lists, mixed nil/node
  values, true/false predicates and repeated equal values; check order,
  identity, range and position units.
- Reject non-boolean predicates and invalid input types at compile time; verify
  runtime errors from the predicate follow the existing action context across
  backends: Predicate Action failure/backtracking versus Value Action abort.
- Demonstrate that `filter` does not narrow `[]*T` to `[]T`; cover optional
  elements, union elements and aliases in both action expressions and typed
  fields.
- For typed collection, cover nil and `Match` values, assignable and
  non-assignable nodes, aliases, unions, optional targets, empty results and
  ordering. Verify type diagnostics and the chosen behavior for unexpected
  values.
- Cover repetitions with backtracking, captures, predicates, memoization,
  left recursion, recovery and cuts; assert no extra matching or changed input
  consumption.
- Round-trip formatting, source AST and JSON, compile saved grammars, and
  compare values, node identity where promised, ranges, errors and generated
  typed output across all supported backends.
- If used from streamed rules, test backpressure, error recovery and one-item
  demand to confirm that the feature does not buffer or advance the stream.

## Limitations and Open Questions

- Should the direct typed-list syntax be a repetition pragma such as
  `#collect(T)`, a typed action constructor, or both?
- Does typed collection ignore only nil and `Match`, or skip every value not
  assignable to `T`? A stricter error policy may catch more mistakes.
- How should an empty collected list obtain its zero-width range?
- Should `filter(nil, predicate)` return an empty list, matching `concat`'s
  treatment of a nil list argument, or be a type error?
- Should the output type of `filter` always remain the input list's element
  type, or should PEGO eventually support explicit type-test lambdas?
- Which collection forms are valid in streamed rules while preserving the
  current one-result-at-a-time stream contract?

This record is Proposed. The new function and typed collection syntax are not
implemented or accepted language features.
