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
