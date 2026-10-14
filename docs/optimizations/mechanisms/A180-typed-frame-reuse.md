# A180: Typed Frame Reuse

Runtime-framed typed invocations can release their capture frame at completion
instead of retaining every frame until parsing ends.

## Mechanism and Invariants

Typed values and capture arrays follow distinct ownership rules. Reuse affects
completed scratch frames and preserves values reachable from the result, saved
marks and actions. Go-local direct captures need no runtime frame. This typed
boundary does not justify returning ordinary Node frames early.

## Scope and State

Default generated typed Go runtime. Core and Node frame policies differ;
TypeScript has no Go typed-frame pool.

## Reference Control

No frame-reuse-only same-source comparator is maintained.

## Evidence and Implementation

Record 048 reports typed memory reduction after returning frames, with timing
within noise. The same experiment in ordinary engine capture frames measured
slower and was not adopted. Typed allocation or direct-body comparisons remove
more work and cannot isolate frame release.

Evidence records: [048](../048-a-typed-runtime-for-parseast.md).

Source owners: [`typed frame allocation and
freeFrame`](../../../internal/engine/genrt/typed.go), [`direct local
captures`](../../../internal/engine/gen_direct.go).

The [comparison policy](../../design/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A060](A060-direct-typed-output-construction.md), [A080](A080-generated-node-capture-scratch.md), [A270](A270-shared-empty-capture-frame.md).

[Optimization Catalog](../README.md)
