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

### Conversion after the parse

`ParseAST` calls `Parse` and converts the tree. The converter (`astConv`) has one method per type and per list
type, looks fields up by name, and allocates the values of each type, and the elements of lists, in chunks, as the
runtime does nodes.

The parse itself is unchanged, so the typed result is exactly the engine's tree in another form, and the parity
tests of generated parsers ([008](008-code-generation.md)) keep covering it. The tests also generate every grammar
of the corpus with types, check that `ParseAST` returns the errors of `Parse`, and compare the values of a grammar
that exercises unions with CST members, lists of lists, optional values and recovered errors.

### Cost

On the 262 KB JSON benchmark input (Apple M3 Max), `ParseAST` takes about 6% longer than `Parse` (9.3 → 9.9 ms) and
allocates 11% more bytes (20.0 → 22.3 MB) and 1,441 instead of 1,215 objects. Allocating list elements in chunks
took the object count down from 6,307.

## Alternatives considered

- **A typed runtime that builds the typed values directly, without `Node`.** This is the only way typed values
  could make parsing *faster*: no nodes, no field lists, no conversion. But every part of the runtime that handles
  values (memoization, left recursion, captures, actions, `#recover`, the built-in functions) works on `Node` and
  `any` and is shared with the engine's semantics; a second, typed version of it would double what the parity tests
  must keep equal. Not done for now; converting after the parse gives the API at a small, predictable cost.
- **Converting through JSON.** No generated code, but slower than the parse itself and lossy for unions (the
  member type would have to be decoded from `type`).
- **Exposing typed values from the engine (`pego.Parser`).** Go types cannot be created at run time, so the engine
  can only return `*Node`; typed values are a property of generated code.

## Limitations

- Typed values are a view of the tree after the parse: they cost a little more than `Parse`, not less.
- `ParseAST` is generated for the start rule only (`ParseRule` returns `*Node`).
- Values whose type is not known statically (`node`, `any`, unions with basic members) are `any`.
