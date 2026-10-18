# Keep Going After a Bad Statement in Nested Blocks

**Problem.** A script has statements and blocks that contain statements. A mistake in one statement, even deep inside a
block, must not hide the others: you want every error, and the statements that were fine, at every level.

Put `#recover` on the statement rule, which every block body uses. A bad statement becomes an `error` value in the list
that holds it, the recovery happens in the innermost block that contains the mistake, and the blocks around it are
unaffected. The one rule to keep is that the skip must not eat a brace, or it would swallow the end of the block.

```pego
// script.pego
package script

type Name terminal
type Num terminal
type Set = { Name Name; Value Num }
type Block = { Cond Name; Body []Stmt }
type Stmt ~ Set | Block | error
type Script = { Body []Stmt }

rule main Script = -ws body:stmts -ws $$ => Script{Body: $body}
rule stmts = ss:(s:stmt -ws)* => map($ss, (x) => x.$s)

// A bad statement is skipped up to the next ";" or line end, but never over a brace.
rule stmt Stmt = s:(set / block) #recover(skip=[^;{}\n]+ ";"?) => $s

rule set Set = (
    "set" #atomic sp n:name -ws "=" @error(message="expected '='") -ws v:num @error(message="expected a number")
    -ws ";" @error(message="expected ';'")
) => Set{Name: $n, Value: $v}
rule block Block = (
    "if" #atomic sp c:name -ws "{" @error(message="expected '{'") -ws b:stmts -ws "}" @error(message="expected '}'")
) => Block{Cond: $c, Body: $b}

rule name Name = [a-z]+
rule num Num = [0-9]+
rule sp = [ \t]+
rule ws = (&[ \t\r\n] .)*
```

```go
// main.go
package main

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ornew/pego"
)

//go:embed script.pego
var grammar string

const source = `set a = 1;
set b = ;
if debug {
    set c = 2;
    set d 3;
    if nested {
        set e = x;
    }
}
set f = 6;
`

func main() {
	p, err := pego.CompileSource(grammar, "main")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	root, err := p.Parse(source)
	var errs pego.SyntaxErrors
	if err != nil && !errors.As(err, &errs) {
		fmt.Fprintln(os.Stderr, err) // not a recovered parse: no tree
		os.Exit(1)
	}
	for _, e := range errs {
		fmt.Printf("%d:%d: %s\n", e.Line, e.Col, e.Message())
	}

	// The outline: how many statements were read at each level, and where the bad ones are.
	var outline func(n *pego.Node, indent string)
	outline = func(list *pego.Node, indent string) {
		for _, n := range list.Elems() {
			if n.Kind() == pego.KindError {
				fmt.Printf("%sERROR %q\n", indent, strings.TrimSpace(source[n.Start():n.End()]))
				continue
			}
			switch n.Type() {
			case "Set":
				fmt.Printf("%sset %s\n", indent, n.Field("Name").(*pego.Node).Text())
			case "Block":
				fmt.Printf("%sif %s\n", indent, n.Field("Cond").(*pego.Node).Text())
				outline(n.Field("Body").(*pego.Node), indent+"  ")
			}
		}
	}
	outline(root.Field("Body").(*pego.Node), "")
}
```

## Expected Output

```text
2:9: expected a number
5:11: expected '='
7:17: expected a number
set a
ERROR "set b = ;"
if debug
  set c
  ERROR "set d 3;"
  if nested
    ERROR "set e = x;"
set f
```

The three errors are reported together, and the outline shows that `a`, `c` and `f` were read, the bad statements sit in
the block that holds them, and `if nested` is still a block with its own list.

## How It Works

- Every statement, at every level, is read by the same `stmt` rule, so `#recover` applies everywhere. A failure inside a
  nested block is caught by the `stmt` of that block's body, which is the nearest enclosing recovery.
- The `skip` starts where the statement started and runs to the next `;`, but cannot cross `{` or `}`. A bad `set` costs
  its own line only, and the `}` that closes the block stays for the `block` rule.
- Each `@error` replaces the mechanical list of expected characters with one message at the position where the statement
  went wrong.
- `Parse` returns the tree and a `pego.SyntaxErrors` together, so the program checks for that type before treating the
  error as a failure.

## Variations and Limits

- A block that is never closed is not recovered. The skip cannot cross a brace, so the statement that holds the block
  fails as a whole, and the parse fails with one syntax error near the start of that block instead of a tree. Braces are
  the structure, so the recipe leaves them out of the skip on purpose.
- A skip that is allowed to cross braces recovers more, but a `}` inside the skipped text then ends nothing, and the rest
  of the file can be read as part of the wrong block. See the
  [recovery guide](../guide/recovery.md#do-not-let-the-skip-eat-the-closer).
- For a flat list of settings, see [Report several errors in one run](several-errors.md).
