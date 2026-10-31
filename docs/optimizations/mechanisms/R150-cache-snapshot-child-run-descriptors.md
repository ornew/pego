# R150: Cached Snapshot Child Descriptors

Indexed snapshot child access can avoid a sidecar lookup for every edge by
caching immutable record and child-run descriptor pointers in the opaque node
handle.

## Mechanism and Invariants

All handle construction routes use the same cached representation. Random Child
resolves a run directly when chunked and the flat record otherwise; identity
includes the snapshot graph, preserving same-graph sharing/cycles and separating
versions. Materialize expands runs directly. The descriptor remains immutable
and belongs to the graph. Handles grow from 16 to 24 bytes on measured arm64;
caller-owned collections and node-valued Fields pay that cost.

## Scope and State

Default Document snapshot handles for all three engine backends, including flat
and shared-run graphs. Generated Go/TypeScript have no SnapshotNode path.

## Reference Control

No cache-off same-source control is maintained.
`zp_snapshot_child_chunks_reference` disables run sharing while retaining
cached handles; it cannot attribute caching alone.

## Evidence and Implementation

Record 099 reports indexed-read ratios of 0.39–0.41 against historical main
without the cache; that comparison also changes child-run storage and cannot
isolate caching. Same-source read ratios 0.89–0.95 concern run sharing with
caching held fixed. Materialization comparisons against historical main are
noisy. The handle size increase is a concrete cost even where timing is
unmeasured.

Evidence records: [099](../099-share-document-snapshot-child-runs.md).

Source owners: [`SnapshotNode, snapshotHandle and
Child`](../../../internal/engine/document/document_snapshot.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [R130](R130-document-snapshot-record-sharing.md), [R140](R140-share-document-snapshot-child-runs.md).

[Optimization Catalog](../README.md)
