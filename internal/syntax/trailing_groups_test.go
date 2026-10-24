package syntax

import (
	"slices"
	"testing"

	"github.com/ornew/pego/grammar"
)

// Every comment on the line of a node's last token trails it when a line break
// follows, so a comment after the first one stays with the node.
func TestCommentsOnTheLineOfATokenAllTrailIt(t *testing.T) {
	g, err := Parse("type T = {\n    A int /* x */ /* y */\n    B string\n}\nrule r = \"a\" /* p */ /* q */ // r\nrule s = \"b\"\n")
	if err != nil {
		t.Fatal(err)
	}
	fields := g.File.Decls[0].(*grammar.TypeDef).Type.(*grammar.StructType).Fields
	if want := []string{" x ", " y "}; !slices.Equal(fields[0].TrailingComments, want) {
		t.Errorf("A trails %q, want %q", fields[0].TrailingComments, want)
	}
	if len(fields[1].TrailingComments) != 0 || fields[1].LeadingComments != "" {
		t.Errorf("B owns %+v", fields[1].SourceInfo)
	}
	if want := []string{" p ", " q ", " r\n"}; !slices.Equal(g.Rules()[0].TrailingComments, want) {
		t.Errorf("r trails %q, want %q", g.Rules()[0].TrailingComments, want)
	}
}

// Comments that a token follows on their line do not trail the token before them: the
// first trails it and the last leads the next token.
func TestCommentsBeforeATokenOnTheirLineDoNotJoinTheRun(t *testing.T) {
	g, err := Parse("rule r = \"a\" /* p */ /* q */ \"b\"\n")
	if err != nil {
		t.Fatal(err)
	}
	items := g.Rules()[0].Expr.(*grammar.Seq).Items
	if got := items[0].(grammar.SourceNode).Source().TrailingComments; !slices.Equal(got, []string{" p "}) {
		t.Errorf("a trails %q, want one comment", got)
	}
	if got := items[1].(grammar.SourceNode).Source().LeadingComments; got != " q " {
		t.Errorf("b leads %q, want q", got)
	}
}

// The comments of a line are written again as they were, as separate comments, by
// formatting source, and the formatting does not change their owners. Written from
// JSON they stay separate, with the spacing that normalized strings have.
func TestTrailingCommentsRoundTripExactly(t *testing.T) {
	for _, tc := range []struct{ name, src, want, fromJSON string }{
		{"blocks", "rule r = \"a\" /* a1 */ /* a2 */\nrule s = \"b\"\n", "rule r = \"a\" /* a1 */ /* a2 */\nrule s = \"b\"\n", "rule r = \"a\" /* a1 */ /* a2 */\n\nrule s = \"b\"\n"},
		{"blocks without space and a line comment", "rule r = \"a\" /*y1*//*y2*/ // y3\nrule s = \"b\"\n", "rule r = \"a\" /*y1*/ /*y2*/ // y3\nrule s = \"b\"\n", "rule r = \"a\" /*y1 */ /*y2 */ // y3\n\nrule s = \"b\"\n"},
		{"before a semicolon", "rule r = \"a\" /* a1 */ /* a2 */ ; // a3\nrule s = \"b\"\n", "rule r = \"a\" /* a1 */ /* a2 */ // a3\nrule s = \"b\"\n", "rule r = \"a\" /* a1 */ /* a2 */ // a3\n\nrule s = \"b\"\n"},
		{"field", "type T = {\n    A int /* a1 */ /* a2 */\n    B string\n}\n", "type T = {\n    A int /* a1 */ /* a2 */\n    B string\n}\n", "type T = {\n    A int /* a1 */ /* a2 */\n    B string\n}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, err := Parse(tc.src)
			if err != nil {
				t.Fatal(err)
			}
			formatted := grammar.Format(g)
			if formatted != tc.want {
				t.Fatalf("formatted:\n%s\nwant:\n%s", formatted, tc.want)
			}
			data, err := grammar.MarshalJSON(g)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := grammar.UnmarshalJSON(data)
			if err != nil {
				t.Fatal(err)
			}
			if owned := grammar.Format(decoded); owned != tc.fromJSON {
				t.Fatalf("formatted from JSON:\n%s\nwant:\n%s", owned, tc.fromJSON)
			}
			again, err := Parse(formatted)
			if err != nil {
				t.Fatal(err)
			}
			if a, b := jsonOf(g), jsonOf(again); a != b {
				t.Fatalf("owners changed:\n%s\n%s", a, b)
			}
		})
	}
}

// The comments of a line stay separate in JSON.
func TestTrailingCommentsAreAListInJSON(t *testing.T) {
	g, err := Parse("rule r = \"a\" /*y1*//*y2*/ // y3\n")
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
	if want := []string{"y1", "y2", " y3\n"}; !slices.Equal(decoded.Rules()[0].TrailingComments, want) {
		t.Fatalf("JSON trailing %q, want %q\n%s", decoded.Rules()[0].TrailingComments, want, data)
	}
}
