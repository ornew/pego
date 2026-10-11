# 032. Versioned Document Snapshots and Atomic Edit Batches

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Add explicit text versions, atomic edit batches, and immutable parse snapshots to
`Document`. A snapshot captures the current text and its parse outcome through a
read-only tree surface. Existing `Document.Edit`, `Document.Parse`, mutable
`Node` fields, and `Node.Clone` retain their ownership contracts.

Start with an independently owned snapshot graph as the reference implementation.
Investigate immutable structural sharing, relative internal spans, and chunked
child sequences against that reference. Sharing is an implementation candidate,
not a promised reduction in snapshot time or retained memory. Chunked input
storage remains a separate experiment under [design 019](019-incremental-document-and-tree-tooling.md).

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

No new clone, edit, materialization, or live-heap measurements accompany this
proposal. Older incremental guide measurements are not a baseline for this
revision.

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

## Non-goals

This record does not change PEG choices, actions, recovery, `Node` serialization,
`Node.Clone`, or the mutable Document contract. It does not add generated Go/TS
Documents, typed snapshot conversion, streams, automatic concurrent Document
editing, undo/redo, exact change notifications, tree queries, or cross-document
persistent caches. Input ropes/piece tables and caller-owned storage belong to
the separate input-storage experiment. Wider position representations are not
part of this feature.

## Design

### Proposed public API

All signatures below are proposals. Document construction keeps its existing
functional `ParseOption` arguments; no additional options entry point is needed.

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

A valid field whose node value is nil uses a zero SnapshotNode; an untyped nil
field stays nil. `Field` distinguishes absent fields from present nil values.
`Fields` returns an owned slice in the original field order. Modifying that slice
or its entries changes no snapshot state. Child indices and nil child slots
match Node.Children; invalid indices panic as ordinary slice indexing does.
The zero handle returns empty metadata, zero positions/counts, and absent fields.
Its Child operation has no valid index. Handles compare equal within a snapshot
exactly when they denote the same source Node/view. No equality across snapshots
or stable external node ID is promised. A retained handle stays valid even if the
Snapshot variable is released; it may retain that entire version's immutable
storage, but must not retain the live Document or its caches.

Start/End preserve the current Node's int32 spans, with an exclusive End. Batch
positions remain int, as with Edit. No UTF-16 positions or implicit unit conversion
are introduced. Materialize copies the entire root graph into independently
mutable Nodes, preserving shared descendants, cycles, nil slots, field values/order,
and nil versus allocated-empty collections. Each call makes a separate graph.
It is the explicit adapter for existing Node/JSON consumers and must be measured.

### Versions and pending edits

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
once with the same restrictions and order as proposed Document.Parse in
[design 033](033-cancellable-parses-and-work-budgets.md). Context, work limits,
depth and tracing may be overridden; start rule, unit, backend and recognition
mode retain the constructed Document contract. A fresh per-call context permits
capture after cancellation of a previous task without retaining its controller.
It then freezes that exact parse outcome before returning. It does not silently
return the previous successful result when current text fails. Taking another
snapshot of the same version may reparse and change statistics, but not text or
version; snapshots themselves are immutable. No promise is made to reuse an
outcome solely because a version is unchanged, especially with tracing.

### Atomic batch semantics

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

### Parse outcomes, diagnostics, and ownership

After safe graph capture, Snapshot and its returned error describe the same
version and are available for all normally returned parse outcomes:

| Outcome | Snapshot root | Error |
| --- | --- | --- |
| success with value | immutable value graph | nil |
| successful nil value | zero handle | nil |
| ordinary syntax failure | zero handle, as current Parse | copied SyntaxError |
| consuming recovery | exact current root, possibly nil | copied SyntaxErrors |
| action/depth/module error returned by Parse | zero handle, as current Parse | corresponding error |
| unsupported legacy field value during capture | no snapshot | SnapshotCaptureError |
| trace callback panic | no newly returned snapshot | original panic propagates |

Parse-outcome errors do not make the snapshot pointer nil. Callers can display
the failed version's text and diagnostics; they separately retain a previous
successful snapshot if desired. Interrupted parses retain current text/version and clear
reusable state as today. A subsequent retry must match fresh parsing. Do not
publish a partially frozen graph on a propagated panic.

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

### Representation and reuse invariants

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

A sharing experiment may keep immutable payloads, relative spans, versioned
references, and persistent chunked child sequences. Version-specific handles
resolve absolute spans through their location context. Relative spans must cover
all actual graph edges: action fields can point outside a parent's span, and
zero-width/lookahead captures can need different placement contexts. Preserve
observed within-version identity and absolute values rather than assuming every
child is a containment-tree edge. Do not cache absolute positions in a payload
shared between versions. Span resolution must have measured access bounds, not
an unbounded walk through the entire edit history.

Absolute scalar values produced by position-dependent actions cannot be repaired
by shifting spans. Preserve the compiler's positional flags, input/examined-range
dependencies, left-recursion provisional-result invalidation, and repetition
eligibility rules. Recovered errors embed positions in diagnostics and cannot be
shifted past edits using the existing reuse path. Actions and predicates reading
fields/variables still determine reuse eligibility. UTF-8 joins at batch seams
must invalidate decoding dependencies, including truncated prefixes at EOF.

Legacy exported Nodes are writable without interception. Consequently, a
persistent shadow keyed only by their pointers/generations cannot safely assume
unchanged content. A compatibility-preserving sharing prototype must validate
content when freezing, or share independently owned frozen payloads only after
exact equality verification. Hashes alone cannot establish equality. Such a
prototype can reduce retained duplication while still requiring whole-graph
capture work. Cheap capture from immutable parser storage may instead need a
separate exclusively read-only Document surface; that is a later public contract
decision, not an implicit change to Document.Parse sharing.

Chunked child sequences may share unchanged prefixes/suffixes after repetition
resumption. Preserve list order, nil slots, capture sharing, and index behavior;
a copied Fields slice is not a persistent child sequence. Measure balancing,
index/traversal costs, and materialization of a full public Children slice. Avoid
retaining discarded memo/provisional nodes merely because a sequence once
referenced them. Snapshots retain only their immutable graph/text/diagnostics,
not a live Document, memo table, full edit log, or parser scratch.

### Compatibility and implementation sequence

Core Closure, Bytecode, and BytecodeIterative Documents are the initial scope,
including AST-omitted compiled grammars on available bytecode backends. Generated
Go/typed Go and TypeScript retain their current lack of Document support. Stream
and recognition-only rejection remains unchanged. Public Node JSON and saved
module formats remain unchanged; snapshot handles introduce no serialized format.

1. Add version tokens and atomic batches, with a conservative fresh-state reference
   after accepted batches. Establish validation rollback and fresh-parse equivalence.
2. Add independently owned read-only snapshots, diagnostic copying, concurrent
   reads, unsupported-value rejection, cycle preservation, and explicit
   materialization. Compare against Parse plus Clone while
   holding text/error/version separately. Update incremental/runtime guides and
   development/API status when implemented.
3. Prototype immutable sharing, relative spans, and persistent child sequences
   with generation/build-time reference controls or separate binaries. Preserve
   the reference outcomes and ownership. Keep input representation constant.
4. Adopt sharing only after paired edit/reparse/capture/materialization and live-
   heap measurements establish its benefits and costs. If efficient storage needs
   a separate read-only Document API, review that contract before implementation.

No stage is implemented by this record. Stages 1–2 can provide useful contracts
without claiming cheap snapshots. Chunked input-storage work may proceed independently
and later be combined under a separately measured control.

## Alternatives considered

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
  indirect LR/provisional seeds, Pratt, recovered errors, nil results and scalars
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

## Performance and results

No current measurements or speed claims. Pin baseline/candidate revisions,
toolchains, backend/unit, grammar/input hashes, retained-version count, and control
flags. Alternate paired processes; keep routine raw logs outside the repository.

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

## Limitations and open questions

The recommended initial contract is pre-version batches plus owned read-only
snapshots, including failed parse outcomes. It preserves legacy APIs but incurs
capture/materialization copies and may discard batch reuse initially. None of
those costs has been quantified at the inspected revision.

Later, measured sharing results must establish whether a separate read-only
Document mode is worth its additional API. That later tradeoff does not block
the proposed owned facade or justify silently changing mutable Node behavior.
