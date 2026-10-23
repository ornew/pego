# G140: Typed Local Cuts

Ordinary typed rules with cuts can retain direct Go-local captures instead of
falling back to general expression methods.

## Mechanism and Invariants

Lexical cut flags preserve the surrounding choice/repetition scope. Marks
restore input, environment, captures and recovered errors; cuts inside lookahead
disappear on reset. Unsupported layouts and recovery bodies keep their own
route.

## Scope and State

Applied by default only to eligible generated typed Go bodies. Node
parsing/recognition, closure, VMs and TypeScript retain their separate routes.

## Reference Control

No generation option selects an alternative for this mechanism. Typed Go
output comes from the Flow IR printer, and its generated parsers are compared
with the engine oracle in the `internal/engine` tests.

## Evidence and Implementation

Record 078 uses five same-generator direct/general pairs. For 512 rows, median
ratios are 0.562 CodePoints and 0.712 Bytes, with unchanged allocation counts.
Two-row gains are smaller. Both-unit result/error parity holds. The control
removes the ordinary cut-bearing body group, not individual flags or later
LR/Operators Rule cut extensions.

Evidence records: [078](../078-local-cuts-in-typed-direct-rules.md).

Source owners: [`typed structural
emitter`](../../../internal/engine/gen_direct.go), [`framed body
emission`](../../../internal/engine/gen_direct_node.go).

The [comparison policy](../../design/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [G060](G060-ordinary-typed-direct-rules.md), [G170](G170-typed-framed-cuts.md), [G200](G200-typed-local-capture-layouts.md).

[Optimization Catalog](../README.md)
