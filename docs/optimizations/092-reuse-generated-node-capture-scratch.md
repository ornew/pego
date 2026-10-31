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

The constant `nodeFrameScratch` in the shared generated runtime
(`runtime/runtime.go`) selects the scratch allocator; the per-parse frame slab
remains in the runtime only as the disabled alternative. No parse option,
generation option or runtime selector chooses between them. The mechanism changes the generated
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
[f8c2399](https://github.com/ornew/zeroparse/commit/f8c2399e4679af46e98dca39d1bd7ccc9c2a4b49)
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
inlining. At this earlier checkpoint, the normal-GC XML cost remained
unresolved; a later current-main comparison is recorded below.

## Current XML Checkpoint (244be64 vs d16f33e)

A later comparison on Go 1.27.1, Apple M3 Max, darwin/arm64,
`GOMAXPROCS=4`, `GOGC=100` and `GOMEMLIMIT=off` measured the full 256 KiB XML
input in five alternating 500 ms rounds. Against exact parent `d16f33e`,
current generated `Parse` took 0.9478× the time in Bytes (range
0.8973–0.9567) and 0.9414× in CodePoints (0.9368–0.9582); `Recognize` took
0.948× (0.9313–0.9688).
Allocated bytes and allocation counts were also lower in all three cases.
Against a same-source reference with only `nodeFrameScratch` disabled, the
current scratch version measured 0.9556× Bytes (0.8931–0.9651), 0.9453×
CodePoints (0.8863–1.0109) and 0.9422× recognition
(0.9215–0.9535). The full input and parsed Node JSON SHA-256 matched between
parent and current. Five additional alternating 1 s parent/current rounds with
`GOMAXPROCS=16` measured 0.9703× Bytes Parse (0.9426–1.0185), 0.9607×
CodePoints Parse (0.9386–0.9947), and 0.9646× recognition
(0.9575–0.9862); allocated bytes and counts stayed lower.

This checkpoint no longer reproduces the earlier XML slowdown. Because the
current main includes intervening changes, these results do not isolate the
effect of frame scratch and do not establish a general speedup.

## Parser Field Layout Control

A field-layout control against `5609246` moved `captureScratch` after the
existing evaluation fields without changing frame allocation or reset. Five
alternating 300 ms samples on Go 1.27.1, Apple M3 Max, darwin/arm64 and
`GOMAXPROCS=16` covered generated JSON/XML Node parsing in both position units,
typed `ParseAST` and `Recognize` (eight cases). All observed time ranges
included parity, and allocation counts and bytes stayed near their controls.

XML Node CodePoints measured 1.000× (0.964–1.033×), Bytes 0.961×
(0.941–1.009×), and recognition 0.995× (0.992–1.014×). These ranges did not
establish mitigation of the XML cost observed at that earlier checkpoint.
Generated frame boundary, ownership and corpus checks passed; regeneration
covered all 17 standalone parsers. The layout change was not adopted. The
existing field order was retained; this control did not explain the later
current-main result.

## Memo Slot Cleanup Control

A separate control replaced bulk memo-slot clearing at parser release with
nil checks and individual clearing of occupied slots. Runtime tests and the
JSON/XML cross-backend workload checks passed. Five alternating 300 ms pairs
against `5609246`, using the same Go 1.27.1, Apple M3 Max, darwin/arm64 and
`GOMAXPROCS=16` configuration, covered generated JSON/XML Node parsing in both
units and recognition (six cases).

XML Node time ratios were 1.006× CodePoints (0.974–1.065×) and 1.020× Bytes
(0.971–1.153×); both ranges overlapped parity. XML recognition measured 1.012×
(1.004–1.029×), slower in all five pairs. The three JSON ranges also overlapped
parity. This control did not mitigate the GC-sensitive XML cost measured at
that checkpoint and was not adopted. Bulk clearing and all generated runtime
sources remain unchanged.

## Reproduction

The generated fixture test preserves optimized/reference parsers under
`ZP_NODE_FRAME_DIR`; `ZP_NODE_FRAME_TYPESCRIPT` adds the TypeScript-grammar
workload. Run the focused checks and export fixtures with:

```sh
go test ./runtime -run '^TestNodeFrameScratch' -count=1
go test ./internal/gen/gogen -run '^TestGeneratedNodeFrameScratch' -count=1
ZP_NODE_FRAME_DIR=/tmp/zp-node-frame \
  ZP_NODE_FRAME_TYPESCRIPT=1 \
  go test ./internal/gen/gogen -run '^TestGeneratedNodeFrameScratchWorkloads$' -count=1
```

`TestGeneratedNodeFrameScratchWorkloads` exports fixtures, and
`BenchmarkNodeFrameScratch` exercises public parser APIs. Compare fresh parent,
reference and scratch binaries with matching grammar, options, unit and input;
alternate run order across five rounds. The same-source reference contains the
added parser state, so use the exact parent as a separate footprint control.
Preserve raw timing samples locally. The full snapshot complements, and does
not replace, the focused paired evidence above.
