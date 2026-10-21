package cue

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGolden checks the generated parser against the golden files of testdata, which the tests of
// github.com/ornew/pego/parsers check every backend of the engine against.
func TestGolden(t *testing.T) {
	inputs, _ := filepath.Glob("testdata/*.txt")
	if len(inputs) == 0 {
		t.Fatal("no inputs")
	}
	for _, in := range inputs {
		data, err := os.ReadFile(in)
		if err != nil {
			t.Fatal(err)
		}
		var got string
		n, err := ParseAST(string(data))
		if n != nil {
			got = render(n)
		}
		if err != nil {
			if got != "" {
				got += "\n"
			}
			got += "error: " + err.Error()
		}
		want, err := os.ReadFile(strings.TrimSuffix(in, ".txt") + ".golden")
		if err != nil {
			t.Fatal(err)
		}
		if withoutZeroFields(got)+"\n" != withoutZeroFields(string(want)) {
			t.Errorf("%s\n got  %s\n want %s", in, got, want)
		}
	}
}
