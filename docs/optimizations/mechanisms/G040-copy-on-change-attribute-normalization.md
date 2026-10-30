# G040: Copy-on-Change Attribute Normalization

Preparing a grammar should not copy every expression node when its attributes
and children already have the normalized form. Copy-on-change reuses unchanged
paths.

## Mechanism and Invariants

`normalizeAttributes` returns an original node when no supported child or
attribute changes. A changed descendant requires copying each modified ancestor
before replacement, preserving the source AST. Attribute order, binding,
validation, diagnostics and legacy nil/empty container shapes remain exact.
Opaque extension values remain opaque. The prepared grammar can share unchanged
nodes with its source; it is not a detached deep copy.

## Scope and State

Default shared compilation/generation preprocessing. Matching in all six
backends is unchanged. No standalone runtime attribute-normalization path is
implied.

## Reference Control

Internal compilation option `EagerAttributeCopies` restores the eager same-source
normalizer. Selection occurs during compilation and adds no matching-loop
branch.

## Evidence and Implementation

Record 095 reports workload-specific allocation reductions and a longer recheck
that did not reproduce an initial synthetic slowdown. Generated parsers remain
byte-identical. These compilation measurements do not establish a universal
preparation speedup or a matching benefit.

Evidence records: [095](../095-attribute-copy-on-change.md).

Source owners:
[`normalizeAttributes`](../../../internal/compile/normalize_attributes.go),
[`Options.EagerAttributeCopies`](../../../internal/compile/compile.go), [`source
immutability and reference
checks`](../../../internal/compile/normalize_attributes_test.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [G080](G080-equal-capture-type-reuse.md), [G090](G090-variable-read-components.md).

[Optimization Catalog](../README.md)
