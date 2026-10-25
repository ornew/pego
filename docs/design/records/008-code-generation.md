# 008. Generating Go Parsers

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-07

## Summary

Generate Go source code for a parser that depends only on the standard library, from a grammar (`pego gen`, `pego.GenerateGo`).

## Design

### What Is Generated

| Part | Contents |
|:--|:--|
| Runtime | Parser state, memoization, left recursion, the Operators Rule loop, attributes and the built-in functions of actions. The runtime is the package `github.com/ornew/pego/runtime`, which generated code imports as `rt`, or a copy of its files in the generated package's `internal/runtime` directory (the default, so that the generated package depends only on the standard library). Everything the generated code reaches in the runtime has an exported name (`p.Pos`, `p.Peek`, `NewGrammar`); these hooks are for generated parsers and not a stable API. |
| Rule table | For each rule: whether it is memoized, whether it is a left-recursion leader, its capture names and so on (the results of the engine's static analysis) |
| Parser expressions | General fallback: one function per expression, taking the parser as its first parameter (`func e18(p *Parser) (*Node, bool)`). Supported direct Node rules inline their expressions into a shared raw body used by call wrappers and rule-table entries. |
| Actions and predicates | Go expressions (returning `any`). Lambdas become Go function literals. |
| Public API | `parser.go` defines `Parse(input)` and `ParseRule(name, input)`; `api.go` re-exports the runtime names users need (`Node`, `SyntaxError`, `SyntaxErrors`, ...) as aliases and wrappers |

Because the grammar is not interpreted at run time, no grammar loading or compilation is needed, and expression evaluation is optimized by the Go compiler.
The node representation (`Node`) and the JSON format are the same as the engine's, so replacing the engine with a generated parser, or the reverse, gives the same results.

### Reachable Rules

A generated Node or recognizer parser prints the body of a rule only if something can enter it:
a start rule, or a rule that a printed body calls through its slot or its wrapper. A rule that
is referenced only from value-free contexts is entered through its twin, so its original body is
left out, and so are the bodies that only it calls. The generator finds the entered rules in a
first pass over the Node and recognizer tables and prints the second pass from them; it fails if a printed call reaches a rule that
was left out. The typed tables and the TypeScript generator are not pruned. See [record 120](../../optimizations/120-unreachable-rule-bodies.md).

### Direct Rules

The Node generator inlines eligible value-free plain `Recognize` rules and skip twins used by `Parse`, plus
supported value-building Node rules.
The latter can include captures, actions, predicates, scoped repeats, projections and memoized bodies. Local
cuts, recovery, Operators Rule declarations, left-recursion leaders and unresolved capture references keep general
dispatch. References can call direct or general rules; callee eligibility does not restrict the caller.

Each direct call still counts one rule call against the depth limit and preserves input rollback, error state,
ordered diagnostics, silent lookahead, labels, capture/action trails, variable environments, recovery and memo
semantics. Existing `call`, `invoke`, `invokePlain` and `finish` wrappers retain ownership of those behaviors
and node naming. Change 76 duplicates eligible value-free plain bodies between direct-call and generic-entry
methods. Change 77 instead uses one raw Node expression body through the existing wrappers and rule-table entry.
Source size and compiler cost are measured alongside matching speed in [changes 76 and
77](../../optimizations/README.md#optimization-history). TypeScript and typed-value emission are unchanged.

### Alternatives Considered

- **Generating a typed AST (a Go struct per grammar type)**: convenient for users, but it would need a separate design for representing join types, optional types and the `Error` nodes of error recovery as Go types. This time, identical behavior was prioritized, so the generated parser returns the same `Node` as the engine. Typed values were added later as an option on top of `Node` ([012](012-typed-values.md)).
- **Making the engine a public package and calling it from generated code**: the generated code would be smaller, but the runtime internals would have to become public API. The ability to distribute the generated code on its own was prioritized.
- **Sharing one value-free expression body between direct and generic calls**: this avoids duplicated
  statements but keeps an extra expression-body call on the hot direct path. Change 76 retains separate bodies
  for generic entry; its source/compiler cost is measured in that entry. Change 77 shares the unfinished Node
  expression body through the existing wrappers, with wrapper ownership of depth, frames, actions, recovery,
  memoization and node naming.

### Verifying Identical Behavior

The runtime code is written separately from the engine, so the two can diverge. The tests (`internal/engine/gen_test.go`) therefore check the following:

1. Generate parsers from all grammars in `examples/` and from the grammars of the engine tests (Operators Rule, left recursion, predicates and variables, error recovery, `@error`, cut, lookahead, built-in functions, run-time errors and more).
2. Build the generated code together in a temporary module and run it.
3. For every input, check that the node tree (as JSON, including positions) and the error strings match the engine's.

`runtime` is also compiled as an ordinary package, so `go vet ./...` checks it as well.

## Limitations

- Stream parsing (`#stream`) and incremental parsing are not generated. `#stream` is treated as an ordinary repetition.
