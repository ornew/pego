package python_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/python"
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
		n, err := python.Parse(string(data))
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

// TestAcceptance checks inputs at the border of the language against what CPython 3.14.0 does (accept
// or reject), which differential testing found: the end of the input after a line continuation.
func TestAcceptance(t *testing.T) {
	for src, ok := range map[string]bool{
		"a\n\\\n ":                        true,
		"a\n\\\n \n":                      true,
		"a \\\n ":                         true,
		"a\n \\\n ":                       true,
		"a\n\\\n#c":                       true,
		"if 1:\n  a\n\\\n ":               true,
		"a\n\\\n\\\n ":                    true,
		"\\\n ":                           true,
		"a\\\n":                           false,
		"a\n\\\n":                         false,
		"\\\n":                            false,
		"a = (1 \\\n ":                    false,
		"x = 1\x00\n":                     false,
		"\ufeffx = 1\n":                   false,
		" x = 1\n":                        false,
		"if 1:\n\tx = 1\n        y = 2\n": false,
	} {
		if _, err := python.ParseModule(src); (err == nil) != ok {
			t.Errorf("%q: accepted %v, want %v (%v)", src, err == nil, ok, err)
		}
	}
}

// FuzzParse checks that the parser does not panic and that ParseAST, Recognize and Dump agree.
func FuzzParse(f *testing.F) {
	inputs, _ := filepath.Glob("testdata/*.txt")
	for _, in := range inputs {
		data, _ := os.ReadFile(in)
		f.Add(string(data))
	}
	f.Add("a\n\\\n ")
	f.Add("if x:\n\tpass\n  else: 1\n")
	f.Add("f'{x!r:>{w}}' t'{y=}'")
	f.Fuzz(func(t *testing.T, src string) {
		m, err := python.ParseAST(src)
		if rerr := python.Recognize(src); (rerr == nil) != (err == nil) {
			t.Fatalf("ParseAST: %v, Recognize: %v", err, rerr)
		}
		if err != nil {
			return
		}
		python.Check(m, src)
		python.Dump(m)
		python.DumpWithPositions(m, src)
	})
}
