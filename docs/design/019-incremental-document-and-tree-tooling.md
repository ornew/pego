# 019. Incremental Document Storage and Tree Tooling

- **Status**: Proposed; no new public API is accepted by this record
- **Author**: @ornew
- **Date**: 2026-10-09

## Summary

Extend `Document` with input storage that avoids whole-document copying on small edits, immutable/versioned syntax
tree views and change notifications. Add traversal, schema and query tools for consumers of those trees. Keep PEGO's
ordered PEG choices, actions, typed results and standalone generators. This proposal defines component boundaries
and compatibility constraints; feature-specific records must settle API signatures and representation choices.

## Motivation

Reusing a parse result is only one part of an editor update. Moving input, preserving old trees and updating indexes
or highlights can dominate even when the parser evaluates few rules. PEGO already has incremental memoization; the
proposal addresses the surrounding costs and gives grammar authors reproducible ways to check editing correctness.

### Evidence

The comparison scope is PEGO at `9353e0922641` and Tree-sitter at
[`e9930e091568`](https://github.com/tree-sitter/tree-sitter/tree/e9930e0915685efb98a9d325b8f23dfdb9e99559).
PEGO's [input representation](../../internal/engine/input.go), [Document reuse](../../internal/engine/document.go),
[incremental tests](../../internal/engine/document_test.go) and
[interruption tests](../../internal/engine/document_abort_test.go) establish the current behavior.
Equivalent PEGO and Tree-sitter workloads have not been benchmarked for this proposal. Expected performance benefits
remain hypotheses; existing engine memo retirement has focused measurements in [optimization 067](../optimizations/067-retire-memo-entries-when-their-lookup-slots-are-removed.md).

Relevant upstream interfaces include [chunked input](https://tree-sitter.github.io/tree-sitter/using-parsers/2-basic-parsing.html),
[static node types](https://tree-sitter.github.io/tree-sitter/using-parsers/6-static-node-types.html),
[queries](https://tree-sitter.github.io/tree-sitter/using-parsers/queries/1-syntax.html) and
[edit fuzzing](https://tree-sitter.github.io/tree-sitter/cli/fuzz.html).

### Existing foundation

`Document` already tracks examined input, including lookahead, invalidates dependencies after edits, applies memo
shifts lazily and resumes long repetitions. Ordinary parses already have transient memoization, value-free paths,
first-character dispatch, specialized character classes and slab allocation. These are implemented capabilities,
not new Tree-sitter-inspired tasks.

Current limitations matter to the new APIs:

- `input.replace` moves a byte/rune suffix, rebuilds the source string and updates a code-point offset suffix. A
  small reparse can therefore still require work proportional to document size during the edit.
- Reused nodes have absolute positions updated in place. Previously returned trees change; `Node.Clone` preserves
  a tree with a deep copy. `Document` and its shared results cannot be read concurrently with parsing.
- `Stats` counts evaluations/reuse, but does not describe changed output ranges. Actions may return scalar fields,
  references shared by children and fields, or nil; output is more general than a conventional CST.
- Discarded punctuation, comments and whitespace cannot be reconstructed from an AST. The existing highlighting
  cookbook demonstrates grammar-controlled captures, not a general lossless syntax tree.
- The language server edits `.pego` grammars. Target-language queries/highlighting are a separate consumer layer.
- Generated Go/TypeScript parsers currently support neither streams nor Document. New engine APIs must identify
  supported backends explicitly rather than imply generator support.

## Goals

- Make parsing interruptible and editing failures replayable before expanding editor-facing APIs.
- Expose existing trees and inferred types through reusable traversal/schema tools.
- Reduce measured input-update and snapshot costs without changing existing results or ownership implicitly.
- Give downstream tools sufficient change information to update safely, with bounded query/recovery work.

## Non-goals

This record does not specify final API signatures or replace feature-specific designs. It does not adopt GLR,
Tree-sitter query compatibility, automatic whitespace handling, a full target-language semantic server or a new
runtime dependency. It does not claim Tree-sitter performance parity or a speedup before equivalent measurements.

## Design

### Components and data flow

The proposed architecture separates storage, parsing and tree consumers:

1. **Document input storage** owns the current text and edit history, exposes character/range reads and position
   conversion, and can use chunks or pieces. A batch string parse retains its contiguous representation. Text
   returned to a node or snapshot needs a defined lifetime independent of later edits.
2. **Versioned tree storage** associates output with a document version. A snapshot holds an immutable view; parsing
   a new version may share unchanged internal subtrees and child sequences. Existing mutable Nodes remain a
   compatibility path. Position-valued actions still invalidate when their positions change.
3. **Change notifications and tree tools** use explicit versions and units. Notifications conservatively cover text
   and output changes; cursors/queries use a defined child/field graph and can restrict work to safe changed regions.
   A schema exposes what the compiler knows without inventing types for dynamic output.

Existing memo dependency tracking and repetition resumption stay in the parser, with cancellation/work budgets as
shared execution controls. Replayable edit fuzzing verifies storage/parsing equivalence before consumers rely on
notifications. Strict parsing stays available; bounded missing-token insertion is a separate optional recovery design.
The current Node API cannot serve as an immutable snapshot simply by sharing pointers: reuse moves those Nodes.
Choose an immutable internal representation and adapter, or a separate snapshot representation, through the
alternatives and measurement gates below.

### Dependencies and acceptance gates

Correctness/resource safety and fresh-vs-incremental equivalence gate these capabilities. Keep regression fixes
separate from new public APIs. The [development guide](../development.md#language-feature-status) summarizes
implemented language features; the backlog and execution order live in
[Issue #1](https://github.com/ornew/pego/issues/1).

| Stage | Backlog | Work and prerequisite | Acceptance evidence |
|:--|:--|:--|:--|
| 1 | F01 | Cancellation and work budgets; reuse interruption cleanup | Bound cancellation latency in calls, scans, repeats and LR; retry/edited Document equals fresh parse; quantify uncancelled overhead |
| 2 | F16, F06, V01 | Grammar-author edit-fuzz CLI/API, then reusable fixtures | Corpus + seed + replayable/minimized failures; compare values, spans and errors against fresh parsing after edits/undo/redo on every backend/unit |
| 3 | F10, F17 | Iterative Walker/Cursor and versioned output schema | Define child/field order, shared identities, cycles, scalar fields and range filtering; schema marks unknown/dynamic output explicitly |
| 4 | P10 | Chunked Document input, initially internal | Edit-only and parse benchmarks at multiple sizes/positions; UTF-8/chunk boundaries, invalid bytes and position conversion; retain fast contiguous batch path |
| 5 | F04 | Versioned shared snapshots and shared sequence representation | Old versions stay immutable; action-position dependencies remain valid; compare Clone cost, position updates, concurrent reads and retained versions |
| 6 | F18, F19 | Conservative change notifications, then minimal structural queries | No omitted updates for text/scalar changes; compare incremental indexes/highlights with full recomputation; budget and cancel query work |
| 7 | F20 | Explicit missing-token recovery for editing | Zero-width missing tokens distinguished from skipped Error nodes; progress/limits, rollback, EOF and typed/backend equivalence |
| 8 | P11, P12 | Literal dispatch and compact leaf experiments | Adopt only measured wins with unchanged PEG ordering, diagnostics, trace policy and lookahead dependencies |

Stages 4–7 need separate design records with concrete contracts before code. Stage 3 can precede shared trees: APIs
should state their ownership and allow later snapshot implementations. Stage 6 can begin with conservative ranges
without waiting for exact tree differencing; safe invalidation matters more than minimality.

### Contracts to carry into feature designs

**Cancellation.** Start with the Go API and specify deterministic work units separately from wall-clock deadlines.
Define errors, nil results, recovery diagnostics and callback ownership. Cancellation checks must cover long
terminal scans, not just rule calls. Cancelling a blocked arbitrary `io.Reader` requires a separate reader ownership
contract; a context check in the parser cannot promise that. Generated equivalents follow their own portable design.

**Input and snapshots.** Do not silently change `Node.Start/End`, exported children/fields, `Document.Parse` sharing
or `Clone`. Prototype an internal input abstraction with character reads, interval text and position conversion.
Tree-sitter's input callback allows caller-owned ropes/piece tables; it does not supply PEGO with a rope implementation.
For snapshots, investigate relative internal spans plus versioned references and chunked child sequences. Keeping a
mutable Node API may require materialization; include that cost. Structural sharing does not make an action that
stores a position value reusable after a shift. A breaking Node/Document contract requires an explicit compatibility
and migration decision.

**Traversal and schema.** Specify visits by edge or by unique identity, deterministic field/list order, parents when
nodes have multiple incoming edges, and a cycle policy for hand-built Nodes. Cursor ranges need a position unit and
an empty-node boundary rule. Output schemas distinguish declared/inferred typed output, normal CST, captures and
dynamic `any`; they must not claim a complete static schema where actions prevent one. This extends existing type
information and does not adopt Tree-sitter's JSON format as a compatibility promise.

**Notifications and queries.** Edited text, re-evaluated grammar and changed output are different ranges. Replacing
`foo` with `bar` may leave node kinds/spans unchanged while invalidating a text predicate or name index. Action
scalars and enclosing scopes can also affect consumers. Begin with documented conservative invalidation, version IDs
and edit mapping; exact structural/value differences are a later optimization. Minimal queries match types, rules,
fields and captures with bounded traversal. Add text predicates and highlight configuration afterward. Lossless
token/trivia retention is a prerequisite for punctuation/comment highlighting when actions discard those tokens;
scope coloring does not promise full name resolution or type checking.

**Recovery.** Keep strict parsing and current consuming `#recover` semantics. Design opt-in insertion at explicit
recovery points with an insertion count/budget, same-position progress guard, backtracking rollback and typed result
representation. Merely allowing a zero-consumption skip would risk an infinite recovery loop.

**Embedded languages.** F15 starts with contiguous fragments and an explicit source-position map. Discontiguous
included ranges, escaped strings and edit-tracking source maps come later, once notifications and ownership are
defined. Host scanners/functions, if added, must snapshot/restore their state and include it in reuse dependencies.

## Testing and performance measurement

Measure initial Parse/Recognize/typed output separately from Edit, reparse, snapshot/Clone, traversal and query or
highlight updates. Use small, medium and multi-megabyte documents; edit the start, middle and end; include line breaks,
brackets, comments, Unicode, EOF and multiple edits before parsing. Add narrow replayable failures before benchmarks.
Report time, B/op, allocations, examined/reused work and live heap with multiple saved versions; include release of
old versions. Benchmark default batch parsing to catch abstraction overhead.

A Tree-sitter comparison must pin both revisions and toolchains, match accepted language subsets, output/trivia,
recovery behavior and units, and distinguish C/Go/runtime overhead. Recognition-only and direct typed AST output are
not equivalent to building a CST. Record raw results and measured effects in the optimization catalog with the change, and
refresh generated benchmark results at optimization milestones. No numeric comparative speedup is asserted here.

## Alternatives considered

- **Keep contiguous input and deep Clone for every editor operation.** This remains a useful baseline and has the
  simplest ownership model. It avoids new abstractions but retains whole-document edit copying and snapshot costs.
  Keep it for batch parsing and as the reference path when evaluating an editor-specific representation.
- **Require callers to provide a rope immediately.** A generic callback can fit existing editor storage, but adds
  lifetime/position contracts and hot-path indirection before their cost is known. First prototype an internal
Document abstraction; compare a built-in piece table with caller-owned input before choosing the public boundary.
- **Replace public Nodes with relative immutable nodes in one migration.** This could make shared snapshots natural,
  but changes exported positions/children/fields, action access and existing consumers at once. Prefer an additive
  versioned representation or materialized view until measurements justify a breaking migration and it is approved.
- **Notify only exact structural differences.** This can reduce downstream work, but misses same-shape text and scalar
  changes, and requires an old immutable view or extra comparison state. Begin with conservative versioned
  invalidation; exact differences can optimize it after consumers pass full-recomputation comparisons.
- **Implement Tree-sitter Query compatibility first.** It would reuse existing query knowledge, but PEGO output has
  scalar fields, aliases and action-generated graphs that need different ownership/order rules. Start with a small
  PEGO schema-aware query layer and consider compatibility only when the required semantics are clear.
- **Allow current skip recovery to consume nothing.** This is a smaller syntax change, but permits repeated recovery
  at one position without progress and gives typed results no missing-token contract. Use explicit bounded insertion
  rather than weakening the existing consuming recovery guarantee.
- **Use deadlines alone or check cancellation only at rule entry.** Deadlines suit interactive tasks but do not make
  replay deterministic; rule-only checks may not interrupt a long scan. Design work budgets and polling in long loops
  as well, and measure the cost with cancellation disabled.
- **Leave edit fuzzing as repository-only tests.** It avoids maintaining a CLI, but grammar authors cannot readily
  reproduce the same checks for their own grammars. A reusable CLI/API should share the oracle and persisted failure
  format, with a separate fixture runner for ordinary accepted/rejected examples.

### Deferred semantic changes

GLR/dynamic precedence, automatic extras and embedding Tree-sitter's runtime are not part of this plan. They alter
PEG ordering, whitespace/indentation semantics or the standalone distribution contract. Existing captures, terminal
types and struct/union types cover much of the motivation for hidden rules, fields and supertypes. Revisit these only
for a demonstrated gap with a separate design and compatibility decision.

## Limitations and open questions

- API signatures, deterministic work units and cancellation polling intervals need a concrete F01 design and latency
  measurements. Blocking reader interruption is a separate ownership problem.
- A chunk/piece-table implementation may reduce edits while slowing character reads or text materialization. The
  representation and public input boundary remain open until size/position benchmarks and malformed-UTF-8 tests pass.
- Shared snapshots can retain substantial history. Define release/history limits and concurrent-read ownership, and
  measure retained versions as well as creation cost. Public Node mutation remains supported by the current API.
- Cursor/query edge identity, traversal order and scalar matching must be specified before schema consumers rely on
  them. General token/trivia retention may require grammar output changes; it cannot restore already discarded input.
- Conservative ranges may cover much of a document for context-sensitive actions or enclosing scopes. Safe bounds
  come first; minimality is an optimization, not the initial guarantee.
- Missing-token syntax, typed representation and recovery budgets need a separate language/runtime design. Backend
  support and bytecode compatibility must be stated explicitly.
- None of these new APIs is implemented by this record. The sequence may change with reproduced defects or benchmark
  evidence. Decisions that break public contracts will be presented with a concrete compatibility proposal.
