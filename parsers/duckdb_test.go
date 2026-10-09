package parsers_test

import (
	"os"
	"testing"

	"github.com/ornew/pego"
)

func TestDuckDBKeywordNames(t *testing.T) {
	src, err := os.ReadFile("duckdb/duckdb.pego")
	if err != nil {
		t.Fatal(err)
	}
	p, err := pego.CompileSource(string(src), "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		sql string
		ok  bool
	}{
		{"SELECT 1 tExT", false}, {"SELECT 1 iNtEgEr", false},
		{"CREATE TABLE t (x schema.tExT)", false},
		{"SELECT 1 AS tExT", true}, {"SELECT tExT FROM tExT", true},
		{"SELECT 1 tExT_", true}, {"SELECT 1 tExTé", true},
		{"SELECT 1 tExT$", true}, {`SELECT 1 "tExT"`, true},
		{"SELECT 1 iN", false}, {"SELECT 1 iNt", false},
		{"SELECT 1 iNtentionally", true},
	} {
		for _, backend := range []pego.Backend{pego.Closure, pego.Bytecode, pego.BytecodeIterative} {
			for _, unit := range []pego.Unit{pego.CodePoints, pego.Bytes} {
				_, err := p.Parse(tc.sql, pego.WithBackend(backend), pego.WithUnit(unit))
				if (err == nil) != tc.ok {
					t.Errorf("Parse(%q, backend=%v, unit=%v): %v", tc.sql, backend, unit, err)
				}
			}
		}
	}
}
