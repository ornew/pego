# A020: Node and List Slabs

Allocating each Node and child list individually creates many small heap
objects. Parser-owned chunks amortize those allocations and collect repetition
children before copying the published list once.

## Mechanism and Invariants

`newNode`, node-list and field allocators carve storage from chunks. A result
retains the chunk containing its storage; returned cells cannot be recycled
merely because matching has finished. The temporary child stack is separate from
published child slices, whose capacity is constrained to avoid appends
overwriting a neighbor. Slab ownership trades fewer allocation objects for
possible retention of unused siblings.

## Scope and State

Default in closure, both VMs and generated Go Node construction. Typed Go uses a
distinct typed allocation policy. TypeScript arrays/objects do not implement
these Go slabs.

## Reference Control

No independent same-source Node/list slab-disable control is maintained. Typed
object/list controls operate on typed output and do not disable Node chunks.

## Evidence and Implementation

Record 006 reports JSON allocation counts from about 940k to 250k and 5–10%
lower sampled timing for its combined node/list/frame change. Record 011 ports
the storage to generated Go among other changes. These are historical combined
results, not current slab-only effects or guarantees of low retained heap.

Evidence records:
[006](../006-slab-allocation-for-nodes-child-lists-and-frames-5d03bf2.md).

Source owners: [`parser.newNode, nodes, fields and
kids`](../../../internal/engine/runtime.go), [`generated ptrSlab and node/list
allocation`](../../../runtime/runtime.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A010](A010-compact-node-representation.md), [A250](A250-typed-object-slabs.md), [A260](A260-typed-list-slabs.md).

[Optimization Catalog](../README.md)
