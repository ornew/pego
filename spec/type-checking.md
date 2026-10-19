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
| `a b` | A tuple node of the types of the elements, a `node[_]`; if the sequence has captures, a tuple node with the corresponding fields |
| `a / b` | The union of the types of `a` and `b` |
| `a*`, `a+`, `a #repeat(...)` | `[]T`, where `T` is the type of the element; if the element has captures, `T` is a tuple node with the corresponding fields |
| `a?` | `*T`, where `T` is the type of `a` |
| Rule call | The type of the called rule |

### Types of Captures

The type of a capture `name:a` is the type of `a`. If the capture is made only
in some alternatives of a choice, or inside an optional expression, it may not
match, and its type is `*T` instead.

```pego
rule r = x:"a"? y:("b" / z:"c") // $x: *node[string], $y: node[string], $z: *node[string]
```

## Action Expression Types

Action Expressions in [Value Actions](actions.md) and [Predicate Actions](predicates.md#predicate-actions)
have the following types.

| Action Expression | Type |
|:--|:--|
| Integer literal, `len(x)`, `x.Start`, `x.End` | `int` |
| `x.Value` | `string` for a string node; the list of the element types for a tuple or list node |
| String literal, `text(x)` | `string` |
| `true`, `false`, a comparison, `&&`, `\|\|`, `!` | `bool` |
| `nil` | `nil`, assignable to any optional type |
| `$label` | The type of the capture (see above) |
| `T{Field: value, ...}` | Struct Type `T` |
| `[]T{a, b, ...}` | `[]T`; every element must have a known Type assignable to `T` |
| `x.Name` | The type of the field `Name`; optional (`*T`) if `x` has a union type and only some members declare the field |
| `x.$y` | The join of the types of capture `y` of the node types `x` may have |
| `x[i]` | The element type of the list `x`; `i` MUST be an `int` |
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
checks. It does not add primitive-valued list storage or change the rules for
checked compilation.

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
than `any`, except the tuple node of an operator line without an action.

Diagnostics print the shape of a value type as `node[string]` for a string node,
`(node[string], node[string])` for a tuple of two and `[]node[string]` for a list.
A node whose value is not known statically is `node[_]`, and a tuple node with
captures `node{k: T, ...}`. Run-time errors name a value the same way, writing
`_` for elements they do not examine: `node[(_, _)]` for a tuple node of two
elements, `node[[]_]` for a list node and `error` for an error value.

A run-time check tests a value as the value types describe it. `node[string]`
takes a string node; `node[_]` takes every node; a list Type takes a list value, such as one an
action builds, and not the node of a repetition, whose list is its `Value`;
`[]_` and `[]any` take every list value; and an error value from recovery fits
only a Type that admits `error`. Predicates are checked like actions, and a
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
rule pair = k:num "=" v:num // a tuple node of three string nodes, with captures k and v; diagnostics print node{k: node[string], v: node[string]}
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
- a rule or an action whose value is not a node;
- a field access on a value that may be `nil`;
- an operand of an operator or an argument of a built-in function with an
  invalid type;
- a predicate that defines a variable with a value other than `int`, `string`
  or `bool`.

Variables used in predicates are defined by the calling rules, so references to
variables are not type-checked. If the value of a variable has an unexpected
type at run time, the predicate fails.
