# 87. Store deferred-memo seen positions sparsely

**Status:** Candidate on `perf/sparse-deferred-memo`; the bounded typed-reuse
policy is implemented and locally validated. It is not integrated into main.

Deferred-memo bookkeeping can be sparse when a grammar has many rules but a
parse visits only a small subset of rule/position pairs. The candidate selects
sparse storage once per run when the grammar has more than 64 rules and the
input has more than 1,024 decoded units. It stores seen positions in
1,024-position pages grouped into stable 64-page chunks. Each Go runtime caches
the latest page and reuses bounded page and directory storage with inline
`cachedSeenPage` and `markSeenPage` helpers.
TypeScript uses integer page offsets rather than allocating a typed array for
each page. Smaller runs retain the dense path.

The core reference build uses `pego_reference_sparse_memo`; generated Go and
TypeScript use a private generation-time reference switch. No public option or
runtime optimization-disable flag is introduced. The generated typed Go
runtime drops an oversized dense seen buffer on recycle above 1<<20 words. The
reference shares that cap, so it is not attributed to sparse paging. Sparse bookkeeping adds
64 bytes to the core memo-table layout on 64-bit Go and 32 bytes on 32-bit Go
relative to the original dense-only layout. The per-run selector contributes
8 and 4 bytes respectively; both same-source variants include the full layout.

## Scope and validation

The mechanism covers deferred-memo bookkeeping for whole-input parses in the
core closure and VM engines, generated Go runtimes, and generated TypeScript.
Documents and streams use eager memoization and do not use these sparse pages.
Core controls cover 48 pairs of result/work fingerprints. Generated-Go checks
cover one TypeScript grammar across two units, four inputs and three replays
(24 combined fingerprints for Node, AST and recognition), with additional
cutoff-boundary and reuse tests. TypeScript checks cover that grammar across
two units, four inputs and string/byte forms (16 results for Node and
recognition), including Unicode split and cutoff boundaries. The focused
controls pass. The bounded typed-reuse policy also passes full native root,
vet, all 10 standalone parser-module, regeneration and site checks, plus
Linux/386 sparse lifecycle and real-corpus optimized/reference checks. The
real-corpus sequence covers large, small, failed-large, small and large parses
in both units. Final generated-source timing confirmations versus the exact
prior build are reported below.

## Measurements and limits

Measurements were taken on 2026-10-11 with Go 1.27.1 on darwin/arm64, an
Apple M3 Max, and GOMAXPROCS=4. TypeScript uses Node 24.19.0. Reported ratios
are medians of five paired ratios; ranges are the minimum and maximum of those
pairs. Go results use five alternating 500 ms pairs. The previous-build
comparison is against 095b2e9; the same-source dense reference isolates sparse
paging under the current retention policy. That reference shares the typed
seen-buffer release cap, so comparisons to the prior build do not attribute
all changes to sparse paging.

The selector activates only when a run has more than 64 rules and more than
1,024 decoded input units. Across 12 small and 12 large core cases, every
paired timing range includes parity. Small cases retain the dense 1,024-byte
seen buffer. On the larger synthetic workload, sparse seen/page/directory
storage is 2.573–2.584 MB versus 9.101 MB dense, about 72% lower. Storage
counts full page-arena chunks, including unused slots, pointer metadata and
directory capacity; it excludes memo entries, call counters, the rest of the
memo table and total process heap. Core small-case B/op and allocation medians are unchanged; across the large
cases, B/op differs by -215 to +937 bytes and allocations by at most one,
within pooled variation.

All six small generated-Go comparisons (Node, AST and recognition across both
units) include parity in their 500 ms paired ranges. Earlier short typed AST
Bytes series varied: one five-round, two-second series measured 1.0324
[1.0226, 1.0506] versus prior, while later runs measured 1.0039
[0.9516, 1.0110] and 1.0148 [0.9684, 1.0614]. A final five-round,
two-second confirmation measured 0.9968 [0.9852, 1.0282]. The final series
does not repeat the earlier slowdown, but the runs remain distinct evidence.
On the large synthetic input, Node and recognition median ratios versus prior
are 0.8991–0.9105, with every paired range below 1; B/op is 39–42% lower, with
about 837–839 fewer allocations. AST CodePoints initially measured 1.002
[0.939, 1.015], then 1.0275 [0.9975, 1.0414] in a longer confirmation; AST
Bytes is 0.9678 [0.961, 0.992]. These are synthetic workloads, not a general
speedup claim.

### Real-corpus typed storage and reuse

The real TypeScript 5.9.3 `lib/_tsc.js` input is 6,213,092 bytes
(SHA-256 `e8f349eabd48486bdb2bf9dc1a00c89d58297270c54b745838879e2859194419`).
Its sparse backing reaches 29,202,016 bytes: 23,724,032 bytes in bit chunks
and 5,444,536 bytes in directories. The committed fixed 8 MiB typed cap
discards this backing at recycle. The bounded typed-reuse policy instead
limits reusable sparse storage to
`max(1<<20, min(decoded_units, 4<<20))` words, or 8–32 MiB on the measured
64-bit target. Generated-Go dense and plain-parser caps remain 8 MiB. The limit
covers full backing storage, including unused chunk and directory capacity; it
is not a total-heap bound. Trimming occurs after a following parse, not at parse
entry.

Across five interleaved one-second rounds, all six timings versus the exact
prior build include parity. Typed AST CodePoints measured 0.99598
[0.95233, 1.00218], with 1,632 fewer B/op and one fewer allocation; AST Bytes
measured 1.03620 [0.98275, 1.10299], with 536 more B/op and no allocation
change in this earlier series. Compared with the committed fixed-cap candidate,
the bounded policy reduces
AST B/op by 34,524,216 bytes in CodePoints and 34,523,160 bytes in Bytes,
about 3,691–3,692 allocations. Generated Node and recognition retain their
previous real-corpus B/op ratios of 0.7476–0.7561 versus prior, about 3,676
additional allocations, and are unaffected by the typed reuse policy. These
real-corpus measurements do not establish a general speedup.

A final five-round, two-second generated-source confirmation measured synthetic
AST CodePoints at 0.9896 [0.9838, 1.0077], synthetic AST Bytes at 0.9987
[0.9790, 1.0067], and short AST Bytes at 0.9968 [0.9852, 1.0282] versus
prior. Each range includes parity. Median B/op deltas were +11, -1 and -2
bytes respectively, with allocation medians unchanged. Earlier short-run
variation remains part of the evidence.

The full-suite snapshot at `3da3d4a` showed an unpaired 1.0852× Recovery Node
CodePoints ratio against the preceding snapshot at `75b9fe3`. A focused
five-round, two-second comparison against the exact prior build and a
same-source dense reference found all three Recovery timing ranges included
parity. Versus the
prior build, Node CodePoints was 1.0062 [0.9314, 1.0164], Node Bytes was
1.0260 [0.9620, 1.0368], and AST was 0.9892 [0.9752, 1.0461]. The focused
results do not establish a speed change; the full-suite comparison does not
isolate this optimization.

TypeScript cutoff timings use five interleaved Node 24 rounds against the
prior implementation. All four large-input ranges include parity:

| Unit and API | Median ratio | Paired range |
|:--|--:|:--|
| CodePoints Parse | 0.9304 | 0.9009–1.0984 |
| CodePoints Recognize | 0.9920 | 0.9877–1.0075 |
| Bytes Parse | 1.0079 | 0.9449–1.0314 |
| Bytes Recognize | 1.0005 | 0.9822–1.0050 |

Large-input seen bit buffers are 573,440 bytes in CodePoints mode and 753,664
bytes in Bytes mode, versus 8,388,608 bytes dense. On the short control, the
sparse cutoff retains the same 1,024-byte buffer as dense and allocates no page
metadata. All four short timing ranges include parity, but CodePoints
Recognize is highly variable [0.662, 1.632]; treat the short timings as
inconclusive. These synthetic TypeScript results do not establish a general
speedup.

## Reproduction

Run the semantic, boundary and generated-control tests:

```sh
go test ./internal/engine -run 'TestSparseMemo|TestGenerated.*SparseMemo' -count=1
go test ./internal/engine/genrt -run '^TestSparseMemo' -count=1
go test ./internal/engine -run '^TestGeneratedTSSparseMemoControls$' -count=1
```

`TestGeneratedSparseMemoControls` preserves optimized and dense-reference Go
fixtures under `PEGO_SPARSE_MEMO_GENERATED_DIR`; the TypeScript equivalent
preserves fixtures under `PEGO_SPARSE_MEMO_TS_DIR`. They compare results
before timing. `BenchmarkSparseMemoGenerated` measures generated Node, AST
and recognition; `BenchmarkSparseMemoTypeScript` measures the core. Compare
same-source optimized/reference builds in alternating order for five 500 ms
pairs, keeping grammar, unit and input fixed. Report paired time, B/op and
allocation counts separately. For the TypeScript synthetic timing, preserve
prior, same-source reference and optimized versions of the TypeScript grammar
with the `bench.mjs` driver; run five interleaved rounds using
`node --expose-gc bench.mjs` on Node 24. The large input repeats
`const 日本語 = 1 + 2;` 12,000 times. It warms three times and times three
calls. The short control uses one line, warms 1,000 times and times 10,000
calls. Earlier short typed AST Bytes runs varied; the latest five-round,
two-second confirmation includes parity. Keep those distinct results together
rather than treating one run as definitive.

To exercise real-corpus typed reuse, set `PEGO_SPARSE_MEMO_INPUT` to the
TypeScript 5.9.3 `lib/_tsc.js` file and run
`go test ./internal/engine -run '^TestGeneratedSparseMemoControls$' -count=1`.
The generated fixture invokes `TestSparseGeneratedRealCorpusReuse`, which
checks large, small, failed-large, small and large parses in both units. This
is an ownership test, not the paired benchmark.

The clean 131-case snapshot passed at source commit `3da3d4a`; it does not
isolate this optimization. Candidate CI passed on amd64 and 386. The candidate
remains on its work branch and has not been integrated into main.
