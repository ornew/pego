package engine

import (
	"strings"
	"testing"
)

// A hidden rule without an action adds no type to the schema, except where a recursive call of it
// is an element of its own, which the type of the call refers to.
func TestHiddenRulesAddNoSchemaType(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		src  string
		want []string
	}{
		{"rule main = \"a\" _m \"d\"\nrule _m = \"b\" \"c\"", []string{"main"}},
		{"rule main = _l\nrule _l = \"x\" _l / \"x\"", []string{"_l", "main"}},
	} {
		p := mustCompile(t, c.src)
		s, err := outputSchemaPrototype(p, "main", false)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, d := range s.Types {
			got = append(got, d.Name)
		}
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s: types %v, want %v", c.src, got, c.want)
		}
		for _, r := range s.Rules {
			if strings.HasPrefix(r.Name, "_") && r.Name != "_l" {
				t.Errorf("%s: rule %s", c.src, r.Name)
			}
		}
	}
}
