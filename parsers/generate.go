// Package parsers generates and checks the parsers in its subdirectories, each a Go module of its own
// (github.com/ornew/pego/parsers/<language>) that depends only on the standard library.
//
// Each subdirectory holds the grammar (<language>.pego), the parser generated from it (parser.go, api.go, types/types.go and a copy of the runtime in internal/runtime), code
// written by hand around the generated API, and tests against the language's reference implementation
// or conformance suite. The tests of this package check that every generated file is up to date and that
// every backend of the engine gives the results recorded in the subdirectories' golden files, which the
// generated parsers are checked against in their own modules. Regenerate the parsers with
// go generate ./parsers, and test a module with cd parsers/<language> && go test ./... (or
// parsers/test.sh for all of them).
package parsers

//go:generate go run ../cmd/pego gen -g cel/cel.pego -module github.com/ornew/pego/parsers/cel -types -recognize -nodoc -o cel
//go:generate go run ../cmd/pego gen -g csv/csv.pego -module github.com/ornew/pego/parsers/csv -types -recognize -nodoc -o csv
//go:generate go run ../cmd/pego gen -g cue/cue.pego -module github.com/ornew/pego/parsers/cue -types -recognize -nodoc -o cue
//go:generate go run ../cmd/pego gen -g duckdb/duckdb.pego -module github.com/ornew/pego/parsers/duckdb -types -recognize -nodoc -o duckdb
//go:generate go run ../cmd/pego gen -g golang/golang.pego -module github.com/ornew/pego/parsers/golang -types -recognize -nodoc -o golang
//go:generate go run ../cmd/pego gen -g json/json.pego -module github.com/ornew/pego/parsers/json -types -recognize -nodoc -o json
//go:generate go run ../cmd/pego gen -g python/python.pego -module github.com/ornew/pego/parsers/python -types -recognize -nodoc -o python
//go:generate go run ../cmd/pego gen -g typescript/typescript.pego -module github.com/ornew/pego/parsers/typescript -types -recognize -nodoc -o typescript
//go:generate go run ../cmd/pego gen -g xml/xml.pego -module github.com/ornew/pego/parsers/xml -types -recognize -nodoc -o xml
//go:generate go run ../cmd/pego gen -g yaml/yaml.pego -module github.com/ornew/pego/parsers/yaml -types -recognize -nodoc -o yaml
