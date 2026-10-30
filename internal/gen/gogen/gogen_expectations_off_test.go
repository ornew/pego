package gogen_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

// A parser generated without expectations returns the trees of one generated with them and fails
// at the same positions, with no expectations and no messages. It covers the plain, typed and
// Document forms, with and without budget controls.
func TestGeneratedWithoutExpectations(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedWithoutExpectations)
}

func testGeneratedWithoutExpectations(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	type form struct {
		name, src string
		opts      gen.GenOptions
		call      string // the expression that parses the string in
	}
	const plain = `rule main = item+ $$
rule item = "x" @error(message="expected an x") / "y" / "(" ^ [a-z]+ ")"`
	const recover = `rule main = stmt* $$
rule stmt = ([a-z]+ #atomic ";") #recover(skip=[^;]+ ";")`
	const typed = `type N = node[string]
rule main N = ("a" "b" "c") #atomic`
	forms := []form{
		{"plain", plain, gen.GenOptions{}, "Parse(in)"},
		{"recover", recover, gen.GenOptions{}, "Parse(in)"},
		{"typed", typed, gen.GenOptions{Types: true}, "ParseAST(in)"},
		{"document", plain, gen.GenOptions{Document: true}, "docParse(in)"},
	}
	inputs := []string{"xy", "xz", "z", "(ab)", "(a", "ab;1x;cd;", "abc", "ab", "abd", "ab;cd;", ""}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gentest.GoMod("gentest")), 0o644); err != nil {
		t.Fatal(err)
	}
	var imports, table strings.Builder
	for i, f := range forms {
		g, err := parseTest(f.src)
		if err != nil {
			t.Fatal(err)
		}
		for _, on := range []bool{false, true} {
			pkg := fmt.Sprintf("p%d%s", i, map[bool]string{false: "off", true: "on"}[on])
			opts := f.opts
			opts.Start, opts.Expectations = "main", on
			gentest.GeneratePackageRaw(t, dir, pkg, g, opts)
			fmt.Fprintf(&imports, "\t%s \"gentest/%s\"\n", pkg, pkg)
			call := strings.ReplaceAll(f.call, "docParse(", "func(s string) (any, error) { d, err := "+pkg+".NewDocument(s); if err != nil { return nil, err }; return d.Parse() }(")
			call = strings.ReplaceAll(call, "ParseAST(", pkg+".ParseAST(")
			if strings.HasPrefix(call, "Parse(") {
				call = pkg + "." + call
			}
			fmt.Fprintf(&table, "\t{%q, func(in string) (any, error) { return %s },\n\t\tfunc(err error) (string, int) { var l %s.SyntaxErrors; var e *%[3]s.SyntaxError; switch { case errors.As(err, &l): s, n := \"rec\", 0; for _, x := range l { s += fmt.Sprintf(\" %%d:%%d:%%d\", x.Pos, x.Line, x.Col); n += len(x.Expected) + len(x.Messages) }; return s, n; case errors.As(err, &e): return fmt.Sprintf(\"err %%d:%%d:%%d\", e.Pos, e.Line, e.Col), len(e.Expected) + len(e.Messages); case err != nil: return \"other \" + err.Error(), 0 }; return \"ok\", 0 }},\n", fmt.Sprintf("%s/%s/%v", f.name, pkg, on), call, pkg)
		}
	}
	main := `package main

import (
	"errors"
	"fmt"
` + imports.String() + `)

var table = []struct {
	name string
	parse func(string) (any, error)
	kind func(error) (string, int)
}{
` + table.String() + `}

func main() {
	for _, in := range []string{` + quoteAll(inputs) + `} {
		for _, c := range table {
			v, err := c.parse(in)
			pos, n := c.kind(err)
			fmt.Printf("%s\t%q\t%s\t%d\t%v\n", c.name, in, pos, n, v)
		}
	}
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -trimpath", "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	// Each input yields the off and the on line of every form, in that order.
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != len(inputs)*len(forms)*2 {
		t.Fatalf("got %d lines\n%s", len(lines), out)
	}
	failed := 0
	for i := 0; i < len(lines); i += 2 {
		off, on := strings.Split(lines[i], "\t"), strings.Split(lines[i+1], "\t")
		if len(off) != 5 || len(on) != 5 {
			t.Fatalf("malformed lines\n%s\n%s", lines[i], lines[i+1])
		}
		// Input, result and the kind and positions of the errors agree; only the on parser lists
		// expectations.
		if off[1] != on[1] || off[2] != on[2] || expectedText.ReplaceAllString(off[4], "syntax error") != expectedText.ReplaceAllString(on[4], "syntax error") {
			t.Errorf("%s vs %s:\n off %s\n on  %s", off[0], on[0], lines[i], lines[i+1])
		}
		if off[3] != "0" {
			t.Errorf("%s: expectations recorded: %s", off[0], lines[i])
		}
		if kind := strings.Fields(on[2])[0]; kind == "err" || kind == "rec" {
			failed++
			if on[3] == "0" {
				t.Errorf("%s: no expectations recorded: %s", on[0], lines[i+1])
			}
		}
	}
	if failed == 0 {
		t.Fatal("no input failed")
	}
}

var expectedText = regexp.MustCompile(`syntax error: expected [^"]*`)

func quoteAll(ss []string) string {
	var b strings.Builder
	for _, s := range ss {
		fmt.Fprintf(&b, "%q,", s)
	}
	return b.String()
}
