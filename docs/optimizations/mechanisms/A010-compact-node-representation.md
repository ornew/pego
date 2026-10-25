# A010: Compact Node Representation

Every constructed Node carries names, positions, children and fields. Compact
fields and shared kind metadata reduce per-node backing without changing the
tree's observable names or JSON representation.

## Mechanism and Invariants

`Fields` is an ordered slice of name/value pairs rather than a per-node map.
`nodeKind` interns the type/rule pair, and rules precompute their kinds so
matching does not perform an interning lookup. Node positions are `int32`;
callers retrieve names through `Type()` and `Rule()`. Field lookup and sorted
JSON output preserve their contracts. Slabs and snapshot records are separate
storage mechanisms.

## Scope and State

Default Node layout in closure, both VMs and generated Go Node output. Typed Go
results use their own structs; TypeScript uses JavaScript objects and does not
share the Go layout.

## Reference Control

No private same-source old-layout comparator is maintained. Comparing a
historical Node layout changes public representation as well as size.

## Evidence and Implementation

Record 003 reports JSON allocation volume from 72 to 64 MB after replacing maps.
Record 063 measures 11–20% lower Go allocation volume, with timing within
roughly 3%; record 064 records generated allocation reductions with mixed small
timing effects. The recorded 88-byte Node size is host/layout-specific and does
not imply an equal percentage reduction in total heap.

Evidence records: [003](../003-node-fields-as-a-slice-54a5774.md),
[063](../063-a-smaller-node.md),
[064](../064-a-smaller-node-in-generated-parsers.md).

Source owners: [`Node and nodeKind`](../../../internal/engine/node.go),
[`generated Node and Fields`](../../../runtime/runtime.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A020](A020-node-and-list-slabs.md), [A110](A110-expected-set-ids.md).

[Optimization Catalog](../README.md)
