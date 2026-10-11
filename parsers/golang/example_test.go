package golang_test

import (
	"fmt"
	"go/format"
	"go/token"
	"os"

	"github.com/ornew/pego/parsers/golang"
)

const hello = `package main

import "fmt"

func main() {
	for i := range 3 {
		fmt.Println("hello", i)
	}
}
`

func ExampleParseAST() {
	file, err := golang.ParseAST(hello, golang.WithUnit(golang.Bytes))
	if err != nil {
		panic(err)
	}
	fmt.Println("package", file.Name.Text)
	for _, d := range file.Decls {
		switch d := d.(type) {
		case *golang.GenDecl:
			fmt.Printf("%s at %d-%d\n", d.Tok, d.Start, d.End)
		case *golang.FuncDecl:
			fmt.Printf("func %s at %d-%d, %d statement(s)\n", d.Name.Text, d.Start, d.End, len(d.Body.List))
		}
	}
	// Output:
	// package main
	// import at 14-26
	// func main at 28-92, 1 statement(s)
}

func ExampleParseAST_literals() {
	file, err := golang.ParseAST(`package p; var _ = []any{"a\tb", 'x', 0x1F, 1.5}`, golang.WithUnit(golang.Bytes))
	if err != nil {
		panic(err)
	}
	lit := file.Decls[0].(*golang.GenDecl).Specs[0].(*golang.ValueSpec).Values[0].(*golang.CompositeLit)
	s, _ := lit.Elts[0].(*golang.StringLit).Value()
	r, _ := lit.Elts[1].(*golang.CharLit).Value()
	n, _ := lit.Elts[2].(*golang.IntLit).Constant()
	f, _ := lit.Elts[3].(*golang.FloatLit).Constant()
	fmt.Printf("%q %c %v %v\n", s, r, n, f)
	// Output: "a\tb" x 31 1.5
}

// ParseFile returns the tree that go/parser returns, so that go/printer, go/format, go/types and
// other tools work on it.
func ExampleParseFile() {
	fset := token.NewFileSet()
	f, err := golang.ParseFile(fset, "hello.go", []byte(hello), 0)
	if err != nil {
		panic(err)
	}
	fmt.Println(f.Name.Name, fset.Position(f.Decls[1].Pos()))
	if err := format.Node(os.Stdout, fset, f.Decls[1]); err != nil {
		panic(err)
	}
	// Output:
	// main hello.go:5:1
	// func main() {
	// 	for i := range 3 {
	// 		fmt.Println("hello", i)
	// 	}
	// }
}

func ExampleParseFile_syntaxError() {
	_, err := golang.ParseFile(token.NewFileSet(), "x.go", []byte("package p\n\nfunc f() { x := }\n"), 0)
	fmt.Println(err)
	// Output: x.go:3:17: expected operand
}

func ExampleValid() {
	fmt.Println(golang.Valid("package p; type T[P any] struct{}"), golang.Valid("package p; func f() { x := }"))
	// Output: true false
}
