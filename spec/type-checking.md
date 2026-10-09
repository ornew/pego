# Type Checking

Every rule has a type, and the checker verifies, when a grammar is compiled,
that the values flowing through the grammar are of the types they are used as.
This chapter defines the type of each parsing expression, how the type of a rule
without a declaration is inferred, and the errors that the checker reports. The
types themselves and the relation "assignable" are defined in the
[Type System](types.md).

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

## Types of action and predicate expressions

The expressions of [actions](actions.md) and [predicates](predicates.md) have
the following types.

| Expression | Type |
|:--|:--|
| Integer literal, `len(x)`, `x.startPos`, `x.endPos` | `int` |
| String literal, `text(x)` | `string` |
| `true`, `false`, a comparison, `&&`, `\|\|`, `!` | `bool` |
| `nil` | `nil`, assignable to any optional type |
| `$label` | The type of the capture (see above) |
| `$n` | The type of the `n`-th item of the rule body |
| `new T{...}` | `T` |
| `x.Name` | The type of the field `Name`; optional (`*T`) if `x` has a union type and only some members declare the field |
| `foldl(init, l, f)`, `foldr(init, l, f)` | The type of `init`, widened to the union of it and the type of `f`'s result |
| `map(l, f)` | `[]T`, where `T` is the type of `f`'s result, which MUST be a node or `nil` |
| `list(a, b, ...)` | `[]T`, where `T` is the union of the types of the arguments, which MUST be nodes or `nil` |
| `concat(l1, l2, ...)` | `[]T`, where `T` is the union of the element types of the arguments |

The argument of `len` and `text` MUST be a string or a node (possibly `nil`);
the list arguments of `foldl`, `foldr`, `map` and `concat` MUST be lists, and
`Error` (from [error recovery](attributes.md#recover)) counts as an empty list.
A function (`(acc, item) => ...`) can appear only as an argument of `foldl`,
`foldr` or `map`, and it has the number of parameters those functions require.

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
  declared with a terminal type and without an action always produces a
  terminal of that type);
- a rule or an action whose value is not a node;
- `$n` out of range, and a field access on a value that may be `nil`;
- an operand of an operator or an argument of a built-in function with an
  invalid type;
- a predicate that defines a variable with a value other than `int`, `string`
  or `bool`.

Variables used in predicates are defined by the calling rules, so references to
variables are not type-checked. If the value of a variable has an unexpected
type at run time, the predicate fails.
