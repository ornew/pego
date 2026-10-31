# Handling Big and Live Input

Two situations break the picture of "read the whole text, parse it, use the tree": the input is too big to hold, or the
text is edited while it is being parsed. This lesson handles both with the configuration grammar.

## What You Will Build

A stream parser that handles one entry at a time, and a program that reparses an edited document and shows how little it
evaluates again.

## Before You Start

Complete [Building Typed Trees](typed-trees.md): you need `config.zp` and a Go module that requires `github.com/ornew/zeroparse`.

## Streaming

Mark a repetition at the top level of the start rule with `#stream` and each element is handed to your code and
forgotten, so memory depends on the size of one element and not on the size of the file. Save as `stream.zp`:

```zp
type Key = node[string]
type Number = node[string]
type String = node[string]
type Bool = node[string]
type Value ~ Number | String | Bool

type Entry = {
    Key   Key
    Value Value
}

@start
rule main = -ws entry* #stream $$

rule entry Entry = k:key -ws -"=" -ws v:value -ws => Entry{Key: $k, Value: $v}

rule value Value = number / string / bool
rule key Key = ([a-z_]+) #atomic
rule number Number = ([0-9]+) #atomic
rule string String = ("\"" [^"]* "\"") #atomic
rule bool Bool = ("true" / "false") #atomic

rule ws = (blank / comment)*
rule blank = [ \t\r\n]+
rule comment = "#" [^\n]*
```

On the command line, `-stream` prints one line per element. Entries that matched before an error are printed, then the
error is reported with positions in the whole input:

```bash
printf 'host = "localhost"\nport = 8080\ndebug = maybe\n' | zp parse -g stream.zp -stream -f sexpr
```

```text
Entry{Key: Key("host"), Value: String("\"localhost\"")}
Entry{Key: Key("port"), Value: Number("8080")}
zp: 3:9: syntax error: expected "#", "\"", "false", "true", [ \t\r\n], [0-9]
```

## Incremental Parsing

An editor changes a few characters and needs a new tree at once. A `Document` holds the text, applies edits and parses
again, reusing the results that the edit did not affect. This program, which also reads the entries with
`ParseStream`, parses `example.conf`-like text, changes `8080` to `9090` and reparses. Put `stream.zp` and `config.zp`
next to it:

```go
package main

import (
	_ "embed"
	"fmt"
	"log"
	"strings"

	"github.com/ornew/zeroparse"
)

//go:embed stream.zp
var streamSource string

//go:embed config.zp
var configSource string

func main() {
	// Streaming: entries arrive one at a time and are forgotten.
	sp, err := zp.CompileSource(streamSource, "main")
	if err != nil {
		log.Fatal(err)
	}
	input := "host = \"localhost\"\nport = 8080\ndebug = true\n"
	err = sp.ParseStream(strings.NewReader(input), func(n *zp.Node) error {
		key := n.Field("Key").(*zp.Node)
		fmt.Printf("[%d,%d) %s\n", n.Start(), n.End(), key.Text())
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	// Incremental parsing: edit a document and parse it again.
	cp, err := zp.CompileSource(configSource, "main")
	if err != nil {
		log.Fatal(err)
	}
	doc, err := cp.NewDocument(input)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := doc.Parse(); err != nil {
		log.Fatal(err)
	}
	fmt.Println("first parse evaluated", doc.Stats().Evaluated, "rule bodies")

	at := strings.Index(input, "8080")
	if err := doc.Edit(at, at+4, "9090"); err != nil {
		log.Fatal(err)
	}
	cfg, err := doc.Parse()
	if err != nil {
		log.Fatal(err)
	}
	st := doc.Stats()
	fmt.Println("after the edit: evaluated", st.Evaluated, "reused", st.Reused)
	fmt.Println(cfg.Field("Entries").(*zp.Node).Elem(1).Field("Value").(*zp.Node).Text())
}
```

```text
[0,19) host
[19,31) port
[31,44) debug
first parse evaluated 46 rule bodies
after the edit: evaluated 7 reused 7
9090
```

The first parse evaluates every rule body; after the edit only seven are evaluated again and seven results are reused.
The result is always the one a fresh parse of the new text would give; reuse only changes the cost.

## Next Steps

[Working With the Tooling](tooling.md) shows the tools that explain, trace and profile a grammar. The guides
[Streaming](../guide/streaming.md) and [Incremental Parsing](../guide/incremental.md) describe what is emitted and when,
writing grammars that reuse well, and limits. The cookbook has recipes for a [file that does not fit in
memory](../cookbook/big-files.md) and [reparsing on every keystroke](../cookbook/as-you-type.md).
