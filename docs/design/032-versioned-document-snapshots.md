# 032. Versioned Document Snapshots and Atomic Edit Batches

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Add explicit text versions, atomic edit batches, and immutable parse snapshots to
`Document`. Versioned edit batches and the owned Snapshot API are implemented.
Existing `Document.Edit`, `Document.Parse`, mutable `Node` fields, and
`Node.Clone` retain their ownership contracts.

Snapshots expose an immutable graph. Exact sharing of immutable snapshot records
and eligible child-ID runs across versions is selected for default adoption;
relative internal spans remain unimplemented. Mutable parser Nodes and input
buffers are never shared with snapshots.
Chunked input storage is a separate mechanism under
[design 019](019-incremental-document-and-tree-tooling.md).

## Motivation

Editor consumers need to keep the displayed result while applying further edits,
associate diagnostics with the exact text that produced them, and apply one
editor transaction without partially accepting invalid changes. Today, callers
can deep-clone a tree before reparsing, but must separately manage text, errors,
versions, and batch rollback.

Source inspection at `d16f33e04d9f71605682166a4dccddbc072e4c51` establishes these
facts, rather than current performance estimates:

- [`Document`](../../pego.go) is single-owner mutable state. Several edits may
  precede one parse; construction does not parse the input.
- [`Document.Parse`](../../internal/engine/document.go) reuses memo results and
  repetition records. [`moveResult` and `moveTree`](../../internal/engine/runtime.go)
  move reused nonempty Nodes in place, so earlier returned trees can change.
  Empty nodes can require copies because their placement is contextual.
- [`Node.Clone`](../../internal/engine/node.go) copies children and node-valued
  fields with an identity map. Shared descendants stay shared in the clone;
  captured strings and scalar fields are immutable values. Clone does not capture
  document text, a version, or diagnostics.
- [`input.replace`](../../internal/engine/input.go) moves contiguous byte/rune
  storage and rebuilds a source string; code-point edits update an offset suffix.
  Tree sharing alone does not remove these input costs.
- Ordinary failures and recovered errors are completed outcomes; interrupted
  action/depth failures and trace panics discard reusable parse state. Text and
  options survive. [Existing interruption tests](../../internal/engine/document_abort_test.go)
  establish retry behavior.

Edit-batch, Snapshot capture, materialization, and retained-version measurements
are recorded below. Older incremental-guide measurements are not a baseline for
this revision.

## Goals

- Associate every snapshot, span, and diagnostic with one document version and
  position unit; preserve it across later edits, reparses, and caller mutations
  of separately returned legacy Nodes.
- Validate a whole edit batch before publishing any change. Reject stale versions,
  invalid ordering/ranges, and invalid byte boundaries without changing text,
  version, caches, statistics, or prior results.
- Give readers concurrent access to completed snapshots while one goroutine owns
  the mutable Document. Expose no mutable backing slices or Nodes.
- Match existing parse values, graph sharing, terminal text, errors, and reuse
  dependencies on all core Document backends and both units.
- Separate correctness from representation tuning, with independently measured
  snapshot capture, edit, reparse, materialization, and retained-version costs.

## Non-Goals

This record does not change PEG choices, actions, recovery, `Node` serialization,
`Node.Clone`, or the mutable Document contract. It does not add generated Go/TS
Documents, typed snapshot conversion, streams, automatic concurrent Document
editing, undo/redo, exact change notifications, tree queries, or cross-document
persistent caches. Input ropes/piece tables and caller-owned storage belong to
the separate input-storage experiment. Wider position representations are not
part of this feature.

## Design

### Public API and Implementation State

Version tokens, `Unit`, and atomic `EditBatch` are the integrated first stage.
The Snapshot API and independently owned graph capture are implemented. Document
construction keeps its existing functional `ParseOption` arguments.

```go
// DocumentVersion is opaque and comparable. Its zero value is invalid.
type DocumentVersion struct { /* private document identity and sequence */ }
func (v DocumentVersion) Sequence() uint64

// TextEdit uses the document's position unit and the supplied base version.
type TextEdit struct {
    Start, End int
    Text string
}

func (d *Document) Version() DocumentVersion
func (d *Document) Unit() Unit
func (d *Document) EditBatch(base DocumentVersion, edits ...TextEdit) error
func (d *Document) Snapshot(opts ...ParseOption) (*Snapshot, error)

// SnapshotCaptureError reports a result graph that cannot be frozen safely.
type SnapshotCaptureError struct {
    Path string
    ValueType reflect.Type // type metadata only; never the unsupported value
    ParseError error       // owned copy of this capture's parse error, if any
}
func (e *SnapshotCaptureError) Error() string
func (e *SnapshotCaptureError) Unwrap() error // returns ParseError

// Snapshot has private immutable state; callers retain it through ordinary GC.
type Snapshot struct { /* private state */ }
func (s *Snapshot) Version() DocumentVersion
func (s *Snapshot) Unit() Unit
func (s *Snapshot) Text() string
func (s *Snapshot) Root() SnapshotNode
func (s *Snapshot) Error() error
func (s *Snapshot) Stats() ParseStats
func (s *Snapshot) Materialize() *Node

// SnapshotNode is an opaque, comparable handle scoped to one snapshot.
// The zero handle denotes a nil Node.
type SnapshotNode struct { /* private view identity and location */ }
func (n SnapshotNode) Valid() bool
func (n SnapshotNode) Type() string
func (n SnapshotNode) Rule() string
func (n SnapshotNode) Start() int32
func (n SnapshotNode) End() int32
func (n SnapshotNode) Text() string
func (n SnapshotNode) IsTerminal() bool
func (n SnapshotNode) ChildCount() int
func (n SnapshotNode) Child(i int) SnapshotNode
func (n SnapshotNode) Field(name string) (any, bool)
func (n SnapshotNode) Fields() []SnapshotField

type SnapshotField struct {
    Name string
    Value any // SnapshotNode, int, string, bool, or nil; never *Node
}
```

A valid field whose value is a typed nil `*Node` uses a zero `SnapshotNode`;
an untyped nil field stays nil. `Field` distinguishes an absent field from a
present nil value using its boolean result. `Child` includes nil child slots;
`ChildCount` counts those slots. `Fields` preserves nil versus allocated-empty
state and returns an owned slice in original field order. Modifying the slice
or its entries changes no snapshot state. Child indices and nil child slots
match Node.Children; invalid indices panic as ordinary slice indexing does.
The zero handle returns empty metadata, zero positions/counts, and absent fields.
Its Child operation has no valid index. Handles compare equal within a snapshot
exactly when they denote the same source Node/view. No equality across snapshots
or stable external node ID is promised. A retained handle stays valid even if the
Snapshot variable is released; it may retain that entire version's immutable
storage, but must not retain the live Document or its caches.

`Start` and `End` preserve the current Node's `int32` spans, with an exclusive
end. Batch positions remain `int`, as with `Edit`; no UTF-16 conversion is
introduced. `Materialize` copies the whole graph into independently mutable
Nodes, preserving shared identities, cycles, nil slots, field order and values,
and nil versus allocated-empty collections. Each call returns a separate graph.
It is the adapter for consumers that need mutable Nodes or existing Node JSON.

### Versions and Pending Edits

Version is a text revision, not a parse counter or the internal edit-log
generation. Construction starts at sequence 1. Each successful single Edit and
each successful nonempty EditBatch advances once, even for an identity
replacement or empty insertion. An empty batch validates its base token but
changes nothing. Failed edits and parsing never advance the version. Edit-log
compaction/reset must not reset the public sequence. Sequence overflow is
rejected before editing rather than wrapping.

The opaque token includes a distinct document identity: a token from another
Document is invalid even when both have the same sequence and text. Its identity
storage must not keep the Document, text, or parse caches alive. Tokens alone
therefore do not retain old versions. Document methods remain unsafe for
concurrent use; version tokens do not implement compare-and-swap synchronization.

Snapshot parses the current version, including every edit pending since the last
parse. Construction settings provide defaults; invocation options are resolved
once with the same restrictions and order as Document.Parse in
[design 033](033-cancellable-parses-and-work-budgets.md). Context, work limits,
depth and tracing may be overridden; start rule, unit, backend and recognition
mode retain the constructed Document contract. A fresh per-call context permits
capture after cancellation of a previous task without retaining its controller.
Parsing and freezing the text, tree, and diagnostics form one controlled
invocation: they use the same context and controller, and input-work and
execution-step counters accumulate across both phases. Freezing costs therefore
count against the configured limits. It then freezes that exact parse outcome
before returning. It does not silently return the previous successful result
when current text fails. Taking another snapshot of the same version may reparse
and change statistics, but not text or version; snapshots themselves are
immutable. No promise is made to reuse an outcome solely because a version is
unchanged, especially with tracing. Materialize and snapshot getters run after
this invocation and do not accept or inherit its context or work limits.

### Atomic Batch Semantics

All edits are simultaneous replacements in the text identified by base. They
are not a sequence of coordinates adjusted after each replacement. Require
strictly increasing Start values and nonoverlap (`previous.End <= next.Start`).
Reject repeated starts, including multiple insertions at one point. Adjacent
nonempty ranges are allowed. An insertion at the end of the preceding replacement
is allowed; insertion and replacement at the same Start are rejected. This avoids
an implicit ordering policy for ambiguous edits. Callers can combine such edits
into one replacement. Do not sort or modify the caller's slice.

Validate, in order: document identity/version, ordered ranges and bounds,
byte-unit endpoints against the original decoded boundaries, and checked final
length arithmetic. Preserve existing input/position support; do not introduce a
new size limit implicitly through a snapshot representation. Error reporting
identifies the offending edit index and range. Replacement text follows Edit
semantics: bytes remain exact in Bytes mode; malformed text is normalized to
replacement code points in
CodePoints mode. Empty text deletes; zero-length ranges insert. Build the final
text by concatenating unchanged original ranges with replacements.

Validation and preparation publish no partial state. An invalid batch preserves
text, version, memo/repetition caches, input tables, stats, and prior result Nodes.
Commit publishes one new text version after preparation succeeds. It does not
parse; a later parse error does not undo an accepted text transaction. Fatal
process failures such as unrecoverable allocation exhaustion are outside the
ordinary error rollback contract.

Do not implement this by calling public Edit repeatedly. Original byte boundaries
can change after insertion completes malformed UTF-8, and an intermediate error
would violate atomicity. The first correctness implementation may prepare the
new input and conservatively discard memo/run state for a nonempty batch. This
is an explicit reuse cost, to be measured. A later transaction can prepare the
mapped edit log and invalidation state privately, then publish them together.
Edits already pending before the batch belong to the base text; batch validation
uses that text, not the text of the last parse.

### Parse Outcomes, Diagnostics, and Ownership

After safe graph capture, Snapshot and its returned error describe the same
version and are available for all normally returned parse outcomes:

| Outcome | Snapshot root | Error |
| --- | --- | --- |
| success with value | immutable value graph | nil |
| successful nil value | zero handle | nil |
| ordinary syntax failure | zero handle, as current Parse | copied SyntaxError |
| consuming recovery | exact current root, possibly nil | copied SyntaxErrors |
| action/depth/module error returned by Parse | zero handle, as current Parse | corresponding error |
| work-limit exhaustion or cancellation during Parse or freezing | no snapshot | interruption error |
| unsupported legacy field value during capture | no snapshot | SnapshotCaptureError |
| trace callback panic | no newly returned snapshot | original panic propagates |

Ordinary syntax and recovered errors do not make the snapshot pointer nil. Callers
can display the failed version's text and diagnostics; they separately retain a
previous successful snapshot if desired. Cancellation or work-limit exhaustion
in either controlled phase returns no snapshot and publishes no partial text,
tree, or diagnostic capture. If invocation execution has begun, clear reusable
parser state as for an interrupted parse; preserve current text and version for
retry. Cleanup is not charged to the exhausted work budget. Cancellation and
deadline errors retain their normal `errors.Is` identity, and a trace callback
panic propagates as the original panic after cleanup. A subsequent retry must
match fresh parsing.

Legacy NodeField.Value is exported as any, so a caller can put unsupported values
into cached Nodes before capture. Accept only untyped nil and the exact dynamic
Go types *Node, int, string, and bool. A typed nil *Node is valid. Slices, maps,
custom named values, other pointers, functions, and channels are rejected even
when their value is nil; do not silently retain, drop, stringify, or shallow-copy
them. A Go type alias of an accepted type has the same dynamic type and is accepted.

Reject the first unsupported field deterministically, using depth-first traversal
from the root, Children in index order followed by Fields in stored order, and
visiting each Node identity once. Snapshot returns nil and *SnapshotCaptureError.
Path identifies the first edge path, including child indices and field indices
and quoted names (for example, root.Children[0].Fields[1]); ValueType is
reflect.Type metadata sufficient to inspect the Go type and named package path.
The error never retains the unsupported value or its mutable backing. ParseError
is a defensive copy of any parse-outcome error from this call, and Unwrap preserves
its existing errors.As classification. No partially captured graph is exposed.
Text/version stay unchanged; normal parse effects on stats and reusable state
have already occurred and are not rolled back by a capture error.

Store diagnostic/error data independently of mutable SyntaxError pointers and
Expected/Messages slices. Error returns a defensive copy; the error returned by
Snapshot also cannot mutate stored data. Preserve existing error messages and
public errors.As classifications. Current fatal errors have immutable messages;
The work-limit diagnostics proposed in design 033 are copied by value, including
kind, limit, used and requested counters. Context cancellation and deadline
identity remain observable through errors.Is; capture does not retain a context,
controller or cancellation token. Custom context.Cause remains caller-owned and
is not stored by the snapshot. Future wrapped errors need an explicit immutable
copy or a documented immutable cause; do not replace classification with text.
Stats is a value copy of this capture's parse, not a live alias to Document.Stats.
Text and terminal strings are immutable, but substring retention can keep an
entire source allocation alive; include it in retained-version measurements.

Snapshots and their handles are safe for concurrent reads, independently of
subsequent Document edits/parses. Lazy lookup/materialization state, if used,
requires synchronized publication or invocation-local storage; reads may not
race by initializing shared caches. Materialized Nodes are caller-owned mutable
graphs. Snapshot freezing must detach from both parser-owned mutable Nodes and
legacy Nodes returned before capture. Mutation of a legacy graph after capture,
or of a materialized graph, cannot alter an existing snapshot. Freezing observes
the actual legacy parse result at capture time; it cannot retroactively erase
caller changes made before capture or promise fresh equivalence for such changes.

### Representation and Reuse Invariants

The reference path freezes the result graph iteratively with an identity map,
including child and node-field edges. Register each destination identity before
following its outgoing edges. This preserves shared descendants and cycles,
including self-cycles or multi-node cycles added by callers to cached legacy
Nodes before capture, without recursion or exponential path expansion.
Materialize uses the same iterative identity-map discipline and preserves those
cycles in its independent mutable graph. It need not change the recursive public
Clone implementation. No mutable parser scratch or Document input arrays are
retained as snapshot data.

Snapshot readers must account for cycles when traversing the graph; a unique-
identity walk can use comparable handles as its visited keys. Capturing a cycle
does not repair the mutable legacy graph. Document.Parse after edits keeps its
existing ownership and reuse behavior, with no new guarantee to validate or
repair arbitrary caller mutations; snapshot capture promises only to freeze a
graph from a Parse call that returns. Existing Node JSON/string consumers retain
their current behavior on cyclic input.

The selected sharing path freezes the mutable parse graph first, then compares
records at the same traversal index with the previous snapshot graph. It shares
a prior immutable record only when its kind, absolute start/end, text, flags,
nil-versus-empty slices, ordered child IDs, and ordered field names/values match
exactly. Hashes may not replace equality checks. Unmatched records are copied;
graphs with fewer than 128 records or fewer than 25% exact matches use the
contiguous copy path.
The Document keeps only a weak reference to the previous snapshot graph, and
snapshot text remains separately owned. This preserves absolute positions and
within-version graph identity without retaining mutable Nodes or input buffers.

Relative spans remain a possible future design.
Relative spans must cover all actual graph edges: action fields can point
outside a parent's span, and zero-width/lookahead captures can need different
placement contexts. Absolute scalar values produced by position-dependent
actions cannot be repaired by shifting spans. Any such design needs measured
access bounds and exact results without walking an unbounded edit history.

Absolute scalar values produced by position-dependent actions cannot be repaired
by shifting spans. Preserve the compiler's positional flags, input/examined-range
dependencies, left-recursion provisional-result invalidation, and repetition
eligibility rules. Recovered errors embed positions in diagnostics and cannot be
shifted past edits using the existing reuse path. Actions and predicates reading
fields/variables still determine reuse eligibility. UTF-8 joins at batch seams
must invalidate decoding dependencies, including truncated prefixes at EOF.

Legacy exported Nodes are writable without interception, so pointer/generation
keyed shadows cannot safely assume unchanged content. Exact record sharing
freezes and compares content before reuse; hashes alone cannot establish
equality. It reduces retained duplication while still requiring whole-graph
capture work. Cheap capture from immutable parser storage may instead need a
separate exclusively read-only Document surface; that is a later public contract
decision, not an implicit change to Document.Parse sharing.

The adopted child-run representation shares unchanged 256-ID runs of a changed
record with at least 1,024 children when at least three quarters of the runs
match. The sidecar descriptor is allocated only for such records. Other records
stay flat, so there is no balancing tree or unbounded traversal history.
List order, nil slots, capture sharing, and constant-time indexing are
preserved; materialization still builds a full independent public Children
slice. A copied Fields slice is not part of this child-run mechanism. Shared
runs may retain an older flat child array, but not a live Document, memo table,
full edit log, parser scratch, or discarded provisional graph.
Opaque handles cache their record and optional run descriptor to avoid repeated
sidecar lookups; this grows a handle from 16 to 24 bytes on arm64. The snapshot
graph stores records rather than one handle per node. Materialization expands
runs directly, and the measured access/materialization tradeoffs are recorded
in [optimization 099](../optimizations/099-share-document-snapshot-child-runs.md).

### Compatibility and Implementation Sequence

Core Closure, Bytecode, and BytecodeIterative Documents are the initial scope,
including AST-omitted compiled grammars on available bytecode backends. Generated
Go/typed Go and TypeScript retain their current lack of Document support. Stream
and recognition-only rejection remains unchanged. Public Node JSON and saved
module formats remain unchanged; snapshot handles introduce no serialized format.

1. Add an opaque, comparable, non-retaining `DocumentVersion` with a document
   identity and sequence, plus `Document.Version`, `Document.Unit`, `TextEdit`,
   and atomic `Document.EditBatch`. Start at sequence 1. Each successful legacy
   `Edit` advances once, including a no-op; each successful nonempty batch also
   advances once, while a valid empty batch does not. Reject overflow before
   mutation. Interpret all batch ranges in the supplied base text; reject foreign
   or stale versions, repeated or unordered starts, overlapping/out-of-bounds
   ranges, invalid byte boundaries, and final-length overflow. Validate and
   prepare privately, then publish the new text and clear reusable parse state
   without parsing. Any rejected batch preserves observable Document state and
   previously returned Nodes. Verify fresh-parse equivalence on supported core
   backends and both position units.
2. Implement independently owned read-only snapshots, diagnostic copying,
   concurrent reads, unsupported-value rejection, cycle preservation, and
   explicit materialization. This stage is implemented and measured against
   Parse plus Clone while holding text/error/version separately.
3. Use exact immutable snapshot-record sharing, preserving the copy path for
   small or low-reuse graphs. Keep input representation constant in comparisons.
4. Share eligible immutable child-ID runs within changed high-fanout records.
   Keep a same-source flat reference and measure retained history, capture,
   access, and materialization separately.
5. Treat relative spans and a separate read-only Document API as distinct
   designs; do not infer their value from exact-record or child-run results.

The versioned edit API is the first independently useful stage. Owned snapshots,
exact immutable record sharing, and eligible child-run sharing are selected for
adoption; relative spans remain proposed. Chunked input storage is a separate
mechanism and must be measured independently when combined with snapshots.

## Alternatives Considered

- **Continue Parse plus Node.Clone.** Stable, compatible baseline; copies unique
  graph nodes and collections and keeps immutable scalar/string values. Consumers
  still assemble their own version/text/error bundle and batch rollback. Keep it
  as the correctness and resource reference.
- **Return a cloned mutable Node as the snapshot root.** Small API surface and
  existing consumers work directly. Concurrent immutability depends on caller
  discipline, and shared snapshots cannot safely expose writable fields/slices.
  Prefer a read-only facade with explicit Materialize for mutable consumers.
- **Share current Node pointers behind getters.** Avoids an initial copy but later
  Document parses and external writes still change them. Getters alone do not
  create immutability; reject this ownership model.
- **Replace all Nodes with immutable relative storage immediately.** Could support
  cheap snapshots naturally, but changes action access, legacy sharing, exported
  fields, and materialization costs at once. Defer a separate read-only Document
  mode unless compatibility-preserving sharing proves insufficient.
- **Deep-copy Document and replay edits for transaction rollback.** Easy reference
  model but copies input, memo, and results that do not belong to a snapshot.
  Prevalidate and prepare an unpublished commit instead.
- **Sequential batch coordinates or automatic sorting.** Familiar for some editor
  protocols, but mixed ranges and repeated insertions need more mapping policy.
  Prefer explicit pre-batch coordinates and ordered, nonoverlapping replacements;
  adapters can translate editor-protocol changes before calling the API.

## Testing

These are proposed gates, not completed checks:

- Version identity, zero/foreign/stale tokens, no-op edits, empty batches, pending
  edits, sequence overflow, edit-log reset, unchanged parses, and successful/failing
  captures. Verify invalid edits alter no observable state, including stats and
  previously returned Nodes.
- Independently assemble batch text from original ranges. Differential replay of
  multiple batches/undo sequences against fresh parsing on every backend/unit;
  abutting replacements, insertion boundaries, unsorted/overlapping/repeated starts,
  final-length arithmetic overflow, and empty input.
- UTF-8 scalar and malformed-byte boundaries, astral/combining text, invalid bytes,
  byte-prefix completion across neighboring replacements and at EOF, normalized
  code-point editing, newline anchors, and line/column diagnostics.
- Position-dependent action scalars, field/text predicates, action errors, cuts,
  indirect LR/provisional seeds, Operators Rule, recovered errors, nil results and scalars
  inside fields, deep lists, DAGs, and repetition resumption after multiple edits.
- Freeze/materialize versus actual Parse plus Clone: exact spans, text, field order,
  nil/empty collections, error classifications and DAG identities. Deep controls
  must avoid recursive snapshot conversion and exponential graph traversal.
- Before capture, mutate cached legacy fields to slices/maps/custom mutable
  values, typed nil unsupported values, and custom named scalars. Assert nil
  snapshot, SnapshotCaptureError path/type information, deterministic first-field
  selection through shared/cyclic graphs, copied parse error classification, and
  no retained unsupported backing or partial snapshot. Valid scalar/type-alias
  and typed nil *Node controls still capture correctly.
- Before capture, add self-cycles and multi-node cycles through Children and
  node-valued Fields. Verify snapshot/materialization terminate, preserve cycle
  and shared identities, and detach from later legacy/materialized mutations.
  Use identity-aware traversal; do not require ordinary JSON serialization of
  cycles or repair by a subsequent edited legacy Parse.
- Retain old snapshots across edits, failed/recovered/interrupted parses and trace
  panics. Mutate old legacy Nodes, defensive diagnostic/field copies, and separate
  materialized results after capture; snapshots stay unchanged.
- Race tests for concurrent read/traversal/error copies/materialization of one and
  multiple snapshots while a sole owner reparses the Document. Check release of
  old versions, scratch/memo detachment, and large-to-small edits with forced GC.

## Performance and Results

Snapshot capture and retained-version costs have been measured for the
independent immutable graph and the exact-record-sharing implementation. Results
below are scoped to the tested workloads; traversal and broader workloads remain
unmeasured, so no general performance benefit is claimed.
The stage-1 implementation of version tokens and atomic edit batches has
preliminary measurements against the
source-equivalent baseline
[`69af68e`](https://github.com/ornew/pego/commit/69af68ecdec1dea942db89158c36a21ad18ae505).
Those measurements cover edit and batch behavior only.

Existing Edit and Document-creation measurements used three alternating
500 ms pairs. Three 500 ms complete-transaction samples compared batch edits
with sequential edits. The runs used Go 1.27.1 on darwin/arm64 (Apple M3 Max),
with `GOMAXPROCS=16`, `GOGC=100`, and `GOMEMLIMIT=off`. Existing single-edit
allocation counts were unchanged; timing was mostly noisy, with a 2.7% increase
for a 1 MiB CodePoints edit. Document creation added 33 B and one allocation. Bytes-mode creation was 12.7–16.5%
slower in the tested cases, while CodePoints creation was noisy. For three
equal-length sparse edits followed by Parse, the conservative batch path took
2.6–3.5× the time of sequential Edit-plus-Parse on 4 KiB and 64 KiB `abc`/newline
inputs across Closure and iterative Bytecode in both units. It also used 66–333
allocations for the batch cases versus 57–86 for sequential edits; these counts
are specific to the measured cases. This cost reflects the stage-1 path's full
cache reset and fresh parse after a successful batch. It is a stage-1 result,
not a snapshot or sharing result and not a general performance claim.

Reproduce the stage-1 transaction and creation cases with
`go test ./internal/engine -run '^$' -bench '^BenchmarkDocument(EditTransaction|OwnerCreation)$' -benchmem -benchtime=500ms -count=3`.
The existing-edit control uses `BenchmarkDocumentInputEdit` at 1 KiB, 4 KiB and
1 MiB, middle replacements and both units. Inject the identical creation-only
benchmark into the baseline when comparing revisions; compilation and parsing
are excluded from that cold-owner measurement. The transaction cases include
validation, commit and reparse after a warmed initial parse. Routine raw results
remain outside the repository.

For stage 2, `BenchmarkDocumentSnapshotCapture` compares capture with a
`Document.Parse` plus `Node.Clone` bundle containing text, version, diagnostics,
unit, and statistics. It uses a warmed document, 4 KiB and 64 KiB inputs,
Closure/Bytecode/iterative Bytecode backends, and both input units. Three
sequential 300 ms runs per case on the same Go 1.27.1/M3 Max configuration gave
Snapshot-to-bundle median time ratios of 1.33–1.45 and allocation-byte ratios
of 2.47 (4 KiB) and 3.44 (64 KiB). Snapshot used only 1–2% as many allocation
events. `Materialize` alone took roughly 0.13 ms (4 KiB) and 1.7 ms (64 KiB)
in these cases. `B/op` includes transient copies and is not retained heap.

A separate forced-GC retention harness, `go run ./bench/snapshot-retained`,
warms and parses the Document, forces GC, and records baseline `HeapAlloc`.
After capturing versions it forces GC again and reports the `HeapAlloc` delta
over that baseline, divided by the version count. With `-retain=true`, it keeps
64 versions of a 4 KiB input or 16 versions of a 64 KiB input alive through the
measurement. These are additional post-GC bytes per version, not total live
heap. Across three processes per mode, Snapshot used 1.057× and 1.133× as many
additional bytes per version, respectively, as the Parse-plus-Clone bundle.
Five alternating baseline/candidate pairs of the existing cached `Document.Parse` benchmark
showed unchanged allocation counts and no consistent timing regression; the
per-pair timings were noisy. These measurements describe the independent-copy
path, not the benefit of exact record sharing.

Exact record sharing was compared with the copying path after a same-length
one-character edit on Go 1.27.1, Apple M3 Max, with `GOMAXPROCS=16` and
`GOGC=100`. In three alternating retention-process pairs, 64 retained 4 KiB
versions used 455.2–455.6 KB per version in copy mode and 84.6–84.7 KB with
sharing; for 16 retained 64 KiB versions, the figures were 7.792–7.794 MB and
1.540–1.541 MB per version. Three alternating 300 ms Closure/CodePoints capture
pairs showed about 12% higher median time for 4 KiB inputs (paired results
10.9–14.8% higher) and about 6% for 64 KiB inputs (3.7–14.4% higher), with
about 2% higher `B/op`; `Materialize` was broadly similar. These capture
measurements cover only that backend, unit, and edit shape.

The low-reuse fallback was checked with a length-changing prefix insertion.
For 16 retained 64 KiB versions, both copy and sharing modes measured an
additional 8.386–8.388 MB per version over the warmed-Document baseline. Three
alternating 300 ms Snapshot-plus-prefix-edit pairs on 4 KiB and 64 KiB inputs
showed 0–3% timing differences, within the
observed noise, and similar `B/op`. With `-retain=false`, each captured
snapshot is discarded immediately; the post-GC delta over baseline was only
tens of KB in both modes. Five alternating 300 ms pairs of ordinary Document
creation/Parse with sharing disabled versus main had equal
allocation counts and `B/op`, with no consistent timing difference. Other
workloads and backends remain unmeasured.

The selected mechanism, copy fallback, and focused measurements are summarized
in [optimization 097](../optimizations/097-exact-document-snapshot-record-sharing.md).
Additional sharing of child-ID runs inside changed records is measured in
[optimization 099](../optimizations/099-share-document-snapshot-child-runs.md).

Reproduce capture and materialization with
`go test ./internal/engine -run '^$' -bench '^BenchmarkDocumentSnapshotCapture$' -benchmem -benchtime=300ms -count=3`.
Use the retention harness with matching `-mode`, `-size`, `-versions`,
`-backend`, and `-unit` flags in separate processes. Keep timed runs serialized.

Pin baseline/candidate revisions, toolchains, backend/unit, grammar/input hashes,
retained-version count, and control flags. Alternate paired processes; keep
routine raw logs outside the repository.

Measure separately: initial parse; single Edit and EditBatch preparation/commit;
reparse with evaluations/reuse; Parse plus Clone reference; snapshot capture;
read-only indexed/full traversal; public Node materialization; and downstream
serialization. Report ns/op, B/op, allocs/op, source/input size, peak live heap,
post-GC live heap with 0/1/10/100 retained versions, and heap after releasing all
old versions. Include full text allocations and substring retention. Do not count
only the shared payload while excluding version directories, child chunks,
handles, cache maps, diagnostics, or simultaneous materialized Nodes.

Use small, medium, and multi-megabyte ordinary trees plus action-built shared DAGs,
long child sequences, capture-heavy fields, position actions, recovery, and
malformed UTF-8. Edit beginning/middle/end, replace a large document with a small
one, apply sparse and adjacent batches, and compare no-edit captures. Keep ordinary
Parser.Parse/Recognize and legacy Document.Parse controls to detect abstraction
costs. Control DAG traversal by unique identities; naive JSON path expansion is
not a snapshot-allocation measurement.

Test copy facade, exact-verified sharing, and any separately scoped immutable
parser representation on the same revision. Retain contiguous input during these
comparisons so input-storage changes do not masquerade as snapshot gains. Measure
materialization both alone and in the realistic edit-to-consumer pipeline.
Record adopted optimization methods/results in the optimization catalog with
code; update this record with actual outcomes and remaining gates.

## Limitations and Open Questions

The implemented contract combines versioned batches with owned read-only
snapshots, including ordinary failed parse outcomes. It preserves legacy APIs,
and batch commit currently discards reuse state. Exact snapshot-record sharing
and eligible child-run sharing reduce retained memory in measured same-length
edit workloads. Exact sharing raised capture time in the tested
Closure/CodePoints cases; child-run sharing had no clear ordinary-path timing
change in the measured Closure/Bytes cases. Other backends, units, graph shapes
and edit patterns remain unmeasured.

Further measurements can establish whether a separate read-only Document mode
is worth its additional API. That tradeoff does not justify silently changing
mutable Node behavior.
