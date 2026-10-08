package typescript_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/typescript"
)

// TestGolden checks the generated parser against the golden files of testdata, which the tests of
// github.com/ornew/pego/parsers check every backend of the engine against. An input starting with NUL is
// parsed as a .tsx file (see ParseTSX).
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
		n, err := typescript.Parse(string(data))
		if n != nil {
			got = n.String()
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
		if got+"\n" != string(want) {
			t.Errorf("%s\n got  %s\n want %s", in, got, want)
		}
	}
}
