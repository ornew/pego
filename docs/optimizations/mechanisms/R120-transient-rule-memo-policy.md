# R120: Transient Rule Memo Policy

Rules whose ordinary memo results cannot be reused need not create entries.
Transient-rule analysis removes that bookkeeping before parsing.

## Mechanism and Invariants

Rules that call no other rule or have one grammar reference are classified for
ordinary transient execution. A repeated position through the unique caller
chain is already covered by the caller's memo policy. Left-recursion
requirements remain intact. Documents still memoize transient rules because
later edits can reuse their results; the classification does not remove
incremental state.

## Scope and State

Default eligible ordinary rule policy across closure, both VMs, generated Go
Node/typed Go and TypeScript. Document memoization remains eager.

## Reference Control

No transient-classification-only same-source control exists. Public memo
disabling affects a broader rule group.

## Evidence and Implementation

Record 001 combines transient rules with body elision/twins. It does not isolate
transient policy's timing or memory effect.

Evidence records:
[001](../001-value-free-rule-bodies-value-free-twins-transient-rules-0b2c903.md).

Source owners: [`transientRules`](../../../internal/compile/compile.go),
[`Rule.Transient`](../../../internal/compile/compile.go), [`ordinary versus
Document memo decision`](../../../internal/engine/core/runtime.go), [`transient rule
metadata emission`](../../../internal/gen/tsgen/generator.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [R020](R020-deferred-memoization.md), [M050](M050-plain-rule-calls.md).

[Optimization Catalog](../README.md)
