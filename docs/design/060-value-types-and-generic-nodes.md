# 060. Value Types and Generic Nodes

- **Status**: Proposed
- **Author**: @ornew
- **Date**: 2026-10-17

## Summary

Revise the language incompatibly around one idea: a rule produces an ordinary
typed value, and the concrete syntax tree is built from a single generic
`node[T, C]` type whose `Value` is that value. The reserved node types `Match`,
`Seq`, `List`, `Operator` and `Error`, the `terminal` type and node fields
disappear. Types gain tuples, primitive lists, a top type `_`, a bottom type
`_|_`, joins, meets and an explicit distinction between type definitions and
type aliases. Actions lose positional captures and `text`, and gain indexing
and the `.$` capture-access operator.

In this record, *standard syntax* is the syntax users write; *extended syntax*
is notation the implementation uses to describe built-in types that standard
syntax cannot define, such as `node[T, C]`.

## Motivation

- Grammar results are untyped trees of reserved node kinds. A grammar that wants
  an `int` or a primitive list cannot return one, and rules must return nodes.
- Captures are stored as node fields, so a capture of a subtree places that
  subtree in the tree twice.
- Positional captures (`$1`, `$n`, `$0`) are hard to count and break when a rule
  changes.
- `type T = U` means both "alias" and, with a struct right-hand side, nothing
  else; aliases preserve identity in some cases and not in others.

## Types

### Built-In Types

| Type | Written | Values |
|:--|:--|:--|
| `int` | `int` | `0`, `-1`, `100` |
| `string` | `string` | `"..."` |
| `bool` | `bool` | `true`, `false` |
| union | `T \| U` | a value of one of its arguments |
| list | `[]T` | `[]int{1, 2, 3}` |
| tuple | `()`, `(T,)`, `(T, U)` | `(1, Foo{}, []int{})` |
| optional | `*T` | `nil` or a value of `T` |
| struct | `{...}` | `T{...}` of a defined struct type |
| error | `error` | produced only by error recovery |
| top | `_` | every value |
| bottom | `_\|_` | no value |
| node | `node` (extended: `node[T, C]`) | parse results |

`union[...]`, `list[T]`, `tuple[...]` and `optional[T]` are the internal names
of the higher-kinded constructors. Standard syntax writes them only with their
operators, and diagnostics print the operator form.

**Union.** `|` is associative and commutative and is normalized to a flat set:
`T | (U | V)` is `T | U | V`, `T | U` is `U | T`, and duplicates collapse.
`T | U` is the join (least upper bound), so `A | B` is `B` when `A <= B`, and
`int | int` is `int`. A union with one argument is that argument; one with no
arguments, which only computation can produce, is `_|_`.

**Tuple.** `()` is the unit. A trailing comma is optional except in a
one-element tuple, where `(T)` is a group: `(T,)` is required. Tuples are
neither associative nor commutative: `((T, U), V)` differs from
`(T, (U, V))`, and `(T, U)` from `(U, T)`.

**List and tuple indexing.** `x[i]` reads the element at zero-based index `i`.

**Optional.** Untyped `nil` is assignable to every optional type.

**Top `_`.** The join of all types: `_ | T = _` and `_ & T = T`. A `_` value is
not assignable to a concrete type; that is a compile-time error unless an
explicit cast is written, which is checked at run time and can fail there.

**Bottom `_|_`.** The meet of all types: `_|_ | T = T` and `_|_ & T = _|_`.
Any other constructor applied to `_|_` is `_|_` (`[]_|_`, `*_|_`). A variable,
field or other location whose type resolves to `_|_` is a type error.

**Meet `&`.** `T & U` is the greatest lower bound. With the current types every
meet of distinct types is `_|_` (for example `int & string`), so the operator
is defined but has no practical use yet.

**error.** Conceptually `{Start int; End int; Msg string; ...}`, but it carries
runtime and source-map information that standard syntax cannot define. Only
error recovery creates error values; no syntax constructs or handles them, so
they appear only in parse results.

### Generic Nodes

```text
// extended syntax
type node[T _, C :map[:string, :int]] = {Start int; End int; Value T; Source *SourceInfo #runtime}
```

`node` replaces every reserved CST type. `Value` has the type argument `T`.
`C` is static type-level information mapping capture names to element indexes,
for example `node[T, {"name": 1, "foo": 3}]`; its map, strings and integers are
types, not values. The checker uses `C` to resolve a capture access to an index
at compile time. At run time `C` exists only inside opt-in source information.
`Source` is runtime-only and is not accessible from actions. Standard syntax
cannot construct a `node` value.

The examples below omit `C`.

| Rule | Result type |
|:--|:--|
| `rule a = "x"` | `node[string]` |
| `rule a = ("x" "y"*) #atomic` | `node[string]` |
| `rule a = b c d` | `node[(B, C, D)]`, with `B`, `C`, `D` the results of the rules |
| `rule a = b+` | `node[[]B]` |
| `rule a = "a"?` | `*node[string]` |
| `rule a = b? c` | `node[(*B, C)]` |
| `rule a = _ => Foo{}` | `Foo` |
| `rule b = a "a"*` | `node[(Foo, node[[]node[string]])]` |
| `rule a = _ => 100` | `int` |

A rule without an action is an implicit `node` rule; `rule a node = ...` states
it explicitly and still infers the type arguments. Rules may now produce any
value, so arbitrary ASTs can be built. Because captures are no longer fields,
a captured subtree appears in the tree once. Capture names, the producing rule,
capture indexes and the operator level can be compiled into source information
on request.

### Type Definitions and Aliases

`type T = {...}` is a type definition: it creates a new named type whose
underlying type is the right-hand side. Only struct literals are allowed today;
`type Int = int` parses but is a semantic error. `type T ~ U` is an alias: `T`
is only another name that resolves to `U` and creates no type.

```pego
type T = {}   // a new type, distinct from {} and from any other definition
type T ~ {}   // another name for the unnamed type {}
```

Users can define only struct types; every other type is built in or built from
others with type operators: `|`, `&`, `(,)`, `[]`, `*`, `{...}` and type
application `T[U, V]` (as in `node[string]`).

**Structural equivalence and identity are separate.** Types are compared after
normalization. Two struct types are structurally equivalent when they have the
same field names with identical types, in any order. Two types are identical
when their underlying types are structurally equivalent and either both come
from the same type definition or both are unnamed. With `type T = {}` and
`type U = {}`, `T`, `U` and `{}` are structurally equivalent and pairwise not
identical; with `type T ~ U` and `type V ~ U`, `T`, `U` and `V` are identical.

## Rule Expressions

| Operator | Name | Value type | Consumes input |
|:--|:--|:--|:--|
| `"..."` | literal | `string` | yes |
| `[...]` | character class | `string` | yes |
| `.` | any character | `string` | yes |
| `a` | rule call | the rule's type | as the rule |
| `f(...)` | rule function call | the rule's type | as the rule |
| `_` | top | none | no |
| `_\|_` | bottom | none | no |
| `^^`, `$$`, `^`, `$` | anchors | none | no |
| `a b ...` | sequence | `(A, B, ...)` | as the elements |
| `a / b` | ordered choice | `A \| B` | as the alternatives |
| `(a)` | group | `A` | as `a` |
| `a #atomic` | atomic | `string` | as `a` |
| `a #repeat(...)`, `a*`, `a+` | repetition | `[]A` | as `a` |
| `a?` | optional | `*A` | as `a` |
| `&a`, `!a` | lookahead | none | no |
| `-a` | discard | none | as `a` |
| `#cut` | cut | none | no |
| `n:a` | capture | `A` | as `a` |
| `a @n(...)` | attribute | `A` | as `a` |
| `#stream` | stream | none | no |
| `{...}` | Predicate Action | none | no |
| `a => x` | Value Action | `X` | as `a` |

The cut becomes the `#cut` pragma; `--` is removed. Sequence and choice values
are flat: `a (b c)` and `(a b) c` both have the value `(A, B, C)`, because a
group creates no value, while `a (b c => X{})` has `(A, X)`. Choice likewise
produces one flat union.

A compile option selects **CST mode**: every action is replaced by the default,
and the parser returns the default `node` tree even for grammars with actions.

## Actions

- **Captures only by name.** `$1`, ..., `$n` and `$0` are removed. Captures
  resolve to indexes at compile time, so named access has no runtime lookup.
- **Lambda parameters are variables.** Captures remain `$x`; lambda
  parameters share the variable namespace and are written without `$`:
  `{ indent := 1 } => { indent + 1; map($items, (x) => x.$item) }`.
- **Node access.** `text` is removed. A captured node exposes `.Start`, `.End`
  and `.Value`; `.Source` is rejected. `rule a = x:"x" => $x.Value` replaces
  `X{Text: text($x)}`. `len($x.Value)` and `len($x)` keep their meaning.
- **`.$` capture access.** `i.$y` reads capture `y` of node `i`; its receiver
  must be a `node`, and `C` turns the name into an index. For
  `rule a = x:(y:.)* => map($x, (i) => i.$y)`, `$x.Value` has type
  `[]node[..., {"y": 0}]`, so `i.$y` is `i.Value[0]`.

## Impact

The change touches the grammar parser and formatter, the checker, every
backend (Closure, VM bytecode and saved formats, generated Go Node and typed,
TypeScript), Documents and streams, error recovery and missing tokens, output
schemas and structural queries, JSON and S-expression output, the LSP, the
playground, examples, the ready-made parsers in `parsers/`, the specification
and every guide. Saved bytecode and schema formats need new versions.

## Decisions

- **Call-only rules are transparent.** An action-free rule whose body is a
  single rule call, `rule a = b`, has the type of `b` unchanged, so alias-like
  rules do not build `node[node[...]]` chains.
- **Sequences are always tuples.** `rule a = -ws b $$` has type `node[(B,)]`;
  adding or removing valueless elements does not change the shape.
- **Operator lines produce tuple nodes.** Without an action, an infix line
  produces `node[(L, Op, R)]`, a prefix line `node[(Op, R)]` and a postfix line
  `node[(L, Op)]`, with `C` mapping `lhs`, `op` and `rhs`. Actions keep using
  `$lhs`, `$op` and `$rhs`. The binding level is opt-in source information.
- **Recovery is an explicit union.** `a #recover(...)` and a literal with
  `#missing` have type `A | error`. Missing tokens become `error` values that
  record the expected token at run time. `error` is not implicitly assignable
  to other types.
- **Valued subexpressions are nodes.** Inside an action-free rule, literals,
  character classes, `.`, `#atomic` and repetitions each produce a `node`
  with its own range; rule calls keep the called rule's value, and groups and
  sequences flatten. A rule whose top-level expression already produces a
  node uses that node as its value without wrapping it again.
- **Captures in choices share the flat element.** In `(x:a / y:b) c`, the
  choice occupies one tuple element and both `x` and `y` index it. A capture
  whose alternative did not match is `nil`, so `$x` has type `*A` and `$y` type
  `*B`; the runtime records which alternative matched.
- **A capture has one position.** Alternatives that capture the same name must
  capture it at the same position of the tuple: `(y:b / y:d) e` is allowed,
  while `(y:b c / y:d) e` is an error, since `y` is the first element of a
  nested tuple in one alternative and the whole element in the other.
- **Casts are written `x.(T)`.** The postfix form chains with field access and
  fails at run time with an action error when the value is not a `T`.
- **Migration replaces the old syntax at once.** No compatibility mode is
  kept. A `pego fix` rewrite handles the mechanical parts (struct definitions,
  `--` to `#cut`, positional to named captures, lambda parameters), and the
  repository grammars and `parsers/` are migrated with it.

- **Subtyping is minimal.** `_|_ <= T <= _`, `T <= *T`, `nil <= *T`, and
  each union argument is below its union. `[]T`, `*T` and `node[T, C]` are
  covariant in `T`, and tuples are covariant element-wise. Named structs
  compare by identity only, without width subtyping. Nodes whose `C` differ
  are distinct types, and their join is a union.
- **`.$y` requires every alternative to have `y`.** On a union of nodes, `.$y`
  is allowed only when every argument captures `y`; its type is the join of
  their capture types, and differing indexes are selected at run time from the
  matched alternative. An optional receiver is a type error, as for field
  access.
- **Valueless bodies produce the unit node.** `rule stub = _`, and an
  action-free rule whose sequence has no valued element, produce `node[()]`.
- **Typos in the source.** `a?` has type `*A`, not `[]A`, and the tuple example
  is `(T, U) != (U, T)`.

## Open Questions

1. **Migration order.** The order in which the checker, backends, generated
   code, tools and ready-made parsers move, and how `text` on nonterminal
   ranges is rewritten, since it has no mechanical replacement.

## Alternatives Considered

- **Keep reserved node kinds and add values beside them.** This keeps
  compatibility but leaves two value models and the duplicated capture fields.
- **Keep captures as fields.** This keeps runtime name lookup and duplicates
  captured subtrees.
- **Keep positional captures alongside names.** This keeps the fragility the
  change removes.
- **Give a capture a position per alternative.** `C` would map a name to the
  positions of several alternatives, and the runtime would select one from the
  alternative that matched, so every read of such a capture pays for the
  selection. No grammar in the repository captures a name at different
  positions, and distinct names, or a capture of the whole choice, express it.

## Testing and Performance Plan

Every backend must agree on values, types and diagnostics for the corpus
rewritten to the new syntax. Compare tree size, allocation and parse time with
the current representation on the benchmark grammars before and after the
removal of capture fields; record whether compile-time capture indexes remove
lookup costs as expected.
