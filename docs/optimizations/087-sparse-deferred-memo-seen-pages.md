# 87. Store deferred-memo seen positions sparsely

**Status:** Candidate on `perf/sparse-deferred-memo`; not integrated into main.

Deferred-memo bookkeeping can be sparse when a grammar has many rules but a
parse visits only a small subset of rule/position pairs. The candidate selects
sparse storage once per run when the grammar has more than 64 rules and the
input has more than 1,024 decoded units. It stores seen positions in
1,024-position pages grouped into stable 64-page chunks. The Go runtime caches
the latest page and reuses bounded page and directory storage; core and
generated Go share the inline `cachedSeenPage` and `markSeenPage` helpers.
TypeScript uses integer page offsets rather than allocating a typed array for
each page. Smaller runs retain the dense path.

The core reference build uses `pego_reference_sparse_memo`; generated Go and
TypeScript use a private generation-time reference switch. No public option or
runtime optimization-disable flag is introduced. The typed Go runtime drops an
oversized dense seen buffer on recycle above 1<<20 words. The reference shares
that cap, so it is not attributed to sparse paging. Sparse bookkeeping adds
64 bytes to the core memo-table layout on 64-bit Go and 32 bytes on 32-bit Go
relative to the original dense-only layout. The per-run selector contributes
8 and 4 bytes respectively; both same-source variants include the full layout.

## Scope and validation

The mechanism covers deferred-memo bookkeeping for whole-input parses in the
core closure and VM engines, generated Go runtimes, and generated TypeScript.
Documents and streams use eager memoization and do not use these sparse pages.
Core controls cover 48 pairs of result/work fingerprints. Generated-Go
checks cover one TypeScript grammar across two units, four inputs and three
replays (24 combined fingerprints for Node, AST and recognition). Additional
typed-run tests cover 1,024/1,025-unit cutoff boundaries and reuse. TypeScript checks cover that grammar across two units, four
inputs and string/byte forms (16 results for Node and recognition), including
Unicode split and cutoff boundaries. These focused controls pass. Final
Linux/386 checks match all 48 core semantic/work fingerprints and 24
generated-Go fingerprints against the dense reference; generated-runtime
sparse-boundary, reset, dense-release and cutoff tests pass. Native root
tests, `go vet`, parser regeneration, all 10 standalone parser-module suites
and site checks pass. The full 131-case snapshot remains deferred until the
design settles; do not treat the candidate as ready for main integration.

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
units) include parity in their 500 ms paired ranges, except a longer AST Bytes
confirmation: 1.0324 [1.0226, 1.0506] versus prior, a confirmed 3.2%
slowdown. On the large synthetic input, Node and recognition median ratios
versus the prior implementation are 0.8991–0.9105, with every paired range
below 1; B/op is 39–42% lower, with about 837–839 fewer allocations. AST
CodePoints is 1.002 [0.939, 1.015] in the initial pairs; a longer confirmation
measured 1.0275 [0.9975, 1.0414], whose range includes parity. AST Bytes is
0.9678 [0.961, 0.992] versus prior. AST allocation and byte counts remain
effectively unchanged versus prior. These are synthetic workloads, not a
general speedup claim.

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
calls. Keep the short-input controls in the report because they currently fail
the adoption gate.

The candidate is not ready to become the default on main. Tune the dense path
for small typed Bytes inputs and repeat the short-input controls before
proposing integration.
