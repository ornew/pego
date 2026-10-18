# Declaring Types

A grammar can declare the types of its nodes: structs with named fields, unions of types and terminals that carry only
text. Declared types turn mistakes in actions into compile-time errors and give generated parsers real Go types. This
guide shows how to declare them and how rule result types are inferred. The grammars run like those in [Shaping
Trees](shaping-trees.md), which also explains how to run them. The exact rules are in the [Type System](../../spec/types.md)
chapter.

## Structs, Unions and Terminal Types

A **struct type** (`type T = {...}`) is a node type with named fields; an action builds its nodes with struct constructors such as `T{Field: value}`. A **union**
(`type T ~ A | B`, an alias of a union) is the type of a value that is one of several types, much like a Go interface. A **terminal type** (`type T terminal`)
is a node type for tokens: a rule declared with it produces a terminal of that type holding the matched text,
whatever its body looks like.

```pego
// assign-ast.pego
type Name terminal
type Number terminal
type Value ~ Name | Number
type Assign = {
    Name  Name
    Value Value
}

rule main = ^^ assign $$
rule assign Assign = k:name -ws -"=" -ws v:value -";" => Assign{Name: $k, Value: $v}
rule value Value = number / name
rule name Name = [a-z]+
rule number Number = [0-9]+
rule ws = " "*
```

```bash
pego parse -g assign-ast.pego -f sexpr -i 'ab = 12;'
pego parse -g assign-ast.pego -f sexpr -i 'ab = cd;'
```

```text
(Seq (Assign Name=Name"ab"@name Value=Number"12"@number))@main
(Seq (Assign Name=Name"ab"@name Value=Name"cd"@name))@main
```

Terminal-typed rules need no `@`: `rule name Name =  [a-z]+ ` is a `Name"ab"` terminal, not a list. The `Assign` node has
no `@rule` label because it was built by an action, not by the default tree. Type names and field names start with an
uppercase letter; rule names are in a separate namespace, so `rule name Name = . . .` is fine. `Match`, `Seq`, `List`,
`Operator`, `Error` and `Missing` are predefined node types, and `int`, `string`, `bool`, `node` and `terminal` are built in (a
rule's value must be a node, but struct fields and action expressions may also hold `int`, `string` and `bool`).

Aliases preserve the underlying type: with `type Token ~ Name`, a rule declared
as `Token` produces a `Name` terminal. With `type Binding ~ Assign`,
`Binding{...}` produces an `Assign` node. Chained aliases and unions reduced
to one member follow the same rule; an alias does not create a distinct node
type.

## Optional and List Fields

A field type can be `*T` (a `T` or `nil`) or `[]T` (a `List` whose elements are `T`). Both are checked at compile time:
a value that may be `nil` cannot go into a plain `T` field.

```pego
// decls.pego
type Ident terminal
type Num terminal
type Lit ~ Ident | Num

type Decl = {
    Name  Ident
    Value *Lit // optional: nil when there is no initializer
}

type Program = { Decls []Decl }

rule main Program = ds:decls $$ => Program{Decls: $ds}
rule decls = ds:(-ws d:decl)* -ws => map($ds, (x) => x.$d)
rule decl Decl = -"let" -ws n:ident v:init? -ws -";" => Decl{Name: $n, Value: $v}
rule init Lit = -ws -"=" -ws l:lit => $l
rule lit Lit = num / ident
rule ident Ident = [a-z]+
rule num Num = [0-9]+
rule ws = [ \n]*
```

```bash
pego parse -g decls.pego -f sexpr -i 'let x = 1; let y;'
```

```text
(Program Decls=[(Decl Name=Ident"x"@ident Value=Num"1"@num) (Decl Name=Ident"y"@ident Value=nil)])
```

A field that an action does not set is absent rather than `nil`; in JSON output it is omitted, while an explicit `nil`
prints as `null`.

## Rule Result Types and Inference

A rule can declare its type: `rule name T = . . .`. The checker verifies that the value of the body (or of the action) is
assignable to `T`, and every use of the rule then sees `T`. If you leave the type out it is **inferred** from the body
or the action, iterating until recursive rules stop changing:

| Rule | Inferred type |
|:--|:--|
| `rule num =  [0-9]+ #atomic ` | `Match` |
| `rule key =  [a-z]+ ` | `[]Match` |
| `rule pair = k:num "=" v:num` | `Seq` with the fields `k: Match` and `v: Match` |
| `rule v = a / b` | The union of the types of `a` and `b` |
| `rule o = a?` | `*T`, where `T` is the type of `a` |

Inference follows rule dependencies, so a long helper chain keeps its result type regardless of definition order.
Recursive types that keep expanding have conservative growth budgets and may become `any`; stable helpers retain
their types. See the [inference limits](../../spec/type-checking.md#type-inference) for the exact fallback rules.

Declare types on the rules whose values flow into struct fields, and on recursive rules; leave helpers inferred. The
type shows up in the error messages, for example when a field expects a `Match` and the rule returns a list:

```pego
// field-type.pego
type Pair = { Key Match; Value Match }
rule pair Pair = k:key "=" v:value => Pair{Key: $k, Value: $v}
rule key = [a-z]+
rule value = [0-9]+ #atomic
```

```bash
pego parse -g field-type.pego -s pair -i 'a=1'
```

```text
pego: field-type.pego:3:48: cannot use []Match as Match in field Key of Pair
```

The fix is to make `key` a single token: `rule key =  [a-z]+ #atomic `, or give it a terminal type.

When the checker cannot know a field value's type, for example a rule declared `any` or a variable, the constructor
checks the value when it runs instead: a mismatch makes the action fail with the same message, prefixed by
`action in <rule>:`, and makes a predicate fail. A rule's own value is treated the same way: when the checker cannot
know that a rule produces its declared type, the rule checks its value when it produces it, and a mismatch such as
`cannot use List as Expr in result of rule term` fails the parse. Values whose types the checker knows are never
checked again, so giving the rules that feed constructors precise types avoids both kinds of check.

A terminal type is a property of a rule *without* an action: the rule's value is the matched text. If the rule has an
action, the action's value is the rule's value, and it must already be of that type:

```pego
// term-action.pego
type Num terminal
rule num Num = " "* d:[0-9]+ => $d
```

```bash
pego parse -g term-action.pego -s num -i ' 12'
```

```text
pego: term-action.pego:3:1: rule num produces []Match, which is not assignable to Num
```

Keep whitespace and other trivia out of token rules: write `rule num Num =  [0-9]+ ` and put `-ws` in the rules that
use it.

## Troubleshooting

| Message | Cause and fix |
|:--|:--|
| `cannot use X as Y in field F of T` | The value's type does not fit the field. Inspect the rule's type (declared or inferred) and change the rule, the field or use a union |
| `cannot use *X as X in field F of T` | The value may be `nil` (an optional or a capture in one alternative). Declare the field `*X`, or make the grammar guarantee a match |
| `cannot use *[]X as []X in field F of T` | An optional list; pass it through `concat(...)` to get an empty list instead of `nil` |
| `rule r produces X, which is not assignable to T` | The declared type does not accept the body's value; the CST of a sequence is a `Seq`, so a struct type needs an action |

## Related Topics

- [Shaping Trees](shaping-trees.md): the default tree and captures.
- [Actions](actions.md): building typed nodes.
- [Generated Go Parsers](code-generation.md): the Go types generated from declared types.
