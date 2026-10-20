# Type Checking

Every rule has a type, and the checker verifies, when a grammar is compiled,
that the values flowing through the grammar are of the types they are used as.
This chapter defines the type of each Rule Expression, how the type of a rule
without an explicit Type Expression is inferred, and the errors that the checker
reports. The types themselves and the relation "assignable" are defined in the
[Type System](types.md).

## Rule Expression Types

| Rule Expression | Type |
|:--|:--|
| Literal, character class, `.`, `a #atomic`, `_` | `node[string]` |
| `a b` | `node[(A, B)]`, a tuple node of the types of the elements; the elements of a group that is not captured are elements of the enclosing sequence |
| `a / b` | `A \| B`, the join of the types of `a` and `b` |
| `a*`, `a+`, `a #repeat(...)` | `node[[]A]`, a list node of the type of the element |
| `a?` | `*A`, where `A` is the type of `a` |
| `&a`, `!a` | `node[()]`, a node of the empty tuple |
| Rule call | The declared type of the called rule, or its inferred type |

A node type also records the captures made in the node, which
[`x.$y`](actions.md#field-access) reads; diagnostics show them after the
value, as in `node[(node[string], num), {"k": 1}]`.

### Types of Captures

The type of a capture `name:a` is the type of `a`. If the capture is made only
in some alternatives of a choice, or inside an optional expression, it may not
match, and its type is `*T` instead. A name captured more than once in a match
has the join of the types of its captures, since the last capture that matches
gives its value; when at least one of those captures is always made, the join is
taken without nil, and otherwise each capture that may not be made contributes nil,
so the type is optional, `*T`.

```pego
rule r = x:"a"? y:("b" / z:"c") // $x: *node[string], $y: node[string], $z: *node[string]
rule s = k:num "=" k:("x" "y")  // $k: num | node[(node[string], node[string])]
```

## Action Expression Types

Action Expressions in [Value Actions](actions.md) and [Predicate Actions](predicates.md#predicate-actions)
have the following types.

| Action Expression | Type |
|:--|:--|
| Integer literal, `len(x)`, `x.Start`, `x.End` | `int` |
| `x.Value` | `T` for a node of type `node[T]` or of a named node type over it: `string` for a string node, `(A, B)` for a tuple node and `[]A` for a list node |
| String literal, `text(x)` | `string` |
| `true`, `false`, a comparison, `&&`, `\|\|`, `!` | `bool` |
| `nil` | `nil`, assignable to any optional type |
| `$label` | The type of the capture (see above) |
| `T{Field: value, ...}` | Struct Type `T` |
| `[]T{a, b, ...}` | `[]T`; every element must have a known Type assignable to `T` |
| `x.Name` | The type of the field `Name`; optional (`*T`) if `x` has a union type and only some members declare the field |
| `x.$y` | The join of the types of capture `y` of the node types `x` may have |
| `x[i]` | The element type of `x`, which MUST be a list, a tuple, or a node whose value is a list or a tuple; `i` MUST be an `int`, and a constant index into a tuple selects that element's type |
| `x.(T)` | `T`; the type of `x` or one of its alternatives MUST be assignable to `T` or one of its alternatives, or the other way around |
| `foldl(init, l, f)`, `foldr(init, l, f)` | The type of `init`, widened to the union of it and the type of `f`'s result |
| `map(l, f)` | `[]T`, where `T` is the type of `f`'s result |
| `list(a, b, ...)` | `[]T`, where `T` is the union of the types of the arguments |
| `concat(l1, l2, ...)` | `[]T`, where `T` is the union of the element types of the arguments |

The list functions treat `nil` as an empty list, which contributes no element
Type: `concat(nil, l)` has the Type of `concat(l)`, and `filter(nil, f)` is
assignable to every list Type.

For `[]T{...}`, the checker validates every element against the explicit element
Type `T`. It reports non-assignable elements and elements whose Type is unknown,
including unknown values nested in a typed list. An empty literal still has
the declared `[]T` Type. Nil is allowed only if `T` permits nil.

The internal compiler option `NoTypeCheck` deliberately bypasses assignability
checks. It does not change the rules for checked compilation.

A struct field value whose Type is unknown (`any`, a variable, or a list with
elements of unknown Type) is not reported. The checker records the field, and
the constructor checks the value against the declared field Type at run time;
see [Values and Constructors](actions.md#values-and-constructors). A value whose
Type is known and assignable needs no run-time check. Without type checking,
every field not declared `any` is checked at run time.

Likewise, a rule value whose Type is unknown, from an action, a called rule or
an Operators Rule line, is not reported; the rule checks the value against its
declared Type at run time (see [Types and Errors](actions.md#types-and-errors)).
Without type checking, every rule value is checked against a declared Type other
than `any`, including the tuple node of an operator without an action.

Diagnostics print a type as written in a grammar: `node[string]` for a string
node, `node[(node[string], node[string])]` for a tuple node of two string nodes,
`(node[string], node[string])` for its value and `[]node[string]` for a list
value. A reference to a rule without a declared type prints the rule's name,
also where a recursive type refers to itself, as in `[]*expr`. Run-time errors
name a value the same way, writing `_` for elements they do not examine:
`node[(_, _)]` for a tuple node of two elements, `node[[]_]` for a list node
and `error` for an error value.

A named node type is printed by its name, as `Num`, and `cannot convert X to N` names both types.

A run-time check tests a value as the value types describe it. `node[T]` takes
a node whose value `T` takes, so `node[string]` takes a string node,
`node[(A, B)]` a tuple node of two elements of types `A` and `B`, and
`node[[]A]` a list node of elements of type `A`; `node[_]` takes every node; a
tuple Type takes a tuple value of as many elements; a list Type takes a list
value, such as one an action builds, and not the node of a repetition, whose
list is its `Value`; `[]_` and `[]any` take every list value; and an error
value from recovery fits only a Type that admits `error`. Predicates are checked like actions, and a
value that fails its check makes the predicate fail.

The argument of `len` and `text` MUST be a string or a node (possibly `nil`);
the list arguments of `foldl`, `foldr`, `map` and `concat` MUST be lists, and
an error value (from [error recovery](pragmas.md#recover)) counts as an empty list.
A function (`(acc, item) => ...`) can appear only as an argument of `foldl`,
`foldr` or `map`, and it has the number of parameters those functions require.

## Type Inference

The type of a rule without a declared type is inferred from its body, or from
its action if it has one. Dependencies are inferred before their callers, so
finite acyclic chains do not depend on definition order or an iteration cap.
Recursive dependency groups are iterated until their types no longer change;
union-member ordering does not count as a type change. Operators Rule operator actions
also depend on their rule's type through the `lhs` and `rhs` values.

The implementation bounds changing recursive types conservatively. After an
initial inferred result, further growth beyond 2,048 bytes in the written type
or 50 increases in structural nesting depth widens that rule to `any` for the
rest of inference. Repeating non-stable type states widen only the rules that
vary in the cycle. Stable peers, acyclic rules and stable oversized recursive
types retain their types. These are resource limits, not proofs of infinite
growth: a finite recursive type can also reach them. Declare recursive result
types when precise checking is required.

```pego
rule num = [0-9]+ #atomic // node[string]
rule pair = k:num "=" v:num // node[(num, node[string], num), {"k": 0, "v": 2}]
```

The type of the accumulator of `foldl` and `foldr` is widened to the union of
the type of the initial value and the type of the result of the function.

## Checked Errors

The type checker reports the following errors:

- an undefined type, a type alias that refers to itself, and a duplicate field;
- a field that the struct type does not declare, in a struct constructor or a field access,
  and a field value that is not assignable to the type of the field;
- a typed-list element that is unknown or not assignable to its explicit element Type;
- a rule whose value is not assignable to its declared type;
- a constructor `N{...}` of a named node type with other than one positional argument, or a positional argument
  to a struct type;
- a value that cannot be converted to a named node type where a conversion is required (`N{x}` or a rule's
  declared type), including `_`, `node[_]`, `error`, `nil` and scalars;
- a cast to a named node type that the static types rule out (see [Casts](actions.md#casts)).
- a field access on a value that may be `nil`;
- an operand of an operator or an argument of a built-in function with an
  invalid type;
- a variable that is read or bound without a declaration, a binding whose value
  is not assignable to the declared type of the variable, and a variable
  declaration with an unsupported type or an initial value of another type
  (see [Variables](predicates.md#variables)).

A variable has the type its `var` declaration gives it, so a read of a variable
is checked like any other value of that type: `x + 1` and `V{N: x}` are checked
when the grammar is compiled. The value of a binding whose static type is
unknown (`any`) is checked against the declared type at run time.
