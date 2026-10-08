package parsers_test

import (
	"bufio"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego"
)

var update = flag.Bool("update", false, "update golden files")

// generation is a pego gen command of generate.go.
type generation struct {
	grammar, pkg, out string
	opts              []pego.GenOption
}

// generations reads the go:generate lines of generate.go.
func generations(t *testing.T) []generation {
	t.Helper()
	f, err := os.Open("generate.go")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var gens []generation
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		args, ok := strings.CutPrefix(sc.Text(), "//go:generate go run ../cmd/pego gen ")
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
			case "-pkg":
				i++
				g.pkg = fs[i]
			case "-o":
				i++
				g.out = fs[i]
			case "-types":
				g.opts = append(g.opts, pego.WithTypes())
			case "-recognize":
				g.opts = append(g.opts, pego.WithRecognize())
			case "-nodoc":
				g.opts = append(g.opts, pego.WithoutPackageDoc())
			default:
				t.Fatalf("generate.go: unknown argument %s", fs[i])
			}
		}
		gens = append(gens, g)
	}
	if len(gens) == 0 {
		t.Fatal("generate.go generates nothing")
	}
	return gens
}

// TestGeneratedParsersAreUpToDate checks that every parser.go is what pego gen generates from its
// grammar (run go generate ./parsers after changing a grammar or the generator).
func TestGeneratedParsersAreUpToDate(t *testing.T) {
	for _, g := range generations(t) {
		src, err := os.ReadFile(g.grammar)
		if err != nil {
			t.Fatal(err)
		}
		gr, err := pego.ParseGrammar(string(src))
		if err != nil {
			t.Fatalf("%s:%v", g.grammar, err)
		}
		want, err := pego.GenerateGo(gr, g.pkg, "main", g.opts...)
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(g.out)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("%s is out of date; run go generate ./parsers", g.out)
		}
	}
}

// TestBackends parses the inputs of each parser's testdata (testdata/*.txt) with every backend of the
// engine and compares the trees and errors with the golden files (testdata/*.golden). The modules test
// their generated parsers against the same files. Run go test ./parsers -update to rewrite them.
func TestBackends(t *testing.T) {
	backends := []struct {
		name string
		b    pego.Backend
	}{{"closure", pego.Closure}, {"bytecode", pego.Bytecode}, {"iterative", pego.BytecodeIterative}}
	for _, g := range generations(t) {
		t.Run(filepath.Dir(g.grammar), func(t *testing.T) {
			src, err := os.ReadFile(g.grammar)
			if err != nil {
				t.Fatal(err)
			}
			p, err := pego.CompileSource(string(src), "main")
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
					n, err := p.Parse(string(data), pego.WithBackend(b.b))
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
