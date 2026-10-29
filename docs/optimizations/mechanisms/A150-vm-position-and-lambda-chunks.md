# A150: VM Position and Lambda Chunks

Putting an integer position in a generic operand can allocate a boxed value;
allocating each lambda object also creates many small objects. VM-owned chunks
amortize both.

## Mechanism and Invariants

`PUSHPOS` stores a pointer to an integer cell from `newPos`; sequence, atomic
and repetition consumers dereference it. `EFUNC` takes lambda objects from
`newFunc` chunks. Nested lambda locals still preserve required copies. These
cells stay valid for their evaluation lifetime; the change does not alter
logical positions or public results.

## Scope and State

Default recursive and iterative VM execution. Closure and generated
Go/TypeScript use their own expression representations.

## Reference Control

No maintained same-source per-box/per-lambda allocation reference exists.

## Evidence and Implementation

Record 016 reports VM JSON allocation counts about 32k to 1.9k, with allocated
bytes and time unchanged within noise. This is fewer objects for GC, not an
established throughput improvement.

Evidence records:
[016](../016-start-positions-and-lambdas-from-chunks-in-the-vms.md).

Source owners: [`vmMachine.newPos and
newFunc`](../../../internal/engine/vm.go), [`PUSHPOS and EFUNC
evaluation`](../../../internal/engine/vm.go), [`iterative value
consumers`](../../../internal/engine/ivm.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A040](A040-action-operand-scratch.md), [A130](A130-closure-lambda-arenas.md).

[Optimization Catalog](../README.md)
