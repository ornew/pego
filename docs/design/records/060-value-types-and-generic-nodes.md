# 060. Value Types and Generic Nodes

- **Status**: Implemented
- **Author**: @ornew
- **Date**: 2026-10-17

## Summary

Revise the language incompatibly around one idea: a rule produces an ordinary
typed value, and the concrete syntax tree is built from a single generic
`node[T, C]` type whose `Value` is that value. The reserved node types `Match`,
`Seq`, `List`, `Operator` and `Error`, the `terminal` type and node fields
disappear. Types gain tuples, primitive lists, a top type `_`, a bottom type
`_|_`, joins, meets and an explicit distinction between type definitions and
type aliases. Actions lose positional captures, keep `text` for the input a
node spans, and gain indexing and the `.$` capture-access operator.

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
| join | `T \| U` | a value of one of its arguments |
| list | `[]T` | `[]int{1, 2, 3}` |
| tuple | `()`, `(T,)`, `(T, U)` | `(1, Foo{}, []int{})` |
| optional | `*T` | `nil` or a value of `T` |
| struct | `{...}` | `T{...}` of a defined struct type |
| error | `error` | produced only by error recovery |
| top | `_` | every value |
| bottom | `_\|_` | no value |
| node | `node` (extended: `node[T, C]`) | parse results |

`join[...]`, `list[T]`, `tuple[...]` and `optional[T]` are the internal names
of the higher-kinded constructors. Standard syntax writes them only with their
operators, and diagnostics print the operator form.

**Join.** `|` is associative and commutative and is normalized to a flat set:
`T | (U | V)` is `T | U | V`, `T | U` is `U | T`, and duplicates collapse.
`T | U` is the join (least upper bound), so `A | B` is `B` when `A <= B`, and
`int | int` is `int`. A join with one argument is that argument; one with no
arguments, which only computation can produce, is `_|_`.

**Tuple.** `()` is the unit. A trailing comma is optional except in a
one-element tuple, where `(T)` is a group: `(T,)` is required. Tuples are
neither associative nor commutative: `((T, U), V)` differs from
`(T, (U, V))`, and `(T, U)` from `(U, T)`.

**List and tuple indexing.** `x[i]` reads the element at zero-based index `i`.
A node is indexed through its value: when `x` is a node, `x[i]` is
`x.Value[i]`, so `$x[1]` reads the second element of a captured sequence. A
constant index outside a tuple is a compile-time error.

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
A Type Expression always gives the argument: `node[_]` is a node of any value,
and a bare `node` is an error, so that every field and rule type states what
its node holds. A rule's declared type is its type everywhere, `node[_]` included:
a rule declared `node[_]` has the type `node[_]` and not the type of its value.
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
a captured subtree appears in the tree once. Source information, which is opt-in,
holds the producing rule of each node and the capture names with their
locations; it does not record an operator's binding level.

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
produces one flat join.

A compile option selects **CST mode**: every action is replaced by the default,
and the parser returns the default `node` tree even for grammars with actions.

## Actions

- **Captures only by name.** `$1`, ..., `$n` and `$0` are removed. Captures
  resolve to indexes at compile time, so named access has no runtime lookup.
- **Lambda parameters are variables.** Captures remain `$x`; lambda
  parameters share the variable namespace and are written without `$`:
  `{ indent := 1 } => { indent + 1; map($items, (x) => x.$item) }`.
- **Node access.** A captured node exposes `.Start`, `.End` and `.Value`;
  `.Source` is rejected. `text(x)` remains and returns the input that node `x`
  spans as a `string`, whatever its value, so `X{Text: text($x)}` keeps its
  meaning; for a terminal, `$x.Value` is the same string.
  `len($x.Value)` and `len($x)` keep their meaning.
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
  `$lhs`, `$op` and `$rhs`.
- **Recovery is an explicit join.** `a #recover(...)` and a literal with
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
- **A capture has a location per alternative.** Alternatives may capture the
  same name at different positions: in `(y:b c / y:d) e`, `y` is the first
  element of a nested tuple in one alternative and the whole element in the
  other, and `C` maps `y` to both. The runtime selects the location from the
  alternative that matched, which it records anyway, so only reads of such a
  capture pay for a branch. The repository grammars use this in dozens of
  places, for example `ws n:NOT? ws t:(NULL / TRUE) / t:ISNULL`, and a choice
  of a one-element sequence and a bare element, `&[0-9] v:int / v:paren`.
- **Captures in positive lookaheads are kept beside the tuple.** A capture in
  `&a` has no element, since the lookahead consumes nothing, but its value
  remains in effect after the lookahead, so actions, predicates and
  bindings read it. `C` records that its value is beside the tuple, and an
  alternative may capture a name in a lookahead while another captures it at a
  position, as in `!nl -ws e:expr / &nl &(-ws e:missing)`, which places a
  zero-width node after the whitespace that the statement does not consume.
- **Casts are written `x.(T)`.** The postfix form chains with field access and
  fails at run time with an action error when the value is not a `T`.
- **Migration replaces the old syntax at once.** No compatibility mode is
  kept. A `pego fix` rewrite handles the mechanical parts (struct definitions,
  `--` to `#cut`, positional to named captures, lambda parameters), and the
  repository grammars and `parsers/` are migrated with it.
- **Terminal types become one-field structs.** A terminal type names the kind
  of a token, and joins of them, such as `Number | String | Bool`, tell the
  kinds apart, which `node[string]` cannot do. `pego fix` rewrites
  `type T terminal` to `type T = { Text string }`, a rule `rule r T = e`
  without an action to `rule r T = t:(e) #atomic => T{Text: text($t)}`
  (without `#atomic` when `e` has captures, which predicates may read), and a
  positional constructor `T{s}` to `T{Text: s}`. The struct keeps the range of
  the rule, and a generated Go `*T` keeps its `Span` and `Text`. The reserved
  names are rewritten to the value types they stand for: `Match` and
  `terminal` to `node[string]`, `Seq`, `List` and `Operator` to `node`, and
  `Error` to `error`.

- **Subtyping is minimal.** `_|_ <= T <= _`, `T <= *T`, `nil <= *T`, and
  each join argument is below its join. `[]T`, `*T` and `node[T, C]` are
  covariant in `T`, and tuples are covariant element-wise. Named structs
  compare by identity only, without width subtyping. Nodes whose `C` differ
  are distinct types, and their join keeps both.
- **`.$y` requires every alternative to have `y`.** On a join of nodes, `.$y`
  is allowed only when every argument captures `y`; its type is the join of
  their capture types, and differing indexes are selected at run time from the
  matched alternative. An optional receiver is a type error, as for field
  access.
- **Cycles through actions are least fixed points.** A rule type stays a
  reference until an action reads into it, so recursion without actions needs
  no iteration. When actions of rules that refer to each other read into each
  other's values, the rules of that strongly connected component start from
  `_|_` and are computed again from the previous round's values until none
  changes; reading into `_|_` is not an error. The result and the errors do
  not depend on the order of the rules. Values that still change after a fixed
  number of rounds become unknown, as the value of a predicate variable is.
- **Valueless bodies produce the unit node.** `rule stub = _`, and an
  action-free rule whose sequence has no valued element, produce `node[()]`.
- **Output formats follow the value.** In JSON a node is
  `{"start": S, "end": E, "value": V}`, where `V` is a string, an array for a
  tuple or a list, `null` for an optional that did not match, or the value of
  an action: an object for a struct, a number or a boolean. A tuple and a list
  are told apart by the schema. The S-expression output writes the value's
  shape without positions: `( )` for a tuple, `[ ]` for a list, quoted
  strings and `nil`. Each captured subtree appears once. Rule names and
  capture names are written only when the grammar is compiled with source
  information, as `"rule"` and `"captures"` members in JSON and as `@rule`
  and a `name:` label before the element in S-expressions, for example
  `(k:["a"]@key ":" v:["1"]@num)@main`. `pego parse` does not request source
  information unless asked to.
- **The Go node is opaque.** The public `Node` exposes methods only: its
  range, its value, the elements of a tuple or list value, and, with source
  information, its rule and captures. No field or method commits to a memory
  layout, so the engine may store trees in flat arrays or arenas and avoid
  allocating nodes for terminals.
- **The engine moves at once on a branch.** The repository grammars, the
  closure backend, the VM, Documents and snapshots, the output formats,
  schemas and queries, and generated code move in separate commits on one
  branch, which replaces the current model on the main line only when every
  backend agrees and every test and ready-made parser passes. No release
  carries both tree models.
- **Typos in the source.** `a?` has type `*A`, not `[]A`, and the tuple example
  is `(T, U) != (U, T)`.

## Open Questions

No open questions remain.

## Alternatives Considered

- **Keep reserved node kinds and add values beside them.** This keeps
  compatibility but leaves two value models and the duplicated capture fields.
- **Keep captures as fields.** This keeps runtime name lookup and duplicates
  captured subtrees.
- **Keep positional captures alongside names.** This keeps the fragility the
  change removes.
- **Give a capture one position.** Rejecting a name captured at different
  positions keeps `C` a map from a name to one index, but the repository
  grammars capture names at different positions in dozens of choices, and
  rewriting them needs distinct names and a way to choose between them in the
  action.
- **Reject reading captures in lookaheads from actions.** Actions would need
  the captured value through a predicate variable, which holds only `int`,
  `string` and `bool`, and the zero-width node placed after whitespace could
  not be expressed.

## Testing and Performance Plan

Every backend must agree on values, types and diagnostics for the corpus
rewritten to the new syntax. Compare tree size, allocation and parse time with
the current representation on the benchmark grammars before and after the
removal of capture fields; record whether compile-time capture indexes remove
lookup costs as expected.

## Performance and Results

Three alternating samples of `BenchmarkParse`, `BenchmarkRecognize`,
`BenchmarkIncremental` and `BenchmarkStream` compared the previous tree model
with the value model, each binary run against its own revision's grammars, on
an Apple M3 Max with Go 1.27.1. Over the 120 benchmarks the geometric means of
the median ratios were 1.013× time, 1.066× bytes and 1.048× allocations per
operation; the time spread between samples was mostly 0–3% and up to 8%.

- **Every node is larger.** A node now has room for a value built by an
  action, which accounts for the uniform 1.04–1.09× bytes on JSON, CSV, XML, Outline and streams, at 0.98–1.04× time.
- **Grammars with many valued subexpressions build more nodes.** Optionals,
  repetitions and literals inside sequences are nodes of their own, and
  operator lines build tuples. Minilang parses took 1.20–1.41× bytes and
  1.03–1.08× time on the engine backends and 1.70× bytes on the generated
  `Node` parser; operators-rule arithmetic took 1.27× bytes and recovery
  1.21–1.67×.
- **Typed generated values are smaller.** Without node fields, generated typed
  Minilang parses took 0.21× bytes and 0.84× time, and recovery 0.25× bytes and
  0.83× time.
- **Recognition is unchanged** within 0.99–1.04× time; its allocation counts
  stay in the tens per parse.
- **Incremental reparses** took 1.04–1.06× time; fresh Documents took
  1.22–1.41× bytes and Document edits 1.03×.

The node size and the extra nodes of valued subexpressions are the candidates
for later work: carrying action values outside the node, and not allocating
nodes for terminals, which the opaque `Node` permits.
