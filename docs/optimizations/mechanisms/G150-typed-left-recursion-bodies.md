# G150: Typed Left-Recursion Bodies

Eligible typed left-recursion leaders can inline matching expressions while
retaining runtime seed growth.

## Mechanism and Invariants

The runtime still owns leader invocation, growth, memoization, action and
finish. The cut-free route requires a matching capture layout; unresolved or
unsupported bodies remain general. Framed cuts and recovery use separately
selected extensions.

## Scope and State

Applied by default only to eligible generated typed Go bodies. Node
parsing/recognition, closure, VMs and TypeScript retain their separate routes.

## Reference Control

No generation option selects an alternative for this mechanism. Typed Go
output comes from the Flow IR printer, and its generated parsers are compared
with the engine oracle in the `internal/engine` tests.

## Evidence and Implementation

Record 079 uses five same-generator direct/general pairs. Large action/capture
LR fixtures measure 0.893 CodePoints and 0.943 Bytes; calculator ranges all
cross parity. Large allocation counts remain unchanged. Results concern eligible
leader-body inlining, with growth fixed, and do not establish benefits for every
LR grammar.

Evidence records: [079](../079-inline-eligible-typed-left-recursion-bodies.md).

Source owners: [`typed structural
emitter`](../../../internal/engine/gen_flowrule.go), [`framed body
emission`](../../../internal/engine/gen_flowframe.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [G170](G170-typed-framed-cuts.md), [G180](G180-typed-frame-layouts.md), [G190](G190-typed-recovery-bodies.md).

[Optimization Catalog](../README.md)
