# A270: Shared Empty Capture Frame

A scope with no capture slots needs no fresh frame storage. One immutable empty
frame can represent it.

## Mechanism and Invariants

The frame is shared only for zero-slot scopes and is never written. Captured
scopes keep their own slot arrays and rollback. Plain calls can leave a caller's
frame current, while generic zero-slot invocations use the empty owner. Complete
layout collectors must count required dead/nil slots before deciding a scope is
empty.

## Scope and State

Default capture-free scope representation in closure, both VMs, generated Go
Node/typed Go and TypeScript.

## Reference Control

No shared-empty-frame-only comparator is maintained. Plain-call or layout
controls affect a larger route.

## Evidence and Implementation

Record 001 lists the empty-frame mechanism among combined changes; 019 measures
a broader capture-free call path. Neither provides a current isolated
empty-frame effect.

Evidence records:
[001](../001-value-free-rule-bodies-value-free-twins-transient-rules-0b2c903.md),
[019](../019-a-plain-call-path-for-unmemoized-rules-without-captures.md).

Source owners: [`emptyFrame`](../../../internal/engine/compile.go), [`newFrame
and invokePlain`](../../../internal/engine/runtime.go), [`generated empty
frame`](../../../runtime/runtime.go),
[`Parser.newFrame`](../../../internal/engine/tsrt/runtime.ts).

The [comparison policy](../../design/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [M050](M050-plain-rule-calls.md), [A180](A180-typed-frame-reuse.md).

[Optimization Catalog](../README.md)
