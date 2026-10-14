# A030: Iterative VM Frame Reuse

Iterative VM execution needs explicit call/body state. Reusing completed frames
avoids a heap allocation for each step or rule call.

## Mechanism and Invariants

The VM returns completed call, body, skip, longest and Operators Rule frames to
parser-owned free lists. Plain calls combine invocation with a body frame,
reducing push/pop dispatch. State remains live until its frame finishes, and
published values are never recycled with the frame. Active growth, recovery and
capture owners must stay intact.

## Scope and State

Default iterative VM execution only. Closure/recursive calls use the Go stack;
generated Go and TypeScript have separate frame allocation policies.

## Reference Control

No universal same-source fresh-iterative-frame comparator is maintained. Stream
frame controls concern another owner and boundary.

## Evidence and Implementation

Records 002/013 record reduced iterative and Operators Rule allocations. Record
034 measures the one-frame plain route, including iterative JSON full parsing
from 24.3 to 22.0 ms. Record 048 describes typed frame reuse with memory
reduction and timing within noise. These route-specific historical effects are
not additive.

Evidence records: [002](../002-frame-pooling-in-the-iterative-vm-8fab402.md),
[013](../013-allocation-free-operators-rule-attempts-and-pooled-operators-rule-frames-in-the-engine-and-vms.md),
[034](../034-one-frame-per-plain-call-in-the-iterative-vm.md).

Source owners: [`reuse, release and
plainCallFrame`](../../../internal/engine/ivm.go).

The [comparison policy](../../design/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [M050](M050-plain-rule-calls.md), [M110](M110-compact-vm-backtracking-state.md), [A170](A170-operators-attempt-scratch.md).

[Optimization Catalog](../README.md)
