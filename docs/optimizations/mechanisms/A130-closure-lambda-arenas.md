# A130: Closure Lambda Arenas

Calling list-processing lambdas need not allocate a new parameter list and
evaluation context every time.

## Mechanism and Invariants

Closure calls take parameters and contexts from LIFO arenas and release them at
return. Lambda values use an evaluation-scoped arena. A lambda created inside a
nested call keeps a heap copy of the enclosing context when required, preventing
a popped arena window from becoming its environment. Function values cannot
escape into grammar fields or variables.

## Scope and State

Default closure-backend action/predicate evaluation only. VM lambda chunks and
generated language closures are separate mechanisms.

## Reference Control

No maintained same-source heap-per-call comparator exists.

## Evidence and Implementation

Record 026 reports JSON closure allocations about 37k to 1.3k and CSV 56k to
0.8k, with smaller timing improvements. Lambda-free XML/Operators workloads are
unchanged. Results apply to those historical closure workloads, not arbitrary
escaping functions.

Evidence records:
[026](../026-lambda-calls-without-allocation-in-the-closure-backend.md).

Source owners: [`closure.apply and evaluation
arenas`](../../../internal/engine/closure_eval.go), [`evalCtx.keep`](../../../internal/engine/core/eval.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A040](A040-action-operand-scratch.md), [A190](A190-evaluation-context-reuse.md).

[Optimization Catalog](../README.md)
