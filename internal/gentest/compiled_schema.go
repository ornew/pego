package gentest

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// CorpusWithParsers returns the grammars of the schema agreement tests: the backend corpus and the
// grammars of the parsers directory, which are the examples of the generated parsers.
func CorpusWithParsers(t *testing.T) []Case {
	cases := GenCorpus(t)
	paths, err := filepath.Glob(path(t, "../../parsers/*/*.pego"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if testing.Short() && len(src) > 20000 {
			continue
		}
		cases = append(cases, Case{Name: "parsers/" + filepath.Base(filepath.Dir(path)), Src: string(src)})
	}
	return cases
}

// SchemaCorpus returns the grammars of GenCorpus and of the constructor, field check and result check
// tests, for tests of values against output schemas.
func SchemaCorpus(t *testing.T) []Case {
	return slices.Concat(GenCorpus(t), ConstructorCases(), FieldCheckCases(), ResultCheckCases())
}
