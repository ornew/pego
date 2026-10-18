# Working With Trees

A successful parse returns a tree of `*pego.Node`. This guide shows how to read that tree from Go: fields, positions,
printing, JSON, walking and selecting nodes, typed values, and the experimental output schemas and structural queries.
How a grammar shapes the tree is the subject of [Trees and Actions](trees-and-actions.md); the exact node model is
defined in [Nodes and Values](../../spec/nodes.md).

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
action. [Running Parsers](running-parsers.md#position-units) explains how to choose a unit.

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

## Walking a Tree

`pego.Walk` visits a node graph without a hand-written recursion. `pego.Cursor` applies point or range filters to node
spans, in the same order. This program parses a short sequence, visits its nodes, selects the nodes that overlap an
input range and writes the tree as JSON:

```go
// main.go
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/ornew/pego"
)

func main() {
	parser, err := pego.CompileSource(`rule start = "a" "b"`, "start")
	if err != nil {
		log.Fatal(err)
	}

	root, err := parser.Parse("ab", pego.WithUnit(pego.CodePoints))
	if err != nil {
		log.Fatal(err)
	}

	err = pego.Walk(root, func(v pego.WalkVisit) (pego.WalkAction, error) {
		if v.Node != nil {
			fmt.Printf("%s [%d,%d) depth=%d\n", v.Node.Type(), v.Node.Start, v.Node.End, v.Depth)
		}
		return pego.WalkContinue, nil
	})
	if err != nil {
		log.Fatal(err)
	}

	cursor := pego.Cursor{Root: root, Unit: pego.CodePoints}
	err = cursor.Range(0, 1, func(v pego.WalkVisit) (pego.WalkAction, error) {
		fmt.Printf("overlap: %s [%d,%d)\n", v.Node.Type(), v.Node.Start, v.Node.End)
		return pego.WalkContinue, nil
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := root.WriteJSON(os.Stdout); err != nil {
		log.Fatal(err)
	}
	fmt.Println()
}
```

```text
Seq [0,2) depth=0
Match [0,1) depth=1
Match [1,2) depth=1
overlap: Seq [0,2)
overlap: Match [0,1)
{"type":"Seq","rule":"start","start":0,"end":2,"children":[{"type":"Match","start":0,"end":1,"text":"a"},{"type":"Match","start":1,"end":2,"text":"b"}]}
```

The visit gives you the value reached by the current edge, the node when that value is a node, its parent, its depth
and the incoming edge. Return `pego.WalkSkip` to omit the descendants of a visit, `pego.WalkStop` to end the walk
successfully, or an error to stop with that error. A shared node is visited once for each incoming edge, and a cycle
on the active path returns a `*pego.CycleError`, which `errors.As` can inspect. The order of the visits, the cursor
methods `At` and `Range`, and the rule that the unit of the cursor must be the unit of the parse are defined in
[Traversal](../../spec/nodes.md#traversal). Generated Go parsers expose the same walker as `WalkTree`.

Use `Node.WriteJSON` instead of `json.Marshal` for very deep results: it writes the graph without recursion, whereas
`encoding/json` gives up beyond its own nesting limit.

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

## Output Schemas and Structural Queries

`Parser.OutputSchema` describes the possible values of a checked grammar. A structural query searches the nodes of an
actual parse result. Together they let a tool inspect the shape of a grammar and find nodes without generating code or
serializing a tree. These Go APIs are experimental; the schema JSON format is versioned. The contract is defined in
[Output Schemas and Structural Queries](../../spec/schemas-and-queries.md).

```go
package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/ornew/pego"
)

func main() {
	p, err := pego.CompileSource("rule main = name\nrule name = [a-z]+ #atomic", "main")
	if err != nil {
		log.Fatal(err)
	}

	schema, err := p.OutputSchema()
	if err != nil {
		log.Fatal(err)
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		log.Fatal(err)
	}

	query, err := pego.CompileStructuralQuery(pego.QueryPattern{
		Type: "Match", Rule: "name", Capture: "name",
	}, schema)
	if err != nil {
		log.Fatal(err)
	}
	tree, err := p.Parse("abc")
	if err != nil {
		log.Fatal(err)
	}
	matches, err := query.Matches(tree)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(encoded), len(matches))
}
```

To compile many queries against one schema, validate it once with `pego.PrepareQuerySchema(schema)` and compile with
the result's `CompileStructuralQuery(pattern)`. With a 128-type schema, compiling a query takes about 80 µs without
preparation and under 1 µs with it; preparing takes about as long as one unprepared compilation.

```go
prepared, err := pego.PrepareQuerySchema(schema)
if err != nil {
	return err
}
query, err := prepared.CompileStructuralQuery(pego.QueryPattern{
	Type: "Match", Rule: "name", Capture: "name",
})
```

`pego.DiffOutputSchemas(before, after)` compares two schemas and returns the added, removed and changed entries, for
example to review how a grammar change alters its output:

```go
diff, err := pego.DiffOutputSchemas(before, after)
if err != nil {
	return err
}
for _, change := range diff.Types {
	fmt.Println(change.ID, change.Kind)
}
```

Keep in mind:

- A parser restored by `LoadParser` cannot export a schema, even if its file holds the grammar AST. Store the schema
  separately if a consumer needs it later.
- A query sees only what the tree retains. Actions and discard operators remove punctuation and comments, and a query
  does not recover them or interpret source positions.
- A pattern's `list` category means an explicit `[]any` field value. A repetition is a `Node` of type `List`; match it
  with `Type: "List"`.
- Generated parsers and TypeScript do not expose this API.

## Related Topics

- [Trees and Actions](trees-and-actions.md): shaping the tree in the grammar.
- [Running Parsers](running-parsers.md): parse entry points, errors and position units.
- [Incremental Parsing](incremental.md): snapshots keep a stable tree while a document changes.
