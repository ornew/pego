# 73. Dependency-component propagation of variable reads

- `ruleVariables` previously ran a fresh reachable-rule DFS per rule, collecting
  the same transitive reads repeatedly, even when no rule read variables.
  It now collects direct reads lazily and returns an empty result before
  constructing a graph when there are none. Otherwise, the existing syntactic
  call traversal and Tarjan ordering place callee components first. Each
  component unions its direct peer reads and finalized external callee keys
  once, sorts the result, and shares that immutable slice between recursive
  peers. Missing callees contribute nothing. Definitions alone are not reads;
  recovery arguments and Pratt skip/operand/operator expressions/actions keep
  their existing conservative dependency contract. Memo-key values and matching
  behavior are unchanged in engines and standalone generated parsers.
- Current-parent compilation of a 512-rule no-variable chain reproduces
  10.212 ms / 13.400 MB per operation in a one-second profiled diagnostic.
  An allocation profile attributes 87.61% of sampled bytes directly to
  `ruleVariables` (87.68% cumulative), confirming the earlier compiler audit.
  This is an allocation attribution, not a CPU fraction; compilation includes
  all phases. Profile totals also include benchmark setup.
- Five alternating parent/candidate pairs with a 200 ms benchtime compare
  baseline `a1424e5` with component propagation on Apple M3 Max, Go 1.27.1.
  Grammar parsing and fixture generation are outside timing; full `Compile`
  with default options is measured. Chains have the listed generated rules
  plus `main`, in either declaration order; a leaf action either reads `x`
  or returns a constant. The main rule defines `x` in both controls.

  | Chain rules / reads | Caller-first time before → after | Callee-first time before → after | Caller-first B/op before → after |
  |:--|:--|:--|:--|
  | 64 / none | 0.250 → 0.117 ms (−53.3%) | 0.246 → 0.108 ms (−56.2%) | 407.55 → 214.16 KB |
  | 64 / leaf variable | 0.260 → 0.137 ms (−47.5%) | 0.255 → 0.142 ms (−44.5%) | 419.39 → 259.24 KB |
  | 512 / none | 10.114 → 1.003 ms (−90.1%) | 10.063 → 1.017 ms (−89.9%) | 13.414 → 1.711 MB |
  | 512 / leaf variable | 10.516 → 1.219 ms (−88.4%) | 10.521 → 1.223 ms (−88.4%) | 13.496 → 2.089 MB |

  All eight time ranges are disjoint. At 512 rules, caller-first allocation
  counts fall 17,466 → 10,196 without reads and 17,658 → 11,789 with the leaf
  read. Byte/alloc counts are cumulative per compilation, not retained heap.
- Shared and cyclic 128-rule graphs with three leaf reads take
  1.250 → 0.490 ms (−60.8%) and 1.764 → 0.378 ms (−78.6%). Graphs with
  a different direct variable at each of 128 rules take 1.941 → 1.209 ms
  (−37.7%) for a chain and 3.923 → 0.406 ms (−89.7%) for a cycle. All four
  ranges are disjoint. The latter cycle drops 4.238 → 0.590 MB and
  9,507 → 6,607 allocations; sharing the finalized SCC keys avoids allocating
  one equal slice per peer. This does not promise linear total set storage:
  different acyclic closures can still contain many variables, and sorting
  and other compiler analyses remain.
- A first reverse-edge worklist reduced common cases but repeatedly merged
  growing sets. Separate 200 ms diagnostic samples with distinct direct reads
  showed chain/cycle times 1.902 → 6.853 ms and 3.796 → 17.911 ms at 128 rules
  (about 3.6×/4.7× slower). That design was rejected. Final component ordering
  resolves cycles together and finishes callees before callers, avoiding those
  repeated updates. These prototype diagnostics are separate from the final
  five-pair estimates above.
- The unchanged calculator/minilang/outline/JSON grammar AST controls change
  −5.8%/−17.3%/−7.1%/−18.6% in full compilation time. Calculator/outline
  ranges overlap; minilang/JSON do not. Bytes decrease approximately
  1.0%/9.7%/0.7%/12.7%, and allocation counts fall 11/240/3/116, respectively.
  No parsing-runtime speedup is claimed. Positive-read analysis still builds
  transient call maps and SCC metadata; the measured benefit comes from
  avoiding repeated transitive DFS and evolving-set propagation.
- Exact sorted sets match an independent per-root DFS oracle for empty,
  missing-reference, reordered 64/512/2,048-rule chains, shared/cyclic graphs,
  and shuffled distinct-variable growth graphs. Traversal checks include
  choices, lookaheads/discard, optional/repetition, recovery sync, Pratt actions,
  self calls and assignments with no reads. Existing variable-sensitive memo
  tests cover engine backends/position units, memo on/off and Document edits;
  generated-runtime and saved-module tests retain the same keys. Independent
  source review verifies callee-first order, missing/duplicate definitions and
  read-only use of shared metadata.
- Reproduce with
  `go test ./internal/engine -run '^$' -bench '^BenchmarkVariableDependencies$' -benchtime=200ms -benchmem`.
  Build both binaries with identical benchmark source, restoring only
  `analysis.go` from `a1424e5` via a Go overlay for the parent. Alternate five
  pairs with no concurrent tests/builds/probes. For allocation attribution,
  run `BenchmarkVariableDependencies/no-vars/caller-first-512` separately
  with `-benchtime=1s -memprofile=/tmp/pego-compile.pprof`, and inspect
  `go tool pprof -sample_index=alloc_space`. Raw focused records stay local.
  The full-suite checkpoint follows this compiler-analysis optimization.
