package compile

import (
	"strings"
	"testing"
)

func TestPredicateBlocksStatementValidation(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`var x int
rule main = {x := 1; len(true)} "a"`,
		`rule main = {true; $1 == nil} "a"`,
		`rule main = {true; text($missing) == "a"} "a"`,
	} {
		// ParseLegacy reads $1, which still reaches the checker from JSON grammars.
		g, err := parseLegacyTest(source)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := compileStatic(g, Options{}, nil); err == nil {
			t.Fatalf("accepted invalid statement: %s", source)
		}
	}
	for _, source := range []string{`rule main = {}`, `rule main = "a" => {true; true}`} {
		g, err := parseTest(source)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := compileStatic(g, Options{}, nil); err == nil || !strings.Contains(err.Error(), "action statement blocks are not implemented") {
			t.Fatalf("unsupported boundary %s: %v", source, err)
		}
	}
}
