# Actions

An action is an expression written after `->` in a rule definition. It computes
the value of the rule, typically an AST node, from what the parsing expression
matched.

```pego
def <rule name>: <type> = <parsing expression> -> <action>
```

When the parsing expression succeeds, the action is evaluated and its result
becomes the value of the rule. The result MUST be a node or `nil`.

The action applies to the whole right-hand side of the rule. To compute
different values for different alternatives, give each alternative its own rule.

## Capture references

| Syntax | Refers to |
|:--|:--|
| `$label` | The value captured with `label:` |
| `$0` | A list of the values of all elements of the rule body that have a value |
| `$n` | The `n`-th (1-based) element with a value of the sequence that forms the rule body |

```pego
def key_value = k:(?a-z)+ op:"=" v:(?0-9)+
    -> new KV{Key: $1, Op: $2, Value: $3}  // same as $k, $op and $v
```

`$n` counts only elements that have a value (see
[Values and the concrete syntax tree](parser-expressions.md#values-and-the-concrete-syntax-tree)).
If the rule body is not a sequence, `$1` is the value of the body. It is an
error if `n` exceeds the number of elements with a value.

Captures are visible only within the rule that makes them; an action cannot
refer to the captures of the rules that called it. A capture that did not match
is `nil`.

## Variable references

An action can read [variables](predicates.md#variables) by name, like a
predicate. It sees the definitions in effect when the rule body has matched:
those made by the rule itself and by the rules that called it, but not those of
the rules it called, which ended with their invocations. Reading a variable that
is not defined is a runtime error of the action.

```pego
type Item struct { Depth int, Name Match }
def item: Item = [depth = 1] n:name -> new Item{Depth: depth, Name: $n}
```

## Values

Action expressions operate on the following values.

| Value | Examples |
|:--|:--|
| Node | `$label`, `new T{...}` |
| Integer (`int`) | `1`, `len($x)` |
| String (`string`) | `"abc"`, `text($x)` |
| Boolean (`bool`) | `true`, `false`, `1 < 2` |
| Nil | `nil` |
| Function | `(acc, i) => ...` (only as an argument of `foldl`, `foldr` and `map`) |

String literals in actions use the same escape sequences as string literals in
parsing expressions (see [Literals](overview.md#literals)). Integer literals are
decimal; a negative number is written with the unary `-` operator.

A list is a `List` node. Lists are produced by repetitions and by the built-in
functions `list`, `map` and `concat`.

## Creating struct nodes

`new T{Field: value, ...}` creates a node of the struct type `T`. Fields are
separated by commas. A field that is not given is absent and reads as `nil`.

```pego
def rule = name:ident -> new IdentNode{Name: $name}
```

The range of a node created by `new`:

- If the node is the result of the action, its range is the range that the rule
  matched.
- Otherwise (for example, a node created inside the function passed to
  `foldl`), its range is the smallest range that covers the ranges of its fields
  whose values are nodes, or the range that the rule matched if there are no
  such fields.

A list created by `list`, `map` or `concat` has the smallest range that covers
its elements; an empty list has an empty range at the start of the rule's
match.

## Field access

`x.Name` reads the field `Name` of the node `x`.

```pego
type Name terminal
type A struct {
    FieldName Name
}

def name: Name = (?a-z)+
def a: A = n:name -> new A{FieldName: $n}
def foo = a -> $1.FieldName  // the Name node stored in the A node
```

A field that was not set, and a capture that did not match, read as `nil`.
Accessing a field that the struct type does not declare is an error.

When `x` has a union type, `x.Name` is allowed if at least one member type
declares `Name`. If only some members declare it, the static type of the access
is optional (`*T`). Reading a field from a value whose struct type does not
declare it is a [run-time error](#run-time-errors).

The following fields are available on every node:

| Field | Type | Value |
|:--|:--|:--|
| `startPos` | `int` | The start position of the node in the [position unit](overview.md#positions) |
| `endPos` | `int` | The end position of the node (exclusive) |
| `children` | `[]*node` | The children of a `Seq`, `List` or `Operator` node; empty for other nodes |

## Operators

| Operator | Operands |
|:--|:--|
| `+` | integers (addition), strings (concatenation) |
| `-`, `*`, `/`, `%` | integers |
| `<`, `<=`, `>`, `>=` | integers, strings |
| `==`, `!=` | any values; nodes are compared by identity |
| `&&`, `\|\|`, `!` | booleans; `&&` and `\|\|` short-circuit |
| unary `-` | integers |

Integer division truncates toward zero; `%` takes the sign of the dividend.
Strings are compared lexicographically.

Operators bind in the following order, from loosest to tightest:

| Precedence | Operators | Associativity |
|:--|:--|:--|
| 1 (loosest) | `\|\|` | left |
| 2 | `&&` | left |
| 3 | `==`, `!=`, `<`, `<=`, `>`, `>=` | not chainable: `a < b < c` is a syntax error |
| 4 | `+`, `-` | left |
| 5 | `*`, `/`, `%` | left |
| 6 (tightest) | unary `-`, `!` | — |

Field access (`.`) and function calls bind tighter than all operators.
Parentheses group subexpressions.

## Built-in functions

| Function | Description |
|:--|:--|
| `len(x)` | The length of a string or a terminal in the [position unit](overview.md#positions); for any other node, the number of children; `0` for `nil` |
| `text(x)` | The input text that the node `x` covers (for a terminal, its text); `""` for `nil` |
| `foldl(init, list, (acc, item) => ...)` | Folds `list` from the left |
| `foldr(init, list, (acc, item) => ...)` | Folds `list` from the right |
| `map(list, (item) => ...)` | A list of the results of applying the function to each element |
| `list(a, b, ...)` | A list of the arguments, which MUST be nodes or `nil` |
| `concat(l1, l2, ...)` | The concatenation of the lists; a `nil` argument, and an `Error` node produced by `#recover`, count as empty lists. An argument whose type is a union of list types (such as `[]A | []B`) contributes elements of either type |

### `foldl` and `foldr`

`foldl` and `foldr` fold a list into a single value. They are typically used to
turn a flat list into a recursive tree.

- `init`: the initial value.
- `list`: the list to fold, usually the capture of a `*` or `+` repetition.
- `(acc, item) => expression`: the function applied to each element. `acc` is
  the result so far and `item` is the current element. `foldl` applies it from
  the first element to the last, `foldr` from the last element to the first.

Inside the function, parameters are referred to with `$`, as in `$acc`.

```pego
// "1+2+3" → Op{Left: Op{Left: 1, Op: "+", Right: 2}, Op: "+", Right: 3}
def expr: Node =
    l:term rest:(op:term_binary_op r:term)*
    -> foldl($l, $rest, (acc, i) => new Op{Left: $acc, Op: $i.op, Right: $i.r})
```

### `map`, `list` and `concat`

These functions are typically used to collect a separated list into a single
list. The function passed to `map` MUST return a node or `nil`.

```pego
def args = first:expr rest:(-"," e:expr)*
    -> concat(list($first), map($rest, (r) => $r.e))
```

## Types

Action expressions are [type-checked](types.md). For example, a value that is
not assignable to the type of a struct field, or an access to a field that does
not exist, is a compile-time error.

## Run-time errors

An error during the evaluation of an action, such as reading a field that the
node's type does not declare or dividing by zero, aborts the whole parse with
that error. It is not a syntax error and is not subject to backtracking or
[error recovery](attributes.md#recover).
