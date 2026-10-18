package grammar_test

import (
	"slices"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/grammar"
)

func TestTypeRefs(t *testing.T) {
	g, err := pego.ParseGrammar("type Box = { V any; L []Box; O *string }\ntype U ~ Box | Other\ntype Other terminal\n" +
		"rule main Box = x:\"x\" => Box{V: $x.(any), L: []Box{}, O: nil}\n")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range grammar.TypeRefs(g) {
		got = append(got, r.Name)
	}
	want := []string{"any", "Box", "string", "Box", "Other", "Box", "any", "Box"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if refs := grammar.TypeRefs(nil); refs != nil {
		t.Errorf("nil: got %v", refs)
	}
}
