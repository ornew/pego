package syntax

import (
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

// TestDeclarationHeaderCommentsJSON checks that comments inside a declaration
// header, between its keyword and the end of the header, belong to the
// declaration: they survive JSON and Format writes them after the keyword.
func TestDeclarationHeaderCommentsJSON(t *testing.T) {
	tests := []struct {
		name, src string
		want      []string
	}{
		{"package", "package /* hp */ p\nrule r = \"a\"\n", []string{"/* hp */"}},
		{"rule", "rule /* hr */ main /* hs */ = \"a\"\n", []string{"/* hr */", "/* hs */"}},
		{"rule two comments", "rule main /* h1 */ /* h2 */ = \"a\"\n", []string{"/* h1 */", "/* h2 */"}},
		{"rule typed", "rule main /* ht */ Foo /* hu */ = \"a\"\n", []string{"/* ht */", "/* hu */"}},
		{"type", "type /* hy */ T /* hz */ = { A int }\nrule r = \"a\"\n", []string{"/* hy */", "/* hz */"}},
		{"alias", "type U /* ha */ ~ /* hb */ int\nrule r = \"a\"\n", []string{"/* ha */", "/* hb */"}},
		{"node type", "type N /* hn */ = /* ho */ node[string]\nrule r = \"a\"\n", []string{"/* hn */", "/* ho */"}},
		{"var", "var /* hv */ v /* hw */ int = 1\nrule r = \"a\"\n", []string{"/* hv */", "/* hw */"}},
		{"field", "type T = { A /* hf */ /* hg */ int ; B /* hh */ string }\nrule r = \"a\"\n", []string{"/* hf */", "/* hg */", "/* hh */"}},
		{"multi-line field", "type T = {\n\tA /* hf */ int\n\tB /* hg */ string\n}\nrule r = \"a\"\n", []string{"/* hf */", "/* hg */"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, err := Parse(tt.src)
			if err != nil {
				t.Fatal(err)
			}
			data, err := grammar.MarshalJSON(g)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := grammar.UnmarshalJSON(data)
			if err != nil {
				t.Fatal(err)
			}
			out := grammar.Format(decoded)
			for _, c := range tt.want {
				if !strings.Contains(out, c) {
					t.Errorf("comment %s lost through JSON\njson: %s\nout:\n%s", c, data, out)
				}
			}
			if again := grammar.Format(decoded); again != out {
				t.Errorf("Format is not stable")
			}
			// Formatting source moves a comment between a field name and its
			// type before the field, so the output reaches a fixed point
			// after one more pass.
			reparsed, err := Parse(out)
			if err != nil {
				t.Fatalf("output does not parse: %v\n%s", err, out)
			}
			once := grammar.Format(reparsed)
			reparsed, err = Parse(once)
			if err != nil {
				t.Fatalf("formatted output does not parse: %v\n%s", err, once)
			}
			if twice := grammar.Format(reparsed); twice != once {
				t.Errorf("Format is not idempotent\nfirst:\n%s\nsecond:\n%s", once, twice)
			}
			for _, c := range tt.want {
				if !strings.Contains(once, c) {
					t.Errorf("comment %s lost by formatting source\n%s", c, once)
				}
			}
		})
	}
}
