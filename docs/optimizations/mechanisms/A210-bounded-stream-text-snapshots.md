# A210: Bounded Stream Text Snapshots

Many small stream tokens can share one immutable snapshot of loaded text instead
of creating a string copy for each token.

## Mechanism and Invariants

A cache miss may snapshot a range of at most 1,024 positions when at least 64
positions are loaded. It never reads the reader. Empty ranges return empty text;
oversized or undersized-loaded ranges copy. CodePoints snapshots preserve U+FFFD
normalization and offset mapping; Bytes preserves raw bytes. Returned strings
remain valid after compaction and retain at most one bounded snapshot, up to
4,096 bytes.

## Scope and State

Default eligible stream text in closure and both VMs only. Batch substring
sharing is a different input path.

## Reference Control

Build tag `pego_reference_stream_text` selects copying. Both same-source
variants retain the input cache fields, so parent comparisons also include their
layout cost.

## Evidence and Implementation

Record 088 measures large CSV allocations falling by about 97%, with
workload-specific timing benefits. Small recursive-VM CodePoints streams cost
about 3% more; most small paired timing ranges overlap parity. Fewer objects do
not ensure less allocated volume or faster every stream.

Evidence records: [088](../088-share-bounded-stream-text-snapshots.md).

Source owners: [`bounded snapshot
cache`](../../../internal/engine/stream_text_snapshots.go), [`copy
reference`](../../../internal/engine/stream_text_snapshots_reference.go),
[`input.text`](../../../internal/engine/input.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A120](A120-source-token-text.md), [A050](A050-streaming-state-reuse.md).

[Optimization Catalog](../README.md)
