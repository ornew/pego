# A240: Stream VM Stack Cleanup

Popping a VM value stack can leave references beyond its active length. Stream
cleanup clears that inactive suffix when completed elements no longer need it.

## Mechanism and Invariants

Cleanup waits for VM completion of an element and the same prune-batched
1,024-position boundary. It clears only the inactive stack region, preserving
live operands and published results. Capacity remains reusable. This is distinct
from the cleared stack retained by a Document across reparses.

## Scope and State

Default recursive and iterative VM streaming only. Closure has no VM operand
stack; generated Go/TypeScript have no streaming API.

## Reference Control

Build tag `zp_reference_stream_stack` restores the former inactive-reference
behavior. The combined storage tag also changes undo/prefix policy.

## Evidence and Implementation

Record 089 measures the storage controls and their combined retained-heap
reduction. It does not establish an independent universal stack-cleanup speedup
or immediate release at every commit.

Evidence records:
[089](../089-isolate-stream-prefixes-and-batch-scratch-cleanup.md).

Source owners: [`inactive VM value
cleanup`](../../../internal/engine/core/stream_stack.go),
[`vm.Machine.ClearInactive`](../../../internal/engine/vm/vm.go), [`prune-batched
commit cleanup`](../../../internal/engine/core/input.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A050](A050-streaming-state-reuse.md), [A100](A100-document-vm-value-stack.md).

[Optimization Catalog](../README.md)
