# A250: Typed Object Slabs

After small-result bootstrap allocations, typed struct and terminal cells use
chunks to amortize per-object allocation.

## Mechanism and Invariants

Each typed owner starts slabs at 8 cells and grows to 256. Fresh owner state
avoids reusing returned cells; one escaped cell can retain unused siblings in
its chunk. Bootstrap and optional liveness cleanup are separate policies. Direct
construction and Node conversion use the same object allocator.

## Scope and State

Default generated Go typed output only. Node slabs, engine values and TypeScript
objects use different allocation policies.

## Reference Control

Private `disableTypedObjectSlabs` emits individual object allocation while
holding list slab policy independently selectable. It is not the
bootstrap-disable control.

## Evidence and Implementation

Records 048/074 describe typed construction and smaller first chunks; 098 adds
independent early allocations. Retained-output controls preserve spans, sharing
and field pointers. Slab allocation can reduce object counts while increasing
retained siblings; current broad speedup is not established by the individual
allocator controls.

Evidence records: [048](../048-a-typed-runtime-for-parseast.md),
[074](../074-smaller-first-chunks-for-generated-typed-values.md),
[098](../098-bootstrap-generated-typed-output-allocations.md).

Source owners: [`astNew generation`](../../../internal/gen/gogen/types.go),
[`disableTypedObjectSlabs`](../../../internal/gen/gogen/generator.go),
[`objects/individual
controls`](../../../internal/engine/gen_typed_output_retention_test.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A070](A070-typed-output-bootstrap.md), [A260](A260-typed-list-slabs.md), [A090](A090-typed-output-liveness-cleanup.md).

[Optimization Catalog](../README.md)
