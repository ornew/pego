# Reading Trees

A successful parse returns a tree of `*pego.Node`. This guide shows how to read that tree from Go: values, positions,
printing, JSON, reading a whole tree and typed values. [Walking Trees](walking-trees.md) visits and selects nodes
without writing the recursion, and [Output Schemas and Structural Queries](output-schemas-and-queries.md) describes and
searches the output of a grammar. How a grammar shapes the tree is the subject of [Shaping Trees](shaping-trees.md);
the exact node model is defined in [Nodes and Values](../../spec/nodes.md).

The examples use the grammar `pairs.pego`:

```pego
type Pair = { Key node[string]; Value node[string] }

rule main = ws ps:pair+ $$ => $ps
rule pair Pair = k:key "=" v:value ";"? ws => Pair{Key: $k, Value: $v}
rule key = [a-zあ-ん]+ #atomic
rule value = [0-9]+ #atomic
rule ws = [ \t\n]*
```

## Reading Values and Positions

A node is an opaque value that stands for what a rule produced, and `node.Kind()` tells which accessors apply:

| Kind | The node is | Read it with |
|:--|:--|:--|
| `pego.KindString` | A string that the input matched, with a range | `Text()` |
| `pego.KindTuple`, `pego.KindList` | A tuple or a list, with a range | `Len()`, `Elem(i)`, `Elems()` |
| `pego.KindError` | An error that recovery produced | `Text()` (its message) |
| `pego.KindStruct` | A struct that an action built | `Type()`, `Field(name)`, `Fields()` |
| `pego.KindTupleValue`, `pego.KindListValue` | A tuple or a list that an action built, without a range | `Len()`, `Elem(i)`, `Elems()` |
| `pego.KindValue` | Another value that an action built, such as an `int` or a `bool` | `Value()` |

`Elem(i)` returns `nil` for an optional element that did not match. `node.Field(name)` returns the value of a field of a
struct, or `nil` if the field is absent or the node is `nil`, and ranging over `node.Fields()` yields each field name
and value in the order the action wrote them. A field value is a `*pego.Node`, an `int`, a `string`, a `bool` or `nil`.
`Field` and `Fields` also return the captures of a node, but only when the parser is compiled with source information
(see [Printing and JSON](#printing-and-json)). `node.IsTerminal()` reports whether a node is a leaf: a string that the input matched, or an error.

The root of the grammar above is the list that `$ps` captured, and its elements are `Pair` structs:

```go
for _, pair := range node.Elems() {
	key := pair.Field("Key").(*pego.Node)
	fmt.Printf("%s %q at %d-%d\n", pair.Type(), key.Text(), key.Start(), key.End())
}

first := node.Elem(0)
fmt.Println(first.Field("Value")) // "12"
for name, v := range first.Fields() {
	fmt.Println(name, v)
}
fmt.Println(first.Field("Nope") == nil) // true
```

```text
Pair "abc" at 0-3
Pair "あい" at 8-10
"12"
Key "abc"
Value "12"
true
```

Because a field is an `any`, assert its type: `.(*pego.Node)` for nodes and captures, `.(int)` for integers.

`Start()` and `End()` return `int`. They delimit the input a node covers as a half-open range in the position unit of
the parse. A rule that includes trailing white space covers it: the first `Pair` above ends at 8, after `"; "`. To get
the matched text of any node, slice the input (a plain Go slice when you parse with `pego.WithUnit(pego.Bytes)`) or use
`text(...)` in an action. [Running Parsers](position-units.md) explains how to choose a unit.

## Printing and JSON

`node.String()`, which `fmt.Println` uses, renders the value of a node as an S-expression for tests and debugging, and
`pego parse -f sexpr` prints it. A tuple is written in parentheses, a list in square brackets, a string in quotes, an
absent optional as `nil` and a struct as `Type{Field: value}`; positions are not written. A node also encodes with
`encoding/json`:

```go
out, err := json.Marshal(first)
if err != nil {
	return err
}
fmt.Println(string(out))
```

```text
{"type":"Pair","fields":{"Key":{"start":0,"end":3,"value":"abc"},"Value":{"start":4,"end":6,"value":"12"}}}
```

A node of a string, a tuple or a list is `{"start": S, "end": E, "value": V}`, where `V` is a string, an array for a
tuple or a list, or `null` for an optional that did not match. A struct is `{"type": T, "fields": {...}}`, and an `int`
or a `bool` that an action returned is a bare JSON number or boolean. The keys of `fields` are sorted, so the output
is deterministic. This is also what `pego parse` prints by default. Nodes cannot be decoded back from JSON; the JSON is
for inspection and for exchanging results with other programs.

Source information is opt-in. When the parser is compiled with `pego.WithSourceInfo()` (the `-source-info` flag of
`pego parse`), each node also records the rule that produced it and the captures of its tuple, and `Rule()`, `Field`
and `Fields` report them. The JSON gains the members `"rule"` and `"captures"`, and the S-expression gains `@rule`
suffixes and `name:` labels. The grammar `kv.pego` is `rule main = k:key ":" v:num` with `key = [a-z]+ #atomic` and
`num = [0-9]+ #atomic`; for the input `a:1` the S-expression is `("a" ":" "1")` without source information and with it:

```text
(k:"a"@key ":" v:"1"@num)@main
```

The forms are defined in [Text Rendering and JSON Encoding](../../spec/nodes.md#text-rendering).

## Reading a Whole Tree

The program below parses a left-associative sum and prints every node with its range, reading struct fields by name:

```go
// main.go
package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/ornew/pego"
)

const src = `type Bin = {
    Left  Expr
    Op    node
    Right Expr
}
type Expr ~ node | Bin

rule sum Expr = (
    first:num rest:(-ws op:op -ws n:num)*
) => foldl($first, $rest, (acc, i) => Bin{Left: acc, Op: i.$op, Right: i.$n})

rule num = [0-9]+ #atomic
rule op = "+" / "-"
rule ws = " "* `

func main() {
	p, err := pego.CompileSource(src, "sum")
	if err != nil {
		log.Fatal(err)
	}
	n, err := p.Parse("1 - 2 + 3")
	if err != nil {
		log.Fatal(err)
	}
	dump(n, 0)

	// Struct fields are read by name; a string node carries its text.
	right := n.Field("Right").(*pego.Node)
	fmt.Println("right operand:", right.Text(), right.Start(), right.End())
}

func dump(n *pego.Node, depth int) {
	indent := strings.Repeat("  ", depth)
	switch n.Kind() {
	case pego.KindStruct:
		fmt.Printf("%s%s [%d,%d)\n", indent, n.Type(), n.Start(), n.End())
		for name, v := range n.Fields() {
			if c, ok := v.(*pego.Node); ok && c != nil {
				fmt.Printf("%s  .%s\n", indent, name)
				dump(c, depth+2)
			}
		}
	case pego.KindString:
		fmt.Printf("%s%q [%d,%d)\n", indent, n.Text(), n.Start(), n.End())
	}
}
```

```text
Bin [0,9)
  .Left
    Bin [0,5)
      .Left
        "1" [0,1)
      .Op
        "-" [2,3)
      .Right
        "2" [4,5)
  .Op
    "+" [6,7)
  .Right
    "3" [8,9)
right operand: 3 8 9
```

The inner `Bin` covers `1 - 2` and the outer one the whole input, even though the inner node was created inside the
lambda. A tuple or list node would be visited with `n.Elems()`; an element is `nil` where an optional did not match.

## Typed Values

`Node` is a generic representation. A generated Go parser built with `-types` also returns the result as values of Go
types that mirror the types of the grammar, so a type switch replaces field lookups and type assertions. See
[Typed Values](generated-typed-values.md) and the mapping defined in
[Generated Parsers](../../spec/generated-parsers.md#typed-values).

## Related Topics

- [Walking Trees](walking-trees.md): visiting nodes and selecting them by position.
- [Shaping Trees](shaping-trees.md): shaping the tree in the grammar.
- [Running Parsers](running-parsers.md): parse entry points, errors and position units.
