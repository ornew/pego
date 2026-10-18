package engine

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine/genrt"
	"github.com/ornew/pego/internal/syntax"
)

// Exercise the exported APIs against the same hand-built DAG, including nil,
// nested lists and a shared value reached through different owning parents.
func TestTreeRuntimeParity(t *testing.T) {
	leaf := &Node{Text: "é<&>", Start: 1, End: 3}
	gLeaf := &genrt.Node{Text: leaf.Text, Start: 1, End: 3}
	child := &Node{Children: []*Node{leaf}, Fields: Fields{{"list", []any{leaf, nil, []byte{1, 2}}}}}
	gChild := &genrt.Node{Children: []*genrt.Node{gLeaf}, Fields: genrt.Fields{{Name: "list", Value: []any{gLeaf, nil, []byte{1, 2}}}}}
	root := &Node{Children: []*Node{child, nil, leaf}, Fields: Fields{{"z", true}, {"a", leaf}}}
	gRoot := &genrt.Node{Children: []*genrt.Node{gChild, nil, gLeaf}, Fields: genrt.Fields{{Name: "z", Value: true}, {Name: "a", Value: gLeaf}}}
	core, err := root.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	generated, err := gRoot.MarshalJSON()
	if err != nil || !bytes.Equal(core, generated) {
		t.Fatalf("JSON parity: %s != %s, %v", core, generated, err)
	}
	var a, b []string
	describe := func(depth int, kind uint8, child int, field string, list int, nodeText, parentText string) string {
		return fmt.Sprintf("%d/%d/%d/%s/%d/%s/%s", depth, kind, child, field, list, nodeText, parentText)
	}
	ca := func(v WalkVisit) (WalkAction, error) {
		n, p := "", ""
		if v.Node != nil {
			n = v.Node.Text
		}
		if v.Parent != nil {
			p = v.Parent.Text
		}
		a = append(a, describe(v.Depth, uint8(v.Edge.Kind), v.Edge.ChildIndex, v.Edge.FieldName, v.Edge.ListIndex, n, p))
		return WalkContinue, nil
	}
	cb := func(v genrt.WalkVisit) (genrt.WalkAction, error) {
		n, p := "", ""
		if v.Node != nil {
			n = v.Node.Text
		}
		if v.Parent != nil {
			p = v.Parent.Text
		}
		b = append(b, describe(v.Depth, uint8(v.Edge.Kind), v.Edge.ChildIndex, v.Edge.FieldName, v.Edge.ListIndex, n, p))
		return genrt.WalkContinue, nil
	}
	if err := Walk(root, ca); err != nil {
		t.Fatal(err)
	}
	if err := genrt.Walk(gRoot, cb); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("walk parity: %v != %v", a, b)
	}
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		for _, span := range [][2]int32{{0, 0}, {1, 2}, {3, 4}} {
			a, b = nil, nil
			if err := (Cursor{Root: root, Unit: unit}).Range(span[0], span[1], ca); err != nil {
				t.Fatal(err)
			}
			if err := (genrt.Cursor{Root: gRoot, Unit: genrt.PositionUnit(unit)}).Range(span[0], span[1], cb); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(a, b) {
				t.Fatalf("cursor parity: %v != %v", a, b)
			}
		}
	}
	root.Children = []*Node{root}
	gRoot.Children = []*genrt.Node{gRoot}
	var c *CycleError
	var gc *genrt.CycleError
	if !errors.As(Walk(root, ca), &c) || !errors.As(genrt.Walk(gRoot, cb), &gc) {
		t.Fatal("missing cycle errors")
	}
	if len(c.Path) != len(gc.Path) || uint8(c.Path[1].Kind) != uint8(gc.Path[1].Kind) {
		t.Fatal("cycle path parity")
	}
}

// Compile the emitted single-file parser and exercise the same depth, cycle,
// encoding and cursor regressions used by the core. This catches imports and
// typed-name collisions that direct genrt-package tests cannot establish.
func TestGeneratedTreeTraversal(t *testing.T) {
	if testing.Short() {
		t.Skip("runs generated code")
	}
	var source strings.Builder
	names := []string{"WalkTree", "WalkAction", "WalkContinue", "WalkSkip", "WalkStop", "WalkVisit", "WalkEdge", "WalkEdgeKind", "WalkRoot", "WalkChild", "WalkField", "WalkListElement", "CycleError", "Cursor"}
	for _, name := range names {
		fmt.Fprintf(&source, "type %s = { Value int }\n", name)
	}
	source.WriteString("rule main = \"é\"\n")
	grammar, err := syntax.Parse(source.String())
	if err != nil {
		t.Fatal(err)
	}
	code, err := Generate(grammar, GenOptions{Package: "tree", Start: "main", Types: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if !strings.Contains(string(code), "type "+name+"_ struct") {
			t.Fatalf("grammar name %s not escaped", name)
		}
	}
	dir := t.TempDir()
	write := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", []byte("module tree\n\ngo 1.27.1\n"))
	write("parser.go", code)
	for _, name := range []string{"node_json_test.go", "node_walk_test.go"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		data = bytes.Replace(data, []byte("package engine"), []byte("package tree"), 1)
		write(name, bytes.ReplaceAll(data, []byte("Walk("), []byte("WalkTree(")))
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated tree checks: %v\n%s", err, out)
	}
}

// DuckDB supplies Walk(any, func(any) bool) for its typed AST. Compile its actual
// helper beside a fresh generated parser and exercise both traversal surfaces.
func TestGeneratedDuckDBWalkCompatibility(t *testing.T) {
	grammar, err := syntax.Parse("type Leaf terminal\nrule main Leaf = \"é\"\n")
	if err != nil {
		t.Fatal(err)
	}
	code, err := Generate(grammar, GenOptions{Package: "duckdb", Start: "main", Types: true})
	if err != nil {
		t.Fatal(err)
	}
	helper, err := os.ReadFile(filepath.Join("..", "..", "parsers", "duckdb", "walk.go"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", []byte("module duckdbwalk\n\ngo 1.27.1\n"))
	write("parser.go", code)
	write("walk.go", helper)
	write("walk_test.go", []byte(`package duckdb
import "testing"
func TestBothWalkAPIs(t *testing.T) {
 node, err := Parse("é")
 if err != nil { t.Fatal(err) }
 generic := 0
 if err := WalkTree(node,func(v WalkVisit)(WalkAction,error){generic++;return WalkContinue,nil});err!=nil||generic!=1 {t.Fatal(generic,err)}
 generic=0
 if err := (Cursor{Root:node,Unit:CodePoints}).At(0,func(v WalkVisit)(WalkAction,error){generic++;return WalkContinue,nil});err!=nil||generic!=1 {t.Fatal(generic,err)}
 typed, err := ParseAST("é")
 if err != nil { t.Fatal(err) }
 count:=0
 Walk(typed,func(any)bool{count++;return true})
 if count!=1 {t.Fatalf("typed Walk visits %d, want 1",count)}
 if span,ok:=SpanOf(typed);!ok||span.Start!=0||span.End!=1 {t.Fatal(span,ok)}
}
`))
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("DuckDB walk compatibility: %v\n%s", err, out)
	}
}
