// Package examples_test parses the testdata inputs of every example grammar and compares the results with
// golden files.
//
// Every .pego file in a directory is run against that directory's testdata/*.txt. Grammars in the same
// directory must produce the same results, except where a result depends on how the grammar is written
// (such as the expected alternatives of a syntax error); such results can be overridden per grammar with
// <input>.<grammar>.golden. Run go test ./examples -update to regenerate the golden files.
package examples_test

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego"
)

var update = flag.Bool("update", false, "update golden files")

func TestExamples(t *testing.T) {
	grammars, err := filepath.Glob("*/*.pego")
	if err != nil {
		t.Fatal(err)
	}
	if len(grammars) == 0 {
		t.Fatal("no grammars")
	}
	written := map[string]string{} // contents of the shared golden files written by -update
	for _, gpath := range grammars {
		t.Run(gpath, func(t *testing.T) {
			src, err := os.ReadFile(gpath)
			if err != nil {
				t.Fatal(err)
			}
			p, err := pego.CompileSource(string(src), "main")
			if err != nil {
				t.Fatalf("%s:%v", gpath, err)
			}
			inputs, _ := filepath.Glob(filepath.Join(filepath.Dir(gpath), "testdata", "*.txt"))
			if len(inputs) == 0 {
				t.Fatal("no inputs")
			}
			for _, in := range inputs {
				data, err := os.ReadFile(in)
				if err != nil {
					t.Fatal(err)
				}
				// Print the tree and the errors, including those recovered with #recover.
				var got string
				n, err := p.Parse(string(data))
				if n != nil {
					got = n.String()
				}
				if err != nil {
					if got != "" {
						got += "\n"
					}
					got += "error: " + err.Error()
				}
				base := strings.TrimSuffix(in, ".txt")
				golden := base + ".golden"
				own := base + "." + strings.TrimSuffix(filepath.Base(gpath), ".pego") + ".golden"
				if _, err := os.Stat(own); err == nil {
					golden = own
				}
				if *update {
					// The first grammar in a directory writes the shared golden file; other grammars write their own only where they differ.
					golden = base + ".golden"
					if shared, ok := written[golden]; ok && shared != got {
						golden = own
					} else {
						written[golden] = got
						os.Remove(own)
					}
					if err := os.WriteFile(golden, []byte(got+"\n"), 0o644); err != nil {
						t.Fatal(err)
					}
					continue
				}
				want, err := os.ReadFile(golden)
				if err != nil {
					t.Fatalf("%v (run with -update to create it)", err)
				}
				if got+"\n" != string(want) {
					t.Errorf("%s\n got  %s\n want %s", in, got, want)
				}
			}
		})
	}
}
