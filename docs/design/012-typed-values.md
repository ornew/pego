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
`node`, `terminal` or `any` in the result type or in any struct field) and no action or predicate reads a field of a
struct (a typed value keeps its fields converted, so reading one back would differ from the node's field: a list
would be a new list, an omitted `int` 0 rather than nil, an `Error` nil), the generated parser builds the values
directly. `genrt/typed.go` is a second runtime that mirrors `genrt/runtime.go` step by step, with values of type
`any` instead of `*Node`: a value is a pointer to a struct or terminal type, `*Match`, `*Error`, an int, string or
bool, nil, or a `*tnode` for the CST values that actions consume (Seq, List and Operator nodes, which never reach the
result). `tparser` embeds the parser and redefines only the methods that handle values, so matching, expectations,
errors and memoization decisions are literally shared. The generator writes the rules a second time for it
(`trules`), with the expression code of the Node runtime and these differences:

- **Constructors.** `new T{...}` evaluates the fields in the order written and calls a generated `tmk_T` with them in
  declaration order, which converts each value to the field's Go type; a constructor that makes an action's result
  takes the rule's range directly. `Match` and `Error` values carry their freshness in internal wrappers (`tmatch`,
  `terror`), so the public types have no unexported fields.
- **Rule calls.** Each rule gets methods of its own for call, invoke and finish, which call the body and the action
  directly and finish the value as the rule's kind requires.
- **Projections.** A repetition captured only to be taken apart by `map($rest, (r) => $r.f)` (the usual
  `first:x rest:(-"," r:x)*` list) gathers the values of `f` directly instead of a record per element (the
  conditions are in performance.md, change 52; generated `Parse` does the same).
- **Scratch memory.** CST nodes, value lists, capture frames (freed when their rule returns), memo entries and the
  decoded input are pooled with the parser across parses, since the result never refers to them; a parse allocates
  little more than the values it returns.
- **Direct rules** (`gen_direct.go`). Most rules are not written as a method per expression at all: the body is
  inlined into the method that calls the rule (`s<id>`, `v<id>` or `i<id>`), each expression jumping to a label when
  it fails, with captures in Go variables of that method instead of a frame. Where the general code resets to a
  mark, which undoes the trail, a direct rule restores the position, the number of recovered errors, the variable
  environment if the expression assigns variables, and the capture variables the expression may set, saved when it
  began: the trail of a rule's own captures is only ever extended by setCapture into its frame, so this is the same
  state. A repetition element with captures of its own clears its variables at each iteration (a new frame) and
  attaches them as before when its value is used. The action is a Go expression over the variables, evaluated in
  place: no frame, no function value, and for an action that makes a struct with the rule's range none of the checks
  of `tctx.result`, which hold by construction. Character tests read code points from the decoded input without
  calling `peek`, which the compiler does not inline. A direct rule leaves `p.cut`, `p.frame` and `p.trail` alone: it has
  no cut, and the rules it calls restore all three. The general code remains for rules with a cut or `#recover`,
  Pratt rules and the leaders of left recursion (and anything they call keeps working, since calls are the same
  methods either way); in the benchmark grammars that is the Pratt expression of the calculator and the three
  left-recursive rules of the other calculator.

The parity test generates every corpus grammar twice, with the typed runtime and with conversion (below), and checks
that `ParseAST` returns the same values and errors, also when parsing concurrently. The corpus includes the cases two
reviews found where the first version differed (`internal/engine/testdata/typed`): field reads in actions, the order
in which fields are evaluated, projections of names captured twice, of lists that `$n` also reaches and of nil
values, and an `Error` where a list is expected; and grammars for the code paths of direct rules (`direct_*.pego`).
It runs in both position units.

### Conversion after the parse

Otherwise `ParseAST` calls `Parse` and converts the tree. The converter (`astConv`) has one method per type and per
list type, looks fields up by name, and allocates the values of each type, and the elements of lists, in chunks, as
the runtime does nodes. The parse itself is unchanged, so the typed result is exactly the engine's tree in another
form.

### Cost

On the benchmarks (Apple M3 Max, min of 6 runs), the typed runtime makes `ParseAST` 30–46% faster than `Parse` with
a fifth to a third of the memory: JSON (262 KB) 7.2 → 3.9 ms and 12.6 → 2.9 MB, XML 7.8 → 4.3 ms, the left-recursive
calculator 15.5 → 10.3 ms, the outline 4.3 → 2.5 ms; the Pratt calculator, whose Pratt loop is the general one, 6.8 →
6.0 ms. JSON `ParseAST` takes the time of `Recognize`, which builds nothing. Conversion costs about 6% more than
`Parse`.

Direct rules made most of that difference: before them, with every rule run by the general machinery of rule calls
(capture frames and their trail, the evaluation context of actions), `ParseAST` was about 10% faster than `Parse`
(JSON 6.4 ms). A hand-written prototype for JSON that kept captures in Go variables and built lists directly took
4.5 ms on the same machine.

## Alternatives considered

- **Statically typed code for every expression** (captures as Go variables of their Go types, lists as typed
  slices, as in the prototype). Direct rules take the part of it that paid: captures in Go variables, the action in
  place. Values stay `any`, so the actions, the memo and the general code share one representation, and constructs
  that are rare in practice (cuts, `#recover`, Pratt lines, left recursion) keep the general code instead of code of
  their own.
- **Converting through JSON.** No generated code, but slower than the parse itself and lossy for unions (the
  member type would have to be decoded from `type`).
- **Exposing typed values from the engine (`pego.Parser`).** Go types cannot be created at run time, so the engine
  can only return `*Node`; typed values are a property of generated code.

## Limitations

- Grammars whose results include CST values convert the tree after the parse, at a small extra cost.
- `ParseAST` is generated for the start rule only (`ParseRule` returns `*Node`).
- Values whose type is not known statically (`node`, `any`, unions with basic members) are `any`.
