package tsgen_test

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

// The TypeScript type of an anonymous join is the union of its members, in a field, a list
// element and the start value.
func TestGeneratedTypedAnonymousJoinTypeScript(t *testing.T) {
	t.Parallel()
	g, err := parseTest(gentest.AnonJoinGrammar)
	if err != nil {
		t.Fatal(err)
	}
	files, err := gentest.GenerateTS(g, gen.GenOptions{Types: true, Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Path != "types.ts" {
			continue
		}
		c := string(f.Content)
		for _, want := range []string{
			`readonly "Else": RecoveredError | null | Leaf | Pair;`,
			`readonly "Items": readonly (RecoveredError | null | Pair | Leaf)[] | null;`,
		} {
			if !strings.Contains(c, want) {
				t.Errorf("types.ts lacks %q:\n%s", want, c)
			}
		}
		if strings.Contains(c, `"Else": TypedValue`) {
			t.Error("Else is TypedValue")
		}
	}
}
