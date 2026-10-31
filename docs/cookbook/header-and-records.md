# Read a Header and Then Records From a Stream

**Problem.** A file starts with a header (a version, a title) and continues with a very large number of records. You want
to read the header and process the records one at a time without loading the file.

`ParseStream` does not return what comes before the repetition. Make the header an element of the repetition, and tell the
kinds of element apart by the type of the value in the callback.

```go
// main.go
package main

import (
	"fmt"
	"strings"

	"github.com/ornew/zeroparse"
)

const grammar = `package kv

type Header = { Version Version }
type Version = node[string]
type Pair = { Key Key; Value Value }
type Key = node[string]
type Value = node[string]

@start
rule main =  ^^ item* #stream $$
rule item =  header / pair
rule header Header =  "#version " v:version "\n" => Header{Version: $v}
rule version Version =  ([0-9]+) #atomic
rule pair Pair =  k:key "=" v:value "\n" => Pair{Key: $k, Value: $v}
rule key Key =  ([a-z_]+) #atomic
rule value Value =  ([0-9]+) #atomic `

func main() {
	p, err := zp.CompileSource(grammar, "main")
	if err != nil {
		panic(err)
	}
	err = p.ParseStream(strings.NewReader("#version 2\na=1\nb=2\n"), func(n *zp.Node) error {
		switch n.Type() {
		case "Header":
			fmt.Println("header, version", n.Field("Version").(*zp.Node).Text())
		case "Pair":
			fmt.Println("pair", n.Field("Key").(*zp.Node).Text(), "=", n.Field("Value").(*zp.Node).Text())
		}
		return nil
	})
	fmt.Println("err:", err)
}
```

## Expected Output

```text
header, version 2
pair a = 1
pair b = 2
err: <nil>
```

## How It Works

- `item = header / pair` is the repeated element, so the callback receives a `Header` or a `Pair` struct and switches on
  `n.Type()`.
- Everything outside the repetition is parsed and checked but not returned, which is why the header cannot stay outside.

## Variations and Limits

- A stream whose header appears later, or twice, is accepted by this grammar. If that matters, check it in the callback,
  or give the first element its own rule outside the repetition and accept that its value is not delivered.
- To total the records of a large file, see [Process a file that does not fit in memory](big-files.md).
