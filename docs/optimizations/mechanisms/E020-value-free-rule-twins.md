# E020: Value-Free Rule Twins

A CST rule can be called where its result is discarded and elsewhere where its
tree is needed. A separate value-free twin avoids constructing the discarded
tree without changing valued calls.

## Mechanism and Invariants

`twinOf` creates an alternative compiled rule for CST rules without actions or
text types. Calls in discarded, atomic or eligible body contexts select it.
The twin has its own rule and memo identity; its nil result cannot contaminate
valued memo entries. Rules in left-recursion cycles are excluded because mixed
valued/value-free seed growth would change semantics. A callee's action is not
skipped by this CST-only transformation.

## Scope and State

Default eligible twin lowering in closure, both VMs and generated Go Node, typed
Go and TypeScript. The original valued rule remains available.

## Reference Control

No maintained universal same-source twin-disable control exists. Recognition
builds another program and is not a twin-only reference.

## Evidence and Implementation

Record 001 measures twins together with body elision and transient-rule memo
policy. Its JSON timing/allocation reductions cannot be assigned entirely to
twins. No current isolated twin effect is reported.

Evidence records:
[001](../001-value-free-rule-bodies-value-free-twins-transient-rules-0b2c903.md).

Source owners: [`Compiler.twinOf and
Rule.TwinOK`](../../../internal/compile/compile.go), [`twin rule
bytecode`](../../../internal/compile/bcompile.go), [`all rule/twin
generation`](../../../internal/engine/gen.go), [`TypeScript twin rule
generation`](../../../internal/gen/tsgen/generator.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [E010](E010-value-free-rule-bodies.md), [R010](R010-per-position-memo-chains.md), [M060](M060-left-recursion-growth.md).

[Optimization Catalog](../README.md)
