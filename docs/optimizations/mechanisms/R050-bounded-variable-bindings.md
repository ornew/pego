# R050: Bounded Variable Bindings

Repeated assignments should not retain every shadowed binding or make reads
traverse assignment history. A persistent environment keeps one current binding
per name.

## Mechanism and Invariants

Replacing a changed non-head value copies the prefix and shares the unchanged
suffix. Saved environments therefore preserve rule-scope and backtracking state.
Equal scalar assignment can reuse the old environment; a monotonic name hint
avoids searches for first assignments. Live snapshots and memo keys still retain
values required by their contracts. This bounds binding history, not arbitrary
user values.

## Scope and State

Applied by default in closure, both VMs, generated Go Node/typed Go and
TypeScript.

## Reference Control

No maintained same-source append-history reference control exists. The
documented comparison is historical.

## Evidence and Implementation

Record 069 measures a three-name stream environment staying at three bindings
instead of growing to 180,001. It records near-flat retained growth and faster
outer-variable reads, but changed non-head counters increase allocation volume.
Do not infer a universal allocation or speed reduction.

Evidence records: [069](../069-bound-persistent-variable-binding-histories.md).

Source owners: [`environment lookup and
replacement`](../../../internal/engine/runtime.go), [`generated environment
binding`](../../../internal/engine/genrt/runtime.go), [`TypeScript persistent
environment`](../../../internal/engine/tsrt/runtime.ts).

The [comparison policy](../../design/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [R010](R010-per-position-memo-chains.md), [G090](G090-variable-read-components.md).

[Optimization Catalog](../README.md)
