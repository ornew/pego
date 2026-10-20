package pego_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ornew/pego"
)

func TestWithGeneratedMaxDepth(t *testing.T) {
	parser, err := pego.CompileSource(`rule main = "x"`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, ts := range []bool{false, true} {
		generate := func(opts ...pego.GenOption) ([]byte, error) {
			files, err := pego.GenerateGo(parser.Grammar(), "depth", "main", append(opts, pego.WithRuntime(pego.RuntimeImport))...)
			return joinFiles(files), err
		}
		if ts {
			generate = func(opts ...pego.GenOption) ([]byte, error) {
				files, err := pego.GenerateTypeScript(parser.Grammar(), "main", opts...)
				return joinFiles(files), err
			}
		}
		base, err := generate()
		if err != nil {
			t.Fatal(err)
		}
		for _, depth := range []int{0, 100000} {
			got, err := generate(pego.WithGeneratedMaxDepth(depth))
			if err != nil || !bytes.Equal(base, got) {
				t.Fatalf("equivalent default: %v", err)
			}
		}
		got, err := generate(pego.WithGeneratedMaxDepth(-1), pego.WithGeneratedMaxDepth(7))
		want := "var pegoGrammar = pegort.NewGrammar(7)"
		if ts {
			want = "const defaultMaxDepth = 7"
		}
		if err != nil || !strings.Contains(string(got), want) {
			t.Fatalf("last generation option: %v", err)
		}
		if got, err := generate(pego.WithGeneratedMaxDepth(-1)); err == nil || len(got) != 0 {
			t.Fatal("invalid generated max depth accepted")
		}
	}
}

// joinFiles returns the contents of the generated files one after the other.
func joinFiles(files []pego.GeneratedFile) []byte {
	var out []byte
	for _, f := range files {
		out = append(out, f.Content...)
	}
	return out
}
