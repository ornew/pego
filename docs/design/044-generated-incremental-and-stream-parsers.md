# 044. Standalone Generated Document and Stream Parsers

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Propose opt-in generation of mutable incremental Documents and committed stream parsing in standalone Go and
TypeScript parsers. Preserve the engine's examined-input dependency rules, PEG decisions, diagnostics and result
ownership. The two features have different execution needs and can land independently; generated batch parsing
remains available and unchanged by default. APIs below are proposed, not accepted exports.

## Existing contracts and goals

[Design 007](007-streaming-and-incremental-parsing.md), [Document](../../internal/engine/document.go),
[resume.go](../../internal/engine/resume.go), and [stream attributes](../../spec/attributes.md#stream) define the
engine reference. Generated Go explicitly excludes these features in [gen.go](../../internal/engine/gen.go);
generated TS uses synchronous recursive expression functions ([design 013](013-typescript-generation.md)).
Retaining that runtime's input and memo across calls alone does not implement edit invalidation or suspension.

Documents must match a fresh parse after every legal edit, including diagnostics and action values. Streamed
elements must match the engine's committed sequence and remain valid after subsequent input is discarded.
Generated packages stay independent of the compiler and engine at runtime. This design does not add a compiled
TS VM, immutable snapshots, a new text-storage algorithm, or recognition on Documents/streams.

## Proposed APIs

Propose generation options `pego.WithDocument()` and `pego.WithStream()`, with CLI `-document` and `-stream`.
Generate only the requested machinery. Existing generation without these options retains the batch source path;
`#stream` still behaves as an ordinary repetition in a batch parse. Requesting streams requires a legal #stream
repetition in the configured start rule; reject the generation request otherwise. Documents select that same
start rule initially; generic named batch parsing remains separate.

### Go Document

```go
func NewDocument(text string, opts ...ParseOption) (*Document, error)
func (d *Document) Text() string
func (d *Document) Edit(start, end int, text string) error
func (d *Document) Parse(opts ...ParseOption) (*Node, error)
func (d *Document) Stats() ParseStats
// WithTypes only: StartValue is the existing generated configured-start type.
func (d *Document) ParseAST(opts ...ParseOption) (StartValue, error)
```

Constructor options resolve and validate once, without retaining the option functions. The unit and effective
depth become Document-local settings; Edit and positional actions use that unit. No backend selector is needed
for a standalone generated runtime. New APIs use functional options. Constructor settings provide defaults for
each parse; invocation overrides follow [design 033](033-cancellable-parses-and-work-budgets.md),
including ordered once-only resolution, context/limits/depth/trace overrides and rejection of incompatible
unit changes. A fresh per-call context supports retries; memo state never retains an invocation controller.

Edit replaces [start,end), rejecting invalid range or byte character boundaries before modifying text or memo.
Invalid UTF-8 bytes are individual characters, as in the engine; an edit can join bytes into a valid sequence.
Document is not safe for concurrent use. Node results share reusable objects: later Parse may move old nodes,
including objects shared through fields and children. Earlier trees cannot be read concurrently with Parse.
Generate `Node.Clone` when generating Documents, preserving internal shared identities and detaching the tree
from future edits. It is not an immutable snapshot API and does not make arbitrary mutable any values immutable.
Text and diagnostic strings already handed out remain owned data, independent of discarded parser scratch.

Initially implement Document.ParseAST by Node parsing followed by one conversion of the final result. The typed
result belongs to that invocation and is detached from the Document's mutable internal Node graph for converted
objects/arrays. CST Node members still reference the same Document graph: their documented mutation/concurrency
contract is identical to Parse, and Clone or an explicit consumer copy is required to retain them. Preserve all
aliases that the existing converter preserves; never return storage later cleared for parser pooling. Pure typed
direct incremental parsing is deferred until ownership/invalidation can support moving public typed spans and
position-valued fields safely. Do not promise that WithTypes makes every value an immutable snapshot.

### Go reader streams and typed elements

```go
func ParseStream(r io.Reader, emit func(*Node) error, opts ...ParseOption) error
// WithTypes only; StreamValue denotes the inferred type of one repeated element.
func ParseStreamAST(r io.Reader, emit func(StreamValue) error, opts ...ParseOption) error
```

These APIs do not return a start-rule tree. The callback receives each committed element, including nil where
the repeated expression produces no value. StreamValue follows the existing Go typed mapping of that expression,
including unions, aliases, lists, CST/any and recovery's nil/Error representations; it need not equal StartValue.
The generator must infer/export the element contract explicitly rather than reuse ParseAST's start signature.
Conversion occurs after the element is finalized and before callback publication. Convert per element with owned
storage; Node/CST fields and text survive buffer compaction and later elements. Cross-element aliases, if allowed
by grammar values, retain their identity and cannot refer to reusable scratch. A conservative detached copy is a
valid first implementation; retain ownership costs in measurements.

Reader streams may block in Read; the parser does not make a generic Reader cancellable and does not close it.
Complete characters must not trigger unrelated reads; incomplete UTF-8 prefixes need more bytes or actual EOF.
Preserve (n>0,error) handling and non-EOF reader errors. Resolve options before reading, copy invocation state,
and return callback errors unchanged immediately, with no later emit/read/action. Already committed elements
remain final even if a later parse, reader or action error occurs. A nil emitter follows the engine's collection
behavior internally without public element callbacks, rather than panicking. This is not an asynchronous API.

### TypeScript Document and push streams

Propose `createDocument(input: string | Uint8Array, options?: ParseOptions)` returning a discriminated
`{document,error}` result, with `document` null on configuration failure. Copy byte input; resolve option getters
once. Methods `text()`, `edit(start,end,replacement)`, `parse(options?)`, `stats()` and, with types, `parseAST(options?)` follow the
Go contracts and existing TS result/error conventions. Edit returns an Error or null, without partial mutation.
In Bytes, propose `bytes(): Uint8Array` returning a copy for lossless invalid input; text() uses the existing
surrogateescape representation. Replacements accept the same string/byte types. Preserve both units, number|bigint
scalar mapping and existing string/lone-surrogate normalization. TS `Node.clone()` provides the analogous copy.

A portable push stream needs generated continuations, not a blocking Reader translated to JS. Propose a separate
`createStream(options?)` construction result and synchronous `stream.feed(chunk: Uint8Array, final?: boolean)`:

```ts
interface StreamResult {
  readonly elements: readonly (Node | null)[];
  readonly state: "waiting" | "done" | "failed";
  readonly error: Error | null;
}
```

WithTypes, `createASTStream` has the same protocol with the inferred typed element union. `waiting` means suspended
for required bytes; it is not a failed literal or EOF. `final:true` supplies actual EOF and produces done/failed.
Error is null while waiting; final recovered SyntaxErrors accompany done, and fatal/syntax errors accompany failed.
Previously delivered elements are never repeated or retracted. A failed feed may still return newly committed
elements preceding its error. Feed after done/failed returns an invalid-state error, not another delivery.
An empty nonfinal chunk leaves the parser waiting without repeating actions. Copy incoming bytes before returning
so caller mutation cannot alter suspended input. No string chunks initially: independently encoding halves of a
UTF-16 surrogate pair changes input. An optional whole-string adapter can encode once outside the stream.

`stream.abort(reason: Error)` terminally stops and releases suspended state without further execution; feed cannot
resume it. Reject reentrant feed/abort while executing. The array holds all emissions for that feed, so a caller
feeding a huge chunk can itself request a huge result; input discard does not bound caller-owned results. Async
Iterable, browser/Node readers, network blocking and async backpressure adapters are separate explicit utilities;
this proposal makes no implicit promise about closing or interrupting their underlying sources.

## Runtime invariants

For Documents, generated analysis tables must carry examined-range dependencies, transitive position/variable
reads, recovery dependence, LR provisional dependence and resumption eligibility. Matching includes lookahead and
previous-character anchors. Memo keys preserve variable environment and Pratt level. Edits shift only safe ranges;
position-dependent values and recovered diagnostics cannot simply have their root span shifted. Empty nodes at
insertion boundaries follow the engine's copy rule. Shared nodes move once per edit generation, across fields and
children. Completed results dependent on a growing LR seed are invalid after edits; abort/cancel drops all partial
memo/resumption state before retry, while a completed syntax failure may keep valid cache entries.

Begin with rule memo reuse; repetition resumption is a later equivalent optimization. When added, retain the exact
restrictions for predicates/variable reads, enclosing capture writes, recovered errors, provisional LR seeds and
positional values on length-changing edits. Every empty successful iteration counts once and stops. Preserve
repetition finite maxima/minima, cut scope, Pratt candidate-local diagnostics, and lookahead expectations.

For streams, discard only after the legal top-level #stream repetition commits an element. Preserve suffix/prefix
action semantics around the repetition: preceding values needed by later actions must own their text and backing
storage; a start action can still run at final completion. Forbid recovery wrapping the committing repetition in
either attribute order. Keep the previous character and absolute offsets/line state. A zero-width iteration is
emitted once and stops before checking the minimum, as the engine does; delivery cannot be undone by that failure.

The TS lowering requires explicit frames for rule calls, LR growth, Pratt candidates, choices, repeats, captures,
recovery and actions. Suspension at an input read saves the exact continuation and performs no speculative EOF
failure, action rerun or error replay. Distinguish awaiting bytes from completed parse failure at every caller.
Feed suspends at demand reads and returns only when waiting or terminal; a work-limit interruption is a terminal
error, not an implicit scheduling yield. A future cooperative scheduling API needs an explicit additional state.
Interrupted callbacks/read adapters and cancellation release pointer-bearing state without clearing published
values; no pooled parser or conversion arena is shared between concurrent invocations.

## Backend, format and implementation gates

| Scope | Initial reference and proposed rollout |
| --- | --- |
| Closure and both engine VMs | Existing Document/Reader stream oracle; both position units |
| Generated Go Node | Document memo invalidation, then demand-driven Reader stream runtime |
| Generated typed Go | Node Document conversion and per-element stream conversion first; direct path later |
| Generated TS Node/typed conversion | Document first; streams only after continuation lowering passes parity |
| Recognition | Continue rejecting Document/stream mode; batch recognition is unaffected |

This source-generation feature needs no .pegoc or ISA change by itself. If continuation tables are instead stored
as bytecode, allocate/version new operands and validate them; do not silently change existing NEXT semantics.
Keep generated name collision resolution stable and compile external consumers for new exports. Requests for an
unimplemented generation/backend combination fail explicitly, rather than silently batch-parse all input.
Reserve the new exported type/value names in deterministic order and document resulting underscore migrations
for colliding grammar declarations; generating helper methods must not alter existing handwritten types silently.

Port input/edit abstractions and ownership before memo reuse, then generated Go Document, typed conversion, Reader
streams, TS Document, and finally TS push continuations. Snapshot/versioned-tree work is a separate dependency only
if immutable views are exposed; do not block the mutable Document compatibility path on it. Chunked input storage
is a separate measured experiment: start with the current contiguous edit contract. Cancellation/work budgets
need matching cleanup points when introduced, not an ad hoc hidden API in this feature.

## Alternatives, validation and performance

The baseline is batch reparsing on every edit, and engine-backed Document/ParseStream when a dependency is acceptable.
A generated wrapper importing the engine offers fast implementation but gives up standalone deployment; retain it
as a benchmark/control, not the proposed product. Reparse buffered input on every feed is a useful test oracle,
but cannot safely reexecute actions or redeliver elements and does not provide bounded streaming. Using a worker
or async wrapper around synchronous TS does not make its execution suspend at demand reads. Sharing a bytecode VM
could reduce generator complexity but adds a separate portable runtime/distribution choice.

Proposed correctness gates compare fresh parses after random edits and all reader chunk partitions against all
three engine backends. Cover both units, invalid UTF-8 repairs, surrogate strings, zero-width repeats, recovery,
EOF versus temporary starvation, finite maxima, cut/Pratt ties, LR abort/retry, variables and start/end positional
actions. Exercise nil/action-discarded elements, typed concrete/union/list/any/CST conversions, retained text and
aliases, callback stops/panics, unknown reader errors, concurrent independent instances and terminal stream calls.
Run strict TS consumer/declaration tests and standalone Go consumers, including collisions and source determinism.

No throughput or bounded-total-memory claim is made. Measure exact parent and generation-disabled controls,
batch versus Document fresh/edit/reparse, Reader/push first-delivery latency and steady state, and typed conversion
separately. Include local/distant edits, large repetitions, LR/Pratt, growing streams and retained emissions.
Report allocations, live/peak memory, input/memo capacity, generated source/build sizes and public Node/typed
materialization cost; retained caller results and per-feed arrays are separate from runtime working memory.
Pin revisions, inputs and toolchains, serialize paired measurements, and gate ordinary batch regressions even
when only dormant parser fields change. Implementation stages remain Proposed until their gates pass.
