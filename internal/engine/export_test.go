package engine

import "testing"

// CorpusCase is a grammar of the test corpus with its inputs, for external tests.
type CorpusCase struct {
	Name, Src string
	Inputs    []string
}

// Corpus returns the grammars that genCorpus returns, for external tests (sample_test.go).
func Corpus(t *testing.T) []CorpusCase {
	var cs []CorpusCase
	for _, c := range genCorpus(t) {
		cs = append(cs, CorpusCase{Name: c.name, Src: c.src, Inputs: c.inputs})
	}
	return cs
}
