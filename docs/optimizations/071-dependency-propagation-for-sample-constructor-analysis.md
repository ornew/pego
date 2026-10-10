# 71. Dependency propagation for sample constructor analysis

- `sample.New` previously swept every rule until minimal derivation heights,
  byte lengths and coverage reach sets stabilized. Caller-first alias chains
  improved one link per sweep. Analysis now builds reverse dependencies, seeds
  a bounded deduplicated worklist in explicit DFS postorder, and reevaluates
  only callers of changed estimates or sets. Acyclic callees run first; cycles
  still converge to the previous fixed point. Repeated calls add one reverse
  edge, stored in a contiguous array with prefix offsets. Target numbering,
  generation decisions and negative-lookahead/recovery exclusions are unchanged.
  This affects the shared sampling constructor, which requires a grammar AST,
  rather than matching in any backend or generated parser.
- Five alternating 200 ms pairs compare baseline `9159f94` with this dependency
  propagation on Apple M3 Max, Go 1.27.1. Grammars are parsed before timing;
  only `analyze(g, "main")` runs in the timed loop. Each chain ends in `"é"`
  and has the listed derivation depth (one more rule than the depth):

  | Depth | Caller-first time, before → after | Leaf-first time, before → after | B/op, before → after |
  |--:|:--|:--|:--|
  | 128 | 0.502 → 0.0391 ms (−92.2%) | 0.0361 → 0.0378 ms (+4.7%) | 71.94 → 81.32 KB |
  | 512 | 9.687 → 0.1680 ms (−98.3%) | 0.1554 → 0.1603 ms (+3.2%) | 316.51 → 355.55 KB |
  | 1,024 | 44.152 → 0.4001 ms (−99.1%) | 0.3637 → 0.3783 ms (+4.0%) | 802.42 → 878.71 KB |
  | 2,048 | 234.498 → 0.9909 ms (−99.6%) | 0.9525 → 0.9934 ms (+4.3%) | 1.795 → 1.949 MB |

  Caller-first ranges do not overlap; all four leaf-first ranges overlap.
  All chains add nine allocations, irrespective of depth, and B/op grows
  8.6–13.0% for transient dependency/queue storage. An initial per-rule caller
  slice design added 136–2,056 allocations; contiguous edges avoid that cost.
  At depth 2,048 it used 1.988 MB versus the final 1.949 MB. Preserve the simpler
  fixed-point semantics while trading this bounded startup storage for avoiding
  repeated whole-grammar work. Dense per-rule reach bitsets remain unchanged;
  their memory still scales with rules times targets. Sparse/shared sets need
  separate measurements and ownership design before changing representation.
- The same pairs include five controls: a mixed graph fixture with shared
  calls/finite and impossible cycles, a one-rule choice/repetition, and the
  unchanged calculator/minilang/outline example ASTs. Median analysis time
  changes −2.1%, +8.7%, −3.9%, −20.0%, −14.1%, respectively. The first three
  sample ranges overlap; minilang and outline ranges do not. Controls add
  4–7 allocations and 2.5–9.2% B/op. The one-rule median rises roughly
  0.655 → 0.712 µs. These are constructor-analysis results, excluding source
  compilation, generation attempts and parsing; they do not claim a runtime
  parsing speedup or a limit on constructor work from `WithBudget`.
- Reordered 128/2,048-depth chains and 30 shuffled shared/recursive graphs agree
  with an independent whole-sweep scheduling oracle for every rule's height,
  length and reach set. Tests preserve impossible/missing-reference estimates,
  generation-specific call exclusions, Unicode byte lengths and exact chain
  coverage. Existing deterministic/valid/invalid generation, dead-alternative,
  Pratt-level and engine-corpus checks pass. A fixed-seed public generator
  probe produces byte-identical inputs, coverage report and statistics to the
  parent on the same Go release. Independent source review checks CSR count/fill,
  cursor-buffer reuse, dependency completeness and bounded ring invariants.
- Reproduce with
  `go test ./sample -run '^$' -bench '^BenchmarkSampleAnalysis$' -benchtime=200ms -benchmem`.
  Build parent/candidate binaries with the same benchmark source; restore only
  `sample/analysis.go` from `9159f94` through a Go overlay for the parent. Run
  five alternating pairs with no tests/builds/probes concurrently. Raw results
  stay local. The full-suite checkpoint follows P13/P27 in the current core
  analysis optimization group; batch-runtime benchmark workloads are unchanged.
