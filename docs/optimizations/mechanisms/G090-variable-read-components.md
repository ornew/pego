# G090: Variable Read Components

Running a reachable-rule search for every rule repeatedly collects the same
transitive variable reads. Dependency components compute that metadata once per
recursive group.

## Mechanism and Invariants

Direct reads are collected lazily; a grammar with none returns before building a
graph. Callee-first strongly connected components union direct peer reads and
finalized external-callee keys once, sort them and share an immutable key slice
among peers. Definitions alone are not reads; recovery and Operators Rule
expressions retain their conservative dependency traversal.

## Scope and State

Default shared compiler metadata for all compilation/generation targets. Runtime
memo keys consume the result, but matching does not run the component analysis.

## Reference Control

No maintained same-source per-rule DFS comparator exists.

## Evidence and Implementation

Historical record 073 measures full compilation of a 512-rule no-read chain from
10.114 to 1.003 ms and 13.414 to 1.711 MB. Read-bearing/cyclic cases have
separately scoped results; other analyses and distinct transitive sets still
consume work. Matching memo keys are unchanged, so compilation gains are not
parser-throughput estimates.

Evidence records:
[073](../073-dependency-component-propagation-of-variable-reads.md).

Source owners: [`RuleVariables and dependency
components`](../../../internal/compile/analysis.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [R010](R010-per-position-memo-chains.md), [R050](R050-bounded-variable-bindings.md).

[Optimization Catalog](../README.md)
