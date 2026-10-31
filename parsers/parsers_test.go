package parsers_test

import (
	"bufio"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/gen"
)

var update = flag.Bool("update", false, "update golden files")

// generation is a zp gen command of generate.go.
type generation struct {
	grammar, module, out string
	hasTypes             bool
	config               gen.Go
}

// generations reads the go:generate lines of generate.go.
func generations(t *testing.T) []generation {
	t.Helper()
	return generationsIn(t, ".")
}

// generationsIn reads the go:generate lines of the generate.go in dir. The grammar and output paths
// are relative to dir.
func generationsIn(t *testing.T, dir string) []generation {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, "generate.go"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var gens []generation
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		args, ok := strings.CutPrefix(sc.Text(), "//go:generate go run ../cmd/zp gen ")
		if !ok {
			continue
		}
		var g generation
		fs := strings.Fields(args)
		for i := 0; i < len(fs); i++ {
			switch fs[i] {
			case "-g":
				i++
				g.grammar = fs[i]
			case "-module":
				i++
				g.module = fs[i]
			case "-o":
				i++
				g.out = fs[i]
			case "-runtime":
				i++
				if fs[i] != "import" {
					t.Fatalf("generate.go: unknown runtime mode %s", fs[i])
				}
				g.config.Runtime = gen.RuntimeImport
			case "-types":
				g.hasTypes = true
				g.config.Types = true
			case "-expectations":
				g.config.Expectations = true
			case "-recognize":
				g.config.Recognize = true
			case "-budget-controls":
				g.config.BudgetControls = true
			case "-typed-only":
				g.config.TypedOnly = true
			case "-document":
				g.config.Document = true
			case "-stream":
				g.config.Stream = true
			case "-nodoc":
				g.config.OmitPackageDoc = true
			default:
				t.Fatalf("generate.go: unknown argument %s", fs[i])
			}
		}
		g.grammar = filepath.Join(dir, g.grammar)
		g.out = filepath.Join(dir, g.out)
		gens = append(gens, g)
	}
	if len(gens) == 0 {
		t.Fatal("generate.go generates nothing")
	}
	return gens
}

// TestGeneratedParsersAreUpToDate checks that every file of a generated package is what zp gen
// generates from its grammar (run go generate ./parsers after changing a grammar or the generator).
func TestGeneratedParsersAreUpToDate(t *testing.T) {
	for _, g := range generations(t) {
		src, err := os.ReadFile(g.grammar)
		if err != nil {
			t.Fatal(err)
		}
		gr, err := zp.ParseGrammar(string(src))
		if err != nil {
			t.Fatalf("%s:%v", g.grammar, err)
		}
		c := g.config
		c.Module = g.module
		want, err := c.Generate(gr, "main")
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range want {
			path := filepath.Join(g.out, filepath.FromSlash(f.Path))
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(f.Content) {
				t.Errorf("%s is out of date; run go generate ./parsers", path)
			}
		}
	}
}

// TestBackends parses the inputs of each parser's testdata (testdata/*.txt) with every backend of the
// engine and compares the trees and errors with the golden files (testdata/*.golden). The modules test
// their generated parsers against the same files. Run go test ./parsers -update to rewrite them.
func TestBackends(t *testing.T) {
	backends := []struct {
		name string
		b    zp.Backend
	}{{"closure", zp.Closure}, {"bytecode", zp.Bytecode}, {"iterative", zp.BytecodeIterative}}
	for _, g := range generations(t) {
		t.Run(filepath.Dir(g.grammar), func(t *testing.T) {
			src, err := os.ReadFile(g.grammar)
			if err != nil {
				t.Fatal(err)
			}
			p, err := zp.CompileSource(string(src), "main")
			if err != nil {
				t.Fatalf("%s:%v", g.grammar, err)
			}
			inputs, _ := filepath.Glob(filepath.Join(filepath.Dir(g.grammar), "testdata", "*.txt"))
			if len(inputs) == 0 {
				t.Fatal("no inputs")
			}
			for _, in := range inputs {
				data, err := os.ReadFile(in)
				if err != nil {
					t.Fatal(err)
				}
				golden := strings.TrimSuffix(in, ".txt") + ".golden"
				for i, b := range backends {
					// Print the tree and the errors, including those recovered with #recover.
					var got string
					opts := []zp.ParseOption{zp.WithBackend(b.b)}
					if g.config.Expectations {
						opts = append(opts, zp.WithExpectations())
					}
					n, err := p.Parse(string(data), opts...)
					if n != nil {
						got = n.String()
					}
					if err != nil {
						if got != "" {
							got += "\n"
						}
						got += "error: " + err.Error()
					}
					got += "\n"
					if *update && i == 0 {
						if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
							t.Fatal(err)
						}
					}
					want, err := os.ReadFile(golden)
					if err != nil {
						t.Fatalf("%v (run with -update to create it)", err)
					}
					if got != string(want) {
						t.Errorf("%s (%s)\n got  %s\n want %s", in, b.name, got, want)
					}
				}
			}
		})
	}
}
