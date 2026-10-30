# 100. Relative Snapshot Position Overlays

- **Status**: Measured
- **Author**: @ornew
- **Date**: 2026-10-14

## Problem and Mechanism

Exact snapshot-record sharing requires equal absolute start/end positions. A
length-changing edit near the start of the input can shift many otherwise
unchanged records and make them ineligible for sharing. This experiment stores
per-version start/end deltas and generation metadata alongside the graph, so a
`SnapshotNode` can resolve its absolute positions from the shared record plus
the version overlay. It does not change the public spans or snapshot graph
edges.

The default no-tag representation remains unchanged: the graph has no position
overlay and an arm64 `SnapshotNode` handle is 24 bytes. With
`pego_snapshot_position_overlays`, a shared graph needing changed metadata
allocates a 12-byte overlay entry per record, and every handle grows to 32 bytes
on arm64. The opt-in build is private
and default-off; the experiment has not been adopted. The implementation and
related exact-record mechanisms are described in
[mechanism R160](mechanisms/R160-relative-snapshot-position-overlays.md),
[R130](mechanisms/R130-document-snapshot-record-sharing.md),
[R140](mechanisms/R140-share-document-snapshot-child-runs.md), and
[R150](mechanisms/R150-cache-snapshot-child-run-descriptors.md).

## Applicability

| Execution profile | Applied / Partial / Not applied / Not applicable | Reason |
|:--|:--|:--|
| Closure Document snapshots | Private, default-off | Same-source position-overlay candidate |
| Bytecode Document snapshots | Private, default-off | Candidate is graph/backend independent; timed cases used Closure only |
| Iterative Bytecode Document snapshots | Private, default-off | Candidate is graph/backend independent; timed cases used Closure only |
| Ordinary Parse/Recognize and generated parsers | Not applicable | These paths do not expose Document snapshots |

Focused correctness checks cover the supported snapshot backend and unit
combinations. Timing evidence below covers Closure/CodePoints access and capture
cases, and Closure/Bytes retained-heap cases only. Other backends and units are
not inferred from those measurements.

## Reproduction

The comparison used the no-tag absolute-position build as the reference and
`-tags pego_snapshot_position_overlays` for the candidate from the same source
revision: the commit containing this record. Its parent is `eee93b9`; no-tag
comparisons use the candidate source, rather than a different historical revision. Measurements ran on Go 1.27.1, Apple M3 Max (darwin/arm64), with
`GOMAXPROCS=16` and `GOGC=100`. Timed runs used three serialized ABBA blocks and
500 ms per benchmark sample. Each block runs reference, candidate, candidate,
reference. Divide the mean of its two candidate times by the mean of its two
reference times, then report the median and range of the three block ratios. Keep each process separate and serialize timing
runs.

```sh
go test -c -o /tmp/pego-snapshot-absolute.test ./internal/engine
go test -c -tags pego_snapshot_position_overlays \
  -o /tmp/pego-snapshot-relative.test ./internal/engine
GOMAXPROCS=16 GOGC=100 /tmp/pego-snapshot-absolute.test \
  -test.run '^$' \
  -test.bench '^BenchmarkDocumentSnapshot(PrefixEdit|MiddleEdit|MiddleEditRead|PrefixEditRead)$' \
  -test.benchmem -test.benchtime=500ms -test.count=1
GOMAXPROCS=16 GOGC=100 /tmp/pego-snapshot-relative.test \
  -test.run '^$' \
  -test.bench '^BenchmarkDocumentSnapshot(PrefixEdit|MiddleEdit|MiddleEditRead|PrefixEditRead)$' \
  -test.benchmem -test.benchtime=500ms -test.count=1

GOMAXPROCS=16 GOGC=100 go run ./bench/snapshot-retained \
  -mode snapshot -edit prepend -size 4096 -versions 33 -window 0 -backend closure -unit bytes
GOMAXPROCS=16 GOGC=100 go run -tags pego_snapshot_position_overlays \
  ./bench/snapshot-retained -mode snapshot -edit prepend \
  -size 4096 -versions 33 -window 0 -backend closure -unit bytes
```

Repeat each build in alternating order. For 64 KiB retention cases, use
`-size 65536 -versions 17`; test retention windows `-window 0`, `-window 1`,
and `-window 3`. The retained harness reports post-GC `HeapAlloc` delta over a
warmed, parsed live-Document baseline. It includes the live Document and memo
state, not only snapshot records. Raw logs remain outside Git.

## Results

The final three ABBA blocks measured edit-plus-Snapshot and post-edit reads in
Closure/CodePoints mode:

| Operation | Input | Candidate/reference median | Block ratio range |
|:--|--:|--:|--:|
| Prefix edit plus Snapshot | 4 KiB | 1.065× | 1.060–1.102× |
| Prefix edit plus Snapshot | 64 KiB | 1.091× | 1.085–1.092× |
| Middle edit plus Snapshot | 4 KiB | 1.047× | 0.962–1.077× |
| Middle edit plus Snapshot | 64 KiB | 1.029× | 0.939–1.080× |
| Middle edit, indexed `Child(j).End()` reads | 4 KiB | 1.315× | 1.249–1.340× |
| Middle edit, indexed `Child(j).End()` reads | 64 KiB | 1.350× | 1.345–1.380× |
| Prefix edit, indexed `Child(j).End()` reads | 4 KiB | 1.058× | 1.046–1.086× |
| Prefix edit, indexed `Child(j).End()` reads | 64 KiB | 1.046× | 1.042–1.057× |
| Middle edit, Materialize | 4 KiB | 1.016× | 0.892–1.112× |
| Middle edit, Materialize | 64 KiB | 1.005× | 0.937–1.111× |
| Prefix edit, Materialize | 4 KiB | 0.968× | 0.948–1.017× |
| Prefix edit, Materialize | 64 KiB | 1.044× | 0.980–1.072× |

The edit benchmark includes `Edit`, parsing, freezing and sharing, with the
previous snapshot explicitly kept live through capture. The indexed-read
benchmark sums `End()` across the wide child list; it includes child lookup and
absolute-end resolution and allocates zero bytes. Materialization and middle
capture ranges overlap parity, so their medians do not establish a speed change.
Prefix capture adds approximately 100 KB/1.31 MB allocated bytes per operation
at 4/64 KiB and 9–10 allocations. Middle capture adds approximately 0.6/9.5 KB
and one allocation; read/materialization allocations are unchanged.

For prefix edits retaining the full history, post-GC heap delta per version
fell from 543 KB to 196 KB for 33 versions of a 4 KiB input (about 64% lower),
and from 8.39 MB to 2.36 MB for 17 versions of a 64 KiB input (about 72%
lower). Keeping only the newest version instead increased the measured delta
by 4.4% and 7.6%, respectively. Keeping the newest three reduced it by about
14% and 34%. These values include the live Document and memo state. They do
not isolate the Snapshot graph from those objects.

## Correctness and Tradeoffs

The candidate preserves public absolute positions by adding version-specific
position deltas when a handle reads `Start` or `End`. It retains graph identity,
child/field edges, cycles and materialization semantics. The default root-module suite, enabled Snapshot suite and race checks, and
independent source review passed. Checks cover scalar action positions, Unicode
and invalid UTF-8 in both units, cycles, typed nils, zero spans, delta overflow,
partial budget exhaustion and cancellation during metadata construction.
Generation produced no parser changes. These are local checks; remote CI and
deployed behavior were not verified.

The retained-history fixtures show a substantial reduction when many versions
are kept after prefix edits. The benefit depends on retaining older snapshots:
the one-version window retained slightly more memory, and indexed child reads
were slower in the measured middle-edit cases. The overlay also adds per-record
metadata and enlarges handles. Timings cover only the stated Closure/CodePoints
operations; retained-heap probes cover only Closure/Bytes. Do not generalize
these results to other edit patterns, backends, units or consumers.

## Decision and Limits

Keep the candidate private and default-off. The absolute-position reference
remains the default because the measured indexed-read cost and latest-only
retention increase offset the full-history savings for workloads that do not
retain older snapshots. Reconsider only with representative retention windows
and access patterns across the supported Document backends and units.
