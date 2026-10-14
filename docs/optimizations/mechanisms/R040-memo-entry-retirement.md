# R040: Memo Entry Retirement

Removing a memo lookup slot is insufficient if its allocation slab still holds
references to the result. Retirement clears those references and makes the entry
storage reusable.

## Mechanism and Invariants

`memoTable.retire` clears node, environment, recovery-error and expectation
ownership when an entry is replaced, pruned or invalidated. Stream pruning
preserves a seed still owned by an active left-recursion frame; seed replacement
retires the old entry only after the frame installs its next best result. Free
entries can be reused without mutating any published result. This bounds
obsolete memo ownership rather than arbitrary result retention.

## Scope and State

Applied by default in the core memo allocator shared by closure and both VMs,
especially streams and Documents. Generated Go and TypeScript allocators were
not changed by record 067 and do not expose Document/stream APIs.

## Reference Control

No maintained same-source retirement-off switch exists. The historical
measurement restored the parent allocator through a source overlay, which is
distinct from a current static reference control.

## Evidence and Implementation

Record 067 measures discarded stream retention growth falling from tens or
hundreds of MiB to 34–137 KiB, and reduced allocated bytes on focused
memoized/LR-word streams. Batch timing remained within about 4%. The result is
bounded obsolete ownership on those workloads, not a total heap bound or
generated-runtime claim.

Evidence records:
[067](../067-retire-memo-entries-when-their-lookup-slots-are-removed.md).

Source owners: [`memoTable.retire, put, prune and
splice`](../../../internal/engine/memo.go), [`left-recursion seed
ownership`](../../../internal/engine/runtime.go), [`Document edit
invalidation`](../../../internal/engine/document.go).

The [comparison policy](../../design/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [M060](M060-left-recursion-growth.md), [A050](A050-streaming-state-reuse.md), [R070](R070-document-memo-edits.md).

[Optimization Catalog](../README.md)
