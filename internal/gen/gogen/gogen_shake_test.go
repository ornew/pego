package gogen_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gen/gogen"
	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/syntax"
)

func TestKeptUnits(t *testing.T) {
	t.Parallel()
	units := []gogen.ShakeUnit{
		{Name: "a", Text: "func a() { b() }"},
		{Name: "b", Text: "func b() {}"},
		{Name: "c", Text: "func c() { d() }"},
		{Name: "d", Text: "func d() {}"},
		{Name: "e", Text: "func e() {}"},
	}
	// a is named by the text and b by a; c and d name each other only through c, which nothing names;
	// e is named only inside a comment, which counts as a mention.
	kept := gogen.KeptUnits(units, "func main() { a() }\n// e is not called\n")
	for i, want := range []bool{true, true, false, false, true} {
		if kept[i] != want {
			t.Errorf("unit %s kept = %v, want %v", units[i].Name, kept[i], want)
		}
	}
	// A name matches whole identifiers only.
	if kept := gogen.KeptUnits(units[:1], "func main() { aa(); a1(); xa() }"); kept[0] {
		t.Errorf("a was kept by a longer identifier")
	}
}

func TestShakeVars(t *testing.T) {
	t.Parallel()
	src := `package p

var scope1 = []string{"a"}
var lit2 = []rune("x")
var cls3 = Class{}
var steps4 = []ClassStep{{C: &cls3}}
var scope5 = []string{"b"}
var other = 1

var rows = [...]int{
	1,
}

func f() { _ = lit2; _ = steps4 }
`
	got := gogen.ShakeVars(src)
	for _, want := range []string{"var lit2 ", "var cls3 ", "var steps4 ", "var other ", "var rows ", "func f()"} {
		if !strings.Contains(got, want) {
			t.Errorf("shakeVars lost %q:\n%s", want, got)
		}
	}
	for _, gone := range []string{"scope1", "scope5"} {
		if strings.Contains(got, gone) {
			t.Errorf("shakeVars kept %q:\n%s", gone, got)
		}
	}
	if again := gogen.ShakeVars(got); again != got {
		t.Errorf("shakeVars is not idempotent")
	}
}

// reachTypedGrammar has a rule that no start rule reaches and a rule that only the start rule calls.
const reachTypedGrammar = `package g

type Word = node[string]

@start
rule main Word = ws w:word ws $$ => $w
rule ws = " "*
rule word Word = ([a-z]+) #atomic
rule unused Word = ("u" digits [a-z]+) #atomic
rule digits = [0-9]*`

// TestTypedUnreachedRulesAreLeftOut checks that the typed code of a rule that no typed entry point
// reaches is not printed, that an Entry is set only for the start rule, and that the output calls
// nothing it does not define.
func TestTypedUnreachedRulesAreLeftOut(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testTypedUnreachedRulesAreLeftOut)
}

func testTypedUnreachedRulesAreLeftOut(t *testing.T) {
	g, err := syntax.Parse(reachTypedGrammar)
	if err != nil {
		t.Fatal(err)
	}
	funcs := regexp.MustCompile(`(?m)^func [su][0-9]+\(`)
	for _, typedOnly := range []bool{false, true} {
		opts := gen.GenOptions{Start: "main", Package: "p", Types: true, TypedOnly: typedOnly}
		pruned, err := gentest.GenerateCode(t, g, opts)
		if err != nil {
			t.Fatal(err)
		}
		opts.KeepUnreachable = true
		full, err := gentest.GenerateCode(t, g, opts)
		if err != nil {
			t.Fatal(err)
		}
		if got := undefinedGenerated(t, pruned); len(got) > 0 {
			t.Errorf("typedOnly=%v: the output calls undefined functions %v", typedOnly, got)
		}
		if n, m := len(funcs.FindAll(pruned, -1)), len(funcs.FindAll(full, -1)); n >= m {
			t.Errorf("typedOnly=%v: %d typed rule functions with pruning, %d without", typedOnly, n, m)
		}
		for name, src := range map[string][]byte{"pruned": pruned, "full": full} {
			if n := strings.Count(string(src), ".Entry = "); n != 1 {
				t.Errorf("typedOnly=%v: %s output sets %d Entry functions, want 1 (the start rule)", typedOnly, name, n)
			}
		}
		if len(pruned) >= len(full) {
			t.Errorf("typedOnly=%v: the pruned output is %d bytes, the full output %d", typedOnly, len(pruned), len(full))
		}
	}
}

// TestTypedUnreachedCalleeFailsGeneration shows that a typed rule that the first pass wrongly left
// out is noticed by the second: the call of it does not compile away silently.
func TestTypedUnreachedCalleeFailsGeneration(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, func(t *testing.T) {
		g, err := syntax.Parse(reachTypedGrammar)
		if err != nil {
			t.Fatal(err)
		}
		gh := &gogen.Hooks{}
		gentest.SetGenHooks(t, gh)
		gh.Reach = func(l *gogen.LiveRules) {
			n := 0
			for k := range l.Reach {
				if k.Table == "trules" && !isRoot(l, k) {
					delete(l.Reach, k)
					n++
				}
			}
			if n == 0 {
				t.Error("the first pass reached no typed rule besides the start rule")
			}
		}
		_, err = gentest.GenerateCode(t, g, gen.GenOptions{Start: "main", Package: "p", Types: true})
		if err == nil || !strings.Contains(err.Error(), "left out") {
			t.Errorf("generation with a typed callee left out: err = %v, want an internal error", err)
		}
	})
}

// isRoot reports whether k is one of the roots of the first pass of l.
func isRoot(l *gogen.LiveRules, k gogen.RuleKey) bool {
	for _, r := range l.Roots {
		if r == k {
			return true
		}
	}
	return false
}

// TestTypedUnreachedRulesRunLikeTheFullOutput builds the typed parser with the unreached rules left
// out (p) and the one that keeps them (k) and compares what ParseAST returns for the same inputs.
func TestTypedUnreachedRulesRunLikeTheFullOutput(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testTypedUnreachedRulesRunLikeTheFullOutput)
}

func testTypedUnreachedRulesRunLikeTheFullOutput(t *testing.T) {
	jsonSrc, err := os.ReadFile("../../../parsers/json/json.pego")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, src string
		inputs    []string
	}{
		{"words", reachTypedGrammar, []string{"abc", " ab ", "", "u12x", "A"}},
		{"json", string(jsonSrc), []string{`{"a": [1, 2.5e3, true, null], "b": {"c": "d\n"}}`, `[]`, `{`, `[1,]`, ``, `"x"`, ` [ ] `}},
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	for _, tc := range cases {
		for _, typedOnly := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s typedOnly=%v", tc.name, typedOnly), func(t *testing.T) {
				g, err := syntax.Parse(tc.src)
				if err != nil {
					t.Fatal(err)
				}
				dir := t.TempDir()
				write := func(name, content string) {
					path := filepath.Join(dir, name)
					os.MkdirAll(filepath.Dir(path), 0o755)
					if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				write("go.mod", gentest.GoMod("typedreach"))
				opts := gen.GenOptions{Start: "main", Package: "p", Types: true, TypedOnly: typedOnly}
				for _, pkg := range []string{"p", "k"} {
					opts.Package, opts.KeepUnreachable = pkg, pkg == "k"
					code, err := gentest.GenerateCode(t, g, opts)
					if err != nil {
						t.Fatal(err)
					}
					write(pkg+"/parser.go", string(code))
				}
				var inputs []string
				for _, in := range tc.inputs {
					inputs = append(inputs, fmt.Sprintf("%q", in))
				}
				write("main.go", strings.NewReplacer("INPUTS", strings.Join(inputs, ", ")).Replace(`package main

import (
	"encoding/json"
	"fmt"

	k "typedreach/k"
	p "typedreach/p"
)

func main() {
	for _, in := range []string{INPUTS} {
		v, err := p.ParseAST(in)
		pb, _ := json.Marshal(v)
		fmt.Println("p", string(pb), err)
		w, err := k.ParseAST(in)
		kb, _ := json.Marshal(w)
		fmt.Println("k", string(kb), err)
		u, err := p.ParseRuleAST("", in)
		ub, _ := json.Marshal(u)
		fmt.Println("p rule", string(ub), err)
		x, err := k.ParseRuleAST("", in)
		xb, _ := json.Marshal(x)
		fmt.Println("k rule", string(xb), err)
	}
}
`))
				cmd := exec.Command(goBin, "run", "-trimpath", ".")
				cmd.Dir = dir
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("go run: %v\n%s", err, out)
				}
				var pruned, full []string
				for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
					switch {
					case strings.HasPrefix(line, "p "):
						pruned = append(pruned, strings.TrimPrefix(line, "p "))
					case strings.HasPrefix(line, "k "):
						full = append(full, strings.TrimPrefix(line, "k "))
					}
				}
				if len(pruned) != 2*len(tc.inputs) || strings.Join(pruned, "\n") != strings.Join(full, "\n") {
					t.Errorf("the output without the unreached rules differs:\n%s", out)
				}
			})
		}
	}
}
