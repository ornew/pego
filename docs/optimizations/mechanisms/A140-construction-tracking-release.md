# A140: Construction Tracking Release

Temporary constructor tracking must not retain structs after an action returns
nil or rejects its result. Cleanup releases those scratch references at the
evaluation boundary.

## Mechanism and Invariants

Successful, nil and failed action results, predicates and bindings release
construction tracking. Go clears discarded backing tails as well as truncating
length; TypeScript uses finalization cleanup. Returned values and
environment-held values retain their identity and ownership. Clearing
bookkeeping cannot clear the actual returned object.

## Scope and State

Default closure, VM, generated Go Node/typed direct/conversion and TypeScript
evaluation paths. Generated parsers have no stream API, but still need
evaluation cleanup.

## Reference Control

No maintained same-source cleanup-disable comparator exists.

## Evidence and Implementation

Historical record 068 records about 15 MiB of extra live heap from retained
temporary construction tracking in a discard-heavy stream. Cleanup keeps sampled
20k-to-60k growth at 0–6 KiB and reduces focused stream allocations. Ordinary
batch allocation is unchanged and some generated timings cost more. TypeScript
semantics pass but timing is unmeasured. Adoption protects ownership and bounded
tracking rather than promising every action is faster.

Evidence records:
[068](../068-release-construction-tracking-when-an-action-returns-nil.md).

Source owners: [`constructor tracking
cleanup`](../../../internal/engine/eval.go), [`VM evaluator
cleanup`](../../../internal/engine/vm.go), [`generated construction
tracking`](../../../runtime/runtime.go), [`typed
tracking`](../../../runtime/typed.go), [`TypeScript
cleanup`](../../../internal/engine/tsrt/runtime.ts).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A040](A040-action-operand-scratch.md), [A090](A090-typed-output-liveness-cleanup.md).

[Optimization Catalog](../README.md)
