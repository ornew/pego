# R010: Per-Position Memo Chains

Memo lookup can index the input position directly, reducing composite-key
hashing and per-entry heap allocation. Context-sensitive rules still need keys
that distinguish the variable values they read.

## Mechanism and Invariants

A position slot owns a chain of entries. A key also includes rule identity,
recursion level and the statically sorted variable-read values. Entries and
saved expectations use chunk storage. When no variables are defined, a shared
nil-value key replaces a fresh slice; it is immutable. Missing variables remain
distinguishable from defined values according to the existing key comparison.
Document position storage can splice its index without changing this logical
key.

## Scope and State

Default memo organization in closure, both VMs and generated Go Node/typed Go.
TypeScript uses position-indexed chains and variable-sensitive keys but
JavaScript entry allocation, not Go slabs. Eligible rules may defer or omit
ordinary memoization; left-recursion and Document policies remain separate.

## Reference Control

No universal same-source map-versus-chain or key-allocation comparator exists.
Public memo disabling changes caching behavior and retains mandatory
left-recursion memoization.

## Evidence and Implementation

Historical record 005 reports JSON about 130 to 85 ms for its memo-table change.
Record 012 fixes exponential repeated evaluation for variable-reading rules.
Record 066 reports CUE typed config allocations from 128,458 to 3,006 for shared
undefined-variable keys. These results concern distinct workloads and revisions,
not one current combined ratio.

Evidence records: [005](../005-memo-table-with-per-position-chains-cf77e4f.md),
[012](../012-memoizing-rules-that-read-variables.md),
[066](../066-no-memo-key-allocated-for-variables-where-none-is-defined.md).

Source owners: [`memoTable.get and put`](../../../internal/engine/memo.go),
[`envValues and callBegin`](../../../internal/engine/runtime.go),
[`RuleVariables`](../../../internal/compile/analysis.go), [`generated memo
table`](../../../runtime/runtime.go), [`TypeScript memo
entries`](../../../internal/engine/tsrt/runtime.ts).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [R020](R020-deferred-memoization.md), [R040](R040-memo-entry-retirement.md), [R050](R050-bounded-variable-bindings.md), [G090](G090-variable-read-components.md).

[Optimization Catalog](../README.md)
