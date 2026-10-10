# 87. Store deferred-memo seen positions sparsely

**Status:** Candidate on `perf/sparse-deferred-memo`; not integrated into main.

Deferred-memo bookkeeping can be sparse when a grammar has many rules but a
parse visits only a small subset of rule/position pairs. Above 64 rules, the
candidate stores seen positions in 1,024-position pages grouped into stable
64-page chunks. The Go runtime caches the latest page and reuses bounded page
and directory storage. TypeScript uses integer page offsets rather than
allocating a typed array for each page. Small rule sets retain the dense path.

The core reference build uses `pego_reference_sparse_memo`; generated Go and
TypeScript use a private generation-time reference switch. Neither adds a
public option or matching-time selector. The typed Go runtime drops an
oversized dense seen buffer on recycle above 1<<20 words. The reference shares
that cap, so it is not attributed to sparse paging. Sparse paging adds 56 bytes
to the core memo-table layout on 64-bit Go and 28 bytes on 32-bit Go; both
same-source variants include this layout.

## Scope and validation

The mechanism covers deferred-memo bookkeeping for whole-input parses in the
core closure and VM engines, generated Go runtimes, and generated TypeScript.
Documents and streams use eager memoization and do not use these sparse pages.
Core boundary, reset, stride and reuse tests pass. Generated-Go checks use one TypeScript grammar
across two units, four inputs and three replays (24 fingerprints covering
Node, AST and recognition). TypeScript checks use that grammar across two
units, four inputs and string/byte forms (16 results covering Node and
recognition), plus rule, page and wide-position boundaries. Strict TypeScript
checking (194 grammars, 17,828 results), parser regeneration, root tests, vet,
all parser-module tests and site checks pass. Final Linux/386 core and
generated-Go boundary/parity checks also pass.

## Measurements and limits

The reported timings are synthetic and do not establish an across-the-board
speedup. Measurements were taken on 2026-10-11 with Go 1.27.1 on
darwin/arm64, an Apple M3 Max (16 CPUs), and GOMAXPROCS=4; TypeScript
used Node 24.19.0. Each timing ratio is the median of five paired ratios, and its range
is the minimum and maximum of those five pairs. Five alternating 500 ms pairs
compare same-source sparse and dense reference builds; the prior-comparison
baseline is 095b2e9. Ongoing ablations should use the same-source reference.
That reference includes the typed dense-seen release cap, so it isolates sparse
paging within the revised retention policy. Core memo-table layout is the same
in both builds.

The core workload is a mixed TypeScript module of at least 256 KiB;
generated Go and TypeScript use 12,000 repeated Japanese constant lines.

Storage figures count bit-buffer capacity, full page-arena chunks (including
unused slots), pointer metadata and directory capacity. They exclude memo entries, call
counters, the rest of the memo table and total process heap. Across 12 warm
core cases, sparse seen/page/directory storage is 2.573–2.584 MB versus
9.101 MB for dense storage, about 72% lower. B/op and allocation counts are
effectively unchanged, within small pooled variation (about ±260 B/op and
±1 allocation). All timing ranges include parity except recursive-VM
`Recognize/Bytes`, initially 1.0215 [1.0018, 1.0749]; a longer confirmation
was 1.0059 [0.9813, 1.0434], within parity.

Generated-Go timings compare the candidate with the prior implementation and
the same-source dense reference. Against the prior implementation, Node and
recognition medians are 0.8775–0.9137, with every paired range below 1; B/op
is 39–42% lower, with about 837–840 fewer allocations. AST B/op and
allocation counts are unchanged against the prior implementation. AST
CodePoints first measured 1.0194 [1.0014, 1.0453]; a longer confirmation
measured 1.0474 [1.0358, 1.0643], confirming a 4.7% slowdown. AST Bytes
timing remains within parity. The same-source control isolates paging under
the revised release-cap policy; do not attribute the entire difference from
the prior build to sparse paging.

For the large-input TypeScript synthetic workload, five Node 24 interleaved
rounds against the prior implementation measured:

| Unit and API | Median ratio | Paired range |
|:--|--:|:--|
| CodePoints Parse | 1.0039 | 0.9902–1.0716 |
| CodePoints Recognize | 0.9689 | 0.9583–0.9766 |
| Bytes Parse | 0.9279 | 0.8970–0.9495 |
| Bytes Recognize | 1.0032 | 0.9977–1.0256 |

Seen bit buffers are 91–93% smaller; this excludes JavaScript metadata and
total heap. These results are specific to the large synthetic input.

Short, high-rule-count controls expose a tuning problem. On a one-line
Japanese constant, five alternating 300 ms pairs gave core recognition
medians 1.030 [1.012, 1.039] for closure CodePoints and 1.061
[1.003, 1.097] for the bytecode VM CodePoints; the overall median range is
median ratios ranged from 1.027 to 1.077, including 1.0772 for bytecode VM
Bytes. Generated-Go Node
medians were 1.043 [1.023, 1.096] for CodePoints and 1.038 [1.016, 1.093]
for Bytes; AST Bytes was 1.073 [1.053, 1.114]. Every TypeScript short-input
case was slower than the same-source reference: median ratios were
1.044–1.065 with ranges 1.001–1.090. Against the prior implementation,
TypeScript ranges include parity. The short-input bit buffer is 8,192 bytes versus 1,024 bytes
in the prior implementation. No longer confirmation was run for these
short-input controls. The longer confirmations above cover only the flagged
large-input core and generated-Go cases. A cutoff or other short-input policy
is needed before main integration; the results do not support a broad speedup
claim.

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

The candidate is not ready to become the default on main. Revisit the sparse
cutoff and repeat the short-input controls before proposing integration.
