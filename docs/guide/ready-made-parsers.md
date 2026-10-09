# Using the Ready-Made Parsers

[parsers/](../../parsers/README.md) holds complete parsers for ten languages, written in PEGO and generated into Go:
CEL, CSV, CUE, DuckDB SQL, Go, JSON, Python, TypeScript, XML and YAML. You import one, call a function and get a tree
or an error, with nothing to compile or generate at run time. This guide is for the program that uses them: how to
pick an entry point, how to read the tree, what the positions mean, how errors look, what the limits are, and how to
get at the grammar when you want more than the Go API offers. The README of each parser lists its functions and where
it differs from the reference implementation; this guide covers what they have in common.

- [Install](#install)
- [What every parser offers](#what-every-parser-offers)
- [Choosing an entry point](#choosing-an-entry-point)
- [Reading the tree](#reading-the-tree)
- [Positions](#positions)
- [Errors](#errors)
- [Conformance and limits](#conformance-and-limits)
- [Using the grammar with the engine](#using-the-grammar-with-the-engine)
- [When to write your own](#when-to-write-your-own)

## Install

Each parser is a Go module of its own. It depends only on the standard library, not on PEGO and not on the language's
own implementation (DuckDB's parser does not need DuckDB):

```bash
go get github.com/ornew/pego/parsers/json
```

```go
import "github.com/ornew/pego/parsers/json"
```

The parsers are generated code, and large ones are very large: `parsers/json/parser.go` is 200,876 bytes,
`parsers/python/parser.go` 3,948,176 bytes and `parsers/duckdb/parser.go` 13,216,774 bytes. They compile once and are
cached, but the first build of a big parser takes a while: the DuckDB program below took about 80 seconds to build on a
cold build cache (with other builds running), the Go one about 11. They are the output of the
[code generator](code-generation.md), so what is said there about generated code (its size, its diffs) applies.

## What every parser offers

All ten have the API of a parser generated with `pego gen -types -recognize`
([code generation](code-generation.md#the-generated-api)):

| | |
|:--|:--|
| `ParseAST(input, unit...)` | The document as the Go types the grammar declares, each with its `Span` in the input |
| `Parse(input, unit...)` | The document as a tree of `*Node`, as the engine returns it |
| `Recognize(input, unit...)` | Only checks the input; returns the error, builds nothing |
| `*SyntaxError` | The position (`Line`, `Col`, `Pos`) and the expected tokens of a syntax error |
| `Span`, `Unit`, `Bytes`, `CodePoints` | Positions: the start and end of a node, and the unit they are counted in |

On top of that each parser has functions written for its language: the decoded values, the checks the language
reference does after parsing, helpers to walk the tree. The entry points you normally call:

| Language | Package | You normally call |
|:--|:--|:--|
| CEL | `parsers/cel` | `ParseExpr` (parse and check, as cel-go does), `Format`, `Inspect`, `Constant`; `Valid` |
| CSV | `parsers/csv` | `Records`, `Table` (a header and rows), `ParseAST`; `Valid` |
| CUE | `parsers/cue` | `ParseFile`, `Comments`, `Inspect`; `Valid` |
| DuckDB SQL | `parsers/duckdb` | `ParseAST`, `Split` (the statements of a script, with their kind), `Walk`, `Keyword`, `QuoteIdent` |
| Go | `parsers/golang` | `ParseFile` (a `go/ast` tree, as `go/parser` returns it), `ParseAST`, `ToGoAST`; `Valid` |
| JSON | `parsers/json` | `Decode` (Go values), `ParseAST`; `Valid` |
| Python | `parsers/python` | `ParseModule` (parse and check, as `ast.parse` does), `Dump`, `Inspect` |
| TypeScript | `parsers/typescript` | `ParseFile`, `ParseTSX`, `Inspect`, `ForEachChild` |
| XML | `parsers/xml` | `Decode`, `DecodeBytes` (any of the encodings XML allows), `WellFormed` |
| YAML | `parsers/yaml` | `Load`, `LoadAll` (Go values), `ParseAST`, `Events`; `Valid` |

For example, a table and a document as Go values, and an error of the table:

```go
package main

import (
	"errors"
	"fmt"

	"github.com/ornew/pego/parsers/csv"
	"github.com/ornew/pego/parsers/yaml"
)

func main() {
	header, rows, err := csv.Table("id,name\n1,alice\n2,\"bob, jr\"\n")
	fmt.Println(header, rows, err)

	v, err := yaml.Load("name: pego\ntags: [peg, pratt]\nstars: 42\n")
	fmt.Println(v, err)

	_, _, err = csv.Table("id,name\n1\n")
	var fe *csv.FieldCountError
	fmt.Println(errors.As(err, &fe), err)
}
```

```
[id name] [[1 alice] [2 bob, jr]] <nil>
map[name:pego stars:42 tags:[peg pratt]] <nil>
true csv: line 2: record has 1 fields, the header has 2
```

(The last error is not a syntax error: the input is valid CSV, and `Table` adds the rule that every record has as many
fields as the header. See [Errors](#errors).)

## Choosing an entry point

There are four kinds of entry point, and the same input costs different amounts in each:

| Entry point | You get | Use it when |
|:--|:--|:--|
| The language's own (`Decode`, `Load`, `ParseFile`, `ParseModule`, `ParseExpr`, `Records`) | Values or a tree as the language's tools produce them, after the checks that the language's reference does and that are not part of its grammar | You want what the reference implementation would give you: it accepts what the reference accepts |
| `ParseAST` | The grammar's own typed values, with positions | You analyze or transform the source and need to know where everything is, or the language has no value form (SQL, TypeScript) |
| `Parse` | The generic `*Node` tree, as the engine builds it | You work with several PEGO grammars at once, or compare with the engine ([testing](testing-grammars.md)) |
| `Recognize`, `Valid` | Only whether the input is valid | You validate input and do not need the tree |

Note the first row: `ParseAST` is the grammar alone. The language's own function also runs the checks that a grammar
cannot express (the range of a literal, the limits of CEL, the `from __future__` rules of Python), so the two can
differ on inputs that are syntactically right and semantically wrong. The README of each parser lists them.

What each costs, for JSON, on an array of objects of 262 KB (the parser's own benchmarks, Apple M3 Max, Go 1.27.1;
`go test -bench . -benchmem` in `parsers/json` measures them again):

| | Time | Allocated | Allocations |
|:--|--:|--:|--:|
| `ParseAST` | 3.2 ms | 2.4 MB | 233 |
| `Decode` (`ParseAST` and a conversion to `map[string]any`) | 4.3 ms | 5.2 MB | 24,648 |
| `Valid` | 3.5 ms | 6.9 KB | 0 |
| `Parse` | 7.1 ms | 10.5 MB | 744 |
| `encoding/json` `Unmarshal` into an `any` (for comparison) | 6.4 ms | 1.9 MB | 54,357 |
| `encoding/json` `Valid` (for comparison) | 0.31 ms | 0 | 0 |

Two lessons from this table. The typed tree (`ParseAST`) is cheaper than the generic one (`Parse`): twice as fast, with
a quarter of the memory. Prefer it. And `Valid` (`Recognize`) builds nothing, but it does not make a parser as fast as a
hand-written scanner: here it takes about as long as `ParseAST` and is 11 times slower than `encoding/json`'s `Valid`.
Use it to avoid allocating, not to save time. These are the numbers of one grammar and one input; measure yours.
How these compare with the engine's backends is in [benchmarks.md](../benchmarks.md) and
[performance.md](../performance.md#where-pego-stands).

## Reading the tree

`ParseAST` returns Go types: a struct for each type of the grammar, and an interface for each union. Walk them with
type switches, and ask the node for its `Span`. A small program that reports every `port` member of a JSON document that
is not a number, at the line and column where it starts:

```go
// jsoncheck reports every "port" member of a JSON document whose value is not a number, with its position.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/ornew/pego/parsers/json"
)

// lineCol converts an offset in code points to a 1-based line and column.
func lineCol(src string, pos int) (line, col int) {
	line, col = 1, 1
	for i, r := range []rune(src) {
		if i == pos {
			break
		}
		if r == '\n' {
			line, col = line+1, 1
		} else {
			col++
		}
	}
	return line, col
}

func check(src string) error {
	v, err := json.ParseAST(src)
	if err != nil {
		var se *json.SyntaxError
		if errors.As(err, &se) {
			return fmt.Errorf("%d:%d: %s", se.Line, se.Col, se.Message())
		}
		return err
	}
	// Visit every object, whatever its depth.
	var walk func(json.Value)
	walk = func(v json.Value) {
		switch v := v.(type) {
		case *json.Object:
			for _, m := range v.Members {
				if _, isNumber := m.Value.(*json.Number); m.Key.Value() == "port" && !isNumber {
					line, col := lineCol(src, int(m.Span.Start))
					fmt.Printf("%d:%d: %q must be a number\n", line, col, m.Key.Value())
				}
				walk(m.Value)
			}
		case *json.Array:
			for _, e := range v.Elements {
				walk(e)
			}
		}
	}
	walk(v)
	return nil
}

func main() {
	for _, src := range []string{
		"{\n  \"名前\": \"web\",\n  \"server\": {\"port\": \"8080\"},\n  \"replicas\": [{\"port\": 9090}, {\"port\": null}]\n}",
		"{\n  \"名前\": \"web\",\n  \"port\": 80,\n}",
	} {
		if err := check(src); err != nil {
			fmt.Fprintln(os.Stdout, "error:", err)
		}
	}
}
```

```
3:14: "port" must be a number
4:33: "port" must be a number
error: 4:1: syntax error: expected "\"", (? \t\r\n)
```

The second document has a trailing comma, so `ParseAST` fails and `check` returns the `SyntaxError`'s position and
message.

The `Value()` method of `*json.String` decodes the escapes of the source text, which `Text` keeps as written. The other
parsers have methods of this kind for their literals (`StringLit.Value()`, `Number.Int64()`, `Constant.Value()`). Most
also have a function that visits every node in source order: `duckdb.Walk`, `python.Inspect`, `cel.Inspect`,
`cue.Inspect` and `typescript.Inspect` take a function that returns `false` to skip a node's children. Use them rather
than recursing by hand when the tree is large.

The Go parser can also give you the tree of the standard library's `go/ast`, which every Go tool understands:

```go
// gofuncs lists the exported functions of a Go source with go/ast, parsed by parsers/golang.
package main

import (
	"fmt"
	"go/ast"
	"go/token"

	"github.com/ornew/pego/parsers/golang"
)

const src = `package shapes

import "math"

// Area returns the area of a circle.
func Area(r float64) float64 { return math.Pi * r * r }

type Circle struct{ R float64 }

func (c Circle) Perimeter() float64 { return 2 * math.Pi * c.R }

func helper() {}
`

func main() {
	fset := token.NewFileSet()
	f, err := golang.ParseFile(fset, "shapes.go", []byte(src), golang.ParseComments)
	if err != nil {
		panic(err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if fn, ok := n.(*ast.FuncDecl); ok && fn.Name.IsExported() {
			fmt.Printf("%s: %s\n", fset.Position(fn.Pos()), fn.Name.Name)
		}
		return true
	})

	// The same file as the typed values of the grammar, each with its Span (here in bytes).
	file, err := golang.ParseAST(src, golang.Bytes)
	if err != nil {
		panic(err)
	}
	for _, d := range file.Decls {
		if fn, ok := d.(*golang.FuncDecl); ok {
			fmt.Println(fn.Name.Text, fn.Start, fn.End)
		}
	}

	_, err = golang.ParseFile(fset, "bad.go", []byte("package p\nfunc f( {}\n"), 0)
	fmt.Println(err)
}
```

```
shapes.go:6:1: Area
shapes.go:10:1: Perimeter
Area 69 124
Perimeter 159 223
helper 225 241
bad.go:2:9: expected type
```

`ParseFile` takes the same arguments as `go/parser.ParseFile` and returns the same tree, positions included, so `go/printer`,
`go/format` and `go/types` work on it; its errors are `scanner.ErrorList`s, as `go/parser`'s are. `ParseAST` returns the
typed values of the grammar instead, with spans in the unit you ask for (here bytes).

A last example, a DuckDB script cut into statements, with the tables each one reads:

```go
// sqltables lists the statements of a DuckDB script and the tables each one reads.
package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/ornew/pego/parsers/duckdb"
)

const script = `
CREATE TABLE totals AS SELECT region, sum(amount) AS total FROM sales GROUP BY ALL;
FROM totals JOIN regions USING (region) SELECT * EXCLUDE (total) WHERE total > 100;
INSERT INTO log VALUES (1, 'done')`

func main() {
	statements, err := duckdb.Split(script)
	if err != nil {
		panic(err)
	}
	for _, s := range statements {
		tables := map[string]bool{}
		duckdb.Walk(s.Statement, func(n any) bool {
			if t, ok := n.(*duckdb.BaseTable); ok {
				tables[strings.Join(t.Name.Names(), ".")] = true
			}
			return true
		})
		fmt.Printf("%-6s bytes %3d-%3d  reads %v\n", duckdb.Kind(s.Statement), s.Start, s.End, slices.Sorted(maps.Keys(tables)))
	}
}
```

```
CREATE bytes   0- 83  reads [sales]
SELECT bytes  84-167  reads [regions totals]
INSERT bytes 168-203  reads []
```

`Split` cuts the script as DuckDB does (it needs no running DuckDB), `Kind` gives each statement's type, and `Walk`
visits every node of a statement, so a `*duckdb.BaseTable` is found wherever it occurs: in a `FROM`, a `JOIN`, a
subquery. (The target of an `INSERT` is not a `BaseTable`, so the third statement reads nothing.)

## Positions

Every node has a `Span` with `Start` and `End`, a half-open range of the input. Positions are in **code points** by
default; pass `Bytes` for byte offsets, which is what you want when you slice the input as a Go string or when another
tool counts bytes (`ToGoAST` of the Go parser needs bytes). The unit is an optional last argument of `ParseAST`, `Parse` and
`Recognize`, and it applies to `Span`, to `SyntaxError.Pos` and to `Col` alike (`Line` is the same in both):

```go
package main

import (
	"errors"
	"fmt"

	"github.com/ornew/pego/parsers/json"
)

func main() {
	const src = `["あい", 1, x]`
	for _, u := range []struct {
		name string
		unit json.Unit
	}{{"code points", json.CodePoints}, {"bytes", json.Bytes}} {
		unit := u.unit
		_, err := json.ParseAST(src, unit)
		var se *json.SyntaxError
		if errors.As(err, &se) {
			fmt.Printf("%-11s error at pos %d, line %d, col %d\n", u.name, se.Pos, se.Line, se.Col)
		}
		v, _ := json.ParseAST(`["あい", 1]`, unit)
		last := v.(*json.Array).Elements[1]
		fmt.Printf("%-11s the number 1 is at %d-%d\n", u.name, last.(*json.Number).Start, last.(*json.Number).End)
	}
}
```

```
code points error at pos 10, line 1, col 11
code points the number 1 is at 7-8
bytes       error at pos 14, line 1, col 15
bytes       the number 1 is at 11-12
```

Whatever the unit, a node tells you where it is as an offset; you turn it into a line and column yourself, as `lineCol`
does in the program above (it counts code points; with `Bytes`, walk the string with `utf8.DecodeRuneInString`). The
parsers of languages that have a position convention follow it: the Go parser fills a `token.FileSet`, and the Python
parser has a `LineIndex` and prints positions as `ast.dump` does.

Do not mix units: if you parse with `Bytes` and report to an editor that counts UTF-16 units, convert at the border. See
[position units](runtime.md#position-units) for how the engine defines them.

## Errors

A parse that fails returns a `*SyntaxError` of the parser's own package (not `pego.SyntaxError`: the parsers do not
import PEGO). Use `errors.As`:

```go
_, err := json.ParseAST(`{"a": [1, 2,]}`)
var se *json.SyntaxError
if errors.As(err, &se) {
	fmt.Println(se.Line, se.Col, se.Message())
	// 1 13 syntax error: expected "-", "0", "[", "\"", "false", "null", "true", "{", (? \t\r\n), (?1-9)
}
```

`Line` and `Col` are 1-based, `Pos` is the offset, `Expected` lists the tokens that would have been accepted at the
farthest position reached, and `Message()` is the text without the position. These are the same fields as the engine's
(see [errors and recovery](errors-and-recovery.md)); the parsers report the first error and do not recover from it.

Some parsers return other errors as well, for what the grammar cannot decide: `yaml.SemanticError` (anchors, tags,
`Load`), `csv.FieldCountError` (a record of `Table` with another number of fields than the header), and the errors of the
checks of CEL, Python and CUE. Tell them from syntax errors with `errors.As`, as above.

The messages are made from the grammar, so they read like `expected "\"", (? \t\r\n)` rather than like a compiler's: good
enough to point at the place, and not always good enough to show a user as they are. Show `Line`, `Col` and the line of
the input, and treat `Expected` as a hint.

## Conformance and limits

A ready-made parser aims to accept what a named reference implementation accepts and to build what it builds, and each
README says how that was checked and where the two differ (leniencies and deviations are listed, not hidden):

- JSON follows RFC 8259 and, where the RFC leaves a choice, `encoding/json`; the tests run JSONTestSuite, and compare
  `Decode` with `encoding/json` on random and fuzzed input.
- Go accepts exactly the files `go/parser` accepts and builds the same tree, checked on every file of `GOROOT/src`.
- Python is checked against CPython's `ast.dump` on the standard library and its test suite; TypeScript against the
  compiler's trees on its own test cases; DuckDB against DuckDB on 158,605 texts, for acceptance and statement splitting.

The table of [parsers/README.md](../../parsers/README.md) has the specification and the suite for each. Where your
program depends on a subtle behavior, read the README's list of deviations first.

Limits to plan for:

- **Nesting.** The generated parsers limit the depth of rule calls to 100,000, so deeply nested input fails with
  `nesting too deep: more than 100000 rule calls` instead of exhausting the stack. For JSON that is about 33,000 levels
  of arrays. The limit is fixed in generated code ([what is supported](code-generation.md#what-is-supported)); to accept
  deeper input or to set a lower limit, use the grammar with the engine, whose `BytecodeIterative` backend and
  `WithMaxDepth` option are made for it ([runtime guide](runtime.md#deep-nesting-and-withmaxdepth)).
- **Memory.** A tree holds every node of the document. For input of many megabytes, parse in pieces yourself (a log
  file by lines, a JSON stream by documents), or use the engine's streaming below.
- **No recovery.** A parse stops at the first syntax error. For an editor, which needs a tree of the text as it is being
  typed, use the grammar with the engine and `#recover` ([errors and recovery](errors-and-recovery.md)).

## Using the grammar with the engine

The generated Go code gives up what the engine offers: streaming, incremental reparsing, the choice of backend and
the tools (`pego trace`, `pego profile`, `pego sample`). The grammar each parser is generated from, `<language>.pego`, is
in its module, so you can use it with the engine without copying it. Find it in the module cache:

```bash
go list -m -f '{{.Dir}}' github.com/ornew/pego/parsers/csv    # the directory of the module, with csv.pego in it
```

and give it to `pego parse`, or read it in Go and compile it with `pego.CompileSource`. The CSV grammar marks its
records with `#stream`, so a file larger than memory can be read one record at a time:

```bash
$ csv=$(go list -m -f '{{.Dir}}' github.com/ornew/pego/parsers/csv)/csv.pego
$ printf 'id,name\n1,alice\n2,"bob, jr"\n' | pego parse -g "$csv" -stream -f sexpr
(Record Fields=[Field"id"@field Field"name"@field])
(Record Fields=[Field"1"@field Field"alice"@field])
(Record Fields=[Field"2"@field Field"\"bob, jr\""@field])
```

The same grammar works with `NewDocument` for incremental parsing of an edited text
([incremental parsing](incremental.md)), and `pego sample -g csv.pego` generates inputs for tests
([testing grammars](testing-grammars.md)). The engine gives the same trees as the generated parser, because the parser
is generated from the same grammar and tested against it on every backend
([parsers/README.md](../../parsers/README.md#development)). The price is the engine's speed against generated code
([code generation](code-generation.md#why-generate)) and a dependency on `github.com/ornew/pego`.

## When to write your own

If a ready-made parser matches what you need, use it: it has been checked against the language's reference on far more
input than you will write tests for. Write your own grammar when the language is not here, or when you need a variant:
a subset, a dialect, an extra construct, a tree shaped for your program. The ready-made grammars are also the best
examples of large PEGO grammars: [parsers/json/json.pego](../../parsers/json/json.pego) is 41 lines,
[parsers/csv/csv.pego](../../parsers/csv/csv.pego) 28, and you can copy one and change it (each module carries its own
licence file). The [getting-started tutorial](../tutorial/getting-started.md) writes a first grammar, and
[code generation](code-generation.md) turns it into a package like these.

## See also

- [parsers/README.md](../../parsers/README.md): the list of parsers with the specification and the checks of each.
- [Code generation](code-generation.md): how these parsers are made and what their API is.
- [Errors and recovery](errors-and-recovery.md), [running parsers](runtime.md) and
  [testing grammars](testing-grammars.md).
