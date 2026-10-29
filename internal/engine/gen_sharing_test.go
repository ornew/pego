package engine

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gen/gogen"
	"github.com/ornew/pego/internal/gen/tsgen"
	"github.com/ornew/pego/internal/gentest"
)

func TestGeneratedSharingIdentity(t *testing.T) {
	t.Parallel()
	ts := &tsgen.Generator{Generator: gen.Generator{Table: "rules"}}
	name := ts.Method("first", "return [null, true];\n")
	ts.Table = "recRules"
	if ts.Method("recognition", "return [null, true];\n") != name {
		t.Fatal("identical TypeScript bodies should share across rule tables")
	}
	g := &gogen.Generator{Table: "rules"}
	a := g.Method("first", "return nil, true\n")
	g.Table = "recRules"
	if g.Method("recognition", "return nil, true\n") != a {
		t.Fatal("identical Node bodies should share across rule tables")
	}
	g.Table = "trules"
	if g.Method("typed", "return nil, true\n") == a {
		t.Fatal("different receivers and value types must not share")
	}
	g.Table = "rules"
	for _, body := range []string{
		"return p.MatchLiteral(lit1, \"x\", 4, false)\n",
		"return p.MatchLiteral(lit1, \"x\", 5, false)\n",
		"return p.MatchLiteral(lit1, \"x\", 4, true)\n",
		"return p.Call(rules[0], 0)\n",
		"return p.Call(recRules[0], 0)\n",
		"return p.Frame.Vals[0], true\n",
		"return p.Frame.Vals[1], true\n",
	} {
		before := g.Methods.Len()
		g.Method("", body)
		if g.Methods.Len() == before {
			t.Fatal("semantically distinct bodies must not share")
		}
	}
}

func TestGeneratedSharingDeterministic(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedSharingDeterministic)
}

func testGeneratedSharingDeterministic(t *testing.T) {
	g, err := parseTest(`rule main = a "!" / a "?"
rule a = ("é" / "ab")+ #atomic`)
	if err != nil {
		t.Fatal(err)
	}
	for _, generate := range []struct {
		name string
		fn   func(gen.GenOptions) ([]byte, error)
	}{
		{"Go", func(opts gen.GenOptions) ([]byte, error) { return gentest.GenerateCode(t, g, opts) }},
		{"TypeScript", func(opts gen.GenOptions) ([]byte, error) { return generateTSBundle(g, opts) }},
	} {
		t.Run(generate.name, func(t *testing.T) {
			opts := gen.GenOptions{Package: "shared", Start: "main", Recognize: true}
			first, err := generate.fn(opts)
			if err != nil {
				t.Fatal(err)
			}
			second, err := generate.fn(opts)
			if err != nil || !bytes.Equal(first, second) {
				t.Fatal("generation must be deterministic")
			}
			if generate.name == "TypeScript" {
				opts.DisableLiteralSharing = true
				ref, err := generate.fn(opts)
				if err != nil || !bytes.Equal(first, ref) {
					t.Fatal("production TypeScript must leave literal tables unshared")
				}
				opts.EnableTSLiteralSharing = true
				ref, err = generate.fn(opts)
				if err != nil || !bytes.Equal(first, ref) {
					t.Fatal("explicit literal-sharing disable must override the experiment")
				}
				opts.DisableLiteralSharing = false
				experiment, err := generate.fn(opts)
				if err != nil || len(experiment) >= len(first) {
					t.Fatal("fixture must exercise experimental literal sharing")
				}
			}
			opts.DisableMethodSharing, opts.DisableLiteralSharing = true, true
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
	t.Parallel()
	gentest.EachMode(t, testGeneratedUnsharedParsersMatchEngine)
}

func testGeneratedUnsharedParsersMatchEngine(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	opts := gen.GenOptions{DisableMethodSharing: true, DisableLiteralSharing: true}
	testGeneratedParsersCorpus(t, filepath.Join(runtime.GOROOT(), "bin", "go"), gentest.GenCorpus(t), opts)
	testGeneratedTypesCorpus(t, filepath.Join(runtime.GOROOT(), "bin", "go"), gentest.GenCorpus(t), opts)
}

func TestGeneratedUnsharedTSParsersMatchEngine(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs generated TypeScript")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	testGeneratedTSParsersCorpus(t, node, gentest.GenCorpus(t), gen.GenOptions{DisableMethodSharing: true, DisableLiteralSharing: true})
}

// PEGO_GENERATED_SHARING selects generation-only controls: combined also
// enables experimental TypeScript literal sharing; methods, literals, or
// both disables the corresponding mechanism in that experiment. Empty uses
// production defaults. Standalone parsers have no matching-time selector.
func BenchmarkGeneratedSharing(b *testing.B) {
	opts := gen.GenOptions{Package: "shared", Start: "main", Types: true, Recognize: true}
	mode := os.Getenv("PEGO_GENERATED_SHARING")
	opts.DisableMethodSharing = mode == "methods" || mode == "both"
	opts.DisableLiteralSharing = mode == "literals" || mode == "both"
	opts.EnableTSLiteralSharing = mode == "combined" || mode == "methods" || mode == "literals" || mode == "both"
	for _, name := range []string{"json", "typescript", "duckdb"} {
		src, err := os.ReadFile(filepath.Join("../../parsers", name, name+".pego"))
		if err != nil {
			b.Fatal(err)
		}
		g, err := parseTest(string(src))
		if err != nil {
			b.Fatal(err)
		}
		for _, lang := range []string{"Go", "TypeScript"} {
			b.Run(name+"/"+lang, func(b *testing.B) {
				generate := func() ([]byte, error) { return gentest.GenerateCode(b, g, opts) }
				if lang == "TypeScript" {
					tsOpts := opts
					tsOpts.Types = false
					generate = func() ([]byte, error) { return generateTSBundle(g, tsOpts) }
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
