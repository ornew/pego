# 76. Inline value-free plain generated Go rules

- The generated Node runtime reuses the typed runtime's direct structural
  emitter for value-free plain rules. Recognition and ordinary Parse's
  skip twins inline expressions, codepoint tests and short literals.
  Local predicates, value-building and memoized rules, cut, recovery,
  Pratt and LR leaders keep general dispatch. References can still call
  such fallback rules. TypeScript and typed-value emission are unchanged.
- Direct call methods own depth accounting and failed position/recovery
  rollback. Separate inlined rule-table bodies leave those responsibilities
  to their generic caller. Unused recognition captures are omitted;
  diagnostics, silent lookahead, labels and external entry remain exact.
- Conditions: Go 1.27.1, darwin/arm64, Apple M3 Max, 16 CPUs. Baseline
  `b288c8e`; regenerate the same grammars and prebuild both binaries.
  Five alternating 300 ms pairs cover eight workloads, both units and
  recognition/Node output (32 conditions, 110.552 seconds). A separate
  five-pair 300 ms schedule covers eight ParseAST controls (27.928 seconds).
  These focused raw records stay local; no concurrent CPU work runs during
  timing. Medians in milliseconds for recognition:

  | Workload | CodePoints, before → after | Bytes, before → after |
  |:--|--:|--:|
  | JSON | 3.169 → 1.899 | 3.030 → 2.170 |
  | CSV | 1.411 → 0.819 | 1.309 → 1.016 |
  | XML | 3.634 → 2.480 | 3.642 → 2.911 |
  | Arith_Pratt | 4.167 → 3.667 | 4.308 → 4.034 |
  | Arith_LeftRec | 8.936 → 7.257 | 9.175 → 8.069 |
  | Minilang | 5.471 → 4.582 | 5.696 → 5.077 |
  | Recovery | 5.308 → 4.455 | 5.587 → 5.024 |
  | Outline | 1.679 → 1.459 | 1.712 → 1.694 |

  Recognition time is 0.580–0.989×. All eight CodePoints ranges and seven
  Bytes ranges are disjoint; Outline Bytes overlaps. JSON/CSV/XML take
  0.580–0.799× across both units. The Node controls take 0.904–1.012×;
  JSON CodePoints, Minilang CodePoints and Recovery in both units have
  disjoint improvements, while all other Node ranges overlap. Direct
  typed controls take 1.012–1.060× with overlapping ranges; do not infer
  equivalence or a regression from that schedule. Converted Minilang
  takes 0.919× with disjoint ranges, and Recovery 0.910× with overlap.
- The optimization adds no runtime allocation or parser state. Observed
  cumulative bytes and counts still vary with GC and pooled scratch reuse.
  Recognition counts range 0–1,760; before/after medians differ by at most
  three allocations. Node counts differ by at most 20 (LR Bytes), while
  typed direct counts remain equal and conversion varies by two/six.
  These are schedule observations, not retained-heap or object-size gains.
- Source cost: all 17 standalone parsers grow 0.6–11.0%; the seven benchmark
  parsers grow 0.6–5.2%. JSON/XML/Minilang add 21/59/31 eligible bodies
  counting recognition and skip twins; DuckDB adds 1,112 and grows
  15,580,419 → 17,291,807 bytes. The duplicated generic-entry body is a
  deliberate source-size cost, distinct from linked code or live heap.
  Three alternating package-compiler pairs (`go tool compile -pack` with
  shared standard-library export files) take JSON 0.222 → 0.217 s,
  XML 0.636 → 0.592 s, Minilang 0.337 → 0.312 s and DuckDB
  121.988 → 77.420 s (0.979/0.931/0.926/0.635×). JSON ranges overlap;
  the others are disjoint. Compiler archives are 0.907/0.877/0.892/0.760×;
  they include debug/export data and are not linked executable sizes.
  This measures each standalone parser's compiler work, excluding cached
  standard-library compilation, tests, vet and linking. More source does
  not imply a larger archive or a slower compiler; no end-to-end cold
  build or runtime retained-heap improvement is claimed.
- Regression fixtures cover mixed direct/general calls, local predicates
  and assignments, memo/lookahead, cuts/recovery/Pratt/LR, nested labels,
  nullable captured repeats, Unicode/invalid UTF-8, both units and pooled
  reuse. A five-call generated limit compares external entries and nested
  calls with the engine, including exact-limit/exhaustion behavior. The
  general corpus also checks the default-limit boundary with a direct leaf.
- Reproduce CodePoints recognition with
  `go test ./bench -run '^$' -bench '^BenchmarkRecognize$/./generated$' -benchtime=300ms -count=1`,
  Node controls with
  `go test ./bench -run '^$' -bench '^BenchmarkParse$/./generated/(codepoints|bytes)$' -benchtime=300ms -count=1`,
  and typed controls with
  `go test ./bench -run '^$' -bench '^BenchmarkParse$/./generated_ast$' -benchtime=300ms -count=1`.
  For Bytes recognition use the same workloads and loop with each generated
  package's `Recognize(input, Bytes)` instead of its default invocation.
  Prebuild identical benchmark sources and alternate five pairs. P15 stays
  open for value-building Node and memoized direct rules; complete the
  broader full-suite checkpoint after that optimization group.
