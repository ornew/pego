# A220: Stream Prefix Isolation

A long-lived stream root can retain a slab containing otherwise dead early
elements. Private prefix allocation isolates that root from later result chunks.

## Mechanism and Invariants

Before top-level streaming starts, a negative chunk sentinel budgets 32
exact-size prefix allocations. At stream start, tails seal and the sentinel
resets; later cells use normal slabs. Root protection remains necessary if
growth allocates a root later. Published Nodes, lists, fields and functions stay
immutable under reuse.

## Scope and State

Default engine stream prefix ownership in closure and both VMs.
Generated/TypeScript parsers have no stream path.

## Reference Control

Build tag `pego_reference_stream_prefix` selects ordinary prefix slabs; combined
`pego_reference_stream_storage` also disables cleanup routes. The independent
tag isolates prefix ownership.

## Evidence and Implementation

Record 089 measures this with undo/VM cleanup in paired retained-storage cases
showing 81–92% reduction. Combined results are not prefix-only effects. It
records altered inline eligibility in allocation helpers and does not claim
universal throughput improvement.

Evidence records:
[089](../089-isolate-stream-prefixes-and-batch-scratch-cleanup.md).

Source owners: [`prefix allocation
policy`](../../../internal/engine/core/stream_prefix.go), [`nodeChunks and stream
root protection`](../../../internal/engine/core/runtime.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A200](A200-stream-capture-frame-reuse.md), [A230](A230-stream-undo-cleanup.md), [A240](A240-stream-vm-stack-cleanup.md).

[Optimization Catalog](../README.md)
