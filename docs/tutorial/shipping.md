# Shipping a Parser

A grammar becomes useful when other code can call it. There are three ways to ship one: compile the grammar in your
program at start-up, which is what the previous lessons did; generate Go source, so that the program needs neither the
library nor the grammar; or save a compiled grammar and load it. This lesson shows the last two and the choice of a
backend.

## What You Will Build

A `config` Go package generated from the configuration grammar, and a compiled grammar file that the command line
accepts in place of the source.

## Before You Start

Complete [Building Typed Trees](typed-trees.md): you need `config.zp` and a Go module (`go mod init example.com/hello`).

## Generating a Standalone Go Parser

`zp gen` turns a grammar into Go source, so your program does not need the ZeroParse library or the grammar at run time.
The generated package (`parser.go`, `api.go` and a copy of the runtime in `internal/runtime`) uses only the standard
library. `-module` is the import path the package will have, so it starts with your module path. Generate it into a
`config` package:

```bash
zp gen -g config.zp -module example.com/hello/config -o config
```

and call it:

```go
package main

import (
	"fmt"
	"log"

	"example.com/hello/config" // your module path
)

func main() {
	node, err := config.Parse("port = 8080\n")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(node)
}
```

```text
Config{Entries: [Entry{Key: Key("port"), Value: Number("8080")}]}
```

The generated `Node` type mirrors `zeroparse.Node`, and the generated parser gives the same trees and errors as the
library. `config.ParseRule(name, input)` starts at another rule. The generated entry points accept functional
options: `config.Parse(input, config.WithUnit(config.Bytes), config.WithMaxDepth(600_000))`.
`zp gen -max-depth 600000` sets the generated default; omitted or zero keeps 100,000.

## Backends and Compiled Grammars

A grammar can run on several backends with identical results: the default closure-compiled engine, a bytecode VM, and
a bytecode VM that uses an explicit stack instead of the Go stack for deeply nested input. Choose one on the command line
or in Go:

```bash
zp parse -g config.zp -f sexpr -backend bytecode < example.conf
zp parse -g config.zp -check -backend bytecode-iterative < example.conf   # validate only, prints ok
```

```go
n, err := p.Parse("abc=12", zeroparse.WithBackend(zeroparse.Bytecode)) // also zeroparse.WithUnit(zeroparse.Bytes), zeroparse.RecognizeOnly()
```

`zp compile -g config.zp -o config.zpc` saves a compiled grammar that `zp parse` accepts in place of the
`.zp` file (`zp parse -g config.zpc ...`), and `zeroparse.LoadParser` loads it without recompiling. The file serves
the parses of one mode: `-mode parse` (the default) for parsing, `-mode recognize` for `-check`, and `-mode document`
for documents and traces.
`zp convert -to json config.zp` converts a grammar to JSON, which is handy for tools that generate grammars.
The [Running Parsers](../guide/running-parsers.md) guide describes the backends and options in detail, and the
[Compiled Grammars](../guide/compiled-grammars.md) guide the `.zpc` format.

## Next Steps

[Handling Big and Live Input](big-and-live-input.md) covers input that does not fit in memory and text that is edited
while parsed. The guides [Generating Parsers](../guide/code-generation.md), [Generated TypeScript
Parsers](../guide/typescript.md), [Compiled Grammars](../guide/compiled-grammars.md) and [Using the Ready-Made
Parsers](../guide/ready-made-parsers.md) go further, and the cookbook recipe [Ship a Parser and Keep It Up to
Date](../cookbook/ship-a-parser.md) shows how to keep generated code in step with the grammar.
