# 85. Reuse stream capture frames at element commit

**Status:** Candidate; implemented on a development branch, awaiting integration.

**Scope:** closure, recursive VM and iterative VM stream parsing. Generated
Node, typed and TypeScript parsers do not expose a streaming API.

The old allocator advanced through frame-slab slots as elements were parsed;
dead nested scopes stayed in consumed slots until a split replaced the slab,
and splitting could discard its unused suffix. At element commit, the new
path clears dead frames and rewinds the used prefix of the existing slab while
preserving the live top-level frame. Capture value arrays and result child
slices remain separately allocated and immutable, so reuse cannot change an
emitted node. The implementation adds no parser field. The
`pego_reference_stream_frames` build tag selects the former allocation path at
build time, without a runtime selector.

## Evidence

Five alternating 300 ms pairs on 2026-10-11 compared prebuilt reference and
optimized binaries on Go 1.27.1, `darwin/arm64`, Apple M3 Max (16 CPUs), with
the host otherwise idle. Ratios are the median of five optimized/reference
time ratios; ranges contain all five pairs. The standard CSV stream benchmark
parses 50,001 records. All three backends allocate fewer bytes; paired timing
ranges overlap parity.

| Backend | Time ratio | Paired range | Bytes/op | Allocs/op |
|:--|--:|:--|--:|--:|
| Closure | 0.988 | 0.874–1.044 | 103,569,504 → 90,513,742 | 308,050 → 306,050 |
| Recursive VM | 0.994 | 0.909–1.001 | 112,172,816 → 99,115,406 | 310,179 → 308,178 |
| Iterative VM | 1.000 | 0.929–1.032 | 112,168,442 → 99,112,342 | 310,150 → 308,149 |

A supplemental Unicode grammar with a live root capture exercised 32- and
5,000-record streams in both position units. Allocated bytes/op fell by about
5.4–6.0% for 32 records and 12.4–12.9% for 5,000; all paired timing ranges
overlapped parity. The initial 18 JSON, CSV and Minilang batch combinations
had median ratios of 0.961–1.025; 17 ranges included parity, while CSV closure
Bytes stayed below parity. A five-pair, 1 s Minilang follow-up had median
ratios of 0.983–1.008; five of six ranges included parity, while closure
CodePoints stayed below parity. These controls do not establish a general
batch-time gain or regression.

The reported `B/op` values are cumulative allocated bytes per parse, not peak
or retained heap. No peak/live-memory measurement was collected. The change
reuses frame structs only; it does not remove input or capture-value copying.

## Validation and reproduction

Completion-path tests cover failed final elements, callback errors,
zero-progress and empty streams, and streams without capture scopes. Retained
results are compared with whole-input parsing across more than one frame chunk
for CST and action-built trees. The tests run across all three engine backends
and both position units. A lifecycle test verifies dead frames are cleared,
the live root survives, and the next element reuses a cleared frame with fresh
value storage. Focused Linux/386 tests pass in both optimized and reference
builds. Native 64-bit root tests, all parser-module suites, and regeneration of
all 17 tracked generated parsers pass.

Build both benchmark binaries from the same revision; the reference build tag
selects the former allocator:

```sh
go test -c -o /tmp/pego-stream-optimized.test ./bench
go test -tags=pego_reference_stream_frames -c \
  -o /tmp/pego-stream-reference.test ./bench
```

From `bench/`, run the three engine sub-benchmarks in alternating order for
five pairs, reversing the first binary on each pair:

```sh
/tmp/pego-stream-reference.test -test.run='^$' \
  -test.bench='^BenchmarkStream$/(closure|bytecode|iterative)$' \
  -test.benchtime=300ms -test.benchmem
/tmp/pego-stream-optimized.test -test.run='^$' \
  -test.bench='^BenchmarkStream$/(closure|bytecode|iterative)$' \
  -test.benchtime=300ms -test.benchmem
```

The supplemental Unicode benchmark is `BenchmarkStreamFrameScratch` in
`internal/engine`. Build its binaries with and without the same tag:

```sh
go test -c -o /tmp/pego-stream-engine-optimized.test ./internal/engine
go test -tags=pego_reference_stream_frames -c \
  -o /tmp/pego-stream-engine-reference.test ./internal/engine
/tmp/pego-stream-engine-reference.test -test.run='^$' \
  -test.bench='^BenchmarkStreamFrameScratch$' -test.benchtime=300ms -test.benchmem
```

Alternate reference and optimized binaries for five pairs for both stream
benchmarks. Batch controls use `BenchmarkParse` for JSON, CSV and Minilang in
each backend and position unit; the Minilang follow-up uses 1 s per sample.
Keep the source revision, binary, position unit and backend fixed within each
pair.
