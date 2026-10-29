package gogen_test

import (
	goast "go/ast"
	"go/importer"
	goparser "go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

// typeCheck fails the test if src does not compile (it reports unused variables).
func typeCheck(t *testing.T, src string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := goparser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	conf := types.Config{Importer: importer.Default()}
	if _, err := conf.Check("p", fset, []*goast.File{f}, nil); err != nil {
		t.Fatalf("%v\n%s", err, src)
	}
}

// Generated functions have no blank lines, stray semicolons or closures that
// only wrap an expression when generated without work controls.
func TestGenerateStraightLineActions(t *testing.T) {
	t.Parallel()
	g, err := parseTest(`type Leaf = { N int }
type Pair = { Left Leaf; Right Leaf }
rule main Pair = l:leaf "," r:leaf => Pair{Right: $r, Left: $l}
rule leaf Leaf = d:[0-9]+ => Leaf{N: len($d)}
`)
	if err != nil {
		t.Fatal(err)
	}
	files, err := gentest.Generate(t, g, gen.GenOptions{Package: "p", Start: "main", Runtime: gen.RuntimeImport, Types: true, Module: "example.com/p"})
	if err != nil {
		t.Fatal(err)
	}
	src := string(files[0].Content)
	fn := ""
	for i, line := range strings.Split(src, "\n") {
		switch {
		case strings.HasPrefix(line, "func ") && strings.HasSuffix(line, "{"):
			fn = line
		case strings.HasPrefix(line, "}"):
			fn = ""
		case fn != "" && line == "" && !strings.HasPrefix(fn, "func init"):
			t.Errorf("line %d: blank line inside %s", i+1, fn)
		}
	}
	for _, bad := range []string{"{ ;", "StepWork", "InputWork", "func() any { return "} {
		if strings.Contains(src, bad) {
			t.Errorf("generated source contains %q", bad)
		}
	}
	// Written in declaration order the fields are the arguments; out of order the temporaries, assigned
	// before the call, keep the order written, without a function literal.
	if strings.Contains(src, "func() any") {
		t.Errorf("generated source contains a function literal around a term:\n%s", src)
	}
	if !strings.Contains(src, "tmk_Leaf(c, true, c.Length(") {
		t.Errorf("fields in declaration order are not the arguments:\n%s", src)
	}
	m := regexp.MustCompile(`(f_\d+) := \w+\n\s*(f_\d+) := \w+\n\s*v = tmk_Pair\(c, true, (f_\d+), (f_\d+)\)`).FindStringSubmatch(src)
	if m == nil || m[1] != m[4] || m[2] != m[3] {
		t.Errorf("fields out of order do not keep the order written:\n%s", src)
	}
	// Negative control: with work controls the steps are charged.
	files, err = gentest.Generate(t, g, gen.GenOptions{Package: "p", Start: "main", Runtime: gen.RuntimeImport, Types: true, Module: "example.com/p", WorkControls: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(files[0].Content), "StepWork") {
		t.Error("generated source with work controls charges no steps")
	}
}

// A typed constructor given its fields out of declaration order evaluates them in the order
// written, so the error of the field written first wins; in declaration order they are
// arguments of the constructor call. The two fields fail with different errors, so the generated
// parser can only agree with the engine if it evaluates them in the same order.
func TestGeneratedTypedConstructorEvaluationOrder(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedTypedConstructorEvaluationOrder)
}

func testGeneratedTypedConstructorEvaluationOrder(t *testing.T) {
	const a, b = `A: list(1, 2)[len($x) + 3]`, `B: list(1)[len($y) + 7]`
	rule := func(first, second string) string {
		return "type Result = { A int; B int }\nrule main Result = x:\"a\"? y:\"b\"? => Result{" + first + ", " + second + "}\n"
	}
	cases := []gentest.Case{
		{Name: "typed/constructor_fields_declaration_order", Src: rule(a, b), Inputs: []string{"", "a", "b", "ab"}},
		{Name: "typed/constructor_fields_written_out_of_order", Src: rule(b, a), Inputs: []string{"", "a", "b", "ab"}},
		{Name: "typed/constructor_fields_partial", Src: `type Result = { A int; B int; C int }
rule main Result = x:"a"? y:"b"? => Result{A: list(1, 2)[len($x) + 3], C: list(1)[len($y) + 7]}
`, Inputs: []string{"", "a", "b", "ab"}},
	}
	gentest.GeneratedTypesCorpus(t, filepath.Join(runtime.GOROOT(), "bin", "go"), cases, gen.GenOptions{ConvertTypes: true}, 1)
	// Negative control: the two orders fail differently, so a parser that swapped them would
	// differ from the engine.
	first := mustCompile(t, rule(a, b))
	second := mustCompile(t, rule(b, a))
	_, err1 := first.Parse("main", "")
	_, err2 := second.Parse("main", "")
	if err1 == nil || err2 == nil || err1.Error() == err2.Error() {
		t.Fatalf("the order of evaluation is not observable: %v / %v", err1, err2)
	}
}

// TestGeneratedTermStatementsEvaluationOrder checks the order of evaluation of the operands of a term when one
// of them needs statements before its expression (the fused concat pushes its elements one by one). The
// operands before it are assigned to temporaries first, so that their error still wins. The two operands
// fail with different errors, so the generated parser can only agree with the engine if it evaluates them in
// the order written.
func TestGeneratedTermStatementsEvaluationOrder(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedTermStatementsEvaluationOrder)
}

func testGeneratedTermStatementsEvaluationOrder(t *testing.T) {
	const a, b = `list(1)[len($x) + 3]`, `len(concat(list(1), list(list(1)[len($y) + 7])))`
	rule := func(first, second string) string {
		return "type Result = { N int }\nrule main Result = x:\"a\"? y:\"b\"? => Result{N: list(" + first + ", " + second + ")[0]}\n"
	}
	sum := func(first, second string) string {
		return "type Result = { N int }\nrule main Result = x:\"a\"? y:\"b\"? => Result{N: " + first + " + " + second + "}\n"
	}
	cases := []gentest.Case{
		{Name: "typed/statements_after_operand", Src: rule(a, b), Inputs: []string{"", "a", "b", "ab"}},
		{Name: "typed/statements_in_first_operand", Src: rule(b, a), Inputs: []string{"", "a", "b", "ab"}},
		{Name: "typed/statements_after_binary_operand", Src: sum(a, b), Inputs: []string{"", "a", "b", "ab"}},
		{Name: "typed/statements_in_first_binary_operand", Src: sum(b, a), Inputs: []string{"", "a", "b", "ab"}},
	}
	gentest.GeneratedTypesCorpus(t, filepath.Join(runtime.GOROOT(), "bin", "go"), cases, gen.GenOptions{ConvertTypes: true}, 1)
	// Negative control: the two orders fail differently.
	first := mustCompile(t, rule(a, b))
	second := mustCompile(t, rule(b, a))
	_, err1 := first.Parse("main", "")
	_, err2 := second.Parse("main", "")
	if err1 == nil || err2 == nil || err1.Error() == err2.Error() {
		t.Fatalf("the order of evaluation is not observable: %v / %v", err1, err2)
	}
}
