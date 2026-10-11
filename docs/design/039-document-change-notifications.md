# 039. Conservative Document Change Notifications

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Add versioned notifications that distinguish source edits, grammar work re-evaluated by `Document`, and observable parse-output changes. Start with exact text edits and conservative invalidation; report confirmed output changes only when comparing immutable, version-owned snapshots. Proposed APIs below are illustrative and unaccepted. Snapshot version type and identity follow [design 032](032-versioned-document-snapshots.md).

## Motivation

The public `pego.Document` applies edits in its configured position unit and incrementally reuses memo results. Reused Nodes can move in place, so retaining a previous `*Node` does not preserve the old output. `Stats` counts evaluation and reuse; it is not a change range. Versioned snapshots are proposed in design 032, and notifications need that ownership boundary before claiming an old/new output comparison.

## Goals and non-goals

- Associate each text state and parse outcome with the version defined by design 032.
- Separate exact source edits, conservative invalidation/reevaluation evidence, and parse-output comparison.
- Include scalar fields, terminal text, node shape, and positions so same-shape text changes count as output changes.
- Preserve batch coordinate semantics, no-op behavior, and version identity from design 032.
- Do not promise minimal tree-diff ranges, infer ranges from stats, or make legacy mutable Nodes safe to retain.

## Proposed contract

An illustrative notification could be returned by a future `Document.DrainChanges`:

```go
type DocumentChange struct {
    From, To DocumentVersion // the exact shared type from design 032
    Unit Unit
    Edits []TextEdit
    Reevaluation Reevaluation // unknown, broad, or ranges with defined evidence
    Output OutputChange       // unchanged, changed, may-have-changed, unavailable
}
```

This proposal does not introduce a second version type. A change record's version identity and initial version are exactly those of design 032. Construction starts at sequence 1. Every successful single Edit advances once, including identity replacements and empty insertions. Failed edits and parsing do not advance; sequence overflow is rejected before mutation. A parse attempt is associated with the version of the text it parsed and does not itself advance the text version.

Single `Edit` calls use coordinates in the current text, as today, and retain sequential version semantics. A future atomic edit batch takes every range against the same base version, validates and applies the batch atomically, and advances the shared version once for every nonempty accepted batch, including identical final text. An empty batch validates its base token without advancing. Notifications retain batch edits in that base-version coordinate system and their original order. Do not reinterpret batch ranges as sequential edits. If an API coalesces notifications from multiple operations, preserve each operation's version boundary and coordinate basis; consumers whose version is skipped must request a full refresh.

`Edits` record exact accepted replacements and the Document's position unit. Existing code-point and byte semantics, including byte-boundary validation, remain authoritative. `Reevaluation` may report `unknown`, a broad invalidated region, or ranges only where the parser can state what those ranges mean. Invalidation is not proof of actual grammar execution. Never synthesize source ranges from `Stats`.

`OutputChange` reports `unchanged` or `changed` only after comparing complete immutable snapshots for the relevant versions. Equality includes node/rule kind, child order and nil entries, field names and scalar values, terminal text, spans, and parse diagnostics/recovered errors. Replacing text while preserving node shape is therefore a change. Use `may-have-changed` when an edit can affect output but no comparison is available; use `unavailable` when the parse produced no comparable outcome. Comparison may be whole-result only; this design defines no minimal changed subtree or range.

Until design-032 snapshots exist, the API can report exact edit records and conservative `may-have-changed` status only. It must not compare a prior legacy Node because a later Document parse can mutate it. Snapshot-retention and notification-queue limits follow the snapshot ownership policy; on overflow, coalesce to an explicit full-refresh marker that retains the latest shared version and unit.

## Implementation and invalidation

Reuse the Document's existing memo invalidation as the evidence source; do not create a second invalidation algorithm in the notification layer. Initially report the accepted text edit and whole-document `may-have-changed` status. Later expose conservative invalidation ranges only after their meaning is defined. Snapshot equality is a separate stage. Position shifts, recovery, position-valued actions, same-length edits, and left-recursion dependencies must not be treated as unchanged merely because tree shape is stable.

## Compatibility and backend support

This proposal extends the public core `pego.Document`; recognition remains outside its current contract. Closure, recursive bytecode, and iterative bytecode must agree on observable snapshot equality. Evidence ranges may be implementation-specific only when labeled as such. Generated Go and TypeScript parsers do not expose `Document`; no such support is implied. Existing `Edit`, `Parse`, `Text`, and `Stats` remain source-compatible. Position values use the existing Document unit.

## Implementation sequence

1. Reuse the shared version type and no-op/batch semantics from design 032; record exact edit operations.
2. Add conservative change draining and an explicit full-refresh overflow marker.
3. Expose broad invalidation evidence only with pinned semantics.
4. Compare design-032 snapshots to report confirmed whole-output equality or change, including scalars and diagnostics.
5. Consider minimal output ranges only after the tree/edge contract in [design 031](031-iterative-tree-traversal.md) and measured need justify them.

## Alternatives considered

- **Treat stats as ranges:** counters have no source interval semantics and can hide lost reuse. Keep stats separate.
- **Diff legacy Nodes:** Document reuse can mutate the earlier result. Compare version-owned snapshots.
- **Always publish exact changed subtrees:** this needs stable identity, parent/edge and scalar semantics. Begin with whole-output comparison or a conservative status.
- **Notify only on text edits:** that leaves clients to infer output equality from shape. Keep source edits and output changes distinct.

## Testing and performance plan

Test rejected edits, advancing identity edits and nonempty identity batches, empty non-advancing batches, single sequential edits, atomic same-base batches, both position units, Unicode and malformed UTF-8 boundaries, same-shape replacements, parse without edits, syntax errors/recovery, scalar actions, positional values, memo invalidation, shifts, abort/retry, coalescing, overflow/full refresh, and version gaps. Compare incremental results with full recomputation; confirmed output status must match complete snapshot comparison on every core backend. Measure edit recording, invalidation, snapshot comparison/retention, and notification delivery separately across input and tree sizes. No measurements or timing promises are available.

## Limitations and open questions

The queue/drain method shape remains a proposal; recommendation: pull-based draining with explicit full-refresh overflow and consumer-owned snapshots. Version type, identity, no-op behavior, and atomic batch coordinates are inherited from design 032, not re-decided here. Work-limit and cancellation errors follow [design 033](033-cancellable-parses-and-work-budgets.md).
