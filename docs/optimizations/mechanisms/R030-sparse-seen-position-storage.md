# R030: Sparse Seen-Position Storage

Deferred memoization needs first-call bits for rule/position pairs. A dense
array scales with the whole input even when few positions are visited. Sparse
pages reduce that indexed backing on sufficiently large inputs.

## Mechanism and Invariants

The runtime chooses sparse seen pages above both 64 deferred rules and 1,024
decoded units; smaller cases keep dense storage. Core and generated Go cache
the last page to avoid repeated directory lookup; reset clears page ownership
and lookup state, and retention limits trim reusable backing. TypeScript
resolves its directory and arena offset on each access and owns fresh arrays
for each Parser. Generated Go and TypeScript emit the selected representation
support and threshold logic before matching.

## Scope and State

Default whole-input parse/recognition support in closure, both VMs, generated Go
Node, typed Go and TypeScript. Documents and streams do not use this
deferred-memo representation. Selection by input size is not a public
optimization option.

## Reference Control

Core build tag `pego_reference_sparse_memo` restores dense storage. Private
generator option `disableSparseMemo` emits a dense TypeScript control. These
are same-source representation comparisons; they do not disable deferred
memoization. Generated Go has no dense control: its shared runtime always
chooses by the input size and the rule count.

## Evidence and Implementation

Record 087 separates historical, same-source and storage-only comparisons.
Twelve adoption storage probes measure sparse seen backing about 71.6% below
dense, excluding entries, counters and total heap. A 65-case parent comparison
has time medians 0.952–1.035 with noisy ranges, establishing no general speedup.
Generated TypeScript has separately scoped Node measurements; typed-array
capacity is not JavaScript heap. Storage-only probes support no timing claim,
and 32-bit effects remain unmeasured.

Evidence records: [087](../087-sparse-deferred-memo-seen-pages.md).

Source owners: [`sparse memo
selection`](../../../internal/engine/sparse_memo.go),
[`memoTable.markSparseSeen`](../../../internal/engine/memo.go),
[`GenOptions.useSparseMemo`](../../../internal/engine/gen.go), [`generated
sparse pages`](../../../internal/engine/genrt/runtime.go), [`TypeScript sparse
seen pages`](../../../internal/engine/tsrt/runtime.ts).

The [comparison policy](../../design/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [R020](R020-deferred-memoization.md), [A160](A160-whole-input-scratch-pooling.md).

[Optimization Catalog](../README.md)
