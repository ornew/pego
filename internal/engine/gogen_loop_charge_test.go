package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
)

// TestGeneratedLoopCharge checks that every repetition of a list built-in is charged. The terms of an
// action carry no charge, so the loops of the runtime are what bounds the work of an action: map, filter,
// foldl and foldr charge one step per element (map and filter also charge the elements of their result), and a lambda body, called once per element (nested lambdas:
// once per element of each loop), charges nothing itself.
//
// For each built-in and for nested lambdas it generates a parser whose action uses the loop, and one
// whose action is len($xs). It asserts that the first needs at least one step per lambda call more than the
// second, that this grows by at least one per call from the input of N to that of 2N, and that a MaxSteps
// halfway between ends the first with ErrWorkLimit on the large input while the second completes.
// The negative control runs the same checks against a copy of the runtime without the per-element charge of
// the loops: every case has to violate them.
func TestGeneratedLoopCharge(t *testing.T) {
	needsWorkControls(t)
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	cases := []loopCase{
		{"map", `map($xs, (x) => 1)`, 2000, func(n int) int { return n }, true},
		{"filter", `filter($xs, (x) => false)`, 2000, func(n int) int { return n }, true},
		{"foldl", `foldl(0, $xs, (a, x) => a)`, 2000, func(n int) int { return n }, false},
		{"foldr", `foldr(0, $xs, (a, x) => a)`, 2000, func(n int) int { return n }, false},
		{"nested", `map($xs, (x) => foldl(0, $xs, (a, y) => a))`, 100, func(n int) int { return n + n*n }, true},
	}
	for _, typed := range []bool{false, true} {
		form := map[bool]string{false: "node", true: "typed"}[typed]
		t.Run(form+"/runtime", func(t *testing.T) {
			if problems := loopChargeProblems(t, cases, runtimeCopy(), typed); len(problems) != 0 {
				t.Error(strings.Join(problems, "\n"))
			}
		})
		t.Run(form+"/negative control", func(t *testing.T) {
			// Without the charge of the loops, a lambda call is not charged at all.
			loop := regexp.MustCompile(`(func \(c \*T?ActCtx\) (?:PushMap|FilterList)\([^\n]*\{\n(?:[^\n]*\n)*?)\t*c\.P\.StepWork\(\)\n`)
			fold := regexp.MustCompile(`(func \(c \*T?ActCtx\) foldControlled\([^\n]*\{\n(?:[^\n]*\n)*?)\t*c\.P\.StepWork\(\)\n`)
			patched := 0
			dir := runtimeCopyWith(t, func(name, src string) string {
				for _, re := range []*regexp.Regexp{loop, fold} {
					src = re.ReplaceAllStringFunc(src, func(m string) string {
						patched++
						return re.FindStringSubmatch(m)[1]
					})
				}
				return src
			}, nil)
			if patched != 6 {
				t.Fatalf("patched %d loops of the runtime, want 6 (PushMap, FilterList and foldControlled of both contexts)", patched)
			}
			problems := loopChargeProblems(t, cases, dir, typed)
			for _, p := range problems {
				t.Logf("expected violation: %s", p)
			}
			for _, c := range cases {
				if c.name == "map" {
					// The elements of the result are charged as they are pushed and collected, so the loop is
					// charged even without its own charge. The other cases build no list of that size.
					continue
				}
				found := false
				for _, p := range problems {
					found = found || strings.HasPrefix(p, c.name+":")
				}
				if !found {
					t.Errorf("%s: the runtime without the charge of the loops passes the checks", c.name)
				}
			}
		})
	}
}

// loopCase is a loop of an action: its text, the size of the input, the number of lambda calls for an input
// of n elements, and whether it makes a list (a typed action takes its length).
type loopCase struct {
	name, action string
	n            int
	calls        func(n int) int
	list         bool
}

// loopChargeProblems generates the parsers of cases, builds them against the runtime in rtDir and returns
// the violations of the contract of TestGeneratedLoopCharge.
func loopChargeProblems(t *testing.T, cases []loopCase, rtDir string, typed bool) []string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", testGoModAt("loopcharge", rtDir))
	var imports, table strings.Builder
	gen := func(pkg, action string, list bool) {
		if typed && list {
			action = "len(" + action + ")"
		}
		src := "rule main = xs:[a]* $$ => " + action
		if typed {
			src = "type R = { N int }\nrule main R = xs:[a]* $$ => R{N: " + action + "}"
		}
		g, err := parseTest(src)
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		code, err := generateCode(t, g, gen.GenOptions{Package: pkg, Start: "main", Types: typed})
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		write(pkg+"/parser.go", string(code))
		write(pkg+"/probe.go", "package "+pkg+loopChargeProbe)
		fmt.Fprintf(&imports, "\t%s \"loopcharge/%s\"\n", pkg, pkg)
	}
	gen("base", "len($xs)", false)
	for i, c := range cases {
		pkg := fmt.Sprintf("a%d", i)
		gen(pkg, c.action, c.list)
		fmt.Fprintf(&table, "\t{%q, %d, %d, %s.Need, %s.Limited},\n", c.name, c.n, c.calls(c.n), pkg, pkg)
	}
	write("main.go", `package main

import (
	"fmt"
`+imports.String()+`)

func main() {
	for _, c := range []struct {
		name     string
		n, calls int
		need     func(int) uint64
		limited  func(int, uint64) string
	}{
`+table.String()+`	} {
		n := c.n
		limit := base.Need(n) + uint64(c.calls)/2
		fmt.Println(c.name, base.Need(n), c.need(n), base.Need(2*n), c.need(2*n), c.limited(n, limit), base.Limited(n, limit))
	}
}
`)
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -trimpath", "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("want %d reports, got:\n%s", len(cases), out)
	}
	var problems []string
	for i, c := range cases {
		f := strings.Fields(lines[i])
		if len(f) != 7 || f[0] != c.name {
			t.Fatalf("%s: malformed report %q", c.name, lines[i])
		}
		num := func(s string) int { v, _ := strconv.Atoi(s); return v }
		base1, act1, base2, act2 := num(f[1]), num(f[2]), num(f[3]), num(f[4])
		extra1, extra2 := act1-base1, act2-base2
		if extra1 < c.calls(c.n) {
			problems = append(problems, fmt.Sprintf("%s: %d steps for %d lambda calls, want at least one per call", c.name, extra1, c.calls(c.n)))
		}
		if grow, want := extra2-extra1, c.calls(2*c.n)-c.calls(c.n); grow < want {
			problems = append(problems, fmt.Sprintf("%s: the steps of the loop grow by %d from %d to %d elements, want at least %d", c.name, grow, c.n, 2*c.n, want))
		}
		if f[5] != "limit" {
			problems = append(problems, fmt.Sprintf("%s: with the limit halfway the parser ends with %q, want ErrWorkLimit", c.name, f[5]))
		}
		if f[6] != "ok" {
			problems = append(problems, fmt.Sprintf("%s: the parser without the loop ends with %q under the same limit, want completion", c.name, f[6]))
		}
	}
	return problems
}

// loopChargeProbe is the consumer in each generated package: Need returns the smallest MaxSteps with which
// the parse of n bytes completes, and Limited runs it with the given MaxSteps and reports "limit" for
// ErrWorkLimit, "ok" for completion and the error otherwise.
const loopChargeProbe = `

import (
	"errors"
	"strings"
)

func Need(n int) uint64 {
	in := strings.Repeat("a", n)
	lo, hi := uint64(1), uint64(1)<<40
	for lo < hi {
		mid := (lo + hi) / 2
		if _, err := Parse(in, WithWorkLimits(WorkLimits{MaxSteps: mid})); err == nil {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo
}

func Limited(n int, max uint64) string {
	_, err := Parse(strings.Repeat("a", n), WithWorkLimits(WorkLimits{MaxSteps: max}))
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, ErrWorkLimit):
		return "limit"
	}
	return err.Error()
}
`
