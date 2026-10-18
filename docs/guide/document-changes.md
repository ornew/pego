# Document Change Notifications

A tool that keeps derived data (an outline, an index, highlights) next to a `Document` can ask it to record what changed
instead of comparing trees itself. This guide shows how to enable the notifications, what the events say, and how a
consumer should treat them. It assumes the `Document` API of [Incremental Parsing](incremental.md).

Tracking is off by default; enable it before the edits and parses you want to observe:

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

The positive limit bounds the number of events, not the size of replacement strings or of the comparison `Snapshot`
that tracking retains. `DrainChanges` transfers the recorded events and clears the queue. The events say that an edit
was applied (`ChangeEdit`), what a parse or snapshot produced compared with the previous one (`ChangeOutcome`), or that
the history overflowed and you must rebuild from the latest state (`ChangeFullRefresh`).

A conservative consumer rebuilds its indexes from the latest `Snapshot` when it sees source invalidation, a changed or
unavailable output, a gap in revisions or a full-refresh marker, and keeps the edit cursor separate from the version
of the last displayed `Snapshot`. Notifications never promise a minimal difference between trees. The kinds of events,
what counts as a change and how interruption is reported are defined in
[Change Notifications](../../spec/documents.md#change-notifications).

## Related Topics

- [Incremental Parsing](incremental.md): the model, the API and `Snapshot`.
- [Incremental Parsing Performance](incremental-performance.md): what the notifications cost.
- [Documents](../../spec/documents.md#change-notifications): the normative definition of the events.
