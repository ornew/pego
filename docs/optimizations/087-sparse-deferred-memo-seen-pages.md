# 87. Store deferred-memo seen positions sparsely

**Status:** Candidate on `perf/deferred-memo-seen-pages`; not integrated into
`main`.

Deferred-memo bookkeeping can be sparse when a grammar has many rules but a
parse visits only a small subset of rule/position pairs. The candidate selects
sparse storage once per run when the grammar has more than 64 deferred-memo rules and the
input has more than 1,024 decoded units (bytes in Bytes mode and decoded code
points in CodePoints mode). It stores seen positions in
1,024-position pages grouped into stable 64-page chunks. Go caches the latest
page and reuses bounded page and directory storage. TypeScript uses integer
page offsets rather than allocating a typed array for each page. Smaller runs
retain the dense path.

The core reference build uses `pego_reference_sparse_memo`; generated Go and
TypeScript use private generation-time reference switches. There is no public
option or runtime optimization-disable flag. The generated typed Go runtime
drops an oversized dense seen buffer on recycle above 1<<20 words; the dense
reference shares that cap, so it is not attributed to sparse paging. On the
measured 64-bit Go target, the core memo-table layout adds 64 bytes; the
same-source reference includes this layout.

Core reset bounds the complete sparse backing by `maxScratch` words,
including unused chunk slots and directory capacities. Plain generated Go
bounds sparse backing at 1<<20 words; typed Go uses
`max(1<<20, min(decoded_units, 4<<20))` words and trims at recycle after a
following small parse. Reset clears bits, directories and cached pointers.
These limits bound reusable memo backing, not total parser heap. TypeScript
metadata is invocation-owned.

## Scope and validation

This mechanism covers deferred-memo bookkeeping for whole-input parses in the
core closure and VM engines, generated Go runtimes, and generated TypeScript.
Documents and streams use eager memoization and do not use sparse pages.

The current candidate passed the short root suite, all ten standalone parser
modules, vet, generated-parser freshness, targeted Go core/generated controls,
and strict TypeScript controls. The controls include native dense references.
A full root suite, full benchmark checkpoint, and new 32-bit validation have
not been run for this timing snapshot.

## Measurements and limits

Measurements used Go 1.27.1 on darwin/arm64 (Apple M3 Max), GOMAXPROCS=4, and
five alternating 300 ms pairs. The exact parent revision is
`6d415bc`; a same-source dense reference isolates sparse paging. Ratios are
medians of paired ratios, and ranges show the minimum and maximum pair.
TypeScript used Node 24.19.0 with five alternating rounds: three warmups and
three timed calls on the large synthetic input, and 1,000 warmups and 10,000
timed calls on the short input.

For the synthetic core workload, sparse seen/page/directory storage is
2.573–2.584 MB versus 9.101 MB dense, about 72% lower. This counts full backing
capacity, including unused page slots and directory capacity; it excludes memo
entries, call counters and the rest of the memo table. Pooled B/op and
allocation medians are essentially unchanged (0–347 bytes and 0–2 allocations
per call above the parent in these controls), but several timing ranges show a cost. For
example, iterative-VM Bytes Parse is 1.040× the parent
[1.018, 1.052]. Several other cases overlap parity; small closure CodePoints Parse is
1.054× the parent [1.033, 1.074].

On the large generated-Go workload, Node and Recognize B/op is about
0.58–0.61× the parent, with median times about 0.91–0.98×. The Node
CodePoints timing range includes parity; the other listed Node/Recognize
ranges are below parity. Typed AST shows the main tradeoff: CodePoints is
1.042× the parent [1.035, 1.098], and Bytes is 1.030× [0.946, 1.044].
On the small typed workload, CodePoints is 1.050× [0.882, 1.074] and Bytes
is 1.047× [1.031, 1.067]. Typed AST B/op is unchanged from the actual parent:
the 67.2% decrease versus the same-source dense reference is not an allocation
improvement over the parent, because the reference also includes the newly introduced dense-buffer
cap. The actual parent has no such typed dense-buffer cap; this separate
retention policy must not be attributed to sparse paging. Small generated
Bytes Recognize is also 1.042× the parent [1.014, 1.047], despite staying
below the sparse cutoff.

TypeScript's large synthetic seen-bit buffers are 573,440 bytes in CodePoints
mode and 753,664 bytes in Bytes mode, versus 8,388,608 bytes dense. These are
per-invocation bit-buffer capacities, not JavaScript metadata or total retained
heap. All four large-input timing ranges overlap parity. On the short control,
the sparse cutoff keeps the same 1,024-byte bit buffer and allocates no page
chunks or directory slots; ranges are noisy and overlap parity.

**Adoption decision:** The measurements show substantial core storage savings
and lower large generated Node/Recognize allocations, alongside a measured
typed AST time cost and some core timing costs. The recommendation is to keep
the candidate separate and improve the CPU tradeoff while other work proceeds.
The alternative is to accept the current storage/time tradeoff for integration.

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
and Recognize; `BenchmarkSparseMemoTypeScript` measures the core. Compare the
same-source optimized/reference builds in alternating order for five 300 ms
pairs, keeping grammar, unit and input fixed. Report paired time, B/op and
allocation counts separately.

For the TypeScript synthetic timing, preserve parent, same-source reference
and optimized TypeScript grammars with the `bench.mjs` driver. Run five
interleaved rounds using `node --expose-gc bench.mjs` on Node 24. The large
input repeats `const 日本語 = 1 + 2;` 12,000 times; warm three times and time
three calls. The short control uses one line; warm 1,000 times and time 10,000
calls. This short control is noisy and should be reported with its paired
range.

The TypeScript storage measurement reports per-invocation bit-buffer capacity.
It does not measure JavaScript metadata or retained heap.
