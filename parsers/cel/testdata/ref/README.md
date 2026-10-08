# Reference results of cel-go

What the parser of [cel-go](https://github.com/google/cel-go) v0.32.0 (`cel.dev/cel-go`, Apache License 2.0) makes of
each expression, written by [internal/refgen](../../internal/refgen) (`go run . -spec /path/to/cel-spec -out
../../testdata/ref -deep 1000`; see the README of the module). The tests of the module compare the parser with these
files, so that they need no cel-go.

A line is the expression as a Go string literal, a tab, and the result: the AST in the canonical form of
`canon_test.go`, `ERR@line:column` of the first error, or for a file of verdicts (`deep.tsv`, `macros.tsv`) `OK` where
cel-go accepts it. `limits.tsv` has a shape of expression, a size and `ok` or `ERR` instead (see `limits_test.go`).

| File | Expressions |
|:--|:--|
| `conformance.tsv` | The `expr` of the conformance tests of cel-spec (commit 40a3c900, in `../cel-spec`), 2,263 different ones |
| `celgo.tsv` | The expressions in the tests of cel-go's parser (`parser_test.go`, `unparser_test.go`, and the strings of `unescape_test.go`, as strings and as bytes) |
| `edge.tsv` | Hand-picked inputs at the edges of the lexical syntax and the grammar (`refgen/edge.go`) |
| `random.tsv` | 12,000 random expressions and mutations of them (`refgen/gen.go`, seed 1) |
| `literals.tsv` | 4,000 random literals: strings and bytes of every quoting, prefix and escape, numbers |
| `deep.tsv` | 1,000 expressions that nest constructs around cel-go's limit of 250 levels; verdicts only |
| `macros.tsv` | 12,319 expressions, with calls of the standard macros and the others above; verdicts of cel-go with the standard macros |
| `limits.tsv` | For 30 shapes of expression, sizes around the first one that cel-go rejects (its depth of 250, its size of 100,000 code points) |

cel-go is configured with the optional syntax and escaped identifiers on, variadic logical operators (so that `a || b ||
c` is one call), its default limits, and no macros except for `macros.tsv`.
