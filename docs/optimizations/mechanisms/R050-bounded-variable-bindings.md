# R050: Bounded Variable Bindings

Repeated bindings should not retain every shadowed binding or make reads
traverse binding history. A persistent environment keeps one current binding
per name.

## Mechanism and Invariants

Variables are numbered at compile time and held in a persistent vector: a flat
copy-on-write array up to 16 variables, a 16-way trie above. Replacing a value
copies one path and shares the rest, so saved environments preserve rule-scope
and backtracking state. Equal scalar binding reuses the old environment. Live
snapshots and memo keys still retain values required by their contracts. This
bounds binding history, not arbitrary user values.

## Scope and State

Applied by default in closure, both VMs, generated Go Node/typed Go and
TypeScript.

## Reference Control

No maintained same-source append-history reference control exists. The
documented comparison is historical.

## Evidence and Implementation

Record 069 measures a three-name stream environment staying at three bindings
instead of growing to 180,001. It records near-flat retained growth and faster
outer-variable reads. Record 128 removes the name walk and the prefix copy.
Do not infer a universal allocation or speed reduction.

Evidence records: [069](../069-bound-persistent-variable-binding-histories.md),
[128](../128-indexed-variable-environments.md).

Source owners: [`environment lookup and
replacement`](../../../internal/engine/core/runtime.go), [`generated environment
binding`](../../../runtime/runtime.go), [`TypeScript persistent
environment`](../../../internal/gen/tsgen/tsrt/runtime.ts).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [R010](R010-per-position-memo-chains.md), [G090](G090-variable-read-components.md).

[Optimization Catalog](../README.md)
