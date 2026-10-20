# 87. Store Deferred-Memo Seen Positions Sparsely

- **Status**: Adopted
- **Date**: 2026-10-13

## Problem and Mechanism

Deferred-memo bookkeeping tracks which rule/position pairs have been seen. A dense bitset scales with memoized rules × input positions, even when a parse visits only a small fraction of those pairs.

The runtime selects sparse storage when a grammar has more than 64 deferred-memo rules and the input has more than 1,024 decoded units (bytes in Bytes mode; decoded code points in CodePoints mode). It stores seen positions in 1,024-position pages grouped into stable 64-page chunks. Go caches the latest page and reuses bounded page and directory storage. TypeScript uses integer page offsets rather than allocating a typed array for every page. Smaller runs keep dense storage.

For generated parsers, generation-time options omit the matching-time sparse-versus-dense representation branch when both emitted rule tables contain at most 64 deferred-memo rules. A small-table fixture showed byte-identical default Go and TypeScript output with and without Recognize; `go generate ./parsers ./bench` passed. This does not remove all per-parse checks or metadata from every generated profile. This follow-up selector adjustment was checked separately after the timed candidate run and is not included in candidate `f01bd8b` measurements.

Dense reference builds use private build/generation controls; there is no public runtime disable option. Generated typed Go separately trims oversized dense seen buffers on recycle. That cap is shared by the dense reference and is not an effect of sparse paging. The core memo-table layout adds 64 bytes on the measured 64-bit target; the same-source reference includes this layout.

Core reset bounds all sparse backing by `maxScratch` words, including unused chunk slots and directory capacities. Plain generated Go bounds sparse backing at 1<<20 words. Typed Go uses `max(1<<20, min(decoded_units, 4<<20))` words and trims at recycle after a following small parse. These limits bound reusable memo backing, not total parser heap. TypeScript metadata is invocation-owned.

## Applicability

| Execution profile | Applied / Partial / Not Applicable | Scope |
|:--|:--|:--|
| Core closure and VM, whole-input parse/recognize | Applied | Sparse seen tracking applies above both thresholds; dense path remains below either threshold. |
| Generated Go Node and Recognize | Applied | Large synthetic inputs and JSON/XML controls, Bytes and CodePoints. |
| Generated typed Go | Applied | Large synthetic typed-AST controls; dense-buffer trimming is separate. |
| Generated TypeScript, whole-input parse/recognize | Applied | Node 22.18 timing covers large and short inputs in both units. |
| Documents and streams | Not applicable | These paths use eager memoization and do not use sparse pages. |
| 32-bit targets | Unmeasured | Portability work remains deferred. |

## Historical Savepoint Reproduction (f01bd8b)

The Go comparison used parent `1189fa7f7efef7fd3b39187b843515c816af8257`, candidate `f01bd8b7a61e866206eedad6e8c77b67c6d69a38`, and a same-source dense reference. It ran Go 1.27.1 on darwin/arm64 with GOMAXPROCS=4 and five rotating 300 ms samples, with no concurrent benchmark, test or build. The generated and core binaries were built separately and hashed. The generated timed-benchmark parent did not include the candidate's non-timed internal test fixture; comparisons are limited to the recorded timed binaries.

Generated TypeScript measurements used Node 22.18.0, five rotating rounds, three warmups and three timed calls for large input, and 1,000 warmups and 10,000 timed calls for short input. Bit-buffer sizes below are invocation-owned typed-array capacities, not total JavaScript heap. `BenchmarkSparseMemoTypeScript` is a native Go benchmark parsing a TypeScript grammar; it does not measure the generated TypeScript runtime.

Focused boundary/reset tests, generated Go and Node 22 strict-TypeScript controls, and public Go result/work-hash equality among parent, candidate and dense reference passed. Parser and benchmark generation passed. The full root suite and all ten parser-module vet/test runs passed. Site tests and the pinned-WASM build with link validation passed. At this savepoint, the full benchmark checkpoint remained pending; 32-bit validation was not claimed.

For repeatable controls, export optimized and dense-reference generated Go fixtures with `PEGO_SPARSE_MEMO_GENERATED_DIR`; export TypeScript fixtures with `PEGO_SPARSE_MEMO_TS_DIR`. The tests compare results before timing. Example commands:

```sh
go test ./internal/engine -run 'TestSparseMemo|TestGenerated.*SparseMemo' -count=1
go test ./runtime -run '^TestSparseMemo' -count=1
go test ./internal/engine -run '^TestGeneratedTSSparseMemoControls$' -count=1
go test ./internal/engine -run '^$' -bench '^BenchmarkSparseMemoGenerated$' -benchmem
go test ./internal/engine -run '^$' -bench '^BenchmarkSparseMemoTypeScript$' -benchmem
go test ./internal/engine -run '^$' -bench '^BenchmarkSparseMemoDenseGenerated$' -benchmem
node --expose-gc bench.mjs
```

Build the dense reference with the private `pego_reference_sparse_memo` control; regenerate reference artifacts using the generation-time reference switches. Keep parent, candidate and same-source reference on identical grammar, operation, input and unit. Serialize runs and report time, B/op and allocs/op separately.

## Historical Savepoint Results (f01bd8b)

The Go benchmark results summarize 36 cases from five rotating parent/candidate/reference samples. Ratios are medians of paired samples; ranges are observed minima and maxima, not confidence intervals or claims of statistical significance. The static dense JSON/XML controls comprise 12 cases. Storage is reported separately from total heap.

| Workload | Candidate / Parent Time | Candidate / Parent B/op and Allocs/op | Seen Storage |
|:--|--:|--:|--:|
| Core synthetic closure, Parse/Recognize, Bytes and CodePoints | Median ratios 1.009–1.015×; observed pair ranges 0.947–1.028×. Bytes Recognize was 1.015× (1.005–1.023×). | Near 1.000× | 2.57 MB vs 9.10 MB dense (0.283×) |
| Core synthetic bytecode VM, Parse/Recognize, both units | Median ratios 0.998–1.024×; observed pair ranges 0.976–1.054×. CodePoints Recognize was 1.024× (1.017–1.054×). | Near 1.000× | 2.58 MB vs 9.10 MB dense (0.284×) |
| Core synthetic iterative VM, Parse/Recognize, both units | Median ratios 0.996–1.012×; observed pair ranges 0.978–1.044×. Bytes Recognize was 1.011× (1.009–1.012×). | Near 1.000× | 2.58 MB vs 9.10 MB dense (0.284×) |
| Generated synthetic Node | Median ratios 0.916–0.949× | B/op 0.591–0.611×; allocations 0.990× | Not separately measured |
| Generated synthetic Recognize | Median ratios 0.917–0.933× | B/op 0.582–0.602×; allocations 0.990× | Not separately measured |
| Generated synthetic typed AST | Median ratios 1.003–1.013× | B/op and allocations approximately 1.000× vs actual parent | Not separately measured |
| Small core controls | Medians near parity; ranges are noisy and include parity | Approximately unchanged | 1,024 B in both builds |
| Small generated Go controls | Medians near parity; most ranges include parity; Node CodePoints 0.972× (0.724–0.993×) | B/op and allocations approximately unchanged | Below sparse cutoff |
| Generated TypeScript, large inputs, both units | All parent-relative ranges include parity; CodePoints Parse 1.040× (0.924–1.075×) | Timing-only run | 573,440 B CodePoints; 753,664 B Bytes vs 8,388,608 B dense |
| Generated TypeScript, short inputs, both units | All parent-relative ranges include parity; Bytes Recognize 1.039× (0.928–1.147×) | Timing-only run | 1,024 B in all variants |

The synthetic core storage figures count seen bits, page and directory capacity, including unused page slots. They exclude memo entries, call counters and the rest of the memo table. Core B/op and allocations remain effectively unchanged in the measured synthetic cases. Generated Node and Recognize show lower allocated bytes in these synthetic measurements; generated typed-AST B/op is unchanged against the actual parent. Its same-source dense-reference comparison is confounded by a separate dense-buffer cap, so it is not evidence that sparse paging reduces typed-AST allocations.

In the 12 JSON/XML static dense controls, timings generally stayed near parity with ranges overlapping parity. Most B/op and allocation counts matched the parent. JSON CodePoints Node and Recognize were 1.005× (1.001–1.060×) and 1.008× (1.002–1.023×) time; those five-pair ranges also stay above parity. One exception is generated XML CodePoints Recognize: static dense used 2,216,723 B/op and 90 allocations versus 2,162,845 B/op and 89 allocations for the parent (about 2.49% more bytes). In the earlier sparse-candidate generated XML CodePoints Node comparison, it used 10,850,704 B/op versus 10,676,102 for parent and 10,676,092 for static dense. The XML cases remain useful controls, not universal evidence for every generated grammar.

The generated TypeScript large-input bit-buffer capacities are 573,440 bytes in CodePoints and 753,664 bytes in Bytes, versus 8,388,608 bytes dense. Short inputs remain at 1,024 bytes and allocate no page chunks. These are typed-array capacities, not total retained heap. Parent-relative timing ranges include parity; medians alone do not establish a speedup or regression. The short Bytes Recognize comparison against the same-source dense reference is 1.075× (1.036–1.149×); all five pairs are above parity, so the reference-relative cost remains an explicit tradeoff. Two follow-up experiments, a field-layout change and inlining the TypeScript dense-marking path, were inconclusive and discarded; neither is part of this mechanism or an improvement claim.

## Correctness and Tradeoffs

The selector-elision check covers the small-table fixture only; it does not establish byte identity for every generated grammar. At this savepoint, the full root suite and all ten parser-module vet/test runs had passed. Site tests and the pinned-WASM build with link validation passed; the full benchmark checkpoint had not yet run.

The measured storage reduction is substantial for the synthetic core workload. Generated Node/Recognize allocations also fell in the recorded synthetic cases. Core and TypeScript timing samples mostly overlap parity, with some medians and individual ranges above it. The generated typed-AST allocation comparison has the dense-buffer confound; generated timed comparisons have the fixture-composition limitation above. The JSON/XML controls show that a few allocations or bytes can differ even when runtime sparse selection is disabled.

## Decision and Limits

At the f01 savepoint, adoption remained pending. Its synthetic storage savings and timing observations are historical evidence; do not generalize them beyond their measured cases. Retain dense behavior below the thresholds and private reference controls for comparison. Do not extrapolate to Documents, streams, 32-bit targets, total retained heap, or unmeasured grammars and backends.

## Historical Focused Series-Baseline Checkpoint (67f23e0 vs c73bda3)

This checkpoint compares the candidate at `c73bda3` with its pre-optimization-series baseline, `67f23e0`. It used Go 1.27.1 on Apple M3 Max (darwin/arm64, `GOMAXPROCS=16`) with five alternating 300 ms samples per case. The comparison includes both sparse-storage and small-table selector-elision commits. The 52-case matrix covers JSON, XML, Operators arithmetic and Minilang: Parse for closure, recursive VM, iterative VM and generated Go in CodePoints and Bytes (32 cases); generated typed AST Parse (4 cases); and Recognize for the same four backends (16 cases).

| Measure | Result | Limit |
|:--|:--|:--|
| Paired median time ratios | 0.980–1.030× across the 52 cases; most observed five-pair ranges overlap 1.0. Generated Operators arithmetic `ParseAST` was 1.002×; generated Minilang `Recognize` was 0.989×. | These small deviations are descriptive and do not establish a speedup or regression. |
| Allocations | Often unchanged, but not uniformly so. Minilang generated `ParseAST` medians were 6.58→6.84 MB/op and 599→616 allocations; per-sample byte ranges overlapped (parent 6.12–6.99 MB, candidate 6.29–7.01 MB). JSON iterative `Recognize` samples varied from 7.55–46.8 KB/op on the parent and 7.55–47.97 KB/op on the candidate, with 46 allocations in both. | Sample jitter prevents a universal unchanged-allocation claim; bytes/op and allocation counts are reported separately. |

The candidate's focused core and generated Go/TypeScript sparse tests passed. Core backend result/work controls passed, and generated sparse/dense controls matched results. All 17 generated files matched after regeneration. The full root suite, all ten parser-module vet/test runs and site Go tests passed; independent static review passed the production-file scope. The pinned-WASM site build and link check also passed.

The full 131-case, three-sample snapshot at `c73bda3` was an earlier same-revision full-suite run, not a parent-relative trial. Its generated report has since been replaced by the adoption snapshot described below. Treat the c73 ratios above as historical within-run comparisons; they do not isolate sparse memo.

### Supplemental Storage-Only Probe

At that checkpoint, twelve synthetic core cases measured sparse storage against a same-source dense build using `pego_reference_sparse_memo`: closure, recursive VM and iterative VM; Parse and Recognize; CodePoints and Bytes. Seen backing, both before and after reset, was 2,573,184–2,583,528 bytes versus 9,101,312 bytes in the dense build (0.2827–0.2839×, about 72% lower). The probe reports storage outside the timer; its one-iteration run does not support timing or B/op claims. These figures exclude memo entries, call counters, the rest of the memo table and total heap. The dense reference includes the shared 64-byte layout overhead. This is a separate storage-only result, distinct from the f01 synthetic timing and storage results above.


This historical checkpoint narrows its parent-relative comparison to the listed matrix; it does not cover other workloads, 32-bit targets, Documents or streams, and does not prove statistical significance or universal allocation parity.

## Adoption Checkpoint (296a2c1 vs cc8a7c1)

The measured change starts from main `296a2c1396b39f373c5315c5c748e259fe8efd83` and is represented at `cc8a7c190101521943a0e4ac5c0aaf6bab98e9a4`. It was measured with Go 1.27.1 on Apple M3 Max (darwin/arm64, `GOMAXPROCS=16`, `GOGC=100`, `GOMEMLIMIT=off`). The full-suite snapshot at that revision contains 131 cases with three samples each and took 459.566 seconds. It is a single-revision snapshot; its within-run ratios describe this build and do not attribute a change to sparse memo. The current `docs/benchmarks.md` is generated from this run.

A separate 65-case series alternated main and candidate five times with 300 ms per sample. It covers JSON, XML, Operators arithmetic and Minilang: `Parse` on closure, recursive VM, iterative VM and generated Go in both position units (32 cases); generated typed-AST parsing (4); recognition across the same four backends (16); incremental parsing (6); long incremental parsing (3); and streaming (4, including the CSV standard-library control). Time-ratio medians across these cases range from 0.952× to 1.035×, with observed pair ranges from 0.829× to 1.096×. The parse cases' median time ratios range from 0.972× to 1.035×. These deviations are descriptive; the ranges are noisy and do not establish a general speedup or regression. Parse allocation medians were 0.974×–1.001× and B/op medians 0.966×–1.004×. Incremental and stream allocation medians were effectively unchanged in these cases.

Four additional controls used five alternating 1-second pairs: JSON closure and generated `Parse` in Bytes, and Minilang closure and generated `Recognize`. Every observed time-ratio range included parity (paired median ratios 1.005×, 1.007×, 0.989× and 1.006× respectively). These longer controls do not establish a persistent time change in those profiles.

Twelve storage-only core probes measured sparse seen backing against a same-source dense reference across closure, recursive VM and iterative VM; Parse and Recognize; and both position units. Before and after reset, sparse storage was 2,573,184–2,583,528 bytes versus 9,101,312 bytes dense (0.2827×–0.2839×, about 71.6% lower). This excludes memo entries, call counters, other memo-table storage and total heap. The probe measures storage outside the timer and supports no timing or B/op claim.

The full snapshot, focused timing series, longer controls and storage-only probes answer different questions. The full snapshot does not isolate the candidate; the paired timing series measures only the listed profiles; and the storage probe is not a whole-heap measurement. Adoption is limited to reducing indexed seen backing; no general speedup is claimed.
