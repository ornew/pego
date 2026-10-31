# A230: Stream Undo Cleanup

Resetting undo length can leave old capture references in its backing array.
Clearing inactive backing releases those obsolete owners after stream commits.

## Mechanism and Invariants

Once the undo journal is reset, prune-batched cleanup clears its full backing
capacity. It runs when at least 1,024 input positions have been consumed since
pruning. Until that boundary inactive references can remain, and scratch
capacity itself stays retained. Live frame/capture values and published results
remain untouched.

## Scope and State

Default closure and both VM stream cleanup. Generated Go/TypeScript have no
stream API.

## Reference Control

Build tag `zp_reference_stream_undo` keeps the former backing references.
Combined `zp_reference_stream_storage` changes prefix and stack policy too; it
cannot attribute an undo-only effect.

## Evidence and Implementation

Record 089 includes paired retained-storage and throughput measurements for
independent/combined controls. Keep forced-GC retained bytes distinct from
allocated bytes and do not assign the combined reduction entirely to this
cleanup.

Evidence records:
[089](../089-isolate-stream-prefixes-and-batch-scratch-cleanup.md).

Source owners: [`undo backing
cleanup`](../../../internal/engine/core/stream_undo.go), [`parser.commit and
pruning`](../../../internal/engine/core/input.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A050](A050-streaming-state-reuse.md), [A240](A240-stream-vm-stack-cleanup.md).

[Optimization Catalog](../README.md)
