package main

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"cel.dev/cel-go/parser"
)

// celGoParserDir returns the directory of the parser package of the cel-go module that this module uses.
func celGoParserDir() string {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "cel.dev/cel-go").Output()
	if err != nil {
		fatal(err)
	}
	return strings.TrimSpace(string(out)) + "/parser"
}

// shapes are the expressions whose size is a number n, to find the limits of cel-go: its recursion depth, its limit
// on the size of an expression and the places where they differ from the grammar. The tests build the same
// expressions (shapes in limits_test.go) and check what limits.tsv says: shape, n, and ok or ERR.
var shapes = []struct {
	name string
	f    func(n int) string
}{
	{"paren", func(n int) string { return rep("(", n) + "1" + rep(")", n) }},
	{"list", func(n int) string { return rep("[", n) + "1" + rep("]", n) }},
	{"map", func(n int) string { return rep("{1:", n) + "1" + rep("}", n) }},
	{"msg", func(n int) string { return rep("M{f:", n) + "1" + rep("}", n) }},
	{"call", func(n int) string { return rep("f(", n) + "1" + rep(")", n) }},
	{"mcall", func(n int) string { return "a" + rep(".f()", n) }},
	{"mcallarg", func(n int) string { return rep("a.f(", n) + "1" + rep(")", n) }},
	{"select", func(n int) string { return "a" + rep(".b", n) }},
	{"index", func(n int) string { return "a" + rep("[0]", n) }},
	{"indexin", func(n int) string { return rep("a[", n) + "0" + rep("]", n) }},
	{"not", func(n int) string { return rep("!", n) + "a" }},
	{"neg", func(n int) string { return rep("-", n) + "a" }},
	{"add", func(n int) string { return "1" + rep("+1", n) }},
	{"mul", func(n int) string { return "1" + rep("*1", n) }},
	{"rel", func(n int) string { return "1" + rep("<1", n) }},
	{"or", func(n int) string { return "a" + rep("||a", n) }},
	{"and", func(n int) string { return "a" + rep("&&a", n) }},
	{"cond", func(n int) string { return rep("a?b:", n) + "c" }},
	{"condthen", func(n int) string { return rep("(a?", n) + "b" + rep(":c)", n) }},
	{"condcond", func(n int) string { return rep("(", n) + "a" + rep("?b:c)", n) }},
	{"args", func(n int) string { return "f(" + rep("a,", n) + "a)" }},
	{"elems", func(n int) string { return "[" + rep("1,", n) + "1]" }},
	{"entries", func(n int) string { return "{" + rep("1:1,", n) + "1:1}" }},
	{"fields", func(n int) string { return "M{" + rep("f:1,", n) + "f:1}" }},
	{"parenadd", func(n int) string { return rep("(1+", n) + "1" + rep(")", n) }},
	{"addparen", func(n int) string { return rep("(1)+", n) + "1" }},
	{"notparen", func(n int) string { return rep("!(", n) + "a" + rep(")", n) }},
	{"pad", func(n int) string { return "1" + rep(" ", n-1) }},
	{"padrunes", func(n int) string { return "'" + rep("é", n-2) + "'" }},
	{"comment", func(n int) string { return "1 //" + rep("x", n-4) }},
}

func rep(s string, n int) string {
	if n < 0 {
		n = 0
	}
	return strings.Repeat(s, n)
}

// writeLimits writes limits.tsv: for each shape the sizes around the first one that cel-go rejects, and a few others.
func writeLimits(p *parser.Parser, path string) {
	var lines []string
	for _, s := range shapes {
		accepted := func(n int) bool {
			res, ok := reference(p, s.f(n))
			return ok && !strings.HasPrefix(res, "ERR")
		}
		sizes := map[int]bool{}
		for _, n := range []int{3, 4, 5, 10, 12, 24, 32, 64, 100, 128, 200, 1000} {
			sizes[n] = true
		}
		hi := 1 << 17
		if s.name == "pad" || s.name == "padrunes" || s.name == "comment" {
			sizes[100000], sizes[100001], sizes[99999] = true, true, true
		} else {
			// The smallest n that is rejected, by bisection (the limits are monotone).
			lo := 1
			if accepted(hi) {
				fmt.Printf("limits: %s accepted up to %d\n", s.name, hi)
			} else {
				for lo+1 < hi {
					mid := (lo + hi) / 2
					if accepted(mid) {
						lo = mid
					} else {
						hi = mid
					}
				}
			}
			fmt.Printf("limits: %-9s first rejected at n=%d\n", s.name, hi)
			for d := -3; d <= 3; d++ {
				sizes[hi+d] = true
			}
			sizes[hi*2] = true
		}
		var ns []int
		for n := range sizes {
			if n >= 2 {
				ns = append(ns, n)
			}
		}
		sort.Ints(ns)
		for _, n := range ns {
			res := "ok"
			if !accepted(n) {
				res = "ERR"
			}
			lines = append(lines, fmt.Sprintf("%s\t%d\t%s", s.name, n, res))
		}
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		fatal(err)
	}
}
