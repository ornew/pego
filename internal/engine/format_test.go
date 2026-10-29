package engine

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/syntax"
)

func TestFormatPreservesMinusSemantics(t *testing.T) {
	for _, src := range []string{
		`rule main = - -"a" $$`,
		`rule main = (- -"a" / "b") $$`,
		`rule main = (- #cut "a" "b" / "a" "c") $$`,
		`rule main = "a" {- -1 == 1} $$`,
		"type P = { N int }\nrule main P = \"a\" $$ => P{N: - -1}",
	} {
		t.Run(src, func(t *testing.T) {
			for _, options := range []compile.Options{{}, {DisableMemo: true}} {
				before := mustCompile(t, src, options)
				after := mustCompile(t, grammar.Format(before.Grammar), options)
				for _, input := range []string{"", "a", "b", "aa", "ab", "ac"} {
					checkBackends(t, before, "main", input)
					checkBackends(t, after, "main", input)
					for _, unit := range []PositionUnit{CodePoints, Bytes} {
						for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
							opts := ParseOptions{Backend: backend, Unit: unit}
							n1, err1 := before.ParseWith("main", input, opts)
							n2, err2 := after.ParseWith("main", input, opts)
							if resultJSON(n1, err1) != resultJSON(n2, err2) {
								t.Errorf("%s/%s on %q: formatting changes %s to %s", backend, unit, input, resultJSON(n1, err1), resultJSON(n2, err2))
							}
						}
					}
				}
			}
		})
	}
}

// TestFormatPreservesCorpus checks that formatting each corpus grammar and
// every grammar file in the repository and parsing the result again gives the
// same grammar, apart from source positions, that formatting it again changes
// nothing, and that the corpus grammars parse their inputs the same way.
func TestFormatPreservesCorpus(t *testing.T) {
	type source struct {
		name, src string
		inputs    []string
	}
	var sources []source
	for _, c := range genCorpus(t) {
		sources = append(sources, source{c.name, c.src, c.inputs})
	}
	out, err := exec.Command("git", "-C", "../..", "ls-files", "*.pego").Output()
	if err != nil {
		t.Skip("git ls-files:", err)
	}
	for _, name := range strings.Fields(string(out)) {
		data, err := os.ReadFile(filepath.Join("../..", name))
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, source{name: name, src: string(data)})
	}
	if len(sources) < 100 {
		t.Fatalf("only %d grammars", len(sources))
	}
	for _, s := range sources {
		t.Run(s.name, func(t *testing.T) {
			before, err := syntax.Parse(s.src)
			if err != nil {
				t.Fatal(err)
			}
			formatted := grammar.Format(before)
			after, err := syntax.Parse(formatted)
			if err != nil {
				t.Fatalf("%v\n%s", err, formatted)
			}
			if want, got := grammarWithoutSources(t, before), grammarWithoutSources(t, after); got != want {
				t.Fatalf("formatting changes the grammar:\n%s\n%s\n%s", formatted, want, got)
			}
			if again := grammar.Format(after); again != formatted {
				t.Errorf("formatting is not stable:\n%s\n---\n%s", formatted, again)
			}
			if len(s.inputs) == 0 {
				return
			}
			p1, p2 := mustCompile(t, s.src, compile.Options{NoProjections: true}), mustCompile(t, formatted, compile.Options{NoProjections: true})
			for _, input := range s.inputs {
				for _, unit := range []PositionUnit{CodePoints, Bytes} {
					opts := ParseOptions{Unit: unit}
					n1, err1 := p1.ParseWith("main", input, opts)
					n2, err2 := p2.ParseWith("main", input, opts)
					if r1, r2 := resultJSON(n1, err1), resultJSON(n2, err2); r1 != r2 {
						t.Errorf("%v on %q: formatting changes %s to %s", unit, input, r1, r2)
					}
				}
			}
		})
	}
}

// grammarWithoutSources returns the JSON form of g without source ranges
// (source and fields such as skipSource).
func grammarWithoutSources(t *testing.T, g *grammar.Grammar) string {
	data, err := grammar.MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	var strip func(any)
	strip = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			for k, x := range v {
				if k == "source" || strings.HasSuffix(k, "Source") {
					delete(v, k)
				} else {
					strip(x)
				}
			}
		case []any:
			for _, x := range v {
				strip(x)
			}
		}
	}
	strip(v)
	data, _ = json.Marshal(v)
	return string(data)
}
