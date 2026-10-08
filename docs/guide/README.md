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
| [Running parsers](runtime.md) | The Go API, start rules, the `Node` type, position units, backends and how to choose one, nesting limits, recognition mode, compiled grammars (`.pegoc`), memoization, the `pego` command |
| [Code generation](code-generation.md) | Standalone Go parsers with `pego gen`: the generated API, typed values (`-types`), what is supported, keeping generated code up to date, trade-offs |
| [Streaming and incremental parsing](streaming-and-incremental.md) | `#stream` and `ParseStream` for unbounded input; `Document` for reparsing after edits; what is reused and how to write grammars that reuse well |
| [Debugging and profiling](debugging.md) | Why an input fails (`pego explain`), what the parser does (`pego trace`), why a parse is slow (`pego profile`) and how to read the profile, `WithTrace` and `WithProfile` in Go, costs and limits |
| [The web site and the playground](playground.md) | Using the playground (tree view, errors, shared links, generated Go), building and previewing the site, deploying on Netlify, the WebAssembly API |
