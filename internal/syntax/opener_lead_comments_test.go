package syntax

import (
	"slices"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

// The comments on the line of an opening brace lead the first item, as if they were on lines
// of their own: the brace is not a node, so nothing could own them as a trailing comment.
func TestCommentsAfterAnOpeningBraceLeadTheFirstItem(t *testing.T) {
	g, err := Parse("type T = {/*a*/ /*b*/ // c\n    A int\n}\ntype W = { // w1\n    B int\n}\n")
	if err != nil {
		t.Fatal(err)
	}
	fields := func(i int) []*grammar.Field {
		return g.File.Decls[i].(*grammar.TypeDef).Type.(*grammar.StructType).Fields
	}
	a := fields(0)[0]
	if a.LeadingComments != " c\n" || !slices.Equal(a.LeadingDetachedComments, []string{"a", "b"}) || len(a.TrailingComments) != 0 {
		t.Errorf("A owns %+v", a.SourceInfo)
	}
	b := fields(1)[0]
	if b.LeadingComments != " w1\n" || len(b.LeadingDetachedComments) != 0 {
		t.Errorf("B owns %+v", b.SourceInfo)
	}
}

// Through formatting source, the comments stay on the line of the brace; through JSON they are
// kept, written before the first item.
func TestCommentsAfterAnOpeningBraceFormatAndJSON(t *testing.T) {
	for _, c := range []struct{ name, src, wantJSON string }{
		{"run of three", "type T = { /*a*/ /*b*/ // c\n    A int\n}\n", "type T = {\n\n    /*a */\n\n    /*b */\n\n    // c\n    A int\n}\n"},
		{"line comment", "type W = { // w1\n    B int\n}\n", "type W = {\n    // w1\n    B int\n}\n"},
		{"block and line comment", "type W = { /*a*/ // w1\n    B int\n}\n", "type W = {\n\n    /*a */\n\n    // w1\n    B int\n}\n"},
		{"one-line struct", "type R = { /*a*/ // b\n    A int\n}\n", "type R = {\n\n    /*a */\n\n    // b\n    A int\n}\n"},
		{"operators", "rule e = operators { /*a*/ /*b*/ // c\n    operand \"1\"\n}\n", "rule e = operators {\n\n    /*a */\n\n    /*b */\n\n    // c\n    operand \"1\"\n}\n"},
		{"level", "rule e = operators {\n    operand \"1\"\n    level { /*a*/ // c\n        infix left \"+\"\n    }\n}\n", "rule e = operators {\n    operand \"1\"\n    level {\n\n        /*a */\n\n        // c\n        infix left \"+\"\n    }\n}\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if out := checkFormat(t, c.src); out != c.src {
				t.Fatalf("formatted source:\n%q\nwant:\n%q", out, c.src)
			}
			g, err := Parse(c.src)
			if err != nil {
				t.Fatal(err)
			}
			data, err := grammar.MarshalJSON(g)
			if err != nil {
				t.Fatal(err)
			}
			back, err := grammar.UnmarshalJSON(data)
			if err != nil {
				t.Fatal(err)
			}
			if got := grammar.Format(back); got != c.wantJSON {
				t.Fatalf("formatted JSON:\n%q\nwant:\n%q", got, c.wantJSON)
			}
			if n := strings.Count(c.src, "//") + strings.Count(c.src, "/*"); n != strings.Count(c.wantJSON, "//")+strings.Count(c.wantJSON, "/*") {
				t.Fatalf("test expectation lost a comment")
			}
		})
	}
}
