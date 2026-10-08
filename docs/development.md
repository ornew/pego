# Development

This document describes the repository layout, the architecture of the implementation, how to run the tests, the implementation status of the language features, and the roadmap.

## Directory layout

| Path | Contents |
|:--|:--|
| `pego.go` | Public API of package `pego` (`ParseGrammar`, `Compile`, `CompileSource`, `Parser.Parse`, and others) |
| `grammar/` | Grammar AST, JSON conversion (`MarshalJSON`, `UnmarshalJSON`) and formatting as PEGO source (`Format`, which preserves comments). Public package. |
| `internal/syntax/` | Lexer and parser for PEGO source code (source → `grammar.Grammar`) |
| `internal/engine/` | Compiler, static analysis, parser runtime, action evaluation, bytecode VMs and Go and TypeScript code generation |
| `internal/engine/genrt/` | Runtime code embedded in generated Go parsers |
| `internal/engine/tsrt/` | Runtime code embedded in generated TypeScript parsers |
| `cmd/pego/` | Command-line tool |
| `examples/` | Example grammars, with golden tests |
| `playground/` | WebAssembly API used by the web playground (package `main`; `GOOS=js GOARCH=wasm`) |
| `site/` | Web site generator (a separate Go module): landing page, docs, reference and playground; build with `site/build.sh` |
| `netlify.toml` | Netlify build configuration for the site |
| `bench/` | Benchmarks comparing the backends with each other and with standard-library parsers (`gen/` holds generated parsers). Results are in [benchmarks.md](benchmarks.md). |
| `spec/` | Language specification |
| `docs/tutorial/` | Tutorials (getting started) |
| `docs/guide/` | Task-oriented guides to each feature ([index](guide/README.md)) |
| `docs/design/` | Design records |

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
| Type checking | `check.go`, `types.go` | Type inference for undeclared rule types; checking of actions, predicates and struct fields |
| Closure compiler | `compile.go` | Compiles parser expressions into Go closures |
| Runtime | `runtime.go`, `memo.go`, `alloc.go`, `parse.go` | Rule calls, memoization, left recursion, backtracking, node allocation, parse entry points |
| Attributes | `attrs.go` | `#error` and `#recover` |
| Input | `input.go` | Position units, incremental reading and discarding of stream input |
| Actions | `eval.go` | Evaluation of action and predicate expressions |
| Pratt expressions | `pratt.go` | The Pratt loop and longest-match operator selection |
| Incremental parsing | `document.go`, `resume.go` | Reuse of memo entries across edits; resuming long repetitions |
| Tracing and profiling | `trace.go`, `profile.go` | Rule call events for `WithTrace`, the per-rule profile and its hints |
| Bytecode | `bytecode.go`, `bcompile.go`, `disasm.go`, `vm.go`, `ivm.go` | Bytecode module, compiler, disassembler, recursive and iterative VMs |
| Compiled grammars | `compiled.go`, `modulefile.go` | The `.pegoc` file format |
| Code generation | `gen.go`, `genrt/`, `gen_ts.go`, `tsrt/` | Generation of standalone Go and TypeScript parsers |

Files without a directory are in `internal/engine/`. The sections below describe each component.

### Syntax

`internal/syntax` uses a hand-written lexer and recursive-descent parser to build an AST with source positions. When a definition contains an error, parsing resumes at the next definition, so a single run reports errors in several definitions. The parser also records comments and line breaks in the AST (`grammar.LineBreak`, excluded from JSON), which `grammar.Format` uses to format source without losing comments ([design](design/011-source-formatting.md)).

### Closure compiler

The compiler (`compile.go`) turns each parser expression into a function (a closure) that tries to match at the current input position. Where no value is needed (`@`, `-`, lookahead, and the bodies of terminal-type rules and of rules whose action does not use `$n`), it generates a value-free version, and for CST rules called from such places it also generates a value-free version of the rule.

### Static analysis

The analysis (`analysis.go`) computes nullability, the strongly connected components of the left-call graph, and the variables each rule reads (directly or through its callees), and from these decides for each rule whether it is memoized and whether it is a left-recursion leader. Rules that read variables are memoized per combination of those variables' values at the call.

### Runtime

The runtime (`runtime.go`) memoizes rule calls keyed by (rule, position, level), which is packrat parsing. Rules that call no other rule, and rules referenced only once in the grammar, are not memoized in normal parses, because their memo entries would never be reused; `Document` still memoizes them so that edits can reuse their results. In whole-input parses, the other rules are memoized at a position only from their second call there (`parser.firstCall`; a bit set records first calls), unless repeated calls of the rule turn out to be frequent, in which case it is memoized from the first call for the rest of the parse. Either way a rule is evaluated at most twice per position, so parse time stays linear.

Left recursion is handled as in CPython's pegen (see the [reference note](ref-cpython-pegen-packrat-parsing.md)): only the leader rule, which breaks the cycle, is evaluated by growing the seed, and the other rules in the cycle are not memoized.

Backtracking returns to a recorded point: the position, the variable environment (a persistent list) and the history of capture writes.

Nodes, child lists, field lists and capture frames are allocated from per-parse slabs (`alloc.go`), and the memo table is a per-position list of entries (`memo.go`). Scratch state of action and predicate evaluation lives in buffers owned by the parser and reused: an operand stack (`estack`, shared by the VMs' expression code and the closure backend's built-in calls), arenas for lambda calls, and the stack that gathers list elements (`kidStack`). A call of a rule that is not memoized and has no captures takes a shorter path (`invokePlain`) than a full invocation. See [performance.md](performance.md) for the measurements behind these choices.

Three parse options affect the runtime as a whole:

- **Recognition only** (`pego.RecognizeOnly`): the parser builds no tree and only checks whether the input matches, returning the same syntax errors as a full parse. Actions are not evaluated. It is not available for stream parsing or `Document`.
- **Nesting limit** (`pego.WithMaxDepth`): rule calls may nest at most 100,000 deep by default (10,000,000 for the iterative VM); deeper nesting is reported as an error.
- **Tracing** (`pego.WithTrace`, `pego.WithProfile`): every rule call is reported to a function at its start and end. Calls are observed in `parser.call` (and the iterative VM's `traceFrame`); a traced parse sends plain calls through `call` too (`parser.noPlain`). An untraced parse pays one nil check per call through `call`. Calls that a `Document` skips by resuming a repetition are not reported. See [design 014](design/014-tracing-and-profiling.md).

### Attributes

`#error` (`attrs.go`) records the expectations inside its expression separately and replaces them with its message. `#recover` skips input when its expression fails and returns an `Error` node. Recovered errors are recorded so that backtracking can undo them, and they are stored in memo entries.

### Input

The input (`input.go`) is held in the chosen position unit (code points or UTF-8 bytes), and reading a character returns the character and its size. The tests check that byte-unit results, converted to code-point positions, equal the code-point results.

### Streaming

For stream parsing, input is read from a `bufio.Reader` only as far as needed (`fill` in `input.go`). Each time an element of a `#stream` repetition is emitted, the input before it (except the preceding character) and the memo entries can be discarded (`commit` in `input.go`): the read buffer is compacted once half of it is consumed, and the memo is pruned in blocks. At element boundaries the parser also starts new allocation chunks from time to time (`splitChunks`), so that chunks shared with earlier elements do not keep them reachable. Positions remain absolute offsets from the start of the input; line and column numbers are computed by counting the lines in the discarded part. (For whole input, errors look their line up in a table of line starts.)

### Incremental parsing

Each memo entry records the range of input it examined (`document.go`). After an edit, entries that lie entirely before the edit are reused as they are, and entries that lie entirely after it are reused with shifted positions ([design](design/007-streaming-and-incremental-parsing.md)). Repetitions that an action only takes apart with `map($x, (e) => $e.f)` are compiled to gather the field directly in every backend (`project.go`). An edit splices the text, its offset table and the memo table in place (`input.replace`, `memoTable.splice`) and is recorded in the document's edit log. The memo table is a gap buffer, and an edit decides only the entries at the edited positions; any other entry applies the edits since its last use when it is next looked up (`advanceEntry`). A shifted entry's nodes are moved in place when the entry is first reused (`moveResult`): each node records how many edits its positions account for (`Node.gen`), and an edit never falls inside a reused node, so a non-empty node's positions tell which later edits move it. Empty nodes at an insertion point are ambiguous (they can belong to results on both sides) and are copied instead. In the closure backend, a repetition of 16 elements or more records its run, and after an edit it reuses the elements before the edit and, once its elements line up with the old ones again, the rest of the old run (`resumeRepeat`), so that a reparse need not look up every line of a file in the memo table.

### Code generation

The generator (`gen.go`, `genrt/`) emits one Go method per expression and embeds `genrt/runtime.go`, a runtime that behaves like the engine, producing a parser that depends only on the standard library. The tests build the generated parsers and check that they return the same results as the engine ([design](design/008-code-generation.md)). With `GenOptions.Recognize`, it also generates the recognizer (`Program.recognizer`) into a second rule table, behind `Recognize`. With `GenOptions.Types` (`gen_types.go`), it also emits a Go type for each grammar type, from the types the type checker inferred (`Program.typed`), and `ParseAST`, which builds them with a second, typed runtime (`genrt/typed.go`, with the rules generated again for it) or, for grammars whose results include CST values, converts the tree of `Parse` ([design](design/012-typed-values.md)).

The TypeScript generator (`gen_ts.go`) walks the same analysis results and emits one function per expression into a module that embeds `tsrt/runtime.ts`, a port of `genrt/runtime.go` that hides JavaScript's differences (UTF-16 strings, 53-bit numbers, JSON escaping). `TestGeneratedTSParsersMatchEngine` runs the generated modules with Node.js on the corpus of the Go generator's test, plus prefixes and byte deletions of short inputs. It compares the JSON, `Node.String` and recognition with the engine, and type-checks the modules with `tsc` ([design](design/013-typescript-generation.md)).

### Compiled grammars

A compiled grammar file (`compiled.go`, `modulefile.go`) stores the bytecode module and, optionally, the AST and the static-analysis results (version 2; the format is defined in [bytecode.md](bytecode.md#file-format)). Loading skips parsing, static analysis, type checking and compilation to bytecode. A file without the AST can be executed only by the bytecode backends. Version 1 files (AST only) can still be loaded ([design](design/009-compiled-grammar-format.md)).

### Bytecode

The bytecode compiler (`bytecode.go`, `bcompile.go`, `disasm.go`) compiles a program into a language-independent module of tables and instruction sequences. The instruction set is defined in [bytecode.md](bytecode.md).

- The **recursive VM** (`vm.go`) uses the runtime shared with the closure backend (`runtime.go`, `pratt.go`) for rule calls, memoization, left recursion and Pratt operator selection, and executes rule bodies, actions and predicates as bytecode.
- The **iterative VM** (`ivm.go`) calls the same runtime functions from a state machine kept on its own stack instead of the host call stack.

The backend is selected with `ParseOptions.Backend` (`pego.WithBackend` in the public API, `-backend` in the CLI). The tests check that every backend returns the same result as the closure backend for every grammar and input ([design](design/010-bytecode-vm.md)).

### Type checking

The type checker (`check.go`, `types.go`) infers the types of rules without a declared type by fixed-point iteration, and then checks the types of actions, predicates and struct fields.

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
| Done | | Error recovery (`#recover`) and error messages (`#error`) |
| Done | Actions | Actions (`->`) |
| Done | | Variables (`x`, `x = 1`) |
| Done | | Capture references (`$label`, `$1`) |
| Done | | Built-in functions (`foldl`, `len`, `text`, and others) |
| Done | | Constructors (`new`) |
| Done | Advanced | Left recursion |
| Done | | Parsing PEGO source (`.pego`) |
| Done | | Pratt expressions (`pratt`) |
| Done | | Stream parsing (`#stream`) |
| Done | | Incremental parsing (`Document`) |
| Done | | Code generation (Go, `pego gen`) |
| Done | | Typed values in generated parsers (`pego gen -types`) |
| Done | | Saving and loading compiled grammars (`.pegoc`) |
| Done | | Code generation (TypeScript, `pego gen -lang ts`) |
| Done | | Tracing, profiling and error explanation (`WithTrace`, `WithProfile`, `pego trace/profile/explain`) |

## Roadmap

- [x] **AST construction:** implement every feature related to `->` actions (variable references, `fold`, `new`).
- [x] **Detailed error reporting:** report the location (line and column) and the expected tokens when a parse fails.
- [x] **Predicates:** implement variables to support context-sensitive grammars.
- [ ] **Self-hosting:** write the parser for `.pego` files in PEGO itself (it is currently a hand-written Go parser).
- [x] **Error recovery:** treat local errors as error nodes and continue parsing the rest of the input.
- [x] **Documentation:** provide detailed documentation and tutorials for each feature ([tutorial](tutorial/getting-started.md), [guides](guide/README.md)).
- [x] **Go code generator:** generate Go parser code that can be compiled and run directly.
- [x] **Bytecode VM:** a language-independent bytecode and VMs for two execution models, recursive and iterative ([design](design/010-bytecode-vm.md)).
- [x] **TypeScript code generator:** standalone TypeScript parsers that return the engine's results ([design](design/013-typescript-generation.md)).
- [ ] **Code generators for other languages:** generate parsers in Python and other languages.
- [x] **Streaming:** consume input as a stream and emit nodes as a stream.
- [x] **Incremental parsing:** update a parse efficiently after an edit.
