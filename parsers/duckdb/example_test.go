package duckdb_test

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ornew/pego/parsers/duckdb"
)

func ExampleParseAST() {
	src := `SELECT region, sum(amount) AS total FROM sales WHERE year = 2024 GROUP BY region`
	script, err := duckdb.ParseAST(src)
	if err != nil {
		panic(err)
	}
	sel := script.Statements[0].(*duckdb.Select)
	core := sel.Body.(*duckdb.SelectCore)
	for _, item := range core.Items {
		fmt.Printf("%-8s %s\n", fmt.Sprintf("%d-%d", item.Span.Start, item.Span.End), src[item.Span.Start:item.Span.End])
	}
	tbl := core.From[0].(*duckdb.BaseTable)
	fmt.Println("from", tbl.Name.Names())
	// Output:
	// 7-13     region
	// 15-35    sum(amount) AS total
	// from [sales]
}

func ExampleWalk() {
	script, err := duckdb.ParseAST(`SELECT t.a, f(b) FROM s.t JOIN u ON t.id = u.id WHERE c > 1`)
	if err != nil {
		panic(err)
	}
	// the columns and the tables of the statement, in the order of the input
	duckdb.Walk(script, func(n any) bool {
		switch n := n.(type) {
		case *duckdb.ColumnRef:
			fmt.Println("column", n.Parts[len(n.Parts)-1].Name())
		case *duckdb.BaseTable:
			fmt.Println("table", strings.Join(n.Name.Names(), "."))
		}
		return true
	})
	// Output:
	// column a
	// column b
	// table s.t
	// table u
	// column id
	// column id
	// column c
}

func ExampleSplit() {
	statements, err := duckdb.Split("CREATE TABLE t (a INT); INSERT INTO t VALUES (1), (2); FROM t")
	if err != nil {
		panic(err)
	}
	for _, s := range statements {
		fmt.Printf("%-7s %q\n", duckdb.Kind(s.Statement), s.Text)
	}
	// Output:
	// CREATE  "CREATE TABLE t (a INT)"
	// INSERT  " INSERT INTO t VALUES (1), (2)"
	// SELECT  " FROM t"
}

func ExampleIdent_Name() {
	script, err := duckdb.ParseAST(`SELECT Foo, "Bar", "a ""quoted"" name", U&"d\0061ta"`)
	if err != nil {
		panic(err)
	}
	duckdb.Walk(script, func(n any) bool {
		if id, ok := n.(*duckdb.Ident); ok {
			fmt.Printf("%-24s %-16q quoted=%v\n", id.Text, id.Name(), id.Quoted())
		}
		return true
	})
	// Output:
	// Foo                      "Foo"            quoted=false
	// "Bar"                    "Bar"            quoted=true
	// "a ""quoted"" name"      "a \"quoted\" name" quoted=true
	// U&"d\0061ta"             "data"           quoted=true
}

func ExampleStringLit_Value() {
	script, err := duckdb.ParseAST(`SELECT 'it''s', E'tab\there', $$a 'b'$$, 'one'
	'two'`)
	if err != nil {
		panic(err)
	}
	duckdb.Walk(script, func(n any) bool {
		if s, ok := n.(*duckdb.StringLit); ok {
			fmt.Printf("%q\n", s.Value())
		}
		return true
	})
	// Output:
	// "it's"
	// "tab\there"
	// "a 'b'"
	// "onetwo"
}

func ExampleNumber_Type() {
	script, err := duckdb.ParseAST(`SELECT 42, 3000000000, 1.50, 1e3, 1_000, 99999999999999999999`)
	if err != nil {
		panic(err)
	}
	duckdb.Walk(script, func(n any) bool {
		if num, ok := n.(*duckdb.Number); ok {
			fmt.Printf("%-22s %s\n", num.Text, num.Type())
		}
		return true
	})
	// Output:
	// 42                     INTEGER
	// 3000000000             BIGINT
	// 1.50                   DECIMAL
	// 1e3                    DOUBLE
	// 1_000                  INTEGER
	// 99999999999999999999   HUGEINT
}

func ExampleQuoteIdent() {
	for _, name := range []string{"orders", "Region", "order", "left", "year", "my table", `a"b`} {
		fmt.Println(duckdb.QuoteIdent(name))
	}
	// Output:
	// orders
	// Region
	// "order"
	// "left"
	// year
	// "my table"
	// "a""b"
}

func ExampleKeyword() {
	for _, w := range []string{"SELECT", "year", "int", "left", "frobnicate"} {
		c, ok := duckdb.Keyword(w)
		fmt.Println(w, c, ok)
	}
	// Output:
	// SELECT reserved true
	// year unreserved true
	// int column_name true
	// left type_function true
	// frobnicate none false
}

func ExampleSyntaxError() {
	_, err := duckdb.ParseAST("SELECT a,\n  b FROM")
	var se *duckdb.SyntaxError
	if errors.As(err, &se) {
		fmt.Println("line", se.Line, "column", se.Col)
	}
	// Output: line 2 column 9
}

func ExampleRecognize() {
	fmt.Println(duckdb.Recognize("SELECT 1 UNION SELECT 2"))
	fmt.Println(duckdb.Recognize("SELECT 1 UNION") != nil)
	// Output:
	// <nil>
	// true
}
