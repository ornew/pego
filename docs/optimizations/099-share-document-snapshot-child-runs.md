# 099. Share Document Snapshot Child Runs

- **Status**: Adopted
- **Date**: 2026-10-14

## Problem and Mechanism

Exact record sharing reuses an entire immutable snapshot record only when all of
its metadata and children match. A same-length edit can change a repetition
record's generation while leaving nearly all of its child IDs unchanged. Copying
the whole child-ID slice into every retained version then costs space
proportional to the repetition length.

For changed records with at least 1,024 child IDs, snapshot capture compares
256-ID runs with the previous live snapshot. When at least three quarters of the
runs match, the new record reuses those immutable runs and copies changed runs
into exact-size slices. The chunk descriptor is stored only for affected
records; ordinary flat records keep their previous size and representation.
Random child lookup remains constant-time, and materialization still creates
an independent mutable `Children` slice. A weak previous-graph reference
retains no dropped version by itself. This mechanism runs after the exact
record-sharing decision, so small graphs, low record reuse, short child lists,
and low run reuse keep the flat representation.

Opaque node handles cache their immutable record and optional run descriptor,
avoiding a sidecar-map lookup for every indexed child. Materialization expands
each run directly rather than consulting the map for each edge.

## Applicability

| Execution profile | Applied / Partial / Not applied / Not applicable | Reason |
|:--|:--|:--|
| Closure-backed Document snapshots | Partial | Changed high-fanout records with at least 75% matching child runs |
| Bytecode-backed Document snapshots | Partial | Sharing is over the backend-independent frozen graph |
| Iterative-Bytecode-backed Document snapshots | Partial | Sharing is over the backend-independent frozen graph |
| Ordinary Parse/Recognize and generated parsers | Not applicable | These paths do not expose Document snapshots |

## Reproduction

The baseline is the same source built with
`-tags pego_snapshot_child_chunks_reference`; that private build keeps
flat child slices. The ordinary-path control is main commit
`7c3569a4aea5a9f86f1d6ede7c9ca96647da1f5b`. Measurements used Go 1.27.1
on an Apple M3 Max (darwin/arm64), `GOMAXPROCS=16`, `GOGC=100`, Closure,
and the grammar `rule main = item* $$; rule item = [a-z]` with all `a`
input. The middle-edit benchmark uses CodePoints; the retained-heap and
ordinary-capture cases use Bytes. Runs were serialized and alternated
candidate/reference/reference/candidate in three pairs. Same-source timed
cases used 200 ms per process; historical indexed-read/materialization controls
used 150 ms. Retained-heap cases used prebuilt binaries, forced GC, and 33 or
17 live versions. The middle-edit benchmark explicitly keeps the previous
snapshot live until the next capture completes.

```sh
GOMAXPROCS=16 GOGC=100 go test ./internal/engine -run '^$' \
  -bench '^BenchmarkDocumentSnapshotMiddleEdit$' -benchtime=200ms -count=1
GOMAXPROCS=16 GOGC=100 go test -tags pego_snapshot_child_chunks_reference ./internal/engine \
  -run '^$' -bench '^BenchmarkDocumentSnapshotMiddleEdit$' \
  -benchtime=200ms -count=1
GOMAXPROCS=16 GOGC=100 go run ./bench/snapshot-retained -mode snapshot -edit replace-middle \
  -size 4096 -versions 33 -backend closure -unit bytes
GOMAXPROCS=16 GOGC=100 go run -tags pego_snapshot_child_chunks_reference ./bench/snapshot-retained \
  -mode snapshot -edit replace-middle -size 4096 -versions 33 \
  -backend closure -unit bytes
```

For the larger retained case use `-size 65536 -versions 17`. To compare
ordinary capture and materialization, run
`BenchmarkDocumentSnapshotCapture/bytes/closure` against the pinned main
commit. `BenchmarkDocumentSnapshotMiddleEditRead` measures indexed child reads
and materialization after an eligible edit. Overlay this benchmark source onto
the pinned main checkout for a historical comparison. Routine raw output is
kept outside Git.

## Results

Additional live heap over a warmed Document, divided by all retained versions:

| Input and live versions | Flat reference | Shared runs | Reduction |
|:--|--:|--:|--:|
| 4 KiB, 33 versions | 90.2–90.5 KB/version | 59.0–59.3 KB/version | About 34% |
| 64 KiB, 17 versions | 1.517–1.518 MB/version | 1.029–1.030 MB/version | About 32% |

Three alternating pairs of middle one-character edits found 4 KiB capture
ratios from 0.96 to 1.08 relative to the flat reference; 64 KiB ratios were
0.97–1.04. The pair medians were 1.05 and 1.02, with inconsistent signs.
Eligible captures add four allocations at 4 KiB and five at 64 KiB; `B/op`
increases by roughly 0.6 KB and 7 KB, respectively. Against pinned main,
ordinary 4 KiB no-edit Snapshot capture ratios were 0.95–1.05, and 64 KiB
ratios were 0.97–1.03, with equal allocation counts and `B/op`. No clear
ordinary-path timing change is established.

With cached handles enabled in both same-source builds, indexed traversal of
the edited wide child list took 0.91–0.93 times flat-reference time at 4 KiB
and 0.89–0.95 at 64 KiB, with zero allocations. Materialization ratios were
1.00–1.07 at 4 KiB and 1.03–1.07 at 64 KiB, with equal allocations. A separate
comparison against pinned main, which lacks the handle cache, gave indexed-read
ratios of 0.39–0.41; this combined result does not isolate child-run sharing.
Historical materialization ratios were noisy, ranging 0.90–1.28 at 4 KiB and
0.96–1.12 at 64 KiB, so they establish no consistent historical speedup.

## Correctness and Tradeoffs

Child IDs are compared exactly; one equal hash or matching span is not enough.
Changed runs are private, and copy work is charged in bounded units so a work
limit or cancellation cannot publish a partial snapshot. Indexed access,
nil child slots, prior versions, cycles and later mutable materialization keep
their existing semantics. A reused run can retain its older flat backing
array, but it does not retain an old parser or graph solely through a chunk;
this cost is included in the retained-heap measurements.

The sidecar descriptor adds allocations for an eligible changed record and
does not improve capture from a single version. The cached opaque handle grows
from 16 to 24 bytes on arm64; snapshots do not store one handle per record,
but caller-owned handle collections and node-valued `Fields` results pay this
cost. It helps histories containing wide, mostly stable child sequences.
The measured 64 KiB materialization cost is retained as a tradeoff.
Other grammars, backends, position units,
capture-heavy fields, and length-changing edits have not been measured for
this mechanism.

## Decision and Limits

Adopt fixed-size immutable child-run sharing for eligible Document snapshots.
Keep the flat reference build tag for same-source comparisons. Relative spans
remain a separate unimplemented design; this optimization does not shift or
reinterpret positions.
