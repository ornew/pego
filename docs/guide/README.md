# PEGO Guides

Task-oriented guides to the PEGO grammar language and runtime. Each guide explains how and why to use one feature, with
complete, runnable examples, and points to the [specification](../../spec/README.md) for the exact rules.

## Writing Grammars

| Guide | Contents |
|:--|:--|
| [Rule Expressions](rule-expressions.md) | Writing the matching part of a grammar: ordered choice, repetition and lookahead, cut, white space and tokens, keywords, and the pitfalls of each |
| [Shaping Trees](shaping-trees.md) | The tree a grammar builds by default, and shaping it with `#atomic`, `-` and captures; performance guidelines |
| [Declaring Types](types.md) | Struct and union types, optional and list fields, rule result types and inference |
| [Actions](actions.md) | Actions, built-in functions and lambdas, positions of constructed nodes, recipes (`foldl`, flattening lists) |
| [Operators Rule](operators-rule.md) | Operator expressions: Operators Rule Declaration Statements, left-recursive rules and precedence chains compared, level-restricted calls, operator forms and associativity, pitfalls, a larger example |
| [Context-Sensitive Parsing](context-sensitive.md) | Predicates, variables, lookahead and attributes; matching tags, indentation blocks, here-documents and other recipes; the cost of variables |
| [Error Messages](error-messages.md) | How syntax errors are reported, getting them out on the command line and in Go, readable messages with `@error`, cuts |
| [Recovering from Errors](recovery.md) | Recovering with `#recover`, what happens to the errors, handling them in Go, inserting missing literals, recipes |

## Running Parsers

| Guide | Contents |
|:--|:--|
| [Running Parsers](running-parsers.md) | The Go API: the pipeline from source to parser, compiling, start rules, results and errors, concurrency, choosing a feature |
| [Choosing a Backend](backends.md) | The closure, bytecode and iterative backends and how to choose one, the nesting limit |
| [Recognition Mode](recognition-mode.md) | `RecognizeOnly`: checking input without building a tree, what it still evaluates and where it does not apply |
| [Memoization](memoization.md) | What the packrat memo table remembers, when, and what it means for memory, streaming and incremental parsing |
| [Cancellation and Work Limits](cancellation-and-limits.md) | `WithContext` and `WithWorkLimits`: stopping a parse and what it returns |
| [Position Units](position-units.md) | Code points or bytes: what the unit changes, how to choose it, invalid UTF-8 |
| [Parsing a Prefix](prefix-parsing.md) | `ParsePrefix`: parsing a fragment of a larger text and reading how much it consumed |
| [Building Grammars as ASTs and JSON](grammar-asts.md) | Structural validation, formatting, source metadata and JSON loading for tools that build grammars in Go |

## Using the Results

| Guide | Contents |
|:--|:--|
| [Reading Trees](reading-trees.md) | Reading a `Node`: values and positions, printing and JSON, a whole tree, typed values |
| [Walking Trees](walking-trees.md) | `Walk` and `Cursor`: visiting nodes in order and selecting them by position |
| [Output Schemas and Structural Queries](output-schemas-and-queries.md) | Describing the output of a grammar, comparing schemas, finding nodes by their shape |

## Incremental and Streaming Input

| Guide | Contents |
|:--|:--|
| [Incremental Parsing](incremental.md) | `Document` for reparsing a text after edits: the model, the API, keeping a result, a walkthrough with `Stats` |
| [What Incremental Parsing Reuses](incremental-reuse.md) | Which results survive an edit and why, and writing grammars that reuse well |
| [Incremental Parsing Performance](incremental-performance.md) | Measurements of reparsing after an edit, and the limits of a `Document` |
| [Document Change Notifications](document-changes.md) | Recording what an edit and a parse changed, for tools that keep an outline, an index or highlights |
| [Streaming](streaming.md) | `#stream` and `ParseStream` for input that is too big to hold or never ends: what is emitted and when, memory, errors, cancelling |

## Shipping Parsers

| Guide | Contents |
|:--|:--|
| [Compiled Grammars](compiled-grammars.md) | Saving a grammar as a `.pegoc` file and loading it: what a file without the AST cannot do, sizes and load times, `go:embed`, versions, untrusted data, and when to use it instead of generated code |
| [Generating Parsers](code-generation.md) | Standalone parsers with `pego gen`: why generate, the command and its options, what is supported, keeping generated code up to date, trade-offs |
| [The Generated Go API](generated-go-api.md) | The Go package `pego gen` writes: `Parse`, `ParseRule`, nodes and errors, recognition, call options, cancellation and work limits |
| [Typed Values in Generated Go](generated-typed-values.md) | `-types`: a Go type for each grammar type, `ParseAST`, the type mapping, speed and named rules |
| [Incremental Documents in Generated Go](generated-documents.md) | `-document`: the generated `Document`, `Edit` and reparse, what is kept, diagnostics and limits |
| [Streams in Generated Go](generated-streams.md) | `-stream`: `ParseStream`, `Iterate`, diagnostic controls and typed elements |
| [Generated TypeScript Parsers](typescript.md) | Standalone TypeScript modules with `pego gen -lang ts`: the generated API, positions, strings and ints in JavaScript, errors, deep nesting and the JavaScript stack |
| [Using the Ready-Made Parsers](ready-made-parsers.md) | The parsers of `parsers/` (CEL, CSV, CUE, DuckDB SQL, Go, JSON, Python, TypeScript, XML, YAML) in a program: the API they share, choosing an entry point, typed trees, positions and units, errors, conformance and limits |

## Developing Grammars

| Guide | Contents |
|:--|:--|
| [The `pego` Command](command-line.md) | The command-line workflows: parse, format, fix, convert, compile and generate |
| [Testing Grammars](testing-grammars.md) | A test suite for a grammar in layers: examples, golden files, fixture suites, properties, generated inputs, fuzzing, reference implementations, equivalence checks, lint and format checks in CI |
| [Debugging and Profiling](debugging.md) | Why an input fails (`pego explain`), what the parser does (`pego trace`), why a parse is slow (`pego profile`), `WithTrace` and `WithProfile` in Go, costs and limits |
| [Linting Grammars](linting.md) | Finding likely mistakes with `pego lint` and `pego.Lint`, what the checks mean and how to fix what they report, suppressing findings |
| [Sampling Inputs and Fuzzing](sampling-and-fuzzing.md) | Generating inputs a grammar accepts with `pego sample` and package `sample`: seeds and size bounds, coverage, near-miss invalid inputs, seeding Go fuzz tests |
| [Editor Support](editor-support.md) | The language server (`pego lsp`): diagnostics, formatting, navigation, hover, rename, completion and semantic highlighting; setting up VS Code, Neovim, Helix and Emacs |
| [The Playground](playground.md) | The browser playground: inspecting trees and errors, sharing grammar state, its limits and the WebAssembly API |

## Choosing a Guide

| Situation | Read |
|:--|:--|
| Prototyping a grammar, or a tool that reads grammars at run time | [Running Parsers](running-parsers.md); `pego parse` to experiment ([The `pego` Command](command-line.md)) |
| A service that parses with a fixed grammar and wants the fastest parser | [Generating Parsers](code-generation.md) |
| A fixed grammar where start-up matters, or the grammar must be a data file | [Compiled Grammars](compiled-grammars.md) |
| Validating input, no tree needed | [Recognition Mode](recognition-mode.md) |
| Untrusted or machine-generated input that may nest deeply | [Choosing a Backend](backends.md) |
| Input larger than memory | [Streaming](streaming.md) |
| Editor-style reparsing after small edits | [Incremental Parsing](incremental.md) |
| Positions that index a Go string | [Position Units](position-units.md) |
| Several entry points for one grammar | [Running Parsers](running-parsers.md#choosing-a-start-rule) |
| Finding why an input fails or a parse is slow | [Debugging and Profiling](debugging.md) |
