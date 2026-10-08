# Examples

Grammars that show PEGO's features, with golden tests. Complete parsers for real languages, ready to import, are in
[parsers/](../parsers/).

| Directory | Contents | Features shown |
|:--|:--|:--|
| [calculator](calculator/) | A calculator for the four arithmetic operations and exponentiation. `main.go` evaluates the AST. | Pratt expressions (`calc.pego`) compared with left recursion (`calc_lr.pego`); typed AST |
| [outline](outline/) | An outline whose hierarchy is expressed by indentation | Predicates and variables; captures in positive lookahead |
| [csv](csv/) | CSV (RFC 4180). Records can be consumed one at a time. | Quoted fields; `#stream` |
| [xml](xml/) | A subset of XML (elements, attributes, character data, comments, CDATA) | Matching start-tag and end-tag names with a predicate; `#error` |
| [minilang](minilang/) | A small programming language with functions, control flow and expressions | PEG statements nested with Pratt expressions; keyword exclusion; calls without a level |
| [golang](golang/) | Go source files, including generics. `golang_test.go` parses Go files from this repository and the standard library and compares the trees with `go/ast`. | Automatic semicolon insertion; resolving the composite-literal ambiguity in statement headers; typed AST modeled on `go/ast`; `#recover` and `#error` |
| [python](python/) | A realistic subset of Python 3 (all statements except `match`, decorators, full parameter lists, comprehensions, f-strings). `python_test.go` parses modules of the installed Python standard library and compares node counts with Python's `ast`. | Indentation (INDENT/DEDENT) and implicit line joining with predicates and variables; Python's precedence as a Pratt expression; choosing the node kind by folding over optional tails instead of backtracking; typed AST modeled on `ast`; `#recover` and `#error` |

## Running

```bash
go run ./cmd/pego parse -g examples/minilang/minilang.pego -f sexpr < examples/minilang/testdata/fib.txt
go run ./examples/calculator "1 + 2 * (3 - 4) ^ 2"
go run ./cmd/pego parse -g examples/golang/go.pego -f sexpr < pego.go
go run ./cmd/pego parse -g examples/python/python.pego -f sexpr < examples/python/testdata/module.txt
```

## Testing

`examples_test.go` parses every `testdata/*.txt` file in each directory with every `.pego` grammar in that directory and compares the result with the corresponding `testdata/*.golden` file.
Grammars in the same directory (such as `calc.pego` and `calc_lr.pego`) must produce the same results. Where a result legitimately depends on how a grammar is written, such as the expectations in a syntax error, a per-grammar golden file `<input>.<grammar>.golden` (for example `error.calc_lr.golden`) overrides the shared one.

`golang/golang_test.go` parses real Go files (`-short` skips the generated parsers and the larger list of standard library files).
`python/python_test.go` parses Python snippets and, when `python3` is installed, a list of standard library modules (`PEGO_PYTHON_SURVEY=1` surveys every module).

```bash
go test ./examples/...
go test ./examples -update   # regenerate the golden files (per-grammar files only where results differ)
```
