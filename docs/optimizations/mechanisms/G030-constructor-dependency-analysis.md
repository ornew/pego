# G030: Constructor Dependency Analysis

Sample construction need not sweep every rule whenever one derivation estimate
changes. Reverse dependencies and a bounded worklist propagate only changes that
can affect callers.

## Mechanism and Invariants

Analysis builds contiguous reverse edges, deduplicates repeated calls, seeds a
worklist in explicit DFS postorder and reevaluates callers when minimal heights,
byte lengths or coverage sets change. Acyclic callees run first; cycles converge
to the same fixed point. Target numbering and exclusions for negative
lookahead/recovery remain unchanged. This changes analysis work, not the
grammar's matching semantics.

## Scope and State

Default shared `sample.New` analysis requiring a grammar AST. All parser-backend
matching cells are not applicable: selecting closure or a VM does not run this
work in the matching loop. Standalone generated parsers do not perform it.

## Reference Control

No maintained same-source old-sweep control exists. The focused comparison is
historical parent versus candidate analysis.

## Evidence and Implementation

Historical record 071 times analysis after grammar parsing. A caller-first
depth-2,048 alias chain changes from 234.498 to 0.9909 ms, but B/op grows
8.6–13.0% across chain cases for dependency/worklist storage. Leaf-first ranges
overlap parity. These are constructor-analysis scaling and startup tradeoffs,
not parsing throughput or a bound on all cyclic grammars.

Evidence records:
[071](../071-dependency-propagation-for-sample-constructor-analysis.md).

Source owners: [`analyze and reverse-dependency
worklist`](../../../sample/analysis.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [G080](G080-equal-capture-type-reuse.md), [G090](G090-variable-read-components.md).

[Optimization Catalog](../README.md)
