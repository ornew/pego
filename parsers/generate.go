// Package parsers generates and checks the parsers in its subdirectories, each a Go module of its own
// (github.com/ornew/zeroparse/parsers/<language>) that depends only on the standard library and the ZeroParse runtime (github.com/ornew/zeroparse/runtime).
//
// Each subdirectory holds the grammar (<language>.zp), the parser generated from it (parser.go, api.go, and types/types.go; they import the runtime), code
// written by hand around the generated API, and tests against the language's reference implementation
// or conformance suite. The tests of this package check that every generated file is up to date and that
// every backend of the engine gives the results recorded in the subdirectories' golden files, which the
// generated parsers are checked against in their own modules. Regenerate the parsers with
// go generate ./parsers, and test a module with cd parsers/<language> && go test ./... (or
// parsers/test.sh for all of them).
package parsers

//go:generate go run ../cmd/zp gen -g cel/cel.zp -module github.com/ornew/zeroparse/parsers/cel -types -typed-only -nodoc -runtime import -o cel
//go:generate go run ../cmd/zp gen -g csv/csv.zp -module github.com/ornew/zeroparse/parsers/csv -types -recognize -budget-controls -nodoc -runtime import -o csv
//go:generate go run ../cmd/zp gen -g cue/cue.zp -module github.com/ornew/zeroparse/parsers/cue -types -typed-only -nodoc -runtime import -o cue
//go:generate go run ../cmd/zp gen -g duckdb/duckdb.zp -module github.com/ornew/zeroparse/parsers/duckdb -types -typed-only -nodoc -runtime import -o duckdb
//go:generate go run ../cmd/zp gen -g golang/golang.zp -expectations -module github.com/ornew/zeroparse/parsers/golang -types -typed-only -nodoc -runtime import -o golang
//go:generate go run ../cmd/zp gen -g json/json.zp -module github.com/ornew/zeroparse/parsers/json -types -recognize -budget-controls -nodoc -runtime import -o json
//go:generate go run ../cmd/zp gen -g python/python.zp -expectations -module github.com/ornew/zeroparse/parsers/python -types -typed-only -nodoc -runtime import -o python
//go:generate go run ../cmd/zp gen -g typescript/typescript.zp -expectations -module github.com/ornew/zeroparse/parsers/typescript -types -typed-only -nodoc -runtime import -o typescript
//go:generate go run ../cmd/zp gen -g xml/xml.zp -expectations -module github.com/ornew/zeroparse/parsers/xml -types -recognize -budget-controls -nodoc -runtime import -o xml
//go:generate go run ../cmd/zp gen -g yaml/yaml.zp -module github.com/ornew/zeroparse/parsers/yaml -types -recognize -budget-controls -nodoc -runtime import -o yaml
