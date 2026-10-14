# 042. Responsive LSP Analysis and Code Actions

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Extend `pego lsp` to publish grammar lint findings, offer version-checked explanations and safe quick fixes, and keep analysis responsive while a document changes. The proposed protocol keeps the existing push-diagnostics model and adds cancellable, coalesced analysis snapshots. The complete responsive protocol remains proposed. An opt-in prototype provides lint diagnostics and guarded explanations; the server also returns versioned suppression edits to clients that advertise the required workspace-edit capability. The VS Code client currently filters those edits because its `WorkspaceEdit` conversion drops the document version and `workspace.applyEdit` cannot enforce it atomically. Documentation format negotiation applies to ordinary requests. The opt-in actor connects analysis and request workers to protocol input, cancellation and publication; ordinary clients retain the existing synchronous path. Bounded cancellation now covers text-index construction, lexical tokenization and syntax parsing; cancellation within compilation and lint, plus real-editor validation, remains open.

## Prototype Implementation Boundary

The VS Code extension exposes the opt-in setting
`pego.server.experimentalLint`, which defaults to `false` and requires a language
server restart after it changes. When enabled, the extension advertises
`experimental.pego.lintDiagnostics`, `experimental.pego.responsiveAnalysis`, and
`experimental.pego.showExplanation`. Other clients can advertise these
capabilities independently: lint diagnostics also enable code actions, while
responsive analysis alone enables the asynchronous analysis actor without lint.
Without lint opt-in, diagnostics retain their existing syntax and compile/type
scope. This remains an opt-in compatibility boundary, not adoption of a default
lint policy.

The prototype shares the successful compiled analysis with `internal/lint.Run`
and maps its source-positioned findings to stable check codes and
URI/version/generation/finding-key data. It uses `main` when defined; without `main`, it
omits reachability analysis because an editor start-rule setting is not yet
specified. Existing lint directives remain authoritative. No old lint findings
are reused after an analysis failure.

An explanation action additionally requires
`experimental.pego.showExplanation: true`. The VS Code client handles
`pego.showExplanation` by displaying the check name, finding message, and
explanation; it rechecks the document version and analysis generation before
showing the message. The server can return a versioned suppression edit to a
client that advertises
`workspace.workspaceEdit.documentChanges: true`. It does so only after
recompiling and linting the candidate source, proving the targeted finding
disappeared, and checking that the inserted directive is valid. The edit inserts
a standalone `lint:ignore` comment immediately before the finding line and uses
the document's newline convention. The VS Code client currently withholds
suppression edits: its `WorkspaceEdit` conversion drops the document version,
and `workspace.applyEdit` has no conditional version parameter. A check before
application cannot close that race. A version-safe suppression action remains
unavailable in VS Code until the client can apply the edit atomically against
the analyzed version. The server-side candidate is limited to safe comment
positions; rule-wide suppression needs an explicitly scoped action. There are
no suggested grammar transformations and no alternative-deletion action.

`analysisQueue` connects immutable version/generation snapshots to one running
analysis worker globally and one replaceable pending snapshot per document.
The actor owns documents and JSON-RPC output, cancels superseded work, and
publishes only results matching the current URI, version and generation. It
computes lint once in the analysis worker and shares those immutable findings
with diagnostic publication and action requests.

Text-document requests wait for their captured analysis snapshot and run against
immutable copies. At most 64 requests are accepted concurrently and at most four
request workers execute at once; excess requests receive `ServerCancelled`
(-32802). These are conservative prototype bounds, not measured defaults. A
`$/cancelRequest` notification returns `RequestCancelled` (-32800) for its
matching in-flight request. Change, save, reopen and close invalidate matching
requests with `ContentModified` (-32801). The actor removes each request before
responding and discards late worker results, including late edits. Failed current
analysis publishes only its current failure diagnostic; when last-good navigation
ranges belong to older text, responsive requests receive `ContentModified`
instead of interpreting current positions through old text.

Shutdown cancels pending analysis and requests; exit joins analysis and request
workers before returning. Worker panics become failure results. The reader stops
without beginning another read after decoding exit. An arbitrary reader blocked
outside a normal exit sequence cannot be interrupted without a caller-provided
close/cancellation contract; no input ownership change is introduced.

`analyzeContext` builds the UTF-16 text index privately and polls cancellation
at bounded intervals while scanning input bytes; a cancelled build returns no
partial index. Initial lexing also polls at bounded intervals while decoding
source runes, scanning tokens, processing long strings and character classes,
normalizing comments, and assembling token/comment boundaries. Token-index
conversion checks between batches as well. Partial parsing polls as it consumes
tokens and scans ahead or backward for lambda syntax, line breaks and error
recovery. Cancellation makes tokenization and partial parsing return no tokens,
grammar or diagnostics, so an incomplete analysis is not published.

`internal/engine.CompileContext` is an internal entry point used by LSP
analysis; the ordinary `Compile` API is unchanged. The context is polled during
rule/dependency analysis, matcher compilation and type traversal. This is a
partial compiler slice: `grammar.Validate`, attribute normalization, shared
grammar-analysis success helpers and some large single type-helper operations
remain unpolled. Lint and some request traversal/formatting routines also
observe cancellation only at phase boundaries or lack checks. The checks bound
work at selected points, not total LSP cancellation latency. Suppression
validation uses its request context before and after candidate analysis and
lint. The VS Code
explanation-command handler is implemented. Real-editor validation and
full-phase resource measurements remain open; safe suppression application
also remains open because the client API cannot atomically enforce the document
version.

Hover and completion negotiate Markdown only when explicitly advertised;
otherwise their documentation is plaintext. Labels, details and insertion text
retain their established behavior. Completion uses current constructor
`T{field: value}` and restricted-call `level(rule, name)` syntax. It offers
remaining declared constructor fields, the `skip` key in `#recover()`, and the
named levels of the selected operators rule. Defining a new struct field does
not imply an inventory of suggested field names.

Current lexical and header contracts remain authoritative: attribute and pragma
names must be adjacent to their markers, and comments inside statement headers
are rejected. Comment regression fixtures place comments inside an explicit
operator-expression delimiter and retain rejected-header controls; they do not
restore legacy attribute syntax or accept naked multiline operator headers.

## Motivation

The ordinary compatibility path handles each message synchronously. Its `didOpen`, `didChange` and `didSave` analyze and publish before reading the next message, and it ignores `$/cancelRequest`; the opt-in actor addresses this limitation. The implemented design measured roughly 12 ms for the largest example grammars at the time of design 017; that measurement does not establish the cost of linting or future grammars. `internal/lsp/analysis.go` tokenizes and parses every version and compiles only when syntax is valid. `internal/lsp/server.go` publishes only those diagnostics and serializes all requests.

The public linter already defines stable check names and severities, returns source positions, rule names, messages and human-readable `Fix` suggestions, and applies `lint:ignore` / `lint:file-ignore` directives before returning findings (`internal/lint/lint.go`, `docs/design/018-grammar-linting.md`). `Fix` is explanatory text, not an edit. A grammar with syntax errors cannot be linted by the public `pego.Lint` API because lint requires a valid, compiling grammar. The editor should therefore show lint findings only for a version whose grammar compiled successfully.

Design 017 deliberately has no code actions and always returns Markdown for hover and completion documentation. The editor-support follow-up also asks the server to honor plain-text documentation formats. A document version is attached to pushed diagnostics, and the server validates versioned action candidates. VS Code still withholds suppression edits because its application API cannot enforce the version atomically.

## Goals

- Publish syntax/compile diagnostics as today and add lint diagnostics after successful compilation, preserving lint check IDs, messages, severities, positions and source suppression.
- Explain a lint finding and offer suppression or a reviewed, semantics-preserving quick fix without treating `Finding.Fix` as replacement text. A client must be able to apply edits atomically against the validated document version.
- Refuse to apply an edit computed for an obsolete document version.
- Let a newer document version supersede pending analysis and let JSON-RPC cancellation stop work at supported boundaries.
- Honor the client's Markdown/plain-text preferences for hover and completion documentation.
- Preserve the existing UTF-16 LSP ranges, document-local analysis model and partial-grammar navigation after syntax errors.

## Non-Goals

- Linting partial grammars or changing lint checks, check severity, or `lint:ignore` semantics.
- Automatically changing a grammar based on a heuristic performance hint.
- Automatically deleting a shadowed ordered-choice alternative. The known left-recursion interaction remains unresolved; no `shadowed-alternative` deletion action is allowed by this design.
- Workspace-wide imports, pull diagnostics, or replacing the current standard-library JSON-RPC implementation.

## Design

### Diagnostics and Source Mapping

Keep push diagnostics. Add optional LSP diagnostic `code` and `data` fields; for a lint finding, `code` is the stable check name and `data` contains the analyzed document version plus a server-owned finding key. Map lint `Error`, `Warning` and `Hint` to LSP severities 1, 2 and 4 respectively, and use source `pego-lint`. Existing syntax/compile diagnostics retain source `pego` and error severity. Convert `grammar.Pos` through the existing `textIndex` byte-offset mapping before making a UTF-16 range. A finding without a valid source position remains available in CLI output but is not published as an editor diagnostic.

After syntax parsing and compilation succeed, run lint with the same start rule and default enabled checks as `pego lint`. The linter's existing directive pass already removes suppressed findings and reports malformed, unknown, or unused directives. Do not run lint after syntax or compile failure. Preserve the last-good-analysis behavior for internal failures, but never publish lint findings from an older snapshot as if they belonged to the failed current version.

Avoid compiling the same grammar twice if the LSP can call a shared lint-analysis entry point over the already compiled program. If that requires changing the public lint contract, keep it internal and verify parity against `pego.Lint`; the CLI remains authoritative for check behavior.

### Code Actions and Explanations

Register `textDocument/codeAction` and advertise its provider. Accept the requested range and context diagnostics; return actions only for diagnostics whose URI, version, check code, range and finding key match the current analysis. Define the response using ordinary LSP `CodeAction` fields (`title`, `kind`, `diagnostics`, optional `edit`, `command`, and `data`).

Provide these actions:

- **Explain finding** invokes the `pego.showExplanation` client command with the check name, original diagnostic message, and a concise explanation derived from the check documentation. The VS Code extension displays it in an editor notification or equivalent. The extension advertises `experimental.pego.showExplanation` in client capabilities; the server returns this action only to clients that advertise the handler. Other clients retain the complete diagnostic message without receiving an unusable action.
- **Suppress finding** returns a versioned `WorkspaceEdit` adding `// lint:ignore <check>` on the finding line, using the document's line ending. The server offers this candidate only when the edited grammar remains valid and lint confirms its documented suppression scope. If a directive already covers the check, it does not offer a duplicate. A directive may suppress other findings in the same scope, which the action must make explicit; file-wide suppression is not the default. Clients that cannot enforce the document version atomically must not expose or apply the edit. The current VS Code client filters suppression edits because its `WorkspaceEdit` application path cannot enforce that version.
- **Apply suggested fix** is offered only when the server has a check-specific, structured edit implementation and tests proving the edit preserves the accepted language and surrounding source. The string in `Finding.Fix` remains display-only. Do not offer removal of a shadowed alternative, including left-recursive alternatives, while its semantic interaction is unresolved.

Use `WorkspaceEdit.documentChanges` with `TextDocumentEdit` and a `VersionedTextDocumentIdentifier` for every changing action. Do not return an unversioned `changes` fallback. A client that cannot apply a versioned edit atomically may still use the explanation action, but must not be offered suppression. Return no stale action if the document version changes while the request is being handled. Revalidate the finding key and version immediately before producing the response; after a capable editor applies an edit, normal `didChange` analysis establishes the resulting state.

### Responsive Snapshots, Cancellation and Publication

Keep document state under one server actor. The actor owns the open-document map and protocol output. A reader goroutine decodes incoming messages and sends them to the actor; document analysis runs in workers against immutable copies containing URI, text, version and a monotonically increasing generation. Workers return a result message to the actor; they never write protocol output or mutate a document.

On open, change or save, update the snapshot immediately, cancel the prior generation for that URI, and queue the newest analysis. Coalesce pending work so an obsolete generation that has not started is discarded. Check cancellation between tokenization, partial parsing, compilation, lint, and result construction. Add context-aware internal variants for long phases so they can stop within those phases; existing public parse and lint APIs remain unchanged. Cancellation is cooperative and must be polled at bounded points in grammar/token/input loops. A cancellation is not a syntax diagnostic.

Publish a worker result only if its URI is still open and both version and generation equal the current snapshot. A stale worker result is discarded, including its diagnostics and failure. A current result publishes diagnostics with that snapshot's version. `didClose` cancels work and clears diagnostics as today. Requests that need analysis capture one snapshot and either receive a result for that snapshot or an LSP `RequestCancelled` (-32800) / `ContentModified` (-32801) response; never combine ranges from one version with symbols or edits from another. A `$/cancelRequest` notification cancels only the matching request's context; a newer document snapshot independently cancels old per-document analysis.

Bound outstanding work to one running analysis and one replaceable pending snapshot per open document. Bound the worker count, preserve protocol write order through the actor, and ensure every started request receives at most one response. Analysis completion must not block reading cancellation, change, close, shutdown or exit messages. On shutdown, cancel and join workers before the server returns.

### Client Documentation Formats

Record `textDocument.hover.contentFormat` and `textDocument.completion.completionItem.documentationFormat` during initialize. Return the first supported format in the client's advertised order; if none is supported, return plain text. Thus a client that advertises `plaintext` before `markdown` continues to receive plaintext. Render the same semantic description in both forms, stripping markup rather than returning Markdown markers as plain text. This applies to hover and every completion item's `documentation`; `detail`, labels, snippets and insertion behavior stay as they are.

### Compatibility and Implementation Sequence

Existing diagnostic fields and codes remain valid; the new `code` and `data` fields are optional. Lint diagnostics are additive and honor directives exactly as `pego lint` does. Changes to hover/completion content kind follow the capabilities sent by each client. Existing clients continue to receive error diagnostics and UTF-16 ranges.

1. Add shared finding-to-diagnostic mapping and lint integration for compilable documents. Verify source suppression and CLI parity.
2. Add structured code-action protocol models, versioned suppression edits, check explanations, and a VS Code command handler. Keep suggested grammar edits behind a separately reviewed allowlist. VS Code suppression remains withheld until its client API can enforce edit versions atomically.
3. Introduce immutable snapshots, actor-owned state/output, request cancellation, per-document coalescing, and cooperative cancellation through analysis phases. Keep stale-result guards even when cancellation succeeds.
4. Honor hover and completion documentation formats, then validate the protocol and workflows in real editors.

## Alternatives Considered

- **Run lint in the synchronous handler.** This is the smallest change, but increases the time during which the server cannot read cancellation or newer edits. It is suitable as the first diagnostic-only stage, not as the responsive design.
- **Debounce with a timer but keep a single blocking loop.** It can reduce repeated work after typing stops, but cannot read a cancellation or change notification while analysis is running and therefore cannot guarantee freshness.
- **Publish lint findings for a partial grammar.** The partial AST omits failed definitions; compiling or linting it would invent undefined-reference and reachability results. Keep lint restricted to a fully parsed, successfully compiled snapshot.
- **Turn `Finding.Fix` into a text edit.** It is a human-readable suggestion with no range or replacement contract. Use explicit per-check transformations only after their syntax and semantic preservation are tested.
- **Return unversioned edits for clients without versioned workspace edits.** This removes the stale-document guard exactly where the client cannot enforce it. Omit changing actions for those clients.

## Testing

Add JSON-RPC tests for lint error/warning/hint severity, stable check codes, exact UTF-16 ranges, directive suppression, malformed/unused directives, syntax/compile failures, and CLI/LSP finding parity. Cover an analysis panic and ensure old findings are not published for a new version.

For actions, test matching and mismatching version/check/range/data, suppression insertion with LF/CRLF/CR, existing applicable directives, no duplicate action, versioned `documentChanges`, advertised and unadvertised client command support, and the absence of automatic shadowed-alternative deletion. Apply each returned edit to the captured source and verify it targets the intended finding.

For responsiveness, use controlled barriers in analysis phases to send a newer change, close, request cancellation, shutdown and exit while work is pending. Verify coalescing, bounded workers, cancellation latency at each polling boundary, exactly one response per request, no stale diagnostics or edits, and no writes from workers. Race-test actor/worker ownership. Exercise multi-file concurrency and a large generated grammar without timing assertions.

Negotiate Markdown-only, plaintext-only and mixed format capabilities; assert the `MarkupContent.kind` and equivalent rendered content for hover and completion.

## Performance and Results

Focused measurements used Go 1.27.1 on an Apple M3 Max (darwin/arm64). In `BenchmarkAnalyze`, counterbalanced blocks ran five samples per branch in main-then-integration order and three per branch in integration-then-main order. Results were mixed: Go case medians were 17.20/17.24 ms on main and 18.68/17.55 ms on the integration branch; Python medians were 23.67/22.46 ms and 22.79/22.64 ms respectively. The order effect prevents a stable regression conclusion. On the final integration branch, five `BenchmarkF08Analyze` repetitions showed that adding lint and diagnostic mapping changed generated-fixture/Go/Python medians from 7.79/18.69/24.14 ms to 8.53/25.39/29.10 ms. Allocations changed from about 15.47 MB/40,300 to 17.17 MB/43,700 for the generated fixture, 30.01 MB/104,500 to 34.27 MB/121,500 for Go, and 39.73 MB/126,600 to 43.47 MB/145,800 for Python. These are focused branch measurements, not a general responsiveness or memory-peak claim.

Synthetic bursts of 32 or 256 edits per document held two generations for one document and five for four documents: one running and one latest pending generation per document. At that held barrier, signed post-GC heap deltas against idle were about 74 KB and 295 KB. On the final integration branch, a protocol-level synthetic active-work cancellation probe measured mean cancel-to-reply of about 8.7–11.4 μs and cancel-to-synthetic-stop of about 6.3–7.3 μs. These probes do not measure cancellation within parser/lint phases or tail latency. No full-phase cancellation or peak-memory result is claimed. Preserve these limits when comparing editor behavior; benchmarks alone do not establish responsiveness.

An additional paired comparison of `analyzeContext` used Go 1.27.1 on an Apple
M3 Max (darwin/arm64), `GOMAXPROCS=16`, `GOGC=100`, five alternating pairs, and
300 ms per sample. It compared main `da5dbed` with the cancellable-lexer
candidate. Median candidate time was 3.7% higher for the small input, with a
noisy paired range of 0.863–1.135×; allocated bytes were 40,416 versus 40,400.
For many rules, median time was 1.4% higher and bytes were 3,143,447 versus
3,143,431. For 64 KiB comment inputs, median time was 4.3% higher for ASCII,
10.3% higher for emoji, and 6.9% higher for whitespace. Allocated bytes were
1,067,991 versus 936,899 for ASCII, 647,266 versus 516,175 for emoji, and
778,422 versus 778,386 for whitespace. These measurements cover this analysis
entry point and these workloads only. At the time of this comparison, parser,
compiler and lint cancellation remained limited to phase boundaries; the
parser-phase change described above is measured separately below.

A separate five-pair, 300 ms alternating comparison measured the ordinary
`syntax.Parse` path on the same Go 1.27.1, Apple M3 Max, `GOMAXPROCS=16` and
`GOGC=100` setup, comparing main `da5dbed` with the lexer candidate. For a
256-rule grammar, the paired median was 1.0252× (range 1.018–1.037), with
bytes/op 874,516→874,520 and allocations/op unchanged at 3,613. For a 64 KiB
ASCII comment, the median was 1.0459× (1.031–1.069), with bytes/op
486,337→420,805 and allocations 64→63. For a 64 KiB emoji comment, the median
was 1.0155× (1.006–1.024), with bytes/op 289,735→224,198 and allocations
64→63. Thus these ordinary-parse workloads had modestly higher time while
allocation volume was unchanged for many rules and lower for the comment
inputs. This is separate from the `analyzeContext` results above and does not
measure cancellation latency or parser/compiler/lint interruption.

A five-pair, 300 ms alternating comparison then measured `syntax.Parse` and
`ParsePartialContext` on Go 1.27.1, Apple M3 Max (darwin/arm64),
`GOMAXPROCS=4`, `GOGC=100`, and `GOMEMLIMIT` off, against main `67080bf`.
For ordinary Parse on Small, Self, Comments and ActionComments inputs, the
candidate/base median ratios were 1.0148, 1.0186, 1.0089 and 1.0176; paired
ranges were 1.0110–1.0412, 0.9976–1.0374, 0.9976–1.0615 and 1.0098–1.0252.
For the context-enabled ParsePartialContext path, the corresponding medians
were 0.9987, 1.0018, 1.0068 and 1.0093, with ranges 0.9479–1.0343,
0.9990–1.0227, 0.9908–1.0555 and 1.0082–1.0118. Allocations/op were
unchanged; B/op was unchanged or differed by only a few bytes. The ordinary
path was about 1–2% slower in these samples, while context-enabled results
were near parity and noisy. This benchmark did not measure cancellation
latency.

A five-pair, 300 ms alternating comparison of ordinary compilation used Go
1.27.1 on an Apple M3 Max (darwin/arm64), `GOMAXPROCS=4`, `GOGC=100`, and
`GOMEMLIMIT` off, with main `bd128f4` as the base. Candidate/base median time
ratios were 1.0293 for a 512-rule dependency case (range 1.0137–1.0767,
approximately +57 B/op and one extra allocation/op), 1.0195 for JSON
(1.0034–1.0298), 1.0086 for a source grammar (0.9988–1.0247), and 1.0136 for
Operators Rule compilation (0.9893–1.0285). The latter three had approximately
unchanged B/op and allocation counts. These samples show roughly 1–3% higher
ordinary Compile time on the tested cases, with the observed ranges crossing
parity for some cases. This measures ordinary compilation only; the
context-enabled path and cancellation-to-return time were not measured.

## Limitations and Open Questions

- The responsive actor remains opt-in and needs real-editor validation; ordinary clients retain the synchronous path.
- Syntax parsing and selected compiler rule/dependency/matcher/type traversals poll context within phases. Some validation, normalization, shared analysis helpers, large type-helper operations and lint work remain unpolled.
- Suppression application requires a client API that atomically checks the edit version. The current VS Code `WorkspaceEdit` path drops that version, so the extension withholds suppression while retaining guarded explanations.
- The exact work bound and worker-count default should be selected from measurements on large grammars and multi-document editor workloads.
- **Proposed lint scope:** enable enabling all existing checks for every compilable open grammar, with source directives as the suppression mechanism. An owner may instead choose a configurable lint set if editor noise is observed; no such setting is part of this proposal.
- **Proposed explanation command:** use the small `pego.showExplanation` command handled by the VS Code extension. A portable protocol-only explanation request is another option, but would add a custom request method and editor UI contract.
- No grammar-changing action is approved by this design until each transformation has a check-specific proof and review.
