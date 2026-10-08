# Examples

Small grammars that show PEGO's features, with golden tests. Complete parsers for real languages, ready to import
(CEL, CSV, Go, JSON, Python, TypeScript, XML and YAML), are in [parsers/](../parsers/); they are the larger examples.

| Directory | Contents | Features shown |
|:--|:--|:--|
| [calculator](calculator/) | A calculator for the four arithmetic operations and exponentiation. `main.go` evaluates the AST. | Pratt expressions (`calc.pego`) compared with left recursion (`calc_lr.pego`); typed AST |
| [outline](outline/) | An outline whose hierarchy is expressed by indentation | Predicates and variables; captures in positive lookahead |
| [minilang](minilang/) | A small programming language with functions, control flow and expressions | PEG statements nested with Pratt expressions; keyword exclusion; calls without a level |

## Running

```bash
go run ./cmd/pego parse -g examples/minilang/minilang.pego -f sexpr < examples/minilang/testdata/fib.txt
go run ./examples/calculator "1 + 2 * (3 - 4) ^ 2"
```

## Testing

`examples_test.go` parses every `testdata/*.txt` file in each directory with every `.pego` grammar in that directory and compares the result with the corresponding `testdata/*.golden` file.
Grammars in the same directory (such as `calc.pego` and `calc_lr.pego`) must produce the same results. Where a result legitimately depends on how a grammar is written, such as the expectations in a syntax error, a per-grammar golden file `<input>.<grammar>.golden` (for example `error.calc_lr.golden`) overrides the shared one.

```bash
go test ./examples/...
go test ./examples -update   # regenerate the golden files (per-grammar files only where results differ)
```
