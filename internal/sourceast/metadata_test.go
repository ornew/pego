package sourceast

import (
	"reflect"
	"slices"
	"testing"

	"github.com/ornew/pego/grammar"
)

func TestPositionsAndOwnership(t *testing.T) {
	input := "😀\r\nβ\rγ\npackage p"
	p := NewPositions(input)
	m := Metadata{Start: 7, End: 16, Leading: " leading\r\n", Trailing: []string{" trailing"}, Detached: []string{"", " detached\n"}}
	s, err := p.Source(m)
	if err != nil {
		t.Fatal(err)
	}
	if s.Pos != (grammar.Pos{Line: 4, Col: 1}) || s.End != (grammar.Pos{Line: 4, Col: 10}) {
		t.Fatalf("positions: %+v", s)
	}
	if s.LeadingComments != m.Leading || !slices.Equal(s.TrailingComments, m.Trailing) || !reflect.DeepEqual(s.LeadingDetachedComments, m.Detached) {
		t.Fatalf("ownership changed: %+v", s)
	}
	s.LeadingDetachedComments[1] = "changed"
	if m.Detached[1] != " detached\n" {
		t.Fatal("adapter aliases source comment slice")
	}
	for _, bad := range []Metadata{{Start: -1}, {Start: 2, End: 1}, {End: 17}} {
		if _, err := p.Source(bad); err == nil {
			t.Fatalf("accepted invalid span %+v", bad)
		}
	}
}

func TestPackageClause(t *testing.T) {
	g := &grammar.Grammar{}
	p := NewPositions("package p")
	if err := p.PackageClause(g, "p", Metadata{End: 9}); err != nil {
		t.Fatal(err)
	}
	if g.File.Pkg.Name != "p" || g.File.Pkg.SourceInfo.Pos != (grammar.Pos{Line: 1, Col: 1}) || g.File.Pkg.SourceInfo.End != (grammar.Pos{Line: 1, Col: 10}) {
		t.Fatalf("package: %+v", g)
	}
	if err := p.PackageClause(g, "other", Metadata{End: 100}); err == nil || g.File.Pkg.Name != "p" {
		t.Fatal("failed conversion mutated package")
	}
}

func TestSetSpanPreservesOwnership(t *testing.T) {
	p := NewPositions("// 😀\r\ntype T = { Text string } // trailing")
	dst := grammar.SourceInfo{
		LeadingComments: " leading\r\n", TrailingComments: []string{" trailing"},
		LeadingDetachedComments: []string{"", " detached\n"},
		DiagnosticPos:           grammar.Pos{Line: 2, Col: 6},
	}
	if err := p.SetSpan(&dst, 6, 21); err != nil {
		t.Fatal(err)
	}
	if dst.Pos != (grammar.Pos{Line: 2, Col: 1}) || dst.End != (grammar.Pos{Line: 2, Col: 16}) {
		t.Fatalf("span: %+v", dst)
	}
	if dst.LeadingComments != " leading\r\n" || !slices.Equal(dst.TrailingComments, []string{" trailing"}) || !reflect.DeepEqual(dst.LeadingDetachedComments, []string{"", " detached\n"}) || dst.DiagnosticPos != (grammar.Pos{Line: 2, Col: 6}) {
		t.Fatalf("span update replaced ownership or diagnostic position: %+v", dst)
	}
	before := dst
	if err := p.SetSpan(&dst, 0, 100); err == nil || !reflect.DeepEqual(dst, before) {
		t.Fatal("failed span conversion mutated metadata")
	}
	if err := p.SetSpan(nil, 0, 1); err == nil {
		t.Fatal("accepted nil destination")
	}
}

func TestItems(t *testing.T) {
	p := NewPositions("a b c d")
	head := Lead{Leading: " head\n", Detached: []string{" d0\n"}, HasNext: true}
	items := []*Item{
		{Start: 0, End: 1, Trailing: []string{" t0\n"}, Next: Lead{Leading: " l1\n", HasNext: true}},
		{Start: 2, End: 3, Next: Lead{Leading: " unowned\n"}},
		{Start: 4, End: 5, Next: Lead{Leading: " l3\n", HasNext: true}},
		nil,
		{Start: 6, End: 7},
	}
	got, err := p.Items(head, items)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].LeadingComments != " head\n" || !reflect.DeepEqual(got[0].LeadingDetachedComments, []string{" d0\n"}) || !slices.Equal(got[0].TrailingComments, []string{" t0\n"}) {
		t.Fatalf("first: %+v", got[0])
	}
	if got[1].LeadingComments != " l1\n" || got[1].Pos.Col != 3 {
		t.Fatalf("second: %+v", got[1])
	}
	if got[2].LeadingComments != "" {
		t.Fatalf("a boundary without a next owner gives its groups away: %+v", got[2])
	}
	if got[3].Pos.IsValid() {
		t.Fatalf("a nil element owns nothing: %+v", got[3])
	}
	if got[4].LeadingComments != "" {
		t.Fatalf("a nil element leaves its successor without groups: %+v", got[4])
	}
	if _, err := p.Items(Lead{}, []*Item{{Start: 0, End: 99}}); err == nil {
		t.Fatal("accepted invalid span")
	}
}
