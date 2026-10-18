# Shipping a Parser

A grammar becomes useful when other code can call it. There are three ways to ship one: compile the grammar in your
program at start-up, which is what the previous lessons did; generate Go source, so that the program needs neither the
library nor the grammar; or save a compiled grammar and load it. This lesson shows the last two and the choice of a
backend.

## What You Will Build

A `config` Go package generated from the configuration grammar, and a compiled grammar file that the command line
accepts in place of the source.

## Before You Start

Complete [Building Typed Trees](typed-trees.md): you need `config.pego` and a Go module (`go mod init example.com/hello`).

## Generating a Standalone Go Parser

`pego gen` turns a grammar into Go source, so your program does not need the PEGO library or the grammar at run time.
The generated file uses only the standard library. Generate it into a `config` package:

```bash
mkdir config
pego gen -g config.pego -pkg config -o config/parser.go
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
(Config Entries=[(Entry Key=Key"port"@key Value=Number"8080"@number)])
```

The generated `Node` type mirrors `pego.Node`, and the generated parser gives the same trees and errors as the
library. `config.ParseRule(name, input)` starts at another rule. The generated entry points accept functional
options: `config.Parse(input, config.WithUnit(config.Bytes), config.WithMaxDepth(600_000))`.
`pego gen -max-depth 600000` sets the generated default; omitted or zero keeps 100,000. Existing raw unit arguments
migrate to `WithUnit`, including function-value and slice consumers (see the
[migration notes](../guide/generated-go-api.md#invocation-options-and-migration)).

## Backends and Compiled Grammars

A grammar can run on several backends with identical results: the default closure-compiled engine, a bytecode VM, and
a bytecode VM that uses an explicit stack instead of the Go stack for deeply nested input. Choose one on the command line
or in Go:

```bash
pego parse -g config.pego -f sexpr -backend bytecode < example.conf
pego parse -g config.pego -check -backend bytecode-iterative < example.conf   # validate only, prints ok
```

```go
n, err := p.Parse("abc=12", pego.WithBackend(pego.Bytecode)) // also pego.WithUnit(pego.Bytes), pego.RecognizeOnly()
```

`pego compile -g config.pego -o config.pegoc` saves a compiled grammar that every command accepts in place of the
`.pego` file (`pego parse -g config.pegoc ...`), and `pego.LoadParser` loads it without recompiling.
`pego convert -to json config.pego` converts a grammar to JSON, which is handy for tools that generate grammars.
The [Running Parsers](../guide/running-parsers.md) guide describes the backends and options in detail, and the
[Compiled Grammars](../guide/compiled-grammars.md) guide the `.pegoc` format.

## Next Steps

[Handling Big and Live Input](big-and-live-input.md) covers input that does not fit in memory and text that is edited
while parsed. The guides [Generating Parsers](../guide/code-generation.md), [Generated TypeScript
Parsers](../guide/typescript.md), [Compiled Grammars](../guide/compiled-grammars.md) and [Using the Ready-Made
Parsers](../guide/ready-made-parsers.md) go further, and the cookbook recipe [Ship a Parser and Keep It Up to
Date](../cookbook/ship-a-parser.md) shows how to keep generated code in step with the grammar.
