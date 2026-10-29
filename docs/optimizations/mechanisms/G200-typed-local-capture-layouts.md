# G200: Typed Local Capture Layouts

Complete capture-name metadata lets direct Go-local rules omit unreachable
matching code without losing known nil captures.

## Mechanism and Invariants

Ordinary rule layouts come from Node-rule metadata; owned/projected repetition
layouts use the general collector, including dead syntax and
predicate/projection rules. Locals are declared only when reads, marks or values
require them. A known dead capture still reads nil, without introducing a
runtime frame.

## Scope and State

Applied by default only to eligible generated typed Go bodies. Node
parsing/recognition, closure, VMs and TypeScript retain their separate routes.

## Reference Control

No generation option selects an alternative for this mechanism. Typed Go
output comes from the Flow IR printer, and its generated parsers are compared
with the engine oracle in the `internal/engine` tests.

## Evidence and Implementation

Record 084 measures same-generator 128-repetition ordinary, element-predicate
and cut fixtures. All twelve accepted/rejected medians are below parity, about
0.711–0.829, with unchanged allocations. Generation fixtures shrink source and
cost less in those samples; published parser outputs remain unchanged. These
synthetic local-layout results do not establish a general generation or parsing
benefit.

Evidence records: [084](../084-complete-typed-local-capture-layouts.md).

Source owners: [`typed structural
emitter`](../../../internal/gen/gogen/flowrule.go), [`framed body
emission`](../../../internal/gen/gogen/flowframe.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [G060](G060-ordinary-typed-direct-rules.md), [G180](G180-typed-frame-layouts.md).

[Optimization Catalog](../README.md)
