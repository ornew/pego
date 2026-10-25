# R160: Relative Snapshot Position Overlays

Shifted immutable records can share payload storage when version-specific
positions live in a separate overlay.

## Mechanism and Invariants

Capture freezes the mutable graph first, then compares corresponding traversal
indices. Payload equality excludes start/end/generation; field scalars remain
exact, including action-derived positions. Overflowing `int32` deltas reject
reuse. A 12-byte entry per node stores start/end deltas and absolute generation;
handles cache its pointer. Lookup is bounded and replays no edit history.

Relative sharing requires at least 128 nodes, 25% matching records and a reuse
allocation heuristic. That heuristic is not a retained-heap guarantee: reused
records can retain older backing arrays.

## Scope and State

Private, experimental and default-off for engine Document snapshots across
closure and both VMs. Generated parsers are outside scope. The empty default
state preserves the 64-byte graph and 24-byte handle on measured arm64;
enabled handles are 32 bytes.

## Reference Control

Build tag `pego_snapshot_position_overlays` enables the candidate. The same-source
no-tag build is the unchanged default comparator.

## Evidence and Implementation

Three serialized ABBA blocks (500 ms per benchmark invocation) used Go 1.27.1,
Apple M3 Max, `GOMAXPROCS=16` and `GOGC=100`. Closure/CodePoints prefix capture
medians cost 6.5%/9.1% more at
4/64 KiB; indexed reads cost 25–38% more after middle edits and 4–9% after
prefix edits. Materialization ranges include parity.
Closure/Bytes all-history retention falls about 64%/72%, but retaining one
version costs 4.4%/7.6% more; three-version windows improve. Heap totals include
the live Document/cache. Other backends/workloads remain unmeasured; no adoption is
established.

See [record 100](../100-relative-snapshot-position-overlays.md),
[snapshot design](../../design/records/032-versioned-document-snapshots.md),
[`shareSnapshotGraph`](../../../internal/engine/snapshot.go), and the
[enabled](../../../internal/engine/snapshot_position_enabled.go)/
[default](../../../internal/engine/snapshot_position_reference.go) representations.

Related mechanisms: [R130](R130-document-snapshot-record-sharing.md),
[R140](R140-share-document-snapshot-child-runs.md),
[R150](R150-cache-snapshot-child-run-descriptors.md).

[Optimization Catalog](../README.md)
