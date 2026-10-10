# 75. Exclude absent expectations before scanning

- Each active expectation record in the Go engine and generated Go runtime
  keeps a 64-bit membership filter. An unsigned multiplicative hash selects
  one bit for the complete expectation ID, including the message flag.
  An unset bit proves the ID is absent; a set bit still uses the existing
  exact ordered scan. Collisions cannot discard distinct expectations.
  Farthest-position resets clear the bits; isolation saves and restores
  them alongside the record bounds, including VM label/recovery state.
  Memo and recovery copies keep their existing representation and order.
- The filter needs no descriptor-sized array or extra allocation. It adds
  8 bytes to parser state, changes the saved record mark from 16 to 24 bytes
  on darwin/arm64, and adds 8 bytes to VM label/recovery state. Scope marks
  held by call frames also grow. Every recorded expectation pays the hash
  cost, and saturated filters retain linear worst-case duplicate work.
  Plain and typed generated parser recycling clears the filter with the
  active record. TypeScript is unchanged.
- Compare baseline `896c64d` with this filter on Go 1.27.1, Apple M3 Max,
  darwin/arm64. Five alternating 300 ms pairs of prebuilt binaries, without
  competing builds/tests, cover 38 conditions. CPython 3.14.0's vendored
  `_pydecimal.py` and `typing.py` are 229,038 and 134,633 bytes. Medians:

  | Workload | Time before → after | B/op before → after | Allocs/op before → after |
  |:--|:--|:--|:--|
  | Python `_pydecimal.py`, typed, CodePoints | 26.723 → 23.026 ms | 12,143,699 → 11,824,733 | 121,749 → 121,741 |
  | Python `_pydecimal.py`, typed, Bytes | 26.736 → 23.325 ms | 12,000,285 → 11,691,197 | 121,741 → 121,733 |
  | Python `typing.py`, typed, CodePoints | 13.052 → 11.539 ms | 4,982,032 → 4,852,572 | 61,630 → 61,627 |
  | Python `typing.py`, typed, Bytes | 13.875 → 12.282 ms | 4,938,526 → 4,851,950 | 61,629 → 61,627 |
  | Recovery, generated Node, CodePoints | 9.137 → 7.621 ms | 7,700,184 → 7,677,203 | 1,696 → 1,695 |
  | Recovery, generated conversion, CodePoints | 9.539 → 8.173 ms | 9,690,103 → 9,858,460 | 1,916 → 1,921 |

  The four typed Python ranges do not overlap (0.862–0.885× time).
  Generated Python recognition takes 0.930/0.948× for these files;
  only the first range does not overlap. Recovery takes 0.881–0.948× on
  the three engine backends and both units, and 0.832–0.857× on generated
  Node/conversion paths. The recursive VM CodePoints recovery ranges
  overlap; the other recovery ranges do not. Conversion allocates about
  168 KB more and adds five allocations in this schedule. These are
  observed cumulative medians: the filter itself allocates nothing, while
  GC timing and scratch-pool reuse change allocation traffic. Do not
  attribute all measured byte differences to a smaller object layout.
- JSON and Pratt Node controls range 0.956–1.018× across the engine and
  generated paths, with overlapping ranges. Generated typed JSON in the
  root workload takes 0.927× with disjoint ranges; the parser module's
  different JSON workload takes 0.925× with overlapping ranges. The tiny
  29-byte JSON controls take 1.092/1.022× at the median in this schedule,
  with unchanged seven allocations and overlapping ranges. Five separate alternating 700 ms
  confirmation pairs give 1.111 → 1.049 µs (0.944×) and
  1.195 → 1.118 µs (0.936×), again with overlapping ranges and seven
  allocations. The conflicting schedules do not establish a tiny-input
  gain or regression. Range overlap is not evidence
  of statistical equivalence or a universal speedup.
- A fresh three-second baseline Python profile attributes 17.0% of sampled
  CPU to `expect`; the earlier no-op diagnostic and historical profiles
  are not current cost estimates. A descriptor-to-stack-index hint with
  full-ID/range validation was correct but made both Python typed inputs
  about 12% slower in three alternating 500 ms pairs, so it was rejected.
  A separate 32-bit TypeScript filter on Node 24.19.0, three alternating
  500 ms pairs after three warm-up parses, gives ratios
  0.988/1.022/1.023/0.995 for `_pydecimal.py` CodePoints/Bytes and
  `typing.py` CodePoints/Bytes. All ranges overlap; no clear gain was
  established, so that runtime keeps its exact linear implementation.
- Regression tests compare 12,000 deterministic recording/scope/reset/
  silence/aliased-merge operations with an independent linear oracle and
  retained expectation copies. Recording 320 distinct plain/message IDs
  forces collisions without depending on hash assignments. The same test
  runs in generated plain, typed direct and conversion packages. Pool
  tests cover successful and failing calls in both units. An 80-choice
  Unicode corpus checks public diagnostics through memo, lookahead,
  labels and recovery on Go and unchanged TypeScript paths.
- Reproduce Python with
  `cd parsers/python && go test -run '^$' -bench '^(BenchmarkParseAST|BenchmarkParseASTBytes|BenchmarkRecognize)$' -benchtime=300ms -count=1`;
  JSON controls with
  `cd parsers/json && go test -run '^$' -bench '^(BenchmarkSmallParseAST|BenchmarkParseAST)$' -benchtime=300ms -count=1`;
  and engine/recovery controls with
  `cd bench && go test -run '^$' -bench '^BenchmarkParse$/(JSON|Arith_Pratt|Recovery)$' -benchtime=300ms -count=1`.
  Use the final benchmark sources on both versions, regenerate standalone
  parsers, and alternate five pairs of the prebuilt binaries. Focused raw
  records stay local; the clean-commit full-suite checkpoint below provides
  the broader comparison.

- The full-suite checkpoint measures clean commit `2c68ccf`: 131 cases with
  three samples each, 459.013 seconds. Relative to the separate `5610082`
  full snapshot, typed median time ranges 0.799–1.034× and bytes
  0.988–1.000× across the eight workloads. These noninterleaved snapshots
  do not isolate this method's effect; use the paired controls above for
  attribution. The current backend, recognition, incremental, stream and
  preparation comparisons are under [Where PEGO stands](../performance.md#where-pego-stands).
