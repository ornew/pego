package engine

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

func TestGeneratedSharingRollback(t *testing.T) {
	g := &generator{table: "rules"}
	lit := g.literal("kept")
	m := g.method("", "return nil, true\n")
	snap := g.snapshot()
	g.literal("discarded")
	g.method("", "return nil, false\n")
	g.rollback(snap)
	if g.literal("kept") != lit || g.method("", "return nil, true\n") != m {
		t.Fatal("rollback must preserve previously emitted helpers")
	}
	before := g.vars.Len()
	g.literal("discarded")
	if g.vars.Len() == before {
		t.Fatal("discarded literal must be emitted again after rollback")
	}
	before = g.methods.Len()
	g.method("", "return nil, false\n")
	if g.methods.Len() == before {
		t.Fatal("discarded method must be emitted again after rollback")
	}
}

func TestGeneratedSharingIdentity(t *testing.T) {
	ts := &tsGen{generator: &generator{table: "rules"}}
	name := ts.method("first", "return [null, true];\n")
	ts.table = "recRules"
	if ts.method("recognition", "return [null, true];\n") != name {
		t.Fatal("identical TypeScript bodies should share across rule tables")
	}
	g := &generator{table: "rules"}
	a := g.method("first", "return nil, true\n")
	g.table = "recRules"
	if g.method("recognition", "return nil, true\n") != a {
		t.Fatal("identical Node bodies should share across rule tables")
	}
	g.table = "trules"
	if g.method("typed", "return nil, true\n") == a {
		t.Fatal("different receivers and value types must not share")
	}
	g.table = "rules"
	for _, body := range []string{
		"return p.matchLiteral(lit1, \"x\", 4, false)\n",
		"return p.matchLiteral(lit1, \"x\", 5, false)\n",
		"return p.matchLiteral(lit1, \"x\", 4, true)\n",
		"return p.call(rules[0], 0)\n",
		"return p.call(recRules[0], 0)\n",
		"return p.frame.vals[0], true\n",
		"return p.frame.vals[1], true\n",
	} {
		before := g.methods.Len()
		g.method("", body)
		if g.methods.Len() == before {
			t.Fatal("semantically distinct bodies must not share")
		}
	}
}

func TestGeneratedSharingDeterministic(t *testing.T) {
	g, err := syntax.Parse(`def main = a "!" / a "?"
def a = @("é" / "ab")+`)
	if err != nil {
		t.Fatal(err)
	}
	for _, generate := range []struct {
		name string
		fn   func(GenOptions) ([]byte, error)
	}{
		{"Go", func(opts GenOptions) ([]byte, error) { return Generate(g, opts) }},
		{"TypeScript", func(opts GenOptions) ([]byte, error) { return GenerateTS(g, opts) }},
	} {
		t.Run(generate.name, func(t *testing.T) {
			opts := GenOptions{Package: "shared", Start: "main", Recognize: true}
			first, err := generate.fn(opts)
			if err != nil {
				t.Fatal(err)
			}
			second, err := generate.fn(opts)
			if err != nil || !bytes.Equal(first, second) {
				t.Fatal("generation must be deterministic")
			}
			if generate.name == "TypeScript" {
				opts.disableLiteralSharing = true
				ref, err := generate.fn(opts)
				if err != nil || !bytes.Equal(first, ref) {
					t.Fatal("production TypeScript must leave literal tables unshared")
				}
				opts.enableTSLiteralSharing = true
				ref, err = generate.fn(opts)
				if err != nil || !bytes.Equal(first, ref) {
					t.Fatal("explicit literal-sharing disable must override the experiment")
				}
				opts.disableLiteralSharing = false
				experiment, err := generate.fn(opts)
				if err != nil || len(experiment) >= len(first) {
					t.Fatal("fixture must exercise experimental literal sharing")
				}
			}
			opts.disableMethodSharing, opts.disableLiteralSharing = true, true
			ref, err := generate.fn(opts)
			if err != nil {
				t.Fatal(err)
			}
			if len(first) > len(ref) || generate.name == "Go" && len(first) == len(ref) {
				t.Fatal("recognition fixture must exercise sharing")
			}
		})
	}
}

func TestGeneratedUnsharedParsersMatchEngine(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	opts := GenOptions{disableMethodSharing: true, disableLiteralSharing: true}
	testGeneratedParsersCorpus(t, filepath.Join(runtime.GOROOT(), "bin", "go"), genCorpus(t), opts)
	testGeneratedTypesCorpus(t, filepath.Join(runtime.GOROOT(), "bin", "go"), genCorpus(t), opts)
}

func TestGeneratedUnsharedTSParsersMatchEngine(t *testing.T) {
	if testing.Short() {
		t.Skip("runs generated TypeScript")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	testGeneratedTSParsersCorpus(t, node, genCorpus(t), GenOptions{disableMethodSharing: true, disableLiteralSharing: true})
}

// PEGO_GENERATED_SHARING selects generation-only controls: combined also
// enables experimental TypeScript literal sharing; methods, literals, or
// both disables the corresponding mechanism in that experiment. Empty uses
// production defaults. Standalone parsers have no matching-time selector.
func BenchmarkGeneratedSharing(b *testing.B) {
	opts := GenOptions{Package: "shared", Start: "main", Types: true, Recognize: true}
	mode := os.Getenv("PEGO_GENERATED_SHARING")
	opts.disableMethodSharing = mode == "methods" || mode == "both"
	opts.disableLiteralSharing = mode == "literals" || mode == "both"
	opts.enableTSLiteralSharing = mode == "combined" || mode == "methods" || mode == "literals" || mode == "both"
	for _, name := range []string{"json", "typescript", "duckdb"} {
		src, err := os.ReadFile(filepath.Join("../../parsers", name, name+".pego"))
		if err != nil {
			b.Fatal(err)
		}
		g, err := syntax.Parse(string(src))
		if err != nil {
			b.Fatal(err)
		}
		for _, lang := range []string{"Go", "TypeScript"} {
			b.Run(name+"/"+lang, func(b *testing.B) {
				generate := func() ([]byte, error) { return Generate(g, opts) }
				if lang == "TypeScript" {
					tsOpts := opts
					tsOpts.Types = false
					generate = func() ([]byte, error) { return GenerateTS(g, tsOpts) }
				}
				code, err := generate()
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if _, err := generate(); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(len(code)), "source-bytes")
				marker := "func (p *"
				if lang == "TypeScript" {
					marker = "function e"
				}
				b.ReportMetric(float64(strings.Count(string(code), marker)), "helpers")
			})
		}
	}
}
