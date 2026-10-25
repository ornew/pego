# R130: Document Snapshot Record Sharing

Retaining several immutable Document snapshots need not duplicate records that
are exactly equal. Reuse lowers retained version storage while preserving
snapshot ownership and mutable Node independence.

## Mechanism and Invariants

Capture freezes a new graph, then compares each record with the previous live
snapshot at the corresponding traversal index. Kind, absolute positions, text,
flags, nil/empty state, child IDs and ordered fields must all match exactly; a
hash alone is insufficient. Fewer than 128 records or less than one-quarter
reuse keeps a contiguous copy fallback. A weak previous-graph reference avoids
retaining an old snapshot solely through the Document. Text is still copied for
owned snapshot storage; shifted positions can make records ineligible.

## Scope and State

Default Document snapshot capture in closure and both VMs. Generated parsers and
TypeScript do not expose the Document snapshot API. Public mutable Nodes are not
shared by this immutable-record policy.

## Reference Control

Build tag `pego_snapshot_sharing_reference` selects copied records at the same
source. It controls record sharing, not parsing, text ownership or
materialization behavior.

## Evidence and Implementation

Record 097 measures retained per-version graph storage from about 455 KB to 85
KB at 4 KiB and 7.79 MB to 1.54 MB at 64 KiB. Capture costs roughly 12%/6% more
in its closure CodePoints samples, and prefix insertion takes the low-reuse
fallback. This trades capture work for version retention; it is not an ordinary
parse speedup.

Evidence records: [097](../097-exact-document-snapshot-record-sharing.md).

Source owners: [`snapshot graph freezing and exact
sharing`](../../../internal/engine/snapshot.go), [`default
policy`](../../../internal/engine/snapshot_sharing_enabled.go), [`copy
comparator`](../../../internal/engine/snapshot_sharing_reference.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [R140](R140-share-document-snapshot-child-runs.md), [R150](R150-cache-snapshot-child-run-descriptors.md), [R090](R090-moving-document-trees.md).

[Optimization Catalog](../README.md)
