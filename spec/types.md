# Type System

PEGO is statically typed. Types describe the values that rules produce and the
shape of the nodes that actions build. This chapter defines the types, how they
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
| `terminal` | Any terminal: a `Match` node or a node of a terminal type. |
| `[]T` | A list of `T`. A list value is a node (a `List` node). |
| `*T` | A `T` or `nil`. |
| `T` | A user-defined type. |

The value of a rule, and the result of an action, MUST be a node (or `nil`);
`int`, `string` and `bool` values occur in action expressions, in struct fields
and in [variables](predicates.md#variables).

A Type Expression is written with the following syntax. `[]` and `*` apply to
the type that follows, `|` forms a union (see below) and parentheses group.

```pego
type Example struct {
    Items  []Item       // a list
    Maybe  *Item        // an Item or nil
    Either Item | Other // a union
    Many   [](Item | Other)
    Count  int
}

type LongUnion = (
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

### Reserved Node Types

The node types that Rule Expressions produce are predefined and can be used
as types.

| Type | Values |
|:--|:--|
| `Match` | Terminals produced by literals, character classes, `.`, `a #atomic` and `_` |
| `Seq` | Values of sequences |
| `List` | Values of repetitions |
| `Operator` | Values of [Operators Rule operators](operators-rule.md#concrete-syntax-tree) without an action |
| `Error` | Ranges skipped by [error recovery](pragmas.md#recover) |
| `Missing` | Opt-in literal insertions with empty text/range and `expected`/`message` string fields; Closure whole-input parsing only |

## Names

User-defined type names and field names MUST begin with an uppercase ASCII
letter. Identifiers that begin with a lowercase ASCII letter are reserved for
the built-in types (`int`, `string`, `bool`, `node`, `terminal`). The names of
the reserved node types (`Match`, `Seq`, `List`, `Operator`, `Error`, `Missing`) MUST NOT
be defined.
Each type name MUST be defined at most once, and types and rules have separate
namespaces (see [Names](grammar-files.md#names)).

## Type Definitions

The `type` keyword defines a type.

### Struct Types

A struct type is a node type with named fields. Fields are separated by newlines
or semicolons. Field names MUST be unique within a struct. Constructor fields
and function arguments remain comma-separated.

```pego
type MyNode struct {
    Field1 SomeType
    Field2 AnotherType
}
type Pair struct { Key Match; Value Match }
```

A node of a struct type is created with a constructor such as `T{Field: value}` in an [action](actions.md#values-and-constructors).

`[]T{value, ...}` constructs a node list with explicit element Type `T`,
including an empty list. Every element must have a known static type assignable
to `T`; `nil` requires an optional element type, and nested lists are checked
recursively. Primitive-valued list construction is unsupported. A declared
terminal or its alias can be constructed as `T{stringValue}`. The single string
becomes its text without being parsed again. Constructor fields and arguments
use commas.

### Type Aliases and Union Types

`type T = U` defines `T` as another name for the type `U`. With `|`, the
right-hand side is a union type, whose values are the values of any of its
members, much like an interface type in Go.

```pego
type Node = Expression | Statement | Declaration
```

A type alias MUST NOT refer to itself, directly or indirectly. Unions are
normalized: nested unions are flattened and duplicate members are removed (so
`Match | Match` is `Match`), and a union with an optional member is optional
(`A | *B` is `*(A | B)`).

An alias that resolves to a single struct or terminal type preserves that
type's identity. A constructor accepts aliases of struct types and produces a node of
the underlying struct type. A rule declared with an alias of a terminal type
has the same terminal conversion as a rule declared with that terminal type.
This also applies to unions normalized to a single type, such as `T | T`.

### Terminal Types

`type T terminal` defines a terminal type: a node type for terminals, which have
no internal structure. A rule declared with a terminal type and without an
action produces, whatever its body, a terminal of that type holding the matched
text. If the rule has an action, the action's value is the rule's value, and it
must be assignable to the terminal type like any other declared type.

```pego
type Identifier terminal
rule ident Identifier = [a-z]+ // produces Identifier"abc", not a List
```

## Assignability

A value of type `V` is assignable to a location of type `T` (a struct field, the
declared type of a rule, and so on) if any of the following holds:

- `V` and `T` are the same type.
- `V` is a union type and every member of `V` is assignable to `T`.
- `T` is a union type and `V` is assignable to one of its members.
- `T` is `*U`, and `V` is `nil`, or `V` is `*W` with `W` assignable to `U`, or
  `V` is assignable to `U`.
- `T` is `node` and `V` is a node type.
- `T` is `terminal` and `V` is `Match`, a terminal type or `terminal`.
- `T` is `[]U` and `V` is `[]W` with `W` assignable to `U`.
- `T` is `Seq` and `V` is a `Seq` with fields; `T` is `List` and `V` is a list
  type.
- `V` is `Error` (the node produced by error recovery) and `T` is a node type.

A value of type `*T` is not assignable to a location of type `T`, and accessing
a field of a value that may be `nil` is an error.
