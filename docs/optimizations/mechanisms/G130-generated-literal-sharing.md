# G130: Generated Literal Sharing

Repeated immutable literal tables can be emitted once and referenced by multiple
matchers, reducing generated data duplication.

## Mechanism and Invariants

Interning keys use decoded literal contents. Each matcher retains its own
description/expectation identity; sharing data cannot merge diagnostics or rule
wrappers. Failed speculative lowering rolls back inserted literal entries and
reserved declarations. Helper-body sharing is independently selected.

## Scope and State

Default literal sharing in generated Go Node and typed Go. TypeScript literal
sharing is private, experimental and default-off; helper sharing remains default
there. Core closure/VM literals are separate compilation representations.

## Reference Control

Private `disableLiteralSharing` selects duplicate tables.
`enableTSLiteralSharing` opts into the TypeScript experiment, unless explicit
disable takes precedence. These select generated source before parsing.

## Evidence and Implementation

Record 090 compares helper-only, literal-only, combined and no-sharing outputs.
It primarily supports source/build-cost reduction; mixed TypeScript literal
results did not justify default adoption. Combined ratios do not isolate literal
sharing alone.

Evidence records: [090](../090-share-identical-generated-helpers.md).

Source owners: [`gen.Generator literal registry`](../../../internal/gen/generator.go),
[`TypeScript literal sharing selection`](../../../internal/gen/tsgen/generator.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [G010](G010-generated-helper-sharing.md), [G070](G070-literal-text-predicates.md).

[Optimization Catalog](../README.md)
