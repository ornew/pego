//go:build !pego_snapshot_sharing_reference

package engine

import (
	"runtime"
	"testing"

	"github.com/ornew/pego/internal/value"
)

func TestSnapshotSharingCyclesAndTypedNil(t *testing.T) {
	prog := mustCompile(t, `rule main = "a"`)
	d, err := prog.NewDocument("main", "a")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := d.Parse()
	if err != nil {
		t.Fatal(err)
	}
	value.D(legacy).Children = []*value.Node{legacy, nil}
	value.D(legacy).Fields = value.Fields{{Name: "self", Value: legacy}, {Name: "typedNil", Value: (*value.Node)(nil)}, {Name: "int", Value: 7}, {Name: "text", Value: "a"}}
	first, err := d.SnapshotWith(ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := d.SnapshotWith(ParseOptions{})
	if err != nil {
		t.Fatal("shared cycle", err)
	}
	if second.Root().Child(0) != second.Root() {
		t.Fatal("cycle identity")
	}
	if v, ok := second.Root().Field("typedNil"); !ok || v != (SnapshotNode{}) {
		t.Fatal("typed nil")
	}
	materialized := second.Materialize()
	if value.D(materialized).Children[0] != materialized || value.D(materialized).Fields[0].Value != materialized {
		t.Fatal("materialized cycle")
	}
	value.D(legacy).Fields[3].Value = "changed"
	if first.Root().Fields()[3].Value != "a" || second.Root().Fields()[3].Value != "a" {
		t.Fatal("legacy mutation reached snapshots")
	}
	runtime.KeepAlive(first)
}
