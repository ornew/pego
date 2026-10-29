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

	pegort "github.com/ornew/pego/runtime"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/value"
)

// Exercise the exported APIs against the same hand-built DAG, including nil,
// nested lists and a shared value reached through different owning parents.
func TestTreeRuntimeParity(t *testing.T) {
	leaf := value.Ptr(value.Data{Kind: value.MatchKind, Terminal: true, Text: "é<&>", Start: 1, End: 3})
	gLeaf := pegort.NewNode(pegort.KindString, 1, 3, value.D(leaf).Text, nil, "", nil, nil)
	child := value.Ptr(value.Data{Children: []*value.Node{leaf}, Fields: value.Fields{{Name: "list", Value: []any{leaf, nil, []byte{1, 2}}}}})
	gChild := pegort.NewNode(pegort.KindStruct, 0, 0, "", []*pegort.Node{gLeaf}, "", pegort.Fields{{Name: "list", Value: []any{gLeaf, nil, []byte{1, 2}}}}, nil)
	root := value.Ptr(value.Data{Children: []*value.Node{child, nil, leaf}, Fields: value.Fields{{Name: "z", Value: true}, {Name: "a", Value: leaf}}})
	gRoot := pegort.NewNode(pegort.KindStruct, 0, 0, "", []*pegort.Node{gChild, nil, gLeaf}, "", pegort.Fields{{Name: "z", Value: true}, {Name: "a", Value: gLeaf}}, nil)
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
	ca := func(v value.WalkVisit) (value.WalkAction, error) {
		n, p := "", ""
		if v.Node != nil {
			n = value.D(v.Node).Text
		}
		if v.Parent != nil {
			p = value.D(v.Parent).Text
		}
		a = append(a, describe(v.Depth, uint8(v.Edge.Kind), v.Edge.ChildIndex, v.Edge.FieldName, v.Edge.ListIndex, n, p))
		return value.WalkContinue, nil
	}
	cb := func(v pegort.WalkVisit) (pegort.WalkAction, error) {
		n, p := "", ""
		if v.Node != nil {
			n = v.Node.Text()
		}
		if v.Parent != nil {
			p = v.Parent.Text()
		}
		b = append(b, describe(v.Depth, uint8(v.Edge.Kind), v.Edge.ChildIndex, v.Edge.FieldName, v.Edge.ListIndex, n, p))
		return pegort.WalkContinue, nil
	}
	if err := value.Walk(root, ca); err != nil {
		t.Fatal(err)
	}
	if err := pegort.Walk(gRoot, cb); err != nil {
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
			if err := (pegort.Cursor{Root: gRoot, Unit: pegort.PositionUnit(unit)}).Range(span[0], span[1], cb); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(a, b) {
				t.Fatalf("cursor parity: %v != %v", a, b)
			}
		}
	}
	value.D(root).Children = []*value.Node{root}
	pegort.SetNodeElems(gRoot, []*pegort.Node{gRoot})
	var c *value.CycleError
	var gc *pegort.CycleError
	if !errors.As(value.Walk(root, ca), &c) || !errors.As(pegort.Walk(gRoot, cb), &gc) {
		t.Fatal("missing cycle errors")
	}
	if len(c.Path) != len(gc.Path) || uint8(c.Path[1].Kind) != uint8(gc.Path[1].Kind) {
		t.Fatal("cycle path parity")
	}
}

// Compile the emitted single-file parser and exercise the same depth, cycle,
// encoding and cursor regressions used by the core. This catches imports and
// typed-name collisions that direct runtime package tests cannot establish.
func TestGeneratedTreeTraversal(t *testing.T) { eachMode(t, testGeneratedTreeTraversal) }

func testGeneratedTreeTraversal(t *testing.T) {
	if testing.Short() {
		t.Skip("runs generated code")
	}
	var source strings.Builder
	names := []string{"Walk", "WalkAction", "WalkContinue", "WalkSkip", "WalkStop", "WalkVisit", "WalkEdge", "WalkEdgeKind", "WalkRoot", "WalkChild", "WalkField", "WalkListElement", "CycleError", "Cursor"}
	for _, name := range names {
		fmt.Fprintf(&source, "type %s = { Value int }\n", name)
	}
	source.WriteString("rule main = \"é\"\n")
	grammar, err := parseTest(source.String())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	generatePackage(t, dir, "tree", grammar, gen.GenOptions{Start: "main", Types: true})
	typesSource, err := os.ReadFile(filepath.Join(dir, "tree", "types", "types.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if !strings.Contains(string(typesSource), "type "+name+" struct") {
			t.Fatalf("grammar type %s is not declared under its name", name)
		}
	}
	write := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", []byte(testGoMod("gentest")))
	// The traversal itself is the runtime's and is tested there; this checks that the generated
	// package declares the colliding grammar types beside the runtime's names and walks a tree.
	var consumer strings.Builder
	consumer.WriteString("package tree\n\nimport (\n\t\"testing\"\n\n\t\"gentest/tree/types\"\n\tpegort \"github.com/ornew/pego/runtime\"\n)\n\nfunc TestWalk(t *testing.T) {\n")
	for _, name := range names {
		fmt.Fprintf(&consumer, "\t_ = types.%s{Value: 1}\n", name)
	}
	consumer.WriteString(`	root := &pegort.Node{RawChildren: []*pegort.Node{{RawText: "é"}}}
	visits := 0
	if err := pegort.Walk(root, func(v pegort.WalkVisit) (pegort.WalkAction, error) {
		visits++
		return pegort.WalkContinue, nil
	}); err != nil || visits != 2 {
		t.Fatalf("visits=%d err=%v", visits, err)
	}
}
`)
	write("tree/tree_test.go", []byte(consumer.String()))
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated tree checks: %v\n%s", err, out)
	}
}

// DuckDB supplies Inspect(any, func(any) bool) for its typed AST. Compile its actual
// helper beside a fresh generated parser and exercise both traversal surfaces.
func TestGeneratedDuckDBInspectCompatibility(t *testing.T) {
	eachMode(t, testGeneratedDuckDBInspectCompatibility)
}

func testGeneratedDuckDBInspectCompatibility(t *testing.T) {
	grammar, err := parseTest("type Leaf = { Text string }\nrule main Leaf = t:(\"é\") #atomic => Leaf{Text: text($t)}\n")
	if err != nil {
		t.Fatal(err)
	}
	helper, err := os.ReadFile(filepath.Join("..", "..", "parsers", "duckdb", "inspect.go"))
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
	write("go.mod", []byte(testGoMod("gentest")))
	generatePackage(t, dir, "duckdb", grammar, gen.GenOptions{Start: "main", Types: true})
	write("duckdb/inspect.go", helper)
	write("duckdb/walk_test.go", []byte(`package duckdb
import (
	"testing"
)
func TestBothWalkAPIs(t *testing.T) {
 node, err := Parse("é")
 if err != nil { t.Fatal(err) }
 generic := 0
 // A text struct node and its Text field value.
 if err := Walk(node,func(v WalkVisit)(WalkAction,error){generic++;return WalkContinue,nil});err!=nil||generic!=2 {t.Fatal(generic,err)}
 generic=0
 if err := (Cursor{Root:node,Unit:CodePoints}).At(0,func(v WalkVisit)(WalkAction,error){generic++;return WalkContinue,nil});err!=nil||generic!=1 {t.Fatal(generic,err)}
 typed, err := ParseAST("é")
 if err != nil { t.Fatal(err) }
 count:=0
 Inspect(typed,func(any)bool{count++;return true})
 if count!=1 {t.Fatalf("typed Inspect visits %d, want 1",count)}
 if span,ok:=SpanOf(typed);!ok||span.Start!=0||span.End!=1 {t.Fatal(span,ok)}
}
`))
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("DuckDB walk compatibility: %v\n%s", err, out)
	}
}
