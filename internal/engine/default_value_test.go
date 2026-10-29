package engine

import (
	"errors"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
)

// compileDefault compiles src with default values and source information.
func compileDefault(t *testing.T, src string) *Program {
	t.Helper()
	return mustCompile(t, src, compile.Options{DefaultValue: true})
}

// checkDefaultValue checks that src compiled with default values gives the same results on every
// backend as stripped, the same grammar written without actions and declared types.
func checkDefaultValue(t *testing.T, src, stripped string, inputs ...string) {
	t.Helper()
	got, want := compileDefault(t, src), mustCompile(t, stripped)
	for _, in := range inputs {
		if a, b := result(got, in), result(want, in); a != b {
			t.Errorf("input %q\n default value %s\n stripped      %s", in, a, b)
		}
		checkBackends(t, got, "main", in)
	}
}

func TestDefaultValueDropsActions(t *testing.T) {
	checkDefaultValue(t, lines(
		"type Pair = { A node[string]; B node[string] }",
		"rule main Pair = a:\"x\" b:[a-z]+ #atomic => Pair{A: $a, B: $b}",
	), lines(
		"rule main = a:\"x\" b:[a-z]+ #atomic",
	), "xyz", "x", "y")
	checkDefaultValue(t, lines(
		"rule main = n:num \",\" m:num => list($n, $m)",
		"rule num = [0-9]+ #atomic",
	), lines(
		"rule main = n:num \",\" m:num",
		"rule num = [0-9]+ #atomic",
	), "1,2", "1,", "x")
	// An action that returns a capture is dropped too.
	checkDefaultValue(t, "rule main = x:\"a\" \"b\" => $x", "rule main = x:\"a\" \"b\"", "ab")
	// The result is the default node tree, not the action's value.
	prog := compileDefault(t, "rule main = \"a\" \"b\" => 42")
	if got := result(prog, "ab"); strings.Contains(got, "42") {
		t.Errorf("result %s has the value of the action", got)
	}
	if !prog.DefaultValue() {
		t.Error("DefaultValue() is false")
	}
	if mustCompile(t, "rule main = \"a\"").DefaultValue() {
		t.Error("DefaultValue() is true without the option")
	}
}

func TestDefaultValueSkipsTypeErrors(t *testing.T) {
	// The action does not type-check against the declared type.
	src := lines(
		"type T = { A string }",
		"rule main T = x:\"x\" => T{A: 1, Zzz: $x}",
	)
	if _, err := Compile(mustParse(src), compile.Options{}); err == nil {
		t.Fatal("the grammar compiles without default values; the test needs a type error")
	}
	checkDefaultValue(t, src, "rule main = x:\"x\"", "x", "y")
	// Errors in expressions are still reported.
	if _, err := Compile(mustParse("rule main = missing => 1"), compile.Options{DefaultValue: true}); err == nil {
		t.Error("an undefined rule compiled with default values")
	}
}

func TestDefaultValueKeepsPredicates(t *testing.T) {
	src := "rule main = n:[0-9]+ #atomic &{ len(text($n)) < 3 } => int(text($n))"
	checkDefaultValue(t, src, "rule main = n:[0-9]+ #atomic &{ len(text($n)) < 3 }", "1", "12", "123")
	prog := compileDefault(t, src)
	if got := result(prog, "123"); !strings.HasPrefix(got, "error: ") {
		t.Errorf("the predicate did not reject the input: %s", got)
	}
	if got := result(prog, "12"); strings.HasPrefix(got, "error: ") {
		t.Errorf("the predicate rejected a matching input: %s", got)
	}
}

// A predicate can read the value that an action made, so the rule behind a capture a predicate
// reads keeps its action, and every other rule loses it.
func TestDefaultValueKeepsActionsPredicatesRead(t *testing.T) {
	src := lines(
		"type Name = { Text string }",
		"rule main = a:name \"=\" b:name &{ $a.Text == $b.Text } c:other => 1",
		"rule name Name = t:([a-z]+) #atomic => Name{Text: text($t)}",
		"rule other = \"!\" => 2",
	)
	prog := compileDefault(t, src)
	if got, want := result(prog, "a=a!"), `(a:Name{Text: "a"} "=" b:Name{Text: "a"} c:"!"@other)@main`; got != want {
		t.Errorf("got %s\n want %s", got, want)
	}
	if got := result(prog, "a=b!"); !strings.HasPrefix(got, "error: ") {
		t.Errorf("the predicate did not reject the input: %s", got)
	}
	checkBackends(t, prog, "main", "a=a!")
}

func TestDefaultValueOperatorsRule(t *testing.T) {
	src := lines(
		"type Num = { Text string }",
		"type Bin = { L node[_]; R node[_] }",
		"rule main = e $$",
		"rule e = #operators {",
		"    operand t:([0-9]+) #atomic => Num{Text: text($t)}",
		"    operand \"(\" x:e \")\" => $x",
		"    level add { infix left \"+\" => Bin{L: $lhs, R: $rhs} }",
		"    level mul { infix left \"*\" => Bin{L: $lhs, R: $rhs} }",
		"    level neg { prefix \"-\" => $rhs }",
		"    level call { postfix \"!\" => $lhs }",
		"}",
	)
	stripped := lines(
		"rule main = e $$",
		"rule e = #operators {",
		"    operand t:([0-9]+) #atomic",
		"    operand \"(\" x:e \")\"",
		"    level add { infix left \"+\" }",
		"    level mul { infix left \"*\" }",
		"    level neg { prefix \"-\" }",
		"    level call { postfix \"!\" }",
		"}",
	)
	checkDefaultValue(t, src, stripped, "1", "1+2*3", "(1+2)*3", "-1!", "1+", "+")
}

func TestDefaultValueKeepsRecovery(t *testing.T) {
	src := lines(
		"rule main = stmt* $$",
		"rule stmt = ([a-z]+ #atomic \";\") #recover(skip=[^;]+ \";\") => 1",
	)
	stripped := lines(
		"rule main = stmt* $$",
		"rule stmt = ([a-z]+ #atomic \";\") #recover(skip=[^;]+ \";\")",
	)
	checkDefaultValue(t, src, stripped, "a;b;", "a;1;b;", "1")
	prog := compileDefault(t, src)
	n, err := prog.Parse("main", "a;1;b;")
	var errs SyntaxErrors
	if n == nil || err == nil || !errors.As(err, &errs) || len(errs) != 1 {
		t.Errorf("got %v, %v; want a tree and one recovered error", n, err)
	}
}

func TestDefaultValueCompiledRoundTrip(t *testing.T) {
	src := lines(
		"type T = { A string }",
		"rule main T = x:\"x\" => T{A: 1}",
	)
	prog, err := Compile(mustParse(src), compile.Options{DefaultValue: true})
	if err != nil {
		t.Fatal(err)
	}
	want := result(prog, "x")
	for _, mode := range valueModes {
		loaded := saveLoad(t, prog, "main", mode)
		if !loaded.DefaultValue() {
			t.Errorf("%s mode: the loaded program lost the option", mode)
		}
		if got := resultWith(loaded, "x", servedBy(mode, ParseOptions{})); got != want {
			t.Errorf("%s mode: got %s, want %s", mode, got, want)
		}
	}
}

func TestDefaultValueGeneratedRejectsTypes(t *testing.T) {
	eachMode(t, testDefaultValueGeneratedRejectsTypes)
}

func testDefaultValueGeneratedRejectsTypes(t *testing.T) {
	g := mustParse("rule main = \"a\" => 1")
	if _, err := generateCode(g, gen.GenOptions{Package: "p", Start: "main", Types: true, DefaultValue: true}); err == nil || !strings.Contains(err.Error(), "default values") {
		t.Errorf("typed output with default values: %v", err)
	}
	if _, err := generateTSBundle(g, gen.GenOptions{Start: "main", Types: true, DefaultValue: true}); err == nil || !strings.Contains(err.Error(), "default values") {
		t.Errorf("typed TypeScript output with default values: %v", err)
	}
	if _, err := generateCode(g, gen.GenOptions{Package: "p", Start: "main", DefaultValue: true}); err != nil {
		t.Errorf("generation with default values: %v", err)
	}
}
