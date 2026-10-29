# R140: Document Snapshot Child Run Sharing

A changed wide snapshot record can have nearly all of its child IDs unchanged.
Sharing those immutable runs avoids copying a full child-ID slice into every
retained version.

## Mechanism and Invariants

After exact-record sharing, changed records with at least 1,024 children compare
exact 256-ID runs with the previous live graph. At least 75% of runs must match;
otherwise the list stays flat. Matching runs reuse immutable backing and changed
runs copy into private exact-size slices. A sidecar descriptor exists only for
chunked records. Materialize expands an independent mutable Children slice. Weak
previous ownership, exact ID checks and bounded copy charging prevent stale or
partially published snapshots.

## Scope and State

Default eligible closure/recursive-VM/iterative-VM Document snapshot graphs.
Generated Go/TypeScript and ordinary Parse/Recognize do not expose this snapshot
path. Small graphs, low record reuse and length-changing shifts often retain the
flat fallback.

## Reference Control

Private build tag `pego_snapshot_child_chunks_reference` keeps flat child
slices. Cached handles remain in both variants, so the control isolates child
storage rather than descriptor caching.

## Evidence and Implementation

Record 099 measures about 34%/32% lower additional retained bytes per version on
4/64 KiB wide-child fixtures. Capture ratios have inconsistent signs; eligible
captures add 4/5 allocations and materialization costs up to about 7% more in
same-source samples. Other grammars/backends/units/length-changing edits are
unmeasured. The 24-byte handle and read results belong to the separate
cached-descriptor mechanism.

Evidence records: [099](../099-share-document-snapshot-child-runs.md).

Source owners: [`shareSnapshotChildren and
snapshotChildChunks`](../../../internal/engine/document/document_snapshot.go), [`flat child
comparator`](../../../internal/engine/document/document_snapshot_child_chunks_reference.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [R130](R130-document-snapshot-record-sharing.md), [R150](R150-cache-snapshot-child-run-descriptors.md).

[Optimization Catalog](../README.md)
