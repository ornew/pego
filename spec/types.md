# Type System

PEGO is statically typed. Types describe the values that rules produce (see [Nodes and Values](nodes.md)) and the
shape of the values that actions build. This chapter defines the types, how they
are declared, their names and when a value of one type may be used where another
is expected. How the type of an expression is determined, and the errors that
the checker reports, are defined in [Type Checking](type-checking.md). Type
errors are reported when the grammar is compiled, with the position of the
offending construct.

## Built-In Types

The built-in types are named by these reserved names:

| Type | Values |
|:--|:--|
| `bool` | Booleans. |
| `int` | Integers. Positions in the input are also `int` values (see [Positions](parsing.md#positions)). |
| `string` | Strings. `len` measures a string in the [position unit](parsing.md#positions). |
| `node[T]` | Nodes whose value has type `T`, for any type `T` (see [Node Types](#node-types)): `node[string]` for a string node, `node[(A, B)]` for the tuple node of a sequence, `node[[]A]` for the list node of a repetition. |
| `error` | The values of [error recovery](pragmas.md#recover): a skipped range, or a `missing` token. |
| `missing` | The tokens that [`#missing`](pragmas.md#missing) inserted. A `missing` is an `error`. |
| `_` | Every value: the top type (see [Top, Bottom, Join and Meet](#top-bottom-join-and-meet)). |
| `_\|_` | No value: the bottom type. |

Other types are formed from types `T`, `U`, ... by these forms, or named by
[Type Declaration Statements](#type-declaration-statements):

| Type | Values |
|:--|:--|
| `(T, U, ...)` | Tuples of a `T`, a `U`, ...: `()` is the unit, `(T,)` has one element (see [Tuple Types](#tuple-types)). |
| `[]T` | Lists of `T` values. |
| `*T` | The values of `T`, and `nil`. |
| `T \| U` | The values of `T` and those of `U`: the join of `T` and `U` (see [Union Types](#union-types)). |
| `T & U` | The values that are both a `T` and a `U`: the meet of `T` and `U` (see [Top, Bottom, Join and Meet](#top-bottom-join-and-meet)). |
| `N` | The values of the type named `N`: a struct type or a named node type defined by a Type Definition, or the type a Type Alias names. |

Every value has a static type. The value of a rule, the result of an action, an
element of a tuple or list and a field of a struct are nodes, tuples, lists,
structs, `int`, `string` and `bool` values, errors or `nil`; a function is not a
value. [Variables](predicates.md#variables) hold `int`, `string` and `bool`
values. `node[_]` is a node whose value is not known statically: it is the
last resort, and a precise `node[T]` is preferred wherever the value is known.

A Type Expression is written with the following syntax. `[]` and `*` apply to
the type that follows, `&` forms a meet and binds tighter than `|`, which forms
a union, parentheses with commas form a tuple, and parentheses without a comma
group.

```pego
type Example = {
    Items  []Item                       // a list
    Maybe  *Item                        // an Item or nil
    Either Item | Other                 // a union
    Many   [](Item | Other)
    Count  int
    Pair   (node[string], node[string]) // a tuple
    Call   node[(Name, []Arg)]          // the node of a sequence
    Digits node[[]node[string]]         // the node of a repetition
}

type LongUnion ~ (
    Item
    | Other
)
```

Parentheses group a Type Expression and allow it to continue across physical
newlines. The source AST retains an explicit parenthesized type as a source-only
`TypeGroup` node implementing `TypeExpr`; compilation and generation lower the
wrapper transparently, so grouping does not change type meaning. See
[Parenthesized Groups in the Public AST](grammar-files.md#parenthesized-groups-in-the-public-ast)
for source metadata, JSON and formatter behavior. Outside such a
group, a Type Expression follows the normal hard newline Declaration Statement boundary.

### Node Types

`node[T]` is the type of a node whose value, `.Value`, has type `T`, for any
value type `T`. A node also has its input range `.Start` and `.End`. The nodes
that Rule Expressions produce have these types:

| Expression | Type |
|:--|:--|
| a literal, a character class, `.`, `a #atomic` | `node[string]` |
| a sequence `a b c` | `node[(A, B, C)]`, with `A`, `B` and `C` the types of the elements |
| a repetition `a*`, `a+` | `node[[]A]`, with `A` the type of an iteration |
| an expression without elements | `node[()]` |

So `"(" items:(item ("," item)*) ")"` has a value of type
`node[(node[string], node[(item, node[[]node[(node[string], item)]])], node[string])]`,
where `item` stands for the type of the rule `item`: a captured group is one
element, while the elements of a group that is not captured are elements of the
enclosing sequence (see [Sequences](parser-expressions.md)). `node[T]` can be
written for any `T`. The value of a node that a Rule Expression produces is a
string, a tuple or a list, so a cast to a type such as `node[int]` or
`node[Item]` admits no node.

Diagnostics and `RuleType` show the captures of a node beside its value, as in
`node[(node[string], item), {"items": 1}]`, which locates the capture `items`
at element 1 of the tuple. A node type written in a grammar has no captures; a
node with captures is assignable to it when their values are.

`node` takes exactly one type argument: a bare `node` is an error ("node takes
a type argument"), and `node[A, B]` is the error
"node takes one type argument, got 2". The type argument follows `node`
directly, without a space. No other type takes type arguments.

### Tuple Types

A tuple type is written in parentheses with commas: `()` is the unit type,
`(T,)` has one element and `(T, U)` two; a trailing comma is optional after two
or more elements. Parentheses around a single type without a comma group it.
The value of a sequence node is a tuple, so `x.Value` of an `x` of type
`node[(A, B)]` has type `(A, B)` and `x.Value[0]` type `A`. Tuple types may be
used wherever a type may: in fields, aliases, rule types, casts, list
constructors and type arguments.

### Reserved Forms

These forms are reserved: they parse, format and convert to JSON, but do not
compile, also when types are otherwise not checked.

- An unnamed struct type is written with the braces and fields of a struct
  definition, as in `type T ~ { Key node[string]; Value node[string] }` or a
  field of type `{ Line int }`. It is the error "unnamed struct types are not
  supported yet"; `type T = {...}` defines a struct type.

### Top, Bottom, Join and Meet

`_` is the top type: every value is a `_`, and `_ | T` is `_`. A value of type
`_` is assignable only to `_`; a [cast](actions.md#casts) such as `x.(T)` checks it at
run time and narrows it. No type is written `any`: the name is not a type, and the compiler
asks for `_`. `_|_` is the bottom type, which has no values:
`_|_ | T` is `T`, and `[]_|_` and `*_|_` are `_|_`. A struct field, a rule, a
list element or a cast whose type resolves to `_|_` is an error, also when
types are otherwise not checked.

`T | U` is the join of `T` and `U`: its values are the values of either (see
[Union Types](#union-types)). The type of a
choice is the join of the types of its alternatives.

`T & U` is the meet of `T` and `U`, the largest type assignable to both: the
one assignable to the other, `[]V & []W` is `[](V & W)`, `node[V] & node[W]` is
`node[V & W]`, and for unions and optional types it is the union of the meets
of their members. A meet of unrelated types is `_|_`. So `(node[string] | int) & node[_]` is
`node[string]`, and `int & string` is an error where a location has that type.

For a named node type `N`, `(N | int) & node[_]` is `N`, and `N & M` is `_|_` for a distinct named
node type `M`. For a struct type `Num`, `(Num | int) & node[_]` is `_|_`, since a struct is not a node.

### Node Type Names

The values that Rule Expressions produce have the shapes defined in
[Nodes and Values](nodes.md#node-model). `Node.Type()`, the `type` member of
the JSON form and generated code name the nodes that are not structs by their
kind:

| Name | Values | Type |
|:--|:--|:--|
| `string` | String nodes produced by literals, character classes, `.` and `a #atomic` | `node[string]` |
| `tuple` | Tuple nodes: values of sequences, and of [Operators Rule operators](operators-rule.md#concrete-syntax-tree) without an action, unless the rule is declared with a [named node type](operators-rule.md#named-node-types) | `node[(T1, T2, ...)]` |
| `list` | List nodes: values of repetitions | `node[[]T]` |
| `error` | Error values: ranges skipped by [error recovery](pragmas.md#recover) | `error` |
| `missing` | Opt-in literal insertions with empty text/range and `expected`/`message` string fields; Closure whole-input parsing only | `missing` |

The kind names in the first column are names of node kinds, not types:
diagnostics name these values by the types in the third column, which are as
precise as the expression that produced the value. `missing` is a kind of
`error`: a value of type `missing` is assignable to `error`.

A named node type `N` (see [Named Node Types](#named-node-types)) is not a kind and has no row here.
A node converted to `N` reports the name `N` in place of its underlying kind, in `Node.Type()`, in the
`type` member of the JSON form and in generated code.

## Names

User-defined type names and field names MUST begin with an uppercase ASCII
letter. Identifiers that begin with a lowercase ASCII letter are reserved for
the built-in types (`int`, `string`, `bool`, `node[T]`, `error`, `missing`),
and `_` and `_|_` name the top and bottom types.
Each type name MUST be defined at most once, and types and rules have separate
namespaces (see [Names](grammar-files.md#names)).

## Type Declaration Statements

A Type Declaration Statement `type ...` declares a type name. Type Declaration
Statements and [Rule Declaration Statements](grammar-files.md#rule-declaration-statements)
are the Declaration Statements of a grammar file. A Type Declaration Statement
is one of two distinct declarations:

| Form | Declaration | Declares |
|:--|:--|:--|
| `type T = S` | [Type Definition](#type-definitions) | a new type `T`, distinct from every other type: a struct type `S`, or a named node type `node[U]` |
| `type T ~ U` | [Type Alias](#type-aliases) | another name for the type `U`; it defines no type |

## Type Definitions

A Type Definition `type T = S` defines a new type `T` from the type specification
`S`. `S` is a struct type or a node type `node[U]`, with `U` any type, including `_` and
`T` itself; `type T = node[U]` defines a [named node type](#named-node-types). Any other
right-hand side is an error, and an alias is written `type T ~ U`.

### Struct Types

A struct type is a type of values with named fields. A struct is a value that an
action builds, not a node of the parse, so a struct type is not assignable to
`node[_]`.
Fields are separated by newlines
or semicolons. Field names MUST be unique within a struct. Constructor fields
and function arguments remain comma-separated.

```pego
type MyNode = {
    Field1 SomeType
    Field2 AnotherType
}
type Pair = { Key node[string]; Value node[string] }
```

A value of a struct type is created with a constructor such as `T{Field: value}` in an [action](actions.md#values-and-constructors).

`[]T{value, ...}` constructs a list value with explicit element Type `T`,
including an empty list. Every element must have a known static type assignable
to `T`; `nil` requires an optional element type, and nested lists are checked
recursively, so `[]int{1, 2}` and `[](node[string], Item){}` are list values.
Constructor fields and arguments use commas.

### Named Node Types

A Type Definition `type N = node[U]` defines a named node type `N` whose underlying type is
`node[U]`. Its values are nodes whose value has type `U`, and each carries the name `N` at run
time. `U` may be any type, including `_`, as in `type N = node[_]`, and `N` itself. A named node type is not an alias: it is distinct from `node[U]` and from every other named
node type, even one with the same underlying type. `U` may refer to `N` or to other types, as in
a recursive definition:

```pego
type Num = node[int]
type Ident = node[string]
type Tree = node[[]Tree]
```

Assignability to and from a named node type `N`:

- `N` is assignable to `N`, to `node[U]` whenever the underlying `node[W]` of `N` has `W`
  assignable to `U`, and to `node[_]`. A union whose members are all assignable to a location is
  assignable to it, as for any union.
- Nothing else converts implicitly to `N`: a `node[U]`, a `_`, or a value of another named node type
  `M` is not assignable to `N`. Two distinct named node types never meet implicitly.
- A rule converts its value only when its declared Type is exactly `N`, also through aliases. A declared
  union, even one of named node types, does not convert: its value is checked by assignability. An
  [Operators Rule](operators-rule.md#types) declared with exactly `N` converts the value of every
  operator, as described there.

A value becomes a value of `N` by a conversion, which has two forms:

- a constructor `N{x}` with exactly one positional argument `x`;
- a rule whose declared Type is exactly `N` (also through aliases) converts the value that the rule
  produces, as in `rule a Num = "a"`;
- an Operators Rule whose declared Type is exactly `N` converts the value of each prefix, infix and postfix
  operator it applies.

The conversion applies when every alternative of the static type of `x` (or of the rule's value) is a
node, named or unnamed, whose underlying type is assignable to the underlying type of `N`. So a value of
type `M`, whose underlying type fits, converts to `N`. An Operators Rule declared with exactly `N` converts the value of each operator, not the
values of its operands, by the same rule. The following are
rejected at compile time with `cannot convert X to N` (for `N{x}`) or
`rule r produces X, which cannot be converted to N` (for a rule): `_`, `node[_]`, `error`, `nil` and the
scalar types. Cast first, as in `$x.(node[string])`.

A conversion keeps the range and the content of the node, and the converted node has no rule name. Its
`Node.Type()` and the `type` member of its JSON form report `N`.

Constructor arity: a named node type takes exactly one positional argument, so `N{x, y}` and `N{Field: x}`
are errors. A struct type's constructor names its fields, so a positional argument to a struct type is an
error ("a struct constructor names its fields"). Mixing positional and named arguments in one constructor is a
syntax error.

A named node type has the members `.Start`, `.End` and `.Value`, and `len` and `text` accept it, as for its
underlying `node[U]`. Its `.Value` has type `U`.

## Type Aliases

A Type Alias `type T ~ U` declares `T` as another name for the type `U`. With `|`, the
right-hand side is a union type, whose values are the values of any of its
members, much like an interface type in Go.

```pego
type Node ~ Expression | Statement | Declaration
type Entry ~ (node[string], Value)
type Words ~ node[[]node[string]]
```

A type alias MUST NOT refer to itself, directly or indirectly. Unions are
normalized: nested unions are flattened and duplicate members are removed (so
`T | T` is `T`), and a union with an optional member is optional
(`A | *B` is `*(A | B)`).

### Union Types

A union type `A | B` is written on the right-hand side of an alias or wherever
a Type Expression is expected. An alias that resolves to a single struct type
preserves that type's identity. A constructor accepts aliases of struct types and produces a node of
the underlying struct type. This also applies to unions normalized to a single
type, such as `T | T`.

## Assignability

A value of type `V` is assignable to a location of type `T` (a struct field, the
declared type of a rule, and so on) if any of the following holds:

- `V` and `T` are the same type.
- `T` is `_`, or `V` is `_|_`.
- `V` is a union type and every member of `V` is assignable to `T`.
- `T` is a union type and `V` is assignable to one of its members.
- `T` is `*U`, and `V` is `nil`, or `V` is `*W` with `W` assignable to `U`, or
  `V` is assignable to `U`.
- `T` is `node[_]` and `V` is `node[W]` for any `W`: every node is a `node[_]`.
- `T` is `node[U]` and `V` is `node[W]` with `W` assignable to `U`, whatever
  captures the node has.
- `V` is a named node type `N` with underlying `node[W]`, and `T` is `node[U]` with `W` assignable to
  `U`, whatever captures the node has.
- `T` is a named node type `N` and `V` is any other type except `N` itself: not assignable; a conversion
  is required (see [Named Node Types](#named-node-types)).
- `V` and `T` are distinct named node types: not assignable.
- `T` is `[]U` and `V` is `[]W` with `W` assignable to `U`.
- `T` is `(U1, ..., Un)` and `V` is `(W1, ..., Wn)`, with the same number of
  elements and each `Wi` assignable to `Ui`.
- `T` is `error` and `V` is `missing`.

An error value from [error recovery](pragmas.md#recover) is assignable only to a
type that admits `error`, such as `T | error`. A struct is not a node, and a
node is not a tuple or a list: `node[(A, B)]` is not assignable to `(A, B)`,
whose values are the values of the node.

A value of type `*T` is not assignable to a location of type `T`, and accessing
a field of a value that may be `nil` is an error. A value of type `_` is
assignable only to `_`.
