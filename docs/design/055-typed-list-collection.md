# 055. Typed Node Lists and Filtering

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-14

## Summary

This record covers two separate capabilities. The implemented typed-list subset
constructs lists of nodes with an explicit element Type, including empty and
nested lists. The broader `filter(list, predicate)` operation remains Proposed
and unresolved. Primitive-valued lists are outside both scopes.

## Motivation

Repetitions and the `list`, `map` and `concat` functions construct node lists,
but a result may have an element Type broader than a field requires. A Boolean
filter does not prove a narrower static Type: checking `x != nil` does not make
a `[]*T` result assignable to `[]T`. A typed list constructor declares the
element Type and applies ordinary assignability checks to every element.

```pego
[]Comment{$lineComment, $blockComment}
[]Comment{}
[][]Comment{}
[]*Comment{$lineComment, nil}
```

A non-optional `[]Comment` rejects `nil`; `[]*Comment` permits it. No
constructor silently filters or drops values.

## Selected Typed Node-List Contract

The source form is `[]T{expr, ...}`. `T` is an explicit Type Expression and
may be a node type, an alias resolving to one, or a union whose members are
node types. Nested lists are explicit: `[][]Comment{}` is valid, and a
non-empty inner list supplies its own element Type, as in
`[][]Comment{[]Comment{$item}}`. No expected-Type inference is performed.

The braces contain zero or more Action Expressions separated by commas, as
with constructor fields and function arguments. An empty list is non-nil.
Elements evaluate once from left to right and retain their order and identity.
Each element must have a statically known Type assignable to `T`; a statically
unknown `any` element is rejected, including when nested. `nil` is accepted
only if the target element Type permits it. These are compile-time checks under
normal type checking. `NoTypeCheck` deliberately bypasses assignability
checking; it does not add primitive-list support or a filtering/narrowing
operation.

The source AST node is `ListConstructor{Elem TypeExpr, Items []Term}`. It
preserves the explicit element Type and items, including empty and nested
constructors. After checking, it lowers to the existing `list()` runtime
operation. The emitted bytecode remains ISA 3 for this list-only path, but the
new source-AST tag is unsupported by older AST-bearing compiled-grammar
readers. AST-free list-only artifacts remain ISA 3 compatible.

The resulting list range spans the ranges of all non-nil child nodes. For an
empty list, or one whose elements are all nil, its range is zero-width at the
start of the matched input range of the current action context, not the
Action Expression's position in the grammar source. This rule applies in CodePoints and
Bytes modes.

The constructor does not add a parser boundary or change Rule Expression
evaluation, capture visibility, memoization, recovery, cuts, or input
consumption.

## Filtering Remains Proposed

A filter operation would preserve input order and node identity, but a
Boolean predicate alone cannot narrow the input list's static element Type.
For example, `filter($xs, (x) => $x != nil)` cannot establish `[]T` from
`[]*T`. Predicate Action errors follow the existing failure/backtracking
contract; Value Action errors abort as they do today. This proposal does not
select a signature, nil-input behavior, or filtering syntax.

## Alternatives Considered

- **A repetition pragma such as `#collect(T)`.** This couples typed-list
  construction to repetition and still needs rules for nil and non-assignable
  elements.
- **A typed `list<T>(...)` function.** This reuses function calls but adds
  generic-call syntax. A typed constructor keeps the element Type next to the
  values it constrains.
- **Filter only.** A Boolean predicate cannot generally narrow static element
  Types, so it cannot by itself initialize `[]T` from `[]*T`.
- **Keep `list` and `concat` only.** They preserve nil and do not establish a
  narrower static element Type.
- **Primitive-valued lists.** The current runtime list representation stores
  nodes. Primitive list storage needs a separate use-case and representation
  investigation.

## Compatibility and Validation Scope

The node-list syntax is additive. Preserve existing `list`, `map` and
`concat` behavior. Validate aliases and unions with the same canonical Type
resolution and assignability rules as other values. Keep source-AST and JSON
round trips, formatter stability, caller-AST immutability, and compiled-grammar
compatibility explicit. The closure, recursive VM, iterative VM, generated Go,
typed Go and TypeScript outputs must agree on values, identity, ordering,
ranges and diagnostics. Streams remain one result at a time; list construction
does not implicitly buffer a `ParseStream`.

## Testing

- Parse, format, JSON-round-trip and compile non-empty, empty, nested and
  optional-element lists.
- Accept assignable node elements and permitted nil; reject incompatible,
  statically unknown `any`, and invalid nested elements at compile time.
- Verify alias and union resolution, order, identity, non-nil empty results,
  and list ranges in both position units.
- Verify `NoTypeCheck` bypasses assignability checks without changing runtime
  representation.
- Compare all interpreters and generated backends for values, ranges,
  diagnostics and errors; check source-AST immutability and saved-grammar
  compatibility.
- Keep existing `map`, `list`, `concat`, repetitions, recovery and streamed
  rules unchanged.
- If `filter` is designed separately, test its independent error and nil
  behavior and verify it does not narrow static list Types.

## Limitations and Open Questions

- What concrete use cases justify primitive-valued lists such as `[]int` or
  `[]string`, given that the current runtime `List` stores nodes? Investigate
  before designing their storage or syntax.
- What signature and nil-input behavior should a separately proposed filter
  operation have? Its Boolean predicate cannot narrow static Types by itself.

Typed node-list constructors are implemented. This record remains Proposed
because filtering is unresolved. Primitive-valued lists remain outside the
implemented subset.

Local validation covers source and strict JSON round trips, all three runtime
backends and both position units, AST-bearing and AST-free compiled grammars,
generated Go and TypeScript, and typed result conversion. The root-module
suite, generated parser suite and site build/link check pass. The implementation
also received independent critical review. Remote CI remains paused.

## Focused Performance Evidence

On Go 1.27.1, darwin/arm64, Apple M3 Max, `GOMAXPROCS=16`, five
alternating 500 ms samples compare typed constructors with equivalent `list()`
calls for empty, three-element and nested lists. Across closure, recursive
bytecode and iterative bytecode in CodePoints, median time ratios are
0.979–1.018; allocated bytes and allocation counts are identical in every
paired case. The result supports reusing the existing list representation and
runtime checks; it does not establish a speed improvement. Compilation and
generated typed result conversion are outside this focused comparison.

Reproduce the workloads with:

```bash
GOMAXPROCS=16 go test ./internal/engine -run '^$' \
  -bench '^BenchmarkNodeListConstruction$' -benchmem -benchtime=500ms -count=5
```

Regression controls compare the complete change with `2afb673` using the
existing JSON and XML Parse workloads, all three runtime backends plus
generated Go, and both position units. Five alternating 500 ms samples give
median time ratios of 0.893–1.004, allocated-byte ratios of 0.9983–1.0011,
and allocation-count differences of −3 to +1 per parse. Ordinary background
activity and timing variation limit attribution; these controls do not
establish a speed improvement or describe every workload. Raw samples remain
outside the repository. Terminal-constructor-specific costs were not isolated.
