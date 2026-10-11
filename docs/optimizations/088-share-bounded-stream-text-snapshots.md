# 88. Share bounded stream text snapshots

**Status:** Candidate  
**Scope:** Closure, recursive VM and iterative VM streaming. Generated parsers
have no streaming API.

The candidate keeps one immutable normalized snapshot of already-loaded input
positions. Text wholly inside it can share its backing string. A cache miss may
build a new snapshot at the requested start when the range is at most 1,024
positions and at least 64 positions are already loaded. Empty requests return
an empty string; oversized requests and requests with too little loaded input
use the existing detached copy. See [design 027](../design/027-bounded-stream-text-snapshots.md)
for the ownership and boundary contract.
Cache construction never reads from the reader. Byte snapshots preserve raw
bytes; code-point snapshots preserve existing U+FFFD substitution for malformed
input and use parser-owned offsets into the normalized string. The cache owner
is released when compaction makes it obsolete; returned strings remain valid
and can retain at most one snapshot (up to 4,096 bytes) each. The input buffer
and discard policy are unchanged.

A same-source build with `pego_reference_stream_text` selects the existing copy
path. The candidate implementation is on a development branch; mainline still
uses the copy path. It adds a 56-byte field on 64-bit and 28-byte field on
32-bit inputs. The field exists in both same-source reference and candidate,
so focused ratios do not measure this addition relative to the parent. These
are struct-layout sizes, not allocated heap size classes.

## Evidence

Five alternating one-second rounds on 2026-10-11 with Go 1.27.1, Apple M3
Max, `darwin/arm64`, `GOMAXPROCS=4`, compare the candidate with both its
same-source reference and parent revision `00245ef`. Ratios below are medians
of paired time ratios; ranges show the minimum and maximum pair, not confidence
intervals.

The 50,001-record CSV stream benchmark reduced allocations as follows; bytes
per operation fell 1.1–1.2%. All same-source candidate/reference timing ranges
include parity. Parent-relative ratios were 0.981, 0.973 and 0.986; only the
recursive VM was below parity in all five parent comparisons.

| Backend | Candidate/reference time | Paired range | Allocations/op, reference → candidate |
|:--|--:|:--|--:|
| Closure | 0.984 | 0.946–1.009 | 306,046 → 8,159 |
| Recursive VM | 0.996 | 0.950–1.005 | 308,128 → 10,240 |
| Iterative VM | 1.000 | 0.949–1.029 | 308,127 → 10,240 |

For a Unicode stream grammar with a live root capture, the 32-record case
reduced allocations by 63–75%. CodePoints bytes/op increased 1.30–1.42%; Bytes
increased 0.05%. At 5,000 records, allocations fell 94%; CodePoints bytes/op
fell 0.35–0.37%, while Bytes increased 0.05%. All timing ranges included
parity. Nine batch controls across JSON, CSV and Minilang also had ranges that
included parity, so they establish no general batch-time effect.

A five-round retained-heap check with GC measured first-row retention of about
88–89 KB for 5,000- and 50,000-row streams. Median increases over the reference
were 1,120 bytes and 160 bytes; whole-result retention was about 5.02 vs 5.04
MB and 49.84 vs 50.08 MB. These measurements include GC and are not throughput
results. They do not establish a maximum live-heap bound for all outstanding
values or measure stale undo-trail references.

## Validation and reproduction

The focused tests compare retained text and parse results with whole-input
parsing across all three engine runtimes and both units. They cover malformed
UTF-8, random ranges, boundaries, oversized requests, requests with too little
loaded input, short reads, early EOF and near-`MaxInt` positions. A one-byte
reader check confirms that text access does not trigger another read. Linux/386
optimized/reference tests and boundary tests pass, and parser regeneration has
no output changes.
The full root suite, vet, all ten parser-module suites and site tests pass.
Independent review found no remaining correctness or ownership issue.

For the main CSV benchmark, build the same revision with and without the
reference tag, then run `BenchmarkStream` for closure, bytecode and iterative
backends in alternating order for five one-second pairs:

```sh
go test -c -o /tmp/pego-stream-text-candidate.test ./bench
go test -tags=pego_reference_stream_text -c \
  -o /tmp/pego-stream-text-reference.test ./bench
(cd bench && GOMAXPROCS=4 /tmp/pego-stream-text-reference.test \
  -test.run='^$' -test.bench='^BenchmarkStream$/(closure|bytecode|iterative)$' \
  -test.benchtime=1s -test.benchmem)
(cd bench && GOMAXPROCS=4 /tmp/pego-stream-text-candidate.test \
  -test.run='^$' -test.bench='^BenchmarkStream$/(closure|bytecode|iterative)$' \
  -test.benchtime=1s -test.benchmem)
```

The supplemental Unicode stream workloads use `BenchmarkStreamFrameScratch`
from the engine package. Build both binaries at the same revision and alternate
reference and candidate runs for five pairs:

```sh
go test -c -o /tmp/pego-stream-text-engine-candidate.test ./internal/engine
go test -tags=pego_reference_stream_text -c \
  -o /tmp/pego-stream-text-engine-reference.test ./internal/engine
GOMAXPROCS=4 /tmp/pego-stream-text-engine-reference.test -test.run='^$' \
  -test.bench='^BenchmarkStreamFrameScratch$' -test.benchtime=1s -test.benchmem
GOMAXPROCS=4 /tmp/pego-stream-text-engine-candidate.test -test.run='^$' \
  -test.bench='^BenchmarkStreamFrameScratch$' -test.benchtime=1s -test.benchmem
```

The retained-heap check uses one iteration of `BenchmarkStreamTextRetainedHeap`
from the same binaries:

```sh
GOMAXPROCS=4 /tmp/pego-stream-text-engine-reference.test -test.run='^$' \
  -test.bench='^BenchmarkStreamTextRetainedHeap$' -test.benchtime=1x
GOMAXPROCS=4 /tmp/pego-stream-text-engine-candidate.test -test.run='^$' \
  -test.bench='^BenchmarkStreamTextRetainedHeap$' -test.benchtime=1x
```

This benchmark forces GC and includes GC time; it observes retained memory,
not throughput.

Batch controls use `BenchmarkParse` for JSON, CSV and Minilang. Build those
benchmark binaries from the same source revision and alternate reference and
candidate order, keeping backend and position unit fixed within each pair.
A clean 131-case, three-sample full benchmark checkpoint completed on source
`efcd2db` on 2026-10-11 at default GOMAXPROCS=16. It is a descriptive snapshot,
not attribution to this optimization. Compared unpaired with the prior full
snapshot at `3da3d4a`, JSON no-AST load time was 12.37% higher, with the same
allocation count and three fewer bytes. A focused five-round comparison of
that path gave a 1.019 median time ratio and 0.971–1.099 paired range, which
includes parity; the unpaired difference did not recur consistently. The
candidate remains on its development branch. Undo-trail and old live-root
sibling retention are broader follow-ups outside this snapshot's ownership
scope.

## Limits

Text outside the snapshot still copies. A returned substring may keep a bounded
snapshot alive after compaction, and multiple outstanding strings can retain
multiple snapshots. The measurements are specific to these stream and batch
workloads; they do not establish universal throughput gains or eliminate input
text copying. Undo-trail and old live-root sibling references have separate
ownership and cleanup behavior.
