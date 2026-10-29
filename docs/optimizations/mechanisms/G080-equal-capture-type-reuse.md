# G080: Equal Capture Type Reuse

Repeated capture labels can repeatedly normalize and format the same inferred
record type. Structural equality lets the merger reuse an already equal
immutable type.

## Mechanism and Invariants

Equal basic, named, list and record shapes reuse the prior type. Ordered nested
joins and named-node kinds compare conservatively; top-level optional/join
forms retain the general normalizer. Capture availability remains the logical OR
of occurrences, so equality of value type cannot erase conditional availability.

## Scope and State

Default shared type checking before compilation/generation. No parser backend
performs this analysis in its matching loop.

## Reference Control

No maintained same-source general-join-only control exists.

## Evidence and Implementation

Historical record 072 measures complete type checking on 1,024 recursive
duplicate captures from 423.966 to 61.126 ms and 1.188 GB to 32.16 MB. Mixed
optional/heterogeneous and unique-label controls remain near parity. Large
single-iteration diagnostics are reported separately. The benefit concerns
repeated equivalent capture types, not parser runtime or a promise of cheap
arbitrary joins.

Evidence records:
[072](../072-reuse-structurally-equal-duplicate-capture-types.md).

Source owners: [`equal captured type
merge`](../../../internal/compile/check.go), [`immutable inferred type
structures`](../../../internal/compile/types.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [G030](G030-constructor-dependency-analysis.md), [G085](G085-action-free-positional-items.md).

[Optimization Catalog](../README.md)
