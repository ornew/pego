# G160: Typed Operators Rule Line Bodies

Eligible typed operand, operator and skip matchers can inline structural bodies
instead of dispatching each expression.

## Mechanism and Invariants

Each line owns its capture scope. Runtime precedence, longest-match selection,
frames and action finalization remain unchanged. This cut-free route excludes
incompatible captures/layouts; framed-cut and recovery extensions have their own
selectors.

## Scope and State

Applied by default only to eligible generated typed Go bodies. Node
parsing/recognition, closure, VMs and TypeScript retain their separate routes.

## Reference Control

No generation option selects an alternative for this mechanism. Typed Go
output comes from the Flow IR printer, and its generated parsers are compared
with the engine oracle in the `internal/engine` tests.

## Evidence and Implementation

Record 080 uses five same-generator direct/general pairs. Large action/capture
line fixtures measure 0.891 CodePoints and 0.940 Bytes with unchanged
allocations. Small action cases overlap parity, and large calculator Bytes also
overlaps. The effect is line-body dispatch with the runtime fixed, not
longest-attempt allocation or arbitrary Operators Rule grammars.

Evidence records: [080](../080-inline-eligible-typed-operators-rule-lines.md).

Source owners: [`typed structural
emitter`](../../../internal/engine/gen_direct.go), [`framed body
emission`](../../../internal/engine/gen_direct_node.go).

The [comparison policy](../../design/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [E030](E030-operators-line-value-elision.md), [G170](G170-typed-framed-cuts.md), [G180](G180-typed-frame-layouts.md), [G190](G190-typed-recovery-bodies.md).

[Optimization Catalog](../README.md)
