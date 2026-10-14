# Walking Parse Trees

Use `pego.Walk` to inspect a parse result without writing a recursive tree walk. `pego.Cursor` applies point or range filters to node spans while preserving the same traversal order.

## Walk a Tree

This example parses a short sequence, visits its nodes, selects nodes overlapping an input range, and writes JSON directly:

```go
package main

import (
	"fmt"
	"os"

	"github.com/ornew/pego"
)

func main() {
	parser, err := pego.CompileSource(`rule start = "a" "b"`, "start")
	if err != nil {
		panic(err)
	}

	root, err := parser.Parse("ab", pego.WithUnit(pego.CodePoints))
	if err != nil {
		panic(err)
	}

	err = pego.Walk(root, func(v pego.WalkVisit) (pego.WalkAction, error) {
		if v.Node != nil {
			fmt.Printf("%s [%d,%d) depth=%d\n", v.Node.Type(), v.Node.Start, v.Node.End, v.Depth)
		}
		return pego.WalkContinue, nil
	})
	if err != nil {
		panic(err)
	}

	cursor := pego.Cursor{Root: root, Unit: pego.CodePoints}
	err = cursor.Range(0, 1, func(v pego.WalkVisit) (pego.WalkAction, error) {
		fmt.Printf("overlap: %s [%d,%d)\n", v.Node.Type(), v.Node.Start, v.Node.End)
		return pego.WalkContinue, nil
	})
	if err != nil {
		panic(err)
	}

	if err := root.WriteJSON(os.Stdout); err != nil {
		panic(err)
	}
}
```

`WalkVisit.Value` is the value reached by the current edge. `Node` is non-nil only when that value is a non-nil `*pego.Node`; `Parent`, `Depth`, and `Edge` describe its nearest owning node and incoming edge. The root is visited first, followed by children in slice order, fields in sorted name order, and nested `[]any` elements in index order. Scalar and nil values are visited too. A shared node or list is visited once for each incoming edge.

Return `pego.WalkSkip` to omit a visit's descendants, or `pego.WalkStop` to end successfully. Returning an error stops the walk and returns that error unchanged. A cycle on the active traversal path returns `*pego.CycleError`; use `errors.As` to inspect it. Walk is read-only and does not retain the graph. Keep the graph unchanged while the walk or its callback is running.

## Select Nodes With a Cursor

Set `Cursor.Unit` to the same unit used to parse the input. The cursor does not infer or convert units. `At` selects nodes containing a position, including zero-width nodes at that position. `Range` uses a half-open range and selects overlapping nodes; an empty range selects none. Scalars and list values do not have spans and are not selected. A parent span does not prune traversal, so matching descendants can still be visited when their parent's span does not match.

## Walk Generated Go Trees

Generated Go parsers expose the corresponding generic node walker as `WalkTree` to avoid colliding with a parser-specific `Walk` function. Its visits and edge-ordering behavior match `pego.Walk`.

## Write Deep Trees as JSON

`Node.WriteJSON` writes a node graph without recursive node or `[]any` traversal. Use it when writing very deep results directly to an `io.Writer`. `Node.MarshalJSON` also uses the iterative writer, but wrapping it in `encoding/json` can still encounter that package's nesting limit while it validates the returned JSON.
