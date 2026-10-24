package syntax

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

func TestSourceGroups(t *testing.T) {
	const src = `type Number = { Text string }
type Alias ~ (
 // type documentation
 (Number) // type tail
)
rule main (Alias) = v1:"x" => (
 // action documentation
 (Number{Text: list($v1)}) // action tail
)
`
	g, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	typ := g.Types()[1].(*grammar.TypeAlias).Type.(*grammar.TypeGroup)
	innerType := typ.Type.(*grammar.TypeGroup)
	action := g.Rules()[0].Action.(*grammar.TermGroup)
	innerAction := action.Term.(*grammar.TermGroup)
	if innerType.LeadingComments != " type documentation\n" || !slices.Equal(innerType.TrailingComments, []string{" type tail\n"}) || innerAction.LeadingComments != " action documentation\n" || !slices.Equal(innerAction.TrailingComments, []string{" action tail\n"}) {
		t.Fatalf("group ownership: %+v %+v", innerType.SourceInfo, innerAction.SourceInfo)
	}
	if typ.Pos != (grammar.Pos{Line: 2, Col: 14}) || typ.End != (grammar.Pos{Line: 5, Col: 2}) || action.Pos != (grammar.Pos{Line: 6, Col: 31}) || action.End != (grammar.Pos{Line: 9, Col: 2}) {
		t.Fatalf("group spans: %+v %+v", typ.SourceInfo, action.SourceInfo)
	}
	if len(g.AllComments()) != 4 {
		t.Fatalf("comments=%d", len(g.AllComments()))
	}
	data, err := grammar.MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	for _, decode := range []func([]byte) (*grammar.Grammar, error){grammar.UnmarshalJSON, grammar.DecodeJSON} {
		decoded, err := decode(data)
		if err != nil {
			t.Fatal(err)
		}
		if mustJSON(t, decoded) != mustJSON(t, g) {
			t.Fatal("JSON lost source group structure")
		}
		got := decoded.Rules()[0].Action.(*grammar.TermGroup).Term.(*grammar.TermGroup)
		if !reflect.DeepEqual(got.SourceInfo, innerAction.SourceInfo) {
			t.Fatal("JSON lost nested ownership")
		}
	}
	out := checkFormat(t, src)
	reparsed, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	gotType := reparsed.Types()[1].(*grammar.TypeAlias).Type.(*grammar.TypeGroup).Type.(*grammar.TypeGroup)
	gotAction := reparsed.Rules()[0].Action.(*grammar.TermGroup).Term.(*grammar.TermGroup)
	if gotType.LeadingComments != innerType.LeadingComments || !slices.Equal(gotType.TrailingComments, innerType.TrailingComments) || gotAction.LeadingComments != innerAction.LeadingComments || !slices.Equal(gotAction.TrailingComments, innerAction.TrailingComments) {
		t.Fatalf("owner paths changed:\n%s\n%+v %+v", out, gotType.SourceInfo, gotAction.SourceInfo)
	}
}

func TestGroupOpeningComments(t *testing.T) {
	for _, src := range []string{
		"type Alias ~ ( // opening\n string\n)",
		"type Alias ~ ( /* opening */ string)",
		"rule main = \"x\" => ( // opening\n 1\n)",
		"rule main = \"x\" => ( /* opening */ 1)",
		"type A = {\n X (\n // field type\n string\n )\n}",
		"rule main = \"x\" => ((\n // inner\n 1\n))",
	} {
		t.Run(src, func(t *testing.T) {
			out := checkFormat(t, src)
			if strings.Contains(out, "<*grammar.") {
				t.Fatal(out)
			}
		})
	}
}

func TestInlineGroupCommentOwnership(t *testing.T) {
	for _, src := range []string{"type T ~ (/*doc*/ A)", "rule main = \"x\" => ( /*doc*/ x)", "type T ~ (/*first*/ /*second*/ A)", "type T ~ (A /*tail*/)", "rule main = \"x\" => (x /*tail*/)", "type T ~ (/*first*/ A /*tail*/)"} {
		t.Run(src, func(t *testing.T) {
			g, err := Parse(src)
			if err != nil {
				t.Fatal(err)
			}
			owners := func(g *grammar.Grammar) []grammar.SourceInfo {
				if len(g.Types()) > 0 {
					group := g.Types()[0].(*grammar.TypeAlias).Type.(*grammar.TypeGroup)
					return []grammar.SourceInfo{group.SourceInfo, *group.Type.(grammar.SourceNode).Source()}
				}
				group := g.Rules()[0].Action.(*grammar.TermGroup)
				return []grammar.SourceInfo{group.SourceInfo, *group.Term.(grammar.SourceNode).Source()}
			}
			before := owners(g)
			out := checkFormat(t, src)
			again, err := Parse(out)
			if err != nil {
				t.Fatal(err)
			}
			after := owners(again)
			for i := range before {
				before[i].Pos, before[i].End, before[i].DiagnosticPos = grammar.Pos{}, grammar.Pos{}, grammar.Pos{}
				after[i].Pos, after[i].End, after[i].DiagnosticPos = grammar.Pos{}, grammar.Pos{}, grammar.Pos{}
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("ownership changed: %+v -> %+v\n%s", before, after, out)
			}
		})
	}
}

// A field type over several lines continues at the field's indentation.
func TestMultilineFieldTypeIndentation(t *testing.T) {
	src := "type T = {\n  F (\n    int\n    | string\n  )\n  G int\n}\n"
	want := "type T = {\n    F (\n        int | string\n    )\n    G int\n}\n"
	g, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if got := grammar.Format(g); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	g, err = Parse(want)
	if err != nil {
		t.Fatal(err)
	}
	if got := grammar.Format(g); got != want {
		t.Fatalf("not idempotent:\n%s", got)
	}
}

// Formatting types is idempotent when block comments span lines inside nested type groups and
// struct types, and when a comment breaks an unnamed struct type written on one line.
func TestTypeFormattingIdempotence(t *testing.T) {
	for _, src := range []string{
		"type T = {\n  F (\n    /* a\n  b */\n    int\n  )\n}\n",
		"type T = {\n  F {\n    /* a\nb */\n    A node[string]\n  }\n}\n",
		"type A ~ ((\n  /* a\n  b */\n  int\n))\n",
		"type A ~ (\n  // x /* y\n  (\n    int\n  )\n)\n",
		"type A ~ { A node[string] /* c */ }\n",
		"rule main { A node[string] /* c */ } = \"a\"\n",
		"type T = { F { A node[string] /* c */ } }\n",
		"rule main = x:\"a\" => []{ A node[string] /* c */ }{}\n",
		"type A ~ ({ A node[string] /* c */ })\n",
		"type A ~ ({ A node[string] /* c */ },)\n",
		"rule main = x:\"a\" => $x.({ A node[string] /* c */ })\n",
		"type A ~ node[{ A node[string] /* c */ }]\n",
		"type T = { F ({ A node[string] /* c */ }); G int }\n",
	} {
		g, err := Parse(src)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		once := grammar.Format(g)
		g, err = Parse(once)
		if err != nil {
			t.Fatalf("%q: formatted source does not parse: %v\n%s", src, err, once)
		}
		if twice := grammar.Format(g); twice != once || !strings.Contains(once, "/*") {
			t.Errorf("%q: not idempotent or comment lost:\n%s\nthen\n%s", src, once, twice)
		}
	}
}

// A group that holds only comments has no body line; formatting it must neither add a blank
// line nor change the result on later passes.
func TestCommentOnlyGroupFormatting(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"action tuple", "rule main = \"x\" => (\n\t// lead\n)\n", "rule main = \"x\" => (\n    // lead\n)\n"},
		{"action tuple block comment", "rule main = \"x\" => (\n\t/* c */\n)\n", "rule main = \"x\" => (\n    /* c */\n)\n"},
		{"action tuple comments", "rule main = \"x\" => (\n\t// a\n\t// b\n)\n", "rule main = \"x\" => (\n    // a\n    // b\n)\n"},
		{"action tuple blank between", "rule main = \"x\" => (\n\t// a\n\n\t// b\n)\n", "rule main = \"x\" => (\n    // a\n\n    // b\n)\n"},
		{"action tuple inline", "rule main = \"x\" => ( /* c */ )\n", "rule main = \"x\" => (/* c */)\n"},
		{"action tuple trailing", "rule main = \"x\" => ( // t\n)\n", "rule main = \"x\" => ( // t\n)\n"},
		{"nested action tuple", "rule main = \"x\" => (\n\t(\n\t\t// lead\n\t)\n)\n", "rule main = \"x\" => (\n    (\n        // lead\n    )\n)\n"},
		{"tuple type", "type T ~ (\n\t// lead\n)\n", "type T ~ (\n    // lead\n)\n"},
		{"nested tuple type", "type T ~ (int, (\n\t// lead\n))\n", "type T ~ (\n    int,\n    (\n        // lead\n    ),\n)\n"},
		{"tuple type inline", "type T ~ ( /* c */ )\n", "type T ~ (/* c */)\n"},
		{"tuple type trailing", "type T ~ ( // t\n)\n", "type T ~ ( // t\n)\n"},
		{"struct type", "type T ~ {\n\t// lead\n}\n", "type T ~ {\n    // lead\n}\n"},
		{"empty struct", "type T ~ {\n}\n", "type T ~ {}\n"},
		{"action block", "rule main = \"x\" => {\n\t// lead\n}\n", "rule main = \"x\" => {\n    // lead\n}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := checkFormat(t, tc.src)
			if out != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", out, tc.want)
			}
			if again := checkFormat(t, out); again != out {
				t.Errorf("second pass changed the result:\n%s", again)
			}
		})
	}
}
