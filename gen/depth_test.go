package gen_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ornew/pego/gen"
)

func TestMaxDepth(t *testing.T) {
	parser, err := compileTestSource(`rule main = "x"`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, ts := range []bool{false, true} {
		generate := func(depth int) ([]byte, error) {
			files, err := gen.Go{Module: "depth", Runtime: gen.RuntimeImport, MaxDepth: depth}.Generate(parser.Grammar(), "main")
			return joinFiles(files), err
		}
		if ts {
			generate = func(depth int) ([]byte, error) {
				files, err := gen.TypeScript{MaxDepth: depth}.Generate(parser.Grammar(), "main")
				return joinFiles(files), err
			}
		}
		base, err := generate(0)
		if err != nil {
			t.Fatal(err)
		}
		got, err := generate(100000)
		if err != nil || !bytes.Equal(base, got) {
			t.Fatalf("equivalent default: %v", err)
		}
		got, err = generate(7)
		want := "var pegoGrammar = rt.NewGrammar(7)"
		if ts {
			want = "const G = mkGrammar(7, "
		}
		if err != nil || !strings.Contains(string(got), want) {
			t.Fatalf("generated max depth: %v", err)
		}
		if got, err := generate(-1); err == nil || len(got) != 0 {
			t.Fatal("invalid generated max depth accepted")
		}
	}
}

// joinFiles returns the contents of the generated files one after the other.
func joinFiles(files []gen.File) []byte {
	var out []byte
	for _, f := range files {
		out = append(out, f.Content...)
	}
	return out
}
