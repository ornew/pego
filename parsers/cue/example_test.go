package cue_test

import (
	"errors"
	"fmt"

	"github.com/ornew/pego/parsers/cue"
)

func ExampleParseFile() {
	f, err := cue.ParseFile(`package config

import "strings"

#Service: {
	name!:    string
	replicas: *2 | int & >=1
	host:     "\(name).example.com"
}

web: #Service & {name: strings.ToLower("WEB")} @go(Web)
`)
	if err != nil {
		panic(err)
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *cue.Package:
			fmt.Println("package", d.Name.Text)
		case *cue.ImportDecl:
			path, _ := d.Specs[0].Path.Unquote()
			fmt.Println("import", path)
		case *cue.Field:
			name := d.Label.(*cue.Ident)
			fmt.Printf("field %s (definition %v) at %d-%d\n", name.Text, name.IsDefinition(), d.Start, d.End)
		}
	}
	// Output:
	// package config
	// import strings
	// field #Service (definition true) at 34-124
	// field web (definition false) at 126-181
}

func ExampleParseAST() {
	src := `a: b + 1 * c.d`
	f, err := cue.ParseAST(src)
	if err != nil {
		panic(err)
	}
	e := f.Decls[0].(*cue.Field).Value.(*cue.BinaryExpr)
	text := func(x cue.Expr) string { // every node has its span; the positions are code points by default
		sp := cue.SpanOf(x)
		return src[sp.Start:sp.End]
	}
	fmt.Println(e.Op.Text, "|", text(e.X), "|", text(e.Y))
	// Output:
	// + | b | 1 * c.d
}

func ExampleParseFile_error() {
	_, err := cue.ParseFile("a: 1\nb: [1, 2\nc: 3\n")
	var se *cue.SyntaxError
	if errors.As(err, &se) {
		fmt.Println(se.Line, se.Col)
	}

	// The grammar accepts what the parser of CUE rejects after the syntax: ParseFile checks it.
	_, err = cue.ParseFile("a: 1\nX=a: 2\nX=b: 3\n")
	var ce *cue.SemanticError
	if errors.As(err, &ce) {
		fmt.Println(ce.Line, ce.Col, ce.Msg)
	}
	// Output:
	// 3 2
	// 3 1 alias "X" redeclared in same scope
}

func ExampleString_Unquote() {
	f, _ := cue.ParseFile("a: #\"\"\"\n\tC:\\dir\\#n\n\t\"\"\"#\nb: \"\\u00e9\\n\"\n")
	for _, d := range f.Decls {
		s, err := d.(*cue.Field).Value.(*cue.String).Unquote()
		fmt.Printf("%q %v\n", s, err)
	}
	// Output:
	// "C:\\dir\n" <nil>
	// "é\n" <nil>
}

func ExampleInt_Value() {
	f, _ := cue.ParseFile("a: 1_000\nb: 0xff\nc: 1.5Ki\nd: 5G\n")
	for _, d := range f.Decls {
		v, err := d.(*cue.Field).Value.(*cue.Int).Value()
		fmt.Println(v, err)
	}
	// Output:
	// 1000 <nil>
	// 255 <nil>
	// 1536 <nil>
	// 5000000000 <nil>
}

func ExampleInspect() {
	f, _ := cue.ParseFile("a: b: c\nd: [for x in a if x != _|_ {x}]\n")
	n := 0
	cue.Inspect(f, func(node any) bool {
		if id, ok := node.(*cue.Ident); ok && id.Text == "x" {
			n++
		}
		return true
	})
	fmt.Println(n, "uses of x")
	// Output:
	// 3 uses of x
}

func ExampleComments() {
	src := "// The service.\nweb: 1 // inline\nnote: \"// not a comment\"\n"
	for _, c := range cue.Comments(src) {
		fmt.Printf("%d-%d %s\n", c.Start, c.End, c.Text)
	}
	// Output:
	// 0-15 // The service.
	// 23-32 // inline
}

func ExampleRecognize() {
	fmt.Println(cue.Recognize("a: [1, 2, 3]") == nil)
	fmt.Println(cue.Recognize("a: [1, 2, 3") == nil)
	// Output:
	// true
	// false
}

func ExampleAttribute_Split() {
	f, _ := cue.ParseFile(`x: 1 @json(x,omitempty) @go(X)`)
	for _, a := range f.Decls[0].(*cue.Field).Attrs {
		name, body := a.Split()
		fmt.Println(name, body)
	}
	// Output:
	// json x,omitempty
	// go X
}
