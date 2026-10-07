# 008. Generating Go Parsers

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-07

## Summary

Generate Go source code for a parser that depends only on the standard library, from a grammar (`pego gen`, `pego.GenerateGo`).

## Design

### What is generated

| Part | Contents |
|:--|:--|
| Runtime | Parser state, memoization, left recursion, the Pratt loop, attributes and the built-in functions of actions. `internal/engine/genrt/runtime.go` is embedded as is. |
| Rule table | For each rule: whether it is memoized, whether it is a left-recursion leader, its capture names and so on (the results of the engine's static analysis) |
| Parser expressions | One method per expression. Constants (literals, character classes, capture slots, rule indices) are embedded. |
| Actions and predicates | Go expressions (returning `any`). Lambdas become Go function literals. |
| Public API | `Parse(input)`, `ParseRule(name, input)`, `Node`, `SyntaxError`, `SyntaxErrors` |

Because the grammar is not interpreted at run time, no grammar loading or compilation is needed, and expression evaluation is optimized by the Go compiler.
The node representation (`Node`) and the JSON format are the same as the engine's, so replacing the engine with a generated parser, or the reverse, gives the same results.

### Alternatives considered

- **Generating a typed AST (a Go struct per grammar type)**: convenient for users, but it would need a separate design for representing union types, optional types and the `Error` nodes of error recovery as Go types. This time, identical behavior was prioritized, so the generated parser returns the same `Node` as the engine.
- **Making the engine a public package and calling it from generated code**: the generated code would be smaller, but the runtime internals would have to become public API. The ability to distribute the generated code on its own was prioritized.

### Verifying identical behavior

The runtime code is written separately from the engine, so the two can diverge. The tests (`internal/engine/gen_test.go`) therefore check the following:

1. Generate parsers from all grammars in `examples/` and from the grammars of the engine tests (Pratt, left recursion, predicates and variables, error recovery, `#error`, cut, lookahead, built-in functions, run-time errors and more).
2. Build the generated code together in a temporary module and run it.
3. For every input, check that the node tree (as JSON, including positions) and the error strings match the engine's.

`genrt` is also compiled as an ordinary package, so `go vet ./...` checks it as well.

## Limitations

- Stream parsing (`#stream`) and incremental parsing are not generated. `#stream` is treated as an ordinary repetition.
