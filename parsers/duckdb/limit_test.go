package duckdb_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ornew/pego/parsers/duckdb"
)

func TestLimitPercentAST(t *testing.T) {
	for _, tc := range []struct{ value, percent, offset, kind string }{
		{"-1|", "%", "", "postfix"},
		{"1 IN (1) * 2", "%", "", "multiply"},
		{"1 ISNULL + 2 ISNULL", "%", "", "is"},
		{"1 ISNULL IN (1)", "%", "", "in"},
		{"1 = ANY (SELECT 1)", "%", "", "any"},
		{"(NOT 1)", "%", "", "paren"},
		{"10", "PERCENT", "", "number"},
		{"1|", "%", "2", "comma"},
	} {
		sql := `SELECT "é" FROM t LIMIT ` + tc.value + " " + tc.percent
		if tc.offset != "" {
			sql += ", " + tc.offset
		}
		for _, unit := range []duckdb.Unit{duckdb.CodePoints, duckdb.Bytes} {
			script, err := duckdb.ParseAST(sql, unit)
			if err != nil {
				t.Fatal(err)
			}
			var limits []*duckdb.LimitClause
			duckdb.Walk(script, func(n any) bool {
				if l, ok := n.(*duckdb.LimitClause); ok {
					limits = append(limits, l)
				}
				return true
			})
			if len(limits) != 1 {
				t.Fatalf("%q: got %d limit clauses", sql, len(limits))
			}
			l := limits[0]
			span := func(start, end int) duckdb.Span {
				if unit == duckdb.CodePoints {
					start, end = utf8.RuneCountInString(sql[:start]), utf8.RuneCountInString(sql[:end])
				}
				return duckdb.Span{Start: start, End: end}
			}
			start := strings.Index(sql, "LIMIT")
			vstart := start + len("LIMIT ")
			wantValue := span(vstart, vstart+len(tc.value))
			if l.Keyword != "LIMIT" || l.All || l.Percent != tc.percent || l.Span != span(start, len(sql)) {
				t.Fatalf("%q (%v): unexpected limit %#v", sql, unit, l)
			}
			if tc.offset == "" {
				if l.Offset != nil {
					t.Fatalf("%q: unexpected offset %#v", sql, l.Offset)
				}
			} else {
				n, ok := l.Offset.(*duckdb.Number)
				if !ok || n.Text != tc.offset || n.Span != span(len(sql)-len(tc.offset), len(sql)) {
					t.Fatalf("%q: unexpected offset %#v", sql, l.Offset)
				}
			}
			switch tc.kind {
			case "postfix", "comma":
				p, ok := l.Value.(*duckdb.PostfixOp)
				if !ok || p.Op.Text != "|" || p.Span != wantValue || p.Op.Span != span(vstart+len(tc.value)-1, vstart+len(tc.value)) {
					t.Fatalf("%q: unexpected postfix %#v", sql, l.Value)
				}
				if tc.kind == "postfix" {
					u, ok := p.X.(*duckdb.Unary)
					if !ok || u.Op.Text != "-" || u.Span != span(vstart, vstart+2) {
						t.Fatalf("%q: unexpected prefix %#v", sql, p.X)
					}
					n, ok := u.X.(*duckdb.Number)
					if !ok || n.Text != "1" || n.Span != span(vstart+1, vstart+2) {
						t.Fatalf("%q: unexpected operand %#v", sql, u.X)
					}
				}
			case "multiply":
				b, ok := l.Value.(*duckdb.Binary)
				if !ok || b.Op.Text != "*" || b.Span != wantValue {
					t.Fatalf("%q: unexpected multiplication %#v", sql, l.Value)
				}
				i, ok := b.Left.(*duckdb.In)
				if !ok || i.Negated || len(i.Items) != 1 || i.Query != nil || i.Source != nil || i.Span != span(vstart, vstart+8) {
					t.Fatalf("%q: unexpected left predicate %#v", sql, b.Left)
				}
			case "is":
				i, ok := l.Value.(*duckdb.IsExpr)
				if !ok || i.Test != "ISNULL" || i.Span != wantValue {
					t.Fatalf("%q: unexpected predicate %#v", sql, l.Value)
				}
				b, ok := i.X.(*duckdb.Binary)
				if !ok || b.Op.Text != "+" || b.Span != span(vstart, vstart+12) {
					t.Fatalf("%q: unexpected enclosed arithmetic %#v", sql, i.X)
				}
				if _, ok := b.Left.(*duckdb.IsExpr); !ok {
					t.Fatalf("%q: unexpected first predicate %#v", sql, b.Left)
				}
			case "in":
				i, ok := l.Value.(*duckdb.In)
				if !ok || i.Span != wantValue || len(i.Items) != 1 {
					t.Fatalf("%q: unexpected IN %#v", sql, l.Value)
				}
				if _, ok := i.X.(*duckdb.IsExpr); !ok {
					t.Fatalf("%q: unexpected left predicate %#v", sql, i.X)
				}
			case "any":
				a, ok := l.Value.(*duckdb.AnyAll)
				if !ok || a.Op != "=" || a.Quantifier != "ANY" || a.Span != wantValue {
					t.Fatalf("%q: unexpected ANY %#v", sql, l.Value)
				}
			case "paren":
				p, ok := l.Value.(*duckdb.Paren)
				if !ok || p.Span != wantValue {
					t.Fatalf("%q: unexpected parentheses %#v", sql, l.Value)
				}
				if _, ok := p.X.(*duckdb.Not); !ok {
					t.Fatalf("%q: unexpected enclosed expression %#v", sql, p.X)
				}
			case "number":
				n, ok := l.Value.(*duckdb.Number)
				if !ok || n.Text != "10" || n.Span != wantValue {
					t.Fatalf("%q: unexpected number %#v", sql, l.Value)
				}
			}
		}
	}
}
