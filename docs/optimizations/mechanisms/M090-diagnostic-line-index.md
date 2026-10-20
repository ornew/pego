# M090: Diagnostic Line Index

Repeated errors should not rescan the whole source to compute line and column. A
lazily built line-start index makes location lookup a binary search plus bounded
column work.

## Mechanism and Invariants

Whole-input parsers build the table at the first location request. Line-break
interpretation and the selected position unit remain exact. Streams, whose
prefix is discarded, scan from committed positions instead. Recovered errors use
the same lookup, avoiding quadratic prefix scans on error-heavy input.

## Scope and State

Default diagnostic location indexing in closure, both VMs, generated Go
Node/typed Go and TypeScript. Stream diagnostics retain their separate
committed-prefix route.

## Reference Control

No maintained same-source scan-from-start comparator exists.

## Evidence and Implementation

Historical record 039 measures the Recovery workload 15–32% faster for full
parsing and 17–30% for recognition. Valid inputs that never ask for a location
do not build the table. These are error-heavy workload results, not a general
valid-parse gain.

Evidence records: [039](../039-a-line-table-for-error-positions.md).

Source owners: [`input.buildLines and
lineCol`](../../../internal/engine/input.go), [`generated line
lookup`](../../../runtime/runtime.go), [`TypeScript line
index`](../../../internal/engine/tsrt/runtime.ts).

The [comparison policy](../../design/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [G110](G110-one-pass-code-point-preparation.md), [G120](G120-lsp-position-checkpoints.md).

[Optimization Catalog](../README.md)
