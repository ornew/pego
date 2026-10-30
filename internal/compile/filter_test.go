package compile

import (
	"strings"
	"testing"
)

func TestFilterTypeAndRuntimeValidation(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`rule main = v1:"a" => filter(list($v1), (x) => 1)`,
		`rule main = v1:"a" => filter(list($v1), (x, y) => true)`,
		`rule main = v1:"a" => filter(list($v1), true)`,
		`rule main = "a" => filter(1, (x) => true)`,
		`rule main = v1:"a" => filter(list($v1))`,
		`type Out = { Items []node[string] }
rule main = v1:"a" => Out{Items: filter(list($v1, nil), (x) => x != nil)}`,
	} {
		g, err := parseTest(source)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := compileStatic(g, Options{}, nil); err == nil {
			t.Fatalf("accepted invalid filter: %s", source)
		}
	}
	for _, source := range []string{
		`rule main = v1:"a" => filter(list($v1), (x) => 1)`,
		`rule main = v1:"a" => filter(list($v1), (x) => nil)`,
	} {
		if got := compileMessage(t, source); !strings.Contains(got, "filter: predicate must produce a bool") {
			t.Fatalf("%s: %q", source, got)
		}
	}
}

// The checker rejects a predicate of type _, so a predicate that produces another value than a
// bool is rejected when the grammar is compiled.
func TestFilterNonBoolPredicateIsRejected(t *testing.T) {
	t.Parallel()
	const src = `rule main = v1:"a" d:one $$ => filter(list($v1), (item) => $d)
rule one _ = _ => 1`
	if got := compileMessage(t, src); !strings.Contains(got, "filter: predicate must produce a bool, got one") {
		t.Errorf("got %q", got)
	}
}
