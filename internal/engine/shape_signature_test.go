package engine

import (
	"testing"

	"github.com/ornew/pego/internal/compile"
)

func TestShapeSignatureShowsMovedDiscard(t *testing.T) {
	compile := func(src string) *Program {
		t.Helper()
		p, err := Compile(mustParse(src), compile.Options{})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	// The type of main is int in both, but the group builds a pair in one and a single value in the other.
	pair := compile("@start\nrule main = g:(\"a\" - &\"b\"* \"c\") => len($g.Value)\n")
	single := compile("@start\nrule main = g:(\"a\" - \"c\") => len($g.Value)\n")
	t1, _ := pair.RuleType("main")
	t2, _ := single.RuleType("main")
	if t1 != t2 {
		t.Fatalf("the test needs equal rule types, got %q and %q", t1, t2)
	}
	a, ok1 := pair.ShapeSignature("main", nil)
	b, ok2 := single.ShapeSignature("main", nil)
	if !ok1 || !ok2 || a == b {
		t.Errorf("signatures are equal or missing: %q %q", a, b)
	}
	with := compile("@start\nrule main = g:(\"a\" &\"b\"* \"c\") => len($g.Value)\n")
	without := compile("@start\nrule main = g:(\"a\" \"c\") => len($g.Value)\n")
	x, _ := with.ShapeSignature("main", nil)
	y, _ := without.ShapeSignature("main", nil)
	if x != y {
		t.Errorf("a lookahead changes the signature:\n%q\n%q", x, y)
	}
	if _, ok := pair.ShapeSignature("missing", nil); ok {
		t.Error("signature of a missing rule")
	}
}
