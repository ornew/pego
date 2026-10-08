// Package parsers generates and checks the parsers in its subdirectories, each a Go module of its own
// (github.com/ornew/pego/parsers/<language>) that depends only on the standard library.
//
// Each subdirectory holds the grammar (<language>.pego), the parser generated from it (parser.go), code
// written by hand around the generated API, and tests against the language's reference implementation
// or conformance suite. The tests of this package check that every parser.go is up to date and that
// every backend of the engine gives the results recorded in the subdirectories' golden files, which the
// generated parsers are checked against in their own modules. Regenerate the parsers with
// go generate ./parsers, and test a module with cd parsers/<language> && go test ./... (or
// parsers/test.sh for all of them).
package parsers

//go:generate go run ../cmd/pego gen -g json/json.pego -pkg json -types -recognize -nodoc -o json/parser.go
