# 92. Reuse Generated Node Capture Scratch

**Status:** Applied to generated Go Node capture scratch. Full root, parser-module,
regeneration, vet and site checks pass.

Generated plain Go Node parsing creates capture frames and slot storage while
walking rule calls. These values are parse scratch, but must remain available
through parse completion for invocation, rollback, left recursion, Operators Rule,
cuts, recovery and actions. The candidate uses stable, append-only frame and
slot arenas separate from public result `ptrSlab` storage. Individual frames
are not returned or recycled before the parse ends.

At parser recycle, the scratch owner clears frame and slot backing, directory
tails, and undo/saved references. Reusable frame and slot backing share a
combined bound of `1<<20` native words, including full chunks, directory
capacity and arena state. Large individual scopes above `ptrChunk/4` use exact
unretained allocations rather than growing reusable scratch without bound.
Arena growth during a parse remains proportional to captures; this is not a
total per-parse heap bound.

The private generation control `GenOptions.disableNodeFrameScratch` selects
the prior generated allocator through a static emitted constant. It adds no
public parse option or runtime selector. The mechanism changes the generated
Go Node frame allocator; typed AST capture storage, the core engine, VM paths
and TypeScript are outside its scope. Recognition can use the shared Node
frame path for capture-dependent predicates. The parser gains nine native
words of state; same-source reference binaries include this 72-byte field on
64-bit hosts, while exact-parent comparisons include its footprint cost.

## Scope and Validation

The behavioral contract is to preserve every result node and capture value,
including values visible to actions and memoized calls, while releasing
inactive capture scratch at parser recycle. The separate result arena remains
responsible for public nodes. Backtracking and failed actions restore capture
state without letting scratch cleanup mutate a returned tree.

Focused chunk, backing-bound, ownership, generated-corpus and control checks
pass. Three generated fixture pairs cover capture-heavy parsing, capture-free
parsing, and a TypeScript grammar parsed by the generated Go parser. Across
those fixtures, optimized, reference and parent outputs match in 216 checked
fingerprints. Full root and parser-module suites, generation freshness, vet and
site tests and shipped/benchmark parser regeneration pass. The clean 131-case,
three-sample full snapshot at
[f8c2399](https://github.com/ornew/pego/commit/f8c2399e4679af46e98dca39d1bd7ccc9c2a4b49)
is recorded in [the full results](../benchmarks.md), without attributing it to
this optimization.

## Measurements and Limits

Measurements used Go 1.27.1 on darwin/arm64 (Apple M3 Max,
`GOMAXPROCS=4`). Five alternating parent, same-source reference and scratch
runs measured 36 cases at 300 ms each. Capture-heavy generated Node parsing
was faster in the paired medians: small inputs were 0.879×/0.861× and large
inputs 0.954×/0.946× for CodePoints/Bytes. Small Node parsing used 43,536/43,556
B/op versus 50,065/50,082, with one fewer allocation; large inputs used
1,254,676/1,254,669 B/op versus 1,432,651/1,432,640, with 25/26 fewer
allocations.

Capture-heavy `Recognize` medians were 0.651×/0.623× (small) and 0.891×/0.837×
(large). Small B/op fell from 40,583/40,599 to 24,580/24,599 with two fewer
allocations; large B/op fell from 669,609/669,643 to 491,642/491,646 with 25
fewer allocations. On the large Go-generated TypeScript-grammar workload,
Node medians were 0.891×/0.906× and `Recognize` medians 0.923×/0.887× for
CodePoints/Bytes. Node B/op fell from about 6.42 MB to 2.71/2.85 MB, and
recognition from about 6.18 MB to 2.58 MB, with roughly 495–510 fewer
allocations. Capture-free medians were near parity and allocation counts were
unchanged.

The typed AST controls kept allocation counts unchanged. One large capture
AST Bytes case measured 1.038× against the parent and 1.012× against the
same-source reference; both paired ranges include parity, so no typed-AST
speedup is claimed. In that 36-case sample, no parent-comparison case was slower in all five rounds.
Each fixture parser source grew by 2,403 bytes versus its parent. Build time and
executable size were not measured; test-binary size includes harness and test
code. These results describe selected capture workloads and do not establish a
general parser speedup.

A subsequent full snapshot exposed an XML cost absent from those fixtures.
Five alternating 500 ms runs against the exact parent `d16f33e` measured XML
`Parse` at 1.254× CodePoints and 1.394× Bytes, and `Recognize` at 1.380×.
Allocated bytes fell by about 15–17%, but all five pairs were slower. Scratch
versus the same-source disabled reference took 0.966×/0.976×/0.922× time;
both builds remained slower than the exact parent. Five fixed-iteration
(50x) pairs with `GOGC=off` overlapped parity; that is a diagnostic control,
not a recommended execution setting. Moving the added state to the parser's
tail did not mitigate the slowdown, and compiler inspection ruled out lost
inlining. The normal-GC regression remains unresolved.

## Reproduction

The generated fixture test preserves optimized/reference parsers under
`PEGO_NODE_FRAME_DIR`; `PEGO_NODE_FRAME_TYPESCRIPT` adds the TypeScript-grammar
workload. Run the focused checks and export fixtures with:

```sh
go test ./internal/engine/genrt -run '^TestNodeFrameScratch' -count=1
go test ./internal/engine -run '^TestGeneratedNodeFrameScratch' -count=1
PEGO_NODE_FRAME_DIR=/tmp/pego-node-frame \
  PEGO_NODE_FRAME_TYPESCRIPT=1 \
  go test ./internal/engine -run '^TestGeneratedNodeFrameScratchWorkloads$' -count=1
```

`TestGeneratedNodeFrameScratchWorkloads` exports fixtures, and
`BenchmarkNodeFrameScratch` exercises public parser APIs. Compare fresh parent,
reference and scratch binaries with matching grammar, options, unit and input;
alternate run order across five rounds. The same-source reference contains the
added parser state, so use the exact parent as a separate footprint control.
Preserve raw timing samples locally. The full snapshot complements, and does
not replace, the focused paired evidence above.
