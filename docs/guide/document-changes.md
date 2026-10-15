# Document change notifications

Enable notifications on a Document before the edits and parses you want to
observe:

```go
doc, err := parser.NewDocument(source)
if err != nil {
    return err
}
if err := doc.EnableChanges(64); err != nil {
    return err
}
snapshot, parseErr := doc.Snapshot()
changes := doc.DrainChanges()
```

Tracking is off by default. The positive limit bounds event count; it does not
bound replacement-string bytes or the size of the comparison Snapshot. Repeated
enabling is rejected. Draining transfers owned records and clears the queue,
while tracking retains one comparison Snapshot.

Events have three kinds:

- `ChangeEdit` records an accepted edit or atomic batch. `From` is the coordinate
  base and `To` the resulting source revision. Batch edits remain simultaneous
  replacements in the base coordinates and original order. Failed operations and
  empty batches emit no event. Identity edits still advance and emit events.
- `ChangeOutcome` records a Parse or Snapshot outcome. `From` identifies the
  preceding comparable capture, and `To` the current revision. These events may
  cross multiple previously delivered edits; they do not advance an edit-replay
  cursor. Track source and displayed-output revisions separately.
- `ChangeFullRefresh` replaces overflowed history. Rebuild from the latest state;
  its marker cannot reconstruct dropped edits or intermediate comparisons. A gap
  in edit revisions also requires rebuilding.

Edit events mark the whole output conservatively invalidated. Reevaluation is
`unknown`, because invalidation and memo statistics cannot establish execution
ranges. Notifications never promise minimal tree differences.

The first capture reports `OutputMayHaveChanged`. Later complete captures report
`OutputUnchanged` or conservative `OutputChanged`, comparing immutable node
metadata, positions, terminal text, children, scalar fields, syntax diagnostics,
and graph sharing. Alias topology and nil-versus-empty collection differences
count as changed; this is a refresh policy rather than language value equality.
Unknown non-syntax diagnostic types conservatively count as changed. Source text
and parse statistics are not output equality.

Both Parse and Snapshot capture/compare when tracking is enabled. That work uses
the invocation's cumulative context and work limits. Cancellation or work-limit
interruption returns nil and emits `OutputUnavailable`, clearing comparison
continuity. The next successful capture reports `OutputMayHaveChanged`. Completed
syntax failures remain comparable. Unsupported captures also report unavailable.

Snapshots retained by consumers stay immutable. Parse still returns its legacy
mutable Node, so use Snapshot for output that must survive later Document parses.
For a conservative consumer, rebuild indexes and highlights from the latest
Snapshot on source invalidation, changed/unavailable output, a version gap, or a
full-refresh marker. Keep the edit cursor separate from the last displayed
Snapshot version.
