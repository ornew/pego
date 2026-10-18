# Shaping Trees

A PEGO grammar does two jobs: it decides which input is valid, and it decides what **tree** you get back. This guide is
about the tree that every grammar produces without any extra work and how to shape it with captures, atomic (`#atomic`)
and discard (`-`). [Declaring Types](types.md) describes the typed nodes of a grammar, and [Actions](actions.md) shows how
to replace the tree with a typed AST built by [actions](../../spec/actions.md).

The guide complements the normative chapters [Rule Expressions](../../spec/parser-expressions.md),
[Type System](../../spec/types.md) and [Actions](../../spec/actions.md); read them for the exact rules. Here the focus
is on how and why to use each feature. For operator expressions (`foldl` chains
versus Operators Rule Definition Statements), see the companion guide [Operators Rule](operators-rule.md).

Every grammar and output below was produced by running it. To follow along, put each grammar in the file named on its
first line and run the command shown, using either the installed tool or `go run ./cmd/pego` from a checkout:

```bash
go install github.com/ornew/pego/cmd/pego@latest
```

`pego parse -g <grammar> -s <rule> -i '<input>' -f sexpr` parses the input with the start rule `<rule>` (default
`main`), which must match the whole input, and prints the tree.

## Reading a Tree

Start with the smallest grammar that has some structure:

```pego
// assign.pego
rule main = ^^ assign $$
rule assign = name ws "=" ws value ";"
rule name = [a-z]+
rule value = number / name
rule number = [0-9]+
rule ws = " "*
```

```bash
pego parse -g assign.pego -f sexpr -i 'ab = 12;'
```

```text
(Seq (Seq ["a" "b"]@name [" "]@ws "=" [" "]@ws ["1" "2"]@number ";")@assign)@main
```

The `sexpr` format is a compact notation for nodes:

| Notation | Meaning |
|:--|:--|
| `"ab"` | A `Match` node: a terminal holding the matched text |
| `Number"12"` | A terminal of the user-defined terminal type `Number` |
| `(Seq a b)` | A node with children: a `Seq` here, or any struct type, as in `(Pair Key=... Value=...)` |
| `[a b]` | A `List` node (the value of a repetition) |
| `x@name` | The node `x` was produced by the rule `name` (see below) |
| `k=...` | A field (a capture or a struct field), printed after the children and sorted by name |
| `nil` | An absent value, such as an optional element that did not match |
| `` `text` ``, `5`, `true` | A string, integer or boolean struct field (see [Actions](actions.md)) |

Reading the output above from the inside out: `name` is the repetition `[a-z]+`, so it is a `List` of one `Match` per
letter (shown as `["a" "b"]`), labeled `@name` because the rule `name` created it. `ws` is also a list (of spaces), and
`value` is `number`, so the node is the list of digits labeled `@number`. `assign` is a sequence, so a `Seq` labeled
`@assign`; and `main` wraps that in another `Seq`, because `^^` and `$$` have no value and the sequence therefore has a
single child.

The default JSON format carries the same information and adds positions. Here is a rule with an optional element:

```pego
// optional.pego
rule main = name ("," name)?
rule name = [a-z]+ #atomic
```

```bash
pego parse -g optional.pego -i 'a'
```

```text
{
  "type": "Seq",
  "rule": "main",
  "start": 0,
  "end": 1,
  "children": [
    {
      "type": "Match",
      "rule": "name",
      "start": 0,
      "end": 1,
      "text": "a"
    },
    null
  ]
}
```

| JSON key | Meaning |
|:--|:--|
| `type` | `Match`, `Seq`, `List`, `Operator`, `Error`, `Missing`, or the name of a type you declared |
| `rule` | The rule that produced the node, when it is a node of the default tree (omitted otherwise) |
| `start`, `end` | The range of the node in the input; `end` is exclusive (see [Positions](actions.md#positions)) |
| `text` | The text of a terminal |
| `children` | The children of `Seq`, `List` and `Operator` nodes; an optional element that did not match is `null` |
| `fields` | Struct fields and captures, with keys sorted; a value is a node, a number, a string, a boolean or `null` |

## The Default Tree

If a rule has no action, the tree is determined by the shape of the Rule Expression alone. This is the **concrete
syntax tree** (CST). The rules are short enough to memorize:

| Expression | Value |
|:--|:--|
| `"x"`, `[a-z]`, `.`, `a #atomic`, `_` | A `Match` terminal |
| `a b` | A `Seq`; its children are the values of the elements that **have** a value |
| `a / b` | The value of the alternative that matched; the choice adds no node |
| `a*`, `a+`, `a #repeat(n,m)` | A `List`, one child per iteration |
| `a?` | The value of `a`, or `nil`; it is not a list (`a #repeat(0,1)` is) |
| `label:a` | The value of `a` (and a field, see below) |
| Rule call | The value of the called rule |
| `-a`, `&a`, `!a`, `#cut`, anchors, predicates | No value: never a child of a `Seq` |

Three consequences are worth knowing before you write any action.

**One value per rule, and rule names.** A rule produces exactly one value. A node that a rule creates *in its own body*
is labeled with the rule's name; a node that comes from another rule keeps that rule's label.

```pego
// cst.pego
rule seq = "y" "z"
rule alt = "x" / "y" "z"
rule one = "x"
rule same = one
rule rep = "x"*
rule opt = "x"?
rule cap = k:"x" "y"
```

```bash
pego parse -g cst.pego -s seq -f sexpr -i 'yz'
pego parse -g cst.pego -s alt -f sexpr -i 'x'
pego parse -g cst.pego -s alt -f sexpr -i 'yz'
pego parse -g cst.pego -s same -f sexpr -i 'x'
pego parse -g cst.pego -s rep -f sexpr -i 'xxx'
pego parse -g cst.pego -s opt -f sexpr -i 'x'
pego parse -g cst.pego -s cap -f sexpr -i 'xy'
```

```text
(Seq "y" "z")@seq
"x"@alt
(Seq "y" "z")@alt
"x"@one
["x" "x" "x"]@rep
"x"@opt
(Seq "x" "y" k="x")@cap
```

`same` yields the node labeled `@one`, not `@same`. The choice `alt` yields a `Match` or a `Seq` depending on the
alternative, which is why it is called *transparent* (the reasoning is in
[design record 002](../design/002-transparent-choice.md)). In `cap`, the capture `k` adds a field to the sequence but
the matched `"x"` is still the first child; see [Captures](#captures).

**Only valued elements are children.** In `"(" expr ")"` the two parentheses are children of the `Seq`, next to the
expression. To keep them out, discard them with `-`; do the same for whitespace and separators.

**The start rule must consume the whole input.** Remaining input is a syntax error ("expected end of input"), so the
start rule needs no `$$` for that. What it does need is to consume trailing whitespace and comments explicitly.

## Shaping the Tree

### Atomic and Discard

`a #atomic` matches `a` and produces *one* terminal holding the matched text, instead of the tree of `a`. Use it for tokens:
a number is one node, not a list of digits. `-a` matches `a` and produces nothing. Use it for input the syntax needs but
the tree does not:

```pego
// shape.pego
rule main = ^^ assign $$
rule assign = name -ws -"=" -ws value -";"
rule name = [a-z]+ #atomic
rule value = number / name
rule number = [0-9]+ #atomic
rule ws = " "*
```

```bash
pego parse -g shape.pego -f sexpr -i 'ab = 12;'
```

```text
(Seq (Seq "ab"@name "12"@number)@assign)@main
```

Compare this with the tree in Reading a Tree: the same input now yields a `Seq` of just two terminals (inside the `Seq` of
`main`). Both operators suppress construction, so they are also cheaper (see
[Performance Guidelines](#performance-guidelines)). Because captures are meaningless inside them, `(k:"x" "y") #atomic` and
`-(k:"x" "y")` are compile errors.

### Captures

A capture `label:a` gives the value of `a` a name that actions and predicates can use as `$label`. Without an action,
captures also show up in the tree: they become **fields** of the node. They do not remove anything from the children.

```pego
// capture.pego
rule pair = k:name -ws -"=" -ws v:value -";"
rule name = [a-z]+ #atomic
rule value = number / name
rule number = [0-9]+ #atomic
rule ws = " "*
rule list = name ("," n:name)*
rule list2 = name (-"," n:name)*
```

```bash
pego parse -g capture.pego -s pair -f sexpr -i 'ab = 12;'
pego parse -g capture.pego -s list -f sexpr -i 'a,b,c'
pego parse -g capture.pego -s list2 -f sexpr -i 'a,b,c'
```

```text
(Seq "ab"@name "12"@number k="ab"@name v="12"@number)@pair
(Seq "a"@name [(Seq "," "b"@name n="b"@name) (Seq "," "c"@name n="c"@name)])@list
(Seq "a"@name [(Seq "b"@name n="b"@name) (Seq "c"@name n="c"@name)])@list2
```

Note how `k` and `v` appear both as children and as fields (the same node is printed twice). A capture inside a
repetition belongs to the **scope of one iteration**: each element of the list is a `Seq` that carries the field `n`.
If the iteration's value is not a `Seq` the engine wraps it in one, so that the field has somewhere to live. This is
what makes `$rest` in `foldl($first, $rest, (acc, i) => ... i.n ...)` work (see the [recipes](actions.md#recipes)).

Two details matter in practice:

- A capture that did not match (inside `?` or in only some alternatives of a choice) is `nil`, and its type is
  optional (`*T`).
- A captured value keeps its **whole subtree**. If you capture a repetition such as `rest:(ws "," ws x:ident)*`, every
  whitespace node inside it stays in the tree, because you may inspect it with `.children` or `text` later. Discard the
  parts you do not need *inside* the capture, punctuation included: `rest:(-ws -"," -ws x:ident)*`.

### Unwrapping the Root

A start rule such as `main = ^^ expr $$` wraps the real result in a `Seq`. When you build an AST, return the value you
want instead:

```pego
rule main Expr = e:expr $$ => $e
```

This is the idiom of the calculator example.

## Using the Tree from Go

`Parser.Parse` returns a `*pego.Node` that mirrors the JSON keys above: the methods `Type()` and `Rule()`, the fields
`Start` and `End`, `Text`, `Children` and `Fields`, and `Field(name)` for a struct field or capture. Reading fields
is covered in [Reading Trees](reading-trees.md), walking the tree and selecting nodes by position in [Walking
Trees](walking-trees.md), and the
node model is defined in [Nodes and Values](../../spec/nodes.md).

## Performance Guidelines

PEGO builds values only where something can observe them. To reduce allocations, help the engine identify what your grammar does not need:

1. **Discard trivia inside captured expressions** with `-x`: `rest:(-ws -"," -ws v:value)*`. The engine cannot drop it
   automatically, because a captured value exposes its full CST.
2. **Use terminal types or `(...) #atomic` for tokens.** Their bodies are matched without building values: an identifier is
   one node instead of one node per character.

How much do they matter? Parsing one 140 KB list of 20,000 identifiers separated by `" ,  "` with the default backend
allocated, measured around a single `Parse` call with `runtime.MemStats`:

| Grammar | Allocated per parse |
|:--|--:|
| Whitespace kept in the captured repetition (`rest:(ws "," ws x:ident)*`), `ident` a terminal type | 22.5 MB |
| Whitespace discarded (`rest:(-ws "," -ws x:ident)*`) | 11.9 MB |
| Whitespace and the separator discarded (`rest:(-ws -"," -ws x:ident)*`) | 8.0 MB |
| Whitespace discarded, but `ident` without a type: `rule ident =  [a-z]+ ` (a list of `Match` nodes) | 16.4 MB |
| Same, with `rule ident =  [a-z]+ #atomic ` | 11.9 MB |

These choices matter most when a grammar captures many repeated values. Measure your own input and inspect the resulting tree to ensure that discarding text does not remove information your application needs.

If you only need to know whether the input is valid, use `pego parse -check` (`pego.RecognizeOnly()`): it builds no
values and runs no actions.

## Troubleshooting

| Message | Cause and fix |
|:--|:--|
| `capture k inside #atomic, -, or ! has no effect` | Captures cannot be made inside atomic, discard or negative lookahead; capture the whole expression or restructure |

## Related Topics

- [Declaring Types](types.md): struct, union and terminal types.
- [Actions](actions.md): building the tree yourself.
- [Rule Expressions](rule-expressions.md): the expressions that the tree is made of.
- [Reading Trees](reading-trees.md): reading the result in Go.
