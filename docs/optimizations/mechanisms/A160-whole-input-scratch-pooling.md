# A160: Whole-Input Scratch Pooling

Repeated batch parses can reuse decoded input, offset, memo and collection
buffers that no returned result references.

## Mechanism and Invariants

Engine Programs and generated Go runtimes acquire/release cleared scratch
through `sync.Pool`. Result Node/list/field storage is detached from pooled
scratch; token strings may still retain source text. Oversized backing is
discarded under per-buffer limits. Recognition may use another engine Program
pool or a generated shared pool with rule-table counters reset. A pool may be
cleared by GC and is not persistent result ownership.

## Scope and State

Default closure/VM whole-input parsing and generated Go Node/recognition/typed
parsing. Documents and streams keep their own state; TypeScript creates an
invocation-owned parser with no cross-call pool.

## Reference Control

No universal same-source pool-disable control exists. Narrow Node frame/sparse
controls do not disable all pooling.

## Evidence and Implementation

Records 057/058 report lower allocation volume and workload-specific time
reductions; JSON generated Parse remains within noise. Typed pooling is also
described in 048. These historical results do not promise a warmed pool or zero
allocation for every invocation.

Evidence records:
[057](../057-pooling-the-scratch-memory-of-whole-input-parses.md),
[058](../058-pooling-the-scratch-memory-of-generated-parse-and-recognize.md).

Source owners: [`acquirePooledParser and
releaseScratch`](../../../internal/engine/input.go),
[`sharedProgram.scratch`](../../../internal/engine/runtime.go), [`generated parser
pool`](../../../runtime/runtime.go), [`typed pool and
release`](../../../runtime/typed.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A080](A080-generated-node-capture-scratch.md), [A100](A100-document-vm-value-stack.md).

[Optimization Catalog](../README.md)
