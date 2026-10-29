# A070: Typed Output Bootstrap

A tiny typed result can retain a large first slab containing discarded siblings.
Independent early allocations keep common small results from immediately paying
that slab footprint.

## Mechanism and Invariants

For each owner, the first three objects/terminals allocate independently before
object slabs grow from 8 to 256 cells. The first nonempty small list also
allocates independently before list slabs grow from 64 to 1,024 elements. Later
slab maxima and exact-size large lists remain unchanged. Fresh ownership
preserves append isolation, spans and shared result values; returned chunks are
not pooled. Full/bounded liveness sweeps keep legacy helpers and are measured
separately.

## Scope and State

Applied by default to generated Go typed output, both direct construction and
Node conversion. Ordinary Node parse/recognition, closure, VMs and TypeScript
are unchanged.

## Reference Control

Private generation option `disableTypedOutputBootstrap` emits the legacy
first-slab strategy. Object/list slab controls can further isolate ownership.
The legacy same-source comparator retains bootstrap state fields, so it is not
layout-identical to historical main.

## Evidence and Implementation

Record 098 measures direct discard-heavy retained output from about 2.1 MB to at
most a few KiB after cloning returned Text. Conversion retained little in both
routes. Ordinary XML Bytes costs about 2.3% versus the same-generator legacy
route, with additional allocations; small JSON improves. These scoped tradeoffs
supported adoption and do not establish a general speedup.

Evidence records:
[074](../074-smaller-first-chunks-for-generated-typed-values.md),
[098](../098-bootstrap-generated-typed-output-allocations.md).

Source owners: [`typed allocation helper
emission`](../../../internal/gen/gogen/types.go), [`typed allocator
state`](../../../runtime/typed.go),
[`disableTypedOutputBootstrap`](../../../internal/gen/gogen/generator.go),
[`retained-output
fixture`](../../../internal/gen/gogen/gogen_typed_output_retention_test.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A250](A250-typed-object-slabs.md), [A260](A260-typed-list-slabs.md), [A090](A090-typed-output-liveness-cleanup.md).

[Optimization Catalog](../README.md)
