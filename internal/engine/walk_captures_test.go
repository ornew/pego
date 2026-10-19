package engine

import (
	"strings"
	"testing"
)

// TestWalkHiddenCaptures checks that Walk visits only the public fields of a node: without
// source information the captures of an operator node are not output, and following them, as
// well as the operands they share with the elements, made a chain of prefix operators take
// time exponential in its length.
func TestWalkHiddenCaptures(t *testing.T) {
	prog := compileBare(t, `rule main = x
rule x = operators {
    operand "x"
    level { prefix "-" }
}`, Options{})
	n, err := prog.Parse("main", strings.Repeat("-", 40)+"x")
	if err != nil {
		t.Fatal(err)
	}
	visits := 0
	if err := Walk(n, func(v WalkVisit) (WalkAction, error) {
		visits++
		if v.Edge.Kind == WalkField {
			t.Fatalf("visited the hidden capture %s", v.Edge.FieldName)
		}
		return WalkContinue, nil
	}); err != nil {
		t.Fatal(err)
	}
	if visits != 2*40+1 {
		t.Fatalf("%d visits, want %d", visits, 2*40+1)
	}
}
