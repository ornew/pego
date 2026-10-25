# M020: Character-Class Tests

A character class should not repeatedly walk its complete range table when a
small comparison or ASCII bit lookup can decide membership.

## Mechanism and Invariants

Closure compilation specializes common one- and two-range classes into captured
constant comparisons. Larger closure classes use an ASCII bitmap and a copied
range fallback. VM program preparation builds 128-bit ASCII masks once;
non-ASCII characters retain range tests. Generated Go and TypeScript emit class
comparisons from the grammar. Negation and out-of-input behavior must remain
identical on every route; an ASCII mask does not replace Unicode membership.

## Scope and State

Default class specialization applies in all six targets: closure, recursive VM,
iterative VM, generated Go Node/recognition, typed Go and TypeScript. Generated
direct Go bodies can also read decoded code points directly, a separate access
optimization.

## Reference Control

No universal same-source class-specialization control exists. A bitmap-only
closure experiment was not adopted; its result does not describe the current
small-range specialization.

## Evidence and Implementation

Historical record 017 measures 1.3–4.6% faster VM recognition on four workloads.
Record 031 measures 1.2–4.9% faster closure full parsing and 1.6–9.4%
recognition. Record 011 includes generated inline comparisons among several
changes. These backend-specific results do not isolate every current generated
class path.

Evidence records: [011](../011-generated-parsers-catch-up-with-changes-410.md),
[017](../017-ascii-bitmaps-for-character-classes-in-the-vms.md),
[031](../031-specialized-character-class-tests-in-the-closure-backend.md).

Source owners: [`classAccept`](../../../internal/engine/compile.go), [`newASCIISet
and VM class tests`](../../../internal/engine/vm.go), [`class test
emission`](../../../internal/engine/gen.go), [`TypeScript class test
emission`](../../../internal/engine/gen_ts.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [M070](M070-vm-predecoded-match-data.md), [M040](M040-character-scan-loops.md), [M080](M080-direct-code-point-access.md).

[Optimization Catalog](../README.md)
