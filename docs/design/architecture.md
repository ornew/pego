# Architecture

How a grammar becomes a parser in PEGO, which package owns each stage, and where to look when you change one. Read it
before you work in `internal/engine/`, `runtime/` or the generators. For the contracts the stages must keep, read the
[specification](../../spec/README.md); for the two internal representations in the middle, read
[Core and Flow IR](intermediate-representations.md).

## Stages

A grammar passes through the following stages. Each stage lists what it takes, what it makes and where it lives.

| Stage | Input | Output | Owner |
|:--|:--|:--|:--|
| Source parsing | `.pego` text | `grammar.Grammar` | `internal/syntax/` (`lexer.go`, `parser.go`), `pego.ParseGrammar` |
| Grammar syntax tree | | `grammar.File` of declarations (`Decl`), expressions (`Expr`) and terms (`Term`) | `grammar/` (`ast.go`); JSON in `json.go`; source formatting in `format.go`; structural checks in `validate.go` |
| Compilation | A `grammar.Grammar` and `engine.Options` | An `engine.Program`: rules with flags, closures, types | `internal/engine/compile.go` (`Compile`, `buildNamed`) |
| Analysis | The grammar | Call graph, left calls, memo and leader decisions, nullability, value shapes, checked types, projections | `analysis.go`, `check.go`, `value_types.go`, `type_resolve.go`, `value_shape.go`, `project.go`, `reach.go`, `internal/grammaranalysis/` |
| Core IR | A compiled `Program` | A `core.Module` with attributes, after passes | `core_build.go`, `internal/engine/core/` |
| Flow IR | A `core.Module` | One `flow.Func` per rule variant | `internal/engine/flow/` |
| Emission | Core IR, Flow IR or the grammar | Go source, TypeScript source, bytecode, `.pegoc` files | `gen*.go`, `bcompile.go`, `modulefile.go`, `compiled.go` |

Compilation does the following before it assembles rules (`buildNamed`): validates the grammar (`validateGrammar`),
turns inline actions into hidden rules (`inline_actions.go`), applies `DefaultValue` (`default_value.go`), resolves the
start rules (`start.go`), and normalizes attributes into a copy of the grammar (`normalize_attributes.go`). It then
analyzes, sets rule flags (`memo`, `leader`, `lean`, `plain`, value-free twins), assembles closures (`compileRule`),
checks types (`checkTypes`) and numbers the memoized rules.

```text
 .pego text ──syntax.Parse──▶ grammar.Grammar ◀── grammar JSON
                                    │                  ▲
                                    │ grammar.Validate │ grammar.MarshalJSON
                                    ▼                  │
                              engine.Compile ──────────┘ (SourceGrammar)
                                    │
        ┌───────────────────────────┼───────────────────────────────┐
        ▼                           ▼                               ▼
  closure program            Program.Module()                 generators
  (compile.go)              bytecode (bcompile.go)       (gen.go, gen_ts.go)
        │                           │                               │
        ▼                           ▼                               ▼
   Program.Parse             recursive VM (vm.go)         generated Go or TS
                           iterative VM (ivm.go)
                                    │
                                    ▼
                         .pegoc (compiled.go, modulefile.go)
```

The closure program and the bytecode module share the runtime state in `runtime.go` (the `parser`), the memo table
(`memo.go`), Operators Rule selection (`operators_rule.go`) and action evaluation (`eval.go`).

## Backends

| Backend | Built from | Runs as | Notes |
|:--|:--|:--|:--|
| Closures | `compiler.expr` over the grammar (`compile.go`, `attrs.go`) | Go closures called by `Program.Parse` | The reference path and the default (`Backend` `Default`) when the grammar is available. The only backend that supports `#missing` |
| Recursive VM | `bcompiler` over the compiled rules (`bcompile.go`) into a `Module` (`bytecode.go`) | `vm.go`, rule calls use the host stack | Instruction set specified in [Bytecode](../../spec/bytecode.md) |
| Iterative VM | The same `Module` | `ivm.go`, rule calls and Operators Rule loops live on the VM stack | Deep input does not use the host stack |
| Compiled grammar | A `Module` of one mode (parse, recognize or document) | Loaded by `LoadProgram`, `pego.LoadParser` | `modulefile.go` writes and validates; `compiled.go` is the container. A loaded program has no grammar syntax tree, so it runs on a VM |
| Generated Go | `generate` in `gen.go` | The package that `pego gen` writes, over `runtime/` | Three rule tables: the Node table (`rules`), the recognizer (`recRules`) and the typed table (`trules`) |
| Generated TypeScript | `GenerateTS` in `gen_ts.go` | A `.ts` package over `tsrt/` | Batch parsing, typed values and observability; no Documents or streams |

Recognition (`RecognizeOnly`, `-recognize`) runs a program in which no rule builds a value (`Program.recognizer`).
Value-free twins (`rule.twin`) are the unit of that specialization.

### Generated Go

`generate` runs `generateOnce` twice: the first pass records which rules of the Node and recognizer tables something
can reach (`liveRules`, `reach.go`), and the second leaves out the others. Each pass compiles the grammar, creates a
`generator`, prints the tables, then runs the textual passes over the output.

```text
 grammar ──engine.Compile──▶ Program
                                │
        ┌───────────────────────┼─────────────────────────────┐
        ▼                       ▼                             ▼
   Node table             recognizer table               typed table
   (rules)                (recRules)                     (trules, -types)
        │                       │                             │
   dgen direct            Flow route if eligible         buildCore ─ passes ─ Verify
   (gen_direct*.go)       (startRecFlow) else dgen              │
   generator.expr         dgen for the rest              flow.Lower / LowerFrame
   (gen.go) fallback             │                             │
        │                       │                        flowPrinter
        └───────────┬───────────┴─────────────────────────────┘
                    ▼
        go/format ─ ordinary-method specialization ─ pruneTypedEntries ─ qualifyRuntime
                    ▼
       parser.go, api.go, types.go, runtime copy (gen_runtime.go, gen_types*.go)
```

| Concern | Files |
|:--|:--|
| Entry, options, tables | `gen.go` (`Generate`, `GenOptions`, `generator`) |
| Node and recognizer rules | `gen_direct.go`, `gen_direct_node.go`, `gen_choice_guard.go`, `gen_terminal.go` |
| Typed values | `gen_types.go`, `gen_types_values.go`, `gen_types_package.go`, `gen_typed_document_clone.go` |
| Core and Flow routes | `core_build.go`, `gen_flow.go`, `gen_flow_recognizer.go`, `gen_decisions.go` (test hooks) |
| Textual passes | `gen_ordinary_methods.go`, `gen_ordinary_fold.go`, `gen_ordinary_peek.go` (work-control specialization), `gen_prune.go`, `gen_unread.go`, `gen_trim.go` |
| Runtime embedding | `gen_runtime.go`, `runtime_source.go` |
| Observability variants | `gen_observe.go`, `gen_document_observe.go` |

### Generated TypeScript

`tsGen` prints TypeScript directly from the grammar (`gen_ts.go`, `gen_ts_types.go`, `gen_ts_files.go`,
`gen_ts_observe.go`). It does not use Core IR or Flow IR. The runtime is `internal/engine/tsrt/runtime.ts`, joined
with `work.ts`, `trace.ts` and `profile.ts`, and embedded or imported according to `RuntimeMode`.

## Runtime Packages

| Package | Role |
|:--|:--|
| `internal/engine` | The engine: closures, VMs, memoization, recovery, Documents, streams, tracing and profiling, work controls, and the generators |
| `runtime/` | The Go runtime of generated parsers (`runtime.go`, `typed.go`, `document.go`, `typed_document.go`, `stream.go`, `terminal.go`, `work.go`, `trace.go`, `profile.go`). Generated code imports it as `pegort` or carries a copy. Its behavior must match `internal/engine`; the generator tests compare them. The names generated code calls are not a stable API |
| `internal/engine/tsrt/` | Source of the TypeScript runtime |
| `pego` (root) | The public API: `ParseGrammar`, `Compile`, `CompileSource`, `GenerateGo`, `GenerateTypeScript`, `LoadParser`, documents and options (`pego.go`, `document_*.go`) |
| `grammar/` | The public grammar AST, JSON, validation and formatting |

## Values, Documents and Streams

| Mechanism | Engine | Generated Go |
|:--|:--|:--|
| Values | The value model: rule values and `Node` trees, shapes in `value_shape.go`, construction in `node_build.go`, actions in `eval.go` and `value_runtime.go` | `Node` table over `runtime.go`; typed table over `typed.go` builds Go structs directly |
| Position units | `input.go` (`PositionUnit`: code points or bytes) | `runtime/` input types |
| Memoization | `memo.go`, `sparse_memo.go`, `work_memo.go` | `runtime.go`, `typed.go` |
| Documents | `document.go` (versioned atomic edit batches, `document_edits.go`, `document_changes.go`), reuse of memo entries and resumable repetitions (`resume.go`, `fragment_cursor.go`), snapshots (`snapshot*.go`, `document_pieces*.go`), diagnostics (`document_diagnostics.go`) | `runtime/document.go`, `typed_document.go` (generation with `-document`) |
| Streams | `stream_prefix.go`, `stream_undo.go`, `stream_stack.go`, `stream_text.go`, `stream_iterator.go`: the `#stream` repetition commits elements and drops the input behind them | `runtime/stream.go` (generation with `-stream`) |
| Work controls | `work_control.go`, `work_setup.go`, `work_state.go`, `work_input.go` | `runtime/work.go`, generated charging (with `-work-controls`) |

A Document parse runs the call-preserving path: every rule call keeps its memo and trace checks, so reuse and examined
ranges are exact. A batch parse takes the plain path for rules that are never memoized outside Documents (`rule.plain`,
`parser.noPlain`).

## Observability

| Mechanism | Where |
|:--|:--|
| Engine tracing and profiling | `trace.go` (every rule call goes through `parser.call`; the nil check of a plain call is the only cost when off) and `profile.go`; the CLI commands `pego trace`, `pego profile`, `pego explain` (`cmd/pego/`) |
| Generated tracing and profiling | `gen_observe.go`, `gen_document_observe.go`, `runtime/trace.go`, `runtime/profile.go`; TypeScript in `gen_ts_observe.go` and `tsrt/trace.ts`, `tsrt/profile.ts` |
| Optimization attribution | `gen_decisions.go` records lowering decisions for the tests that compare Core passes with the generator; the private `disable*` switches of `GenOptions` select reference routes ([record 024](records/024-optimization-mechanism-controls.md)) |
| Printing IR | `core.Print`, `flow.String` ([Core and Flow IR](intermediate-representations.md#printed-text-form)) |
| Contracts | [Observability](../../spec/observability.md) |

## Tools Around the Engine

| Tool | Path | Uses |
|:--|:--|:--|
| Command line | `cmd/pego/` | The public API |
| Linter | `internal/lint/`, `internal/grammaranalysis/` | The grammar syntax tree and shared proofs |
| Language server | `internal/lsp/` | `internal/syntax`, `grammar/`, the linter, the engine's types |
| Input generation | `sample/`, `editfuzz/`, `fixture/` | The engine, as an oracle |
| Playground | `playground/`, `site/` | The engine compiled to WebAssembly |
| Ready-made parsers | `parsers/` | Generated Go, one module each |

## Where to Change What

| Change | Start here | Then check |
|:--|:--|:--|
| Grammar syntax | `internal/syntax/`, `grammar/ast.go`, `spec/` | `grammar/validate.go`, `grammar/format.go`, JSON, `go generate ./spec` |
| A type-checking rule | `value_types.go`, `type_resolve.go`, `check.go` | Output schema, `gen_types*.go`, `gen_ts_types.go` |
| Matching semantics | `compile.go`, `attrs.go`, `bcompile.go`, `vm.go`, `ivm.go`, `runtime.go` | The generators (`gen_direct*.go`, `gen_flow.go`, `gen_ts.go`) and `runtime/`, `tsrt/` |
| An optimization of generated Go | A pass in `internal/engine/core/` or lowering in `internal/engine/flow/` | `gen_flow.go` printer, [Core and Flow IR](intermediate-representations.md), parity tests |
| The bytecode instruction set | `bytecode.go`, `bcompile.go`, `vm.go`, `ivm.go`, `modulefile.go` | [Bytecode](../../spec/bytecode.md) |
| A Document or stream behavior | `document*.go`, `stream*.go`, `runtime/document.go`, `runtime/stream.go` | The differential tests named in [Development](../development.md) |
| The runtime of generated parsers | `runtime/`, `internal/engine/tsrt/` | `go generate ./parsers ./bench`, `parsers/test.sh` |

## Related Topics

- [Core and Flow IR](intermediate-representations.md): the representations and passes of the generated route.
- [Design records](records/): decisions and their alternatives.
- [Development](../development.md): repository layout, tests and feature status.
- [Optimization catalog](../optimizations/README.md): measured mechanisms.
