package compile

import (
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/bytecode"
)

// TestVarDeclarationErrors checks the compile errors of variable declarations and of the uses
// of variables, with their messages.
func TestVarDeclarationErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"duplicate", "var x int\nvar x string\nrule main = \"a\"", "2:1: variable x is already defined"},
		{"true as a name", "var true bool\nrule main = \"a\"", "variable name true is reserved; it is a literal"},
		{"false as a name", "var false = false\nrule main = \"a\"", "variable name false is reserved; it is a literal"},
		{"nil as a name", "var nil int\nrule main = \"a\"", "variable name nil is reserved; it is a literal"},
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
	t.Parallel()
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

// TestLoadedBindingNeedsNoCheck checks that a crafted module whose ASSIGN asks for a check, as the
// earlier instruction set let it, is rejected, and that a binding of a value of another type than
// the variable's is memory-safe: variables hold any value and reads check the types they use.
func TestLoadedBindingNeedsNoCheck(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, "var x int\nrule main = {x := 1} \"a\" => x\n")
	m := prog.BytecodeModule()
	m.Code = append([]bytecode.Instr(nil), m.Code...)
	for i := range m.Code {
		if m.Code[i].Op == bytecode.OpBind {
			m.Code[i].C = 1
		}
	}
	if err := ValidateModule(m); err == nil {
		t.Error("an ASSIGN with a check operand was accepted")
	}

	// A binding of another type is rejected when the grammar is compiled, whatever reads the variable.
	for _, rule := range []string{"bound = {x := \"s\"} \"a\" => x", "sum = {x := \"s\"} \"a\" => x + 1"} {
		if got := compileMessage(t, "var x int\nrule "+rule+"\n"); !strings.Contains(got, "cannot use string as int in the binding of variable x") {
			t.Errorf("%s: %q", rule, got)
		}
	}
}
