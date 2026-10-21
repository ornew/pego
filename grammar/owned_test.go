package grammar

import "testing"

// A grammar built by hand writes the comments its nodes own: a text that ends
// in a line break is line comments, any other text is a block comment, and a
// trailing comment over several lines is one block comment.
func TestFormatWritesOwnedComments(t *testing.T) {
	rule := func(leading, trailing string, detached ...string) *Grammar {
		r := &RuleDef{Name: "r", Expr: &Literal{Value: "a"}}
		r.LeadingComments, r.TrailingComments, r.LeadingDetachedComments = leading, trailing, detached
		return &Grammar{File: File{Decls: []Decl{r}}}
	}
	for _, tc := range []struct {
		name string
		g    *Grammar
		want string
	}{
		{"line comments", rule(" one\n two\n", " end\n"), "// one\n// two\nrule r = \"a\" // end\n"},
		{"block comment", rule(" doc ", " end "), "/* doc */\nrule r = \"a\" /* end */\n"},
		{"detached paragraphs", rule(" lead\n", "", " first\n", " second\n"), "// first\n\n// second\n\n// lead\nrule r = \"a\"\n"},
		{"trailing over lines", rule("", " a\n b\n"), "rule r = \"a\" /* a\n * b\n*/\n"},
		{"line comment at the end of a file", rule("", " tail"), "rule r = \"a\" /* tail */\n"},
		{"block comment end in the text", rule(" a */ b ", ""), "/* a * / b */\nrule r = \"a\"\n"},
		{"none", rule("", ""), "rule r = \"a\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Format(tc.g); got != tc.want {
				t.Fatalf("got:\n%q\nwant:\n%q", got, tc.want)
			}
		})
	}
}
