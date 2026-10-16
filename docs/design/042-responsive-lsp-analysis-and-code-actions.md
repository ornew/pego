# 042. Responsive LSP Analysis and Code Actions

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-11

## Summary

Extend `pego lsp` to publish grammar lint findings, offer version-checked explanations and safe quick fixes, and keep analysis responsive while a document changes. The proposed protocol keeps the existing push-diagnostics model and adds cancellable, coalesced analysis snapshots. The complete responsive protocol remains proposed. An opt-in prototype provides lint diagnostics and guarded explanations; the server also returns versioned suppression edits to clients that advertise the required workspace-edit capability. The VS Code client currently filters those edits because its `WorkspaceEdit` conversion drops the document version and `workspace.applyEdit` cannot enforce it atomically. Documentation format negotiation applies to ordinary requests. The opt-in actor connects analysis and request workers to protocol input, cancellation and publication; ordinary clients retain the existing synchronous path. Bounded cancellation covers text-index construction, lexical tokenization, syntax parsing, grammar AST validation, selected compiler rule/dependency/matcher/type traversals, shared grammar-success proofs, attribute normalization, context-aware grammar metadata collection, selected lint metadata traversals, class-descriptor/range analysis, context-aware grammar and LSP formatting, and semantic-token classification and encoding. Other shared operations and large primitives remain partly unpolled, and real-editor validation remains open.

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
grammar AST validation, rule/dependency analysis, matcher compilation and type
traversal. The LSP compile path also passes its control through attribute and
pragma normalization. That traversal polls while visiting arguments, keyword
arguments, attributes and fields, and while copying copy-on-write structures in
chunks of at most 256 entries; shared type/action-group lowering callbacks,
including array copies, use the same controlled path. Cancellation discards
partial normalized output. Ordinary `Compile` retains its bulk-copy path and
existing API. Context-aware validation polls during frame processing, child
enqueueing, character-range checks in chunks of at most 256 ranges, and
attribute argument collection, returning no partial diagnostic on
cancellation. Ordinary validation retains its previous result and ordering and
does not poll per character range. Shared grammar-analysis success/failure/cut
proofs now poll during recursive traversal and fixed-point rule visits. The
grammar package also offers context-aware `Rules`, `Types`, `AllComments` and
`Attribute.ExpressionArgs` collection. Controlled compiler and lint paths use
the rule and type collectors; lint also uses the comment collector. Nested
attribute-argument traversal shares the invocation's poll helper, while the
public `Attribute.ExpressionArgsContext` supports standalone collection.
These paths poll while scanning definitions, traversing comments and
arguments, and sorting keyword names, and return no partial slice on
cancellation. Successful collection preserves existing ordering and AST
pointer identity without mutating the grammar. Lint also polls while unwrapping
groups and neutral wrappers, traversing attributes and grouped types,
extracting visibility and positions, comparing expression structure, and
scanning captures, predicates, index use, position reads and repetition
metadata. Compiler class analysis polls descriptor construction, range copies,
ASCII bitmap visits and first-terminal analysis in bounded chunks of at most
256 visits. Lint polls range membership and range normalization, sorting,
merging and equality scans. The context is used only during the invocation and
is not retained; ordinary helpers and parser runtimes keep their existing
paths. Context-aware grammar formatting and LSP formatting poll during AST
traversal, layout and keyword ordering, output construction, newline conversion,
and minimal-edit scans. Document-symbol, hover and completion rendering also
uses an explicit control local to each request; it is not stored on the shared
analysis. Polls cover token, capture, declaration, field and documentation
walks, as well as sorting and chunked documentation splitting, joining and
copying. Grammar text uses `FormatContext`; inferred rule-type text uses the
internal `RuleTypeWithPoll`, while ordinary `RuleType` and string rendering
remain unchanged. Cancellation discards the response and returns JSON-RPC
`RequestCancelled` (-32800) at the request boundary; unrelated panics propagate
unchanged. Individual string comparisons, allocations and concatenations,
standard-library regexp replacement/matching, JSON wire encoding and other
large operations remain indivisible or outside this control. These checks
bound work at selected points, not total LSP cancellation latency. Suppression
validation uses its request context before and after candidate analysis and
lint. The VS Code
explanation-command handler is implemented. Real-editor validation and
full-phase resource measurements remain open; safe suppression application
also remains open because the client API cannot atomically enforce the document
version.

References, document highlights, `prepareRename` and `rename` reuse the same
request-local control. Polls cover full occurrence collection, unused-name
scans (`checkUnused`), long new-name identifier checks and the loops that build
reference, highlight and rename results. Successful responses preserve
declaration inclusion, source order, highlight kinds, exact rename error
messages and null-versus-array shapes. Cancellation returns a `nil` result with
JSON-RPC `RequestCancelled` (-32800), so no partial locations, highlights or
workspace edits escape. Foreign panic identity and actor stale
`ContentModified` handling are unchanged.

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
entry point and these workloads only. This comparison predates the later
parser-, compiler- and lint-polling slices described and measured separately
below.

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

The lint-enabled analysis worker now carries its context through linting and
diagnostic conversion. It publishes only a complete payload after the actor
confirms the URI, version and generation still match; cancellation discards
partial lint results and diagnostics. Polling occurs at lint-owned traversal
boundaries and periodically while converting findings, but some helpers have
coarser polling, so this does not establish a hard cancellation-latency bound.

A five-pair, 300 ms alternating comparison of the lint-enabled analysis path
used Go 1.27.1 on an Apple M3 Max (darwin/arm64), `GOMAXPROCS=4`, `GOGC=100`,
and `GOMEMLIMIT` off, comparing main `4f84b74` with the lint-context candidate.
Candidate/base median time ratios (with the paired range) were:

| Input | Analyze | Lint diagnostics |
| --- | ---: | ---: |
| Generated fixture | 0.913 (0.859–1.015) | 0.973 (0.914–1.033) |
| Go | 0.950 (0.881–1.046) | 1.002 (0.932–1.086) |
| Python | 0.958 (0.798–1.049) | 0.984 (0.870–1.048) |

Allocation counts were unchanged or lower by at most two per operation in the
medians, and bytes/op were effectively unchanged. The paired ranges are wide
and cross parity; these measurements show no stable ordinary-time change. They
do not measure actor or JSON-RPC latency, real-editor behavior, cancellation
while a lint helper is active, or retained heap.

A separate five-pair alternating comparison measured ordinary validation and
LSP analysis after adding cancellable grammar validation, against main
`87a4088` on Go 1.27.1, Apple M3 Max (darwin/arm64),
`GOMAXPROCS=4`, `GOGC=100`, with `GOMEMLIMIT` off. Validation subbenchmarks ran
for 500 ms and existing LSP Analyze subbenchmarks for 300 ms. The following
are medians of paired candidate/base time ratios, followed by the observed
paired range (they are not quotients of aggregate medians):

| Case | Candidate/base time ratio |
| --- | ---: |
| Validate: Small | 1.038 (1.009–1.364) |
| Validate: WideRules | 1.027 (0.957–1.076) |
| Validate: DeepOptional | 1.003 (0.898–1.025) |
| Validate: AttributeKeys | 0.994 (0.840–1.016) |
| Validate: CharacterRanges | 1.000 (0.945–1.006) |
| LSP Analyze: generated | 1.009 (0.974–1.039) |
| LSP Analyze: Go | 0.992 (0.909–1.121) |
| LSP Analyze: Python | 1.043 (0.924–1.161) |

The Small validation median was 533.4→547.3 ns/op with no allocation change;
other allocation counts were effectively unchanged, except for one additional
allocation in the Go Analyze case. The larger validation fixtures and all LSP
cases have paired ranges crossing parity, so they do not establish a stable
time change. Sources are `grammar/validate_bench_test.go` and
`internal/lsp/responsive_bench_test.go`. These measurements do not cover the
cancellable-context path, cancellation latency or input parsing.

A focused end-to-end compilation comparison of the normalization change used
already-parsed ASTs, excluding source parsing. It ran on Go 1.27.1 on an Apple
M3 Max (darwin/arm64), `GOMAXPROCS=1`, `GOGC=100`, `GOMEMLIMIT` off, with five
alternating 300 ms pairs against baseline `3f46735`. The source-grammar fixture
was identical between baseline and candidate; the attribute-heavy fixture had
64 `@error` annotations. The table gives median paired candidate/base time
ratios:

| Fixture | Ordinary Compile | Context-aware Compile |
|---|---:|---:|
| JSON grammar | 1.0032 | 1.0023 |
| Source grammar | 1.0028 | 1.0063 |
| 64 `@error` annotations | 1.0041 | 0.9725 |

Ordinary allocation counts were unchanged. Context-aware JSON and source cases
added 16 B/op and one allocation each; the attribute-heavy case changed from
104,784 B/op and 548 allocations to 94,936 B/op and 524 allocations, including
copy-on-write append work. Timings were noisy: one ordinary source pair was
12.3% slower while other ordinary pairs were within 0–1.8%; these results do
not establish a speed gain. This measures full compilation of parsed ASTs, not
a normalization-only operation, runtime parsing or cancellation latency.

### Shared Proof Cancellation

A separate comparison measured full `Compile`/`CompileContext` and
`Run`/`RunContext` calls on already-parsed JSON, source-grammar, and
64-`@error`-annotation ASTs. It used Go 1.27.1 on an Apple M3 Max
(darwin/arm64), `GOMAXPROCS=1`, `GOGC=100`, with no `GOMEMLIMIT`, and five
alternating 200 ms pairs against main `74d6a8c`. The baseline used the same lint
benchmark driver through a test-only Go overlay. Values are paired-median
candidate/base time ratios:

| Fixture | `Compile` | `CompileContext` | `Run` | `RunContext` |
|---|---:|---:|---:|---:|
| JSON grammar | 1.014 | 1.001 | 1.010 | 1.008 |
| 64 `@error` annotations | 1.000 | 1.002 | 0.999 | 0.996 |
| Source grammar* | 0.997* | 0.966* | 1.002† | 1.012† |

The source-grammar compile measurements marked `*` came from an additional
five-pair rerun with a fixed 100-iteration count; the original variable-count
medians were 1.013 for `Compile` and 1.022 for `CompileContext`. The lint values
marked `†` are from that original run. Individual ratios ranged from 0.889 to
1.248, so these measurements show neither a speedup nor a stable regression.
Allocation counts were unchanged in the other fixtures. In the fixed-count
source case they were 31,216 for ordinary compilation and 31,181–31,182 for
context-aware compilation; B/op differences were at most 28 bytes out of about
3.6 MB. These are whole compile/lint calls, not proof-only cost, parser runtime,
cancellation latency, peak memory, or real-editor behavior. Sources are
`internal/engine/normalize_context_bench_test.go` and
`internal/lint/context_bench_test.go`.

### Lint Metadata Cancellation

Five alternating 200 ms pairs compared the final lint-metadata traversal with
main `43b66f6`, using the same `BenchmarkSuccessProofLintCancellation` cases
and already-parsed JSON, source-grammar, and 64-`@error`-annotation ASTs. The
environment was Go 1.27.1 on an Apple M3 Max (darwin/arm64),
`GOMAXPROCS=1`, `GOGC=100`, with no `GOMEMLIMIT`. Values are paired-median
candidate/base time ratios for the whole lint call:

| Fixture | `Run` | `RunContext` |
|---|---:|---:|
| JSON grammar | 0.991 | 1.021 |
| Source grammar | 1.002 | 1.010 |
| 64 `@error` annotations | 1.037 | 1.040 |

Allocation counts matched exactly. B/op matched except for source-grammar
variation of at most 5 bytes out of about 952 KB. Individual paired time
ratios ranged from 0.914 to 1.074. Cancellable annotation and source-grammar
calls were slower in every pair; treat their 4.0% and 1.0% median increases
as measured overhead. Other cases had both faster and slower pairs. These
short runs measure whole `Run`/`RunContext` calls, not compiler or parser
runtime, cancellation latency, peak memory, or real-editor behavior.

### Shared Grammar Metadata Collection

Five alternating 200 ms pairs compared main `ace727f` with the final shared
metadata collectors, using already-parsed JSON, source-grammar, and 64-
`@error`-annotation ASTs. Whole `Compile`/`CompileContext` and `Run`/`RunContext`
calls were measured with Go 1.27.1 on an Apple M3 Max (darwin/arm64),
`GOMAXPROCS=1`, `GOGC=100`, and no `GOMEMLIMIT`. Values are paired-median
candidate/base time ratios, with the range across the five pairs:

| Fixture | `Compile` | `CompileContext` | `Run` | `RunContext` |
|---|---:|---:|---:|---:|
| JSON grammar | 1.022 (0.938–1.047) | 1.028 (1.015–1.142) | 1.004 (0.948–1.022) | 1.039 (1.011–1.045) |
| Source grammar | 1.012 (1.001–1.020) | 1.006 (0.995–1.020) | 1.002 (0.939–1.010) | 1.019 (0.986–1.032) |
| 64 `@error` annotations | 0.997 (0.990–1.006) | 1.023 (0.963–1.076) | 0.995 (0.989–1.053) | 1.110 (1.045–1.134) |

Allocation counts were unchanged for JSON and annotated grammars. For source
grammars, ordinary compilation varied by −3 to +7 allocations and −206 to
+410 B/op; context compilation varied by −1 to +1 allocation and −1 to +20
B/op. Lint allocations were unchanged except source-grammar `RunContext`,
which added one allocation and 6–9 B/op; ordinary source-grammar lint varied
by −4 to 0 B/op. Context-aware JSON compilation and lint were slower in every
pair, with median increases of 2.8% and 3.9%. Context-aware annotated-grammar
lint and ordinary source-grammar compilation were also slower in every pair,
with median increases of 11.0% and 1.2%. Other cases included faster and slower
pairs. These results describe whole compiler
and lint calls, not metadata-only work, parser execution, cancellation
latency, peak memory, or real-editor behavior. No stable performance
improvement is established.

Controlled compiler and lint traversals pass their invocation poll helper
through nested metadata collection. The public grammar API separately exposes
`Attribute.ExpressionArgsContext` for callers collecting expression
arguments directly.

### Character-Class Analysis Cancellation

Five alternating 200 ms pairs compared main `4bbbb7e` with the class-analysis
change. Go 1.27.1 on an Apple M3 Max (darwin/arm64), `GOMAXPROCS=1`,
`GOGC=100`, and no `GOMEMLIMIT` were used. Measurements cover whole
`Compile`/`CompileContext` and `Run`/`RunContext` calls on already-parsed JSON,
source-grammar, 64-`@error`-annotation, and 512-range class fixtures. Entries
show paired-median candidate/base time ratios and the range across pairs:

| Fixture | `Compile` | `CompileContext` | `Run` | `RunContext` |
|---|---:|---:|---:|---:|
| JSON grammar | 0.985 (0.944–1.005) | 1.010 (0.889–1.018) | 1.009 (0.989–1.031) | 1.000 (0.997–1.006) |
| Source grammar | 1.000 (0.966–1.002) | 1.002 (0.990–1.026) | 0.999 (0.987–1.079) | 1.015 (0.960–1.116) |
| 64 `@error` annotations | 1.002 (0.989–1.010) | 0.990 (0.983–1.003) | 1.035 (0.998–1.169) | 1.018 (0.990–1.042) |
| 512-range class | 1.004 (0.985–1.040) | 1.225 (1.219–1.251) | 0.994 (0.963–1.024) | 1.021 (1.014–1.044) |

The 512-range context compile and lint cases were slower in all five pairs,
with median increases of 22.5% and 2.1%. All other profiles crossed parity.
Context lint reduced the JSON case by 9 allocations and 144 B/op, and the
source case by 14 allocations and 292–295 B/op. Other allocation counts
matched except for source-grammar compile variation (ordinary −1 to +3,
context −1 to 0); B/op varied by −64 to +182 for ordinary and −4 to 0 for
context compilation. Other B/op differences were at most 4 bytes. The
benchmarks use the existing compiler/lint drivers and
`internal/engine/class_context_bench_test.go` plus
`internal/lint/class_context_bench_test.go`; baseline overlays add the same
drivers. These results measure whole compile/lint calls, not parser execution,
cancellation latency, peak memory or real-editor behavior. They do not support
a general speedup or regression claim.

### Grammar and LSP Formatting Cancellation

Five alternating 200 ms pairs compared main `4e21b0e` with the formatting
cancellation change, using Go 1.27.1 on an Apple M3 Max (darwin/arm64),
`GOMAXPROCS=1`, `GOGC=100`, and no `GOMEMLIMIT`. Inputs were parsed or
analyzed before timed calls. The table shows paired-median candidate/base time
ratios and the range across pairs. `ordinary` uses the existing non-context
path; `context` uses the cooperative path. No cancellation was triggered
during these successful calls.

| Operation / fixture | Ordinary | Context |
|---|---:|---:|
| Grammar formatting — JSON | 1.076 (1.064–1.121) | 1.175 (1.153–1.191) |
| Grammar formatting — source grammar | 1.052 (1.009–1.056) | 1.122 (1.111–1.125) |
| Grammar formatting — 8 KiB literal | 0.928 (0.889–0.940) | 1.017 (0.950–1.045) |
| Lint — JSON | 0.990 (0.945–1.087) | 1.009 (0.958–1.029) |
| Lint — source grammar | 1.006 (0.958–1.011) | 0.996 (0.871–1.031) |
| Lint — 64 `@error` annotations | 0.989 (0.975–1.007) | 0.997 (0.992–1.017) |
| Lint — 32 predicates | 1.004 (0.871–1.014) | 1.095 (1.080–1.099) |
| Lint — long diagnostic | 0.790 (0.765–0.827) | 0.867 (0.861–0.907) |
| LSP formatting — JSON | 1.083 (1.046–1.095) | 1.175 (1.162–1.238) |
| LSP formatting — source grammar | 1.030 (0.961–1.078) | 1.105 (1.055–1.106) |
| LSP formatting — 8 KiB literal with CRLF | 0.913 (0.897–0.950) | 0.976 (0.957–1.010) |

The baseline had no context-aware formatter, so its synthetic context-format
wrapper called the existing unpolled formatter; the existing lint
`RunContext` path was unchanged. Allocation changes were concentrated in
formatter calls and synthetic lint fixtures. Context formatting added one allocation and 24 B/op for JSON and
two allocations and 4,120 B/op for the long literal; the LSP long-literal path
added three allocations and 4,136 B/op. Source-grammar formatting used 47–48
fewer allocations and about 0.75 KB fewer bytes/op. Context lint on 32
predicates added 93 allocations and 2,232 B/op; long-diagnostic lint used
about 52% fewer bytes/op. Other lint fixtures were effectively unchanged.
These focused measurements cover successful formatter and lint calls, not
parsing, editor interaction, cancellation latency or peak memory. Benchmark
drivers are `grammar/format_context_bench_test.go`,
`internal/lint/format_context_bench_test.go`, the existing
`internal/lint/context_bench_test.go`, and
`internal/lsp/format_context_bench_test.go`; the same drivers were applied to
the baseline. Raw results remain outside the repository.

### LSP Symbol, Hover, and Completion Rendering

Five serial alternating 200 ms pairs compared main `bca31b7` with the render
cancellation change on Go 1.27.1, Apple M3 Max (darwin/arm64),
`GOMAXPROCS=1`, `GOGC=100`, with no `GOMEMLIMIT`. Inputs were analyzed before
the timed request-rendering calls. The table shows paired-median candidate/main
time ratios and the range across pairs for rich-client responses: hierarchical
document symbols, Markdown hover, and snippet-capable completion. The same
matrix also measured flat symbols, plaintext hover, and completion without
snippet support. The baseline handlers use the existing unpolled response
builders even when a request context is present; `ordinary` uses no request
context, while `context-aware` uses a live cancellable context. No cancellation
was triggered in either measured path.

| Fixture | Response | Ordinary | Context-aware |
|---|---|---:|---:|
| JSON grammar | Document symbols | 1.009 (0.991–1.052) | 1.065 (0.966–1.130) |
| JSON grammar | Hover | 1.014 (0.965–1.019) | 1.117 (1.050–1.172) |
| JSON grammar | Completion | 1.126 (1.108–1.318) | 1.352 (1.332–1.419) |
| Source grammar | Document symbols | 1.019 (1.006–1.025) | 1.070 (1.063–1.077) |
| Source grammar | Hover | 0.997 (0.925–1.023) | 1.066 (0.907–1.094) |
| Source grammar | Completion | 1.161 (1.150–1.243) | 1.435 (1.350–1.541) |
| 512-field fixture | Document symbols | 1.005 (0.747–1.099) | 1.089 (0.942–1.131) |
| 512-field fixture | Hover | 1.012 (0.945–1.034) | 1.228 (1.178–1.261) |
| 512-field fixture | Completion | 1.061 (1.044–1.094) | 1.384 (1.355–1.413) |

The 18 historical response fingerprints matched exactly. The plain/flat
variants showed the same broad pattern, with context-aware completion ratios
of 1.323–1.408, document-symbol ratios of 1.066–1.088, and hover ratios of
1.137–1.225 across these fixtures. In the rich 512-field responses, context
handling increased completion from 525 to 1,038 allocations and 102,334 to
114,647 B/op, and document symbols from 531 to 1,044 allocations and 203,956
to 216,269 B/op. Source-grammar completion instead used 674 to 661
allocations. These are focused synthetic rendering measurements, not a claim
of zero overhead or a general speed result. They exclude analysis, parsing,
wire encoding, cancellation latency, real-editor behavior, and peak memory.
The benchmark is `internal/lsp/render_context_bench_test.go`; raw results are
outside the repository.

### Occurrence Request Cancellation

Five alternating 200 ms pairs compared main `fa2917c` with the occurrence
cancellation change on Go 1.27.1, Apple M3 Max (darwin/arm64),
`GOMAXPROCS=1`, `GOGC=100`, and no `GOMEMLIMIT`. The 20 profiles cover 32 or
4,096 references, plus a 64 KiB new identifier for rename, across references,
document highlights, `prepareRename`, and rename. Inputs were analyzed before
timing; request decoding, occurrence lookup and response construction were
inside the timed call. The table shows paired-median candidate/main time ratios
and ranges across pairs. The baseline used the exact parent `features.go` with
the new controlled wrappers omitted, under the same independent harness. Its
handlers use unpolled response builders even when given a context; the
candidate context path used a live context, with no cancellation triggered.
The source fixture included CRLF and a Unicode comment; the long-name case used
a 64 KiB ASCII identifier.

| Fixture | Response | No context | Cancellable context |
|---|---|---:|---:|
| 32 references | References | 1.107 (0.977–1.149) | 1.108 (1.085–1.200) |
| 32 references | Document highlights | 1.121 (1.103–1.133) | 1.111 (1.100–1.143) |
| 32 references | `prepareRename` | 1.039 (1.009–1.043) | 1.085 (1.057–1.095) |
| 32 references | Rename | 1.114 (1.060–1.127) | 1.130 (1.119–1.163) |
| 32 references, 64 KiB name | Rename | 1.009 (1.006–1.092) | 1.646 (1.542–1.662) |
| 4,096 references | References | 1.106 (1.084–1.111) | 1.108 (1.082–1.120) |
| 4,096 references | Document highlights | 1.100 (1.044–1.126) | 1.112 (1.060–1.130) |
| 4,096 references | `prepareRename` | 1.051 (0.988–1.112) | 1.069 (1.031–1.096) |
| 4,096 references | Rename | 1.103 (1.096–1.110) | 1.115 (1.004–1.128) |
| 4,096 references, 64 KiB name | Rename | 1.089 (1.069–1.100) | 1.167 (1.161–1.181) |

The 46 historical response and error fingerprints matched exactly. Active
requests generally added one allocation; the 32-reference cases added 24
B/op, and the 4,096-reference cases added 4–48 B/op. The 64 KiB name case
was about 65% slower with 32 references and 17% slower with 4,096; its
identifier scan is now cancellable. These focused successful-response
measurements exclude grammar analysis, parser execution, wire encoding,
cancellation latency, peak memory, and real-editor behavior; request decoding,
occurrence lookup and response construction are included. They are not a
general performance result. The shared harness is
`internal/lsp/occurrence_context_bench_test.go`; raw results remain outside
the repository.

### Semantic-Token Request Cancellation

`textDocument/semanticTokens/full` uses the request-local render control while
classifying tokens and constructing the response. Polls cover token and
occurrence-index traversal, operator-body context scans, multiline token
splitting, newline trimming, and appending the five-integer LSP token records.
The response preserves token order, modifiers, CRLF handling, UTF-16 positions,
and the existing empty `data: []` result. Cancellation returns no partial data
and the existing `RequestCancelled` error; actor stale-request handling and
foreign-panic behavior are unchanged. The control and output are invocation
local.

Position lookup still uses the existing bounded index conversions. JSON
decoding, individual string comparisons, allocation, and wire encoding remain
indivisible; this adds cooperative cancellation points but no wall-clock bound.

Five alternating 200 ms pairs compared the change with main `651deef` on Go
1.27.1, Apple M3 Max (darwin/arm64), `GOMAXPROCS=1`, `GOGC=100`, and no
`GOMEMLIMIT`. The ten profiles used pre-analyzed JSON, dense-token, source-
grammar, comment, and incomplete-operators documents; they measured
semantic-token request handling, excluding grammar analysis, parser execution
and wire encoding. The baseline used the exact parent semantic implementation
with controlled wrappers and tests omitted. No-context runs used a nil
context; the baseline did not poll even when given a live context, and
candidate live contexts were not canceled. Ratios are paired-median
candidate/baseline times with ranges across pairs.

| Fixture | No context | Live context |
|---|---:|---:|
| JSON grammar | 0.877 (0.677–0.901) | 0.920 (0.820–0.937) |
| Dense 2,048-reference input | 0.899 (0.891–0.948) | 0.931 (0.891–0.937) |
| Source grammar | 0.931 (0.751–0.942) | 0.966 (0.917–1.024) |
| CRLF comment, 4,099 indexed lines | 0.915 (0.854–0.932) | 0.953 (0.943–0.966) |
| Incomplete operators, 128 entries | 0.991 (0.973–1.023) | 1.289 (1.142–1.303) |

Ordinary medians were 1–12% lower, but these measurements do not establish an
optimization; layout and timing noise are plausible, and the source-grammar
live-context range crosses parity. The incomplete-operators live-context case
was about 29% slower, with every pair slower. Ordinary B/op differed by at
most 13 bytes and allocation counts were unchanged. Live-context cases added
24 B/op and one allocation each. The comment fixture had 4,099 indexed lines
(4,096 repeated lines plus the trailing empty line) and 2,050 nonempty comment
records; `comment4096` is the fixture label. Fifteen historical response and
error fingerprints matched exactly. The harness is
`internal/lsp/semantic_context_bench_test.go`; raw results remain outside the
repository.

### Code-Action Request Cancellation

Code-action construction uses the request-local render control while filtering
`context.only`, matching supplied diagnostics, building lint-check explanations,
and scanning tokens for suppression edits. Existing context checks in lint
analysis, per-diagnostic context checks and the 256-item finding/definition
scans remain in place. A final
control check immediately after suppression analysis and another at request
completion prevent cancellation from being reported as an ordinary unavailable
edit or publishing a partial action list.

The response retains the existing capability filters, diagnostic URI, version,
generation, key, range, code, message and source matching, action order,
versioned edits, and final document-version guard. Cancellation returns no
partial actions and `RequestCancelled`; stale actor requests and foreign panics
keep their existing behavior. The control is request-local. JSON decoding,
`strings.Contains`, candidate-string construction, individual comparisons,
formatting, allocations, and wire encoding remain indivisible, so this does not
provide a wall-clock bound.

Five alternating 200 ms pairs compared the code-action request path with parent
`4c744fd` on Go 1.27.1, Apple M3 Max (darwin/arm64), `GOMAXPROCS=1`,
`GOGC=100`, and no `GOMEMLIMIT`. The eight workloads include request JSON
decoding, cached lint-result mapping and response construction, plus
suppression-candidate analysis and lint where applicable; initial snapshot
analysis, standalone parser throughput, wire encoding and editor latency are
excluded. The baseline already had per-diagnostic context checks, 256-item
finding/definition checks, and controlled candidate analysis/lint, but lacked
the new inner-walk and request-envelope checks. Ordinary runs used no request
context; cancellable runs used a live context without triggering cancellation.
Values are paired-median candidate/baseline time ratios with the range across
pairs.

| Workload | Ordinary | Live context |
|---|---:|---:|
| Disabled | 1.002 (0.957–1.016) | 1.025 (0.996–1.039) |
| Explanation | 1.016 (1.006–1.033) | 1.017 (1.011–1.066) |
| 128 findings | 1.002 (0.994–1.017) | 1.035 (1.005–1.044) |
| 4,096 `only` filters | 0.986 (0.959–1.067) | 1.030 (1.020–1.062) |
| 4,096 nonmatches plus target (4,097 supplied), match | 1.005 (0.996–1.023) | 1.000 (0.996–1.019) |
| 4,096 supplied diagnostics, miss | 1.000 (0.991–1.015) | 1.005 (0.991–1.039) |
| Suppression edit | 1.010 (0.987–1.053) | 1.017 (0.997–1.142) |
| Suppression with 512 rule references | 1.003 (0.995–1.009) | 1.001 (0.999–1.023) |

Ordinary allocation counts were unchanged and B/op differed by at most two.
Live-context runs added one allocation and 23–30 B/op (typically 24, with
rounding variation on the larger JSON workloads). These synthetic request
measurements do not quantify cancellation latency, initial snapshot analysis,
standalone parser throughput, wire encoding, retained memory or real-editor
behavior. Fifty historical response and error fingerprints matched exactly.
The existing panic-to-error conversion in candidate analysis is unchanged;
new polling rethrows unrelated panics. The harness is
`internal/lsp/code_action_context_bench_test.go`; raw results remain outside
the repository.

### LSP Analysis Metadata Cancellation

Request-local analysis carries the invocation's render control through
declaration-start indexing, collection and definition of declarations,
documentation comments, type expressions and terms, and attributes, including
expression-argument traversal. The same control covers occurrence and
diagnostic mapping, range conversion and ordering. Cancellation discards
partial analysis; controlled compilation rolls back staged program and
diagnostic results. Existing comment joining and trimming use their controlled
helpers, while ordinary background paths retain their existing behavior.
Allocations, map operations and individual string comparisons remain
indivisible, so polling does not establish a wall-clock bound.

Five alternating 200 ms pairs compared main `d7fd2ab` and the metadata
cancellation change on Go 1.27.1, Apple M3 Max (darwin/arm64),
`GOMAXPROCS=1`, `GOGC=100`, and no `GOMEMLIMIT`. The full stage includes
text-index and declaration-index construction, tokenization, parsing,
compilation and diagnostic handling. The metadata-only stage starts from
prepared immutable syntax and token indexes and measures declaration indexing
and collection, including documentation and occurrence sorting; it excludes
parsing, compilation and diagnostic mapping. Ordinary runs use no control;
live-context runs use a non-canceled context. The parent already polls
parser/compiler phases; its LSP metadata stage checks cancellation at
boundaries but lacks inner traversal polls. Values are paired-median
candidate/base time ratios with ranges across pairs.

| Stage | Fixture | Ordinary | Live context |
|---|---|---:|---:|
| Full | JSON | 1.003 (0.970–1.025) | 1.003 (0.986–1.022) |
| Full | Source grammar | 0.978 (0.945–1.014) | 1.007 (0.953–1.031) |
| Full | 512 wide rules | 1.004 (0.926–1.056) | 1.011 (0.950–1.050) |
| Full | 512 rules plus partial rule | 0.988 (0.983–1.001) | 1.004 (0.993–1.017) |
| Full | 512 compile errors | 1.007 (0.975–1.111) | 1.022 (1.004–1.066) |
| Full | 512 documentation lines | 0.996 (0.989–1.013) | 0.992 (0.964–1.026) |
| Metadata only | JSON | 0.997 (0.976–1.019) | 1.032 (1.004–1.068) |
| Metadata only | Source grammar | 0.996 (0.948–1.032) | 1.061 (0.931–1.131) |
| Metadata only | 512 wide rules | 0.999 (0.908–1.011) | 1.028 (0.962–1.040) |
| Metadata only | 512 rules plus partial rule | 1.010 (0.993–1.064) | 1.043 (1.017–1.113) |
| Metadata only | 512 compile errors | 0.992 (0.922–1.013) | 1.027 (0.972–1.041) |
| Metadata only | 512 documentation lines | 0.999 (0.903–1.013) | 1.413 (1.382–1.442) |

Ordinary metadata-only allocation counts and B/op were unchanged; live-context
metadata-only runs added one allocation and 24 B/op. Full-path allocations
varied by up to nine allocations and 434 B/op, with no consistent change.
Most timing ranges overlap parity. For live contexts, all five pairs were slower
for full-path 512 compile errors (+2.2%, 1.004–1.066), metadata-only JSON (+3.2%,
1.004–1.068), 512 rules plus a partial rule (+4.3%, 1.017–1.113), and
512 documentation lines (+41.3%, 1.382–1.442). These measurements do not
establish a broad speedup.
Eighteen historical fingerprints and fourteen cancellation-phase controls
matched their expected results, and targeted race checks passed. The harness
is `internal/lsp/metadata_context_bench_test.go`; raw results remain outside
the repository.

### Responsive Request Resource Probe

The current actor configuration admits up to 64 logical requests with four
worker slots. A framed-protocol probe held requests at a pre-dispatch barrier
after preparing a 65,542-byte immutable analyzed source. Eight configurations
(1, 4, 16 or 64 requests; tiny hover or large code-action payload) were
measured in five counterbalanced fresh-process runs each on Go 1.27.1, Apple
M3 Max (darwin/arm64), `GOMAXPROCS=16`, `GOGC=100`, and no `GOMEMLIMIT`. The
hover body was 150 bytes; the valid stress code-action body was 1,048,807 bytes
with 65,536 `context.only` entries. The barrier prevents handler execution,
so this measures retained request/session state rather than handler load. This
is a configuration probe, not a before/after comparison.

The table reports post-GC `HeapAlloc` delta from the prepared idle session,
in KiB as median (range), while requests are held and after all request workers
join. The 64-request held sample is taken after the 65th-request rejection;
other held samples follow the admission-sequence acknowledgement. Goroutine
deltas are shown in parentheses. Heap and cumulative-allocation figures are
whole-process client, server, session and framing state, not attribution solely
to retained requests or handler work.

| In flight | Payload | Held heap Δ KiB (goroutines) | Joined heap Δ KiB (goroutines) |
|---:|---|---:|---:|
| 1 | Tiny hover | 11.8 (11.4–12.1) (+1) | 13.2 (12.5–18.3) (0) |
| 1 | Large code action | 2,135.3 (1,047.1–2,140.5) (+1) | 16.2 (11.4–17.2) (0) |
| 4 | Tiny hover | 27.2 (22.0–28.3) (+4) | 30.3 (25.3–32.2) (0) |
| 4 | Large code action | 4,153.7 (4,149.0–5,246.8) (+4) | 34.7 (25.1–45.1) (0) |
| 16 | Tiny hover | 55.0 (48.8–55.3) (+16) | 41.3 (36.0–63.7) (0) |
| 16 | Large code action | 17,656.7 (16,573.9–17,662.8) (+16) | 52.1 (41.2–61.5) (0) |
| 64 | Tiny hover | 141.8 (140.6–152.2) (+64) | 80.7 (80.7–90.2) (0) |
| 64 | Large code action | 68,377.2 (67,286.5–69,472.2) (+64) | 98.4 (91.3–116.6) (0) |

At 64 admitted requests, a 65th received `ServerCancelled`. Every admitted
request received one cancellation response; queued replies drained, all
workers joined, a normal retry succeeded, and shutdown completed. These checks
do not establish a physical goroutine ceiling during cancellation churn: the
limit counts logical admitted requests, and requests were held before handler
dispatch. Forced-GC heap deltas include runtime, pool and short-lived join-probe
state and are not a leak test. Cumulative `TotalAlloc` includes client and
server transport activity, so it is not a per-request allocation rate. This
probe does not measure peak RSS, handler throughput, natural cancellation
latency, or real-editor behavior, and does not justify changing defaults. The
harness is `internal/lsp/responsive_resources_test.go`; raw results remain
outside the repository.

### Bounded Request Dispatch

Cancellation acknowledges a logical request before its handler necessarily
returns. With four canceled handlers held at an indivisible-operation barrier,
the previous implementation started workers for another 64 admitted requests.
The actor now reserves an execution slot before starting a worker;
waiting requests remain in the existing admission map without a worker or a
copied document. Every completion releases its slot, including obsolete results,
and rechecks pending requests. The four execution slots and 64 logical-request
limit remain unchanged. This does not add FIFO fairness or establish an exact
whole-process goroutine ceiling.

A comparison against `1aeff8d` used the same framed resource harness with only
`responsive.go` restored for the baseline. Five alternating fresh-process pairs
covered 1 and 64 requests, each with tiny hover or large code-action parameters
(40 processes). The environment was Go 1.27.1, Apple M3 Max (darwin/arm64),
`GOMAXPROCS=16`, `GOGC=100`, and no `GOMEMLIMIT`. Post-GC process heap deltas
include the client, framing and runtime; they are neither peak RSS nor isolated
server retention. For 64 held requests:

| Payload | Baseline Held Heap Δ KiB | Candidate Held Heap Δ KiB | Held Goroutine Δ, Baseline → Candidate |
|---|---:|---:|---:|
| Tiny hover | 141.5 (136.7–147.7) | 59.8 (58.9–65.7) | +64 → +4 |
| Large code action | 68,369.4 (67,276.9–69,454.5) | 68,291.0 (66,116.3–68,300.9) | +64 → +4 |

Heap values are medians with ranges across five samples. Large parameter bodies
still dominate retention. Both versions returned to the idle goroutine count
after cancellation and joining; this is not a leak or cancellation-latency test.
The raw measurements remain outside the repository.

The focused responsive tests, race checks and scoped LSP vet pass. Regression
cases cover held cancellation tails, obsolete completion wakeups, ID reuse,
pending cancellation, close invalidation, panic recovery and early cancellation.
Three source mutants were each rejected by these cases: skipping the pending
scan after a completion, releasing a slot only for current results, and removing
the cancellation check before the handler.

Timing used the same binaries and five alternating fresh-process pairs. At
`GOMAXPROCS=1`, `BenchmarkResponsiveDispatch` measured candidate/baseline
median ratios of 1.004 (0.935–1.068) for hover and 1.020 (0.960–1.046) for
document symbols, with equal allocations. `BenchmarkF08ProtocolCancel` is
unsuitable for duration-based runs at `GOMAXPROCS=1`: the busy synthetic handler
delays the benchmark goroutine about 24 ms per iteration outside the timed
region, while `B.Loop` sizes iteration counts from the timed region alone. Fast
early samples can therefore request about 10,000 iterations and exceed a harness
timeout without a hang. Its single-processor cancel latency also ranged from
0.87× to 3.6× between pairs, which supports no comparison. With 2,000 fixed
iterations at `GOMAXPROCS=16`, mean cancel-to-reply latency had medians of
11.6 µs (11.3–11.9) for the baseline and 11.8 µs (11.3–12.0) for the candidate;
the paired median ratio was 1.012 (0.982–1.057). These are synthetic in-process
means, not tail latencies. `BenchmarkResponsiveDispatch` measures a sequential
framed request/reply path; it does not cover concurrent editor workloads.

### Incremental Change Application

The protocol actor applies `didChange` content changes itself, and that work
cannot be cancelled. Each change applies to the result of the previous ones,
so applying them one at a time indexed and copied the whole document per
change: one notification with k changes cost O(k·n). A client can order a
multi-cursor edit's changes from the bottom of the document up, so that each
change is unaffected by the previous ones. A change whose lines and line
terminators end at or before the start of the previous pending change, or that
lies at the start of the text, is now located in the index of the text before
the pending changes, and consecutive such changes are spliced in one copy. A change in any other order first applies the pending
changes and indexes the result, as before. A differential test compares 20,000
random notifications with sequential application across all line terminators,
surrogate pairs, out-of-range and reversed ranges, and full replacements; it
rejects mutants that loosen the line condition, ignore the end line or accept
the last line.

`BenchmarkApplyChanges` applies 1,024 column-zero insertions to a 1 MiB
document. Five alternating pairs against sequential application used Go 1.27.1
on an Apple M3 Max with default `GOMAXPROCS`:

| Order | Sequential | Spliced | Allocated per notification |
|---|---:|---:|---:|
| Bottom-up | 1.71–1.80 s | 2.00–2.10 ms | 3.75 GB → 3.78 MB |
| Top-down | 1.71–1.75 s | 1.72–1.74 s | 3.75 GB → 3.75 GB |
| Single change | 1.71–1.77 ms | 1.68–1.87 ms | 3.65 MB → 3.66 MB |

Top-down notifications keep their quadratic cost; supporting them would require
mapping positions through earlier edits. The measurement covers change
application only, not the analysis that follows each version.

### Multi-Document Resource Probe

`TestDocumentResourceProbe` (run with `PEGO_LSP_DOCUMENT_PROBE=1`, a document
count in `PEGO_LSP_DOCUMENT_COUNT` and `PEGO_LSP_RESPONSIVE=0` or `1`) opens
copies of the 65,542-byte benchmark grammar under distinct URIs. It then sends
one change to every document followed by a hover on the last one, reading
replies concurrently as a client does, and finally closes them all. Three
fresh processes per configuration ran on Go 1.27.1, Apple M3 Max
(darwin/arm64), `GOMAXPROCS=16`, `GOGC=100` and no `GOMEMLIMIT`. Values are
medians, with ranges in parentheses; heap figures are post-GC process heap,
including the client, not peak heap or RSS.

| Documents | Mode | Heap per open document MiB | Heap after closing Δ MiB | Burst until all published ms | Burst until hover reply ms |
|---:|---|---:|---:|---:|---:|
| 1 | synchronous | 3.8 (3.8–3.8) | 1.2 | 8.0 (7.4–10.8) | 8.1 (7.5–11.0) |
| 1 | responsive | 3.8 (3.8–3.8) | 1.2 | 8.3 (7.7–8.7) | 8.4 (7.8–8.8) |
| 8 | synchronous | 2.8 (2.8–2.8) | 1.2 | 53.6 (53.4–53.9) | 53.6 (53.5–53.9) |
| 8 | responsive | 2.8 (2.8–2.8) | 1.2 | 52.8 (52.7–53.8) | 52.9 (52.7–53.9) |
| 64 | synchronous | 2.7 (2.7–2.7) | 1.3 | 419.2 (413.5–419.2) | 419.2 (413.6–419.3) |
| 64 | responsive | 2.7 (2.7–2.7) | 1.3 | 419.0 (416.7–425.9) | 419.1 (416.8–426.0) |
| 256 | synchronous | 2.7 (2.7–2.7) | 1.3 | 1,675.5 (1,674.4–1,681.2) | 1,675.5 (1,674.5–1,681.3) |
| 256 | responsive | 2.7 (2.7–2.7) | 1.4 | 1,663.3 (1,654.2–1,680.2) | 1,663.4 (1,654.2–1,680.3) |

Each open document retains about 43 times its source size in text, indexes
and analysis, and the server has no limit on open documents; the editor
decides how many are open. After every document closes, the heap returns to
within 1.2–1.4 MiB of the idle session regardless of the count, so closing
releases per-document state. Analysis runs one document at a time, about
6.5 ms per change of this grammar. In these measurements a hover on the last
changed document was answered only after every earlier change had been
analysed, in both modes: the synchronous server handles messages in order, and
the responsive queue analysed documents in the order they changed.

### Request Analysis Priority

A request waits for the analysis of its document's current version. The
responsive server now moves that analysis to the front of the queue when such a
request arrives; an analysis already running is not interrupted, and other
pending documents keep their order. Requests therefore no longer wait for
documents that changed earlier. A document that is both edited and queried
faster than it can be analysed can postpone the diagnostics of other
documents; it cannot postpone its own.

The probe compared the previous and the new server in three alternating
fresh-process pairs per count, in the environment above:

| Documents | Burst until hover reply ms, before → after | Burst until all published ms, before → after |
|---:|---:|---:|
| 8 | 54.6 (54.1–58.6) → 14.0 (14.0–14.6) | 54.5 (54.0–58.6) → 54.9 (54.5–55.1) |
| 64 | 423.3 (420.3–423.7) → 19.7 (19.6–19.9) | 423.2 (420.2–423.6) → 418.1 (417.1–423.2) |
| 256 | 1,672.1 (1,665.4–1,690.3) → 40.8 (40.3–40.8) | 1,672.0 (1,665.4–1,690.2) → 1,666.9 (1,665.3–1,673.6) |

The hover reply now follows the analysis already running and that of its own
document; the time to analyse every change is unchanged. The synchronous server
still handles messages in order.

### Neovim Client Validation

`TestNeovimClient` runs Neovim's built-in client, configured as in the editor
guide, against the synchronous and the responsive server. It runs only with
`PEGO_LSP_NEOVIM=1`. Neovim 0.11.1 edits a 202-line grammar with ordinary
editor commands: an insert, a range substitution, `:global` with `normal`,
a visual-block insert, joins, deletions, a single undo and redo of all of
those, multibyte insertion, `nvim_buf_set_lines`, thirty separate appends and
a twenty-line macro. After each operation, the semantic tokens of the edited
document are compared with those of a fresh copy of the buffer, which detects
any divergence of the server's text; a server that applied only the first
change of each notification failed at the undo step. Hover and formatting
requests also succeed in both modes.

Every operation except undo and redo arrived as one change per notification,
including `:global`, the macro and visual-block insertion, so none exercised
multi-change ordering. The undo and redo notifications each carried 136
changes in mixed order, which take the sequential path. No editor tested so far
sends top-down notifications. The test does not exercise cancellation, and
VS Code, Helix and Emacs remain unvalidated.

## Limitations and Open Questions

- The responsive actor remains opt-in; ordinary clients retain the synchronous path. Neovim edits keep the server text in sync in both modes, but VS Code, Helix and Emacs, and cancellation from a real client, remain unvalidated.
- Syntax parsing, grammar AST validation, selected compiler rule/dependency/matcher/type traversals, and attribute/pragma normalization poll context within phases. Normalization polls argument, keyword-argument, attribute and field traversal plus copy-on-write chunks; ordinary `Compile` keeps its existing bulk-copy path. Shared success/failure/cut proofs poll recursive expression and attribute visits and fixed-point rule passes. Context-aware metadata collectors cover rule/type enumeration and attribute expression arguments in controlled compilation, plus comments and metadata in lint. Controlled compiler and lint paths propagate their invocation poll helper through nested metadata collection; the public `Attribute.ExpressionArgsContext` API supports standalone collection. Lint also polls selected metadata traversals, including group/wrapper unwrapping, attribute/type-group visits, structural comparisons, and capture, predicate, position and repetition analysis. Compiler class analysis polls descriptor construction, range copies, ASCII bitmap visits and first-terminal selection in chunks of up to 256 visits; lint polls range membership and normalization/sort/merge/equality scans. The context is invocation-scoped, and ordinary helper/runtime paths retain their previous behavior. The grammar package provides `FormatContext`, `FormatExprContext`, `FormatTermContext` and `FormatTypeContext`; they preserve ordinary formatting output and return an empty string with `ctx.Err()` when cancelled, including cancellation detected before publishing completed output. The LSP formatting request uses this path, preserves the document newline convention, polls newline conversion and minimal-edit scans, and returns `RequestCancelled` without edits when cancelled. Document-symbol, hover, completion, references, document-highlight, prepare-rename and rename requests poll through selected render or occurrence walks and controlled formatting; cancellation returns `RequestCancelled` with no partial result and preserves unrelated panics. Semantic-token classification and encoding also poll through token/occurrence traversal and multiline output construction while preserving exact token data. Occurrence-index lookup and 64-byte checkpoint conversions remain unchanged. JSON decoding, individual string comparisons, `fmt` calls, allocations, wire encoding and other large operations remain indivisible, so cancellation has no wall-clock bound.
- Suppression application requires a client API that atomically checks the edit version. The current VS Code `WorkspaceEdit` path drops that version, so the extension withholds suppression while retaining guarded explanations.
- The exact work bound and worker-count default should be selected from measurements on large grammars and editor workloads; the multi-document probe measures retention and analysis order, not request concurrency.
- **Proposed lint scope:** enable all existing checks for every compilable open grammar, with source directives as the suppression mechanism. An owner may instead choose a configurable lint set if editor noise is observed; no such setting is part of this proposal.
- **Proposed explanation command:** use the small `pego.showExplanation` command handled by the VS Code extension. A portable protocol-only explanation request is another option, but would add a custom request method and editor UI contract.
- No grammar-changing action is approved by this design until each transformation has a check-specific proof and review.
