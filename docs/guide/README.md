# PEGO Guides

Task-oriented guides to the PEGO grammar language and runtime. They explain how and why to use each feature, with
complete, runnable examples. New to PEGO? Start with the [getting-started tutorial](../tutorial/getting-started.md).
The normative definition of the language is the [specification](../../spec/README.md).

| Guide | Contents |
|:--|:--|
| [Trees and actions](trees-and-actions.md) | The tree a grammar builds by default, shaping it with `@`, `-` and captures, struct, union and terminal types, actions, built-in functions and lambdas, positions, using trees from Go, recipes (`foldl`, flattening lists), performance guidelines |
| [Expressions](expressions.md) | Operator expressions: Pratt expressions, left-recursive rules and precedence chains compared, level-restricted calls, operator forms and associativity, pitfalls, a larger example |
| [Errors and recovery](errors-and-recovery.md) | How syntax errors are reported, getting them out on the command line and in Go, readable messages with `#error`, cuts, recovering with `#recover`, recipes |
| [Context-sensitive parsing](context-sensitive.md) | Predicates, variables, lookahead and attributes; matching tags, indentation blocks, here-documents and other recipes; the cost of variables |
| [Running parsers](runtime.md) | The Go API, start rules, the `Node` type, position units, backends and how to choose one, nesting limits, recognition mode, compiled grammars (`.pegoc`, in brief), memoization, the `pego` command |
| [Compiled grammars](compiled-grammars.md) | Saving a grammar as a `.pegoc` file (`pego compile`, `MarshalBinary`, `WithoutAST`) and loading it (`LoadParser`): what a file without the AST cannot do, sizes and load times, `go:embed`, versions and compatibility, loading untrusted data, and when to use it instead of `pego gen` or compiling at start-up |
| [Code generation](code-generation.md) | Standalone Go parsers with `pego gen`: the generated API, typed values (`-types`), what is supported, keeping generated code up to date, trade-offs |
| [TypeScript parsers](typescript.md) | Standalone TypeScript modules with `pego gen -lang ts`: the generated API, positions, strings and ints in JavaScript, errors, deep nesting and the JavaScript stack, keeping generated code up to date |
| [Streaming](streaming.md) | `#stream` and `ParseStream` for input that is too big to hold or never ends: what is emitted and when, memory, errors, recipes (summing a column, a header followed by records, cancelling) |
| [Incremental parsing](incremental.md) | `Document` for reparsing a text after edits: the model, what is reused and why, writing grammars that reuse well, performance and limits, an editor loop |
| [Debugging and profiling](debugging.md) | Why an input fails (`pego explain`), what the parser does (`pego trace`), why a parse is slow (`pego profile`) and how to read the profile, `WithTrace` and `WithProfile` in Go, costs and limits |
| [Linting grammars](linting.md) | Finding likely mistakes with `pego lint` and `pego.Lint`: alternatives that can never match, contradictory anchors and lookaheads, nullable repetitions, unused captures and rules, character class slips, hints for incremental parsing, suppressing findings with `lint:ignore` |
| [Testing grammars](testing-grammars.md) | A test suite for a grammar in layers: examples, golden files, properties that hold for every input, generated inputs, fuzzing, reference implementations, checking that backends, compiled grammars, generated code and incremental parses agree, lint and format checks in CI |
| [Sampling inputs and fuzzing](sampling-and-fuzzing.md) | Generating inputs a grammar accepts with `pego sample` and package `sample`: seeds and size bounds, coverage of rules and alternatives (and shadowed alternatives), near-miss invalid inputs, testing code that consumes parse results, seeding Go fuzz tests |
| [Editor support](editor-support.md) | The language server (`pego lsp`): diagnostics, formatting, go to definition and references, hover with inferred rule types, rename, completion and semantic highlighting; setting up VS Code (the extension in `editors/vscode`), Neovim, Helix and Emacs; limits and troubleshooting |
| [The web site and the playground](playground.md) | Using the playground (tree view, errors, shared links, generated Go), building and previewing the site, deploying on Netlify, the WebAssembly API |
