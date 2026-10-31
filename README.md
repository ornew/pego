<div align="center">

# ZeroParse

**One Grammar. Every Parser.**

Never write a parser again.

[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENCE) [![Go Reference](https://pkg.go.dev/badge/github.com/ornew/zeroparse.svg)](https://pkg.go.dev/github.com/ornew/zeroparse) [![Go](https://img.shields.io/badge/go-1.27.1+-00ADD8?logo=go&logoColor=white)](go.mod)

[Website](https://zeroparse.dev/) · [Playground](https://zeroparse.dev/playground/) · [Quick start](docs/tutorial/getting-started.md) · [Guides](docs/guide/README.md) · [Specification](spec/README.md) · [Examples](examples/)

</div>

Describe your language once. ZeroParse builds the parser: fast, safe and wherever you need it. Here is a complete
calculator, operator precedence included:

```zp
// calc.zp
type Num = node[string]
type Bin = { Left Expr; Op node[string]; Right Expr }
type Expr ~ Bin | Num

@start
rule main Expr = #operators {
    skip " "*
    operand num
    operand "(" e:main ")" => $e
    level additive { infix left "+" / "-" => Bin{Left: $lhs, Op: $op, Right: $rhs} }
    level multiplicative { infix left "*" / "/" => Bin{Left: $lhs, Op: $op, Right: $rhs} }
    level power { infix right "^" => Bin{Left: $lhs, Op: $op, Right: $rhs} }
}

rule num Num = ([0-9]+) #atomic
```

```bash
zp parse -g calc.zp -f sexpr -i '1 + 2 * 3 ^ 2'
```

```text
Bin{Left: Num("1"), Op: "+", Right: Bin{Left: Num("2"), Op: "*", Right: Bin{Left: Num("3"), Op: "^", Right: Num("2")}}}
```

```bash
zp parse -g calc.zp -i '1 + * 2'
```

```text
zp: 1:5: syntax error: expected "(", [0-9]
```

> [!NOTE]
> ZeroParse is pre-release: the language, the Go API and the file formats may change. Generated Go is not yet as fast
> as Go's hand-written parsers, and closing that gap is the current work. [Why ZeroParse](docs/why-zeroparse.md) has every
> measured number, the current limits and how ZeroParse compares with other tools.

## Why ZeroParse

- **Runs anywhere, any way.** Any target language, any engine, any optimization. One grammar becomes generated code,
  a VM or an interpreter, tuned for speed, memory or size, and every one of them returns the same tree, the same
  positions and the same errors. Whatever you build, ZeroParse fits.
- **Unmatched expressive power.** Build values of any type you define, carry state for context-sensitive syntax and
  write a whole operator precedence table as one rule. Recover from errors with a complete tree, and parse endless
  streams as they arrive. If you can describe a language, you can parse it.
- **Performance pushed to the limit.** A multi-stage IR optimizes at every level, from grammar-wide rewrites down to
  the generated code. Incremental parsing reuses everything an edit did not touch, so a fresh tree is ready between
  keystrokes.
- **Safe on any input.** ZeroParse analyzes your grammar to bound the time and memory a parse can take, and built-in
  budget and nesting limits mean hostile input can never hang the parser or blow its stack.
- **Complete support for building your language.** A built-in language server brings completion, diagnostics,
  navigation and formatting to any editor. Trace, profile, lint and debug show exactly what the parser does, so you
  never have to guess.

## Install

```bash
go get github.com/ornew/zeroparse                        # library
go install github.com/ornew/zeroparse/cmd/zp@latest    # command-line tool
```

Go 1.27.1 or later. No dependencies outside the standard library.

## Use It

**From Go**

```go
p, err := zeroparse.CompileSource(grammarSource, "main") // grammar errors come with positions

node, err := p.Parse(input)                       // positions in code points by default
p.Parse(input, zeroparse.WithUnit(zeroparse.Bytes))         // ... or in UTF-8 bytes
p.Parse(input, zeroparse.WithBackend(zeroparse.Bytecode))   // pick a backend
p.Parse(input, zeroparse.RecognizeOnly())              // validate without building a tree
json.Marshal(node)                                // nodes encode with types and positions

err = p.ParseStream(r, func(n *zeroparse.Node) error { return nil }) // one #stream element at a time

doc, _ := p.NewDocument(text)
doc.Edit(10, 12, "new text") // replace [10, 12)
node, err = doc.Parse()      // reuses rule results the edit did not reach

data, _ := p.MarshalBinary() // save; zeroparse.LoadParser(data) skips parsing and checking
```

**From the command line**

```bash
zp parse -g grammar.zp -i 'abc=12'           # parse and print the tree (JSON, or -f sexpr)
zp parse -g grammar.zp -check < input.txt    # validate only
zp explain -g grammar.zp < input.txt         # which rule calls make up a syntax error
zp trace -g grammar.zp -i 'abc=12'           # every rule call as an indented tree (-f json for tools)
zp profile -g grammar.zp < input.txt         # cost per rule, with hints on wasted work
zp lint -g grammar.zp                        # likely mistakes: dead alternatives, unused captures, ...
zp lsp                                         # language server for editors (errors, formatting, rename, ...)
zp fmt -w grammar.zp                         # format in place, comments kept
zp fix -w grammar.zp                         # rewrite older syntax to the current one
zp compile -g grammar.zp -o grammar.zpc    # precompile
zp gen -g grammar.zp -module example.com/calc -o calc  # generate a standalone Go parser
zp gen -g grammar.zp -module example.com/calc -types -o calc # ... with Go types for the grammar's types
zp gen -g grammar.zp -module example.com/calc -recognize -o calc # ... with Recognize, which validates without a tree
zp gen -lang ts -g grammar.zp -o ts-parser    # generate a TypeScript parser into a directory
zp gen -lang ts -g grammar.zp -types -o ts-parser # also emit types.ts and parseAST
zp gen -g grammar.zp -module example.com/calc -max-depth 600000 -o calc # set its nested rule-call default
zp sample -g grammar.zp -n 20 -coverage      # generate inputs the grammar accepts (tests, fuzz seeds)
zp edit-fuzz -g grammar.zp -minimize -save-failures out  # check Document edits against fresh parses
```

Run `zp` without arguments for every command and flag.

## Ready-Made Parsers

[parsers/](parsers/) has parsers for common languages, written in ZeroParse, generated into Go and tested against the
languages' conformance suites and reference implementations. Each is a module of its own that depends only on the
standard library and ZeroParse's runtime package. Until ZeroParse publishes a release, use them from a checkout of the
repository; `go get` does not fetch them from another module yet (see [Ready-Made Parsers](docs/guide/ready-made-parsers.md)):

```go
import "github.com/ornew/zeroparse/parsers/json"

v, err := json.Decode(`{"name": "zp", "tags": ["peg", "operators-rule"]}`)
```

| Language | Module | Specification |
|:--|:--|:--|
| CEL | [`github.com/ornew/zeroparse/parsers/cel`](parsers/cel/) | The CEL language definition |
| CSV | [`github.com/ornew/zeroparse/parsers/csv`](parsers/csv/) | RFC 4180 |
| CUE | [`github.com/ornew/zeroparse/parsers/cue`](parsers/cue/) | The CUE language specification |
| DuckDB SQL | [`github.com/ornew/zeroparse/parsers/duckdb`](parsers/duckdb/) | DuckDB 1.5.6's SQL dialect |
| Go | [`github.com/ornew/zeroparse/parsers/golang`](parsers/golang/) | The Go specification (go1.27), as `go/parser` parses it |
| JSON | [`github.com/ornew/zeroparse/parsers/json`](parsers/json/) | RFC 8259 |
| Python | [`github.com/ornew/zeroparse/parsers/python`](parsers/python/) | Python 3.14 |
| TypeScript | [`github.com/ornew/zeroparse/parsers/typescript`](parsers/typescript/) | TypeScript 5.9 and TSX |
| XML | [`github.com/ornew/zeroparse/parsers/xml`](parsers/xml/) | XML 1.0 (Fifth Edition), non-validating |
| YAML | [`github.com/ornew/zeroparse/parsers/yaml`](parsers/yaml/) | YAML 1.2.2 |

## Examples

Small grammars with tests that show the features live in [examples/](examples/): a calculator (Operators Rule and
left-recursive), an indentation-based outline format and a small programming language. The complete parsers of
[parsers/](parsers/), such as the ones for **Go** and **Python 3**, are the larger examples.

## Documentation

| | |
|:--|:--|
| [Why ZeroParse](docs/why-zeroparse.md) | What ZeroParse aims to be, what it offers today, its limits, and how it compares with other parser tools |
| [Tutorial](docs/tutorial/README.md) | Ten lessons from a first grammar to a shipped, tested and debugged parser |
| [Grammar ASTs and JSON](docs/guide/grammar-asts.md) | Constructing and validating grammars, structured error paths and JSON compatibility |
| [Guides](docs/guide/README.md) | Trees and actions, expressions, errors and recovery, context-sensitive parsing, running parsers, using the ready-made parsers, compiled grammars, code generation (Go and TypeScript), streaming, incremental parsing, debugging and profiling, linting grammars, testing grammars, sampling inputs and fuzzing, editor support |
| [Cookbook](docs/cookbook/README.md) | Short, complete recipes for concrete tasks: a configuration file into Go structs, a filter language, errors with a caret, several errors at once, parsing every keystroke, shipping a generated parser, testing against a grammar |
| [Editor support](docs/guide/editor-support.md) | `zp lsp` and the VS Code extension: errors as you type, formatting, navigation, hover with inferred types, rename, completion |
| [Playground](docs/guide/playground.md) | Try grammars in the browser, inspect parse results and use the WebAssembly API |
| [Website maintenance](docs/site.md) | Build, test and publish the documentation site and playground |
| [Language specification](spec/README.md) | The ZeroParse grammar language |
| [Development guide](docs/development.md) | Repository layout, architecture, language support and tests |
| [Releasing](docs/releasing.md) | What a release promises, supported platforms and Go versions, and how a release is tagged |
| [Bytecode specification](spec/bytecode/README.md) | Instruction set and VM semantics, for porting the runtime |
| [Benchmarks](docs/benchmarks.md) · [Performance](docs/performance.md) · [Optimization catalog](docs/optimizations/README.md) | How fast, and how it got there |
| [Design](docs/design/README.md) · [Design records](docs/design/records/) | How it is built, and the decisions and their rationale |
| [Project strategy](docs/strategy.md) | Target users, competitive position, tracks and measurable targets |

## Contributing

Bug reports, ideas and pull requests are welcome. Run `go test ./...` before sending changes, and follow the
[commit message guidelines](docs/commit-messages.md). Report security vulnerabilities privately, as the
[security policy](SECURITY.md) describes.

## License

[Apache License 2.0](LICENCE)
