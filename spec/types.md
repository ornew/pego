# Type System

PEGO is statically typed. Types describe the values that rules produce and the
shape of the nodes that actions build. Type errors are reported when the grammar
is compiled, with the position of the offending construct.

## Built-in types

| Type | Description |
|:--|:--|
| `int` | An integer. Positions in the input are also `int` values (see [Positions](overview.md#positions)). |
| `string` | A string. `len` measures its length in the [position unit](overview.md#positions). |
| `bool` | A boolean. |
| `node` | Any node. |
| `terminal` | Any terminal: a `Match` node or a node of a terminal type. |
| `[]T` | A list of `T`. A list value is a node (a `List` node). |
| `*T` | A `T` or `nil`. |
| `T` | A user-defined type. |

The value of a rule, and the result of an action, MUST be a node (or `nil`);
`int`, `string` and `bool` values occur in action expressions, in struct fields
and in [variables](predicates.md#variables).

### Reserved node types

The node types that parsing expressions produce are predefined and can be used
as types.

| Type | Values |
|:--|:--|
| `Match` | Terminals produced by literals, character classes, `.`, `@a` and `_` |
| `Seq` | Values of sequences |
| `List` | Values of repetitions |
| `Operator` | Values of [Pratt operators](pratt.md#concrete-syntax-tree) without an action |
| `Error` | Ranges skipped by [error recovery](attributes.md#recover) |

User-defined type names and field names MUST begin with an uppercase letter.
The built-in type names and the reserved node type names MUST NOT be defined.

## Type definitions

The `type` keyword defines a type.

### Struct types

A struct type is a node type with named fields. Fields are separated by newlines
or commas. Field names MUST be unique within a struct.

```pego
type MyNode struct {
    Field1 SomeType
    Field2 AnotherType
}
type Pair struct { Key Match, Value Match }
```

### Type aliases and union types

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

### Terminal types

`type T terminal` defines a terminal type: a node type for terminals, which have
no internal structure. A rule declared with a terminal type produces, whatever
its body, a terminal of that type holding the matched text.

```pego
type Identifier terminal
def ident: Identifier = (?a-z)+   // produces Identifier"abc", not a List
```

## Types of expressions

| Expression | Type |
|:--|:--|
| Literal, character class, `.`, `@a`, `_` | `Match` |
| `a b` | `Seq`; if the sequence has captures, a `Seq` with the corresponding fields |
| `a / b` | The union of the types of `a` and `b` |
| `a*`, `a+`, `a{n,m}` | `[]T`, where `T` is the type of the element; if the element has captures, `T` is a `Seq` with the corresponding fields |
| `a?` | `*T`, where `T` is the type of `a` |
| Rule call | The type of the called rule |

### Types of captures

The type of a capture `name:a` is the type of `a`. If the capture is made only
in some alternatives of a choice, or inside an optional expression, it may not
match, and its type is `*T` instead.

```pego
def r = x:"a"? y:("b" / z:"c")   // $x: *Match, $y: Match, $z: *Match
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

## Type inference

The type of a rule without a declared type is inferred from its body, or from
its action if it has one. For recursive rules, inference is repeated until the
types of all rules no longer change.

```pego
def num = @(?0-9)+         // Match
def pair = k:num "=" v:num // Seq{k: Match, v: Match}
```

The type of the accumulator of `foldl` and `foldr` is widened to the union of
the type of the initial value and the type of the result of the function.

## Checked errors

The type checker reports the following errors:

- an undefined type, a type alias that refers to itself, and a duplicate field;
- a field that the struct type does not declare, in `new` or in a field access,
  and a field value that is not assignable to the type of the field;
- a rule whose value is not assignable to its declared type (except that a rule
  declared with a terminal type always produces a terminal of that type);
- a rule or an action whose value is not a node;
- `$n` out of range, and a field access on a value that may be `nil`;
- an operand of an operator or an argument of a built-in function with an
  invalid type;
- a predicate that defines a variable with a value other than `int`, `string`
  or `bool`.

Variables used in predicates are defined by the calling rules, so references to
variables are not type-checked. If the value of a variable has an unexpected
type at run time, the predicate fails.
