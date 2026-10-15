# 044. Standalone Generated Document and Stream Parsers

- **Status**: Partially Implemented
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

This design covers incremental Documents and committed stream parsing in standalone Go and TypeScript parsers.
The opt-in generated Go `Document` supports Node parsing and, with typed generation, a `ParseAST` conversion of
each completed Node result. Ordinary batch parsing remains available and unchanged by default. Direct typed
incremental matching, repetition resumption, Reader streams and TypeScript Documents/streams are not implemented.

## Existing Contracts and Goals

[Design 007](007-streaming-and-incremental-parsing.md), [Document](../../internal/engine/document.go),
[resume.go](../../internal/engine/resume.go), and [stream pragma](../../spec/pragmas.md#stream) define the
engine reference. The current generated Go path supports a Node `Document` and an optional typed `ParseAST`
conversion; it does not generate streams. Generated TS still uses synchronous recursive expression functions
([design 013](013-typescript-generation.md)) and has no Document or stream API. Retaining a runtime's input and
memo across calls alone does not implement edit invalidation or suspension.

Documents must match fresh-parse semantic results after every legal edit, including diagnostics and action values,
when parsing completes within the configured resources. The depth-limit outcome is an explicit exception: under the
physical nesting contract in [Parsing](../../spec/parsing.md#memoization), memo-answered calls do not nest, so a
near-limit parse can succeed or fail depending on memoization. Document equivalence does not add logical-depth
accounting or require cache flushing to force equal near-limit resource errors. Streamed elements must match the
engine's committed sequence and remain valid after subsequent input is discarded.
Generated packages stay independent of the compiler and engine at runtime. This design does not add a compiled
TS VM, immutable snapshots, a new text-storage algorithm, or recognition on Documents/streams.

## Current and Proposed APIs

The current Go Document is opt-in through `pego.WithDocument()` or the `-document` CLI option. It selects the
configured start rule and can be combined with typed Go generation (`-types`), but not TypeScript generation.
With types enabled, `Document.ParseAST` converts the completed Node result for each invocation; it is not direct
typed incremental matching. Existing generation without `WithDocument`/`-document` retains the batch source path.
A generated `Document` is a mutable parse view, not an immutable snapshot.

The remaining generation options and APIs in this section are proposals. `pego.WithStream()` and `-stream` would
generate stream machinery only when the configured start rule has a legal `#stream` repetition; in batch mode,
`#stream` continues to behave as an ordinary repetition. Documents select the configured start rule initially;
generic named batch parsing remains separate.

### Current Generated Go Node Document

```go
func NewDocument(text string, opts ...ParseOption) (*Document, error)
func (d *Document) Text() string
func (d *Document) Edit(start, end int, text string) error
func (d *Document) Parse(opts ...ParseOption) (*Node, error)
// Generated only with WithTypes / -types.
func (d *Document) ParseAST(opts ...ParseOption) (StartValue, error)
func (d *Document) Stats() ParseStats
```

Constructor options resolve and validate once, without retaining the option functions. The unit and effective
depth become Document-local settings; Edit and positional actions use that unit. No backend selector is needed
for a standalone generated runtime. New APIs use functional options. Constructor settings provide defaults for
each parse; invocation overrides follow [design 033](033-cancellable-parses-and-work-budgets.md),
including ordered once-only resolution, context/limits/depth overrides and rejection of incompatible unit changes.
A fresh per-call context supports retries; memo state never retains an invocation controller.

Edit replaces [start,end), rejecting invalid range or byte character boundaries before modifying text or memo.
Invalid UTF-8 bytes are individual characters, as in the engine; an edit can join bytes into a valid sequence.
Document is not safe for concurrent use. A later Parse may reuse Node pointers held in memo entries retained before
an edit; it does not shift or move those Nodes. Edits discard memo entries at and after the edit. Results can share
Nodes through fields and children, so use `Node.Clone` to detach a tree before retaining or mutating it. Clone
preserves internal shared identities; it is not an immutable snapshot API and does not make arbitrary mutable any
values immutable.
Text and diagnostic strings already handed out remain owned data, independent of discarded parser scratch.

Each `Document.Parse` and `Document.ParseAST` call also publishes owned syntax diagnostics: recovered `SyntaxError`
values and their `Expected` and `Messages` slices are copied; ordinary failures are freshly
constructed. Mutating a returned diagnostic cannot change memoized errors or diagnostics from a later parse. This ownership guarantee does not detach the Node
graph, which may still share nodes with the mutable Document; use `Node.Clone` when detachment is needed.

`ParseAST` parses through the Node Document path and converts the final result on each call. The returned typed
values are owned by that invocation, while CST `Node` members may share the mutable Document graph; clone such
members when they must be detached. Parsing, diagnostic copying and conversion use the same invocation work
controller. If parsing, diagnostic copying or conversion is interrupted, the method returns the result's zero value
and clears reusable memo state as needed so a later call can retry.
Direct typed incremental matching remains deferred; `WithTypes` does not make the Document immutable.

### Proposed Direct Typed Document Matching

The current `Document.ParseAST` fallback parses through the Node Document and converts the finalized result on
each call. Direct typed matching is not implemented. A future opt-in direct path must preserve per-call ownership
of returned typed values while reusing only completed parse work. Keep typed memo entries separate from Node memo
entries, and use fresh parser and allocation arenas for each invocation so a later parse cannot overwrite values
referenced by an earlier result.

Retain only completed typed entries before the edit whose examined frontier is also before the edit; discard
entries that depend on left-recursion growth, provisional results, or the edited suffix. Generated wrappers may
memoize calls that are otherwise optimized away, but the existing default-off batch path must remain unchanged.
Before publishing a result, make an ownership-preserving typed copy wherever cached or scratch storage could be
observable. Preserve cycles, aliases and repeated references to the same slice view; unused slice capacity and
arbitrary overlapping slice views need not be preserved, matching `Node.Clone`'s ownership boundary. Parsing and
that copy use the same invocation controls. If either is interrupted, return the typed result's zero value and
clear the cache state required for a clean retry.

Direct typed matching should initially use the same eligibility rules as the current batch `ParseAST` path. Compare
against both the exact main revision and a generation-disabled same-source control. Measure batch parsing, fresh
Document parsing, warm parsing and edit-plus-parse, along with retained memory and allocations; do not infer typed
incremental gains from Node memo reuse alone.

### Go Reader Streams and Typed Elements

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

### TypeScript Document and Push Streams

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

## Runtime Invariants

For future suffix reuse and repetition resumption, generated analysis tables must carry examined-range
dependencies, transitive position/variable reads, recovery dependence, LR provisional dependence and resumption
eligibility. Matching includes lookahead and previous-character anchors. Memo keys preserve variable environment
and Operators Rule level. Edits shift only safe ranges; position-dependent values and recovered diagnostics cannot
simply have their root span shifted. Empty nodes at insertion boundaries follow the engine's copy rule. Shared
nodes move once per edit generation, across fields and children. Completed results dependent on a growing LR seed
are invalid after edits; abort/cancel drops all partial memo/resumption state before retry, while a completed syntax
failure may keep valid cache entries.

The current Node Document takes a conservative approach: an edit retains only completed memo entries wholly before
the edit and discards the suffix; it copies retained prefix entries and does not move suffix Nodes. It does not
resume repetitions. Any broader reuse must retain the exact
restrictions for predicates/variable reads, enclosing capture writes, recovered errors, provisional LR seeds and
positional values on length-changing edits. Every empty successful iteration counts once and stops. Preserve
repetition finite maxima/minima, cut scope, Operators Rule candidate-local diagnostics, and lookahead expectations.

For streams, discard only after the legal top-level #stream repetition commits an element. Preserve suffix/prefix
action semantics around the repetition: preceding values needed by later actions must own their text and backing
storage; a start action can still run at final completion. Forbid recovery wrapping the committing repetition in
either attribute order. Keep the previous character and absolute offsets/line state. A zero-width iteration is
emitted once and stops before checking the minimum, as the engine does; delivery cannot be undone by that failure.

The TS lowering requires explicit frames for rule calls, LR growth, Operators Rule candidates, choices, repeats, captures,
recovery and actions. Suspension at an input read saves the exact continuation and performs no speculative EOF
failure, action rerun or error replay. Distinguish awaiting bytes from completed parse failure at every caller.
Feed suspends at demand reads and returns only when waiting or terminal; a work-limit interruption is a terminal
error, not an implicit scheduling yield. A future cooperative scheduling API needs an explicit additional state.
Interrupted callbacks/read adapters and cancellation release pointer-bearing state without clearing published
values; no pooled parser or conversion arena is shared between concurrent invocations.

## Backend, Format and Implementation Gates

| Scope | Initial reference and proposed rollout |
| --- | --- |
| Closure and both engine VMs | Existing Document/Reader stream oracle; both position units |
| Generated Go Node | Current opt-in prefix-memo Document; Reader streams remain proposed |
| Generated typed Go | `Document.ParseAST` converts the completed Node result; direct typed incremental matching and stream conversion remain proposed |
| Generated TS Node/typed conversion | Documents and streams remain proposed; streams require continuation lowering |
| Recognition | Continue rejecting Document/stream mode; batch recognition is unaffected |

This source-generation feature needs no .pegoc or ISA change by itself. If continuation tables are instead stored
as bytecode, allocate/version new operands and validate them; do not silently change existing NEXT semantics.
Keep generated name collision resolution stable and compile external consumers for new exports. Requests for an
unimplemented generation/backend combination fail explicitly, rather than silently batch-parse all input.
Reserve the new exported type/value names in deterministic order and document resulting underscore migrations
for colliding grammar declarations; generating helper methods must not alter existing handwritten types silently.

The current implementation adds the generated Go Node Document with prefix-only memo reuse and per-call typed
conversion through `ParseAST`. Remaining work includes direct typed incremental matching, Reader streams, TS Documents,
and TS push continuations. Snapshot/versioned-tree work is a
separate dependency only if immutable views are exposed; do not block the mutable Document compatibility path on it.
Chunked input storage is a separate measured experiment: start with the current contiguous edit contract.
Cancellation/work budgets need matching cleanup points when introduced, not an ad hoc hidden API in this feature.

## Alternatives, Validation and Performance

The baseline is batch reparsing on every edit, and engine-backed Document/ParseStream when a dependency is acceptable.
A generated wrapper importing the engine offers fast implementation but gives up standalone deployment; retain it
as a benchmark/control, not the proposed product. Reparse buffered input on every feed is a useful test oracle,
but cannot safely reexecute actions or redeliver elements and does not provide bounded streaming. Using a worker
or async wrapper around synchronous TS does not make its execution suspend at demand reads. Sharing a bytecode VM
could reduce generator complexity but adds a separate portable runtime/distribution choice.

Proposed correctness gates compare semantic parse results, diagnostics and action values after random edits and all
reader chunk partitions against all three engine backends; resource-limit outcomes such as near-cap nesting are
excluded as specified above. Cover both units, invalid UTF-8 repairs, surrogate strings, zero-width repeats, recovery,
EOF versus temporary starvation, finite maxima, cut/Operators Rule ties, LR abort/retry, variables and start/end positional
actions. Exercise nil/action-discarded elements, typed concrete/union/list/any/CST conversions, retained text and
aliases, callback stops/panics, unknown reader errors, concurrent independent instances and terminal stream calls.
Run strict TS consumer/declaration tests and standalone Go consumers, including collisions and source determinism.

No throughput or bounded-total-memory claim is made. Measure exact parent and generation-disabled controls,
batch versus Document fresh/edit/reparse, Reader/push first-delivery latency and steady state, and typed conversion
separately. Include local/distant edits, large repetitions, LR/Operators Rule, growing streams and retained emissions.
Report allocations, live/peak memory, input/memo capacity, generated source/build sizes and public Node/typed
materialization cost; retained caller results and per-feed arrays are separate from runtime working memory.
Pin revisions, inputs and toolchains, serialize paired measurements, and gate ordinary batch regressions even
when only dormant parser fields change. The remaining implementation stages remain Proposed until their gates pass.

### Local Measurements for the Go Node Document Candidate

On an Apple M3 Max with Go 1.27.1 and GOMAXPROCS=1, ten paired one-second runs compared default generated `Parse`
against the parent revision across eight cases. Median time deltas ranged from -1.2% to +6.3%, with allocations
and B/op essentially unchanged. Recovery's +6.3% time result was noisy and should not be read as a stable cost
estimate.

A same-source generation-flag comparison used five 500 ms samples on a 4,096-record near-EOF fixture. Default-off
versus Document-enabled fresh `Parse` measured 1,444,747 vs 1,428,190 ns/op in CodePoints and 1,476,083 vs
1,483,182 ns/op in Bytes. Bytes/op and allocations were effectively identical: 2,487,670 vs 2,487,651 B/op
and 132 vs 132 allocations in CodePoints; 2,487,646 vs 2,487,645 B/op and 132 vs 132 allocations in Bytes.

For Document-enabled near-EOF edits, median `Edit+Parse` versus fresh-`Parse` times were 42,280 vs 70,035 ns
(256 records, CodePoints), 35,845 vs 73,114 ns (256, Bytes), 758,497 vs 1,404,930 ns (4,096, CodePoints),
and 631,864 vs 1,419,394 ns (4,096, Bytes). At 4,096 records, Document/fresh parsing used 1,498,944/
2,488,807 B/op in CodePoints and 1,359,680/2,488,505 B/op in Bytes. The candidate edit path scans and copies
the reusable memo prefix and discards the suffix. These synthetic measurements do not establish workload-wide
gains.

### Local Measurements for Typed Go Document Conversion

The typed Document slice was measured against main `dd19e0f` on an Apple M3 Max (darwin/arm64), Go 1.27.1,
`GOMAXPROCS=1`, `GOGC=100`, and `GOMEMLIMIT=off`. Three alternating 200 ms main/candidate pairs on the existing
Node fixture found median candidate/main ratios of 0.984–1.003 across fresh batch and near-EOF `Edit+Parse`
cases in both units; paired ranges were 0.975–1.025. Allocations per operation were unchanged and bytes per
operation differed by at most two bytes. This supports no measurable Node-path regression on that fixture.

The new [typed benchmark fixture](../../internal/engine/testdata/generated_document_bench/typed_bench_test.go)
uses 256 or 4,096 records of `é😀=123;`. Five 200 ms samples of the same generated revision measured direct
typed `ParseAST`, a warm `Document.ParseAST` without an edit, and near-EOF `Edit+Document.ParseAST`. The edit
benchmark includes the edit and conversion; direct inputs are prebuilt. Values were compared against a fresh
typed parse before timing.

| Records | Unit | Direct `ParseAST` | Warm Document | Edit + Document | Direct / warm / edit B/op |
| ---: | --- | ---: | ---: | ---: | ---: |
| 256 | CodePoints | 39,127 ns/op | 70,693 ns/op | 154,353 ns/op | 70,115 / 139,248 / 312,864 |
| 256 | Bytes | 41,075 ns/op | 64,917 ns/op | 152,218 ns/op | 70,115 / 122,864 / 329,248 |
| 4,096 | CodePoints | 654,560 ns/op | 1,342,077 ns/op | 2,042,025 ns/op | 825,602 / 1,770,658 / 3,148,528 |
| 4,096 | Bytes | 721,575 ns/op | 1,205,629 ns/op | 2,001,884 ns/op | 825,605 / 1,533,088 / 3,435,248 |

The current implementation converts the full finalized Node graph on every call, so memo reuse does not imply
typed-output reuse. On this simple grammar, direct typed batch parsing is faster even after a near-EOF edit.
For the 4,096-record Bytes batch case, enabling Document generation on the same source also measured 1.03–1.14×
the generation-disabled direct path across ten paired 300 ms runs, despite identical per-operation allocations;
the cause of this timing difference has not been isolated. These measurements do not
establish behavior for expensive grammars or typical editor workloads. Direct typed incremental matching remains
a separate performance stage.
