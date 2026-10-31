package gogen_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
	"github.com/ornew/zeroparse/internal/ir/exec/core"
)

// A parser generated without expectations, and one generated with them that a parse does not ask
// to record, return the trees of a parse that records and fail at the same positions, with no
// expectations and no messages. It covers the plain, typed and Document forms, with and without
// budget controls.
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
		for _, mode := range []string{"off", "avail", "on"} {
			pkg := fmt.Sprintf("p%d%s", i, mode)
			opts := f.opts
			opts.Start, opts.Expectations = "main", mode != "off"
			ask := ""
			if mode == "on" {
				ask = pkg + ".WithExpectations()"
			}
			gentest.GeneratePackageRaw(t, dir, pkg, g, opts)
			fmt.Fprintf(&imports, "\t%s \"gentest/%s\"\n", pkg, pkg)
			call := strings.ReplaceAll(f.call, "docParse(", "func(s string) (any, error) { d, err := "+pkg+".NewDocument(s"+opt(ask)+"); if err != nil { return nil, err }; return d.Parse() }(")
			call = strings.ReplaceAll(call, "ParseAST(in)", pkg+".ParseAST(in"+opt(ask)+")")
			if strings.HasPrefix(call, "Parse(in)") {
				call = pkg + ".Parse(in" + opt(ask) + ")"
			}
			fmt.Fprintf(&table, "\t{%q, func(in string) (any, error) { return %s },\n\t\tfunc(err error) (string, int) { var l %s.SyntaxErrors; var e *%[3]s.SyntaxError; switch { case errors.As(err, &l): s, n := \"rec\", 0; for _, x := range l { s += fmt.Sprintf(\" %%d:%%d:%%d\", x.Pos, x.Line, x.Col); n += len(x.Expected) + len(x.Messages) }; return s, n; case errors.As(err, &e): return fmt.Sprintf(\"err %%d:%%d:%%d\", e.Pos, e.Line, e.Col), len(e.Expected) + len(e.Messages); case err != nil: return \"other \" + err.Error(), 0 }; return \"ok\", 0 }},\n", fmt.Sprintf("%s/%s", f.name, pkg), call, pkg)
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
	cmd.Env = append(os.Environ(), "GOFLAGS="+core.GoFlags("-mod=mod -trimpath"), "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	// Each input yields the off, avail and on line of every form, in that order.
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != len(inputs)*len(forms)*3 {
		t.Fatalf("got %d lines\n%s", len(lines), out)
	}
	failed := 0
	for i := 0; i < len(lines); i += 3 {
		off, avail, on := strings.Split(lines[i], "\t"), strings.Split(lines[i+1], "\t"), strings.Split(lines[i+2], "\t")
		if len(off) != 5 || len(avail) != 5 || len(on) != 5 {
			t.Fatalf("malformed lines\n%s\n%s\n%s", lines[i], lines[i+1], lines[i+2])
		}
		if avail[3] != "0" {
			t.Errorf("%s: expectations recorded without asking: %s", avail[0], lines[i+1])
		}
		if avail[1] != on[1] || avail[2] != on[2] || expectedText.ReplaceAllString(avail[4], "syntax error") != expectedText.ReplaceAllString(on[4], "syntax error") || avail[2] != off[2] || avail[4] != off[4] {
			t.Errorf("%s vs %s:\n avail %s\n on    %s", avail[0], on[0], lines[i+1], lines[i+2])
		}
		// Input, result and the kind and positions of the errors agree; only the on parser lists
		// expectations.
		if off[1] != on[1] || off[2] != on[2] || expectedText.ReplaceAllString(off[4], "syntax error") != expectedText.ReplaceAllString(on[4], "syntax error") {
			t.Errorf("%s vs %s:\n off %s\n on  %s", off[0], on[0], lines[i], lines[i+2])
		}
		if off[3] != "0" {
			t.Errorf("%s: expectations recorded: %s", off[0], lines[i])
		}
		if kind := strings.Fields(on[2])[0]; kind == "err" || kind == "rec" {
			failed++
			if on[3] == "0" {
				t.Errorf("%s: no expectations recorded: %s", on[0], lines[i+2])
			}
		}
	}
	if failed == 0 {
		t.Fatal("no input failed")
	}
}

// opt returns the call argument that passes the option ask, if any.
func opt(ask string) string {
	if ask == "" {
		return ""
	}
	return ", " + ask
}

var expectedText = regexp.MustCompile(`syntax error: expected [^"]*`)

func quoteAll(ss []string) string {
	var b strings.Builder
	for _, s := range ss {
		fmt.Fprintf(&b, "%q,", s)
	}
	return b.String()
}

// WithExpectations passed through the runtime to a parser generated without expectations is
// rejected with an error on every entry point, instead of recording into a parser that has no
// descriptions of the expectations.
func TestGeneratedRejectsExpectationsOption(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedRejectsExpectationsOption)
}

func testGeneratedRejectsExpectationsOption(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	const src = `type N = node[string]
rule main N = ("a" "b") #atomic`
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gentest.GoMod("gentest")), 0o644); err != nil {
		t.Fatal(err)
	}
	gentest.GeneratePackageRaw(t, dir, "off", g, gen.GenOptions{Start: "main", Types: true, Document: true})
	gs, err := parseTest("rule main = ^^ items:item* #stream $$\nrule item = \"a\\n\"")
	if err != nil {
		t.Fatal(err)
	}
	gentest.GeneratePackageRaw(t, dir, "offs", gs, gen.GenOptions{Start: "main", Stream: true})
	gentest.GeneratePackageRaw(t, dir, "avail", g, gen.GenOptions{Start: "main", Types: true, Document: true, Expectations: true})
	main := `package main

import (
	"fmt"
	"strings"

	avail "gentest/avail"
	off "gentest/off"
	offs "gentest/offs"
	rt "github.com/ornew/zeroparse/runtime"
)

func try(name string, f func() error) {
	defer func() {
		if x := recover(); x != nil {
			fmt.Printf("%s\tPANIC %v\n", name, x)
		}
	}()
	err := f()
	if err == nil {
		fmt.Printf("%s\tnil\n", name)
		return
	}
	fmt.Printf("%s\t%v\n", name, err)
}

func main() {
	ask := rt.WithExpectations()
	try("parse", func() error { _, err := off.Parse("a", ask); return err })
	try("ast", func() error { _, err := off.ParseAST("a", ask); return err })
	try("stream", func() error { return offs.ParseStream(strings.NewReader("a\n"), func(*offs.Node) error { return nil }, ask) })
	try("document", func() error { _, err := off.NewDocument("a", ask); return err })
	d, err := off.NewDocument("a")
	if err != nil {
		panic(err)
	}
	try("document-parse", func() error { _, err := d.Parse(ask); return err })
	try("document-ast", func() error { _, err := d.ParseAST(ask); return err })
	try("avail-parse", func() error { _, err := avail.Parse("a", avail.WithExpectations()); return err })
	try("avail-ast", func() error { _, err := avail.ParseAST("a", avail.WithExpectations()); return err })
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS="+core.GoFlags("-mod=mod -trimpath"), "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	seen := 0
	for _, line := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		name, res, _ := strings.Cut(line, "\t")
		seen++
		if strings.HasPrefix(name, "avail-") {
			if strings.Contains(res, "PANIC") || strings.Contains(res, "without expectations") {
				t.Errorf("%s: %s", name, res)
			}
			continue
		}
		// A Document parse reports the mode mismatch, the Document having been built without them.
		if !strings.Contains(res, "generated without expectations") && !(strings.HasPrefix(name, "document-") && strings.Contains(res, "cannot change expectations")) {
			t.Errorf("%s: %s", name, res)
		}
	}
	if seen != 8 {
		t.Errorf("got %d results\n%s", seen, out)
	}
}
