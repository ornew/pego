# E060: Concat Input Fusion

Nested `list`, `map` and `concat` calls can build lists only for an outer concat
to copy their elements. Fusion appends those elements directly to the final
collection.

## Mechanism and Invariants

Every concat argument must be a supported list/map/concat call. Mixed arguments
stay general, preserving argument evaluation and error order. Closure evaluation
and generated emitters push elements directly; VM expression instructions use
list begin/push/map/end operations. The final published list still owns its
copied-out storage.

## Scope and State

Default eligible action expressions in closure, both VMs, generated Go Node,
typed Go and TypeScript. It does not suppress map lambdas or arbitrary
list-producing actions.

## Reference Control

No independent universal fusion-disable reference control exists. Repetition
projection is a separate analysis with its own control.

## Evidence and Implementation

Historical record 037 reports JSON allocation volume from 21.4 to 20.0 MB and
CSV from 15.6 to 14.0 MB in closure/generated paths. Record 044 ports fusion and
native text comparisons to VMs with timing within roughly 3%. These results do
not establish a current isolated effect in all targets.

Evidence records: [037](../037-no-intermediate-lists-in-concat.md),
[044](../044-changes-36-and-37-in-the-vms-instruction-set-2.md).

Source owners: [`concat element evaluation`](../../../internal/engine/core/eval.go),
[`ELISTBEGIN/ELISTPUSH/EMAPPUSH/ELISTEND`](../../../internal/compile/bcompile.go),
[`concat emission`](../../../internal/gen/gogen/generator.go), [`concat
emission`](../../../internal/gen/tsgen/generator.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [E050](E050-projected-repetition-values.md), [A040](A040-action-operand-scratch.md).

[Optimization Catalog](../README.md)
