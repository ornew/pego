# 012. Typed Values in Generated Parsers

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-08

## Summary

With `pego gen -types` (`pego.WithTypes()`), a generated parser also defines a Go type for each type of the grammar
and a function `ParseAST`, which parses like `Parse` and returns the result of the start rule as values of those
types instead of `*Node`.

## Design

### Where the types come from

The type checker already infers a type for every rule and checks every action against the declared types (see
[spec/types.md](../../spec/types.md)). It now keeps what it inferred (`Program.typed`: the type of each rule, the
fields of each struct type and the resolved alias types), and the generator maps those types to Go:

| Grammar type | Go type |
|:--|:--|
| `int`, `string`, `bool` | the same |
| struct type `T` | `*T`, a struct with the same fields and an embedded `Span` (its range) |
| terminal type `T` | `*T`, a struct `{Span; Text string}` |
| `Match`, `Error` | `*Match`, `*Error` (`Error` also has `Message`) |
| union `U = A \| B` of node types | an interface `U` that `*A`, `*B` and `*Error` implement through a marker method |
| `[]T` | `[]T'` |
| `*T` | `*int`, `*string` or `*bool` for basic types; otherwise the Go type of `T`, which is already nilable |
| `Seq`, `List`, `Operator`, records | `*Node`, unchanged |
| `node`, `terminal`, `any`, other unions | `any`, holding the converted value |

Names that clash with the runtime's exported names (`Node`, `Match`, `Error`, `Parse`, ...) get a trailing `_`
(a grammar type `Node` becomes `Node_`), and the helper `Span` takes a name no type or field uses.

`Error` implements every union so that a `#recover` inside a list of a union type (`[]Stmt`) keeps the error in the
list. Where a struct or terminal type is expected, an `Error` node becomes `nil`; it is reported in the
`SyntaxErrors` that `ParseAST` returns anyway.

### The typed runtime

When every value `ParseAST` can return has a Go type of its own (`runtimeOK`: no `Seq`, `List`, `Operator`, record,
`node`, `terminal` or `any` in the result type or in any struct field), the generated parser builds the values
directly. `genrt/typed.go` is a second runtime that mirrors `genrt/runtime.go` step by step, with values of type
`any` instead of `*Node`: a value is a pointer to a struct or terminal type, `*Match`, `*Error`, an int, string or
bool, nil, or a `*tnode` for the CST values that actions consume (Seq, List and Operator nodes, which never reach the
result). `tparser` embeds the parser and redefines only the methods that handle values, so matching, expectations,
errors and memoization decisions are literally shared. The generator writes the rules a second time for it
(`trules`), with the expression code of the Node runtime and these differences:

- **Constructors.** `new T{...}` calls a generated `tmk_T` with the fields in declaration order, which converts each
  value to the field's Go type; a constructor that makes an action's result takes the rule's range directly.
- **Rule calls.** Each rule gets methods of its own for call, invoke and finish, which call the body and the action
  directly and finish the value as the rule's kind requires.
- **Projections.** A repetition captured only to be taken apart by `map($rest, (r) => $r.f)` (the usual
  `first:x rest:(-"," r:x)*` list) gathers the values of `f` directly instead of a record per element.
- **Scratch memory.** CST nodes, value lists, capture frames (freed when their rule returns), memo entries and the
  decoded input are pooled with the parser across parses, since the result never refers to them; a parse allocates
  little more than the values it returns.

The parity test generates every corpus grammar twice, with the typed runtime and with conversion (below), and checks
that `ParseAST` returns the same values and errors, also when parsing concurrently. Eight of the 25 grammars use the
typed runtime, including JSON, XML, both calculators (Pratt and left recursion) and the indentation outline
(predicates and variables).

### Conversion after the parse

Otherwise `ParseAST` calls `Parse` and converts the tree. The converter (`astConv`) has one method per type and per
list type, looks fields up by name, and allocates the values of each type, and the elements of lists, in chunks, as
the runtime does nodes. The parse itself is unchanged, so the typed result is exactly the engine's tree in another
form.

### Cost

On the benchmarks (Apple M3 Max), the typed runtime makes `ParseAST` about 20% faster than `Parse` with a tenth of the
memory: JSON (262 KB) 9.9 → 7.7 ms and 20.0 → 2.4 MB, XML 10.0 → 8.0 ms, the left-recursive calculator 18.7 → 15.0 ms,
the outline 4.5 → 3.5 ms; the Pratt calculator is about as fast as `Parse` (its Pratt loop is the general one).
Conversion costs about 6% more than `Parse` (JSON 9.3 → 9.9 ms, 20.0 → 22.3 MB).

A hand-written prototype for JSON that kept captures in Go variables and built lists directly took 4.5 ms, about the
time of recognition alone; the rest of the gap is the general machinery of rule calls (capture frames and their
trail, the evaluation context of actions), which the typed runtime still shares with `Parse`.

## Alternatives considered

- **Statically typed code for every expression** (captures as Go variables, lists as typed slices, as in the
  prototype). It would close most of the remaining gap, but every construct (choices, lookaheads, `#recover`, Pratt
  lines, lambdas) would need code of its own; the typed runtime reuses the expression code instead.
- **Converting through JSON.** No generated code, but slower than the parse itself and lossy for unions (the
  member type would have to be decoded from `type`).
- **Exposing typed values from the engine (`pego.Parser`).** Go types cannot be created at run time, so the engine
  can only return `*Node`; typed values are a property of generated code.

## Limitations

- Grammars whose results include CST values convert the tree after the parse, at a small extra cost.
- `ParseAST` is generated for the start rule only (`ParseRule` returns `*Node`).
- Values whose type is not known statically (`node`, `any`, unions with basic members) are `any`.
