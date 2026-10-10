package parsers_test

import (
	"fmt"
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

func TestDuckDBSetofTypes(t *testing.T) {
	src, err := os.ReadFile("duckdb/duckdb.pego")
	if err != nil {
		t.Fatal(err)
	}
	p, err := pego.CompileSource(string(src), "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		typ string
		ok  bool
	}{
		{"SETOF INT[]", true}, {"SETOF varchar(10)", true},
		{"SETOF main.u ARRAY[2]", true}, {`SETOF "é"."u" ARRAY`, true},
		{"main.u[]", true}, {"STRUCT(a SETOF INT)", true},
		{"MAP(VARCHAR, SETOF INT)", true}, {"SETOFmain.u", true},
		{"SETOF main.u", false}, {"SETOF main.u[]", false},
		{"SETOF STRUCT(a INT)", false}, {"SETOF ROW(a INT)", false},
		{"SETOF UNION(a INT)", false}, {"SETOF MAP(VARCHAR, INT)", false},
		{"STRUCT(a SETOF main.u)", false},
	} {
		for _, template := range []string{"SELECT CAST(NULL AS %s)", "SELECT NULL::%s", "CREATE TABLE t (x %s)"} {
			sql := fmt.Sprintf(template, tc.typ)
			for _, backend := range []pego.Backend{pego.Closure, pego.Bytecode, pego.BytecodeIterative} {
				for _, unit := range []pego.Unit{pego.CodePoints, pego.Bytes} {
					_, err := p.Parse(sql, pego.WithBackend(backend), pego.WithUnit(unit))
					if (err == nil) != tc.ok {
						t.Errorf("Parse(%q, backend=%v, unit=%v): %v", sql, backend, unit, err)
					}
				}
			}
		}
	}
}

func TestDuckDBFrameBounds(t *testing.T) {
	src, err := os.ReadFile("duckdb/duckdb.pego")
	if err != nil {
		t.Fatal(err)
	}
	p, err := pego.CompileSource(string(src), "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		bound string
		ok    bool
	}{
		{"BETWEEN . PRECEDING AND 1 PRECEDING", true},
		{"BETWEEN IN PRECEDING AND -1 PRECEDING", true},
		{"BETWEEN AND PRECEDING AND 1 PRECEDING", true},
		{"BETWEEN (NOT 1) PRECEDING AND 1 FOLLOWING", true},
		{"BETWEEN 1 PRECEDING AND NOT 1 FOLLOWING", true},
		{"NOT 1 PRECEDING", true},
		{"BETWEEN NOT 1 PRECEDING AND 1 FOLLOWING", false},
		{"BETWEEN NOT (1) PRECEDING AND 1 FOLLOWING", false},
		{"BETWEEN - PRECEDING AND 1 FOLLOWING", false},
		{"BETWEEN + 1 PRECEDING", false},
		{"BETWEEN * 1 PRECEDING", false},
		{"BETWEEN [1] PRECEDING", false},
		{"BETWEEN AT TIME ZONE 'UTC' PRECEDING", false},
		{"BETWEEN PRECEDING", false},
		{"BETWEEN / 1 PRECEDING", true},
		{"BETWEEN % 1 PRECEDING", true},
		{"BETWEEN = 1 PRECEDING", true},
		{"BETWEEN ISNULL PRECEDING", true},
		{"BETWEEN LIKE 'x' PRECEDING", false},
		{"BETWEEN iLiKe 'x' PRECEDING", false},
		{"BETWEEN /* conflict */ GLOB 'x' PRECEDING", false},
		{"BETWEEN SIMILAR TO 'x' PRECEDING", false},
		{"BETWEEN BETWEEN 1 AND 2 PRECEDING", false},
		{"BETWEEN BETWEEN PRECEDING AND 1 FOLLOWING", false},
		{"BETWEEN NOT LIKE 'x' PRECEDING", false},
		{"BETWEEN NOT ILIKE 'x' PRECEDING", false},
		{"BETWEEN NOT SIMILAR TO 'x' PRECEDING", false},
		{"BETWEEN NOT IN (1) PRECEDING", false},
		{"BETWEEN NOT BETWEEN 1 AND 2 PRECEDING", false},
		{"(BETWEEN LIKE 'x') PRECEDING", true},
		{`BETWEEN "BETWEEN" PRECEDING AND 1 FOLLOWING`, true},
		{"BETWEEN (BETWEEN) PRECEDING AND 1 FOLLOWING", true},
		{"BETWEEN like_x PRECEDING AND 1 FOLLOWING", true},
		{"BETWEEN 1 PRECEDING AND . FOLLOWING", false},
	} {
		for _, frameUnit := range []string{"ROWS", "RANGE", "GROUPS"} {
			sql := fmt.Sprintf(`SELECT sum("é") OVER (ORDER BY i %s %s)`, frameUnit, tc.bound)
			for _, backend := range []pego.Backend{pego.Closure, pego.Bytecode, pego.BytecodeIterative} {
				for _, unit := range []pego.Unit{pego.CodePoints, pego.Bytes} {
					_, err := p.Parse(sql, pego.WithBackend(backend), pego.WithUnit(unit))
					if (err == nil) != tc.ok {
						t.Errorf("Parse(%q, backend=%v, unit=%v): %v", sql, backend, unit, err)
					}
				}
			}
		}
	}
}
