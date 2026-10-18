# Reading Trees

A successful parse returns a tree of `*pego.Node`. This guide shows how to read that tree from Go: fields, positions,
printing, JSON, reading a whole tree and typed values. [Walking Trees](walking-trees.md) visits and selects nodes
without writing the recursion, and [Output Schemas and Structural Queries](output-schemas-and-queries.md) describes and
searches the output of a grammar. How a grammar shapes the tree is the subject of [Shaping Trees](shaping-trees.md);
the exact node model is defined in [Nodes and Values](../../spec/nodes.md).

The examples use the grammar `pairs.pego`:

```pego
type Pair = { Key Match; Value Match }

rule main = ws ps:pair+ $$ => $ps
rule pair Pair = k:key "=" v:value ";"? ws => Pair{Key: $k, Value: $v}
rule key = [a-zあ-ん]+ #atomic
rule value = [0-9]+ #atomic
rule ws = [ \t\n]*
```

## Reading Fields and Positions

A field value is a `*pego.Node`, an `int`, a `string`, a `bool` or `nil`. `node.Field(name)` returns the value, or `nil`
if the field is absent or the node is `nil`. `node.Fields.Get(name)` also tells whether the field exists, and ranging
over `node.Fields` yields each `NodeField{Name, Value}` in the order the fields were set.

```go
for _, pair := range node.Children {
	key := pair.Field("Key").(*pego.Node)
	fmt.Printf("%s %q at %d-%d\n", pair.Type(), key.Text, key.Start, key.End)
}

first := node.Children[0]
v, ok := first.Fields.Get("Value")
fmt.Println(v, ok) // "12"@value true
_, ok = first.Fields.Get("Nope")
fmt.Println(first.Field("Nope") == nil, ok) // true false
```

```text
Pair "abc" at 0-3
Pair "あい" at 8-10
```

Because a field is an `any`, assert its type: `.(*pego.Node)` for nodes and captures, `.(int)` for integers.
`node.IsTerminal()` reports whether a node is a terminal; for those, `Text` holds the text.

`Start` and `End` are `int32`, which Go accepts as slice indexes; convert with `int(n.Start)` for arithmetic with
`int` values. They delimit the input a node covers as a half-open range in the position unit of the parse. A rule that
includes trailing white space covers it: the first `Pair` above ends at 8, after `"; "`. To get the matched text of
any node, slice the input (a plain Go slice when you parse with `pego.WithUnit(pego.Bytes)`) or use `text(...)` in an
action. [Running Parsers](position-units.md) explains how to choose a unit.

## Printing and JSON

`node.String()`, which `fmt.Println` uses, renders a compact form for tests and debugging, and `pego parse -f sexpr`
prints it. A node also encodes with `encoding/json`:

```go
out, err := json.Marshal(first)
if err != nil {
	return err
}
fmt.Println(string(out))
```

```text
{"type":"Pair","start":0,"end":8,"fields":{"Key":{"type":"Match","rule":"key","start":0,"end":3,"text":"abc"},"Value":{"type":"Match","rule":"value","start":4,"end":6,"text":"12"}}}
```

The keys of `fields` are sorted, so the output is deterministic, and `rule`, `text`, `children` and `fields` are
omitted when empty. This is also what `pego parse` prints by default. Nodes cannot be decoded back from JSON; the
JSON is for inspection and for exchanging results with other programs. The forms are defined in
[Text Rendering and JSON Encoding](../../spec/nodes.md#text-rendering).
## Reading a Whole Tree

The program below parses a left-associative sum and prints every node with its range, reading fields by name and
following children:

```go
// main.go
package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/ornew/pego"
)

const src = `type Num terminal
type Op terminal
type Bin = {
    Left  Expr
    Op    Op
    Right Expr
}
type Expr ~ Num | Bin

rule sum Expr =  first:num rest:(-ws op:op -ws n:num)*
    => foldl($first, $rest, (acc, i) => Bin{Left: acc, Op: i.op, Right: i.n})

rule num Num =  [0-9]+
rule op Op =  "+" / "-"
rule ws =  " "* `

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

	// Fields are read by name; terminals carry their text.
	right := n.Field("Right").(*pego.Node)
	fmt.Println("right operand:", right.Text, right.Start, right.End)
}

func dump(n *pego.Node, depth int) {
	fmt.Printf("%s%s [%d,%d)", strings.Repeat("  ", depth), n.Type(), n.Start, n.End)
	if n.IsTerminal() {
		fmt.Printf(" %q", n.Text)
	}
	fmt.Println()
	for _, f := range n.Fields {
		if c, ok := f.Value.(*pego.Node); ok && c != nil {
			fmt.Printf("%s.%s\n", strings.Repeat("  ", depth+1), f.Name)
			dump(c, depth+2)
		}
	}
	for _, c := range n.Children { // Seq, List and Operator nodes; a nil child is an unmatched optional
		if c != nil {
			dump(c, depth+1)
		}
	}
}
```

```text
Bin [0,9)
  .Left
    Bin [0,5)
      .Left
        Num [0,1) "1"
      .Op
        Op [2,3) "-"
      .Right
        Num [4,5) "2"
  .Op
    Op [6,7) "+"
  .Right
    Num [8,9) "3"
right operand: 3 8 9
```

The inner `Bin` covers `1 - 2` and the outer one the whole input, even though the inner node was created inside the
lambda.

## Typed Values

`Node` is a generic representation. A generated Go parser built with `-types` also returns the result as values of Go
types that mirror the types of the grammar, so a type switch replaces field lookups and type assertions. See
[Typed Values](code-generation.md#typed-values--types) and the mapping defined in
[Generated Parsers](../../spec/generated-parsers.md#typed-values).

## Related Topics

- [Walking Trees](walking-trees.md): visiting nodes and selecting them by position.
- [Shaping Trees](shaping-trees.md): shaping the tree in the grammar.
- [Running Parsers](running-parsers.md): parse entry points, errors and position units.
