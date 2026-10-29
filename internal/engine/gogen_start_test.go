package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/syntax"
)

// startEntryNames are the names the start-rule entry tests try: two start rules, a rule that is
// not one, a hidden rule, an undefined rule and no name.
var startEntryNames = []string{"main", "item", "other", "_h", "nope", ""}

// startEntryWant is what the interpreter reports for each of startEntryNames, one line each.
func startEntryWant(t *testing.T) string {
	t.Helper()
	prog, err := compileStarts(t, startRestrictionSource)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, name := range startEntryNames {
		if err := prog.CheckStart(name); err != nil {
			fmt.Fprintf(&b, "%s: %v\n", name, err)
		} else {
			fmt.Fprintf(&b, "%s: ok\n", name)
		}
	}
	return b.String()
}

// Generated Go parsers enter only start rules, by name or by default, with the interpreter's errors.
func TestGeneratedGoStartRules(t *testing.T) { eachMode(t, testGeneratedGoStartRules) }

func testGeneratedGoStartRules(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	want := startEntryWant(t)
	if !strings.Contains(want, "other: rule other is not a start rule") || !strings.Contains(want, "_h: rule _h is hidden") || !strings.Contains(want, "item: ok") {
		t.Fatalf("unexpected interpreter results:\n%s", want)
	}
	g, err := syntax.Parse(startRestrictionSource)
	if err != nil {
		t.Fatal(err)
	}
	report := `func report(name string, err error) {
	if err != nil {
		fmt.Printf("%s: %v\n", name, err)
	} else {
		fmt.Printf("%s: ok\n", name)
	}
}
`
	names := fmt.Sprintf("%#v", startEntryNames)
	for _, tc := range []struct {
		name string
		opts gen.GenOptions
		body string
	}{
		{"generic", gen.GenOptions{Package: "p"}, `func main() {
	for _, name := range ` + names + ` {
		_, err := p.ParseRule(name, "a")
		report(name, err)
	}
	_, one := p.Parse("a")
	_, two := p.Parse("aa")
	fmt.Println("default:", one == nil, two == nil)
	_, empty := p.ParseRule("", "aa")
	fmt.Println("empty:", empty == nil)
}
`},
		{"typed", gen.GenOptions{Package: "p", Types: true}, `func main() {
	for _, name := range ` + names + ` {
		_, err := p.ParseRuleAST(name, "a")
		report(name, err)
	}
	for _, name := range ` + names + ` {
		_, err := p.ParseRule(name, "a")
		report(name, err)
	}
	_, one := p.Parse("a")
	_, two := p.Parse("aa")
	fmt.Println("default:", one == nil, two == nil)
	_, empty := p.ParseRule("", "aa")
	_, emptyAST := p.ParseRuleAST("", "aa")
	fmt.Println("empty:", empty == nil, emptyAST == nil)
}
`},
		{"typed only", gen.GenOptions{Package: "p", Types: true, TypedOnly: true}, `func main() {
	for _, name := range ` + names + ` {
		_, err := p.ParseRuleAST(name, "a")
		report(name, err)
	}
	_, one := p.ParseAST("a")
	_, two := p.ParseAST("aa")
	fmt.Println("default:", one == nil, two == nil)
	_, emptyAST := p.ParseRuleAST("", "aa")
	fmt.Println("empty:", emptyAST == nil)
}
`},
		{"named start", gen.GenOptions{Package: "p", Start: "item"}, `func main() {
	for _, name := range ` + names + ` {
		_, err := p.ParseRule(name, "a")
		report(name, err)
	}
	_, one := p.Parse("a")
	_, two := p.Parse("aa")
	fmt.Println("default:", one == nil, two == nil)
	_, empty := p.ParseRule("", "aa")
	fmt.Println("empty:", empty == nil)
}
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, err := generateCode(t, g, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			// Only start rules have typed entries.
			if tc.opts.Types && (!strings.Contains(string(code), "\tcase \"item\":") || strings.Contains(string(code), "\tcase \"other\":")) {
				t.Error("ParseRuleAST does not have cases for exactly the start rules")
			}
			dir := t.TempDir()
			for name, content := range map[string]string{
				"go.mod":      testGoMod("gentest"),
				"p/parser.go": string(code),
				"main.go":     "package main\n\nimport (\n\t\"fmt\"\n\n\t\"gentest/p\"\n)\n\n" + report + tc.body,
			} {
				path := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command(goBin, "run", ".")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -trimpath", "GOTOOLCHAIN=local")
			var stderr strings.Builder
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("go run: %v\n%s", err, stderr.String())
			}
			expected := want
			if tc.name == "typed" {
				expected += want
			}
			// Parse uses the default start rule main, or the start rule item it was generated for.
			if tc.name == "named start" {
				expected += "default: true false\n"
			} else {
				expected += "default: true true\n"
			}
			// An empty name selects the declared default start rule main, which matches "aa",
			// even in a parser generated for item.
			if tc.name == "typed" {
				expected += "empty: true true\n"
			} else {
				expected += "empty: true\n"
			}
			if string(out) != expected {
				t.Errorf("got\n%s\nwant\n%s", out, expected)
			}
		})
	}
}

// Generated TypeScript parsers enter only start rules, with the interpreter's errors.
func TestGeneratedTSStartRules(t *testing.T) {
	want := startEntryWant(t)
	g, err := syntax.Parse(startRestrictionSource)
	if err != nil {
		t.Fatal(err)
	}
	script := `import { parse, parseRule, parseRuleWithOptions } from "./parser.ts";
const names = ["main", "item", "other", "_h", "nope", ""];
for (const f of [(n: string) => parseRule(n, "a"), (n: string) => parseRuleWithOptions(n, "a", {})]) {
  for (const n of names) {
    const r = f(n);
    console.log(n + ": " + (r.error === null ? "ok" : r.error.message));
  }
}
console.log("default: " + (parse("a").error === null) + " " + (parse("aa").error === null));
console.log("empty: " + (parseRule("", "aa").error === null) + " " + (parseRuleWithOptions("", "aa", {}).error === null));
`
	for _, start := range []string{"", "item"} {
		// An empty name selects the declared default start rule main, which matches "aa",
		// even in a parser generated for item.
		out := runTSGrammarScriptWith(t, g, script, gen.GenOptions{Start: start})
		parse := "default: true true\n"
		if start == "item" {
			parse = "default: true false\n"
		}
		if expected := want + want + parse + "empty: true true\n"; out != expected {
			t.Errorf("start %q: got\n%s\nwant\n%s", start, out, expected)
		}
	}
}
