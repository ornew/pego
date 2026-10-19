package sourceast

import (
	"reflect"
	"testing"

	"github.com/ornew/pego/grammar"
)

func TestPositionsAndOwnership(t *testing.T) {
	input := "😀\r\nβ\rγ\npackage p"
	p := NewPositions(input)
	m := Metadata{Start: 7, End: 16, Leading: " leading\r\n", Trailing: " trailing", Detached: []string{"", " detached\n"}}
	s, err := p.Source(m)
	if err != nil {
		t.Fatal(err)
	}
	if s.Pos != (grammar.Pos{Line: 4, Col: 1}) || s.End != (grammar.Pos{Line: 4, Col: 10}) {
		t.Fatalf("positions: %+v", s)
	}
	if s.LeadingComments != m.Leading || s.TrailingComments != m.Trailing || !reflect.DeepEqual(s.LeadingDetachedComments, m.Detached) {
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
	if g.Package != "p" || g.PackageSource.Pos != (grammar.Pos{Line: 1, Col: 1}) || g.PackageSource.End != (grammar.Pos{Line: 1, Col: 10}) {
		t.Fatalf("package: %+v", g)
	}
	if err := p.PackageClause(g, "other", Metadata{End: 100}); err == nil || g.Package != "p" {
		t.Fatal("failed conversion mutated package")
	}
}

func TestSetSpanPreservesOwnership(t *testing.T) {
	p := NewPositions("// 😀\r\ntype T = { Text string } // trailing")
	dst := grammar.SourceInfo{
		LeadingComments: " leading\r\n", TrailingComments: " trailing",
		LeadingDetachedComments: []string{"", " detached\n"},
		DiagnosticPos:           grammar.Pos{Line: 2, Col: 6},
	}
	if err := p.SetSpan(&dst, 6, 21); err != nil {
		t.Fatal(err)
	}
	if dst.Pos != (grammar.Pos{Line: 2, Col: 1}) || dst.End != (grammar.Pos{Line: 2, Col: 16}) {
		t.Fatalf("span: %+v", dst)
	}
	if dst.LeadingComments != " leading\r\n" || dst.TrailingComments != " trailing" || !reflect.DeepEqual(dst.LeadingDetachedComments, []string{"", " detached\n"}) || dst.DiagnosticPos != (grammar.Pos{Line: 2, Col: 6}) {
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
