package engine

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

func TestDropUnread(t *testing.T) {
	const src = `package p

func call() (int, bool) { return 1, true }

func f(in []int) int {
	var a, b, c, d int
	var ok, e bool
	var n int
	a = len(in)
	b = a
	c = len(in)
	_, _ = b, c
	d, ok = call()
	e = ok
	_ = e
	for range in {
		n++
	}
L1:
	n++
	if len(in) > 0 {
		goto L1
	}
	return d
}
`
	typeCheck(t, strings.Replace(src, "\t_, _ = b, c\n", "\t_, _ = b, c\n\t_, _ = a, n\n", 1))
	got, err := dropUnread([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	out := string(got)
	typeCheck(t, out)
	for _, gone := range []string{"a = ", "b = ", "c = ", "e = ", "_ = e", "_, _ = b, c", "n++", "var n", "var ok"} {
		if strings.Contains(out, gone) {
			t.Errorf("source keeps %q:\n%s", gone, out)
		}
	}
	// The effect of the call stays, with the unread result discarded; the read variable stays.
	for _, want := range []string{"d, _ = call()", "return d", "var d int", "goto L1", "L1:"} {
		if !strings.Contains(out, want) {
			t.Errorf("source lost %q:\n%s", want, out)
		}
	}
}

// A variable read in a closure or by its address is not dropped (negative control).
func TestDropUnreadKeepsReadVariables(t *testing.T) {
	const src = `package p

func f() int {
	var a, b, c int
	a = 1
	b = 2
	c = 3
	g := func() int { return a }
	p := &b
	*p = 4
	_ = c
	return g()
}
`
	got, err := dropUnread([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	typeCheck(t, string(got))
	for _, want := range []string{"a = 1", "b = 2", "var a, b"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("source lost %q:\n%s", want, got)
		}
	}
	if strings.Contains(string(got), "c = 3") {
		t.Errorf("source keeps the unread c:\n%s", got)
	}
}

// Generated functions have no blank lines, stray semicolons or closures that
// only wrap an expression when generated without work controls.
func TestGenerateStraightLineActions(t *testing.T) {
	g, err := parseTest(`type Leaf = { N int }
type Pair = { Left Leaf; Right Leaf }
rule main Pair = l:leaf "," r:leaf => Pair{Right: $r, Left: $l}
rule leaf Leaf = d:[0-9]+ => Leaf{N: len($d)}
`)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(g, GenOptions{Package: "p", Start: "main", Runtime: RuntimeImport, Types: true, Module: "example.com/p"})
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
	// Written in declaration order the fields are the arguments; out of order the temporaries keep
	// the order written.
	if !strings.Contains(src, "tmk_Leaf(c, true, c.Length(") {
		t.Errorf("fields in declaration order are not the arguments:\n%s", src)
	}
	m := regexp.MustCompile(`(f_\d+) := \w+; (f_\d+) := \w+; return tmk_Pair\(c, true, (f_\d+), (f_\d+)\)`).FindStringSubmatch(src)
	if m == nil || m[1] != m[4] || m[2] != m[3] {
		t.Errorf("fields out of order do not keep the order written:\n%s", src)
	}
	// Negative control: with work controls the steps are charged.
	files, err = Generate(g, GenOptions{Package: "p", Start: "main", Runtime: RuntimeImport, Types: true, Module: "example.com/p", WorkControls: true})
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
	const a, b = `A: list(1, 2)[len($x) + 3]`, `B: list(1)[len($y) + 7]`
	rule := func(first, second string) string {
		return "type Result = { A int; B int }\nrule main Result = x:\"a\"? y:\"b\"? => Result{" + first + ", " + second + "}\n"
	}
	cases := []genCase{
		{"typed/constructor_fields_declaration_order", rule(a, b), []string{"", "a", "b", "ab"}},
		{"typed/constructor_fields_written_out_of_order", rule(b, a), []string{"", "a", "b", "ab"}},
		{"typed/constructor_fields_partial", `type Result = { A int; B int; C int }
rule main Result = x:"a"? y:"b"? => Result{A: list(1, 2)[len($x) + 3], C: list(1)[len($y) + 7]}
`, []string{"", "a", "b", "ab"}},
	}
	testGeneratedTypesCorpus(t, filepath.Join(runtime.GOROOT(), "bin", "go"), cases, GenOptions{convertTypes: true}, 1)
	// Negative control: the two orders fail differently, so a parser that swapped them would
	// differ from the engine.
	first := compile(t, rule(a, b))
	second := compile(t, rule(b, a))
	_, err1 := first.Parse("main", "")
	_, err2 := second.Parse("main", "")
	if err1 == nil || err2 == nil || err1.Error() == err2.Error() {
		t.Fatalf("the order of evaluation is not observable: %v / %v", err1, err2)
	}
}
