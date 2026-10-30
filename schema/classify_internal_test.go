package schema

import "testing"

func newTestCanonicalizer() *schemaCanonicalizer {
	return &schemaCanonicalizer{copies: make(map[*Shape]canonicalSchemaCopy), comparisons: make(map[schemaShapePair]int)}
}

// An origin flip with a changed shape is breaking; the origin impact stays listed as compatible.
// The inputs of Compare cannot reach this case (a type's ID carries its origin and a rule's result
// is a reference to its own type), so the function is called directly.
func TestChangeImpactsOriginFlipWithShapeChange(t *testing.T) {
	c := newTestCanonicalizer()
	old := &Shape{Kind: "scalar", Scalar: "int"}
	new := &Shape{Kind: "scalar", Scalar: "string"}
	got := changeImpacts(c, true, old, new)
	want := []Impact{
		{Path: "$", Class: Compatible, Rule: RuleOriginChanged},
		{Path: "$", Class: Breaking, Rule: RuleScalarChanged},
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] || classOf(got) != Breaking {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	// Without the shape change the flip alone is compatible.
	if got := changeImpacts(c, true, old, old); len(got) != 1 || got[0] != want[0] || classOf(got) != Compatible {
		t.Fatalf("origin only: %+v", got)
	}
}

// Negative control for the safety net: shapes that differ in a member the walk does not read for
// their kind (a struct with an element) still produce a breaking impact.
func TestChangeImpactsUnexplainedDifferenceIsBreaking(t *testing.T) {
	c := newTestCanonicalizer()
	elem := &Shape{Kind: "scalar", Scalar: "int"}
	old := &Shape{Kind: "struct"}
	new := &Shape{Kind: "struct", Element: elem}
	if got := c.shapeImpacts(old, new, "$", nil); len(got) != 0 {
		t.Fatalf("the walk was expected to miss this difference: %+v", got)
	}
	got := changeImpacts(c, false, old, new)
	if len(got) != 1 || got[0] != (Impact{Path: "$", Class: Breaking, Rule: RuleKindChanged}) {
		t.Fatalf("got %+v", got)
	}
}
