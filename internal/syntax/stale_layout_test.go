package syntax

import (
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

// An attribute edited after it was read has no layout for the new arguments,
// but its comments are still written.
func TestEditedAttributeKeepsComments(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		edit func(a *grammar.Attribute)
		want string
	}{
		{"appended argument", "rule r = \"a\" @custom(1, /* a */ 2) \"b\"\n",
			func(a *grammar.Attribute) { a.Args = append(a.Args, 3) },
			"rule r = \"a\" @custom(\n    /* a */\n    1,\n    2,\n    3,\n) \"b\"\n"},
		{"arguments removed", "rule r = \"a\" @custom(1, /* a */ 2) \"b\"\n",
			func(a *grammar.Attribute) { a.Args = nil },
			"rule r = \"a\" @custom(\n    /* a */\n) \"b\"\n"},
		{"line comment", "rule r = \"a\" @custom(1, // a\n    2) \"b\"\n",
			func(a *grammar.Attribute) { a.Args = append(a.Args, 3) },
			"rule r = \"a\" @custom(\n    // a\n    1,\n    2,\n    3,\n) \"b\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, err := Parse(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			a := g.Rules()[0].Expr.(*grammar.Seq).Items[0].(*grammar.Attributed).Attrs[0]
			tc.edit(a)
			got := grammar.Format(g)
			if got != tc.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tc.want)
			}
			if !strings.Contains(got, "a */") && !strings.Contains(got, "// a") {
				t.Fatalf("the comment was dropped:\n%s", got)
			}
		})
	}
}
