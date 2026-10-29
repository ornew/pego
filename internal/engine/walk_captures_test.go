package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/value"
)

// TestWalkHiddenCaptures checks that Walk visits only the public fields of a node: without
// source information the captures of an operator node are not output, and following them, as
// well as the operands they share with the elements, made a chain of prefix operators take
// time exponential in its length.
func TestWalkHiddenCaptures(t *testing.T) {
	prog := compileBare(t, `rule main = x
rule x = #operators {
    operand "x"
    level { prefix "-" }
}`, compile.Options{})
	n, err := prog.Parse("main", strings.Repeat("-", 40)+"x")
	if err != nil {
		t.Fatal(err)
	}
	visits := 0
	if err := value.Walk(n, func(v value.WalkVisit) (value.WalkAction, error) {
		visits++
		if v.Edge.Kind == value.WalkField {
			t.Fatalf("visited the hidden capture %s", v.Edge.FieldName)
		}
		return value.WalkContinue, nil
	}); err != nil {
		t.Fatal(err)
	}
	if visits != 2*40+1 {
		t.Fatalf("%d visits, want %d", visits, 2*40+1)
	}
}

// TestWalkElementCaptures checks that, with source information, Walk visits the captures of a
// node that are not its elements, and visits those that are only as elements: an operator chain
// takes visits linear in its length.
func TestWalkElementCaptures(t *testing.T) {
	prog := compileBare(t, `rule main = x
rule x = #operators {
    operand "x"
    level { prefix "-" }
}`, compile.Options{SourceInfo: true})
	n, err := prog.Parse("main", strings.Repeat("-", 40)+"x")
	if err != nil {
		t.Fatal(err)
	}
	visits := 0
	if err := value.Walk(n, func(v value.WalkVisit) (value.WalkAction, error) {
		visits++
		if v.Edge.Kind == value.WalkField {
			t.Fatalf("visited the element capture %s", v.Edge.FieldName)
		}
		return value.WalkContinue, nil
	}); err != nil {
		t.Fatal(err)
	}
	if visits != 2*40+1 {
		t.Fatalf("%d visits, want %d", visits, 2*40+1)
	}

	prog = compileBare(t, `rule main = &s:word w:word "!"
rule word = [a-z]+`, compile.Options{SourceInfo: true})
	n, err = prog.Parse("main", "ab!")
	if err != nil {
		t.Fatal(err)
	}
	var fields []string
	if err := value.Walk(n, func(v value.WalkVisit) (value.WalkAction, error) {
		if v.Edge.Kind == value.WalkField {
			fields = append(fields, v.Edge.FieldName)
		}
		return value.WalkContinue, nil
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(fields, " ") != "s" {
		t.Fatalf("visited the captures %q, want the capture s beside the elements", fields)
	}
}
