# Getting Started With PEGO

This lesson takes you from an empty directory to a first grammar that you run on the command line and from a Go
program. Every grammar, command and Go program in the tutorial is complete and was run to produce the output shown, so
you can type along.

## What You Will Build

A grammar that matches `hello world`, the tree it produces, and a Go program that parses with it and reports an error.

## Before You Start

You need Go 1.27 or later. No other dependencies are required. To try a grammar before installing anything, use the
[playground](https://pego.ornew.net/playground/), which runs PEGO in your browser.

## Install

PEGO has two parts: the Go library, and the `pego` command-line tool that runs, formats and compiles grammars. The
command-line tool is the quickest way to experiment, so install it first.

```bash
go install github.com/ornew/pego/cmd/pego@latest    # command-line tool
```

Running `pego` without arguments lists its commands, among them `parse`, `test`, `fmt`, `lint`, `gen` and `compile`. To use the library
from your own program, add it to your module:

```bash
go get github.com/ornew/pego                        # library
```

Create a directory for the tutorial files:

```bash
mkdir pego-tutorial && cd pego-tutorial
```

## Your First Grammar

A PEGO grammar is a set of **rules**. A rule has a name and a body that says what input it matches. Parsing starts at
the **start rule**, which is called `main` by default. Save this as `hello.pego`:

```pego
rule main = "hello" " " "world"
```

`main` matches the string literal `hello`, then a space, then `world`. Run it with `pego parse`, giving the grammar with
`-g` and the input with `-i`:

```bash
pego parse -g hello.pego -i 'hello world'
```

```json
{
  "type": "Seq",
  "rule": "main",
  "start": 0,
  "end": 11,
  "children": [
    {
      "type": "Match",
      "start": 0,
      "end": 5,
      "text": "hello"
    },
    {
      "type": "Match",
      "start": 5,
      "end": 6,
      "text": " "
    },
    {
      "type": "Match",
      "start": 6,
      "end": 11,
      "text": "world"
    }
  ]
}
```

The result is a **tree of nodes**. Each node has a `type`, a range in the input (`start` inclusive, `end` exclusive,
counted in Unicode code points), and either `text` (for a terminal) or `children`. Here the sequence of three literals
became a `Seq` node, labeled `"rule": "main"` because the rule `main` created it, with three `Match` terminals under it.

The JSON form is complete but long. `-f sexpr` prints the same tree as a compact S-expression, which is what the rest
of this tutorial uses:

```bash
pego parse -g hello.pego -f sexpr -i 'hello world'
```

```text
(Seq "hello" " " "world")@main
```

How to read it:

| Syntax | Meaning |
|:--|:--|
| `"abc"` | a terminal (a `Match` node) holding the text `abc` |
| `Name"abc"` | a terminal of the user-defined terminal type `Name` |
| `(Seq a b)` | a `Seq` node (the value of a sequence) with children `a` and `b` |
| `[a b]` | a `List` node (the value of a repetition) |
| `(Pair Key=a Value=b)` | a node of a struct type with fields (and captures) `Key` and `Value` |
| `x@rule` | the node was created by the rule `rule` |

The start rule must match the **whole input**. If it matches only a prefix, that is a syntax error, and so is input
that does not match at all. Both are reported with a position and what was expected there:

```bash
pego parse -g hello.pego -f sexpr -i 'hello there'
```

```text
pego: 1:7: syntax error: expected "world"
```

The position is `line:column`, both 1-based. The command exits with status 1.

### From Go

The same grammar from a Go program. Create a module and `main.go`:

```bash
go mod init example.com/hello
go get github.com/ornew/pego
```

```go
package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/ornew/pego"
)

const src = `rule main = "hello" " " "world"`

func main() {
	p, err := pego.CompileSource(src, "main")
	if err != nil {
		log.Fatal(err) // a mistake in the grammar
	}

	node, err := p.Parse("hello world")
	if err != nil {
		log.Fatal(err) // the input does not match
	}

	fmt.Println(node) // the S-expression form
	fmt.Println(node.Type(), node.Rule(), node.Start, node.End)
	for _, c := range node.Children {
		fmt.Printf("%s %q [%d,%d)\n", c.Type(), c.Text, c.Start, c.End)
	}

	data, _ := json.Marshal(node)
	fmt.Println(string(data))

	_, err = p.Parse("hello there")
	fmt.Println(err)
}
```

```bash
go run .
```

```text
(Seq "hello" " " "world")@main
Seq main 0 11
Match "hello" [0,5)
Match " " [5,6)
Match "world" [6,11)
{"type":"Seq","rule":"main","start":0,"end":11,"children":[{"type":"Match","start":0,"end":5,"text":"hello"},{"type":"Match","start":5,"end":6,"text":" "},{"type":"Match","start":6,"end":11,"text":"world"}]}
1:7: syntax error: expected "world"
```

- `pego.CompileSource(grammarSource, startRule)` parses and type-checks the grammar and returns a `*pego.Parser`. It
  fails with an error that has the line and column in the grammar source if the grammar is wrong. Compile once and reuse
  the parser; it can be used from several goroutines.
- `p.Parse(input)` returns a `*pego.Node`. Nodes expose `Type()`, `Rule()`, `Start`, `End`, `Text`, `Children`
  and `Fields`, and encode to JSON as shown.
- When the input does not match, `Parse` returns a `*pego.SyntaxError` (see [Reporting Errors](errors.md)).

## Next Steps

The next lesson, [Parsing Structured Input](structured-input.md), grows this grammar into a parser for a small
configuration language. The [tutorial index](README.md) lists all lessons.
