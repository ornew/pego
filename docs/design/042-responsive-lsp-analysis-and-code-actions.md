# 042. Responsive LSP Analysis and Code Actions

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Extend `pego lsp` to publish grammar lint findings, offer version-checked explanations and safe quick fixes, and keep analysis responsive while a document changes. The proposed protocol keeps the existing push-diagnostics model and adds cancellable, coalesced analysis snapshots. The complete responsive protocol remains proposed. An opt-in protocol prototype provides lint diagnostics and guarded explanations/suppression, while documentation format negotiation applies to ordinary requests. The opt-in actor now connects analysis and request workers to protocol input, cancellation and publication; ordinary clients retain the existing synchronous path. Intra-phase cancellation and real-editor validation remain open.

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
showing the message. Suppression additionally requires
`workspace.workspaceEdit.documentChanges: true`. The server returns a
versioned edit only after recompiling and linting the candidate source, proving
the targeted finding disappeared, and checking that the inserted directive is
valid. It inserts a standalone `lint:ignore` comment immediately before the
finding line and uses the document's newline convention. The client validates
the action's URI, version, generation, session, and exact approved insertion
before applying it. The title states that the finding line is suppressed: the
inserted directive covers its own comment line and the immediately following
finding line. Insertions before declarations, inside multiline tokens, or on
lines with existing ignore directives are withheld; rule-wide suppression
needs an explicitly scoped action. There are no suggested grammar
transformations and no alternative-deletion action.

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

`analyzeContext` checks cancellation between phases and during token-index
construction. Suppression validation uses the request context before and after
candidate analysis and lint. Syntax tokenization/parsing, compilation, lint and
some existing request traversal/formatting routines still lack intra-phase
polling. Cancellation responds promptly through the actor and suppresses stale
results, but worker shutdown latency remains bounded by those phases. The VS
Code extension handles the explanation command; real-editor validation and
resource measurements remain open.

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

Design 017 deliberately has no code actions and always returns Markdown for hover and completion documentation. The editor-support follow-up also asks the server to honor plain-text documentation formats. A document version is already attached to pushed diagnostics, but the request handlers do not capture and validate a versioned snapshot when returning edits.

## Goals

- Publish syntax/compile diagnostics as today and add lint diagnostics after successful compilation, preserving lint check IDs, messages, severities, positions and source suppression.
- Explain a lint finding and offer suppression or a reviewed, semantics-preserving quick fix without treating `Finding.Fix` as replacement text.
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
- **Suppress finding** adds `// lint:ignore <check>` on the finding line, using the document's line ending. If a directive already on that line covers the check, do not offer a duplicate. Use the narrowest valid comment placement under the existing line/next-line or rule directive semantics. A directive may suppress other findings with the same check in that scope; show that scope explicitly. Do not use file-wide suppression by default. Offer the action only when the edited grammar remains valid and lint confirms its documented suppression scope.
- **Apply suggested fix** is offered only when the server has a check-specific, structured edit implementation and tests proving the edit preserves the accepted language and surrounding source. The string in `Finding.Fix` remains display-only. Do not offer removal of a shadowed alternative, including left-recursive alternatives, while its semantic interaction is unresolved.

Use `WorkspaceEdit.documentChanges` with `TextDocumentEdit` and a `VersionedTextDocumentIdentifier` for every changing action. Do not return an unversioned `changes` fallback for edits: a client that cannot apply versioned document changes can still use the explanation action and edit the suppression comment itself. Return no stale action if the document version changes while the request is being handled. Revalidate the finding key and version immediately before producing the response; after the editor applies an edit, normal `didChange` analysis establishes the resulting state.

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
2. Add structured code-action protocol models, versioned suppression edits, check explanations, and a VS Code command handler. Keep suggested grammar edits behind a separately reviewed allowlist.
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

No latency or resource measurements are claimed. Measure `BenchmarkAnalyze` and separate tokenization, parse, compile, lint and queue/publication costs on small examples, the 500–560-line Go/Python examples used by design 017, and large generated grammars. Compare synchronous analysis, diagnostics with lint, and coalesced analysis with one worker per document. Record cancellation-to-stop latency, queued/running work, allocations and peak retained snapshots under rapid edits. Report warmup, Go version, host, repetitions and raw results; do not add a debounce delay or claim responsiveness from a benchmark alone.

## Limitations and Open Questions

- The responsive actor remains opt-in and needs real-editor validation; ordinary clients retain the synchronous path.
- Cooperative cancellation needs checks in syntax and engine compilation/lint loops; until those are added, those phases can only be cancelled before or after the phase.
- A server-command explanation depends on a client handler. The VS Code extension is the first planned client; the diagnostic itself remains understandable without the action.
- The exact work bound and worker-count default should be selected from measurements on large grammars and multi-document editor workloads.
- **Proposed lint scope:** enable enabling all existing checks for every compilable open grammar, with source directives as the suppression mechanism. An owner may instead choose a configurable lint set if editor noise is observed; no such setting is part of this proposal.
- **Proposed explanation command:** use the small `pego.showExplanation` command handled by the VS Code extension. A portable protocol-only explanation request is another option, but would add a custom request method and editor UI contract.
- No grammar-changing action is approved by this design until each transformation has a check-specific proof and review.
