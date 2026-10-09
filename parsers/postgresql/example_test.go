package postgresql_test

import (
	"errors"
	"fmt"

	"github.com/ornew/pego/parsers/postgresql"
)

func ExampleParseAST() {
	script, err := postgresql.ParseAST(`SELECT u.Name, count(*) FROM "Users" AS u WHERE u.age >= 18 GROUP BY 1; DELETE FROM t`)
	if err != nil {
		panic(err)
	}
	for _, raw := range script.Stmts {
		switch s := raw.Stmt.(type) {
		case *postgresql.SelectStmt:
			fmt.Printf("select with %d targets, %d tables at %d-%d\n", len(s.TargetList), len(s.FromClause), s.Start, s.End)
			for _, t := range s.TargetList {
				if c, ok := t.Val.(*postgresql.ColumnRef); ok {
					for _, f := range c.Fields {
						if id, ok := f.(*postgresql.Ident); ok {
							fmt.Printf("  column part %q\n", id.MustValue())
						}
					}
				}
			}
		default:
			fmt.Printf("%T\n", s)
		}
	}
	// Output:
	// select with 2 targets, 1 tables at 0-70
	//   column part "u"
	//   column part "name"
	// *postgresql.DeleteStmt
}

func ExampleParseAST_syntaxError() {
	_, err := postgresql.ParseAST("SELECT 1 +")
	var se *postgresql.SyntaxError
	if errors.As(err, &se) {
		fmt.Println(se.Line, se.Col)
	}
	// Output: 1 11
}

func ExampleSplit() {
	src := `select 'a;b'; create function f() returns int begin atomic select 1; select 2; end; -- done`
	for _, s := range postgresql.Split(src) {
		fmt.Printf("%q\n", src[s.Start:s.End])
	}
	// Output:
	// "select 'a;b'"
	// "create function f() returns int begin atomic select 1; select 2; end"
}

func ExampleIdent_Value() {
	script, _ := postgresql.ParseAST(`SELECT Foo, "Foo", U&"d\0061ta" FROM t`)
	sel := script.Stmts[0].Stmt.(*postgresql.SelectStmt)
	for _, t := range sel.TargetList {
		id := t.Val.(*postgresql.ColumnRef).Fields[0].(*postgresql.Ident)
		fmt.Println(id.MustValue())
	}
	// Output:
	// foo
	// Foo
	// data
}

func ExampleRawJSON() {
	script, _ := postgresql.ParseAST(`SELECT -1`)
	b, _ := postgresql.RawJSON(script)
	fmt.Println(string(b))
	// Output: [{"SelectStmt":{"limitOption":"LIMIT_OPTION_DEFAULT","op":"SETOP_NONE","targetList":[{"ResTarget":{"val":{"A_Const":{"ival":{"ival":-1}}}}}]}}]
}
