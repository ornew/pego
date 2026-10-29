# G170: Typed Framed Cuts

Cut-bearing unfinished LR and Operators Rule bodies can use structural emission
without moving runtime frame ownership.

## Mechanism and Invariants

Each body initializes lexical cut state from incoming `p.cut` and publishes it
on both returns. Choice, optional and repetition scopes remain separate;
lookahead resets discard its cut. Repetition restores the full mark before cut
failure drops collected values. Incompatible layouts remain general.

## Scope and State

Applied by default only to eligible generated typed Go bodies. Node
parsing/recognition, closure, VMs and TypeScript retain their separate routes.

## Reference Control

No generation option selects an alternative for this mechanism. Typed Go
output comes from the Flow IR printer, and its generated parsers are compared
with the engine oracle in the `internal/engine` tests.

## Evidence and Implementation

Record 081 measures large same-generator LR and Operators Rule fixtures at about
0.889–0.943 of reference time, with unchanged allocations. The small Operators
Rule Bytes case is 0.4% slower at its median. These eligibility-specific gains
and costs do not establish a broad speedup or recovery support by this selector
alone.

Evidence records: [081](../081-scoped-cuts-in-unfinished-typed-go-bodies.md).

Source owners: [`typed structural
emitter`](../../../internal/gen/gogen/flowrule.go), [`framed body
emission`](../../../internal/gen/gogen/flowframe.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [G140](G140-typed-local-cuts.md), [G180](G180-typed-frame-layouts.md), [G190](G190-typed-recovery-bodies.md).

[Optimization Catalog](../README.md)
