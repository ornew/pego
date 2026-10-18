# Find Nodes in a Tree and at a Position

**Problem.** After parsing, you want to answer two questions about the tree without writing a recursive function for each:
where in the text are all the nodes of one kind (every function call, say), and which node is under the cursor.

Use `pego.Walk` for the first question: it visits every node, and the visit gives the node, its parent and its depth. Use
`pego.Cursor` for the second: `At` visits the nodes whose span contains a position, outermost first. This recipe lists
the calls of a small program with their positions, and finds the call that contains a position and the argument under it.

```pego
// calls.pego
package calls

type Name terminal
type Num terminal
type Call = { Name Name; Args []Expr }
type Expr ~ Call | Name | Num
type Program = { Body []Expr }

rule main Program = -ws body:stmts $$ => Program{Body: $body}
rule stmts = es:(e:expr -ws ";" -ws)* => map($es, (x) => x.$e)
rule expr Expr = call / name / number
rule call Call = n:name -ws "(" -ws args:args? ")" => Call{Name: $n, Args: concat($args)}
rule args = first:expr rest:(-ws "," -ws e:expr -ws)* => concat(list($first), map($rest, (r) => r.$e))
rule name Name = [a-z]+
rule number Num = [0-9]+
rule ws = (&[ \t\r\n] .)*
```

```go
// main.go
package main

import (
	_ "embed"
	"fmt"
	"os"

	"github.com/ornew/pego"
)

//go:embed calls.pego
var grammar string

const source = "print(add(1, mul(2, x)));\nlog(sum());\n"

func main() {
	p, err := pego.CompileSource(grammar, "main")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	root, err := p.Parse(source)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Every node of one kind, with its depth.
	err = pego.Walk(root, func(v pego.WalkVisit) (pego.WalkAction, error) {
		if v.Node != nil && v.Node.Type() == "Call" {
			name := v.Node.Field("Name").(*pego.Node).Text()
			fmt.Printf("call %-5s [%2d,%2d) depth %d\n", name, v.Node.Start(), v.Node.End(), v.Depth)
		}
		return pego.WalkContinue, nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// The nodes at a position, outermost first.
	pos := int32(17) // the 2 in mul(2, x)
	cursor := pego.Cursor{Root: root, Unit: pego.CodePoints}
	var innermostCall, innermost *pego.Node
	err = cursor.At(pos, func(v pego.WalkVisit) (pego.WalkAction, error) {
		if v.Node != nil {
			innermost = v.Node
			if v.Node.Type() == "Call" {
				innermostCall = v.Node
			}
		}
		return pego.WalkContinue, nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("at %d: innermost node %s %q, innermost call %s\n", pos, innermost.Type(), innermost.Text(),
		innermostCall.Field("Name").(*pego.Node).Text())
}
```

## Expected Output

```text
call print [ 0,24) depth 2
call add   [ 6,23) depth 4
call mul   [13,22) depth 6
call log   [26,36) depth 2
call sum   [30,35) depth 4
at 17: innermost node Num "2", innermost call mul
```

## How It Works

- `Walk` visits every edge of the graph in order: first the node, then what it holds. The `Depth` counts edges, so it is
  larger than the nesting of the source, because lists and fields are edges too. Return `pego.WalkSkip` to leave a
  subtree out, or `pego.WalkStop` to stop at the first match.
- `Cursor.At(pos, fn)` calls `fn` for the nodes whose span contains `pos`, from the outermost to the innermost. Keeping
  the last visit of each kind gives the innermost node of that kind. The cursor's `Unit` must be the unit of the parse
  (code points by default).
- `Node.Field("Name")` returns the value of a field of a typed node, here another node.

## Variations and Limits

- To find nodes by their shape instead of by type name, describe the shape with an output schema; see [Output Schemas and
  Structural Queries](../guide/output-schemas-and-queries.md).
- For a range, such as a selection in an editor, use `Cursor.Range(start, end, fn)`.
- A span is half open: `[start, end)`. A position at the end of a node, such as 22 for `mul` (the `)` after `x`), is not inside it.
- `Walk` visits a shared node once for each edge that reaches it, and returns a `*pego.CycleError` for a cycle.
