<div align="center">

# PEGO

**One Grammar. Every Parser.**

Today: Go and TypeScript targets, three Go engines and a published bytecode, all from one PEG-based grammar. The aim is
every target, engine, optimization profile and use, at the best measured speed.

[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENCE) [![Go Reference](https://pkg.go.dev/badge/github.com/ornew/pego.svg)](https://pkg.go.dev/github.com/ornew/pego) [![Go](https://img.shields.io/badge/go-1.27.1+-00ADD8?logo=go&logoColor=white)](go.mod) [![Netlify Status](https://api.netlify.com/api/v1/badges/29040d97-0646-4829-8871-188310aec03a/deploy-status)](https://app.netlify.com/projects/pego-go/deploys)

[Website](https://pego.ornew.net/) · [Playground](https://pego.ornew.net/playground/) · [Quick start](docs/tutorial/getting-started.md) · [Guides](docs/guide/README.md) · [Specification](spec/README.md) · [Examples](examples/)

</div>

A `.pego` grammar extends [Parsing Expression Grammars](https://en.wikipedia.org/wiki/Parsing_expression_grammar) with
types, operator precedence, left recursion and error recovery, so one file defines both the syntax and the values you
get back. PEGO is a grammar compiler and parsing platform designed for every target, engine and use: it checks and
compiles a grammar once, and every target and engine returns the same result for the features it supports.

**Today** PEGO generates Go and TypeScript and runs grammars on three engine backends in Go: a closure backend and two
VMs for a bytecode with a published specification. Only the Go VMs implement the bytecode, and it stays unstable until
it is declared stable and versioned, which the first tagged release does not do. One grammar gives trees, typed values,
recognition, declared recovery, an incremental `Document` and streaming, with per-target gaps. Generated Go needs only
the standard library, and generated TypeScript has no dependencies. Its best fit today is a Go program, alone or with a
TypeScript front end, that parses its own DSL, configuration, query or policy language.

**Next** come a tagged release with work limits on by default, a public cross-tool benchmark, faster parsers and
generation profiles for speed, memory and size, editor support for the languages you define, and `Document` and
streaming in TypeScript. More target languages and another implementation of the bytecode follow after the release.

**Speed** is a pillar: the aim is the best measured speed in every target and use, against [published
targets](docs/why-pego.md#commitments-and-targets). Today generated Go is slower than Go's hand-written standard-library
parsers on every comparable workload: it takes 1.1–5.4× their time for typed values and 1.6–6.8× for trees, from JSON
(3.89 ms against 3.50 ms for `encoding/json` `Unmarshal` into `any`, which records no positions) to left-recursive
arithmetic (see the [benchmarks](docs/benchmarks.md)).

**Not yet** released or safe by default. There is no tagged release, and the language, the API and file formats may
change. For untrusted input, only the nesting cap is on: work limits are opt-in, there is no memory limit, and no
run-time bound holds for every grammar. [Why PEGO](docs/why-pego.md) pairs each aim with its status today, compares
PEGO with other tools and lists the current limits.

```pego
// calc.pego
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
pego parse -g calc.pego -f sexpr -i '1 + 2 * 3 ^ 2'
```

```text
Bin{Left: Num("1"), Op: "+", Right: Bin{Left: Num("2"), Op: "*", Right: Bin{Left: Num("3"), Op: "^", Right: Num("2")}}}
```

```bash
pego parse -g calc.pego -i '1 + * 2'
```

```text
pego: 1:5: syntax error: expected "(", [0-9]
```

```go
p, _ := pego.CompileSource(src, "main")
tree, err := p.Parse("1 + 2 * 3 ^ 2") // a *pego.Node, or a *pego.SyntaxError with line, column and expectations
```

> [!NOTE]
> PEGO is pre-release: the language, the Go API and the file formats may change between versions.
> See the [implementation status](docs/development.md) and the
> [living backlog](https://github.com/ornew/pego/issues/1).

## Why PEGO

- **Typed values from the grammar.** Declare struct and join types and build them in actions. Rule types are
  inferred and checked when the grammar compiles. `pego gen -types` emits matching Go values directly, and generated
  TypeScript converts its tree with `parseAST` (see the [code generation guide](docs/guide/generated-typed-values.md)).
- **Errors that point the way.** Failures report the farthest position and what was expected there. `@error`
  rewrites the message, and `#recover` skips the broken part and keeps going, where the grammar declares it.
- **Reparse after an edit.** A `Document` keeps the rule results whose examined input an edit did not reach; how
  much that is depends on how the grammar splits into rules (see
  [incremental reuse](docs/guide/incremental-reuse.md)). On the Minilang workload, an 89 KB program edited by one
  character reparses in 116–133 µs against 15.6–24.5 ms for a full parse, on the engine backends (see the
  [benchmarks](docs/benchmarks.md)).
- **Long input, bounded memory.** `ParseStream` hands over each element of a `#stream` repetition as it matches and
  drops the input behind it, so the input and tree memory it keeps depend on the size of one element, not on the
  length of the input. The start rule needs a top-level `#stream` repetition, and recovered errors are kept unless
  you set a limit (see the [streaming guide](docs/guide/streaming.md)).
- **Go and TypeScript today, more targets as the aim.** `pego gen` emits a standalone parser: Go that needs only the
  standard library, or a TypeScript module with no dependencies. The repository tests run the TypeScript under Node.js;
  Deno, Bun and browsers are documented but not tested. Generated TypeScript has no `Document` or streaming (see the
  [TypeScript guide](docs/guide/typescript.md)).
- **A published bytecode.** Grammars also compile to a bytecode with a [published
  specification](spec/bytecode/README.md), implemented today by PEGO's Go VMs and unstable until it is declared stable
  and versioned. A precompiled `.pegoc` file of the JSON or Minilang grammar is ready to parse in 35.5–135 µs.
- **Every backend, one answer.** For the features they share, the test suite checks that the closure backend, both
  bytecode VMs, generated Go and generated TypeScript return the same trees, values, positions and errors.
- **Speed as a goal, measured in the open.** The aim is to match, then beat, hand-written parsers while returning typed
  values. Today generated parsers take 1.1–5.4× the time of the Go standard library's hand-written parsers for typed
  values and 1.6–6.8× for trees on PEGO's benchmarks, which do different work (`encoding/json` builds no positions,
  `encoding/xml` returns tokens, `encoding/csv` returns records, `go/parser` builds a positioned Go syntax tree); a
  recognizer validates JSON with zero allocations (see the [benchmarks](docs/benchmarks.md) and the
  [targets](docs/why-pego.md#commitments-and-targets)).
- **Limits you can set.** Nested rule calls are capped by default: 100,000 on the closure backend, the recursive VM
  and generated Go, and 10,000,000 on the iterative VM, which runs on its own stack
  ([deep nesting](docs/guide/backends.md#deep-nesting-and-withmaxdepth)). Work limits and cancellation are opt-in;
  a generated parser carries them when it is generated with `-work-controls`
  ([Cancellation and Work Limits](docs/guide/cancellation-and-limits.md)).
- **Precedence without the pain.** Operators are declared by binding level in an Operators Rule, not a ladder of
  rules. Left recursion, direct or indirect, works.
- **See what your grammar does.** `pego trace` shows every rule call with positions, results and memo hits,
  `pego explain` which calls make up a syntax error, and `pego profile` where the time and the backtracking go.
  `pego lint` points out alternatives that can never match and other likely mistakes before any input does.
- **Editor support for grammars.** `pego lsp` gives any LSP editor errors as you type in `.pego` files, formatting,
  go to definition, hover with inferred rule types, rename and completion; a VS Code extension is included.
- **Context when you need it.** Predicates and scoped variables handle indentation, matching tags and other things
  plain PEG cannot.

## Install

```bash
go get github.com/ornew/pego                        # library
go install github.com/ornew/pego/cmd/pego@latest    # command-line tool
```

Go 1.27.1 or later. No dependencies outside the standard library.

## Use It

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
node, err = doc.Parse()      // reuses rule results the edit did not reach

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
pego fix -w grammar.pego                         # rewrite older syntax to the current one
pego compile -g grammar.pego -o grammar.pegoc    # precompile
pego gen -g grammar.pego -module example.com/calc -o calc  # generate a standalone Go parser
pego gen -g grammar.pego -module example.com/calc -types -o calc # ... with Go types for the grammar's types
pego gen -g grammar.pego -module example.com/calc -recognize -o calc # ... with Recognize, which validates without a tree
pego gen -lang ts -g grammar.pego -o ts-parser    # generate a TypeScript parser into a directory
pego gen -lang ts -g grammar.pego -types -o ts-parser # also emit types.ts and parseAST
pego gen -g grammar.pego -module example.com/calc -max-depth 600000 -o calc # set its nested rule-call default
pego sample -g grammar.pego -n 20 -coverage      # generate inputs the grammar accepts (tests, fuzz seeds)
pego edit-fuzz -g grammar.pego -minimize -save-failures out  # check Document edits against fresh parses
```

Run `pego` without arguments for every command and flag.

## Ready-Made Parsers

[parsers/](parsers/) has parsers for common languages, written in PEGO, generated into Go and tested against the
languages' conformance suites and reference implementations. Each is a module of its own that depends only on the
standard library and PEGO's runtime package. Until PEGO publishes a release, use them from a checkout of the
repository; `go get` does not fetch them from another module yet (see [Ready-Made Parsers](docs/guide/ready-made-parsers.md)):

```go
import "github.com/ornew/pego/parsers/json"

v, err := json.Decode(`{"name": "pego", "tags": ["peg", "operators-rule"]}`)
```

| Language | Module | Specification |
|:--|:--|:--|
| CEL | [`github.com/ornew/pego/parsers/cel`](parsers/cel/) | The CEL language definition |
| CSV | [`github.com/ornew/pego/parsers/csv`](parsers/csv/) | RFC 4180 |
| CUE | [`github.com/ornew/pego/parsers/cue`](parsers/cue/) | The CUE language specification |
| DuckDB SQL | [`github.com/ornew/pego/parsers/duckdb`](parsers/duckdb/) | DuckDB 1.5.6's SQL dialect |
| Go | [`github.com/ornew/pego/parsers/golang`](parsers/golang/) | The Go specification (go1.27), as `go/parser` parses it |
| JSON | [`github.com/ornew/pego/parsers/json`](parsers/json/) | RFC 8259 |
| Python | [`github.com/ornew/pego/parsers/python`](parsers/python/) | Python 3.14 |
| TypeScript | [`github.com/ornew/pego/parsers/typescript`](parsers/typescript/) | TypeScript 5.9 and TSX |
| XML | [`github.com/ornew/pego/parsers/xml`](parsers/xml/) | XML 1.0 (Fifth Edition), non-validating |
| YAML | [`github.com/ornew/pego/parsers/yaml`](parsers/yaml/) | YAML 1.2.2 |

## Examples

Small grammars with tests that show the features live in [examples/](examples/): a calculator (Operators Rule and
left-recursive), an indentation-based outline format and a small programming language. The complete parsers of
[parsers/](parsers/), such as the ones for **Go** and **Python 3**, are the larger examples.

## Documentation

| | |
|:--|:--|
| [Why PEGO](docs/why-pego.md) | What PEGO aims to be, what it offers today, its limits, and how it compares with other parser tools |
| [Tutorial](docs/tutorial/README.md) | Ten lessons from a first grammar to a shipped, tested and debugged parser |
| [Grammar ASTs and JSON](docs/guide/grammar-asts.md) | Constructing and validating grammars, structured error paths and JSON compatibility |
| [Guides](docs/guide/README.md) | Trees and actions, expressions, errors and recovery, context-sensitive parsing, running parsers, using the ready-made parsers, compiled grammars, code generation (Go and TypeScript), streaming, incremental parsing, debugging and profiling, linting grammars, testing grammars, sampling inputs and fuzzing, editor support |
| [Cookbook](docs/cookbook/README.md) | Short, complete recipes for concrete tasks: a configuration file into Go structs, a filter language, errors with a caret, several errors at once, parsing every keystroke, shipping a generated parser, testing against a grammar |
| [Editor support](docs/guide/editor-support.md) | `pego lsp` and the VS Code extension: errors as you type, formatting, navigation, hover with inferred types, rename, completion |
| [Playground](docs/guide/playground.md) | Try grammars in the browser, inspect parse results and use the WebAssembly API |
| [Website maintenance](docs/site.md) | Build, test and publish the documentation site and playground |
| [Language specification](spec/README.md) | The PEGO grammar language |
| [Development guide](docs/development.md) | Repository layout, architecture, language support and tests |
| [Bytecode specification](spec/bytecode/README.md) | Instruction set and VM semantics, for porting the runtime |
| [Benchmarks](docs/benchmarks.md) · [Performance](docs/performance.md) · [Optimization catalog](docs/optimizations/README.md) | How fast, and how it got there |
| [Design](docs/design/README.md) · [Design records](docs/design/records/) | How it is built, and the decisions and their rationale |
| [Project strategy](docs/strategy.md) | Target users, competitive position, tracks and measurable targets |

## Contributing

Bug reports, ideas and pull requests are welcome. Run `go test ./...` before sending changes, and follow the
[commit message guidelines](docs/commit-messages.md).

## License

[Apache License 2.0](LICENCE)
