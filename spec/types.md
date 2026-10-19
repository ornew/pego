# Type System

PEGO is statically typed. Types describe the values that rules produce (see [Nodes and Values](nodes.md)) and the
shape of the values that actions build. This chapter defines the types, how they
are declared, their names and when a value of one type may be used where another
is expected. How the type of an expression is determined, and the errors that
the checker reports, are defined in [Type Checking](type-checking.md). Type
errors are reported when the grammar is compiled, with the position of the
offending construct.

## Built-In Types

| Type | Description |
|:--|:--|
| `int` | An integer. Positions in the input are also `int` values (see [Positions](parsing.md#positions)). |
| `string` | A string. `len` measures its length in the [position unit](parsing.md#positions). |
| `bool` | A boolean. |
| `node` | Any node. |
| `node[string]` | A string node: the node of a literal, a character class, `.` or an atomic expression. |
| `error` | The value of [error recovery](pragmas.md#recover): an error value or a `#missing` insertion. |
| `[]T` | A list of `T`: a list value that an action builds. The node of a repetition is a list node, a `node` whose list is its `.Value`. |
| `*T` | A `T` or `nil`. |
| `T & U` | The values that are both a `T` and a `U` (see [Top, Bottom and Meet](#top-bottom-and-meet)). |
| `_` | Every value: the top type. |
| `_\|_` | No value: the bottom type. |
| `T` | A user-defined type. |

The value of a rule, and the result of an action, MUST be a node (or `nil`);
`int`, `string` and `bool` values occur in action expressions, in struct fields
and in [variables](predicates.md#variables).

A Type Expression is written with the following syntax. `[]` and `*` apply to
the type that follows, `&` forms a meet and binds tighter than `|`, which forms
a union (see below), and parentheses group.

```pego
type Example = {
    Items  []Item       // a list
    Maybe  *Item        // an Item or nil
    Either Item | Other // a union
    Many   [](Item | Other)
    Count  int
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
group, a Type Expression follows the normal hard newline Definition Statement boundary.

### Reserved Type Syntax

Three forms of Type Expression are reserved for value types that are not
supported yet. Grammars that use them parse, format and convert to JSON, but
do not compile, also when types are otherwise not checked:

- A tuple type is written in parentheses with commas: `()` is the unit type,
  `(T,)` has one element and `(T, U)` two; a trailing comma is optional after
  two or more elements. Parentheses around a single type without a comma group
  it.
- A type application `T[U, ...]` applies a type to type arguments, as in
  `node[Expr]`. The `[` follows the name directly, without a space. Only `node`
  takes type arguments, and of its applications only `node[string]` is
  supported; any other type takes none.
- An unnamed struct type is written with the braces and fields of a struct
  definition, as in `type T ~ { Key node[string]; Value node[string] }` or a field of type
  `{ Line int }`. `type T = {...}` remains the way to define a struct type.

### Top, Bottom and Meet

`_` is the top type: every value is a `_`, and `_ | T` is `_`. A value of type
`_` is assignable only to `_` and to `any`, whose values are checked where they
reach a typed location; a [cast](actions.md#casts) such as `x.(T)` checks it at
run time. `any` is deprecated: unlike `_`, it is also assignable to every
other type, so its values are checked where they are used rather than where a
cast narrows them. `_|_` is the bottom type, which has no values:
`_|_ | T` is `T`, and `[]_|_` and `*_|_` are `_|_`. A struct field, a rule, a
list element or a cast whose type resolves to `_|_` is an error, also when
types are otherwise not checked.

`T & U` is the meet of `T` and `U`, the largest type assignable to both: the
one assignable to the other, `[]V & []W` is `[](V & W)`, and for unions and
optional types it is the union of the meets of their members. A meet of unrelated types is `_|_`, so
`(node[string] | int) & node` is `node[string]` while `int & string` is an error where a
location has that type.

### Node Type Names

The values that Rule Expressions produce have the shapes defined in
[Nodes and Values](nodes.md#node-model). `Node.Type()` returns the following
names for nodes that are not structs, and type-checking diagnostics use them
for the inferred types of such values. Except for `Missing`, they are not
types of the grammar language: a Type Expression that names one is an error,
which `pego fix` rewrites to the type in the second column.

| Name | Values | Type |
|:--|:--|:--|
| `Match` | String nodes produced by literals, character classes, `.` and `a #atomic` | `node[string]` |
| `Seq` | Tuple nodes: values of sequences | `node` |
| `List` | List nodes: values of repetitions | `node` |
| `Operator` | Tuple nodes of [Operators Rule operators](operators-rule.md#concrete-syntax-tree) without an action | `node` |
| `Error` | Error values: ranges skipped by [error recovery](pragmas.md#recover) | `error` |
| `Missing` | Opt-in literal insertions with empty text/range and `expected`/`message` string fields; Closure whole-input parsing only | `Missing` |

`terminal`, the former type of every string node, is likewise an error that
`pego fix` rewrites to `node[string]`.

## Names

User-defined type names and field names MUST begin with an uppercase ASCII
letter. Identifiers that begin with a lowercase ASCII letter are reserved for
the built-in types (`int`, `string`, `bool`, `node`, `error`), and `_` and
`_|_` name the top and bottom types. The [node type names](#node-type-names)
(`Match`, `Seq`, `List`, `Operator`, `Error`, `Missing`) MUST NOT
be defined.
Each type name MUST be defined at most once, and types and rules have separate
namespaces (see [Names](grammar-files.md#names)).

## Type Definitions

The `type` keyword declares a type name in one of two forms:

| Form | Declares |
|:--|:--|
| `type T = {...}` | a type definition: a new struct type `T` |
| `type T ~ U` | an alias: another name for the type `U`, which creates no type |

Only struct types can be defined with `=`: `type T = U` with any other
right-hand side is an error, and an alias is written `type T ~ U`.

### Struct Types

A struct type is a type of values with named fields. `type T = {...}` defines it.
A struct is a value that an action builds, not a node of the parse, so a struct
type is not assignable to `node`.
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

A node of a struct type is created with a constructor such as `T{Field: value}` in an [action](actions.md#values-and-constructors).

`[]T{value, ...}` constructs a list value with explicit element Type `T`,
including an empty list. Every element must have a known static type assignable
to `T`; `nil` requires an optional element type, and nested lists are checked
recursively. Primitive-valued list construction is unsupported. Constructor
fields and arguments use commas.

### Type Aliases and Union Types

`type T ~ U` declares `T` as another name for the type `U`. With `|`, the
right-hand side is a union type, whose values are the values of any of its
members, much like an interface type in Go.

```pego
type Node ~ Expression | Statement | Declaration
```

A type alias MUST NOT refer to itself, directly or indirectly. Unions are
normalized: nested unions are flattened and duplicate members are removed (so
`T | T` is `T`), and a union with an optional member is optional
(`A | *B` is `*(A | B)`).

An alias that resolves to a single struct type preserves that type's
identity. A constructor accepts aliases of struct types and produces a node of
the underlying struct type. This also applies to unions normalized to a single
type, such as `T | T`.

### Text Structs

A text struct is a struct type whose only field is `Text string`. It gives a
token a type of its own: a rule builds it from the text of an atomic match.

```pego
type Identifier = { Text string }
rule ident Identifier = t:([a-z]+) #atomic => Identifier{Text: text($t)}
```

A text struct is an ordinary struct type. `type T terminal`, which declared a type for string nodes, is an error that
`pego fix` rewrites to a text struct, and the rules declared with it to rules
like the one above.

## Assignability

A value of type `V` is assignable to a location of type `T` (a struct field, the
declared type of a rule, and so on) if any of the following holds:

- `V` and `T` are the same type.
- `T` is `_`, or `V` is `_|_`.
- `V` is a union type and every member of `V` is assignable to `T`.
- `T` is a union type and `V` is assignable to one of its members.
- `T` is `*U`, and `V` is `nil`, or `V` is `*W` with `W` assignable to `U`, or
  `V` is assignable to `U`.
- `T` is `node` and `V` is the type of a node: a string, tuple or list node.
- `T` is `[]U` and `V` is `[]W` with `W` assignable to `U`.

An error value from [error recovery](pragmas.md#recover) is assignable only to a
type that admits `error`, such as `T | error`.

A value of type `*T` is not assignable to a location of type `T`, and accessing
a field of a value that may be `nil` is an error. A value of type `_` is
assignable only to `_` and `any`.
