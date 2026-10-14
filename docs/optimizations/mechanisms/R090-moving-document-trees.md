# R090: Moving Document Trees

After an edit shifts reused results, copying every descendant defeats
incremental reuse. Generation-aware in-place movement updates positions of
reusable Nodes.

## Mechanism and Invariants

Each Node tracks the edit generation represented by its positions. `moveResult`
applies accumulated shifts, replaying edits only when a shared descendant is
already current through another result. Empty nodes at insertion points are
ambiguous and use a copy path that preserves descendant shifts.
Length-preserving edits need no position movement. Earlier mutable trees can
change on later Document parses; Clone or owned snapshots preserve older
versions.

## Scope and State

Default engine Document result reuse in closure and both VMs. Generated
parsers/TypeScript have no Document move path.

## Reference Control

No maintained current-source copy-every-tree control exists. Snapshot sharing
compares immutable record storage and does not disable mutable tree movement.

## Evidence and Implementation

Historical record 045 measures Minilang one-character edit/reparse from 1.34 to
0.90 ms in closure, with 2.4 to 0.34 MB and 151 to 49 allocations. Record 029
previously reduced tree-copy allocation using chunks. These revisions do not
isolate current snapshot capture or every edit schedule; earlier mutable trees
can change as positions move.

Evidence records:
[029](../029-shifting-reused-subtrees-into-the-parsers-chunks.md),
[045](../045-moving-reused-trees-in-place-after-document-edits.md).

Source owners: [`moveResult and moved Node
handling`](../../../internal/engine/runtime.go), [`Node generation and
Clone`](../../../internal/engine/node.go), [`edit log and result
shift`](../../../internal/engine/document.go).

The [comparison policy](../../design/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [R130](R130-document-snapshot-record-sharing.md), [R140](R140-share-document-snapshot-child-runs.md).

[Optimization Catalog](../README.md)
