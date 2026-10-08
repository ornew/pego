<div align="center">

# PEGO

**Typed Parsers for Go, from One Grammar**

Write the grammar. Get the tree. Ship the parser.

[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENCE) [![Go Reference](https://pkg.go.dev/badge/github.com/ornew/pego.svg)](https://pkg.go.dev/github.com/ornew/pego) [![Go](https://img.shields.io/badge/go-1.27+-00ADD8?logo=go&logoColor=white)](go.mod)

[Quick start](docs/tutorial/getting-started.md) · [Guides](docs/guide/README.md) · [Specification](spec/README.md) · [Examples](examples/)

</div>

PEGO extends [Parsing Expression Grammars](https://en.wikipedia.org/wiki/Parsing_expression_grammar) with types,
operator precedence, left recursion and error recovery. One `.pego` file describes the syntax *and* the tree you
want; PEGO runs it on an engine, a portable bytecode VM, or as generated Go with zero dependencies, and returns the
same tree on all of them. No lexer, no codegen step to start, nothing outside the standard library.

```pego
// calc.pego
type Num terminal
type Bin struct { Left Expr, Op Match, Right Expr }
type Expr = Bin | Num

def main: Expr = pratt {
    skip    " "*
    operand num
    operand "(" e:main ")" -> $e
    level { infix left  "+" / "-" -> new Bin{Left: $lhs, Op: $op, Right: $rhs} }
    level { infix left  "*" / "/" -> new Bin{Left: $lhs, Op: $op, Right: $rhs} }
    level { infix right "^"       -> new Bin{Left: $lhs, Op: $op, Right: $rhs} }
}

def num: Num = (?0-9)+
```

```console
$ pego parse -g calc.pego -f sexpr -i '1 + 2 * 3 ^ 2'
(Bin Left=Num"1"@num Op="+" Right=(Bin Left=Num"2"@num Op="*" Right=(Bin Left=Num"3"@num Op="^" Right=Num"2"@num)))

$ pego parse -g calc.pego -i '1 + * 2'
pego: 1:5: syntax error: expected "(", (?0-9)
```

```go
p, _ := pego.CompileSource(src, "main")
tree, err := p.Parse("1 + 2 * 3 ^ 2") // a *pego.Node, or a *pego.SyntaxError with line, column and expectations
```

> [!NOTE]
> PEGO is under active development; the language and the Go API may still change.
> See the [implementation status and roadmap](docs/development.md).

## Why PEGO

- **Trees, not parse dumps.** Declare struct, union and terminal types and build them in actions. Rule types are
  inferred and checked when the grammar compiles, not when your program crashes.
- **Expressions without the ladder.** Precedence and associativity are `pratt` levels, not a tower of rules. Left
  recursion works too, direct or indirect.
- **Errors people can act on.** Failures point at the farthest position with what was expected. `#error` rewrites
  the message; `#recover` skips a broken statement and keeps parsing.
- **Context when you need it.** Predicates and scoped variables handle indentation-based blocks, matching tags and
  other things plain PEG cannot.
- **One grammar, every backend.** A closure-compiled engine, a bytecode VM (recursive, or with an explicit stack for
  deep nesting), and standalone generated Go — all checked to return identical trees, positions and errors.
- **Built for editors and pipelines.** Stream unbounded input element by element, reparse edited documents
  incrementally, validate without building a tree, and load precompiled grammars (`.pegoc`) without compiling them
  again.

## Install

```bash
go get github.com/ornew/pego                        # library
go install github.com/ornew/pego/cmd/pego@latest    # command-line tool
```

Go 1.27 or later. No dependencies outside the standard library.

## Use it

**From Go**

```go
p, err := pego.CompileSource(grammarSource, "main") // grammar errors come with positions

node, err := p.Parse(input)                       // positions in code points by default
p.Parse(input, pego.WithUnit(pego.Bytes))         // ... or in UTF-8 bytes
p.Parse(input, pego.WithBackend(pego.Bytecode))   // pick a backend
p.Parse(input, pego.RecognizeOnly())              // validate without building a tree
json.Marshal(node)                                // nodes encode with types and positions

err = p.ParseStream(r, func(n *pego.Node) error { return nil }) // one #stream element at a time

doc, _ := p.NewDocument(text)
doc.Edit(10, 12, "new text") // replace [10, 12)
node, err = doc.Parse()      // reuses everything the edit did not touch

data, _ := p.MarshalBinary() // save; pego.LoadParser(data) skips parsing and checking
```

**From the command line**

```bash
pego parse -g grammar.pego -i 'abc=12'           # parse and print the tree (JSON, or -f sexpr)
pego parse -g grammar.pego -check < input.txt    # validate only
pego fmt -w grammar.pego                         # format in place, comments kept
pego compile -g grammar.pego -o grammar.pegoc    # precompile
pego gen -g grammar.pego -pkg calc -o parser.go  # generate a standalone Go parser
pego gen -g grammar.pego -pkg calc -types        # ... with Go types for the grammar's types
```

Run `pego` without arguments for every command and flag.

## Examples

Complete grammars with tests live in [examples/](examples/): JSON, CSV, XML, a calculator (Pratt and
left-recursive), an indentation-based outline format and a small programming language — plus practical grammars
for **Go** and **Python 3**, tested against the Go standard library with `go/ast` and the Python standard library
with CPython's `ast` module.

## Documentation

| | |
|:--|:--|
| [Getting started](docs/tutorial/getting-started.md) | From a first grammar to typed trees and operator precedence, step by step |
| [Guides](docs/guide/README.md) | Trees and actions, expressions, errors and recovery, context-sensitive parsing, running parsers, code generation, streaming and incremental parsing |
| [Language specification](spec/README.md) | The PEGO grammar language |
| [Development guide](docs/development.md) | Architecture, repository layout, implementation status, roadmap |
| [Bytecode specification](docs/bytecode.md) | Instruction set and VM semantics, for porting the runtime |
| [Benchmarks](docs/benchmarks.md) · [Performance log](docs/performance.md) | How fast, and how it got there |
| [Design records](docs/design/) | Decisions and their rationale |

## Contributing

Bug reports, ideas and pull requests are welcome. Run `go test ./...` before sending changes, and follow the
[commit message guidelines](docs/commit-messages.md).

## License

[Apache License 2.0](LICENCE)
