package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// compileMessage compiles src and returns the error message, or "" if it compiles.
func compileMessage(t *testing.T, src string, opts ...Options) string {
	t.Helper()
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}
	_, err = Compile(g, o)
	if err == nil {
		return ""
	}
	return err.Error()
}

// TestVarDeclarationErrors checks the compile errors of variable declarations and of the uses
// of variables, with their messages.
func TestVarDeclarationErrors(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"duplicate", "var x int\nvar x string\nrule main = \"a\"", "2:1: variable x is already defined"},
		{"unknown type", "var x float\nrule main = \"a\"", "variable x must have type int, string, or bool, got float"},
		{"node type", "var x node[string]\nrule main = \"a\"", "variable x must have type int, string, or bool, got node[string]"},
		{"list type", "var x []int\nrule main = \"a\"", "variable x must have type int, string, or bool, got []int"},
		{"optional type", "var x *int\nrule main = \"a\"", "variable x must have type int, string, or bool, got *int"},
		{"nil initial value", "var x = nil\nrule main = \"a\"", "variable x cannot be nil; its type is int, string, or bool"},
		{"nil with a type", "var x int = nil\nrule main = \"a\"", "variable x cannot be nil; its type is int, string, or bool"},
		{"no type and no value", "var x\nrule main = \"a\"", "variable x needs a type or an initial value"},
		{"initial value of another type", "var x int = \"a\"\nrule main = \"a\"", "cannot use string as int in the initial value of variable x"},
		{"initial bool for string", "var x string = true\nrule main = \"a\"", "cannot use bool as string in the initial value of variable x"},
		{"initial value is not a literal", "var x int = 1 + 1\nrule main = \"a\"", "the initial value of variable x must be an int, string, or bool literal"},
		{"initial value reads a variable", "var x int\nvar y int = x\nrule main = \"a\"", "the initial value of variable y must be an int, string, or bool literal"},
		{"read of an undeclared variable", "rule main = {x == 1} \"a\"", "1:14: undefined variable x; declare it with var x at the top level"},
		{"binding of an undeclared variable", "rule main = {x := 1} \"a\"", "1:14: undefined variable x; declare it with var x at the top level"},
		{"read in an action", "type T = { N int }\nrule main = \"a\" => T{N: x}", "undefined variable x; declare it with var x at the top level"},
		{"binding of the wrong type", "var x int\nrule main = {x := \"a\"} \"a\"", "cannot use string as int in the binding of variable x"},
		{"binding of a node", "var x string\nrule main = n:\"a\" {x := $n} \"b\"", "cannot use node[string] as string in the binding of variable x"},
		{"bool for int", "var x int\nrule main = {x := x == 1} \"a\"", "cannot use bool as int in the binding of variable x"},
		{"read with the declared type", "var x string\nrule main = {x + 1 == 2} \"a\"", "invalid operands string and int for +"},
		{"field of the declared type", "type V = { N int }\nvar x string\nrule main = \"a\" => V{N: x}", "cannot use string as int in field N of V"},
		{"arithmetic on a bool", "var x bool\nrule main = {x + 1 == 2} \"a\"", "invalid operands bool and int for +"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := compileMessage(t, tc.src); !strings.Contains(got, tc.want) {
				t.Fatalf("got %q; want an error containing %q", got, tc.want)
			}
		})
	}
}

// TestVarDeclarationsCompile checks the declarations that are valid, with and without a type
// and an initial value.
func TestVarDeclarationsCompile(t *testing.T) {
	for _, src := range []string{
		"var d int = 0\nrule main = \"a\"",
		"var m string\nrule main = \"a\"",
		"var s = false\nrule main = \"a\"",
		"var n = -4\nvar t string = \"x\"\nvar b bool = true\nrule main = \"a\"",
		"rule main = \"a\"\nvar late int\nrule other = {late == 0} \"b\"",
		// A variable declaration may follow rules and the package clause.
		"package p\nvar x int\nrule main = {x := x + 1} \"a\"",
	} {
		if got := compileMessage(t, src); got != "" {
			t.Errorf("%q: %v", src, got)
		}
	}
}

// TestVarInitialValues checks that every parse starts from the declared initial values, through
// every entry point and every backend, and that a grammar can read them before it assigns.
func TestVarInitialValues(t *testing.T) {
	const src = `type R = { N int; S string; B bool; M int; Z int }
var n int = 7
var s = "init"
var b bool
var m = -3
var z int
rule main R = "a" => R{N: n, S: s, B: b, M: m, Z: z}
rule other R = {n := n + 1} "b" => R{N: n, S: s, B: b, M: m, Z: z}
`
	prog := compile(t, src)
	const want = `R{B: false, M: -3, N: 7, S: "init", Z: 0}`
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			opts := ParseOptions{Backend: backend, Unit: unit}
			n, err := prog.ParseWith("main", "a", opts)
			if err != nil || n.String() != want {
				t.Errorf("%v/%v main: got %v, %v; want %s", backend, unit, n, err, want)
			}
			// Another start rule starts from the same values.
			n, err = prog.ParseWith("other", "b", opts)
			if err != nil || n.String() != `R{B: false, M: -3, N: 8, S: "init", Z: 0}` {
				t.Errorf("%v/%v other: got %v, %v", backend, unit, n, err)
			}
			// A parse that follows another one is not affected by its bindings.
			n, err = prog.ParseWith("main", "a", opts)
			if err != nil || n.String() != want {
				t.Errorf("%v/%v main again: got %v, %v", backend, unit, n, err)
			}
		}
	}
	// A document and a stream start from the initial values as well.
	doc, err := prog.NewDocument("main", "a")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := doc.Parse(); err != nil || n.String() != want {
		t.Errorf("document: got %v, %v", n, err)
	}
	// A saved program keeps them, with and without its syntax tree.
	for _, o := range []MarshalOptions{{}, {OmitAST: true}} {
		data, err := prog.MarshalBinaryWith("main", o)
		if err != nil {
			t.Fatal(err)
		}
		loaded, start, err := LoadProgram(data, Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			if o.OmitAST && backend == Closure {
				continue
			}
			n, err := loaded.ParseWith(start, "a", ParseOptions{Backend: backend})
			if err != nil || n.String() != want {
				t.Errorf("loaded (omit AST %t) %v: got %v, %v", o.OmitAST, backend, n, err)
			}
		}
	}
	// The module lists the variables with their types and values.
	vars := prog.Module().Vars
	if len(vars) != 5 {
		t.Fatalf("module variables: %+v", vars)
	}
}

// TestVarTypedReads checks that reads and bindings have the declared type: a read can be used
// where its type is accepted, and the checks do not apply without the type checker.
func TestVarTypedReads(t *testing.T) {
	prog := compile(t, `type V = { N int; S string; B bool }
var d int
var tag string = "t"
var on bool = true
rule main V = {d := d + 2} {tag := tag + "!"} "a" => V{N: d * 3, S: tag, B: on && d == 2}
`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		n, err := prog.ParseWith("main", "a", ParseOptions{Backend: backend})
		if err != nil || n.String() != `V{B: true, N: 6, S: "t!"}` {
			t.Errorf("%v: got %v, %v", backend, n, err)
		}
	}
	// Without the type checker the names are still resolved.
	if got := compileMessage(t, "rule main = {x := 1} \"a\"", Options{NoTypeCheck: true}); !strings.Contains(got, "undefined variable x") {
		t.Errorf("unchecked: %q", got)
	}
}

// TestGrammarWithoutVariables checks that a grammar without variable declarations has no
// variable table and no variables in its module.
func TestGrammarWithoutVariables(t *testing.T) {
	prog := compile(t, `rule main = "a" ("b" / "c")* $$`)
	if prog.vars != nil || len(prog.Module().Vars) != 0 {
		t.Fatalf("variables: %v, %v", prog.vars, prog.Module().Vars)
	}
	for _, in := range []string{"abc", "a", "ab?"} {
		a, errA := prog.ParseWith("main", in, ParseOptions{Backend: Closure})
		b, errB := prog.ParseWith("main", in, ParseOptions{Backend: Bytecode})
		if resultJSON(a, errA) != resultJSON(b, errB) {
			t.Errorf("%q: %s / %s", in, resultJSON(a, errA), resultJSON(b, errB))
		}
	}
}
