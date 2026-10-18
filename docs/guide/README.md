# PEGO Guides

Task-oriented guides to the PEGO grammar language and runtime. Each guide explains how and why to use one feature, with
complete, runnable examples, and points to the [specification](../../spec/README.md) for the exact rules.

## Writing Grammars

| Guide | Contents |
|:--|:--|
| [Rule Expressions](rule-expressions.md) | Writing the matching part of a grammar: ordered choice, repetition and lookahead, cut, white space and tokens, keywords, and the pitfalls of each |
| [Trees and Actions](trees-and-actions.md) | The tree a grammar builds by default, shaping it with `#atomic`, `-` and captures, struct, union and terminal types, actions, built-in functions and lambdas, positions, recipes (`foldl`, flattening lists), performance guidelines |
| [Operators Rule](operators-rule.md) | Operator expressions: Operators Rule Definition Statements, left-recursive rules and precedence chains compared, level-restricted calls, operator forms and associativity, pitfalls, a larger example |
| [Context-Sensitive Parsing](context-sensitive.md) | Predicates, variables, lookahead and attributes; matching tags, indentation blocks, here-documents and other recipes; the cost of variables |
| [Errors and Recovery](errors-and-recovery.md) | How syntax errors are reported, getting them out on the command line and in Go, readable messages with `@error`, cuts, recovering with `#recover`, inserting missing literals, recipes |

## Running Parsers

| Guide | Contents |
|:--|:--|
| [Running Parsers](running-parsers.md) | The Go API: compiling a grammar, start rules, results and errors, concurrency, cancellation, parsing a prefix, position units, backends and how to choose one, nesting limits, recognition mode, memoization |
| [Working With Trees](working-with-trees.md) | Reading a `Node`: fields and positions, printing and JSON, walking and cursors, typed values, output schemas and structural queries |
| [Building Grammars as ASTs and JSON](grammar-asts.md) | Structural validation, formatting, source metadata and JSON loading for tools that build grammars in Go |
| [The `pego` Command](command-line.md) | The command-line workflows: parse, format, fix, convert, compile and generate |

## Incremental and Streaming Input

| Guide | Contents |
|:--|:--|
| [Incremental Parsing](incremental.md) | `Document` for reparsing a text after edits: the model, change notifications, what is reused and why, writing grammars that reuse well, performance and limits, an editor loop, fuzzing edits against fresh parses |
| [Streaming](streaming.md) | `#stream` and `ParseStream` for input that is too big to hold or never ends: what is emitted and when, memory, errors, recipes (summing a column, a header followed by records, cancelling) |

## Shipping Parsers

| Guide | Contents |
|:--|:--|
| [Compiled Grammars](compiled-grammars.md) | Saving a grammar as a `.pegoc` file and loading it: what a file without the AST cannot do, sizes and load times, `go:embed`, versions, untrusted data, and when to use it instead of generated code |
| [Generated Go Parsers](code-generation.md) | Standalone Go parsers with `pego gen`: the generated API, typed values, documents, streams, recognition, keeping generated code up to date, trade-offs |
| [Generated TypeScript Parsers](typescript.md) | Standalone TypeScript modules with `pego gen -lang ts`: the generated API, positions, strings and ints in JavaScript, errors, deep nesting and the JavaScript stack |
| [Using the Ready-Made Parsers](ready-made-parsers.md) | The parsers of `parsers/` (CEL, CSV, CUE, DuckDB SQL, Go, JSON, Python, TypeScript, XML, YAML) in a program: the API they share, choosing an entry point, typed trees, positions and units, errors, conformance and limits |

## Developing Grammars

| Guide | Contents |
|:--|:--|
| [Testing Grammars](testing-grammars.md) | A test suite for a grammar in layers: examples, golden files, fixture suites, properties, generated inputs, fuzzing, reference implementations, equivalence checks, lint and format checks in CI |
| [Debugging and Profiling](debugging.md) | Why an input fails (`pego explain`), what the parser does (`pego trace`), why a parse is slow (`pego profile`), `WithTrace` and `WithProfile` in Go, costs and limits |
| [Linting Grammars](linting.md) | Finding likely mistakes with `pego lint` and `pego.Lint`, what the checks mean and how to fix what they report, suppressing findings |
| [Sampling Inputs and Fuzzing](sampling-and-fuzzing.md) | Generating inputs a grammar accepts with `pego sample` and package `sample`: seeds and size bounds, coverage, near-miss invalid inputs, seeding Go fuzz tests |
| [Editor Support](editor-support.md) | The language server (`pego lsp`): diagnostics, formatting, navigation, hover, rename, completion and semantic highlighting; setting up VS Code, Neovim, Helix and Emacs |
| [The Playground](playground.md) | The browser playground: inspecting trees and errors, sharing grammar state, its limits and the WebAssembly API |
