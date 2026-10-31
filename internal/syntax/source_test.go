package syntax

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/grammar"
)

// Expected groups are shared with the executable boundary grammar's contract.
func TestLexerCommentBoundaries(t *testing.T) {
	cases := []struct {
		name, rule, input, trailing, leading string
		detached                             []string
	}{
		{"empty start", "start", "next", "", "", nil},
		{"file paragraphs", "start", "// orphan\n\n// foo\nnext", "", " foo\n", []string{" orphan\n"}},
		{"comment-only blank line", "start", "// first\n//\n// third\nnext", "", " first\n\n third\n", nil},
		{"inline line flushed", "after", " // trailing\n// next\nnext", " trailing\n", " next\n", nil},
		{"package to rule", "after", " // trailing\n// trailing\n\n// B2\n// B2\n\n// B1\n// B1\nnext", " trailing\n", " B1\n B1\n", []string{" trailing\n", " B2\n B2\n"}},
		{"baz to moo", "after", "\n// baz\n// another baz\n\n// moo\n//\n// another moo\nnext", " baz\n another baz\n", " moo\n\n another moo\n", nil},
		{"corge to grault", "after", "\n/* Block attached\n * to corge. */\n/* Block attached to\n * grault. */\nnext", " Block attached\n to corge. ", " Block attached to\n grault. ", nil},
		{"standalone leading", "after", "\n// next\nnext", "", " next\n", nil},
		{"standalone block leading", "after", "\n/* next */ next", "", " next ", nil},
		{"blank disables trailing", "after", "\n\n// detached\n\n// next\nnext", "", " next\n", []string{" detached\n"}},
		{"block switches to line", "after", "\n/* previous */\n// next\nnext", " previous ", " next\n", nil},
		{"line switches to block", "after", "\n// previous\n/* next */\nnext", " previous\n", " next ", nil},
		{"inline block", "after", " /* previous */\n// next\nnext", " previous ", " next\n", nil},
		{"ambiguous block", "after", " /* detached */ next", "", "", []string{" detached "}},
		{"ambiguous multiline block", "after", " /* first\n * second */ next", "", "", []string{" first\n second "}},
		{"empty ambiguous block", "after", " /**/ next", "", "", []string{""}},
		{"two inline blocks", "after", " /* previous */ /* next */ next", " previous ", " next ", nil},
		{"file-start inline block", "start", "/* next */ next", "", " next ", nil},
		{"empty group still detached", "start", "/**/\n\n// next\nnext", "", " next\n", []string{""}},
		{"block groups stay separate", "start", "/* first */\n/* second */\n// next\nnext", "", " next\n", []string{" first ", " second "}},
		{"nested block stays whole", "start", "/* outer /* nested */ tail */\nnext", "", " outer /* nested */ tail ", nil},
		{"nested block normalization", "start", "/* outer\n * /* nested\n * content */ tail\n * end */\nnext", "", " outer\n /* nested\n content */ tail\n end ", nil},
		{"nested closing on own line", "start", "/* outer\n * /* nested\n * content\n */ tail\n * end */\nnext", "", " outer\n /* nested\n content\n*/ tail\n end ", nil},
		{"EOF standalone trailing", "after", "\n// previous\n", " previous\n", "", nil},
		{"EOF inline continuation unowned", "after", " // previous\n// unowned\n", " previous\n", "", []string{" unowned\n"}},
		{"EOF detached unowned", "after", "\n\n// unowned\n", "", "", []string{" unowned\n"}},
		{"closing standalone trailing", "after", "\n// previous\n}", " previous\n", "", nil},
		{"closing inline continuation unowned", "after", " // previous\n// unowned\n}", " previous\n", "", []string{" unowned\n"}},
		{"closing inline block trails", "after", " /* previous */ }", " previous ", "", nil},
		{"closing inline block after a comma trails", "after", ", /* previous */ )", " previous ", "", nil},
		{"inline block after an opener leads", "opener", " /* next */ next", "", " next ", nil},
		{"inline block after a bracket leads", "bracket", " /* next */ next", "", " next ", nil},
		{"inline block after a brace leads", "brace", " /* next */ next", "", " next ", nil},
		{"inline block after a comma leads", "comma", " /* next */ next", "", " next ", nil},
		{"inline block after a semicolon stays ambiguous", "semicolon", " /* next */ next", "", "", []string{" next "}},
		{"opener block ending on the next line", "opener", " /* next\n */ next", "", " next\n", nil},
		{"two inline blocks after an opener", "opener", " /* previous */ /* next */ next", "", " next ", []string{" previous "}},
		{"closing inline block on prior line", "after", " /* previous */\n}", " previous ", "", nil},
		{"EOF inline block", "after", " /* previous */", " previous ", "", nil},
		{"CRLF", "start", "// first\r\n// second\r\nnext", "", " first\r\n second\r\n", nil},
		{"CR", "start", "// first\r// second\rnext", "", " first\r second\r", nil},
		{"no final newline", "after", " // previous", " previous", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := tc.input
			switch tc.rule {
			case "after":
				input = "prev" + input
			case "opener":
				input = "f(" + input
			case "bracket":
				input = "node[" + input
			case "brace":
				input = "S{" + input
			case "comma":
				input = "f(1," + input
			case "semicolon":
				input = "S{A int;" + input
			}
			errs := &errorList{}
			toks := lex(input, errs)
			if len(errs.list) != 0 {
				t.Fatal(errs.list)
			}
			var b *commentBoundary
			for _, tok := range toks {
				if tok.source != nil {
					b = tok.source
					break
				}
			}
			if b == nil {
				b = &commentBoundary{hasNext: true}
			}
			if strings.Join(b.trailing, "") != tc.trailing || b.leading != tc.leading || !reflect.DeepEqual(b.detached, tc.detached) {
				t.Fatalf("trailing=%q leading=%q detached=%q; want %q %q %q", strings.Join(b.trailing, ""), b.leading, b.detached, tc.trailing, tc.leading, tc.detached)
			}
		})
	}
}

func TestSourceCommentOwners(t *testing.T) {
	const input = "// orphan\n\n// foo\npackage foo // trailing\n// trailing\n\n// B2\n// B2\n\n// B1\n// B1\nrule B = \"b\" // trailing\n// trailing\n"
	g, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		source            *grammar.SourceInfo
		leading, trailing string
		detached          []string
	}{
		{&g.File.Pkg.SourceInfo, " foo\n", " trailing\n", []string{" orphan\n"}},
		{g.Rules()[0].Source(), " B1\n B1\n", " trailing\n", []string{" trailing\n", " B2\n B2\n"}},
	}
	for _, check := range checks {
		s := check.source
		if s.LeadingComments != check.leading || strings.Join(s.TrailingComments, "") != check.trailing || !reflect.DeepEqual(s.LeadingDetachedComments, check.detached) {
			t.Fatalf("source=%+v", s)
		}
	}
	child := g.Rules()[0].Expr.(grammar.SourceNode).Source()
	if child.LeadingComments != "" || len(child.TrailingComments) != 0 {
		t.Fatalf("comments duplicated on child: %+v", child)
	}
	if len(g.AllComments()) != 10 {
		t.Fatalf("raw comments lost: %d", len(g.AllComments()))
	}
	data, err := grammar.MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	for _, decode := range []func([]byte) (*grammar.Grammar, error){grammar.UnmarshalJSON, grammar.DecodeJSON} {
		copy, err := decode(data)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(g.File.Pkg.SourceInfo, copy.File.Pkg.SourceInfo) || !reflect.DeepEqual(g.Rules()[0].SourceInfo, copy.Rules()[0].SourceInfo) {
			t.Fatal("JSON lost owned comments")
		}
	}
}

func TestSourceDeclarationChildren(t *testing.T) {
	const input = `type A = {
 // field
 X string; // field tail
}


rule main = #operators {
 // skip
 skip " "*; // skip tail
 // operand
 operand "a"; // operand tail
 // level
 level add {
 // operator
 infix left "+"; // operator tail
 }; // level tail
}
`
	g, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	field := g.Types()[0].(*grammar.TypeDef).Struct().Fields[0].Source()
	op := g.Rules()[0].Expr.(*grammar.OperatorsRule)
	for _, check := range []struct {
		s    *grammar.SourceInfo
		name string
	}{
		{field, "field"}, {&op.SkipSource, "skip"}, {op.Operands[0].Source(), "operand"}, {op.Levels[0].Source(), "level"}, {op.Levels[0].Operators[0].Source(), "operator"},
	} {
		if check.s.LeadingComments != " "+check.name+"\n" || !slices.Equal(check.s.TrailingComments, []string{" " + check.name + " tail\n"}) {
			t.Errorf("%s: %+v", check.name, check.s)
		}
	}
}

func TestSourceSpansAndDiagnostics(t *testing.T) {
	cases := []struct {
		input string
		nodes func(*grammar.Grammar) []grammar.SourceNode
		spans [][2]grammar.Pos
	}{
		{"rule a = x+ #atomic @error(\"bad\")", func(g *grammar.Grammar) []grammar.SourceNode {
			rd := g.Rules()[0]
			a := rd.Expr.(*grammar.Attributed)
			atomic := a.Expr.(*grammar.Atomic)
			repeat := atomic.Expr.(*grammar.Repeat)
			return []grammar.SourceNode{rd, a, atomic, repeat, repeat.Expr.(*grammar.Ref), a.Attrs[0]}
		}, [][2]grammar.Pos{{{Line: 1, Col: 1}, {Line: 1, Col: 34}}, {{Line: 1, Col: 10}, {Line: 1, Col: 34}}, {{Line: 1, Col: 10}, {Line: 1, Col: 20}}, {{Line: 1, Col: 10}, {Line: 1, Col: 12}}, {{Line: 1, Col: 10}, {Line: 1, Col: 11}}, {{Line: 1, Col: 21}, {Line: 1, Col: 34}}}},
		{"rule a = (x => 1)", func(g *grammar.Grammar) []grammar.SourceNode {
			gr := g.Rules()[0].Expr.(*grammar.Group)
			return []grammar.SourceNode{gr, gr.Expr.(*grammar.InlineAction)}
		}, [][2]grammar.Pos{{{Line: 1, Col: 10}, {Line: 1, Col: 18}}, {{Line: 1, Col: 11}, {Line: 1, Col: 17}}}},
		{"rule a = {x := 1}", func(g *grammar.Grammar) []grammar.SourceNode {
			p := g.Rules()[0].Expr.(*grammar.Predicate)
			return []grammar.SourceNode{p, p.Term.(*grammar.Bind)}
		}, [][2]grammar.Pos{{{Line: 1, Col: 10}, {Line: 1, Col: 18}}, {{Line: 1, Col: 11}, {Line: 1, Col: 17}}}},
		{"rule a = \"あ\" => 1 + 2 * 3", func(g *grammar.Grammar) []grammar.SourceNode {
			b := g.Rules()[0].Action.(*grammar.Binary)
			return []grammar.SourceNode{b, b.R.(*grammar.Binary)}
		}, [][2]grammar.Pos{{{Line: 1, Col: 17}, {Line: 1, Col: 26}}, {{Line: 1, Col: 21}, {Line: 1, Col: 26}}}},
		{"rule a = \"x\" => $x.A.B", func(g *grammar.Grammar) []grammar.SourceNode {
			b := g.Rules()[0].Action.(*grammar.Member)
			return []grammar.SourceNode{b, b.X.(*grammar.Member)}
		}, [][2]grammar.Pos{{{Line: 1, Col: 17}, {Line: 1, Col: 23}}, {{Line: 1, Col: 17}, {Line: 1, Col: 21}}}},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			g, err := Parse(tc.input)
			if err != nil {
				t.Fatal(err)
			}
			for i, n := range tc.nodes(g) {
				s := n.Source()
				want := tc.spans[i]
				if s.Pos != want[0] || s.End != want[1] {
					t.Errorf("%T span=%v..%v want %v", n, s.Pos, s.End, want)
				}
			}
		})
	}
	g, err := Parse("rule a = \"x\" => 1 + 2")
	if err != nil {
		t.Fatal(err)
	}
	b := g.Rules()[0].Action.(*grammar.Binary)
	if b.Diagnostic() != (grammar.Pos{Line: 1, Col: 19}) {
		t.Fatalf("diagnostic=%v", b.Diagnostic())
	}
	g, err = Parse("// doc\r\nrule a = \"あ\" // tail\r\n")
	if err != nil {
		t.Fatal(err)
	}
	r := g.Rules()[0]
	if r.Pos != (grammar.Pos{Line: 2, Col: 1}) || r.End != (grammar.Pos{Line: 2, Col: 13}) || r.LeadingComments != " doc\r\n" || !slices.Equal(r.TrailingComments, []string{" tail\r\n"}) {
		t.Fatalf("CRLF/rune metadata=%+v", r.SourceInfo)
	}
}

func TestPartialSourceCommentOwners(t *testing.T) {
	const input = "// good\nrule good = \"g\"\n// bad\nrule bad = (\n// surviving\nrule tail = \"t\""
	g, errs := ParsePartial(input)
	if len(errs) == 0 || len(g.Rules()) != 2 {
		t.Fatalf("partial grammar: %v, %v", g, errs)
	}
	if g.Rules()[0].LeadingComments != " good\n" || g.Rules()[1].LeadingComments != " surviving\n" {
		t.Fatalf("partial ownership: %+v, %+v", g.Rules()[0].SourceInfo, g.Rules()[1].SourceInfo)
	}
}
