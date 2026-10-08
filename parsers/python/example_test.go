package python_test

import (
	"errors"
	"fmt"

	"github.com/ornew/pego/parsers/python"
)

func ExampleParseModule() {
	m, err := python.ParseModule("import os\n\ndef greet(name: str, *, loud=False) -> str:\n    return f'hello {name}'\n")
	if err != nil {
		panic(err)
	}
	for _, s := range m.Body {
		switch s := s.(type) {
		case *python.Import:
			fmt.Printf("import of %d module at %d-%d\n", len(s.Names), s.Start, s.End)
		case *python.FunctionDef:
			fmt.Printf("def %s with %d parameters and %d keyword-only\n", s.Name, len(s.Args.Args), len(s.Args.KwOnlyArgs))
		}
	}
	// Output:
	// import of 1 module at 0-9
	// def greet with 1 parameters and 1 keyword-only
}

func ExampleDump() {
	m, err := python.ParseModule("x = [i * 2 for i in range(3) if i]")
	if err != nil {
		panic(err)
	}
	// The same text as Python's ast.dump(ast.parse(source)).
	fmt.Println(python.Dump(m))
	// Output:
	// Module(body=[Assign(targets=[Name(id='x', ctx=Store())], value=ListComp(elt=BinOp(left=Name(id='i', ctx=Load()), op=Mult(), right=Constant(value=2)), generators=[comprehension(target=Name(id='i', ctx=Store()), iter=Call(func=Name(id='range', ctx=Load()), args=[Constant(value=3)]), ifs=[Name(id='i', ctx=Load())], is_async=0)]))])
}

func ExampleDumpWithPositions() {
	src := "a = 1\nb = (a +\n     2)\n"
	m, err := python.ParseModule(src)
	if err != nil {
		panic(err)
	}
	// Lines count from 1 and columns (in bytes) from 0, as in Python's ast.dump(..., include_attributes=True).
	fmt.Println(python.DumpWithPositions(m.Body[1], src))
	// Output:
	// Assign(targets=[Name(id='b', ctx=Store(), lineno=2, col_offset=0, end_lineno=2, end_col_offset=1)], value=BinOp(left=Name(id='a', ctx=Load(), lineno=2, col_offset=5, end_lineno=2, end_col_offset=6), op=Add(), right=Constant(value=2, lineno=3, col_offset=5, end_lineno=3, end_col_offset=6), lineno=2, col_offset=5, end_lineno=3, end_col_offset=6), lineno=2, col_offset=0, end_lineno=3, end_col_offset=7)
}

func ExampleInspect() {
	m, err := python.ParseModule("print(len(items) + total.count)")
	if err != nil {
		panic(err)
	}
	// Every name in the module, in source order.
	python.Inspect(m, func(n any) bool {
		if name, ok := n.(*python.Name); ok {
			fmt.Println(name.Id, name.Ctx)
		}
		return true
	})
	// Output:
	// print Load
	// len Load
	// items Load
	// total Load
}

func ExampleConstant_Value() {
	m, err := python.ParseModule("values = [0x_ff, 1_000.5e-1, 3.5j, 'a' 'b\\n', b'\\x00\\xff', None, ...]")
	if err != nil {
		panic(err)
	}
	list := m.Body[0].(*python.Assign).Value.(*python.ListExpr)
	for _, e := range list.Elts {
		v, err := e.(*python.Constant).Value()
		if err != nil {
			panic(err)
		}
		fmt.Printf("%T %v\n", v, v)
	}
	// Output:
	// *big.Int 255
	// float64 100.05
	// complex128 (0+3.5i)
	// string ab
	//
	// []uint8 [0 255]
	// <nil> <nil>
	// python.EllipsisType {}
}

func ExampleParseModule_syntaxError() {
	_, err := python.ParseModule("class A:\npass\n")
	var se *python.SyntaxError
	if errors.As(err, &se) {
		fmt.Println(se.Line, se.Col, se.Message())
	}
	// ParseModule also makes the checks that CPython makes outside its grammar.
	_, err = python.ParseModule("from __future__ import braces\n")
	fmt.Println(err)
	// Output:
	// 2 1 expected an indented block
	// 1:24: not a chance
}

func ExampleLineIndex() {
	src := "s = 'ä' + x\n"
	m, err := python.ParseModule(src)
	if err != nil {
		panic(err)
	}
	// Spans are in code points; CPython's columns are in UTF-8 bytes.
	x := m.Body[0].(*python.Assign).Value.(*python.BinOp).Right.(*python.Name)
	line, col := python.NewLineIndex(src).Position(x.Start)
	fmt.Println(x.Start, x.End, "is line", line, "column", col)
	// Output: 10 11 is line 1 column 11
}
