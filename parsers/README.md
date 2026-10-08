# Ready-made parsers

Parsers for common languages, written in PEGO and generated into Go with `pego gen`. Each is a Go module of its own
that depends only on the standard library: import it and parse, with nothing to generate or compile at run time.

| Language | Module | Specification | Checked against |
|:--|:--|:--|:--|
| CSV | [`github.com/ornew/pego/parsers/csv`](csv/) | [RFC 4180](https://www.rfc-editor.org/rfc/rfc4180), with the leniencies of `encoding/csv` | 73 edge cases, `encoding/csv` (differential and fuzz tests) |
| JSON | [`github.com/ornew/pego/parsers/json`](json/) | [RFC 8259](https://www.rfc-editor.org/rfc/rfc8259) | [JSONTestSuite](https://github.com/nst/JSONTestSuite) (every `y_` and `n_` case), `encoding/json` (differential and fuzz tests) |
| XML | [`github.com/ornew/pego/parsers/xml`](xml/) | [XML 1.0 (Fifth Edition)](https://www.w3.org/TR/xml/), non-validating, with [Namespaces in XML 1.0](https://www.w3.org/TR/xml-names/) | [W3C XML Conformance Test Suite](https://www.w3.org/XML/Test/) (no failure among the XML 1.0 tests that need no external entity), `encoding/xml` (differential and fuzz tests) |

```bash
go get github.com/ornew/pego/parsers/json
```

```go
import "github.com/ornew/pego/parsers/json"

v, err := json.Decode(`{"name": "pego", "tags": ["peg", "pratt"]}`) // map[string]any{...}
ast, err := json.ParseAST(src)                                      // typed values with positions
```

Every parser has the API of a parser generated with `pego gen -types -recognize` (see
[code generation](../docs/guide/code-generation.md#the-generated-api)), plus functions written for its language:

| | |
|:--|:--|
| `ParseAST(input, unit...)` | The document as the Go types the grammar declares, each with its `Span` in the input |
| `Parse(input, unit...)` | The document as a tree of `*Node`, as the engine returns it |
| `Recognize(input, unit...)` | Only checks the input, without building anything |
| `*SyntaxError` | The position (`Line`, `Col`, `Pos`) and the expected tokens of a syntax error |

Positions are in code points by default; pass `Bytes` for byte offsets.

## Layout

Each directory holds:

| File | Contents |
|:--|:--|
| `<language>.pego` | The grammar. It can be used with the engine too (`pego.CompileSource`), for streaming, incremental parsing or the `pego` tools. |
| `parser.go` | The parser generated from the grammar. Do not edit it. |
| other `.go` files | Code written for the language: documentation, conversions to Go values, helpers |
| `*_test.go` | Tests against the language's specification, conformance suites and reference implementations; examples; benchmarks |
| `testdata/*.txt`, `*.golden` | Inputs and the expected trees and errors, checked on the generated parser here and on every backend of the engine by [parsers_test.go](parsers_test.go) |
| `go.mod`, `LICENCE` | The module |

## Development

```bash
go generate ./parsers                 # regenerate every parser.go (after changing a grammar or the generator)
go test ./parsers                     # parser.go up to date; golden files on every backend of the engine
go test ./parsers -update             # rewrite the golden files from the engine
parsers/test.sh                       # both, and the tests of every module
cd parsers/json && go test ./...      # the tests of one module
```

The generated `parser.go` declares unexported names of its runtime (such as `parse`, `parser` and `rule`) in the
package, so hand-written code must avoid them.

The generation commands are the `go:generate` lines of [generate.go](generate.go); the up-to-date test reads them, so a
new parser needs only a new line there. The modules are not part of the main module's `go test ./...`: run
`parsers/test.sh` after changing the engine or the generator.
