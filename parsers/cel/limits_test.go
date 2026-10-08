package cel_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/cel"
)

func rep(s string, n int) string { return strings.Repeat(s, max(n, 0)) }

// shapes are the expressions of internal/refgen/limits.go, which grow with a number n.
var shapes = map[string]func(n int) string{
	"paren":    func(n int) string { return rep("(", n) + "1" + rep(")", n) },
	"list":     func(n int) string { return rep("[", n) + "1" + rep("]", n) },
	"map":      func(n int) string { return rep("{1:", n) + "1" + rep("}", n) },
	"msg":      func(n int) string { return rep("M{f:", n) + "1" + rep("}", n) },
	"call":     func(n int) string { return rep("f(", n) + "1" + rep(")", n) },
	"mcall":    func(n int) string { return "a" + rep(".f()", n) },
	"mcallarg": func(n int) string { return rep("a.f(", n) + "1" + rep(")", n) },
	"select":   func(n int) string { return "a" + rep(".b", n) },
	"index":    func(n int) string { return "a" + rep("[0]", n) },
	"indexin":  func(n int) string { return rep("a[", n) + "0" + rep("]", n) },
	"not":      func(n int) string { return rep("!", n) + "a" },
	"neg":      func(n int) string { return rep("-", n) + "a" },
	"add":      func(n int) string { return "1" + rep("+1", n) },
	"mul":      func(n int) string { return "1" + rep("*1", n) },
	"rel":      func(n int) string { return "1" + rep("<1", n) },
	"or":       func(n int) string { return "a" + rep("||a", n) },
	"and":      func(n int) string { return "a" + rep("&&a", n) },
	"cond":     func(n int) string { return rep("a?b:", n) + "c" },
	"condthen": func(n int) string { return rep("(a?", n) + "b" + rep(":c)", n) },
	"condcond": func(n int) string { return rep("(", n) + "a" + rep("?b:c)", n) },
	"args":     func(n int) string { return "f(" + rep("a,", n) + "a)" },
	"elems":    func(n int) string { return "[" + rep("1,", n) + "1]" },
	"entries":  func(n int) string { return "{" + rep("1:1,", n) + "1:1}" },
	"fields":   func(n int) string { return "M{" + rep("f:1,", n) + "f:1}" },
	"parenadd": func(n int) string { return rep("(1+", n) + "1" + rep(")", n) },
	"addparen": func(n int) string { return rep("(1)+", n) + "1" },
	"notparen": func(n int) string { return rep("!(", n) + "a" + rep(")", n) },
	"pad":      func(n int) string { return "1" + rep(" ", n-1) },
	"padrunes": func(n int) string { return "'" + rep("é", n-2) + "'" },
	"comment":  func(n int) string { return "1 //" + rep("x", n-4) },
}

// TestLimits checks, for each shape of expression, the sizes at which cel-go's parser starts to reject it (its limit
// of 250 on the depth of recursion and of 100,000 code points on the size) against ParseExpr.
func TestLimits(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(refDir(), "limits.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			t.Fatalf("bad line %q", line)
		}
		shape, ok := shapes[f[0]]
		n, err := strconv.Atoi(f[1])
		if !ok || err != nil {
			t.Fatalf("bad line %q", line)
		}
		seen[f[0]] = true
		_, perr := cel.ParseExpr(shape(n))
		if (perr == nil) != (f[2] == "ok") {
			t.Errorf("%s(%d): ParseExpr: %v, cel-go: %s", f[0], n, perr, f[2])
		}
	}
	for name := range shapes {
		if !seen[name] {
			t.Errorf("shape %s is not in limits.tsv", name)
		}
	}
}
