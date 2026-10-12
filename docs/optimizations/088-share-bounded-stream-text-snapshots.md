# 88. Share Bounded Stream Text Snapshots

**Status:** Active

**Scope:** Closure, recursive VM and iterative VM streaming. Generated parsers
have no streaming API.

The implementation keeps one immutable text snapshot of already-loaded input
positions. Text wholly inside it can share its backing string. A cache miss
may build a new snapshot at the requested start when the range is at most
1,024 positions and at least 64 positions are already loaded. Empty requests
return an empty string; oversized requests and requests with too little loaded
input use the existing detached copy. See [design
026](../design/026-bounded-stream-text-snapshots.md) for the ownership and
boundary contract. Cache construction never reads from the reader. Byte
snapshots preserve raw bytes; code-point snapshots preserve existing U+FFFD
substitution for malformed input and use parser-owned offsets into the
normalized string. The cache owner is released when compaction makes it
obsolete; returned strings remain valid and can retain at most one snapshot
(up to 4,096 bytes) each. The input buffer and discard policy are unchanged.

A same-source build with `pego_reference_stream_text` selects the existing
copy path. The implementation adds a 56-byte field on 64-bit and 28-byte field
on 32-bit inputs. This field is present in both same-source default and
reference builds; the exact-parent comparison includes its layout cost. These
are struct-layout sizes, not allocated heap size classes.

## Evidence

Measurements use the bounded-snapshot source based on main revision `b18832d`.
Thirty-three timed cases used five alternating 500 ms pairs on Go 1.27.1,
Apple M3 Max (`darwin/arm64`), with `GOMAXPROCS=4`. All timed cases compare
the default with the same-source copying reference and exact parent. Ratios
are medians of paired time ratios, and ranges are the minimum and maximum
pair, not confidence intervals.

For 50,001 CSV records, stream allocations fell from 306,046/308,148/308,138
to 8,159/10,261/10,249 for closure, recursive VM and iterative VM. Compared
with the same-source reference, bytes/op decreased 1.1–1.2%. Default/parent
median time ratios were 0.979/0.960/0.953; all three ranges were below parity.
Default/reference ratios were 0.993/0.950/0.968; the closure range included
parity, while recursive and iterative VM were faster than the reference across
all five pairs. This is workload-specific evidence, not a general speed claim.

For 32-row Unicode streams, allocations fell 63–75% versus the same-source
reference; CodePoints bytes/op rose 1.30–1.42% and Bytes rose about 0.05%. For
5,000 rows, allocations fell 94% versus the same-source reference; CodePoints
bytes/op fell 0.35–0.37% and Bytes rose about 0.05%. Most paired time ranges
included parity. The 32-row recursive-VM CodePoints result was a repeatable
exception: default/parent was 1.033 [1.024–1.055] and default/reference was
1.030 [1.019–1.050], with allocations falling from 138 to 43 and bytes/op
rising from 114,008 to 115,504. This small-workload cost is a tradeoff against
the stream allocation reductions.

Eighteen JSON, CSV and Minilang batch cases covered both position units and
all three backends. No case was slower across all five exact-parent pairs.
Some CSV closure comparisons were slower than the same-source reference across
all five pairs, so the results show no general batch-time gain. Allocation and
byte differences were small and variable; they do not establish zero overhead.

A five-round closure/CodePoints retained-heap check with GC measured default/reference first-row
retention at 89,440/88,304 bytes for 5,000 rows and 88,560/88,400 bytes for
50,000 rows. Whole-result retention was 5.020/5.043 MB and 49.839/50.078 MB,
about 0.5% lower for the default in both all-result cases. These measurements
include GC time; they do not bound all possible outstanding values or measure
throughput.

## Validation and Reproduction

The focused tests compare retained text and parse results with whole-input
parsing across all three engine runtimes and both units. They cover malformed
UTF-8, random ranges, boundaries, oversized requests, requests with too little
loaded input, short reads, early EOF and near-`MaxInt` positions. A one-byte
reader check confirms that text access does not trigger another read. Parser
regeneration has no output changes. Current integration checks on the
bounded-snapshot source based on `b18832d` pass: root tests (engine 602.313
s), generator freshness, all ten parser modules, vet and site tests. Focused
default/reference stream tests pass for both units and all three engine
backends, including the frame-reference interaction. Independent source review
passes.

Build the default and `pego_reference_stream_text` binaries from the same
bounded-snapshot source based on `b18832d`. Build exact-parent binaries from a
separate `b18832d` worktree with the same Go version. For each case, run
parent, reference and default in alternating ABC/CBA order for five pairs;
keep backend and unit fixed. Each timed case uses a 500 ms benchmark interval:

```sh
git worktree add --detach /tmp/pego-stream-text-parent b18832d
(cd /tmp/pego-stream-text-parent && go test -c \
  -o /tmp/pego-stream-text-parent.test ./bench)
(cd /tmp/pego-stream-text-parent && go test -c \
  -o /tmp/pego-stream-text-engine-parent.test ./internal/engine)
go test -c -o /tmp/pego-stream-text-candidate.test ./bench
go test -tags=pego_reference_stream_text -c \
  -o /tmp/pego-stream-text-reference.test ./bench
(cd bench && GOMAXPROCS=4 /tmp/pego-stream-text-parent.test \
  -test.run='^$' -test.bench='^BenchmarkStream$/(closure|bytecode|iterative)$' \
  -test.benchtime=500ms -test.benchmem)
(cd bench && GOMAXPROCS=4 /tmp/pego-stream-text-reference.test \
  -test.run='^$' -test.bench='^BenchmarkStream$/(closure|bytecode|iterative)$' \
  -test.benchtime=500ms -test.benchmem)
(cd bench && GOMAXPROCS=4 /tmp/pego-stream-text-candidate.test \
  -test.run='^$' -test.bench='^BenchmarkStream$/(closure|bytecode|iterative)$' \
  -test.benchtime=500ms -test.benchmem)
```

The Unicode stream workload uses `BenchmarkStreamFrameScratch` from the engine
package. Run the corresponding parent, same-source reference and default
binaries in alternating ABC/CBA order for five 500 ms pairs:

```sh
go test -c -o /tmp/pego-stream-text-engine-candidate.test ./internal/engine
go test -tags=pego_reference_stream_text -c \
  -o /tmp/pego-stream-text-engine-reference.test ./internal/engine
GOMAXPROCS=4 /tmp/pego-stream-text-engine-parent.test -test.run='^$' \
  -test.bench='^BenchmarkStreamFrameScratch$' -test.benchtime=500ms -test.benchmem
GOMAXPROCS=4 /tmp/pego-stream-text-engine-reference.test -test.run='^$' \
  -test.bench='^BenchmarkStreamFrameScratch$' -test.benchtime=500ms -test.benchmem
GOMAXPROCS=4 /tmp/pego-stream-text-engine-candidate.test -test.run='^$' \
  -test.bench='^BenchmarkStreamFrameScratch$' -test.benchtime=500ms -test.benchmem
```

The retained-heap check uses one iteration of
`BenchmarkStreamTextRetainedHeap` from the same-source reference and default
engine binaries in five alternating rounds:

```sh
GOMAXPROCS=4 /tmp/pego-stream-text-engine-reference.test -test.run='^$' \
  -test.bench='^BenchmarkStreamTextRetainedHeap$' -test.benchtime=1x
GOMAXPROCS=4 /tmp/pego-stream-text-engine-candidate.test -test.run='^$' \
  -test.bench='^BenchmarkStreamTextRetainedHeap$' -test.benchtime=1x
```

This benchmark forces GC and includes GC time; it observes retained memory,
not throughput. The focused 32-row recursive-VM CodePoints confirmation used
five alternating one-second pairs.

The same three bench binaries run `BenchmarkParse` for JSON, CSV and Minilang
with
`-test.bench='^BenchmarkParse$/(JSON|CSV|Minilang)$/(closure|bytecode|iterative)$/codepoints$'`
or the corresponding `bytes` filter. Keep backend and unit fixed within each
comparison, and alternate parent/reference/default order across five 500 ms
pairs.

## Limits

Text outside the snapshot still copies. A returned substring may keep a
bounded snapshot alive after compaction, and multiple outstanding strings can
retain multiple snapshots. The measurements are specific to these stream and
batch workloads; they do not establish universal throughput gains or eliminate
input text copying. Undo-trail and old live-root sibling references have
separate ownership and cleanup behavior.
