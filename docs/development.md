# Development

This document describes the repository layout and architecture, how to run tests, and the implementation status of language features. The living backlog is maintained in [Issue #1](https://github.com/ornew/pego/issues/1).

## Directory layout

| Path | Contents |
|:--|:--|
| `pego.go`, `lint.go` | Public API of package `pego` (`ParseGrammar`, `Compile`, `CompileSource`, `Parser.Parse`, `Lint`, and others) |
| `grammar/` | Grammar AST, structural validation (`Validate`), JSON conversion (`MarshalJSON`, `UnmarshalJSON`) and formatting as PEGO source (`Format`, which preserves comments). Public package. |
| `internal/grammaranalysis/` | Conservative success and cut proofs shared by the compiler and linter |
| `internal/lint/` | Grammar linter behind `pego.Lint` and `pego lint`: shared static analysis (`analysis.go`) and checks (`checks.go`) |
| `internal/lsp/` | Language Server Protocol server for `.pego` files (`pego lsp`): JSON-RPC over standard input and output, document sync, diagnostics, formatting, navigation, hover, rename, completion, semantic tokens |
| `editors/vscode/` | VS Code extension (Node.js, not part of the Go module): language configuration, TextMate grammar (`syntaxes/pego.tmLanguage.json`) and a client that starts `pego lsp` |
| `sample/` | Generation of inputs that a grammar accepts, coverage reports, near-miss invalid inputs and a fuzzing helper (`Generate`, `New`, `Seed`). Public package. |
| `internal/syntax/` | Lexer and parser for PEGO source code (source → `grammar.Grammar`) |
| `internal/engine/` | Compiler, static analysis, parser runtime, action evaluation, bytecode VMs and Go and TypeScript code generation |
| `internal/engine/genrt/` | Runtime code embedded in generated Go parsers |
| `internal/engine/tsrt/` | Runtime code embedded in generated TypeScript parsers |
| `cmd/pego/` | Command-line tool |
| `examples/` | Small example grammars that show the features, with golden tests |
| `parsers/` | Ready-made parsers for common languages, each a Go module of its own (`parsers/<language>/`, with the grammar, the generated parser and conformance tests); `generate.go` and `parsers_test.go` (main module) generate them and check them on every backend ([README](../parsers/README.md)) |
| `playground/` | WebAssembly API used by the web playground (package `main`; `GOOS=js GOARCH=wasm`) |
| `site/` | Web site generator (a separate Go module): landing page, docs, reference and playground; build with `site/build.sh` |
| `netlify.toml` | Netlify build configuration for the site |
| `bench/` | Benchmarks comparing the backends with each other and with standard-library parsers (`gen/` holds generated parsers). Results are in [benchmarks.md](benchmarks.md), generated with `go run ./bench/report` (`report/`) from the raw results in `results.txt`. `node bench/ts-literal.mjs [runtime.ts]` measures the embedded TS literal matcher, excluding input/rule setup, trees and error construction; its optional path compares checkouts. |
| `spec/` | Language specification, including the bytecode and VM specification ([bytecode.md](../spec/bytecode.md)) |
| `docs/tutorial/` | Tutorials (getting started) |
| `docs/guide/` | Task-oriented guides to each feature ([index](guide/README.md)) |
| `docs/cookbook/` | Recipes: complete, runnable solutions to concrete tasks ([index](cookbook/README.md)) |
| `docs/design/` | Design records; [template](design/000-template.md) for new proposals |
| `docs/optimizations/` | Numbered optimization history, measured evidence and backend applicability ([catalog](optimizations/README.md)); measurement method and latest analysis remain in [performance.md](performance.md) |

## Architecture

```
.pego ──syntax.Parse──▶ grammar.Grammar ──engine.Compile──▶ engine.Program ──Parse──▶ Node
                         ▲      │                              │      ▲
              JSON ──────┘      └── grammar.Format ──▶ .pego   │      └── LoadProgram ── .pegoc
                                                               └── MarshalBinary ──▶ .pegoc
```

| Component | Main files | Responsibility |
|:--|:--|:--|
| Grammar intake | `internal/syntax/`, `grammar/` | Parses source or JSON into the public AST; formatting preserves comments and layout. |
| Compiler and analysis | `internal/engine/compile.go`, `analysis.go`, `check.go` | Checks types, analyzes calls and dependencies, and compiles expressions. |
| Runtime | `runtime.go`, `pratt.go`, `attrs.go`, `eval.go` | Implements ordered choice, backtracking, actions, recovery and Pratt selection. |
| Bytecode | `bytecode.go`, `bcompile.go`, `vm.go`, `ivm.go` | Runs the same grammar semantics with recursive and iterative VMs; see the [bytecode specification](../spec/bytecode.md). |
| Input and documents | `input.go`, `document.go`, `resume.go` | Tracks position units, streaming input, edits and resumable repetitions. |
| Compiled grammars | `compiled.go`, `modulefile.go` | Saves and loads bytecode modules, optionally with AST and analysis data. |
| Code generation | `gen.go`, `gen_direct.go`, `genrt/`, `gen_ts.go`, `tsrt/` | Emits standalone Go and TypeScript parsers; unsupported rules use general expression dispatch. |
| Language tools | `internal/lint/`, `internal/lsp/`, `editors/vscode/` | Provides static checks, editor requests and the VS Code client. |
| Input generation | `sample/` | Generates accepted inputs and coverage; generated inputs are checked by the parser. |

Files without a directory are in `internal/engine/`. Optimization evidence is in the [catalog](optimizations/README.md); contracts and proposals are in `spec/` and `docs/design/`.

### Runtime

The closure compiler is the reference execution path. The recursive and iterative VMs share rule-call, memoization,
left-recursion and Pratt logic with it. Backtracking restores input position, the variable environment, recovered
errors and capture history; farthest-failure expectations use separate isolation and merge scopes. Recognition
returns no tree and skips value and action work that predicates do not need; predicate-dependent rules and callees
can still evaluate actions.
Nesting limits and tracing apply to rule calls. `Document` can reuse memo entries across edits and resume long
repetitions; stream parsing emits elements and discards input already committed by the grammar.

### Code generation

Generated Go and TypeScript parsers embed their runtime and depend only on their standard libraries. Generated Go
has direct paths for supported rules and general per-expression fallback; typed Go also emits inferred Go types
and `ParseAST`. Generated paths preserve the engine's tree, error, depth, action and memo semantics.

## Testing

```bash
go test ./...
```

Engine tests write grammars in PEGO source and compare results using the S-expression form of nodes (`Node.String`).
The `check` helper also verifies that the result is the same with memoization disabled, with every backend (closure, recursive bytecode, iterative bytecode), with both position units, in recognition-only mode, and with tracing on.

The TypeScript generator's test needs Node.js 22.18 or later (`node`) and, for its type check, `tsc`; it is skipped when they are missing.

`internal/lint` tests every check with positive and negative cases, lints every example grammar against a hand-checked list of findings, and `TestSoundness` checks the certain findings against the engine on random grammars, exhaustively over short inputs (`-soundness=N` for a longer run).

`internal/lsp` tests talk to the server in process over pipes (lifecycle, malformed messages, incremental edits with non-ASCII and astral characters and `\r\n`, every request) and analyze every example grammar. In `editors/vscode`, `npm install && npm run check` compiles the TextMate grammar's regular expressions with Oniguruma and checks the scopes of a sample and the example grammars; `npm run check-server -- <path to pego>` talks to the server with the VS Code client's JSON-RPC library.

`sample` tests generate inputs for every example grammar and every grammar of the engine's test corpus (`internal/engine/sample_test.go`, through `export_test.go`) and parse them on every backend.

`spec/syntax_test.go` runs the grammar of the [syntax summary](../spec/syntax.md) on every `.pego` file and every `pego` code block of the repository and requires it to agree with the parser of `internal/syntax`.

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

## Language feature status

| Status | Category | Feature |
|:--|:--|:--|
| Done | Type system | Type definitions (`type`) |
| Done | | Terminal types (`terminal`) |
| Done | | Structs (`struct`) |
| Done | | Union types (`T \| U`) |
| Done | | Type checking |
| Done | | Type inference |
| Done | Parser expressions | Literals (`"..."`) |
| Done | | Character classes (`(?...)`) |
| Done | | Sequences (`a b`) |
| Done | | Choices (`a / b`) |
| Done | | Repetition (`{,}`, `*`, `+`, `?`) |
| Done | | Lookahead (`&`, `!`) |
| Done | | Atomic (`@`) |
| Done | | Discard (`-`) |
| Done | | Cut (`--`) |
| Done | | Anchors (`^^`, `$$`, `^`, `$`) |
| Done | | Top and bottom (`_`, `_\|_`) |
| Done | | Captures (`label:a`) |
| Done | | Groups (`(...)`) |
| Done | | Any character (`.`) and negated character classes (`(?^...)`) |
| Done | | Predicates (`[...]`) |
| Done | | Attributes (`#...`) |
| Done | | Consuming skip recovery (`#recover`) and error messages (`#error`); missing-token insertion is proposed |
| Done | Actions | Actions (`->`) |
| Done | | Variables (`x`, `x = 1`) |
| Done | | Capture references (`$label`, `$1`) |
| Done | | Built-in functions (`foldl`, `len`, `text`, and others) |
| Done | | Constructors (`new`) |
| Done | Advanced | Left recursion |
| Done | | Parsing PEGO source (`.pego`) |
| Done | | Pratt expressions (`pratt`) |
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
