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
  "start": 0,
  "end": 11,
  "value": [
    {
      "start": 0,
      "end": 5,
      "value": "hello"
    },
    {
      "start": 5,
      "end": 6,
      "value": " "
    },
    {
      "start": 6,
      "end": 11,
      "value": "world"
    }
  ]
}
```

The result is a **tree of nodes**. Each node has a range in the input (`start` inclusive, `end` exclusive, counted in
Unicode code points) and a `value`. The value of a literal is the text it matched. A sequence has a tuple of the values of
its elements, which is a JSON array, so the sequence of three literals became a node whose value holds three nodes, one
per literal.

The JSON form is complete but long. `-f sexpr` prints the same tree as a compact S-expression, which is what the rest
of this tutorial uses:

```bash
pego parse -g hello.pego -f sexpr -i 'hello world'
```

```text
("hello" " " "world")
```

How to read it:

| Syntax | Meaning |
|:--|:--|
| `"abc"` | a node whose value is the text `abc` |
| `( a b )` | a node whose value is a tuple (the value of a sequence) with elements `a` and `b` |
| `[ a b ]` | a node whose value is a list (the value of a repetition) |
| `nil` | an optional that did not match |
| `Pair{Key: a, Value: b}` | a struct built by an action, with fields `Key` and `Value` |

An S-expression shows only values, not positions. Rule and capture names are source information, which `pego parse` leaves
out unless you ask for it with `-source-info`:

```bash
pego parse -g hello.pego -f sexpr -source-info -i 'hello world'
```

```text
("hello" " " "world")@main
```

Here `@main` says that the rule `main` created the node. Captures appear as `name:` labels in front of their elements, as
later lessons show.

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
	fmt.Println(node.Kind(), node.Start(), node.End())
	for _, c := range node.Elems() {
		fmt.Printf("%q [%d,%d)\n", c.Text(), c.Start(), c.End())
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
("hello" " " "world")
tuple 0 11
"hello" [0,5)
" " [5,6)
"world" [6,11)
{"start":0,"end":11,"value":[{"start":0,"end":5,"value":"hello"},{"start":5,"end":6,"value":" "},{"start":6,"end":11,"value":"world"}]}
1:7: syntax error: expected "world"
```

- `pego.CompileSource(grammarSource, startRule)` parses and type-checks the grammar and returns a `*pego.Parser`. It
  fails with an error that has the line and column in the grammar source if the grammar is wrong. Compile once and reuse
  the parser; it can be used from several goroutines.
- `p.Parse(input)` returns a `*pego.Node`. `Kind()` tells what the value is (here a tuple), `Start()` and `End()` give the
  range, `Text()` is the text of a string node, and `Elems()` iterates over the elements of a tuple or list. Nodes encode
  to JSON as shown.
- When the input does not match, `Parse` returns a `*pego.SyntaxError` (see [Reporting Errors](errors.md)).

## Next Steps

The next lesson, [Parsing Structured Input](structured-input.md), grows this grammar into a parser for a small
configuration language. The [tutorial index](README.md) lists all lessons.
