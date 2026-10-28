package engine

import (
	"regexp"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
)

func TestVarLinesExcept(t *testing.T) {
	// The first variable of each type places the type, so dead x1 keeps int before any.
	vs := []dvar{{"x1", "int"}, {"v2", "any"}, {"x3", "int"}, {"c", "*TActCtx"}, {"v4", "any"}}
	for _, tc := range []struct {
		name        string
		dead, quiet map[string]bool
		want        string
	}{
		{"all declared", nil, nil, "var x1, x3 int\nvar v2, v4 any\nvar c *TActCtx\n"},
		{"a dead variable leaves the group", map[string]bool{"x3": true}, nil, "var x1 int\nvar v2, v4 any\nvar c *TActCtx\n"},
		{"a dead variable still places its type", map[string]bool{"x1": true}, nil, "var x3 int\nvar v2, v4 any\nvar c *TActCtx\n"},
		{"a dead type leaves no declaration", map[string]bool{"x1": true, "x3": true}, nil, "var v2, v4 any\nvar c *TActCtx\n"},
		{"a dead first variable keeps the order", map[string]bool{"v2": true}, nil, "var x1, x3 int\nvar v4 any\nvar c *TActCtx\n"},
		{"a whole type dead", map[string]bool{"c": true}, nil, "var x1, x3 int\nvar v2, v4 any\n"},
		{"quiet variables are silenced after their declaration", nil, map[string]bool{"x3": true, "v2": true, "v4": true}, "var x1, x3 int\n_ = x3\nvar v2, v4 any\n_, _ = v2, v4\nvar c *TActCtx\n"},
		{"a variable that is dead is not silenced", map[string]bool{"x3": true}, map[string]bool{"x3": true}, "var x1 int\nvar v2, v4 any\nvar c *TActCtx\n"},
	} {
		if got := varLinesExcept(vs, tc.dead, tc.quiet); got != tc.want {
			t.Errorf("%s: varLinesExcept = %q, want %q", tc.name, got, tc.want)
		}
	}
	if varLinesExcept(nil, nil, nil) != "" {
		t.Error("varLinesExcept of nothing is not empty")
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
	code, err := generateCode(g, gen.GenOptions{Package: "p", Start: "main", Types: true})
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
