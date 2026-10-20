# M060: Left-Recursion Growth State

Seed growth invokes a left-recursive leader repeatedly while searching for a
longer result. Keeping growth state in the call and memo entries in owned chunks
avoids allocating a new state object and heap entry for each growth step.

## Mechanism and Invariants

The runtime keeps `growState` as a value in the caller or iterative call frame.
Memo entries for the seed and each better match come from the memo allocator.
The leader remains memoized: the seed is part of the recursion algorithm, not
optional caching. Retirement cannot clear a seed still owned by an active growth
frame. Expression inlining leaves growth ownership in the runtime.

## Scope and State

Default growth-state reuse applies to closure, both VMs and the generated Go
Node and typed runtimes. TypeScript implements seed growth with JavaScript
objects; it does not implement Go's slab allocation or by-value growth-state
layout. VM position/lambda chunks are related allocation work, not a TypeScript
guarantee.

## Reference Control

No universal same-source growth-allocation reference control exists. Disabling
memoization does not disable left-recursion seeds and is not the former
allocator.

## Evidence and Implementation

Historical record 035 reports left-recursive calculator allocations from 86k to
2.2k, with 6–9% faster full parsing and 6–10% recognition on its recorded host.
Those results concern seed/growth allocation in measured Go paths, not every
left-recursive grammar or TypeScript.

Evidence records:
[016](../016-start-positions-and-lambdas-from-chunks-in-the-vms.md),
[035](../035-left-recursion-growth-without-allocation.md).

Source owners: [`growState and seed
growth`](../../../internal/engine/runtime.go), [`iterative growth
frame`](../../../internal/engine/ivm.go),
[`memoTable.alloc`](../../../internal/engine/memo.go), [`generated
growth`](../../../runtime/runtime.go), [`typed
growth`](../../../runtime/typed.go).

The [comparison policy](../../design/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [R040](R040-memo-entry-retirement.md), [G150](G150-typed-left-recursion-bodies.md).

[Optimization Catalog](../README.md)
