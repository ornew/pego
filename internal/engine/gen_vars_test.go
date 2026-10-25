package engine

import (
	"regexp"
	"strings"
	"testing"
)

func TestVarLines(t *testing.T) {
	got := varLines([]dvar{{"x1", "int"}, {"v2", "any"}, {"x3", "int"}, {"c", "*TActCtx"}, {"v4", "any"}})
	const want = "var x1, x3 int\nvar v2, v4 any\nvar c *TActCtx\n"
	if got != want {
		t.Errorf("varLines = %q, want %q", got, want)
	}
	if varLines(nil) != "" {
		t.Error("varLines of nothing is not empty")
	}
}

// A generated rule method declares the variables of one type together.
func TestGeneratedMethodsGroupVariables(t *testing.T) {
	eachMode(t, testGeneratedMethodsGroupVariables)
}

func testGeneratedMethodsGroupVariables(t *testing.T) {
	g, err := parseTest("rule main = (item \";\")* $$\nrule item = (\"a\" / \"b\")? (\"c\" / \"d\")? [a-z]* / [0-9]+ \"!\"\n")
	if err != nil {
		t.Fatal(err)
	}
	code, err := generateCode(g, GenOptions{Package: "p", Start: "main", Types: true})
	if err != nil {
		t.Fatal(err)
	}
	src := string(code)
	if !regexp.MustCompile(`(?m)^\s*var \w+(, \w+)+ int$`).MatchString(src) {
		t.Errorf("no grouped variable declaration in:\n%s", src)
	}
	seen := map[string]bool{}
	for _, f := range regexp.MustCompile(`(?m)^func [^\n]*\{\n((?:\s*var [^\n]*\n)+)`).FindAllStringSubmatch(src, -1) {
		clear(seen)
		for _, line := range strings.Split(strings.TrimSpace(f[1]), "\n") {
			typ := line[strings.LastIndex(line, " ")+1:]
			if seen[typ] {
				t.Errorf("a method declares the type %s in two statements:\n%s", typ, f[0])
			}
			seen[typ] = true
		}
	}
}
