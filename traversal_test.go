package pego_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/ornew/pego"
)

func TestTraversalPublicFacade(t *testing.T) {
	parser, err := pego.CompileSource(`rule main = "é"`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, unit := range []pego.Unit{pego.CodePoints, pego.Bytes} {
		node, err := parser.Parse("é", pego.WithUnit(unit))
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		callback := func(v pego.WalkVisit) (pego.WalkAction, error) {
			count++
			if v.Depth != 0 || v.Edge.Kind != pego.WalkRoot || v.Node != node || v.Parent != nil {
				t.Fatal(v)
			}
			return pego.WalkContinue, nil
		}
		if err := pego.Walk(node, callback); err != nil || count != 1 {
			t.Fatal(count, err)
		}
		count = 0
		if err := (pego.Cursor{Root: node, Unit: unit}).At(0, callback); err != nil || count != 1 {
			t.Fatal(count, err)
		}
		var output bytes.Buffer
		if err := node.WriteJSON(&output); err != nil || output.Len() == 0 {
			t.Fatal(err)
		}
		node.Children = []*pego.Node{node}
		var cycle *pego.CycleError
		if err := pego.Walk(node, func(pego.WalkVisit) (pego.WalkAction, error) { return pego.WalkContinue, nil }); !errors.As(err, &cycle) {
			t.Fatalf("cycle = %v", err)
		}
	}
}
