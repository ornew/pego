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

Every declared alias is exported as a Go alias to its resolved value representation: `type Alias = Word` in the
grammar becomes `type Alias = *Word` in Go, and a list alias becomes an alias to the corresponding slice. Chained,
scalar, optional and CST aliases follow the same mapping. Node unions with the same normalized representation
share one canonical interface and marker-method set; the other declared names alias that interface. This preserves
assignment compatibility without duplicating runtime kinds, converters or marker methods. Name collision handling
applies to alias names as well as struct and terminal declarations.

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
  Discarded undo-trail entries are cleared after rollback, rule return and captured repetition elements, including
  generated call methods. Live caller undo entries remain available. Recycling clears the full trail and Pratt
  saved-capture backing slices, since their lengths may have shrunk after retaining overwritten values. This releases
  discarded captures from pooled scratch while preserving returned terminals/structs; it does not rely on a later pool GC.
- **Direct rules** (`gen_direct.go`). Eligible rule bodies are inlined into the
  method that calls the rule (`s<id>`, `v<id>` or `i<id>`), each expression
  jumping to a label when it fails, with captures in Go variables instead of a
  frame. Where the general code resets to a mark, which undoes the trail, a
  direct rule restores the position, recovered-error count, variable
  environment when assigned, and capture variables the expression may set.
  A repetition element with its own captures clears them at each iteration and
  attaches them as before when its value is used. The action is a Go expression
  over those variables; a struct action uses the rule range directly.
  Character tests and short literals read decoded code points without calling
  `peek` or `matchLiteral`.

  The typed direct path now supports ordinary local cuts as well as rules
  without cuts. It emits scope-local cut state for choices, optionals and
  repetitions; nested scopes and callees retain their own cut boundaries.
  Failed alternatives restore captures, variable bindings and recovered
  errors. The generated wrappers retain rule call depth, memo and result
  ownership. Ordinary cut-bearing rules use the scoped direct path
  ([record 78](../optimizations/078-local-cuts-in-typed-direct-rules.md)).
  Eligible left-recursion leaders can inline their unfinished typed body
  while the runtime retains seed growth and invocation ownership
  ([record 79](../optimizations/079-inline-eligible-typed-left-recursion-bodies.md)).
  Eligible unfinished operand, operator and skip matchers on typed Pratt lines
  can also be inlined ([record 80](../optimizations/080-inline-eligible-typed-pratt-lines.md)).
  Eligible cut-bearing left-recursion and Pratt bodies use a separate framed
  route ([record 81](../optimizations/081-scoped-cuts-in-unfinished-typed-go-bodies.md)).
  Each line owns its `tprattLine.scope`; the runtime retains precedence,
  longest-match selection, frames and action finalization. Left-recursion
  runtime frames and growth remain in place. `#recover` and structurally
  unsupported layouts remain on general expression dispatch; their callees
  can use either path.

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

### Allocation and ownership

Both paths allocate typed values in separate chunks for each struct, terminal and list element type. Value chunks
start at 8 elements and double up to 256; list chunks start at 64 and double up to 1,024, accommodating the requested
list length. Lists longer than 256 elements use an exact-size allocation. Each allocator keeps its next chunk size
separately from its unused slice, so exhausting a chunk does not reset growth. A new `ParseAST` invocation starts
fresh allocators; the parser pool never recycles arrays that returned typed values can still reference.

Every handed-out value stays at its original address. Returned lists have capacity equal to their length, so an
append cannot overwrite another returned list. Empty lists remain non-nil and omitted optional fields retain their
zero values. Growing a chunk allocates a distinct zeroed array; it never copies, clears or reuses earlier values,
including values a failed or overwritten capture may still reference. Returned pointers can therefore still retain
discarded siblings in the same array. Allocation granularity does not solve that separate retention problem.

### Cost

In the earlier benchmark snapshot (Apple M3 Max, min of 6–8 runs), the typed runtime made `ParseAST` 35–55% faster than `Parse`
with a fifth to a third of the memory: JSON (262 KB) 7.2 → 3.3 ms and 12.8 → 2.9 MB, XML 7.5 → 4.0 ms, the
left-recursive calculator 15.1 → 9.8 ms, the outline 4.0 → 2.1 ms; at that time, the Pratt calculator used the
general Pratt loop and took 6.4 → 5.8 ms. JSON `ParseAST` is faster than `Recognize` (3.8 ms), which builds nothing but runs the
general code. Conversion costs about 6% more than `Parse`.

Direct rules made most of that difference (performance.md, changes 60–62): before them, with every rule run by the
general machinery of rule calls (capture frames and their trail, the evaluation context of actions), `ParseAST` was
about 15% faster than `Parse` (JSON 6.2 ms). A hand-written prototype for JSON that kept captures in Go variables and
built lists directly had taken 4.5 ms.

## Alternatives considered

- **Statically typed code for every expression** (captures as Go variables of
  their Go types, lists as typed slices, as in the prototype). Direct rules
  take the part of it that paid: captures in Go variables and actions in place.
  Values stay `any`, so actions, memo and general code share one representation.
  Ordinary local cuts, eligible left-recursion leader bodies and eligible
  Pratt line matchers have direct paths. Eligible cut-bearing left-recursion
  and Pratt bodies use framed direct paths while runtime wrappers retain
  growth, invocation and Pratt-selection ownership. `#recover` and
  structurally unsupported layouts remain general.
- **Converting through JSON.** No generated code, but slower than the parse itself and lossy for unions (the
  member type would have to be decoded from `type`).
- **Exposing typed values from the engine (`pego.Parser`).** Go types cannot be created at run time, so the engine
  can only return `*Node`; typed values are a property of generated code.
- **Sizing every type's first chunk from input length.** Input length does not predict how many values of each
  type will escape. Bounded geometric growth keeps unused types unallocated and limits the first-use cost of rare
  types. Keeping the previous 256-value and 1,024-list-element maximum chunks retains large-output amortization;
  the measured tradeoff is extra allocations while warming up, documented in performance change 74.

## Limitations

- Grammars whose results include CST values convert the tree after the parse, at a small extra cost.
- `ParseAST` is generated for the start rule only (`ParseRule` returns `*Node`).
- Values whose type is not known statically (`node`, `any`, unions with basic members) are `any`.
