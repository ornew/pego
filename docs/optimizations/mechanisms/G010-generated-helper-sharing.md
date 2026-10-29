# G010: Generated Helper Sharing

Generated parsers can repeat identical expression methods and literal tables.
Sharing exact emitted helpers reduces generated source duplication without
merging rule identities.

## Mechanism and Invariants

Helper interning requires an identical method signature and complete body. Rule
wrappers remain separate, preserving memo keys, depth, captures and
expectations. Failed speculative direct lowering rolls back inserted helper
entries and reserved names, so later code cannot call a discarded helper.
Literal-table sharing is a separate registry and selector.

## Scope and State

Default helper sharing in generated Go Node, typed Go and TypeScript. Closure
and VMs are outside this generator mechanism.

## Reference Control

Private `disableMethodSharing` produces a same-generator duplicate-helper
reference. Literal sharing can be held fixed independently; no matching-time
selector is emitted.

## Evidence and Implementation

Record 090 measures emitted-source changes and paired generated workloads in
Go/TypeScript. Its mixed-use TypeScript literal results did not justify default
adoption. Source reduction does not by itself prove lower matching time; use the
record's separate configurations and workload results rather than assigning one
universal speedup.

Evidence records: [090](../090-share-identical-generated-helpers.md).

Source owners: [`Generator.Method`](../../../internal/gen/gogen/generator.go),
[`gen.Generator.LiteralName interning`](../../../internal/gen/generator.go),
[`TypeScript sharing policy`](../../../internal/gen/tsgen/generator.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [G130](G130-generated-literal-sharing.md), [G100](G100-generated-node-body-inlining.md).

[Optimization Catalog](../README.md)
