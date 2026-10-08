package cel_test

import (
	"errors"
	"fmt"
	"sort"

	"github.com/ornew/pego/parsers/cel"
)

func ExampleParseExpr() {
	e, err := cel.ParseExpr(`user.age >= 18 && "admin" in user.roles`)
	if err != nil {
		panic(err)
	}
	b := e.(*cel.Binary) // the && of the two relations
	fmt.Printf("%T %s %T\n", b.Left, b.Op.Text, b.Right)
	// Output: *cel.Binary && *cel.Binary
}

// The variables and the functions an expression uses, found by walking the tree.
func ExampleInspect() {
	e, err := cel.ParseExpr(`request.auth.claims.exists(c, c.startsWith("x")) && size(items) > limit`)
	if err != nil {
		panic(err)
	}
	names := map[string]bool{}
	cel.Inspect(e, func(e cel.Expr) bool {
		switch e := e.(type) {
		case *cel.Ident:
			names["variable "+e.Name()] = true
		case *cel.Call:
			names["function "+e.Func.Name()] = true
		}
		return true
	})
	var list []string
	for n := range names {
		list = append(list, n)
	}
	sort.Strings(list)
	for _, n := range list {
		fmt.Println(n)
	}
	// Output:
	// function exists
	// function size
	// function startsWith
	// variable c
	// variable items
	// variable limit
	// variable request
}

func ExampleParseAST() {
	e, err := cel.ParseAST("a.b + f(1,\n  2u) // sum")
	if err != nil {
		panic(err)
	}
	sum := e.(*cel.Binary)
	call := sum.Right.(*cel.Call)
	fmt.Printf("%s at %d-%d\n", cel.Format(call), call.Start, call.End)
	fmt.Printf("%s at %d-%d\n", call.Func.Name(), call.Func.Start, call.Func.End)
	// Output:
	// f(1, 2u) at 6-16
	// f at 6-7
}

func ExampleParseExpr_syntaxError() {
	_, err := cel.ParseExpr("a + (b *")
	var se *cel.SyntaxError
	if errors.As(err, &se) {
		fmt.Println(se.Line, se.Col)
	}
	// Output: 1 9
}

// Literals are checked after parsing: cel-go rejects them when it reads them, so ParseExpr does too.
func ExampleParseExpr_checkError() {
	_, err := cel.ParseExpr("x < 9223372036854775808")
	fmt.Println(err)
	// A string with the escape of a UTF-16 surrogate pair: backslash, u, D83D, backslash, u, DE03.
	_, err = cel.ParseExpr(`"\` + `uD83D\` + `uDE03"`)
	fmt.Println(err != nil)
	// Output:
	// 1:5: invalid int literal
	// true
}

func ExampleStringLit_Value() {
	e, _ := cel.ParseExpr(`'''line one
line two\t(tab) \` + `u00e9 \x41 \101'''`)
	fmt.Printf("%q\n", e.(*cel.StringLit).Value())
	// Output: "line one\nline two\t(tab) é A A"
}

func ExampleBytesLit_Value() {
	e, _ := cel.ParseExpr(`b"\xff\303\277é"`)
	fmt.Printf("% x\n", e.(*cel.BytesLit).Value())
	// Output: ff c3 bf c3 a9
}

func ExampleConstant() {
	e, _ := cel.ParseExpr(`{"a": [1, 2u, 3.5], "b": (null), 'c': b"x"}`)
	v, ok := cel.Constant(e)
	m := v.(map[any]any)
	fmt.Println(ok, m["a"], m["b"], m["c"])
	_, ok = cel.Constant(mustParse("[1, a]"))
	fmt.Println(ok)
	// Output:
	// true [1 2 3.5] <nil> [120]
	// false
}

func mustParse(src string) cel.Expr {
	e, err := cel.ParseExpr(src)
	if err != nil {
		panic(err)
	}
	return e
}

func ExampleFormat() {
	e, _ := cel.ParseExpr("a&&(b||c)  ?x.y [ 0 ]:-1 // normalized")
	fmt.Println(cel.Format(e))
	// Output: a && (b || c) ? x.y[0] : -1
}

// The macros are calls in the tree. CheckMacros reports those that cel-go would reject, with the standard macros.
func ExampleCheckMacros() {
	e, _ := cel.ParseExpr("items.exists(1, true) && has(a)")
	fmt.Println(cel.CheckMacros(e))
	_, err := cel.ParseExpr("items.exists(i, true)", cel.WithMacros())
	fmt.Println(err)
	// Output:
	// offset 13: argument must be a simple name
	// <nil>
}

func ExampleValid() {
	fmt.Println(cel.Valid("1 + 2"), cel.Valid("1 +"), cel.Valid("1 + 2 // comment"), cel.Valid("if"))
	// Output: true false true false
}
