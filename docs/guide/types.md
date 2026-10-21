# Declaring Types

A grammar can declare the types of the values that its rules produce: structs with named fields and joins of types.
Declared types turn mistakes in actions into compile-time errors and give generated parsers real Go types. This
guide shows how to declare them and how rule result types are inferred. The grammars run like those in [Shaping
Trees](shaping-trees.md), which also explains how to run them. The exact rules are in the [Type System](../../spec/types.md)
chapter.

## Structs and Joins

A **struct type** (`type T = {...}`) is a type with named fields; an action builds its values with struct constructors
such as `T{Field: value}`. A **join** (`type T ~ A | B`, an alias of a join) is the type of a value that is one of
several types, much like a Go interface. A rule without an action produces a node, whose `Value` holds what the rule
matched; a field that holds such a value is declared `node[T]` with the type `T` of that value: `node[string]` for the
text of a literal, a class or `#atomic`, `node[(A, B)]` for a sequence of two valued parts and `node[[]A]` for a
repetition. Every value has a static type; `node[_]`, a node of any value, is a last resort for a value whose type
cannot be written.

```pego
// assign-ast.pego
type Assign = {
    Name  node[string]
    Value node[string]
}

rule main = ^^ assign $$
rule assign Assign = k:name -ws -"=" -ws v:value -";" => Assign{Name: $k, Value: $v}
rule value = number / name
rule name = [a-z]+ #atomic
rule number = [0-9]+ #atomic
rule ws = " "*
```

```bash
pego parse -g assign-ast.pego -f sexpr -i 'ab = 12;'
pego parse -g assign-ast.pego -f sexpr -i 'ab = cd;'
```

```text
(Assign{Name: "ab", Value: "12"})
(Assign{Name: "ab", Value: "cd"})
```

The `Assign` value has no `@rule` label because an action built it, not a rule without an action. Type names and field
names start with an uppercase letter; rule names are in a separate namespace, so `rule name = . . .` is fine. `int`,
`string`, `bool` and `node[T]` are built in, and `error` is the type of the values that [error recovery](recovery.md)
produces. Rule values, list elements, struct fields and action expressions may hold `int`, `string` and `bool` as well as nodes.

An alias is only another name: with `type Binding ~ Assign`, `Binding{...}` produces an `Assign`. Chained aliases and
joins reduced to one member follow the same rule; an alias does not create a distinct type.

## Named Node Types

A leaf of the tree that should have a type of its own and keep the text it matched is a named node type over
`string`, declared as `type N = node[string]`:

```pego
// named-node.pego
type Identifier = node[string]

rule ident Identifier = [a-z]+ #atomic
```

```bash
pego parse -g named-node.pego -s ident -f json -i 'abc'
```

```json
{
  "type": "Identifier",
  "start": 0,
  "end": 3,
  "value": "abc"
}
```

`-f sexpr` prints the node with its type name before the quoted text, `Identifier("abc")`, where a plain `node[string]`
prints just `"abc"`. A named node type over a tuple or a list is written the same way, `Name(...)` or `Name[...]`.

The rule matches the text with `#atomic` and needs no action: a rule whose declared type is exactly `Identifier` converts
its value to that named type, which keeps the range and the text. The type name is recorded on the node, so `Node.Type()`
reports `Identifier`. PEGO builds the node directly, so it is as cheap as a plain match. Use the value in other actions
as `text($ident)`, and in Go as `Text()` on the node (or as the `Value` field of a generated type).

## Optional and List Fields

A field type can be `*T` (a `T` or `nil`) or `[]T` (a list whose elements are `T`). Both are checked at compile time:
a value that may be `nil` cannot go into a plain `T` field.

```pego
// decls.pego
type Decl = {
    Name  node[string]
    Value *node[string] // optional: nil when there is no initializer
}

type Program = { Decls []Decl }

rule main Program = ds:decls $$ => Program{Decls: $ds}
rule decls = ds:(-ws d:decl)* -ws => map($ds, (x) => x.$d)
rule decl Decl = -"let" -ws n:ident v:init? -ws -";" => Decl{Name: $n, Value: $v}
rule init = -ws -"=" -ws l:lit => $l
rule lit = num / ident
rule ident = [a-z]+ #atomic
rule num = [0-9]+ #atomic
rule ws = [ \n]*
```

```bash
pego parse -g decls.pego -f sexpr -i 'let x = 1; let y;'
```

```text
Program{Decls: [Decl{Name: "x", Value: "1"} Decl{Name: "y", Value: nil}]}
```

A field that an action does not set is absent rather than `nil`; in JSON output it is omitted, while an explicit `nil`
prints as `null`.

## Rule Result Types and Inference

A rule can declare its type: `rule name T = . . .`. The checker verifies that the value of the body (or of the action) is
assignable to `T`, and every use of the rule then sees `T`. If you leave the type out it is **inferred** from the body
or the action, iterating until recursive rules stop changing:

| Rule | Inferred type |
|:--|:--|
| `rule num = [0-9]+ #atomic` | `node[string]`: a node whose value is the matched string |
| `rule key = [a-z]+` | `node[[]node[string]]`: a node whose value is a list with one string node per letter |
| `rule pair = k:num "=" v:num` | `node[(num, node[string], num)]`: a node whose value is a tuple, flat, with one element per valued part; `num` stands for the type of the rule `num` |
| `rule v = a / b` | `A \| B`, the join of the types of `a` and `b` |
| `rule o = a?` | `*A`, where `A` is the type of `a` |
| `rule w = a` | The type of `a`: a rule whose body is a single rule call has that rule's type |
| `rule u = -a "x"` | `node[(node[string],)]`, a tuple node with one element: discards, lookaheads and anchors have no value |

Messages show a rule that has no declared type by its name, as `num` above, and add the captures of a node after its
value, as in `node[(num, node[string], num), {"k": 0, "v": 2}]`.

Any of these types can be written in a declaration. A field can hold a tuple node as `node[(node[string], Expr)]`, a
tuple value as `(node[string], Expr)` and the node of a repetition as `node[[]Item]`, and an alias can name one:

```pego
// tuple-types.pego
type Entry ~ node[(node[string], node[string], node[string])]
type Table = { Entries []Entry }

rule main Table = es:(-ws e:entry)* -ws $$ => Table{Entries: map($es, (x) => x.$e)}
rule entry Entry = key "=" val
rule key = [a-z]+ #atomic
rule val = [0-9]+ #atomic
rule ws = [ \n]*
```

```bash
pego parse -g tuple-types.pego -f sexpr -i 'a=1 b=2'
```

```text
Table{Entries: [("a" "=" "1") ("b" "=" "2")]}
```

Inference follows rule dependencies, so a long helper chain keeps its result type regardless of definition order.
Recursive types that keep expanding have conservative growth budgets and may lose their precise types; stable helpers retain
their types. See the [inference limits](../../spec/type-checking.md#type-inference) for the exact fallback rules.

Declare types on the rules whose values flow into struct fields, and on recursive rules; leave helpers inferred. The
type shows up in the error messages, for example when a field expects an `int` and the rule returns a node:

```pego
// field-type.pego
type Pair = { Key node[string]; Value int }
rule pair Pair = k:key "=" v:value => Pair{Key: $k, Value: $v}
rule key = [a-z]+ #atomic
rule value = [0-9]+ #atomic
```

```bash
pego parse -g field-type.pego -s pair -i 'a=1'
```

```text
pego: field-type.pego:3:53: cannot use node[string] as int in field Value of Pair
```

The fix is to declare the field `node[string]`, or to convert the text in the action, as in `Value: text($v)` for a `string` field.

When the checker cannot know a field value's type, for example a rule declared `_`, it rejects the value with a
hint: `cannot use _ as Number in field Value of Pair: the type of the value is not known; convert it with a
cast, as in value.(Number)`. A rule's own value is treated the same way: when the checker cannot know that a rule
produces its declared type, it reports `rule term produces _, which is not assignable to Expr` with the same hint. Write the
cast, `$v.(Number)`: it is the only check that runs, and a value of another type makes the action fail with
`cannot cast ... to Number at input position 4 (line 1, column 5)`, followed by the grammar position of the cast
when the parser records source information. Giving the rules that feed constructors precise types avoids the casts.

Keep whitespace and other trivia out of token rules: write `rule num = [0-9]+ #atomic` and put `-ws` in the rules that
use it.

## Troubleshooting

| Message | Cause and fix |
|:--|:--|
| `cannot use X as Y in field F of T` | The value's type does not fit the field. Inspect the rule's type (declared or inferred) and change the rule, the field or use a join |
| `cannot use *X as X in field F of T` | The value may be `nil` (an optional or a capture in one alternative). Declare the field `*X`, or make the grammar guarantee a match |
| `cannot use *[]X as []X in field F of T` | An optional list; pass it through `concat(...)` to get an empty list instead of `nil` |
| `rule r produces X, which is not assignable to T` | The declared type does not accept the body's value; a sequence without an action is a tuple node, so a struct type needs an action |

## Related Topics

- [Shaping Trees](shaping-trees.md): the default tree and captures.
- [Actions](actions.md): building typed nodes.
- [Generating Parsers](code-generation.md): the Go types generated from declared types.
