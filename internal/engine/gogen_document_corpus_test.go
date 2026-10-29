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
)

// TestGeneratedDocumentsMatchBatchOnCorpus checks generated Documents against
// batch parses of the same generated package, which other tests compare with
// the engine: Node Parse and typed ParseAST of each corpus input in a new
// Document, and along a chain of edits through all inputs of the grammar,
// including insertions at the start and end and their removal, in both
// position units.
func TestGeneratedDocumentsMatchBatchOnCorpus(t *testing.T) {
	t.Parallel()
	eachMode(t, testGeneratedDocumentsMatchBatchOnCorpus)
}

func testGeneratedDocumentsMatchBatchOnCorpus(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	dir := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", testGoMod("doctest"))
	var imports, checks strings.Builder
	cases := genCorpus(t)
	for i, c := range cases {
		g, err := parseTest(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		generatePackageIn(t, dir, "doctest", fmt.Sprintf("g%d", i), g, gen.GenOptions{Start: "main", Types: true, Document: true})
		fmt.Fprintf(&imports, "\tg%d \"doctest/g%d\"\n", i, i)
		fmt.Fprintf(&checks, `	func(texts []string, b bool) (out []string) {
		u := g%[1]d.CodePoints
		if b {
			u = g%[1]d.Bytes
		}
		compare := func(d *g%[1]d.Document) {
			n, err := d.Parse()
			bn, berr := g%[1]d.Parse(d.Text(), g%[1]d.WithUnit(u))
			out = append(out, differ("Parse", d.Text(), n, err, bn, berr)...)
			v, err := d.ParseAST()
			bv, berr := g%[1]d.ParseAST(d.Text(), g%[1]d.WithUnit(u))
			out = append(out, differ("ParseAST", d.Text(), v, err, bv, berr)...)
		}
		for _, s := range texts {
			d, err := g%[1]d.NewDocument(s, g%[1]d.WithUnit(u))
			if err != nil {
				return append(out, err.Error())
			}
			compare(d)
			compare(d)
		}
		d, err := g%[1]d.NewDocument(texts[0], g%[1]d.WithUnit(u))
		if err != nil {
			return append(out, err.Error())
		}
		compare(d)
		for _, s := range append(texts[1:], texts[0]) {
			for _, e := range []struct{ start, end int; text string }{{0, 0, "x"}, {0, 1, ""}, {length(d.Text(), b), length(d.Text(), b), "x"}, {length(d.Text(), b), length(d.Text(), b) + 1, ""}, {0, length(d.Text(), b), s}} {
				if err := d.Edit(e.start, e.end, e.text); err != nil {
					return append(out, err.Error())
				}
				compare(d)
			}
		}
		return out
	},
`, i)
	}
	var inputs strings.Builder
	for _, c := range cases {
		fmt.Fprintf(&inputs, "\t%#v,\n", append([]string{""}, c.inputs...))
	}
	var names strings.Builder
	for _, c := range cases {
		fmt.Fprintf(&names, "\t%q,\n", c.name)
	}
	write("main.go", `package main

import (
	"encoding/json"
	"fmt"
	"os"
	"unicode/utf8"

`+imports.String()+`)

func differ(what, text string, got any, gotErr error, want any, wantErr error) []string {
	g, err1 := json.Marshal(got)
	w, err2 := json.Marshal(want)
	if err1 != nil || err2 != nil || string(g) != string(w) || fmt.Sprint(gotErr) != fmt.Sprint(wantErr) {
		return []string{fmt.Sprintf("%s %q: Document %s / %v; batch %s / %v", what, text, g, gotErr, w, wantErr)}
	}
	return nil
}

func length(s string, bytes bool) int {
	if bytes {
		return len(s)
	}
	return utf8.RuneCountInString(s)
}

var names = []string{
`+names.String()+`}

var inputs = [][]string{
`+inputs.String()+`}

var checks = []func([]string, bool) []string{
`+checks.String()+`}

func main() {
	failed := 0
	for i, check := range checks {
		for _, b := range []bool{false, true} {
			for _, d := range check(inputs[i], b) {
				failed++
				fmt.Printf("%s (bytes %v): %s\n", names[i], b, d)
			}
		}
	}
	if failed != 0 {
		os.Exit(1)
	}
}
`)
	cmd := exec.Command(goBin, "run", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
