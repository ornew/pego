# Development

This document describes the repository layout, the architecture of the implementation, how to run the tests, the implementation status of the language features, and the roadmap.

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

## Architecture

```
.pego ──syntax.Parse──▶ grammar.Grammar ──engine.Compile──▶ engine.Program ──Parse──▶ Node
                         ▲      │                              │      ▲
              JSON ──────┘      └── grammar.Format ──▶ .pego   │      └── LoadProgram ── .pegoc
                                                               └── MarshalBinary ──▶ .pegoc
```

| Component | Files | Responsibility |
|:--|:--|:--|
| Syntax | `internal/syntax/` | Parses PEGO source into an AST with source positions |
| Static analysis | `analysis.go` | Nullability, left-recursion leaders, memoization, variable and position dependencies |
| Type checking | `check.go`, `type_inference.go`, `types.go` | Type inference for undeclared rule types; checking of actions, predicates and struct fields |
| Closure compiler | `compile.go` | Compiles parser expressions into Go closures |
| Runtime | `runtime.go`, `memo.go`, `alloc.go`, `parse.go` | Rule calls, memoization, left recursion, backtracking, node allocation, parse entry points |
| Attributes | `attrs.go` | `#error` and `#recover` |
| Input | `input.go` | Position units, incremental reading and discarding of stream input |
| Actions | `eval.go` | Evaluation of action and predicate expressions |
| Pratt expressions | `pratt.go` | The Pratt loop and longest-match operator selection |
| Incremental parsing | `document.go`, `resume.go` | Reuse of memo entries across edits; resuming long repetitions |
| Tracing and profiling | `trace.go`, `profile.go` | Rule call events for `WithTrace`, the per-rule profile and its hints |
| Language server | `internal/lsp/` | Analyzes open `.pego` documents (tokens, partial parse, compile, rule types) and answers LSP requests |
| Bytecode | `bytecode.go`, `bcompile.go`, `disasm.go`, `vm.go`, `ivm.go` | Bytecode module, compiler, disassembler, recursive and iterative VMs |
| Compiled grammars | `compiled.go`, `modulefile.go` | The `.pegoc` file format |
| Code generation | `gen.go`, `gen_direct.go`, `genrt/`, `gen_ts.go`, `tsrt/` | Generation of standalone Go and TypeScript parsers |

Files without a directory are in `internal/engine/`. The sections below describe each component.

### Syntax

`internal/syntax` uses a hand-written lexer and recursive-descent parser to build an AST with source positions. When a definition contains an error, parsing resumes at the next definition, so a single run reports errors in several definitions. The parser also records comments and line breaks in the AST (`grammar.LineBreak`, excluded from JSON), which `grammar.Format` uses to format source without losing comments ([design](design/011-source-formatting.md)).

### Closure compiler

The compiler (`compile.go`) turns each parser expression into a function (a closure) that tries to match at the current input position. Where no value is needed (`@`, `-`, lookahead, and the bodies of terminal-type rules and of rules whose action does not use `$n`), it generates a value-free version, and for CST rules called from such places it also generates a value-free version of the rule.

### Static analysis

The analysis (`analysis.go`) computes nullability, the strongly connected components of the left-call graph, and the variables each rule reads (directly or through its callees), and from these decides for each rule whether it is memoized and whether it is a left-recursion leader. Rules that read variables are memoized per combination of those variables' values at the call.

The left-call graph excludes zero-count repetition bodies and ordered-choice suffixes only when an earlier
alternative is proven to succeed on every input. `internal/grammaranalysis` supplies this conservative proof,
including cut scopes and recovery skips. Recovery skip calls start at the base expression's original position and
participate unless the base is proven successful. Graph refinement repeats to a fixed point: removing an unreachable
cycle can make another rule's success provable. Cyclic calls can read a failing seed and cannot prove success.
Unknown predicates, Pratt and level-restricted calls remain conservative; the runtime grammar is not rewritten.

### Runtime

The runtime (`runtime.go`) memoizes rule calls keyed by (rule, position, level), which is packrat parsing. Rules that call no other rule, and rules referenced only once in the grammar, are not memoized in normal parses, because their memo entries would never be reused; `Document` still memoizes them so that edits can reuse their results. In whole-input parses, the other rules are memoized at a position only from their second call there (`parser.firstCall`; a bit set records first calls), unless repeated calls of the rule turn out to be frequent, in which case it is memoized from the first call for the rest of the parse. Either way a rule is evaluated at most twice per position, so parse time stays linear.

Left recursion is handled as in CPython's pegen (see the [reference note](ref-cpython-pegen-packrat-parsing.md)): only the leader rule, which breaks the cycle, is evaluated by growing the seed, and the other rules in the cycle are not memoized.

Backtracking returns to a recorded point: the position, the variable environment (a persistent list) and the history of capture writes.
The current environment holds at most one binding per variable name. Replacing a value copies only the preceding
bindings, so saved environments remain immutable; assigning an equal value reuses the existing environment.
Lookups depend on distinct names rather than assignment history. A parse-wide name hint avoids scanning for first
assignments and records names from the first assignment, including abandoned branches.

Nodes, child lists, field lists and capture frames are allocated from per-parse slabs (`alloc.go`), and the memo table is a per-position list of entries (`memo.go`). Scratch state of action and predicate evaluation lives in buffers owned by the parser and reused: an operand stack (`estack`, shared by the VMs' expression code and the closure backend's built-in calls), arenas for lambda calls, and the stack that gathers list elements (`kidStack`). A whole-input parse takes its decoded input, offset table, memo table and `kidStack` from a pool kept by the `Program` and returns them when it is done (`newPooledParser`). A call of a rule that is not memoized and has no captures takes a shorter path (`invokePlain`) than a full invocation. See [performance.md](performance.md) for the measurements behind these choices.

Three parse options affect the runtime as a whole:

- **Recognition only** (`pego.RecognizeOnly`): the parser builds no tree and only checks whether the input matches, returning the same syntax errors as a full parse. Actions are not evaluated. It is not available for stream parsing or `Document`.
- **Nesting limit** (`pego.WithMaxDepth`): rule calls may nest at most 100,000 deep by default (10,000,000 for the iterative VM); deeper nesting is reported as an error.
- **Tracing** (`pego.WithTrace`, `pego.WithProfile`): every rule call is reported to a function at its start and end. Calls are observed in `parser.call` (and the iterative VM's `traceFrame`); a traced parse sends plain calls through `call` too (`parser.noPlain`). An untraced parse pays one nil check per call through `call`. Calls that a `Document` skips by resuming a repetition are not reported. See [design 014](design/014-tracing-and-profiling.md).

### Attributes

`#error` (`attrs.go`) records the expectations inside its expression separately and replaces them with its message. `#recover` skips input when its expression fails and returns an `Error` node. Recovered errors are recorded so that backtracking can undo them, and they are stored in memo entries.

### Input

The input (`input.go`) is held in the chosen position unit (code points or UTF-8 bytes), and reading a character returns the character and its size. The tests check that byte-unit results, converted to code-point positions, equal the code-point results.

### Streaming

For stream parsing, input is read from a `bufio.Reader` only as far as needed (`fill` in `input.go`). Byte-unit decoding requests one byte first and reads further only while the UTF-8 prefix is incomplete; a complete character never requires unrelated future bytes. Each time an element of a `#stream` repetition is emitted, the input before it (except the preceding character) and the memo entries can be discarded (`commit` in `input.go`): the read buffer is compacted once half of it is consumed, and the memo is pruned in blocks. At element boundaries the parser also starts new allocation chunks from time to time (`splitChunks`), so that chunks shared with earlier elements do not keep them reachable. Positions remain absolute offsets from the start of the input; line and column numbers are computed by counting the lines in the discarded part. (For whole input, errors look their line up in a table of line starts.)

### Incremental parsing

Each memo entry records the range of input it examined (`document.go`). After an edit, entries that lie entirely before the edit are reused as they are, and entries that lie entirely after it are reused with shifted positions ([design](design/007-streaming-and-incremental-parsing.md)). Completed calls that depend on an unfinished left-recursion seed are marked transitively and invalidated after any edit: their own examined range may omit input influencing the final seed. Finishing a left-recursion head discharges its own growing-seed dependencies, while completed intermediate dependencies remain marked. Repetitions that an action only takes apart with `map($x, (e) => $e.f)` are compiled to gather the field directly in every backend (`project.go`). An edit splices the text, its offset table and the memo table in place (`input.replace`, `memoTable.splice`) and is recorded in the document's edit log. The memo table is a gap buffer, and an edit decides only the entries at the edited positions; any other entry applies the edits since its last use when it is next looked up (`advanceEntry`). A shifted entry's nodes are moved in place when the entry is first reused (`moveResult`): each node records how many edits its positions account for (`Node.gen`), and an edit never falls inside a reused node, so a non-empty node's positions tell which later edits move it. Empty nodes at an insertion point are ambiguous (they can belong to results on both sides) and are copied instead. A repetition of 16 elements or more records its run (in every backend; the VMs find such repetitions in the bytecode), and after an edit it reuses the elements before the edit and, once its elements line up with the old ones again, the rest of the old run (`resumeRepeat`), so that a reparse need not look up every line of a file in the memo table.

### Code generation

The generator (`gen.go`, `genrt/`) emits one Go method per expression and embeds `genrt/runtime.go`, a runtime that behaves like the engine, producing a parser that depends only on the standard library. The tests build the generated parsers and check that they return the same results as the engine ([design](design/008-code-generation.md)). With `GenOptions.Recognize`, it also generates the recognizer (`Program.recognizer`) into a second rule table, behind `Recognize`. With `GenOptions.Types` (`gen_types.go`), it also emits a Go type for each grammar type, from the types the type checker inferred (`Program.typed`), and `ParseAST`, which builds them with a second, typed runtime (`genrt/typed.go`, with the rules generated again for it, most of which `gen_direct.go` compiles into a single method each: the body inlined, captures in Go variables, the action in place; rules with cuts, `#recover`, Pratt expressions and left-recursion leaders keep a method per expression) or, for grammars whose results include CST values, converts the tree of `Parse` ([design](design/012-typed-values.md)).

The TypeScript generator (`gen_ts.go`) walks the same analysis results and emits one function per expression into a module that embeds `tsrt/runtime.ts`, a port of `genrt/runtime.go` that hides JavaScript's differences (UTF-16 strings, 53-bit numbers, JSON escaping). `TestGeneratedTSParsersMatchEngine` runs the generated modules with Node.js on the corpus of the Go generator's test, plus prefixes and byte deletions of short inputs. It compares the JSON, `Node.String` and recognition with the engine, and type-checks the modules with `tsc` ([design](design/013-typescript-generation.md)).

### Linting

Package `internal/lint` analyzes the AST of a grammar that compiles (`pego.Lint` compiles it first). `analysis.go` computes nullability (separately at the end of the input and elsewhere), whether expressions can match at all (least fixed points), whether an expression certainly succeeds on every input that begins with a given string (`matchPrefix`), a prefix every match begins with, and structural equality. Calls of left-recursive rules are never taken as certain. `checks.go` builds the checks on these so that errors and warnings are proven; hints are heuristics. `lint:ignore` comments are read from `Grammar.AllComments` ([design](design/018-grammar-linting.md)).

### Language server

`internal/lsp` implements LSP 3.17 over standard input and output with the standard library. Each version of an open document is analyzed once: `syntax.Tokenize` gives every token with its span, `syntax.ParsePartial` the definitions that parse even when others have errors, and, when there is no syntax error, `engine.Compile` the compile and type errors and `Program.RuleType` the declared or inferred rule types. Positions are converted through byte offsets between LSP's UTF-16 columns and line ends and PEGO's code-point columns. Completion works on tokens, since the definition being written rarely parses ([design](design/017-language-server.md)).

### Input generation

Package `sample` walks the grammar AST of a `Parser` with a bounded, seeded depth-first search in continuation-passing style (`gen.go`, `pratt.go`), prunes candidates that the parser would read differently with checks evaluated by a partial matcher (`match.go`) and with predicates evaluated on the generated text (`pred.go`), and returns only inputs that `Parse` accepts. It measures coverage of rules, alternatives and Pratt operands and operators (`analysis.go`) and mutates valid inputs into near-miss invalid ones (`mutate.go`) ([design](design/015-input-generation.md)).

### Compiled grammars

A compiled grammar file (`compiled.go`, `modulefile.go`) stores the bytecode module and, optionally, the AST and the static-analysis results (version 2; the format is defined in [bytecode.md](../spec/bytecode.md#file-format)). Loading skips parsing, static analysis, type checking and compilation to bytecode. A file without the AST can be executed only by the bytecode backends. Version 1 files (AST only) can still be loaded ([design](design/009-compiled-grammar-format.md)).

### Bytecode

The bytecode compiler (`bytecode.go`, `bcompile.go`, `disasm.go`) compiles a program into a language-independent module of tables and instruction sequences. The instruction set is defined in [bytecode.md](../spec/bytecode.md).

- The **recursive VM** (`vm.go`) uses the runtime shared with the closure backend (`runtime.go`, `pratt.go`) for rule calls, memoization, left recursion and Pratt operator selection, and executes rule bodies, actions and predicates as bytecode.
- The **iterative VM** (`ivm.go`) calls the same runtime functions from a state machine kept on its own stack instead of the host call stack.

The backend is selected with `ParseOptions.Backend` (`pego.WithBackend` in the public API, `-backend` in the CLI). The tests check that every backend returns the same result as the closure backend for every grammar and input ([design](design/010-bytecode-vm.md)).

### Type checking

The type checker (`check.go`, `type_inference.go`, `types.go`) processes inferred-rule dependency components in
callee-first order, then checks actions, predicates and struct fields. Acyclic rules are inferred once; recursive
components iterate using canonical type keys that ignore union-member order. Pratt operator actions introduce
implicit self-dependencies. Per-rule recursive growth budgets and repeating-state detection permanently widen only
affected rules to `any`, retaining stable peers and unrelated finite types ([specification](../spec/type-checking.md#type-inference)).

### Actions

`eval.go` evaluates action and predicate expressions.

### Pratt expressions

`pratt.go` tries every candidate operator part, selects the longest match, and evaluates the expression with a Pratt loop that recurses with the level (`min`) as an argument.

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

The [living backlog](https://github.com/ornew/pego/issues/1) is the source of individual defects, priorities and
landing commits. Its body lists open work and links to dedicated priority, progress and category completion
comments. Completed items move to their category comment with the landing commit. The 2026-10-09 audit found
correctness/resource gaps despite the implemented feature coverage above.
Action-variable memo keys (C01), interrupted Document cache cleanup (C05), engine memo retirement (C02/P01) and
nil-action construction tracking cleanup (C03, including generated runtimes) and bounded persistent variable
bindings (C04/P02, including generated runtimes), safe YAML directive validation (M01) and iterative Pratt depth
accounting (C06), finite stream/zero repetition bounds (C07) and adjacent-minus formatting integrity (C09) are
implemented, along with staged in-place formatting writes (T01) and canonical terminal/struct aliases across
engines, saved grammars and generated parsers (C10). Full-width repetition bounds (C08) preserve the grammar's
implementation-int range through compact wide instructions ([design](design/020-wide-repetition-bounds.md)).
Hidden/indirect left-recursion reuse (C20) tracks completed calls that depend on unfinished seeds and invalidates
them after edits; finalized heads without outer-seed or completed intermediate dependencies retain range-based reuse.
Left-recursion reachability (C21/C26) excludes proven-unreachable choice suffixes and zero-count repetition bodies,
includes possible recovery skip calls, and refines the graph to stability. The linter also accounts for recovery skip
cuts (C25). Dependency-ordered inference (C11) preserves long finite chains and unrelated rule types while bounding evolving
recursive types. JSON/public AST validation (C12) rejects malformed children, pointer cycles, negative indexes,
invalid repetition bounds and trailing JSON. Its public structural API is implemented ([design](design/021-grammar-ast-validation.md));
opt-in strict JSON intake remains proposed (F02).
Bytecode lowering also canonicalizes every negative public-AST repetition maximum to -1 (C27), preserving
unbounded semantics in ordinary/wide REPEAT and SCAN instructions and saved modules without mutating the AST.
Byte-position streams emit complete elements without waiting for unrelated future bytes (C13), while split UTF-8
prefixes still require continuation bytes or actual EOF. Replacement-character literals match invalid UTF-8 input in
both position units across engines and generated Go/TS (C14), preserving encoded-byte success paths and matched
input text; byte-mode text predicates can still distinguish invalid bytes from encoded U+FFFD. Follow with
raw-invalid-byte AST literal normalization/validation. Byte Document edits follow decoded UTF-8 boundaries,
accepting separate invalid bytes, and invalidate EOF-truncated decoding when appended bytes complete a character
(C15); deterministic malformed-input edits agree with fresh parses across backends and units.
Unicode escapes and public AST/JSON character-range endpoints must be scalar values (C16), with ordered range
bounds; valid endpoints may span the surrogate interval. YAML's redundant surrogate exclusions are removed.
`BenchmarkCharacterRangeValidation` measures AST validation/compilation after source parsing, while
`BenchmarkUnicodeEscapeSyntax` measures source intake. YAML's `BenchmarkUnicodeClasses` uses 500 sequence entries
with ASCII/Unicode mapping values to measure generated Parse, ParseAST and Recognize in both position units.
Integer and positional-reference source tokens share ASCII decimal digits and implementation-int bounds
(C17); overflow or unsupported digits produce ranged diagnostics. Editor tokens retain original digits,
including invalid indices and leading zeros; Unicode identifiers and `$0` semantics remain supported.
`BenchmarkDecimalSource` compares valid plain, integer-heavy and positional-reference-heavy source intake.
Earlier backlog items now share current category IDs and priorities;
their former L001–L058 labels are provenance only. Full benchmark
results were refreshed at the streaming-memory checkpoint on 2026-10-09 (`f8d6c2a`); tuning entries carry focused
optimization measurements, and correctness-only performance impacts are recorded in commit messages. The full suite
includes batch, recognition, incremental, stream and preparation workloads.

The [incremental document and tree tooling proposal](design/019-incremental-document-and-tree-tooling.md) examines
input updates, saved trees and downstream editor work. These stages are proposed, not implemented APIs or measured speedups. Preserve current PEG and mutable
Document/Node contracts while designing additions; large changes need a dedicated design record.

| Order after correctness gates | Planned capability | Backlog |
|:--|:--|:--|
| 1 | Cancellation and explicit work budgets | F01 |
| 2 | Replayable edit fuzzing and grammar fixtures | F16, F06 |
| 3 | Iterative Walker/Cursor and structured output schemas | F10, F17 |
| 4 | Chunked Document input, measured against contiguous input | P10 |
| 5 | Immutable/versioned snapshots and shared trees/sequences | F04 |
| 6 | Conservative output change notifications, then structural queries/highlighting | F18, F19 |
| 7 | Explicit missing-token recovery for editing | F20 |
| 8 | Measured multi-character literal dispatch and compact-leaf experiments | P11, P12 |

Other backlog features remain tracked; this order captures editor dependencies rather than removing them. Existing
memo dependency tracking, lazy edit shifts and repetition resumption remain the foundation. Generated streams and
Document, typed TypeScript, other-language generators and source maps for fragments require separate portable contracts.

- [x] **AST construction:** implement every feature related to `->` actions (variable references, `fold`, `new`).
- [x] **Detailed error reporting:** report the location (line and column) and the expected tokens when a parse fails.
- [x] **Predicates:** implement variables to support context-sensitive grammars.
- [ ] **Self-hosting:** write the parser for `.pego` files in PEGO itself (it is currently a hand-written Go parser).
- [x] **Error recovery:** consume a grammar-specified skip on failure, returning error nodes and continuing parsing.
- [x] **Documentation:** provide detailed documentation and tutorials for each feature ([tutorial](tutorial/getting-started.md), [guides](guide/README.md)).
- [x] **Go code generator:** generate Go parser code that can be compiled and run directly.
- [x] **Bytecode VM:** a language-independent bytecode and VMs for two execution models, recursive and iterative ([design](design/010-bytecode-vm.md)).
- [x] **Editor support:** a language server (`pego lsp`) and a VS Code extension ([design](design/017-language-server.md)).
- [x] **TypeScript code generator:** standalone TypeScript parsers that return the engine's results ([design](design/013-typescript-generation.md)).
- [ ] **Code generators for other languages:** generate parsers in Python and other languages.
- [x] **Streaming:** consume input as a stream and emit nodes as a stream.
- [x] **Incremental parsing:** reuse memoized rules and long repetition results after edits in a mutable Document.
