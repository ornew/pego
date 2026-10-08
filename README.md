# PEGO

PEGO is a parser framework for Go. You describe a language in the PEGO grammar language — an extension of
[Parsing Expression Grammars](https://en.wikipedia.org/wiki/Parsing_expression_grammar) — and PEGO turns it into a
parser that builds typed syntax trees, reports precise errors, and can recover from them.

```pego
type Pair struct { Key Match, Value Match }

def main = k:@(?a-z)+ "=" v:@(?0-9)+ -> new Pair{Key: $k, Value: $v}
```

```go
p, err := pego.CompileSource(src, "main")
node, err := p.Parse("abc=12")
fmt.Println(node) // (Pair Key="abc" Value="12")
```

> **Status:** PEGO is under active development. The language and the Go API may still change.
> See [docs/development.md](docs/development.md) for the implementation status and roadmap.

New to PEGO? The [getting-started tutorial](docs/tutorial/getting-started.md) builds a configuration language and a
calculator step by step, and the [guides](docs/guide/README.md) cover each feature in depth.

## Features

**Grammar language**

- **Typed trees.** Declare struct, union and terminal types and build them with actions; rule types are inferred and
  checked at compile time.
- **Pratt expressions.** Operator precedence and associativity are declared as levels instead of being encoded in rules.
- **Left recursion.** Directly and indirectly left-recursive rules are supported.
- **Context-sensitive parsing.** Predicates and scoped variables handle constructs such as indentation-based blocks or
  matching tags.
- **Error reporting and recovery.** Failures are reported at the farthest position with the expected alternatives;
  `#error` replaces them with custom messages and `#recover` skips malformed input and keeps going.

**Runtime**

- **Several backends with identical results:** a closure-compiled engine, a portable bytecode VM (recursive, or with
  an explicit stack for deeply nested input), and generated standalone Go parsers (`pego gen`).
- **Streaming** parsing of unbounded input, emitting the elements of a `#stream` repetition as they complete.
- **Incremental** reparsing of edited documents, reusing results that an edit did not affect.
- **Recognition mode** that validates input without building a tree.
- **Compiled grammars** (`.pegoc`) that load without recompiling; the bytecode can be saved without the grammar AST
  for a minimal runtime footprint.
- Positions in Unicode code points or UTF-8 bytes.

## Installation

```bash
go get github.com/ornew/pego                        # library
go install github.com/ornew/pego/cmd/pego@latest    # command-line tool
```

PEGO requires Go 1.27 or later and has no dependencies outside the standard library.

## Usage

### Go API

```go
import "github.com/ornew/pego"

p, err := pego.CompileSource(grammarSource, "main")
if err != nil {
	// grammar errors, with positions
}

node, err := p.Parse(input)    // *pego.SyntaxError if the input does not match
data, _ := json.Marshal(node)  // nodes encode to JSON with types and positions

p.Parse(input, pego.WithUnit(pego.Bytes))       // positions in UTF-8 bytes
p.Parse(input, pego.WithBackend(pego.Bytecode)) // choose a backend
p.Parse(input, pego.RecognizeOnly())            // validate without building a tree
```

Grammars can also be built programmatically with the `grammar` package and compiled with `pego.Compile`.

Streaming and incremental parsing:

```go
err := p.ParseStream(reader, func(n *pego.Node) error { /* one element */ return nil })

doc, _ := p.NewDocument(text)
node, err := doc.Parse()
doc.Edit(10, 12, "new text") // replace [10, 12)
node, err = doc.Parse()      // reuses results outside the edit
```

Saving and loading compiled grammars:

```go
data, _ := p.MarshalBinary()   // or p.Marshal(pego.WithoutAST())
p2, _ := pego.LoadParser(data) // no parsing, analysis or type checking at load time
```

### Command-line tool

```bash
pego parse -g grammar.pego -i 'abc=12'           # parse and print the tree as JSON (-f sexpr for S-expressions)
pego parse -g grammar.pego -check < input.txt    # validate only
pego fmt -w grammar.pego                         # format PEGO source in place (comments are kept)
pego convert -to json grammar.pego               # convert between .pego, JSON and .pegoc
pego compile -g grammar.pego -o grammar.pegoc    # save a compiled grammar
pego gen -g grammar.pego -pkg calc -o parser.go  # generate a standalone Go parser
pego gen -g grammar.pego -pkg calc -types        # ... also with Go types for the grammar types
```

Run `pego` without arguments for the full list of commands and flags.

## Examples

The [examples](examples/) directory contains complete grammars with tests, including JSON, CSV, XML, an arithmetic
calculator (with Pratt expressions and with left recursion), an indentation-based outline format, a small
programming language, and practical grammars for Go and Python 3 that are tested against real source files
(the Go standard library and `go/ast`, and the Python standard library and CPython's `ast` module).

## Documentation

| Document | Contents |
|:--|:--|
| [Getting started](docs/tutorial/getting-started.md) | A step-by-step tutorial, from a first grammar to typed trees and operator precedence |
| [Guides](docs/guide/README.md) | Trees and actions, expressions, errors and recovery, context-sensitive parsing, running parsers, code generation, streaming and incremental parsing |
| [Language specification](spec/README.md) | The PEGO grammar language |
| [Development guide](docs/development.md) | Architecture, repository layout, implementation status, roadmap |
| [Bytecode specification](docs/bytecode.md) | Instruction set and VM semantics, for porting the runtime |
| [Benchmarks](docs/benchmarks.md) | Backend comparison and comparison with the Go standard library |
| [Performance tuning log](docs/performance.md) | Optimizations made, experiments, remaining hotspots |
| [Design records](docs/design/) | Design decisions and their rationale |

## Contributing

Bug reports, feature requests and pull requests are welcome. Please run `go test ./...` before submitting changes and
follow the [commit message guidelines](docs/commit-messages.md).

## License

PEGO is licensed under the [Apache License 2.0](LICENCE).
