# Development

This document describes the repository layout and architecture, how to run tests, and the implementation status of language features. The living backlog is maintained in [Issue #1](https://github.com/ornew/pego/issues/1).

## Directory Layout

| Path | Contents |
|:--|:--|
| `pego.go`, `lint.go` | Public API of package `pego` (`ParseGrammar`, `Compile`, `CompileSource`, `Parser.Parse`, `Lint`, and others) |
| `grammar/` | Grammar AST, structural validation (`Validate`), JSON conversion (`MarshalJSON`, `UnmarshalJSON`, strict `DecodeJSON`) and formatting as PEGO source (`Format`, which preserves comments). Public package. |
| `internal/grammaranalysis/` | Conservative success and cut proofs shared by the compiler and linter |
| `internal/lint/` | Grammar linter behind `pego.Lint` and `pego lint`: shared static analysis (`analysis.go`) and checks (`checks.go`) |
| `internal/lsp/` | Language Server Protocol server for `.pego` files (`pego lsp`): JSON-RPC over standard input and output, document sync, diagnostics, formatting, navigation, hover, rename, completion, semantic tokens |
| `editors/vscode/` | VS Code extension (Node.js, not part of the Go module): language configuration, TextMate grammar (`syntaxes/pego.tmLanguage.json`) and a client that starts `pego lsp` |
| `sample/` | Generation of inputs that a grammar accepts, coverage reports, near-miss invalid inputs and a fuzzing helper (`Generate`, `New`, `Seed`). Public package. |
| `pego.pego` | Typed source grammar and recoverable syntax tree; the generated source-parser integration is [proposed](design/054-generated-grammar-source-parser.md). |
| `internal/syntax/` | Lexer and parser for PEGO source code (source → `grammar.Grammar`) |
| `internal/engine/` | Compiler, static analysis, parser runtime, action evaluation, bytecode VMs and Go and TypeScript code generation |
| `internal/engine/genrt/` | Runtime code embedded in generated Go parsers |
| `internal/engine/tsrt/` | Runtime code embedded in generated TypeScript parsers |
| `cmd/pego/` | Command-line tool |
| `examples/` | Small example grammars that show the features, with golden tests |
| `parsers/` | Ready-made parsers for common languages, each a Go module of its own (`parsers/<language>/`, with the grammar, the generated parser and conformance tests); `generate.go` and `parsers_test.go` (main module) generate them and check them on every backend ([README](../parsers/README.md)) |
| `playground/` | WebAssembly API used by the web playground (package `main`; `GOOS=js GOARCH=wasm`) |
| `site/` | Web site generator (a separate Go module): landing page, docs, reference and playground; see [website maintenance](site.md) |
| `netlify.toml` | Netlify build, headers and canonical-domain redirects; see [website maintenance](site.md) |
| `bench/` | Benchmarks comparing the backends with each other and with standard-library parsers (`gen/` holds generated parsers). Results are in [benchmarks.md](benchmarks.md), generated with `go run ./bench/report` (`report/`) from the raw results in `results.txt`. `node bench/ts-literal.mjs [runtime.ts]` measures the embedded TS literal matcher, excluding input/rule setup, trees and error construction; its optional path compares checkouts. |
| `spec/` | Language specification, including the bytecode and VM specification ([bytecode.md](../spec/bytecode.md)) |
| `docs/tutorial/` | The ordered tutorial series ([index](tutorial/README.md)) |
| `docs/guide/` | Task-oriented guides to each feature ([index](guide/README.md)) |
| `docs/cookbook/` | Recipes: complete, runnable solutions to concrete tasks ([index](cookbook/README.md)) |
| `docs/documentation-style.md` | How the documentation is organized and written: page kinds, link directions, titles, voice and examples |
| `docs/design/` | Design records; [template](design/000-template.md) for new proposals |
| `docs/optimizations/` | Numbered optimization history, measured evidence and backend applicability ([catalog](optimizations/README.md)); measurement method and latest analysis remain in [performance.md](performance.md) |

## Architecture

```text
.pego ──syntax.Parse──▶ grammar.Grammar ──engine.Compile──▶ engine.Program ──Parse──▶ Node
                         ▲      │                              │      ▲
              JSON ──────┘      └── grammar.Format ──▶ .pego   │      └── LoadProgram ── .pegoc
                                                               └── MarshalBinary ──▶ .pegoc
```

| Component | Main files | Responsibility |
|:--|:--|:--|
| Grammar intake | `internal/syntax/`, `grammar/` | Parses source or JSON into the public AST; opt-in strict JSON intake checks unknown and duplicate keys ([design 028](design/028-strict-grammar-json-intake.md)); formatting preserves comments and layout. |
| Compiler and analysis | `internal/engine/compile.go`, `analysis.go`, `check.go` | Checks types, analyzes calls and dependencies, and compiles expressions. |
| Runtime | `runtime.go`, `operators_rule.go`, `attrs.go`, `eval.go`, `alloc.go`, `stream_prefix.go`, `stream_undo.go`, `stream_stack.go` | Implements ordered choice, backtracking, actions, recovery and Operators Rule selection; whole-input memo bookkeeping uses sparse seen pages above its rule/input cutoffs ([record 087](optimizations/087-sparse-deferred-memo-seen-pages.md)); streaming commits reuse capture frames ([record 085](optimizations/085-reuse-stream-capture-frames.md)) and isolate prefix storage while batching inactive-scratch cleanup ([record 089](optimizations/089-isolate-stream-prefixes-and-batch-scratch-cleanup.md)). |
| Bytecode | `bytecode.go`, `bcompile.go`, `vm.go`, `ivm.go` | Runs the same grammar semantics with recursive and iterative VMs; see the [bytecode specification](../spec/bytecode.md). |
| Input and documents | `input.go`, `stream_text.go`, `document.go`, `resume.go` | Tracks position units, streaming input, versioned atomic edit batches, edits and resumable repetitions; VM-backed Documents reuse cleared stacks ([record 086](optimizations/086-reuse-document-vm-values.md)), and streaming shares bounded immutable text snapshots ([record 088](optimizations/088-share-bounded-stream-text-snapshots.md)). |
| Compiled grammars | `compiled.go`, `modulefile.go` | Saves and loads bytecode modules, optionally with AST and analysis data. |
| Code generation | `gen.go`, `gen_direct.go`, `genrt/`, `gen_ts.go`, `tsrt/` | Emits standalone Go and TypeScript parsers; exact generated helpers are shared where enabled ([090](optimizations/090-share-identical-generated-helpers.md)), eligible literal-text predicates use switch dispatch ([091](optimizations/091-specialize-pure-literal-text-disjunctions.md)), and generated Node capture scratch is bounded at recycle ([092](optimizations/092-reuse-generated-node-capture-scratch.md)). |
| Language tools | `internal/lint/`, `internal/lsp/`, `editors/vscode/` | Provides static checks, editor requests and the VS Code client. |
| Input generation | `sample/` | Generates accepted inputs and coverage; generated inputs are checked by the parser. |

Files without a directory are in `internal/engine/`. Optimization evidence is in the [catalog](optimizations/README.md); contracts and proposals are in `spec/` and `docs/design/`.

### Runtime

The closure compiler is the reference execution path. The recursive and iterative VMs share rule-call, memoization,
left-recursion and Operators Rule logic with it. Backtracking restores input position, the variable environment, recovered
errors and capture history; farthest-failure expectations use separate isolation and merge scopes. Recognition
returns no tree and skips value and action work that predicates do not need; predicate-dependent rules and callees
can still evaluate actions.
Adaptive memoization enters eager mode when repeated calls exceed one sixteenth of a rule's calls. Its compact
counters halve before overflow, and the eager decision remains sticky for that parse.
Nesting limits and tracing apply to rule calls. `Document` can reuse memo entries across edits and resume long
repetitions; stream parsing emits elements and discards input already committed by the grammar.
Core and generated Go/TypeScript parsing also support invocation-local cancellation and
input-work/execution-step limits; streams and Documents use the same interruption
contract. See [design 033](design/033-cancellable-parses-and-work-budgets.md)
for backend scope, cleanup and measured costs.

### Code Generation

Generated Go and TypeScript parsers embed their runtime and depend only on their standard libraries. Generated Go
has direct paths for supported rules and general per-expression fallback; typed Go also emits inferred Go types
and `ParseAST`/`ParseRuleAST` for configured-start and named rules
([design 029](design/029-typed-named-rule-parsing.md)). Generation defaults (`WithGeneratedMaxDepth`, `gen -max-depth`) and invocation-local depth
options are implemented ([design 022](design/022-generated-parser-depth-limits.md)); Go entry points accept
functional `ParseOption`s, and TypeScript adds options-aware entry points. With `WithTypes` or `-types`, TypeScript
exports grammar declarations and configured-start `parseAST` conversion ([guide](guide/typescript.md#typed-values),
[design 030](design/030-typed-typescript-values.md)). Generated paths preserve the engine's tree, error, depth,
action and memo semantics.

Generated Go `-document` can be combined with `-types`; `Document.ParseAST` directly matches and reuses typed
memo entries when the grammar meets the batch typed runtime's eligibility rules. Other result types use Node
conversion. Returned typed graphs are cloned per invocation, while CST Node members may share the mutable
Document graph. See the [incremental parsing guide](guide/incremental.md) and
[design 044](design/044-generated-incremental-and-stream-parsers.md).

Generated Go `-stream` adds a Node `ParseStream` that follows the engine's Reader stream contract, and with
`-types` a `ParseStreamAST` that converts each element; it cannot yet be combined with TypeScript.
`TestGeneratedTypedStreamMatchesBatch` compares the typed elements with batch `ParseAST`. Stream packages also have
`Iterate` and the diagnostic options, which the engine differential test runs as well. `TestGeneratedStreamMatchesEngine` compares emitted elements, errors and reader
call counts with the engine across chunkings, units, controls, observability and Document generation; changes to
the stream runtime should keep it and the bounded-memory consumer in `TestGeneratedStreamAPI` passing.

Generated Go typed-output cleanup has private full-sweep and bounded generation controls, both disabled by default. They cover typed `ParseAST`/`ParseRuleAST` publication, including conversion and returned string nodes (`TNode[string]`); Node parsing, recognition and TypeScript are unchanged. Unknown ownership falls back to keeping all cells. See [design 034](design/034-typed-output-liveness.md) and [optimization record 094](optimizations/094-generated-typed-output-liveness.md).

## Testing

```bash
go test ./...
```

Engine tests write grammars in PEGO source and compare results using the S-expression form of nodes (`Node.String`).
The `check` helper also verifies that the result is the same with memoization disabled, with every backend (closure, recursive bytecode, iterative bytecode), with both position units, in recognition-only mode, and with tracing on.

`internal/engine/gen_pool_lifecycle_test.go` checks generated Node and typed parser cleanup after oversized
inputs, retained results, and error/retry reuse in both position units.

The TypeScript generator's test needs Node.js 22.18 or later (`node`) and, for its type check, `tsc`; it is skipped when they are missing.

`internal/lint` tests every check with positive and negative cases, lints every example grammar against a hand-checked list of findings, and `TestSoundness` checks the certain findings against the engine on random grammars, exhaustively over short inputs (`-soundness=N` for a longer run).

`internal/lsp` tests talk to the server in process over pipes (lifecycle, malformed messages, incremental edits with non-ASCII and astral characters and `\r\n`, every request) and analyze every example grammar. In `editors/vscode`, `npm install && npm run check` compiles the TextMate grammar's regular expressions with Oniguruma and checks the scopes of a sample, and tokenizes the example and ready-made parser grammars; `npm run check-server -- <path to pego>` talks to the server with the VS Code client's JSON-RPC library.

`sample` tests generate inputs for every example grammar and every grammar of the engine's test corpus (`internal/engine/sample_test.go`, through `export_test.go`) and parse them on every backend.

`go generate ./spec` renders the simple grammar view from `pego.pego` and `spec/syntax.md.tmpl`; edit the template rather than the generated page. `spec/source_grammar_test.go` verifies the simplified display against the canonical source grammar, checks concrete trees and recovery across backends, and builds a generated typed Go consumer. `spec/syntax_test.go` runs the grammar of [Grammar Syntax](../spec/syntax.md) on every `.pego` file and every `pego` code block of the repository and requires it to agree with the parser of `internal/syntax`.

`cd site && go test ./...` builds the site and checks its pages and links; `go test ./playground` runs the WebAssembly smoke test when Node is installed.
Production site builds retain the published current WASM/runtime pair alongside the new pair, using a bounded
asset manifest and immutable deploy source. Workers select the matching hashed Go runtime; previews/local builds
do not automatically read production assets. Hash/HTTP failures stop retention before local output is cleared.
[Design 023](design/023-playground-asset-retention.md) describes one-generation retention, bootstrap and sequential
publication limits; `site/assets_test.go` and `worker_assets_test.mjs` cover its build and worker contracts.
Playground shared-link navigation applies every normalized option and tab even when grammar/input are unchanged;
only parse-affecting changes schedule parsing, preserving editor scroll on tab-only navigation. Actual-app
controlled DOM/client/timer tests cover each field, default restoration, missing start rules and named examples.
URL encoding/decoding is guarded by the latest user action and adopted fragment, including navigation before event
delivery, startup and example fetching. Superseded Share requests do not copy, older parse completions cannot save
during incoming link loading, and landing links keep the latest input. Deferred-promise tests exercise these orderings
and error paths; browser checks complement them with the real WebAssembly worker.
The tree renderer (`site/static/playground/tree.js`) pages direct children in groups of 100, counts node/control rows
within a 400-row initial budget and 5,000-row live cap, and keeps deferred nodes reachable by caret or page controls.
Explicit expansion at the cap compacts materialized branches; paths that cannot fit use a subtree window with a return
control. Controlled row-count, reference/focus and boundary tests cover these resource limits. The tree benchmark
distinguishes equivalent small-tree work from wide-tree display reduction; real browser checks cover DOM/WASM wiring.

GitHub Actions runs the root module and `parsers/test.sh` (including `go vet` for every parser module), site tests, and
Node.js/WebAssembly/generated-TypeScript checks as separate jobs. A newer run on the same branch or pull-request
ref cancels the superseded run. CI uses Go 1.27.1, Node.js 22.18.0, CPython 3.14.0
and TypeScript 5.9.3. Native tests have a 45-minute root timeout and a 30-minute
per-parser timeout within a 90-minute job budget, allowing generated-consumer builds on CI hosts.
The TypeScript differential harness uses the upstream
[`microsoft/TypeScript` v5.9.3 `tests/cases`](https://github.com/microsoft/TypeScript/tree/v5.9.3/tests/cases) corpus.
Required-tool checks fail before tests if a required runtime or the TypeScript corpus is absent. Go tests run with
`-v` so optional skips are visible in the retained 14-day logs. Python uses its installed standard library and vendored
reference data; the additional CPython `Lib/test` corpus (`PEGO_CPYTHON_SRC`) and DuckDB `REFERENCE_FILE` corpus are
not supplied. DuckDB checks use vendored reference results, not a live DuckDB oracle. These jobs do not claim
browser-engine or deployed-site validation.

## Repository Grammar Test Coverage

The guides show how to test a grammar in an application. The repository uses the same layers across its examples and parser corpus:

| Coverage | Location |
|:--|:--|
| Example grammar golden files | [`examples/examples_test.go`](../examples/examples_test.go) |
| Ready-made parser goldens across engine backends and generated parsers | [`parsers/parsers_test.go`](../parsers/parsers_test.go) |
| Reference-implementation and conformance tests with fuzz targets | Each parser module's tests, for example [`parsers/json/json_test.go`](../parsers/json/json_test.go) |
| Generated inputs and backend comparisons | [`sample/example_test.go`](../sample/example_test.go) |
| Compiled grammars, Documents and backend equivalence | `internal/engine/` tests such as `compiled_test.go` and `document_test.go` |

Root `go test ./...` covers the main module. Run [`parsers/test.sh`](../parsers/test.sh) to vet and test all standalone parser modules; `cd site && go test ./...` covers the site module.

## Language Feature Status

| Status | Category | Feature |
|:--|:--|:--|
| Done | Type system | Type definitions (`type`) |
| Done | | Structs (`T = { ... }`) |
| Done | | Union types (`T \| U`) |
| Done | | Type checking |
| Done | | Type inference |
| Done | Rule Expressions | Literals (`"..."`) |
| Done | | Character matching (`[...]`) |
| Done | | Sequences (`a b`) |
| Done | | Choices (`a / b`) |
| Done | | Repetition (`#repeat(...)`, `*`, `+`, `?`) |
| Done | | Lookahead (`&`, `!`) |
| Done | | Atomic (`#atomic`) |
| Done | | Discard (`-`) |
| Done | | Cut (`#cut`) |
| Done | | Anchors (`^^`, `$$`, `^`, `$`) |
| Done | | Top and bottom (`_`, `_\|_`) |
| Done | | Captures (`label:a`) |
| Done | | Groups (`(...)`) |
| Done | | Any character (`.`) and negated character matching (`[^...]`) |
| Done | | Predicate Actions (`{ ... }`) |
| Done | | Attributes (`@...`) and behavioral pragmas (`#...`) |
| Done | | Consuming skip recovery (`#recover`) and error messages (`@error`) |
| Partial | | Opt-in nonempty-literal insertion ([`#missing`](../spec/pragmas.md#missing)): Closure whole-input Node parsing and recognition only; VM, generated parsers, Document, streams and binary serialization are unsupported |
| Done | Actions | Single-statement Predicate Actions (`{term}` or `{name := term}`) and Value Actions (`=> term`) on ordinary rules; Operators Rule declaration actions use default CST behavior |
| Not implemented | | Multiple-statement Predicate Actions and braced Value Actions (`=> { ... }`) parse/format/AST only; compilation rejects them |
| Not implemented | | Inline actions (`(expression => term)` parse/format/AST only; compilation rejects them) |
| Done | | Predicate variables (`{x}`, `{x := 1}`) |
| Done | | Capture references (`$label`) |
| Done | | Built-in functions (`foldl`, `len`, `text`, and others) |
| Done | | Struct constructors (`Type{Field: value}`), and typed node-list constructors (`[]Type{...}`) |
| Done | Advanced | Left recursion |
| Done | | Parsing PEGO source (`.pego`) |
| Done | | Operators Rule declarations |
| Done | | Stream parsing (`#stream`) |
| Done | | Mutable incremental parsing (`Document`); snapshots/change notifications are proposed |
| Done | | Code generation (Go, `pego gen`) |
| Done | | Typed values in generated parsers (`pego gen -types`) |
| Done | | Saving and loading compiled grammars (`.pegoc`) |
| Done | | Code generation (TypeScript, `pego gen -lang ts`) |
| Done | | Input generation and fuzzing (`pego sample`, package `sample`) |
| Done | | Grammar linting (`pego lint`, `pego.Lint`) |
| Done | | Editor support (`pego lsp`, VS Code extension with a TextMate grammar) |
| Done | | Tracing, profiling and error explanation (`WithTrace`, `WithProfile`, `pego trace/profile/explain`) |

## Roadmap

[Issue #1](https://github.com/ornew/pego/issues/1)
