# A080: Generated Node Capture Scratch

Generated Node parsing creates temporary capture frames and slot arrays that
need to live through parsing but not with the returned result. Separate reusable
arenas reduce repeated scratch allocation.

## Mechanism and Invariants

Frame and slot arenas are stable and append-only during a parse; individual
frames are never recycled at ordinary rule return. Recycle clears backing
storage, directory tails and undo/saved references. Reusable backing has a
combined bound of `1<<20` native words; unusually large scopes use exact
unretained allocations. Published Nodes remain in independent result slabs. The
limit bounds retained reusable scratch, not peak parsing memory.

## Scope and State

Default generated Go Node parsing and capture-dependent recognition only. Typed
AST captures, engine/VM execution and TypeScript retain their separate
allocators.

## Reference Control

None. The shared generated runtime always reuses the scratch; the former
allocator is reproducible only from the revisions of record 092.

## Evidence and Implementation

Record 092 measures capture-heavy Node parsing medians around 0.86–0.95 of its
historical parent and lower allocation volume. An initial full XML sample is
slower despite 15–17% fewer allocated bytes; later same-source rechecks favor
scratch medians, with some ranges including parity. A field-layout control is
inconclusive. These scoped observations support reusable scratch while retaining
GC-sensitive uncertainty, rather than a general speed claim.

Evidence records: [092](../092-reuse-generated-node-capture-scratch.md).

Source owners: [`node frame/slot scratch and parser
release`](../../../runtime/runtime.go), [`capture scratch
checks`](../../../internal/engine/gen_frame_scratch_test.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A020](A020-node-and-list-slabs.md), [A160](A160-whole-input-scratch-pooling.md).

[Optimization Catalog](../README.md)
