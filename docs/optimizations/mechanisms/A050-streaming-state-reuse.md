# A050: Bounded Stream Storage

Streaming should discard committed prefixes and obsolete memo slots instead of
growing input storage with every emitted element.

## Mechanism and Invariants

Loaded input has a retained window relative to committed positions.
Discard/prune drops prefixes while preserving bytes or code points still needed
by active matching, memo and diagnostics. Published token strings/Nodes retain
independent ownership. Reader chunks and decoded storage compact according to
the stream policy; result callbacks can still retain arbitrary output.

## Scope and State

Default engine streaming in closure and both VMs. Generated Go Node/typed Go and
TypeScript expose no streaming API. This bounds particular input/memo owners,
not total process heap.

## Reference Control

No single same-source old-stream-storage reference is maintained.
Frame/text/prefix/undo/stack controls are narrower independent mechanisms.

## Evidence and Implementation

Historical record 022 combines result-chunk splitting and in-place read-ahead
compaction. Streaming 50,000 CSV records changes 168.9 to 117.7 ms in closure,
with allocated volume 686 to 351 MB. A retention fixture changes 233 to 0.5 MB
after 300,000 records. These are different timing, cumulative-allocation and
reachable-heap observations; later cleanup controls cannot be attributed to this
historical ratio.

Evidence records:
[022](../022-bounded-memory-and-less-copying-in-stream-parsing.md).

Source owners: [`input.discard and
parser.commit`](../../../internal/engine/input.go),
[`memoTable.prune`](../../../internal/engine/memo.go).

The [comparison policy](../../design/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A200](A200-stream-capture-frame-reuse.md), [A210](A210-bounded-stream-text-snapshots.md), [A220](A220-stream-prefix-isolation.md), [A230](A230-stream-undo-cleanup.md), [A240](A240-stream-vm-stack-cleanup.md).

[Optimization Catalog](../README.md)
