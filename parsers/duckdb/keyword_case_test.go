package duckdb_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/duckdb"
)

func alternatingCase(word string, upperFirst bool) string {
	var b strings.Builder
	for i, ch := range word {
		if (i%2 == 0) == upperFirst && ch >= 'a' && ch <= 'z' {
			ch -= 'a' - 'A'
		}
		b.WriteRune(ch)
	}
	return b.String()
}

// Compare every keyword spelling in both restrictive and permissive name
// contexts. Canonical spellings are also covered by the vendored DuckDB oracle.
func TestKeywordCaseInvariance(t *testing.T) {
	templates := []string{"SELECT 1 %s", "SELECT 1 AS %s", "SELECT %s FROM t", "CREATE TABLE t (x %s)", "CREATE TABLE t (x schema.%s)", "SELECT 1 %s_suffix"}
	for word := range duckdb.Keywords() {
		for _, template := range templates {
			canonical := fmt.Sprintf(template, word)
			want := duckdb.Recognize(canonical) == nil
			for _, spelling := range []string{alternatingCase(word, true), alternatingCase(word, false)} {
				sql := fmt.Sprintf(template, spelling)
				for _, unit := range []duckdb.Unit{duckdb.CodePoints, duckdb.Bytes} {
					if err := duckdb.Recognize(sql, duckdb.WithUnit(unit)); (err == nil) != want {
						t.Errorf("Recognize(%q, %v) case differs from %q: %v", sql, unit, canonical, err)
					}
					if _, err := duckdb.ParseAST(sql, duckdb.WithUnit(unit)); (err == nil) != want {
						t.Errorf("ParseAST(%q, %v) case differs from %q: %v", sql, unit, canonical, err)
					}
				}
			}
		}
	}
}

func TestMixedCaseKeywordNames(t *testing.T) {
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
		for _, unit := range []duckdb.Unit{duckdb.CodePoints, duckdb.Bytes} {
			if err := duckdb.Recognize(tc.sql, duckdb.WithUnit(unit)); (err == nil) != tc.ok {
				t.Errorf("Recognize(%q): %v", tc.sql, err)
			}
			if _, err := duckdb.Parse(tc.sql, duckdb.WithUnit(unit)); (err == nil) != tc.ok {
				t.Errorf("Parse(%q): %v", tc.sql, err)
			}
			if _, err := duckdb.ParseAST(tc.sql, duckdb.WithUnit(unit)); (err == nil) != tc.ok {
				t.Errorf("ParseAST(%q): %v", tc.sql, err)
			}
		}
	}
	ids := find[duckdb.Ident](t, "SELECT tExT FROM tExT")
	if len(ids) != 2 || ids[0].Text != "tExT" || ids[1].Text != "tExT" {
		t.Fatalf("identifier text changed: %#v", ids)
	}
}
