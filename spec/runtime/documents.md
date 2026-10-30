# Incremental Documents

A `Document` holds a text that changes over time, applies edits to it and parses
the current text. Reparsing after an edit reuses results of the previous parse
that the edit did not affect. Reuse never changes the outcome: a parse of a
document is the same as a parse of the current text from scratch. This chapter
defines the document API, its versions and snapshots, change notifications and
reuse-history controls.

## Scope and Terminology

A **document** is a `Document`, created by `Parser.NewDocument`. The **text** is
the current content of the document. A **version** is the opaque token returned
by `Document.Version`. A **snapshot** is an immutable capture of one revision's
text, parse outcome and value graph. A **batch** is a set of replacements that
apply simultaneously to one base revision.

Positions and edit ranges use the document's position unit, which is fixed when
the document is created; see [Positions](parsing.md#positions).

## Contract

### Construction and Ownership

| Operation | Behavior |
|:--|:--|
| `p.NewDocument(text, opts...)` | Returns a document holding `text`. `WithUnit` fixes the position unit (also the unit of edits) and `WithBackend` fixes the backend; `WithContext` and `WithLimits` set defaults for initialization and later parses. It fails for `RecognizeOnly`, for an unavailable backend, for an unknown start rule and when initialization is interrupted. Nothing is parsed |
| `doc.Text()` | The current text |
| `doc.Unit()` | The fixed position unit |
| `doc.Version()` | A comparable opaque token identifying the current text revision. A new document starts at sequence 1. Parsing does not advance the version. The token does not retain the document, the text or parse caches |

A `Document` has one owner and is not safe for concurrent use; a `Parser` is safe
for concurrent use. A document is always parsed as a whole: `#stream` has no
effect on it and it is not a stream parse. Grammars that use `#missing` are
rejected.

For `Bytes`, construction can charge no budget, because a large immutable
input need not be scanned or copied. For `CodePoints`, initialization decodes and
normalizes the input, and the inspected input is charged.

### Edits

`doc.Edit(start, end, text)` replaces `[start, end)` with `text`, in the
document's unit. `start == end` inserts and an empty `text` deletes. It returns
an error if the range is outside the text and, in `Bytes`, if `start` or `end`
lies inside a character. It does not parse. A successful `Edit` advances the
version once, including an edit that changes nothing. A failed edit changes
nothing.

`doc.EditBatch(base, edits...)` applies replacements simultaneously to the text
identified by the token `base`.

- Every range uses coordinates in the original base text and the document's unit.
- Starts MUST be strictly increasing and ranges MUST NOT overlap. A repeated
  start, including several insertions at one position, is rejected.
- A token from another document or from an older revision is rejected, even for
  an empty batch.
- An empty valid batch changes neither text nor version. A successful nonempty
  batch advances the version once and does not parse. Version sequence overflow
  is rejected before any change.
- The complete batch is validated and prepared before it is published. A
  rejected batch leaves the text, version, statistics, reusable state and
  previously returned nodes unchanged.
- In `Bytes`, endpoints are checked against character boundaries of the base
  text. Replacements can create or remove UTF-8 sequences at their seams; the
  next parse decodes them. In `CodePoints`, invalid UTF-8 in replacement text is
  normalized, one replacement character per invalid byte.
- After a successful batch the next parse equals a fresh parse of the resulting
  text. Committing a batch clears reusable parse caches; it does not promise the
  reuse that the same replacements applied as separate `Edit` calls could give.

In `Bytes`, only the interior of a valid UTF-8 character is excluded as an edit
boundary. Each invalid byte is a separate replacement character, so text can be
replaced at it, or inserted before or after it. An edit can join an incomplete
prefix and continuation bytes into a valid character; the next parse
invalidates results that depended on the earlier decoding, including a prefix
completed by an insertion at the end of the text. In `CodePoints`, invalid bytes
have already been replaced with U+FFFD, and editing the normalized text does not
reassemble the original bytes.

`Edit` and `EditBatch` are outside parse budget limits.

### Parsing

`doc.Parse(opts...)` parses the current text using the construction defaults
overridden by the options of the call. Context, budget limits, maximum depth and
tracing MAY vary per call; the unit, backend, start rule and non-recognition mode
MUST NOT, and an option that tries to change them is an error. Counters reset on
each call. Results and errors are those of `Parser.Parse`, including recovered
`SyntaxErrors`.

`Parse` publishes recovered parser-produced syntax diagnostics as caller-owned
values: it copies each `SyntaxError` and its `Expected` and `Messages` slices, so
a caller's mutation cannot change memoized errors or a later parse. Ordinary parse
failures are newly constructed; caller-supplied errors such as `Context.Err` keep
their identity. This does not detach the mutable `Node` graph, which MAY share
nodes with the document, and nodes that an earlier parse returned MAY be moved in
place by a later one. Typed values returned by a generated parser's
`Document.ParseAST` are read-only and shared with the document and earlier results;
its generated `CloneAST` returns an independent copy (see
[Generated Parsers](generated-parsers.md)). Parsing and diagnostic publication use
the same invocation controls. If publication is interrupted, no result is returned and reusable
execution state is cleared for retry.

### Statistics

| Operation | Behavior |
|:--|:--|
| `doc.Stats()` | `ParseStats` of the last parse. `Evaluated` counts rule bodies run, `MemoHits` and `ResumedElements` count the two kinds of reuse, and `Reused` is their sum |
| `doc.CacheStats()` | Point-in-time counts of the retained reuse cache, read by the owner of the document |
| `doc.ResetHistory()` | Drops memo, repetition and reusable parser history and keeps the text, the version, the construction options and the last statistics. The next parse starts fresh. No threshold applies; the caller chooses when to reset. Nodes returned earlier stop moving through reuse; movement from parses before the reset is not undone |

`CacheStats` has the fields `LiveMemoEntries` (memo entries linked for reuse),
`RetiredMemoEntries` (retired entries retained for reuse), `RepetitionElements`
(elements in resumable repetition runs) and `EstimatedBytes` (approximate
cache-owned backing storage). All four are zero immediately after
`ResetHistory`. `EstimatedBytes` excludes the text, caller-held result trees,
shared strings, shared expectation, error and environment payloads, map buckets
and allocator overhead; it is neither process heap use nor a limit.

### Snapshots

`doc.Snapshot(opts...)` captures the current text revision, parse outcome and
value graph in one immutable `Snapshot`. It resolves invocation overrides once
over the construction defaults, with the same variable and fixed options as
`Parse`. Parsing and freezing use the same cancellation context and the same
cumulative spend counter. Copying the text, freezing
nodes and fields and copying diagnostics are part of that invocation.

A normally returned parse outcome produces a snapshot, including an ordinary
syntax error and a recovered error. For an ordinary syntax failure the snapshot
keeps the text and version, has a zero root as `Document.Parse` does, and exposes
a defensive copy of the `SyntaxError`. A recovered outcome keeps its root and
`SyntaxErrors`. Cancellation or an exhausted budget limit during parsing or
freezing returns a nil snapshot and a fatal interruption error, which grammar
choice, predicates and recovery cannot absorb. No partial graph, text or
diagnostics are published. If execution began, reusable execution state is
cleared before retry; completed input tables MAY remain and partial tables are
discarded. An error before execution keeps existing caches and statistics.
Cleanup is not charged. A trace callback panic propagates unchanged after
cleanup. Snapshot accessors and `Materialize` run after capture and do not
inherit its context or budget limits.

| Accessor | Meaning |
|:--|:--|
| `snapshot.Root()` | The captured root, a `SnapshotNode` |
| `snapshot.Text()` | The exact text of the revision |
| `snapshot.Version()` | The revision token |
| `snapshot.Error()` | The diagnostic of that revision |
| `snapshot.Materialize()` | A new independent mutable `*Node` graph. Each call creates a separate graph |

`SnapshotNode` is a read-only handle. Its zero value represents a nil node.
`ChildCount` and `Child` address the elements of a tuple or list node, and `ChildCount` includes nil slots. `Field(name)` returns a value and a
presence flag that distinguishes an absent field from a present nil value; a typed
nil `*Node` field becomes a zero handle and an untyped nil stays nil. `Fields()`
returns an owned slice in original order. Handles and snapshots are immutable and
safe for concurrent reads while the document has one editing and parsing owner.

Freezing preserves shared node identities, cycles, nil child slots and nil versus
allocated-empty collections. It accepts field values of dynamic type `nil`, `int`,
`string`, `bool` and `*Node`, including a typed nil `*Node`. Any other dynamic
type returns no snapshot and a `*SnapshotCaptureError` that contains the
deterministic path and the `reflect.Type`, without retaining the unsupported
value; the error also carries a defensive copy of the ordinary parse error, if one
existed, and preserves `errors.Is` and `errors.As` behavior for it. `Materialize`
preserves identities, cycles, nil slots and nil versus allocated-empty
collections.

### Interruption

A cancellation or option error detected before execution touches reusable state
preserves valid caches and the last statistics. Once execution starts, an
interruption clears memo and repetition state before the next attempt, because
entries can depend on unfinished left-recursion work. Fully completed input
tables MAY be retained; incomplete tables are never published. The text and
options stay available for retry or editing. A retry with fresh controls MUST
produce the result of parsing the current text from scratch. Syntax errors and
recovered errors are completed outcomes and keep their reusable caches.

### Reuse

Reuse is a performance property, not a result property. An implementation MAY
retain, shift, drop or recompute any intermediate result, provided that every
parse returns what a parse of the current text from scratch returns. Reuse is
observable only through the statistics.

## Change Notifications

Notifications are off by default. `doc.EnableChanges(limit)` turns them on for
one document before the edits and parses to be observed. The positive `limit`
bounds the number of queued events; it does not bound the bytes of replacement
text or the size of the comparison snapshot. Enabling twice is rejected.
`doc.DrainChanges()` transfers owned records out and clears the queue. Tracking
retains one comparison snapshot.

There are three kinds of event.

| Kind | Meaning |
|:--|:--|
| `ChangeEdit` | An accepted edit or atomic batch. `From` is the coordinate base and `To` the resulting source revision. Batch edits remain simultaneous replacements in the base coordinates and original order. Failed operations and empty batches emit nothing. Identity edits still advance the version and emit an event |
| `ChangeOutcome` | A `Parse` or `Snapshot` outcome. `From` identifies the preceding comparable capture and `To` the current revision. It MAY cover several previously delivered edits and does not advance an edit-replay cursor, so source revisions and displayed-output revisions are tracked separately |
| `ChangeFullRefresh` | Replaces history that overflowed the limit. Consumers rebuild from the latest state; the marker cannot reconstruct dropped edits or intermediate comparisons. A gap in edit revisions also requires a rebuild |

Edit events mark the whole output conservatively invalidated, and their
re-evaluation is reported as `unknown`, because invalidation and memo statistics
cannot establish execution ranges. Notifications never promise minimal tree
differences.

The first capture reports `OutputMayHaveChanged`. Later complete captures report
`OutputUnchanged` or a conservative `OutputChanged`, comparing immutable node
metadata, positions, string text, elements, scalar fields, syntax diagnostics
and graph sharing. Alias topology and nil versus empty collection differences
count as changes; this is a refresh policy and not language value equality.
Unknown non-syntax diagnostic types count as changed. Source text and parse
statistics are not part of output equality.

With tracking enabled, both `Parse` and `Snapshot` capture and compare, using the
invocation's cumulative context and budget limits. A cancellation or budget limit
interruption returns nil and emits `OutputUnavailable`, which clears comparison
continuity: the next successful capture reports `OutputMayHaveChanged`. Completed
syntax failures stay comparable. Unsupported captures also report unavailable.

Snapshots that consumers retain stay immutable. `Parse` still returns its mutable
`Node`, which later parses of the document can change; a `Snapshot` never changes.
The `To` of a `ChangeEdit` is the document version after the edit, and the `To` of a
`ChangeOutcome` is the document version when the `Parse` or `Snapshot` ran. The
version of the latest outcome can therefore be older than the version of the latest
edit: edits that follow an outcome are not part of it.

## Validation and Errors

| Condition | Result |
|:--|:--|
| `NewDocument` with `RecognizeOnly`, an unavailable backend or an unknown start rule | Error; no document |
| A grammar that uses `#missing`, or a `WithMissingTokens` limit other than zero, in `NewDocument` or `Parse` | Error; no document or result |
| `Edit` range outside the text, or inside a character in `Bytes` | Error; text and version unchanged |
| `EditBatch` with a foreign or stale version, unordered or overlapping edits, repeated starts or invalid ranges | The whole batch is rejected |
| Parse option that changes unit, backend, start rule or mode | Error before execution; caches and statistics kept |
| Cancellation or budget limit during `Parse` | Error and no result; execution state cleared |
| Cancellation or budget limit during `Snapshot` | Nil snapshot and a fatal error |
| Unsupported field value type while freezing | Nil snapshot and `*SnapshotCaptureError` |
| `EnableChanges` twice | Error |
| Stream diagnostic options (`WithDiagnosticCallback`, `WithDiagnosticLimit`) | Rejected |

## Compatibility and Support

All three engine backends and both position units support documents. Generated Go
parsers provide an opt-in `Document` with `NewDocument`, `Text`, `Edit`, `Parse`,
`Stats` and `Node.Clone`, and, with typed values, `Document.ParseAST`; they have
neither `EditBatch`, `Version`, `Unit`, `Snapshot`, `CacheStats`, `ResetHistory`
nor change notifications, and their `Stats` has only `Evaluated` and `Reused`.
Generated TypeScript parsers have no document. See
[Generated Parsers](generated-parsers.md).

## Conformance

The behavior is established by the tests of `internal/engine/document` (among
them `document_edits_test.go`, `snapshot_test.go`, `document_changes_test.go`
and `budget_document_test.go`), by `internal/engine/document_test.go`, which
compares random edits with fresh parses, and by the edit fuzzer of
`pego edit-fuzz`, which compares every reparse with a fresh parse.
