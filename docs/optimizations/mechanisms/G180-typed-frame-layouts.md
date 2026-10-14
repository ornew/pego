# G180: Typed Frame Layouts

Matching direct capture layouts to the general emitter enables bodies whose
structural walk would omit or reorder required slots.

## Mechanism and Invariants

LR uses authoritative Node-rule capture metadata. Independent Operators Rule and
owned repetition scopes collect names with the same build mode, predicate
visibility, projection and order as the general emitter. Dead syntax contributes
known nil slots. Required slots can widen frames or pool classes.

## Scope and State

Applied by default only to eligible generated typed Go bodies. Node
parsing/recognition, closure, VMs and TypeScript retain their separate routes.

## Reference Control

Private generation option `disableTypedFrameLayouts` selects the same-source
alternative. The reference retains previous structural discovery, so this
controls layout eligibility rather than all framed inlining. No generated
matching-time option check remains.

## Evidence and Implementation

Record 082 measures large same-source LR/Operators Rule inputs at 0.904–0.945 of
the structural-discovery reference. Small matching and generation-time ranges
overlap parity. Layout collection can widen frames despite unchanged allocations
in these fixtures. The result concerns newly eligible layouts and does not
establish zero memory cost for every grammar.

Evidence records: [082](../082-match-typed-direct-frame-layouts.md).

Source owners: [`typed structural
emitter`](../../../internal/engine/gen_direct.go), [`framed body
emission`](../../../internal/engine/gen_direct_node.go),
[`GenOptions.disableTypedFrameLayouts`](../../../internal/engine/gen.go).

The [comparison policy](../../design/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [G150](G150-typed-left-recursion-bodies.md), [G160](G160-typed-operators-line-bodies.md), [G200](G200-typed-local-capture-layouts.md).

[Optimization Catalog](../README.md)
