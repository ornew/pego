package duckdb_test

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ornew/pego/parsers/duckdb"
)

func TestFrameBoundaries(t *testing.T) {
	// DuckDB 1.5.6 select.y accepts a_expr offsets. State 3478 of its
	// generated Bison grammar can instead reduce BETWEEN as a column name.
	for _, tc := range []struct {
		bound string
		ok    bool
	}{
		{"BETWEEN . PRECEDING AND 1 PRECEDING", true},
		{"BETWEEN IN PRECEDING AND -1 PRECEDING", true},
		{"BETWEEN AND PRECEDING AND 1 PRECEDING", true},
		{"between /* offset */ . following and 1 preceding", true},
		{"BETWEEN 1 PRECEDING AND 1 FOLLOWING", true},
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
		{"BETWEEN /* offset */ nOt 1 PRECEDING AND 1 FOLLOWING", false},
		{". PRECEDING", false}, {"IN PRECEDING", false}, {"AND PRECEDING", false},
		{"BETWEEN 1 PRECEDING AND . FOLLOWING", false},
		{"BETWEEN 1 PRECEDING AND IN FOLLOWING", false},
		{"BETWEEN 1 PRECEDING AND AND FOLLOWING", false},
	} {
		for _, frameUnit := range []string{"ROWS", "RANGE", "GROUPS"} {
			sql := fmt.Sprintf("SELECT sum(v) OVER (ORDER BY i %s %s)", frameUnit, tc.bound)
			for _, unit := range []duckdb.Unit{duckdb.CodePoints, duckdb.Bytes} {
				if err := duckdb.Recognize(sql, unit); (err == nil) != tc.ok {
					t.Errorf("Recognize(%q, %v): %v", sql, unit, err)
				}
				if _, err := duckdb.Parse(sql, unit); (err == nil) != tc.ok {
					t.Errorf("Parse(%q, %v): %v", sql, unit, err)
				}
				if _, err := duckdb.ParseAST(sql, unit); (err == nil) != tc.ok {
					t.Errorf("ParseAST(%q, %v): %v", sql, unit, err)
				}
			}
		}
	}
}

func TestFrameAST(t *testing.T) {
	for _, tc := range []struct {
		bound string
		kind  string
	}{
		{"BETWEEN . PRECEDING AND 1 PRECEDING", "dot"},
		{"BETWEEN IN PRECEDING AND -1 PRECEDING", "in"},
		{"BETWEEN AND PRECEDING AND 1 PRECEDING", "and"},
		{"BETWEEN 1 PRECEDING AND 1 FOLLOWING", "two"},
		{"BETWEEN (NOT 1) PRECEDING AND 1 FOLLOWING", "not"},
	} {
		for _, frameUnit := range []string{"ROWS", "RANGE", "GROUPS"} {
			sql := fmt.Sprintf(`SELECT sum("é") OVER (ORDER BY i %s %s EXCLUDE TIES)`, frameUnit, tc.bound)
			for _, unit := range []duckdb.Unit{duckdb.CodePoints, duckdb.Bytes} {
				script, err := duckdb.ParseAST(sql, unit)
				if err != nil {
					t.Fatal(err)
				}
				var frames []*duckdb.Frame
				duckdb.Walk(script, func(n any) bool {
					if f, ok := n.(*duckdb.Frame); ok {
						frames = append(frames, f)
					}
					return true
				})
				if len(frames) != 1 {
					t.Fatalf("%q: got %d frames", sql, len(frames))
				}
				f := frames[0]
				start, end := strings.Index(sql, frameUnit), len(sql)-1
				if unit == duckdb.CodePoints {
					start, end = utf8.RuneCountInString(sql[:start]), utf8.RuneCountInString(sql[:end])
				}
				if f.Unit != frameUnit || f.Exclude != "EXCLUDE TIES" || f.Start == nil || f.Start.Kind != "PRECEDING" || f.Span != (duckdb.Span{Start: start, End: end}) {
					t.Fatalf("%q: unexpected frame %#v", sql, f)
				}
				if tc.kind == "two" || tc.kind == "not" {
					if f.End == nil || f.End.Kind != "FOLLOWING" {
						t.Fatalf("%q: missing second bound", sql)
					}
					if tc.kind == "two" {
						if n, ok := f.Start.Expr.(*duckdb.Number); !ok || n.Text != "1" {
							t.Fatalf("%q: unexpected first offset %#v", sql, f.Start.Expr)
						}
					} else {
						p, ok := f.Start.Expr.(*duckdb.Paren)
						if !ok {
							t.Fatalf("%q: unexpected first offset %#v", sql, f.Start.Expr)
						}
						if _, ok := p.X.(*duckdb.Not); !ok {
							t.Fatalf("%q: unexpected parenthesized offset %#v", sql, p.X)
						}
					}
					continue
				}
				if f.End != nil {
					t.Fatalf("%q: expected a single bound, got %#v", sql, f.End)
				}
				b, ok := f.Start.Expr.(*duckdb.Binary)
				if !ok || !strings.EqualFold(b.Op.Text, "AND") {
					t.Fatalf("%q: unexpected offset %#v", sql, f.Start.Expr)
				}
				column := func(e duckdb.Expr, names ...string) {
					t.Helper()
					c, ok := e.(*duckdb.ColumnRef)
					if !ok || len(c.Parts) != len(names) {
						t.Fatalf("%q: unexpected column %#v", sql, e)
					}
					for i, name := range names {
						if c.Parts[i].Text != name {
							t.Fatalf("%q: column part %q, want %q", sql, c.Parts[i].Text, name)
						}
					}
				}
				switch tc.kind {
				case "dot":
					column(b.Left, "BETWEEN", "PRECEDING")
				case "in":
					i, ok := b.Left.(*duckdb.In)
					if !ok || i.Negated || i.Query != nil || len(i.Items) != 0 {
						t.Fatalf("%q: unexpected IN offset %#v", sql, b.Left)
					}
					column(i.X, "BETWEEN")
					column(i.Source, "PRECEDING")
				case "and":
					left, ok := b.Left.(*duckdb.Binary)
					if !ok || !strings.EqualFold(left.Op.Text, "AND") {
						t.Fatalf("%q: unexpected AND offset %#v", sql, b.Left)
					}
					column(left.Left, "BETWEEN")
					column(left.Right, "PRECEDING")
				}
			}
		}
	}
}
