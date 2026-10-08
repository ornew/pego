<div align="center">

# PEGO

**Grammar In. Bulletproof Parser Out.**

Blazing fast. Runs anywhere. Streams forever. Reparses in a blink.

[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENCE) [![Go Reference](https://pkg.go.dev/badge/github.com/ornew/pego.svg)](https://pkg.go.dev/github.com/ornew/pego) [![Go](https://img.shields.io/badge/go-1.27+-00ADD8?logo=go&logoColor=white)](go.mod) [![Netlify Status](https://api.netlify.com/api/v1/badges/29040d97-0646-4829-8871-188310aec03a/deploy-status)](https://app.netlify.com/projects/pego-go/deploys)

[Website](https://pego.ornew.net/) · [Playground](https://pego.ornew.net/playground/) · [Quick start](docs/tutorial/getting-started.md) · [Guides](docs/guide/README.md) · [Specification](spec/README.md) · [Examples](examples/)

</div>

PEGO turns one grammar file into a production parser. The grammar language extends
[Parsing Expression Grammars](https://en.wikipedia.org/wiki/Parsing_expression_grammar) with types, operator
precedence, left recursion and error recovery, so a single `.pego` file defines both the syntax and the tree you get
back. Run it on the engine, on a portable bytecode VM, or as generated code with zero dependencies — the result is
the same everywhere.

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

- **Edits in a blink.** `Document` reparses only what an edit touched: about 1 ms after a one-character edit to an
  89 KB program, 15× faster than parsing it again.
- **Infinite input, flat memory.** `ParseStream` emits each element as soon as it matches and drops the input it
  has consumed, so memory stays constant however long the stream runs.
- **Write once, run anywhere.** Grammars compile to a language-independent bytecode with a
  [published specification](docs/bytecode.md). Precompiled `.pegoc` files load in tens of microseconds, and
  `pego gen` emits a standalone parser: Go that needs nothing but the standard library, or a TypeScript module with
  no dependencies for Node.js, Deno, Bun and browsers.
- **Fast out of the box.** A 262 KB JSON document becomes a fully typed, positioned tree in about 3 ms with a
  generated parser — less time than merely validating it. Recognition mode validates without building a tree at all.
- **Every backend, one answer.** Closure engine, recursive VM, iterative VM, generated Go and TypeScript: the test
  suite checks that all of them return identical trees, positions and errors.
- **No nesting too deep.** The iterative VM runs on its own stack, up to 10 million nested rule calls.
- **Types all the way down.** Declare struct, union and terminal types and build them in actions. Rule types are
  inferred and checked when the grammar compiles, and `pego gen -types` emits matching Go types that the parser
  builds directly — in half the time of its own tree and a quarter of the memory.
- **Precedence without the pain.** Operators are `pratt` levels, not a ladder of rules. Left recursion, direct or
  indirect, just works.
- **Errors that point the way.** Failures report the farthest position and what was expected there. `#error`
  rewrites the message; `#recover` skips the broken part and keeps going.
- **See what your grammar does.** `pego trace` shows every rule call with positions, results and memo hits,
  `pego explain` which calls make up a syntax error, and `pego profile` where the time and the backtracking go.
  `pego lint` points out alternatives that can never match and other likely mistakes before any input does.
- **At home in your editor.** `pego lsp` gives any LSP editor errors as you type, formatting, go to definition,
  hover with inferred rule types, rename and completion; a VS Code extension is included.
- **Context when you need it.** Predicates and scoped variables handle indentation, matching tags and other things
  plain PEG cannot.

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
pego explain -g grammar.pego < input.txt         # which rule calls make up a syntax error
pego trace -g grammar.pego -i 'abc=12'           # every rule call as an indented tree (-f json for tools)
pego profile -g grammar.pego < input.txt         # cost per rule, with hints on wasted work
pego lint -g grammar.pego                        # likely mistakes: dead alternatives, unused captures, ...
pego lsp                                         # language server for editors (errors, formatting, rename, ...)
pego fmt -w grammar.pego                         # format in place, comments kept
pego compile -g grammar.pego -o grammar.pegoc    # precompile
pego gen -g grammar.pego -pkg calc -o parser.go  # generate a standalone Go parser
pego gen -g grammar.pego -pkg calc -types        # ... with Go types for the grammar's types
pego gen -g grammar.pego -pkg calc -recognize    # ... with Recognize, which validates without a tree
pego gen -lang ts -g grammar.pego -o parser.ts     # generate a standalone TypeScript module
pego sample -g grammar.pego -n 20 -coverage      # generate inputs the grammar accepts (tests, fuzz seeds)
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
| [Guides](docs/guide/README.md) | Trees and actions, expressions, errors and recovery, context-sensitive parsing, running parsers, code generation (Go and TypeScript), streaming and incremental parsing, debugging and profiling, linting grammars, sampling inputs and fuzzing, editor support |
| [Editor support](docs/guide/editor-support.md) | `pego lsp` and the VS Code extension: errors as you type, formatting, navigation, hover with inferred types, rename, completion |
| [Web site and playground](docs/guide/playground.md) | Try grammars in the browser at [pego.ornew.net/playground](https://pego.ornew.net/playground/); build and preview the site locally |
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
