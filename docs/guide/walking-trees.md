# Walking Trees

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
	parser, err := pego.CompileSource(`@start
rule start = "a" "b"`, "start")
	if err != nil {
		log.Fatal(err)
	}

	root, err := parser.Parse("ab", pego.WithUnit(pego.CodePoints))
	if err != nil {
		log.Fatal(err)
	}

	err = pego.Walk(root, func(v pego.WalkVisit) (pego.WalkAction, error) {
		if v.Node != nil {
			fmt.Printf("%s [%d,%d) depth=%d\n", v.Node.Kind(), v.Node.Start(), v.Node.End(), v.Depth)
		}
		return pego.WalkContinue, nil
	})
	if err != nil {
		log.Fatal(err)
	}

	cursor := pego.Cursor{Root: root, Unit: pego.CodePoints}
	err = cursor.Range(0, 1, func(v pego.WalkVisit) (pego.WalkAction, error) {
		fmt.Printf("overlap: %s [%d,%d)\n", v.Node.Kind(), v.Node.Start(), v.Node.End())
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
tuple [0,2) depth=0
string [0,1) depth=1
string [1,2) depth=1
overlap: tuple [0,2)
overlap: string [0,1)
{"start":0,"end":2,"value":[{"start":0,"end":1,"value":"a"},{"start":1,"end":2,"value":"b"}]}
```

The visit gives you the value reached by the current edge, the node when that value is a node, its parent, its depth
and the incoming edge. Return `pego.WalkSkip` to omit the descendants of a visit, `pego.WalkStop` to end the walk
successfully, or an error to stop with that error. A node's elements are visited before its fields; with source
information, a capture that labels an element is visited only as that element, so a walk stays linear in the size of
the tree however deeply operators nest. A shared node is visited once for each incoming edge, and a cycle
on the active path returns a `*pego.CycleError`, which `errors.As` can inspect. The order of the visits, the cursor
methods `At` and `Range`, and the rule that the unit of the cursor must be the unit of the parse are defined in
[Traversal](../../spec/runtime/nodes.md#traversal). Generated Go parsers expose the same walker as `Walk`.

Use `Node.WriteJSON` instead of `json.Marshal` for very deep results: it writes the graph without recursion, whereas
`encoding/json` gives up beyond its own nesting limit.

## Related Topics

- [Reading Trees](reading-trees.md): values, positions, printing and JSON.
- [Output Schemas and Structural Queries](output-schemas-and-queries.md): finding nodes by their shape.
- [Incremental Parsing](incremental.md): snapshots keep a stable tree while a document changes.
