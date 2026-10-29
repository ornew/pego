# G190: Typed Recovery Bodies

Eligible unfinished typed bodies containing recovery can inline matching while
retaining runtime rule ownership.

## Mechanism and Invariants

Ordinary, LR and Operators Rule framed bodies use full rollback marks,
expectation isolation/keep helpers and recovery state. Failed skips, zero
progress, capture/environment rollback and cut scopes remain exact. Runtime
growth/selection and result ownership remain fixed; unsupported layouts fall
back.

## Scope and State

Applied by default only to eligible generated typed Go bodies. Node
parsing/recognition, closure, VMs and TypeScript retain their separate routes.

## Reference Control

No generation option selects an alternative for this mechanism. Typed Go
output comes from the Flow IR printer, and its generated parsers are compared
with the engine oracle in the `internal/engine` tests.

## Evidence and Implementation

Record 083 measures same-generator accepted Operators Rule recovery fixtures at
0.891 CodePoints and 0.911 Bytes; other accepted/recovered/rejected shapes have
mixed ranges, often crossing parity. Allocation counts stay fixed, while
generated source and generation allocations increase slightly. These scoped
matching results do not establish a universal recovery or generation-time
speedup.

Evidence records: [083](../083-inline-typed-recovery-bodies.md).

Source owners: [`typed structural
emitter`](../../../internal/gen/gogen/flowrule.go), [`framed body
emission`](../../../internal/gen/gogen/flowframe.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [G170](G170-typed-framed-cuts.md), [G180](G180-typed-frame-layouts.md).

[Optimization Catalog](../README.md)
