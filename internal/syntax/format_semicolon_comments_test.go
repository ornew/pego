package syntax

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

// commentOwners lists, for every node of g that owns comments, its path in the
// JSON form and the comments it owns.
func commentOwners(t *testing.T, g *grammar.Grammar) []string {
	t.Helper()
	data, err := grammar.MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	var out []string
	var walk func(v any, path string)
	walk = func(v any, path string) {
		switch v := v.(type) {
		case map[string]any:
			if src, ok := v["source"].(map[string]any); ok {
				var owned []string
				for k, c := range src {
					if strings.Contains(k, "omment") {
						owned = append(owned, fmt.Sprintf("%s=%v", k, c))
					}
				}
				if len(owned) > 0 {
					sort.Strings(owned)
					out = append(out, path+" "+strings.Join(owned, " "))
				}
			}
			for k, c := range v {
				walk(c, path+"/"+k)
			}
		case []any:
			for i, c := range v {
				walk(c, fmt.Sprintf("%s/%d", path, i))
			}
		}
	}
	walk(v, "")
	sort.Strings(out)
	return out
}

// TestFormatKeepsOwnersOfSharedLines checks that formatting source keeps the
// owner of each comment when several statements share a line, and that the
// output is stable.
func TestFormatKeepsOwnersOfSharedLines(t *testing.T) {
	tests := []struct{ name, src string }{
		{"declarations", "package p; // a\nvar v = 1; /* b */ rule r = \"a\"; /* c */ type U ~ int // d\n"},
		{"struct fields", "type T = { A int; /* y */ B string; C int } // z\nrule r = \"a\"\n"},
		{"operators rule", "rule ops = operators { operand \"n\"; /* e */ level { infix left \"+\"; /* f */ prefix \"-\" }; operand \"m\" // h\n}\n"},
		{"opening braces", "type T = { /* a */ A int }\nrule ops = operators { /* b */ operand \"n\"; level { /* c */ infix left \"+\" } }\n"},
		{"before semicolons", "package p /* a */ ;\ntype T = { A int /* x */ ; B string /* y */ ; C int }\nvar v = 1 /* z */; rule r = \"a\" /* w */ ; rule ops = operators { operand \"n\" /* e */ ; level { infix left \"+\" /* f */ ; prefix \"-\" } /* g */ ; operand \"m\" }\n"},
		{"field gaps", "type T = {\n\tA /* x */ /* y */ int\n\tB /* z */ string\n}\nrule r = \"a\"\n"},
		{"level", "rule ops = operators {\n\toperand \"n\"\n\tlevel { /* a */ infix left \"+\" /* b */ } // c\n}\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, err := Parse(tt.src)
			if err != nil {
				t.Fatal(err)
			}
			out := grammar.Format(g)
			again, err := Parse(out)
			if err != nil {
				t.Fatalf("output does not parse: %v\n%s", err, out)
			}
			if got := grammar.Format(again); got != out {
				t.Errorf("Format is not idempotent\nfirst:\n%s\nsecond:\n%s", out, got)
			}
			want, got := commentOwners(t, g), commentOwners(t, again)
			if strings.Join(want, "\n") != strings.Join(got, "\n") {
				t.Errorf("comment owners changed\nbefore:\n%s\nafter:\n%s\noutput:\n%s", strings.Join(want, "\n"), strings.Join(got, "\n"), out)
			}
		})
	}
}

// TestFormatKeepsFieldGaps checks that formatting source writes the comments
// between the name and the type of a field where they stand.
func TestFormatKeepsFieldGaps(t *testing.T) {
	g, err := Parse("type T = {\n\tA /* x */ /* y */ int\n\tB /* z */ string\n}\nrule r = \"a\"\n")
	if err != nil {
		t.Fatal(err)
	}
	out := grammar.Format(g)
	for _, want := range []string{"A /* x */ /* y */ int", "B /* z */ string"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q\n%s", want, out)
		}
	}
}
