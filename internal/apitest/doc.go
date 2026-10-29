// Package apitest holds the behavior tests of package pego. They test the root package from outside
// it, through its public API, and use internal packages only to build values, to mark start rules
// and to count how often options apply, so that the root holds only the API source and its
// documentation examples. Measure the coverage of package pego with
//
//	go test -coverpkg=github.com/ornew/pego ./internal/apitest
package apitest
