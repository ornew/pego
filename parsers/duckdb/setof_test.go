package duckdb_test

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ornew/pego/parsers/duckdb"
)

func TestSetofTypeBoundaries(t *testing.T) {
	// DuckDB 1.5.6 Typename distinguishes SETOF SimpleTypename from
	// qualified names with explicit ARRAY, and from constructed types.
	for _, tc := range []struct {
		typ string
		ok  bool
	}{
		{"SETOF INT", true}, {"setof varchar(10)", true},
		{"SetOf DECIMAL(10,2)[]", true}, {"SETOF u[2][]", true},
		{`SETOF "u" ARRAY[2]`, true}, {"SETOF TIMESTAMP(3) WITH TIME ZONE", true},
		{"SETOF INTERVAL DAY TO HOUR", true}, {"SETOF BIT VARYING(3)", true},
		{"SETOF main.u ARRAY", true}, {"SETOF main.u ARRAY[2]", true},
		{`SETOF "main"."u" ARRAY[2]`, true}, {"SETOF a.b.u ARRAY", true},
		{"SETOF /* comment */ main /* name */ . u ARRAY[2]", true},
		{"main.u", true}, {"main.u[]", true}, {"main.u[2]", true},
		{"main.u ARRAY", true}, {"main.u ARRAY[2]", true},
		{"STRUCT(a INT)", true}, {"ROW(a INT)", true},
		{"UNION(a INT)", true}, {"MAP(VARCHAR, INT)", true},
		{"STRUCT(a SETOF INT)", true}, {"ROW(a SETOF INT)", true},
		{"UNION(a SETOF INT)", true}, {"MAP(VARCHAR, SETOF INT)", true},
		{"STRUCT(a SETOF main.u ARRAY[2])", true},
		{"SETOFint", true}, {"SETOFmain.u", true}, {`"SETOF"`, true},
		{"SETOF main.u", false}, {"SETOF main.u[]", false},
		{"SETOF main.u[2]", false}, {`SETOF "main"."u"`, false},
		{"SETOF main.u(2) ARRAY", false},
		{"SETOF STRUCT(a INT)", false}, {"SETOF ROW(a INT)[]", false},
		{"SETOF UNION(a INT)[2]", false}, {"SETOF MAP(VARCHAR, INT)", false},
		{"STRUCT(a SETOF main.u)", false}, {"SETOF INT ARRAY[]", false},
	} {
		for _, template := range []string{"SELECT CAST(NULL AS %s)", "SELECT NULL::%s", "CREATE TABLE t (x %s)"} {
			sql := fmt.Sprintf(template, tc.typ)
			for _, unit := range []duckdb.Unit{duckdb.CodePoints, duckdb.Bytes} {
				if err := duckdb.Recognize(sql, duckdb.WithUnit(unit)); (err == nil) != tc.ok {
					t.Errorf("Recognize(%q, %v): %v", sql, unit, err)
				}
				if _, err := duckdb.Parse(sql, duckdb.WithUnit(unit)); (err == nil) != tc.ok {
					t.Errorf("Parse(%q, %v): %v", sql, unit, err)
				}
				if _, err := duckdb.ParseAST(sql, duckdb.WithUnit(unit)); (err == nil) != tc.ok {
					t.Errorf("ParseAST(%q, %v): %v", sql, unit, err)
				}
			}
		}
	}
}

func TestSetofTypeAST(t *testing.T) {
	for _, tc := range []struct {
		typ, name, mod, size string
	}{
		{"SETOF varchar(10) ARRAY[2]", "varchar", "10", "2"},
		{`SETOF "é"."u" ARRAY[3]`, `"é"."u"`, "", "3"},
		{"main.u[2]", "main.u", "", "2"},
	} {
		sql := "SELECT CAST(NULL AS " + tc.typ + ")"
		for _, unit := range []duckdb.Unit{duckdb.CodePoints, duckdb.Bytes} {
			script, err := duckdb.ParseAST(sql, duckdb.WithUnit(unit))
			if err != nil {
				t.Fatal(err)
			}
			var types []*duckdb.Type
			duckdb.Walk(script, func(n any) bool {
				if typ, ok := n.(*duckdb.Type); ok {
					types = append(types, typ)
				}
				return true
			})
			if len(types) != 1 {
				t.Fatalf("%q: got %d types", sql, len(types))
			}
			typ := types[0]
			start, end := strings.Index(sql, tc.name), len(sql)-1
			if unit == duckdb.CodePoints {
				start, end = utf8.RuneCountInString(sql[:start]), utf8.RuneCountInString(sql[:end])
			}
			if typ.Name != tc.name || typ.Start != start || typ.End != end {
				t.Errorf("%q: type name/span = %q/%v, want %q/%v", sql, typ.Name, typ.Span, tc.name, duckdb.Span{Start: start, End: end})
			}
			if len(typ.Dims) != 1 || typ.Dims[0].Size == nil || typ.Dims[0].Size.Text != tc.size {
				t.Errorf("%q: dimensions = %#v", sql, typ.Dims)
			}
			if tc.mod == "" {
				if len(typ.Mods) != 0 {
					t.Errorf("%q: unexpected modifiers", sql)
				}
			} else if len(typ.Mods) != 1 {
				t.Errorf("%q: modifier count = %d", sql, len(typ.Mods))
			} else if n, ok := typ.Mods[0].(*duckdb.Number); !ok || n.Text != tc.mod {
				t.Errorf("%q: modifier = %#v", sql, typ.Mods[0])
			}
		}
	}
}
