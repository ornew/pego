# 097. Exact Document Snapshot Record Sharing

- **Status**: Adopted
- **Date**: 2026-10-14

## Problem and Mechanism

Document snapshots previously copied every record in the immutable result
graph. The selected implementation reuses exact immutable records from the
previous live snapshot while preserving the public mutable `Node` and owned
snapshot contracts.

Every capture first freezes the current parse result into a new graph. It then
compares records at corresponding traversal indices with the previous snapshot
graph. A record is reusable only when its interned kind, absolute start/end,
text, internal flags, nil-versus-empty state, ordered child IDs, and ordered
field names and values match. String and field comparisons verify exact
contents; hashes alone are insufficient. Reused records are immutable. New or
changed records are copied into the new graph. If the graph has fewer than 128
records or fewer than one quarter of records match, the capture keeps the
contiguous copy representation instead of paying for a per-version reference
directory.

The `Document` keeps a weak reference to its previous snapshot graph, so the
Document alone does not retain an old snapshot. A new snapshot may retain
immutable records still referenced by its own graph. Snapshot text is copied
separately; parser Nodes, input buffers, memo state, and mutable slices are
never shared. Absolute spans remain stored directly, so a length-changing edit
that shifts positions naturally falls back to copying where records no longer
match. The same mechanism applies to Snapshot capture after any supported core
Document backend; it does not affect ordinary Parse or generated parsers.

## Applicability

| Execution profile | Applied / Partial / Not applied / Not applicable | Reason |
|:--|:--|:--|
| Closure-backed Document snapshots | Applied | Exact frozen-record comparison and low-reuse fallback |
| Bytecode-backed Document snapshots | Applied | Sharing is over the backend-independent frozen graph |
| Iterative-Bytecode-backed Document snapshots | Applied | Sharing is over the backend-independent frozen graph |
| Ordinary Parse/Recognize and generated parsers | Not applicable | These paths do not expose Document snapshots |

The retained-memory figures below cover Closure/CodePoints fixtures. Capture
timing was measured only for Closure/CodePoints; no performance result is
claimed for other backends or units.

## Reproduction

The measurements used Go 1.27.1 on an Apple M3 Max (darwin/arm64), with
`GOMAXPROCS=16` and `GOGC=100`. The default build uses snapshot sharing; the
`pego_snapshot_sharing_reference` build tag selects the copy baseline. Build
binaries before timing and alternate fresh processes. Snapshot capture cases
use three alternating 300 ms pairs. Retention cases use three alternating
process pairs with forced GC.

```sh
GOMAXPROCS=16 GOGC=100 go test -tags pego_snapshot_sharing_reference \
  ./internal/engine -run '^$' \
  -bench '^BenchmarkDocumentSnapshotCapture$' -benchmem -benchtime=300ms -count=1
GOMAXPROCS=16 GOGC=100 go test ./internal/engine -run '^$' \
  -bench '^BenchmarkDocumentSnapshotCapture$' \
  -benchmem -benchtime=300ms -count=1

GOMAXPROCS=16 GOGC=100 go run -tags pego_snapshot_sharing_reference \
  ./bench/snapshot-retained \
  -mode snapshot -edit replace-first -size 4096 -versions 64
GOMAXPROCS=16 GOGC=100 go run \
  ./bench/snapshot-retained -mode snapshot -edit replace-first \
  -size 4096 -versions 64
```

For the 64 KiB retained case use `-size 65536 -versions 16`; for the
length-changing prefix fallback use `-edit prepend`. Keep reference and
default-sharing processes alternating. The prefix-edit benchmark is
`BenchmarkDocumentSnapshotPrefixEdit`. Routine raw logs remain outside Git.

## Results

After a same-length one-character edit, the forced-GC retention harness warmed
and parsed the Document, recorded baseline `HeapAlloc`, then forced GC after
capturing versions and measured the `HeapAlloc` delta. With `-retain=true`, all
captured versions stayed alive through measurement. The values below are
additional post-GC heap per version over the warmed-Document baseline, not
total live heap:

| Workload | Copy mode | Sharing mode | Measurement |
|:--|--:|--:|:--|
| 4 KiB input, 64 retained versions | 455.2–455.6 KB/version | 84.6–84.7 KB/version | Three alternating process pairs |
| 64 KiB input, 16 retained versions | 7.792–7.794 MB/version | 1.540–1.541 MB/version | Three alternating process pairs |

Three alternating 300 ms Closure/CodePoints capture pairs showed about 12%
higher median capture time for 4 KiB inputs (paired results 10.9–14.8% higher)
and about 6% for 64 KiB inputs (3.7–14.4% higher), with about 2% more `B/op`.
`Materialize` was broadly similar. With a length-changing prefix insertion, the
low-reuse fallback measured an additional 8.386–8.388 MB/version in both modes
for 16 retained 64 KiB versions. Snapshot-plus-prefix-edit timing differed by
0–3% across the 4 KiB and 64 KiB cases, within observed noise, with similar
`B/op`. With `-retain=false`, each captured snapshot is discarded immediately;
the post-GC heap delta over baseline was only tens of KB in both modes. Five
alternating 300 ms ordinary Document creation/Parse pairs with sharing disabled
versus main had the same allocation counts and `B/op`,
with no consistent timing difference.

## Correctness and Tradeoffs

Correctness depends on exact comparison of the complete immutable record,
including absolute positions and graph edges. Matching only node pointers,
generations, spans, or hashes could reuse stale values because legacy `Node`
fields remain caller-mutable and Document reparses can move Nodes in place.
The implementation still freezes and scans the whole graph, so this mechanism
reduces retained record duplication rather than eliminating capture work.

The copy fallback avoids a reference directory when the graph is small or reuse
is low. A prefix shift demonstrates that absolute-span changes can make sharing
ineligible without adding a material retention cost in the tested case. Capture
was slower in the measured same-length-edit cases; adoption accepts that
scoped cost for the substantial retained-memory reduction in those fixtures.
Traversal, serialization, larger inputs, other edit patterns, other units and
cross-backend performance remain unmeasured. Do not extrapolate these figures
to arbitrary Documents.

## Decision and Limits

Adopt exact immutable record sharing for Document snapshots, with the low-reuse
copy fallback. Keep absolute spans, graph identity, cycles, nil/empty shape,
diagnostics and snapshot ownership unchanged. Relative spans and persistent
child sequences are separate designs and are not implied by this mechanism.
