package pego_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ornew/pego"
)

func TestWithGeneratedMaxDepth(t *testing.T) {
	parser, err := pego.CompileSource(`def main = "x"`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, ts := range []bool{false, true} {
		generate := func(opts ...pego.GenOption) ([]byte, error) {
			return pego.GenerateGo(parser.Grammar(), "depth", "main", opts...)
		}
		if ts {
			generate = func(opts ...pego.GenOption) ([]byte, error) {
				return pego.GenerateTypeScript(parser.Grammar(), "main", opts...)
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
		if err != nil || !strings.Contains(string(got), "const defaultMaxDepth = 7") {
			t.Fatalf("last generation option: %v", err)
		}
		if got, err := generate(pego.WithGeneratedMaxDepth(-1)); err == nil || len(got) != 0 {
			t.Fatal("invalid generated max depth accepted")
		}
	}
}
