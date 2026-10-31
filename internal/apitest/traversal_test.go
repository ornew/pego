package apitest_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/internal/value"
)

func TestTraversalPublicFacade(t *testing.T) {
	parser, err := compileTestSource(`rule main = "é"`, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, unit := range []zp.PositionUnit{zp.CodePoints, zp.Bytes} {
		node, err := parser.Parse("é", zp.WithUnit(unit))
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		callback := func(v zp.WalkVisit) (zp.WalkAction, error) {
			count++
			if v.Depth != 0 || v.Edge.Kind != zp.WalkRoot || v.Node != node || v.Parent != nil {
				t.Fatal(v)
			}
			return zp.WalkContinue, nil
		}
		if err := zp.Walk(node, callback); err != nil || count != 1 {
			t.Fatal(count, err)
		}
		count = 0
		if err := (zp.Cursor{Root: node, Unit: unit}).At(0, callback); err != nil || count != 1 {
			t.Fatal(count, err)
		}
		var output bytes.Buffer
		if err := node.WriteJSON(&output); err != nil || output.Len() == 0 {
			t.Fatal(err)
		}
		value.SetNodeElems(node, []*zp.Node{node})
		var cycle *zp.CycleError
		if err := zp.Walk(node, func(zp.WalkVisit) (zp.WalkAction, error) { return zp.WalkContinue, nil }); !errors.As(err, &cycle) {
			t.Fatalf("cycle = %v", err)
		}
	}
}
