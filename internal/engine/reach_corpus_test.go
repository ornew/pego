package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/syntax"
)

// parseNaturalStart parses src without starttest.MarkAll: only the rule main (or, without one, the
// first rule that is not private) is a start rule, as in a grammar written for use. A grammar that
// has attributes marks its own start rules.
func parseNaturalStart(src string) (*grammar.Grammar, error) {
	g, err := syntax.Parse(src)
	if err != nil {
		return nil, err
	}
	var first, main *grammar.RuleDef
	for _, rd := range g.Rules() {
		if len(rd.Attrs) > 0 {
			return g, nil
		}
		if first == nil && !strings.HasPrefix(rd.Name, "_") {
			first = rd
		}
		if rd.Name == "main" {
			main = rd
		}
	}
	if main == nil {
		main = first
	}
	if main != nil {
		main.Attrs = []*grammar.Attribute{{Name: "start"}}
	}
	return g, nil
}

// TestUnreachableCorpusGeneration generates every corpus grammar, with its natural start rule, in
// the option sets of the generated parsers, and requires that whatever generates without the
// pruning generates with it: the second pass can choose a different route (Flow or direct) than the
// first, because sharing depends on what was printed, and must still call only rules it prints.
func TestUnreachableCorpusGeneration(t *testing.T) { eachMode(t, testUnreachableCorpusGeneration) }

func testUnreachableCorpusGeneration(t *testing.T) {
	optionSets := []struct {
		name string
		opts gen.GenOptions
	}{
		{"plain", gen.GenOptions{}},
		{"recognize", gen.GenOptions{Recognize: true}},
		{"document", gen.GenOptions{Document: true}},
		{"work controls", gen.GenOptions{WorkControls: true}},
		{"observability", gen.GenOptions{Observability: true}},
		{"observability document", gen.GenOptions{Observability: true, Document: true}},
		{"types", gen.GenOptions{Types: true}},
		{"types recognize", gen.GenOptions{Types: true, Recognize: true}},
	}
	checked, pruned := 0, 0
	for _, c := range genCorpus(t) {
		for _, set := range optionSets {
			g, err := parseNaturalStart(c.src)
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			opts := set.opts
			opts.Package, opts.Start = "p", "main"
			kept := opts
			kept.KeepUnreachable = true
			full, keptErr := generateCode(g, kept)
			if keptErr != nil {
				continue // the grammar does not generate in this option set
			}
			code, err := generateCode(g, opts)
			if err != nil {
				t.Errorf("%s with %s: generation fails with the pruning (%v), not without it", c.name, set.name, err)
				continue
			}
			checked++
			if len(code) < len(full) {
				pruned++
			}
		}
	}
	t.Logf("%d generations compared, %d of them smaller with the pruning", checked, pruned)
	if checked == 0 || pruned == 0 {
		t.Errorf("the sweep shows nothing: %d generations, %d pruned", checked, pruned)
	}
}

// TestUnreachableCorpusRuns builds the pruned and the unpruned parser of every corpus grammar with
// its natural start rule and requires the same Parse and Recognize result on every corpus input.
func TestUnreachableCorpusRuns(t *testing.T) { eachMode(t, testUnreachableCorpusRuns) }

func testUnreachableCorpusRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	dir := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", testGoMod("reachcorpus"))
	var imports, calls strings.Builder
	n := 0
	for _, c := range genCorpus(t) {
		g, err := parseNaturalStart(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		opts := gen.GenOptions{Recognize: true, Start: "main"}
		kept := opts
		kept.KeepUnreachable = true
		kept.Package = fmt.Sprintf("k%d", n)
		keptCode, err := generateCode(g, kept)
		if err != nil {
			continue
		}
		opts.Package = fmt.Sprintf("p%d", n)
		code, err := generateCode(g, opts)
		if err != nil {
			t.Fatalf("%s: generation fails with the pruning: %v", c.name, err)
		}
		write(fmt.Sprintf("p%d/parser.go", n), string(code))
		write(fmt.Sprintf("k%d/parser.go", n), string(keptCode))
		fmt.Fprintf(&imports, "\tp%[1]d \"reachcorpus/p%[1]d\"\n\tk%[1]d \"reachcorpus/k%[1]d\"\n", n)
		var in []string
		for _, s := range c.inputs {
			in = append(in, fmt.Sprintf("%q", s))
		}
		fmt.Fprintf(&calls, `	for _, in := range []string{%s} {
		line("%d parse", call(func() string { n, err := p%[2]d.Parse(in); return show(n, err) }), call(func() string { n, err := k%[2]d.Parse(in); return show(n, err) }))
		line("%[2]d recognize", call(func() string { return fmt.Sprint(p%[2]d.Recognize(in)) }), call(func() string { return fmt.Sprint(k%[2]d.Recognize(in)) }))
	}
`, strings.Join(in, ", "), n)
		n++
	}
	write("main.go", `package main

import (
	"fmt"
`+imports.String()+`)

func call(f func() string) (out string) {
	defer func() {
		if r := recover(); r != nil {
			out = fmt.Sprint("PANIC ", r)
		}
	}()
	return f()
}

func show(n interface{ String() string }, err error) string {
	if n == nil {
		return fmt.Sprint("nil ", err)
	}
	return n.String() + " " + fmt.Sprint(err)
}

func line(what, p, k string) {
	if p == k {
		fmt.Println("same", what)
	} else {
		fmt.Printf("DIFF %s\n  pruned %q\n  full   %q\n", what, p, k)
	}
}

func main() {
`+calls.String()+`}
`)
	cmd := exec.Command(goBin, "run", "-trimpath", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%.4000s", err, out)
	}
	same := strings.Count(string(out), "same ")
	if strings.Contains(string(out), "DIFF") || same == 0 {
		t.Fatalf("pruned and full parsers disagree (%d agree):\n%.6000s", same, out)
	}
	t.Logf("%d grammars, %d results agree", n, same)
}
