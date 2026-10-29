# Development

This document describes the repository layout and architecture, how to run tests, and the implementation status of language features. The living backlog is maintained in [Issue #1](https://github.com/ornew/pego/issues/1).

## Directory Layout

| Path | Contents |
|:--|:--|
| `pego.go` | Public API of package `pego` (`ParseGrammar`, `Compile`, `CompileSource`, `Parser.Parse`, and others), which joins the engine to the output schema (`Parser.OutputSchema`) |
| `gen/` | Public package of code generation (`gen.Go`, `gen.TypeScript`, `gen.Generator`), which joins the engine to the generators of `internal/gen/`. It embeds the copy of the runtime that generated Go packages carry (`gen/_runtime`, refreshed by `go generate ./gen` and checked against `runtime/` by a test) |
| `grammar/` | Grammar AST, structural validation (`Validate`), JSON conversion (`MarshalJSON`, `UnmarshalJSON`, strict `DecodeJSON`) and formatting as PEGO source (`Format`, which preserves comments). Public package. |
| `internal/grammaranalysis/` | Conservative success and cut proofs shared by the compiler and linter |
| `lint/` | Grammar linter (`lint.Config`) behind `pego lint` and the language server: shared static analysis (`analysis.go`) and checks (`checks.go`). Public package. |
| `internal/lsp/` | Language Server Protocol server for `.pego` files (`pego lsp`): JSON-RPC over standard input and output, document sync, diagnostics, formatting, navigation, hover, rename, completion, semantic tokens |
| `editors/vscode/` | VS Code extension (Node.js, not part of the Go module): language configuration, TextMate grammar (`syntaxes/pego.tmLanguage.json`) and a client that starts `pego lsp` |
| `sample/` | Generation of inputs that a grammar accepts, coverage reports, near-miss invalid inputs and a fuzzing helper (`Generate`, `New`, `Seed`). Public package. |
| `editfuzz/` | Random edit sequences checked against fresh parses, their minimization, saved sequences and their replay (`Fuzz`, `Minimize`, `Replay`, `Load`, `Save`); the library behind `pego edit-fuzz`. Public package. |
| `pego.pego` | Typed source grammar and recoverable syntax tree; the generated source-parser integration is [proposed](design/records/054-generated-grammar-source-parser.md). |
| `internal/syntax/` | Lexer and parser for PEGO source code (source → `grammar.Grammar`) |
| `internal/compile/` | Compiler and static analysis: type checking, call and dependency analysis, the walk of rule bodies that decides their static facts, and the bytecode compiler |
| `internal/engine/` | Parser runtime: `Compile`, compiled grammars, streams and work controls, built on `internal/engine/core/`, the closure backend of `internal/engine/closure/`, the bytecode VMs of `internal/engine/vm/` and the Document of `internal/engine/document/`. It does not import the generators or the output schema: package `gen` joins the generators to it, and the root package the schema export |
| `internal/engine/core/` | The part of the parser runtime that every backend shares: the parser state, input, memo table, expectations, values, built-ins of actions, work controls, tracing and stream state, and the parse options, results and errors that `internal/engine` re-exports. Its package documentation states what a backend provides and what it may use |
| `internal/engine/closure/` | The closure backend: builds a closure graph for each rule body and evaluates actions on the grammar AST |
| `internal/engine/vm/` | The bytecode backend: the recursive and iterative VMs, which run a compiled module over the same instruction loop |
| `internal/engine/document/` | The incremental Document: edits and edit batches, reparsing that reuses what the edits did not touch, snapshots, change reports, diagnostics and the node cursor. It parses through a runner that `internal/engine` implements, and `internal/engine` re-exports its types |
| `schema/` | Output schema: the description of the values a grammar can produce (`schema.Output`), its JSON form and validation, and `Compare`, which reports how two schemas differ. Public package; it imports only the standard library |
| `query/` | Structural queries: patterns that match values of parse results, checked against an output schema when they compile (`Compile`, `Prepare`, `Query.Matches`). Public package |
| `internal/schema/` | The export of a checked program's output schema as a `schema.Output` |
| `internal/value/` | `Node`, the value that parsing produces: kinds, fields, JSON form, traversal and the representation that the engine packages share |
| `internal/limits/` | Work limits of a parse invocation, the error of an exhausted limit and overflow-safe work arithmetic, shared by the parser, the Document and package `query` |
| `internal/bytecode/` | The bytecode instruction set and module, its disassembly, the encoding of modules in compiled grammars and the validation of loaded modules |
| `internal/gen/` | What the Go and TypeScript generators share: generation options, the runtime mode of generated Go parsers, generated files, and the generator state both print with (the program and rule being printed, the expectation table, shared names, the Flow IR state of recognizer rules and the action value resolver) |
| `runtime/` | Runtime package of generated Go parsers, imported by them or copied into them (`github.com/ornew/pego/runtime`) |
| `internal/gen/gogen/` | Go generator: prints the parser, its types package and the runtime copy from the compiled program and Flow IR, and leaves out what the entry points cannot reach |
| `internal/gen/tsgen/` | TypeScript generator: prints the parser, its types and its runtime files from the compiled program and Flow IR |
| `internal/gen/tsgen/tsrt/` | Source of `runtime.ts`, the runtime that generated TypeScript parsers import (joined from `runtime.ts`, `work.ts`, `trace.ts` and `profile.ts`) |
| `cmd/pego/` | Command-line tool |
| `examples/` | Small example grammars that show the features, with golden tests |
| `parsers/` | Ready-made parsers for common languages, each a Go module of its own (`parsers/<language>/`, with the grammar, the generated parser and conformance tests); `generate.go` and `parsers_test.go` (main module) generate them and check them on every backend ([README](../parsers/README.md)) |
| `cmd/playground/` | WebAssembly API used by the web playground (package `main`; `GOOS=js GOARCH=wasm`) |
| `site/` | Web site generator (a separate Go module): landing page, docs, reference and playground; see [website maintenance](site.md) |
| `netlify.toml` | Netlify build, headers and canonical-domain redirects; see [website maintenance](site.md) |
| `bench/` | Benchmarks comparing the backends with each other and with standard-library parsers (`gen/` holds generated parsers). Results are in [benchmarks.md](benchmarks.md), generated with `go run ./bench/report` (`report/`) from the raw results in `results.txt`, `results-ts.txt` (generated TypeScript under Node) and `results-size.txt` (generated parser sizes). `node bench/ts-literal.mjs [runtime.ts]` measures the embedded TS literal matcher, excluding input/rule setup, trees and error construction; its optional path compares checkouts. |
| `spec/` | The specifications: grammar language, parser and runtime, Core IR, and bytecode and VM ([bytecode](../spec/bytecode/README.md)) |
| `docs/tutorial/` | The ordered tutorial series ([index](tutorial/README.md)) |
| `docs/guide/` | Task-oriented guides to each feature ([index](guide/README.md)) |
| `docs/cookbook/` | Recipes: complete, runnable solutions to concrete tasks ([index](cookbook/README.md)) |
| `docs/why-pego.md`, `docs/strategy.md` | Project positioning: [who PEGO is for and how it compares](why-pego.md), and the [project strategy](strategy.md) with its tracks and targets |
| `docs/documentation-style.md` | How the documentation is organized and written: page kinds, link directions, titles, voice and examples |
| `docs/design/` | How the system is built today: [architecture](design/architecture.md) and [Core and Flow IR](design/intermediate-representations.md) |
| `docs/design/records/` | Design records; [template](design/records/000-template.md) for new proposals |
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
| Grammar intake | `internal/syntax/`, `grammar/` | Parses source or JSON into the public AST; opt-in strict JSON intake checks unknown and duplicate keys ([design 028](design/records/028-strict-grammar-json-intake.md)); formatting preserves comments and layout. |
| Compiler and analysis | `internal/compile/compile.go`, `internal/compile/compile_walk.go`, `internal/compile/analysis.go`, `internal/compile/check.go` | Checks types, analyzes calls and dependencies, and walks the rule bodies, deciding their static facts while a backend builder builds their executable form. |
| Runtime | `program.go`, `core/runtime.go`, `closure/builder.go`, `closure/eval.go`, `core/operators_rule.go`, `core/eval.go`, `core/alloc.go`, `core/stream_prefix.go`, `core/stream_undo.go`, `core/stream_stack.go` | Implements ordered choice, backtracking, actions, recovery and Operators Rule selection; whole-input memo bookkeeping uses sparse seen pages above its rule/input cutoffs ([record 087](optimizations/087-sparse-deferred-memo-seen-pages.md)); streaming commits reuse capture frames ([record 085](optimizations/085-reuse-stream-capture-frames.md)) and isolate prefix storage while batching inactive-scratch cleanup ([record 089](optimizations/089-isolate-stream-prefixes-and-batch-scratch-cleanup.md)). |
| Bytecode | `internal/bytecode/bytecode.go`, `internal/compile/bcompile.go`, `vm/vm.go`, `vm/ivm.go` | Runs the same grammar semantics with recursive and iterative VMs; see the [bytecode specification](../spec/bytecode/README.md). |
| Input and documents | `core/input.go`, `core/stream_text.go`, `document/document.go`, `core/resume.go` | Tracks position units, streaming input, versioned atomic edit batches, edits and resumable repetitions; VM-backed Documents reuse cleared stacks ([record 086](optimizations/086-reuse-document-vm-values.md)), and streaming shares bounded immutable text snapshots ([record 088](optimizations/088-share-bounded-stream-text-snapshots.md)). |
| Compiled grammars | `program_compiled.go`, `internal/bytecode/modulefile.go`, `internal/bytecode/codec.go` | Saves and loads the bytecode module of one mode (parse, recognize or document) and serves only that mode's operations. |
| Code generation | `internal/gen/gogen/`, `runtime/`, `internal/gen/tsgen/` | Emits standalone Go and TypeScript parsers; exact generated helpers are shared where enabled ([090](optimizations/090-share-identical-generated-helpers.md)), eligible literal-text predicates use switch dispatch ([091](optimizations/091-specialize-pure-literal-text-disjunctions.md)), and generated Node capture scratch is bounded at recycle ([092](optimizations/092-reuse-generated-node-capture-scratch.md)). |
| Language tools | `lint/`, `internal/lsp/`, `editors/vscode/` | Provides static checks, editor requests and the VS Code client. |
| Input generation | `sample/` | Generates accepted inputs and coverage; generated inputs are checked by the parser. |

Files without a directory are in `internal/engine/`. There, a file's prefix names the part it belongs to: `program` for the entry points of a program (compilation, compiled grammars, the parse drivers and the runner that a Document parses through). `compile_api.go` and `core_api.go` hold the names that the engine exposes from the compiler and from `internal/engine/core`, and `profile.go` reads parse results. What every backend shares is in `internal/engine/core/`: the parser state, the input, memo and stream state (`input_`, `stream_` and `work_` files and `runtime.go`), the built-ins of actions (`eval.go`, `filter.go`) and the tracer (`trace.go`). The closure backend is in `internal/engine/closure/`, the recursive and iterative bytecode VMs are in `internal/engine/vm/`, and the Document with its snapshots, change reports and node cursor is in `internal/engine/document/`, which `document_api.go` re-exports. Tests that compare generated parsers with the engine are named after the generator they check: `gogen_` for Go, `tsgen_` for TypeScript and `gen_` for both. Optimization evidence is in the [catalog](optimizations/README.md); contracts and proposals are in `spec/` and `docs/design/records/`.

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
contract. See [design 033](design/records/033-cancellable-parses-and-work-budgets.md)
for backend scope, cleanup and measured costs.

### Code Generation

Generated Go and TypeScript parsers embed their runtime and depend only on their standard libraries. Generated Go
is printed from Flow IR ([design 062](design/records/062-backend-independent-optimization-ir.md)): lean value-free rules as direct rules and the others as framed bodies the runtime calls; typed Go also emits inferred Go types
and `ParseAST`/`ParseRuleAST` for configured-start and named rules
([design 029](design/records/029-typed-named-rule-parsing.md)). Generation defaults (`MaxDepth` of `gen.Go` and `gen.TypeScript`, `gen -max-depth`) and invocation-local depth
options are implemented ([design 022](design/records/022-generated-parser-depth-limits.md)); Go entry points accept
functional `ParseOption`s, and TypeScript adds options-aware entry points. With `Types` or `-types`, TypeScript
exports grammar declarations and configured-start `parseAST` conversion ([guide](guide/typescript.md#typed-values),
[design 030](design/records/030-typed-typescript-values.md)). Generated paths preserve the engine's tree, error, depth,
action and memo semantics.

Generated Go `-document` can be combined with `-types`; `Document.ParseAST` directly matches and reuses typed
memo entries when the grammar meets the batch typed runtime's eligibility rules. Other result types use Node
conversion. Returned typed graphs are cloned per invocation, while CST Node members may share the mutable
Document graph. See the [incremental parsing guide](guide/incremental.md) and
[design 044](design/records/044-generated-incremental-and-stream-parsers.md).

Generated Go `-stream` adds a Node `ParseStream` that follows the engine's Reader stream contract, and with
`-types` a `ParseStreamAST` that converts each element; it cannot yet be combined with TypeScript.
`TestGeneratedTypedStreamMatchesBatch` compares the typed elements with batch `ParseAST`. Stream packages also have
`Iterate` and the diagnostic options, which the engine differential test runs as well. `TestGeneratedStreamMatchesEngine` compares emitted elements, errors and reader
call counts with the engine across chunkings, units, controls, observability and Document generation; changes to
the stream runtime should keep it and the bounded-memory consumer in `TestGeneratedStreamAPI` passing.

Generated Go typed-output cleanup is not implemented; the measured full-sweep and bounded candidates and their limits are in [design 034](design/records/034-typed-output-liveness.md) and [optimization record 094](optimizations/094-generated-typed-output-liveness.md).

## Testing

```bash
go test ./...
```

The tests of `internal/engine` that build generated parsers build them in two forms. A parser generated with work controls charges work and accepts the options `WithContext` and `WithWorkLimits`. A parser generated without them is the form that most parsers have, and a different part of the generator prints it. Every test runs the form that it exists to check:

| Kind of test | Runs | How |
|:--|:--|:--|
| Tests that build fixtures and check their behavior, output or equivalence with the engine | Both forms, as the subtests `controls` and `ordinary` | The test body runs under `eachMode` |
| Tests that pass a control option (`WithContext`, `WithWorkLimits`) | With controls only | `needsWorkControls` |
| Tests of output that exists only without work controls (terminal programs, the Flow IR recognizer and Node rules, choice guards) | Without controls only | The test sets `PEGO_TEST_ORDINARY=1` itself |
| Tests that do not build fixtures | Not form-specific | |

A test that builds a fixture without any of these fails the run when it finishes, because it would check one form only. `go test ./...` therefore covers both forms and needs no setting.

`PEGO_TEST_ORDINARY` restricts a run to one form: `0` runs everything that builds fixtures with work controls, and `1` runs it without, skipping the tests that pass a control. Use it to reproduce a failure of one form; it does not widen coverage.

Engine tests write grammars in PEGO source and compare results using the S-expression form of nodes (`Node.String`).
The `check` helper also verifies that the result is the same with memoization disabled, with every backend (closure, recursive bytecode, iterative bytecode), with both position units, in recognition-only mode, and with tracing on.

`internal/engine/gogen_pool_lifecycle_test.go` checks generated Node and typed parser cleanup after oversized
inputs, retained results, and error/retry reuse in both position units.

The TypeScript generator's test needs Node.js 22.18 or later (`node`) and, for its type check, `tsc`; it is skipped when they are missing.
The generated TypeScript has the same two forms, chosen by `-work-controls`. The TypeScript tests generate the form without work controls unless the test passes a control option (`signal`, `maxInputWork`, `maxSteps`), which generates with `WorkControls`. `TestGeneratedTSParsersMatchEngine` compares both forms with the engine, and `tsgen_work_forms_test.go` checks that the plain form has no charge, no control option and no cancellation export, and that the controlled form accepts the options.
TypeScript is printed from the same Flow IR as Go (`internal/gen/tsgen/flow.go`, `internal/gen/tsgen/outline.go`); `TestGeneratedTSPrintsLeanRulesFromFlow` and the frozen decision goldens cover what is chosen, and a rule that Flow IR does not print fails generation.

`lint` tests every check with positive and negative cases, lints every example grammar against a hand-checked list of findings, and `TestSoundness` checks the certain findings against the engine on random grammars, exhaustively over short inputs (`-soundness=N` for a longer run).

`internal/lsp` tests talk to the server in process over pipes (lifecycle, malformed messages, incremental edits with non-ASCII and astral characters and `\r\n`, every request) and analyze every example grammar. In `editors/vscode`, `npm install && npm run check` compiles the TextMate grammar's regular expressions with Oniguruma and checks the scopes of a sample, and tokenizes the example and ready-made parser grammars; `npm run check-server -- <path to pego>` talks to the server with the VS Code client's JSON-RPC library.

`sample` tests generate inputs for every example grammar and every grammar of the engine's test corpus (`internal/engine/sample_test.go`, through `export_test.go`) and parse them on every backend.

`go generate ./spec/language` renders the simple grammar view from `pego.pego` and `spec/language/syntax.md.tmpl`; edit the template rather than the generated page. `spec/language/source_grammar_test.go` verifies the simplified display against the canonical source grammar, checks concrete trees and recovery across backends, and builds a generated typed Go consumer. `spec/language/syntax_test.go` runs the grammar of [Grammar Syntax](../spec/language/syntax.md) on every `.pego` file and every `pego` code block of the repository and requires it to agree with the parser of `internal/syntax`.

`cd site && go test ./...` builds the site and checks its pages and links; `go test ./cmd/playground` runs the WebAssembly smoke test when Node is installed.
Production site builds retain the published current WASM/runtime pair alongside the new pair, using a bounded
asset manifest and immutable deploy source. Workers select the matching hashed Go runtime; previews/local builds
do not automatically read production assets. Hash/HTTP failures stop retention before local output is cleared.
[Design 023](design/records/023-playground-asset-retention.md) describes one-generation retention, bootstrap and sequential
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

The same-source work-control reference build compiles and tests the engine packages without core work-control
instrumentation. After changing `internal/engine` test files, check that both builds compile with
`go vet ./internal/engine/...` and `go vet -tags pego_work_controls_reference ./internal/engine/...`; test files that use
helpers defined in untagged files keep both builds compiling.

GitHub Actions runs the root module and `parsers/test.sh` (including `go vet` for every parser module), site tests, and
Node.js/WebAssembly/generated-TypeScript checks as separate jobs. A newer run on the same branch or pull-request
ref cancels the superseded run. CI uses Go 1.27.1, Node.js 22.18.0, CPython 3.14.0
and TypeScript 5.9.3. Native tests have a 45-minute root timeout and a 30-minute
per-parser timeout within a 90-minute job budget, allowing generated-consumer builds on CI hosts.
The TypeScript differential harness uses the upstream
[`microsoft/TypeScript` v5.9.3 `tests/cases`](https://github.com/microsoft/TypeScript/tree/v5.9.3/tests/cases) corpus.
Required-tool checks fail before tests if a required runtime or the TypeScript corpus is absent. Go tests run with
`-v` so optional skips are visible in the retained 14-day logs. Python uses its installed standard library and recorded
reference data; the live comparisons on a CPython checkout (`PEGO_CPYTHON_SRC`) and the DuckDB `REFERENCE_FILE` corpus
are not supplied. DuckDB checks use recorded reference results, not a live DuckDB oracle. These jobs do not claim
browser-engine or deployed-site validation.

## Repository Grammar Test Coverage

The guides show how to test a grammar in an application. The repository uses the same layers across its examples and parser corpus:

| Coverage | Location |
|:--|:--|
| Example grammar golden files | [`examples/examples_test.go`](../examples/examples_test.go) |
| Ready-made parser goldens across engine backends and generated parsers | [`parsers/parsers_test.go`](../parsers/parsers_test.go) |
| Reference-implementation and conformance tests with fuzz targets | Each parser module's tests, for example [`parsers/json/json_test.go`](../parsers/json/json_test.go) |
| Generated inputs and backend comparisons | [`sample/example_test.go`](../sample/example_test.go) |
| Compiled grammars, Documents and backend equivalence | `internal/engine/` tests such as `compiled_test.go` and `document_test.go`, and the tests of `internal/engine/document/` |
| Generated Go against the engine and regression freezes of what the engine cannot say (observation, Documents, work, streams, lowering decisions) | `internal/engine/` tests `gogen_flow_*_test.go` and `core_*_test.go` with `internal/engine/testdata/*.golden`; a change of what a golden freezes is rewritten with `go test ./internal/engine -run <test> -update` and its diff reviewed |

Root `go test ./...` covers the main module. Run [`parsers/test.sh`](../parsers/test.sh) to vet and test all standalone parser modules; `cd site && go test ./...` covers the site module.

### Third-Party Test Corpora

The conformance suites and reference data of the ready-made parsers use text from other projects (cel-spec, JSONTestSuite,
yaml-test-suite, CPython, DuckDB and others). It is not in the repository: [`internal/corpora/manifest.json`](../internal/corpora/manifest.json)
pins each source (archive URL at a fixed revision, license, the files kept, and the SHA-256 digest of those files), and
`go run ./hack/corpora fetch` downloads what is missing into `$PEGO_CORPORA` (default: `pego/corpora` in the user cache
directory) and checks the digests. Other subcommands: `list`, `verify`, `dir NAME`, and `pin NAME`, which prints the digest
for a new manifest entry. The digest is over the extracted files (the lines of `sha256sum`, sorted by path), because
GitHub does not promise identical archive bytes for a commit. `fetch` also checks the files that are already in the
cache against the digest and fetches an entry again if they differ, so `parsers/test.sh` runs against verified data.
All sources are archives at a full commit SHA except cel-go, which is the module zip of its release at
`proxy.golang.org`: a module version is immutable and recorded in the Go checksum database.

`go test` never uses the network: a test that needs a corpus that is not fetched is skipped with the command to run.
[`parsers/test.sh`](../parsers/test.sh) fetches first and stops if that fails, so a full run skips nothing for a missing corpus.
Reference data kept in a parser's `testdata` refers to the upstream text by file, position and hash instead of containing it.

## Language Feature Status

| Status | Category | Feature |
|:--|:--|:--|
| Done | Type system | Type definitions (`type`) |
| Done | | Structs (`T = { ... }`) |
| Done | | Join types (`T \| U`) |
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
| Partial | | Opt-in nonempty-literal insertion ([`#missing`](../spec/language/pragmas.md#missing)): Closure whole-input Node parsing and recognition only; VM, generated parsers, Document, streams and binary serialization are unsupported |
| Done | Actions | Predicate Actions (`{term}`, `{name := term}` and multi-statement blocks `{a; b}`) and Value Actions (`=> term`) on ordinary rules; Operators Rule declaration actions use default CST behavior |
| Not implemented | | Braced Value Actions (`=> { ... }`) parse/format/AST only; compilation rejects them |
| Done | | Inline Value Actions (`(expression => term)`, a hidden rule at its position; braced inline blocks are rejected) |
| Done | | Typed variables (`var x int = 0` declarations; `{x}`, `{x := 1}`) |
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
| Done | | Grammar linting (`pego lint`, package `lint`) |
| Done | | Editor support (`pego lsp`, VS Code extension with a TextMate grammar) |
| Done | | Tracing, profiling and error explanation (`WithTrace`, `WithProfile`, `pego trace/profile/explain`) |

## Roadmap

[Issue #1](https://github.com/ornew/pego/issues/1)
