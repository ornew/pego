# 039. Conservative Document Change Notifications

- **Status**: Accepted
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Add versioned notifications that distinguish source edits, grammar work re-evaluated by `Document`, and observable parse-output changes. Start with exact text edits and conservative invalidation; report confirmed output changes only when comparing immutable, version-owned snapshots. The opt-in API below uses the snapshot version type and identity from [design 032](032-versioned-document-snapshots.md).

## Motivation

The public `pego.Document` applies edits in its configured position unit and incrementally reuses memo results. Reused Nodes can move in place, so retaining a previous `*Node` does not preserve the old output. `Stats` counts evaluation and reuse; it is not a change range. Versioned snapshots are implemented in design 032, and notifications need that ownership boundary before claiming an old/new output comparison.

## Goals and Non-Goals

- Associate each text state and parse outcome with the version defined by design 032.
- Separate exact source edits, conservative invalidation/reevaluation evidence, and parse-output comparison.
- Include scalar fields, terminal text, node shape, and positions so same-shape text changes count as output changes.
- Preserve batch coordinate semantics, no-op behavior, and version identity from design 032.
- Do not promise minimal tree-diff ranges, infer ranges from stats, or make legacy mutable Nodes safe to retain.

## Contract

Enable tracking with `Document.EnableChanges(queueLimit)` and pull events with
`Document.DrainChanges`. Tracking is disabled by default. A positive queue limit
is required; repeated enabling is rejected without changing state.

```go
func (d *Document) EnableChanges(queueLimit int) error
func (d *Document) DrainChanges() []DocumentChange

type DocumentChange struct {
    Kind DocumentChangeKind // edit, outcome, full-refresh
    From, To DocumentVersion
    Unit Unit
    Edits []TextEdit
    FullRefresh bool
    Invalidated bool // conservative whole-output invalidation, not execution
    Reevaluation string // currently "unknown"
    Output OutputChange // unchanged, changed, may-have-changed, unavailable
}
```

`ChangeEdit` events advance the source revision: `From` identifies the edit's
coordinate basis and `To` its accepted result. `ChangeOutcome` events identify
the previously captured outcome and current captured revision. They do not
advance a source-edit replay cursor and may cross several already-delivered edit
operations. Consumers track source and displayed-outcome revisions separately.
`ChangeFullRefresh` events explicitly replace lost history and require rebuilding
from the latest state. An edit-stream version gap also requires a full refresh.
No event is emitted for enabling tracking or an empty validated batch.


This API does not introduce a second version type. A change record's version identity and initial version are exactly those of design 032. Construction starts at sequence 1. Every successful single Edit advances once, including identity replacements and empty insertions. Failed edits and parsing do not advance; sequence overflow is rejected before mutation. A parse attempt is associated with the version of the text it parsed and does not itself advance the text version.

Single `Edit` calls use coordinates in the current text, as today, and retain sequential version semantics. An atomic edit batch takes every range against the same base version, validates and applies the batch atomically, and advances the shared version once for every nonempty accepted batch, including identical final text. An empty batch validates its base token without advancing. Notifications retain batch edits in that base-version coordinate system and their original order. Do not reinterpret batch ranges as sequential edits. If an API coalesces notifications from multiple operations, preserve each operation's version boundary and coordinate basis; consumers whose version is skipped must request a full refresh.

`Edits` record exact accepted replacements and the Document's position unit. Existing code-point and byte semantics, including byte-boundary validation, remain authoritative. `Reevaluation` may report `unknown`, a broad invalidated region, or ranges only where the parser can state what those ranges mean. Invalidation is not proof of actual grammar execution. Never synthesize source ranges from `Stats`.

`OutputChange` reports `unchanged` or `changed` only after comparing complete immutable snapshots for the relevant versions. Equality includes node/rule kind, child order and nil entries, field names and scalar values, terminal text, spans, and parse diagnostics/recovered errors. Replacing text while preserving node shape is therefore a change. Use `may-have-changed` when an edit can affect output but no comparison is available; use `unavailable` when the parse produced no comparable outcome. Comparison may be whole-result only; this design defines no minimal changed subtree or range.

Only immutable, version-owned snapshots are compared; legacy mutable Nodes are
never used as the old outcome. The count-bounded queue and one retained
comparison snapshot follow the retention policy below.

## Implementation and Invalidation

Reuse the Document's existing memo invalidation as the evidence source; do not create a second invalidation algorithm in the notification layer. Edit events report accepted text edits and conservative whole-output invalidation with reevaluation marked `unknown`. Outcome events compare complete immutable snapshots; position shifts, recovery, position-valued actions, same-length edits, and left-recursion dependencies must not be treated as unchanged merely because tree shape is stable.

## Compatibility and Backend Support

This API extends the public core `pego.Document`; recognition remains outside
its current contract. Closure, recursive bytecode, and iterative bytecode must
agree on observable snapshot equality. Evidence ranges may be
implementation-specific only when labeled as such. Generated Go parsers expose
a separate mutable Node `Document` and, with typed generation,
`Document.ParseAST` ([design 044](044-generated-incremental-and-stream-parsers.md));
neither generated API exposes these change notifications. TypeScript generation
has no `Document` API. Existing `Edit`, `Parse`, `Text`, and `Stats` remain
source-compatible. Position values use the existing Document unit.

## Implemented Scope

The opt-in path records accepted edits, queues versioned edit/outcome/full-refresh
notifications, and compares immutable snapshots after parsing. Edit events retain
base-coordinate operations and conservative invalidation evidence; outcome events
report whole-output comparison only. Minimal changed subtrees/ranges remain out of
scope. The notification API is limited to the core `pego.Document`; generated Go
Documents do not emit these events, and TypeScript generation has no Document API.

## Alternatives Considered

- **Treat stats as ranges:** counters have no source interval semantics and can hide lost reuse. Keep stats separate.
- **Diff legacy Nodes:** Document reuse can mutate the earlier result. Compare version-owned snapshots.
- **Always publish exact changed subtrees:** this needs stable identity, parent/edge and scalar semantics. Begin with whole-output comparison or a conservative status.
- **Notify only on text edits:** that leaves clients to infer output equality from shape. Keep source edits and output changes distinct.

## Testing and Performance Plan

Focused normal and snapshot-directory-ID comparison tests cover same-shape edits, scalar and terminal changes, aliases/cycles, typed nil, empty collections, diagnostics, budget interruption and retry across core backends and both position units. The full root suite, generated-parser checks and site checks passed. The measurements below compare both the opt-in edit-and-capture path and the default-off path against its pre-feature revision.

## Capture, Comparison and Retention

Enabling tracking applies to future `Parse` and `Snapshot` calls. Both capture a
complete immutable outcome and compare with the last retained outcome inside the
same cumulative context and work budget as parsing. Node traversal, diagnostic
comparison, field inspection and bounded text comparisons charge that controller.
`Parse` still returns its ordinary legacy Node, with its existing mutable reuse
contract; tracking does not make that result safe to retain across edits.

A first completed capture reports `may-have-changed`. Syntax failures with a
completed snapshot remain comparable. Interrupted parsing, capture or comparison
returns nil, emits an `unavailable` outcome event, and clears the comparison
baseline. A successful retry then reports `may-have-changed`. The candidate
snapshot is published only after comparison and the final cancellation check;
comparison interruption cannot publish a partial snapshot or an unchanged claim.
Unsupported captures also break continuity and report unavailable. Opting in may
therefore fail an invocation that previously needed only legacy Node output.

Equality includes metadata/spans, terminal text, child slots, scalar fields in
stored order, typed nil values, complete syntax diagnostics, and alias topology.
Nil versus allocated-empty collections are distinguished. Repeated references and
cycles are compared by immutable snapshot directory IDs directly, without building
a bijective identity mapping. Alias or allocation-state differences conservatively
count as changed and require a refresh; this notification policy does not define
language value equality. Unknown non-syntax diagnostic types conservatively count
as changed rather than invoking uncontrolled formatting or custom equality code.
Source text and Stats are excluded from output equality: ignored source edits
may leave observable output unchanged.

Accepted operations retain the supplied replacement string. CodePoints
normalization and byte-boundary validation remain Document behavior. Rejected
operations enqueue nothing. Edit events invalidate the whole output conservatively
and report reevaluation as unknown. No second memo invalidation algorithm or
minimal changed range is introduced.

The queue bounds event count, not replacement-string bytes or snapshot size.
Edit and outcome events each count as one record. Overflow replaces queued
history with one full-refresh marker retaining the oldest queued base and latest
version/unit. Further events update that marker until draining. Intermediate
edits and comparisons cannot be reconstructed from it. Draining transfers owned
records and clears the queue; caller mutations affect no retained state.
Tracking retains at most one comparison Snapshot in addition to snapshots retained
by consumers. Snapshot handles may independently keep old storage alive through
ordinary garbage collection. Disabled tracking introduces no capture or comparison
work into ordinary Parse.

## Performance and Results

A serial `BenchmarkDocumentChangesSnapshotEdit` comparison used GOMAXPROCS=1,
100 ms samples repeated five times, 4 KiB and 64 KiB inputs, all three core
backends and both position units. It measures opt-in same-length middle edits
through `Document.Edit`, `Snapshot` and `DrainChanges`; it is not a comparison of
default-off parsing with pre-feature main. An earlier enabled/disabled
implementation measured 1.96–2.12× median time, 1.78–1.83× bytes and
1.67–1.97× allocations. After replacing bijective graph mapping with direct
snapshot-directory-ID comparison, candidate/disabled time ratios were 0.985–1.140,
bytes were approximately 1.000× and allocations 1.005–1.031×. The optimized
candidate greatly reduces the opt-in overhead in this workload; ratios just
below 1× are within measurement noise, not evidence that enabling notifications
improves parsing speed. Other workloads and retained-memory effects were not
measured.

To check the default-off path, the existing
`BenchmarkDocumentSnapshotMiddleEdit` ran on the pre-feature main and this
revision in six alternating pairs of 300 ms samples. Candidate/main median
time ratios were 1.006× at 4 KiB and 1.023× at 64 KiB; bytes and allocation
counts were 1.000×. Three additional alternating pairs of one-second samples
gave 1.002× and 0.992× respectively, again with unchanged allocations. These
samples do not establish a consistent default-off timing regression.

## Validation Status

Focused normal and snapshot-directory-ID comparison tests, the full root suite,
generated-parser checks, site checks and independent static review passed.
