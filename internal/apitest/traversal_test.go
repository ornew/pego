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
	for _, unit := range []zeroparse.PositionUnit{zeroparse.CodePoints, zeroparse.Bytes} {
		node, err := parser.Parse("é", zeroparse.WithUnit(unit))
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		callback := func(v zeroparse.WalkVisit) (zeroparse.WalkAction, error) {
			count++
			if v.Depth != 0 || v.Edge.Kind != zeroparse.WalkRoot || v.Node != node || v.Parent != nil {
				t.Fatal(v)
			}
			return zeroparse.WalkContinue, nil
		}
		if err := zeroparse.Walk(node, callback); err != nil || count != 1 {
			t.Fatal(count, err)
		}
		count = 0
		if err := (zeroparse.Cursor{Root: node, Unit: unit}).At(0, callback); err != nil || count != 1 {
			t.Fatal(count, err)
		}
		var output bytes.Buffer
		if err := node.WriteJSON(&output); err != nil || output.Len() == 0 {
			t.Fatal(err)
		}
		value.SetNodeElems(node, []*zeroparse.Node{node})
		var cycle *zeroparse.CycleError
		if err := zeroparse.Walk(node, func(zeroparse.WalkVisit) (zeroparse.WalkAction, error) { return zeroparse.WalkContinue, nil }); !errors.As(err, &cycle) {
			t.Fatalf("cycle = %v", err)
		}
	}
}
