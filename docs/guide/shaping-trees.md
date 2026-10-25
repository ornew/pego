# Shaping Trees

A PEGO grammar does two jobs: it decides which input is valid, and it decides what **tree** you get back. This guide is
about the tree that every grammar produces without any extra work and how to shape it with captures, atomic (`#atomic`)
and discard (`-`). [Declaring Types](types.md) describes the typed nodes of a grammar, and [Actions](actions.md) shows how
to replace the tree with a typed AST built by [actions](../../spec/language/actions.md).

The guide complements the normative chapters [Rule Expressions](../../spec/language/parser-expressions.md),
[Type System](../../spec/language/types.md) and [Actions](../../spec/language/actions.md); read them for the exact rules. Here the focus
is on how and why to use each feature. For operator expressions (`foldl` chains
versus Operators Rule Declaration Statements), see the companion guide [Operators Rule](operators-rule.md).

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
@start
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
((["a" "b"] [" "] "=" [" "] ["1" "2"] ";"))
```

The `sexpr` format is a compact notation for values:

| Notation | Meaning |
|:--|:--|
| `"ab"` | A string node: the text that a literal, a class, `.` or `#atomic` matched |
| `(a b)` | A tuple: the values of the elements of a sequence |
| `[a b]` | A list: the value of a repetition |
| `()` | The unit: the value of a sequence that has no valued element |
| `Type{Field: v}` | A struct that an action built (see [Actions](actions.md)) |
| `error("message")` | The value that [error recovery](recovery.md) leaves where it skipped input |
| `nil` | An absent value, such as an optional element that did not match |

Reading the output above from the inside out: `name` is the repetition `[a-z]+`, so it is a list of one string node per
letter (shown as `["a" "b"]`). `ws` is also a list (of spaces), and `value` is `number`, so the node is the list of
digits. `assign` is a sequence, so a tuple of its six valued elements; and `main` wraps that in another tuple, because
`^^` and `$$` have no value and the sequence therefore has a single element.

Source information adds the rule that produced each node and the names of the captures. It is off by default, because
most programs do not need it; ask for it with `-source-info` (`pego.WithSourceInfo()` in Go):

```bash
pego parse -g assign.pego -f sexpr -source-info -i 'ab = 12;'
```

```text
((["a" "b"]@name [" "]@ws "=" [" "]@ws ["1" "2"]@number ";")@assign)@main
```

`x@name` means that the node `x` is the value of the rule `name`. Only the value of a rule is labeled: the elements of
a tuple that come from literals, like the `"="` above, are not.

The default JSON format carries the same information and adds positions. Here is a rule with an optional element:

```pego
// optional.pego
@start
rule main = name ("," name)?
rule name = [a-z]+ #atomic
```

```bash
pego parse -g optional.pego -i 'a'
```

```text
{
  "start": 0,
  "end": 1,
  "value": [
    {
      "start": 0,
      "end": 1,
      "value": "a"
    },
    null
  ]
}
```

| JSON key | Meaning |
|:--|:--|
| `start`, `end` | The range of the node in the input; `end` is exclusive (see [Positions](actions.md#positions)) |
| `value` | The text of a string node, or an array of the elements of a tuple or a list; an optional element that did not match is `null` |
| `error` | In place of `value`, the message of an error value |
| `type`, `fields` | A struct that an action built: its type name and its fields. A struct has no `start` or `end` of its own |
| `rule` | With source information: the rule that produced the node |
| `captures` | With source information: the captures of a tuple, each with the `path` of the element it names |

## The Default Tree

If a rule has no action, its value is determined by the shape of the Rule Expression alone. The rules are short enough
to memorize:

| Expression | Value |
|:--|:--|
| `"x"`, `[a-z]`, `.`, `a #atomic` | A string node holding the matched text |
| `a b` | A tuple of the values of the elements that **have** a value; a group `(a b)` inside it does not nest |
| `a / b` | The value of the alternative that matched; the choice adds no node |
| `a*`, `a+`, `a #repeat(n,m)` | A list, one element per iteration |
| `a?` | The value of `a`, or `nil`; it is not a list (`a #repeat(0,1)` is) |
| `label:a` | The value of `a` (and a name for it, see below) |
| Rule call | The value of the called rule |
| `-a`, `&a`, `!a`, `#cut`, `_`, anchors, predicates | No value: never an element of a tuple |

A sequence with no valued element has the unit `()` as its value.

Three consequences are worth knowing before you write any action.

**One value per rule, and rule names.** A rule produces exactly one value. With source information, a node that a rule
creates *in its own body* is labeled with the rule's name; a node that comes from another rule keeps that rule's label.

```pego
// cst.pego
@start(default)
rule seq = "y" "z"
@start
rule alt = "x" / "y" "z"
rule one = "x"
@start
rule same = one
@start
rule rep = "x"*
@start
rule opt = "x"?
@start
rule cap = k:"x" "y"
```

```bash
pego parse -g cst.pego -s seq -f sexpr -source-info -i 'yz'
pego parse -g cst.pego -s alt -f sexpr -source-info -i 'x'
pego parse -g cst.pego -s alt -f sexpr -source-info -i 'yz'
pego parse -g cst.pego -s same -f sexpr -source-info -i 'x'
pego parse -g cst.pego -s rep -f sexpr -source-info -i 'xxx'
pego parse -g cst.pego -s opt -f sexpr -source-info -i 'x'
pego parse -g cst.pego -s cap -f sexpr -source-info -i 'xy'
```

```text
("y" "z")@seq
"x"@alt
("y" "z")@alt
"x"@one
["x" "x" "x"]@rep
"x"@opt
(k:"x" "y")@cap
```

`same` yields the node labeled `@one`, not `@same`. The choice `alt` yields a string node or a tuple depending on the
alternative, which is why it is called *transparent* (the reasoning is in
[design record 002](../design/records/002-transparent-choice.md)). In `cap`, the capture `k` names the first element of the
tuple; see [Captures](#captures).

**Only valued elements are in the tuple.** In `"(" expr ")"` the two parentheses are elements of the tuple, next to the
expression. To keep them out, discard them with `-`; do the same for whitespace and separators.

**The start rule must consume the whole input.** Remaining input is a syntax error ("expected end of input"), so the
start rule needs no `$$` for that. What it does need is to consume trailing whitespace and comments explicitly.

## Shaping the Tree

### Atomic and Discard

`a #atomic` matches `a` and produces *one* string node holding the matched text, instead of the tree of `a`. Use it for
tokens: a number is one node, not a list of digits. `-a` matches `a` and produces nothing. Use it for input the syntax
needs but the tree does not:

```pego
// shape.pego
@start
rule main = ^^ assign $$
rule assign = name -ws -"=" -ws value -";"
rule name = [a-z]+ #atomic
rule value = number / name
rule number = [0-9]+ #atomic
rule ws = " "*
```

```bash
pego parse -g shape.pego -f sexpr -source-info -i 'ab = 12;'
```

```text
(("ab"@name "12"@number)@assign)@main
```

Compare this with the tree in Reading a Tree: the same input now yields a tuple of just two string nodes (inside the
tuple of `main`). Both operators suppress construction, so they are also cheaper (see
[Performance Guidelines](#performance-guidelines)). Because captures are meaningless inside them, `(k:"x" "y") #atomic` and
`-(k:"x" "y")` are compile errors.

### Captures

A capture `label:a` gives the value of `a` a name that actions and predicates can use as `$label`. Without an action,
a capture does not change the value of the rule; it only names an element of it, which source information shows as
`label:` before the element. A capture removes nothing from the tuple.

```pego
// capture.pego
@start(default)
rule pair = k:name -ws -"=" -ws v:value -";"
rule name = [a-z]+ #atomic
rule value = number / name
rule number = [0-9]+ #atomic
rule ws = " "*
@start
rule list = name ("," n:name)*
@start
rule list2 = name (-"," n:name)*
```

```bash
pego parse -g capture.pego -s pair -f sexpr -source-info -i 'ab = 12;'
pego parse -g capture.pego -s list -f sexpr -source-info -i 'a,b,c'
pego parse -g capture.pego -s list2 -f sexpr -source-info -i 'a,b,c'
```

```text
(k:"ab"@name v:"12"@number)@pair
("a"@name [("," n:"b"@name) ("," n:"c"@name)])@list
("a"@name [(n:"b"@name) (n:"c"@name)])@list2
```

Note how `k` and `v` label the two elements of the tuple. A capture inside a repetition belongs to the **scope of one
iteration**: each element of the list is a tuple that carries the capture `n`. If the iteration's value is not a tuple
the engine wraps it in one, so that the capture has somewhere to live. This is what makes `$rest` in
`foldl($first, $rest, (acc, i) => ... i.$n ...)` work (see the [recipes](actions.md#recipes)).

Two details matter in practice:

- A capture that did not match (inside `?` or in only some alternatives of a choice) is `nil`, and its type is
  optional (`*T`).
- A captured value keeps its **whole subtree**. If you capture a repetition such as `rest:(ws "," ws x:ident)*`, every
  whitespace node inside it stays in the value, because you may inspect it with `x[i]` or `text` later. Discard the
  parts you do not need *inside* the capture, punctuation included: `rest:(-ws -"," -ws x:ident)*`.

### Unwrapping the Root

A start rule such as `main = ^^ expr $$` wraps the real result in a tuple. When you build an AST, return the value you
want instead:

```pego
@start
rule main Expr = e:expr $$ => $e
```

This is the idiom of the calculator example.

## Using the Tree From Go

`Parser.Parse` returns a `*pego.Node`, which is opaque: `Kind()` says whether it is a string node, a tuple, a list, an
error value or a struct, and the methods that fit the kind read it: `Start()` and `End()`, `Text()`, `Len()`, `Elem(i)`
and `Elems()`, `Field(name)` and `Fields()` for the fields of a struct, and `Value()`. `Rule()` and the capture names are
there only if the parser was compiled with `pego.WithSourceInfo()`. Reading values is covered in
[Reading Trees](reading-trees.md), walking the tree and selecting nodes by position in [Walking
Trees](walking-trees.md), and the node model is defined in [Nodes and Values](../../spec/runtime/nodes.md).

## Performance Guidelines

PEGO builds values only where something can observe them. To reduce allocations, help the engine identify what your grammar does not need:

1. **Discard trivia inside captured expressions** with `-x`: `rest:(-ws -"," -ws v:value)*`. The engine cannot drop it
   automatically, because a captured value exposes its whole subtree.
2. **Use `#atomic` for tokens.** Their bodies are matched without building values: an identifier is
   one node instead of one node per character.

How much do they matter? Parsing one 140 KB list of 20,000 identifiers separated by `" ,  "` with the default backend
allocated, measured around a single `Parse` call with `runtime.MemStats`:

| Grammar | Allocated per parse |
|:--|--:|
| Whitespace kept in the captured repetition (`rest:(ws "," ws x:ident)*`), `ident` an atomic token (`[a-z]+ #atomic`) | 22.5 MB |
| Whitespace discarded (`rest:(-ws "," -ws x:ident)*`) | 11.9 MB |
| Whitespace and the separator discarded (`rest:(-ws -"," -ws x:ident)*`) | 8.0 MB |
| Whitespace discarded, but `ident` without a type: `rule ident =  [a-z]+ ` (a list of string nodes) | 16.4 MB |
| Same, with `rule ident =  [a-z]+ #atomic ` | 11.9 MB |

These choices matter most when a grammar captures many repeated values. Measure your own input and inspect the resulting tree to ensure that discarding text does not remove information your application needs.

If you only need to know whether the input is valid, use `pego parse -check` (`pego.RecognizeOnly()`): it builds no
values and runs no actions.

## Troubleshooting

| Message | Cause and fix |
|:--|:--|
| `capture k inside #atomic, -, or ! has no effect` | Captures cannot be made inside atomic, discard or negative lookahead; capture the whole expression or restructure |

## Related Topics

- [Declaring Types](types.md): struct and join types.
- [Actions](actions.md): building the tree yourself.
- [Rule Expressions](rule-expressions.md): the expressions that the tree is made of.
- [Reading Trees](reading-trees.md): reading the result in Go.
