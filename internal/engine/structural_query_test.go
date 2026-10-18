package engine

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

func TestStructuralQuerySharedEdgesAndCaptures(t *testing.T) {
	leaf := &Node{kind: kindOf(TypeMatch, "identifier"), terminal: true, Text: "hello"}
	tuple := &Node{kind: kindTupleValue, Children: []*Node{leaf}}
	list := &Node{kind: kindListValue, Children: []*Node{nil, leaf}}
	root := &Node{kind: kindOf("Pair", ""), Fields: Fields{{"z", list}, {"a", leaf}, {"t", tuple}}}
	q, err := CompileStructuralQuery(QueryPattern{Rule: "identifier", Capture: "name", Value: &QueryPattern{Category: "string", Capture: "text"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := q.Matches(root, QueryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("matches %d", len(got))
	}
	want := [][]QueryEdge{{{Kind: "field", Name: "a"}}, {{Kind: "field", Name: "t"}, {Kind: "element", Index: 0}}, {{Kind: "field", Name: "z"}, {Kind: "element", Index: 1}}}
	parents := []*Node{root, tuple, list}
	for i, m := range got {
		if len(m.Captures) != 2 || m.Captures[0].Value != leaf || m.Captures[0].Category != "node" || m.Captures[0].Parent != parents[i] || !reflect.DeepEqual(m.Captures[0].Path, want[i]) {
			t.Fatalf("capture %d: %+v", i, m)
		}
		if m.Captures[1].Value != "hello" || m.Captures[1].Category != "string" || m.Captures[1].Parent != leaf ||
			!reflect.DeepEqual(m.Captures[1].Path, append(append([]QueryEdge(nil), want[i]...), QueryEdge{Kind: "value"})) {
			t.Fatalf("scalar capture %+v", m)
		}
	}
}

func TestStructuralQueryElementsSeeThroughNodes(t *testing.T) {
	a := &Node{kind: kindMatch, terminal: true, Text: "a"}
	b := &Node{kind: kindBox, box: 7}
	look := &Node{kind: kindMatch, terminal: true, Text: "!"}
	tuple := &Node{kind: kindSeq, Children: []*Node{a, b}, Fields: Fields{{"x", a}, {"y", look}}}
	elements := QueryPattern{Category: "node", Elements: []QueryPattern{{Value: &QueryPattern{Category: "string"}, Capture: "first"}, {Category: "int", Capture: "second"}}}
	value := QueryPattern{Value: &QueryPattern{Category: "tuple", Elements: []QueryPattern{{}, {Category: "int"}}, Capture: "value"}}
	for name, tc := range map[string]struct {
		pattern QueryPattern
		paths   [][]QueryEdge
	}{
		"elements": {elements, [][]QueryEdge{{{Kind: "element", Index: 0}}, {{Kind: "element", Index: 1}}}},
		"value":    {value, [][]QueryEdge{{{Kind: "value"}}}},
	} {
		q, err := CompileStructuralQuery(tc.pattern, nil)
		if err != nil {
			t.Fatal(err)
		}
		got, err := q.Matches(tuple, QueryOptions{})
		if err != nil || len(got) != 1 || len(got[0].Captures) != len(tc.paths) {
			t.Fatalf("%s: %+v %v", name, got, err)
		}
		for i, c := range got[0].Captures {
			if !reflect.DeepEqual(c.Path, tc.paths[i]) || c.Parent != tuple {
				t.Fatalf("%s capture %d: %+v", name, i, c)
			}
		}
		if name == "elements" && (got[0].Captures[0].Value != a || got[0].Captures[1].Value != 7) {
			t.Fatalf("element values: %+v", got[0].Captures)
		}
		if v, ok := got[0].Captures[0].Value.(*Node); name == "value" && (!ok || v.shape() != shapeTupleValue || len(v.Children) != 2) {
			t.Fatalf("node value: %+v", got[0].Captures[0])
		}
	}
	// The walk visits the elements and the capture that is not an element, not the capture of a.
	q, err := CompileStructuralQuery(QueryPattern{Category: "node", Capture: "n"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := q.Matches(tuple, QueryOptions{})
	if err != nil || len(got) != 3 {
		t.Fatalf("walk: %+v %v", got, err)
	}
	want := [][]QueryEdge{{}, {{Kind: "element", Index: 0}}, {{Kind: "capture", Name: "y"}}}
	for i, m := range got {
		if len(m.Captures[0].Path) != len(want[i]) || len(want[i]) > 0 && !reflect.DeepEqual(m.Captures[0].Path, want[i]) {
			t.Fatalf("walk path %d: %+v", i, m.Captures[0].Path)
		}
	}
}

func TestStructuralQueryRuleNeedsSourceInfo(t *testing.T) {
	g, err := syntax.Parse(`rule main = item ("," item)*
rule item = [a-z]`)
	if err != nil {
		t.Fatal(err)
	}
	pattern := QueryPattern{Rule: "item", Capture: "item"}
	for _, sourceInfo := range []bool{false, true} {
		p, err := Compile(g, Options{SourceInfo: sourceInfo})
		if err != nil {
			t.Fatal(err)
		}
		s, err := p.OutputSchemaPrototype("main", false)
		if err != nil {
			t.Fatal(err)
		}
		if s.SourceInfo != sourceInfo {
			t.Fatalf("schema sourceInfo %v, want %v", s.SourceInfo, sourceInfo)
		}
		_, err = CompileStructuralQuery(pattern, s)
		if sourceInfo != (err == nil) || !sourceInfo && !strings.Contains(err.Error(), "source information") {
			t.Fatalf("sourceInfo %v: compile error %v", sourceInfo, err)
		}
		q, err := CompileStructuralQuery(pattern, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			root, err := p.ParseWith("main", "a,b,c", ParseOptions{Backend: backend})
			if err != nil {
				t.Fatal(err)
			}
			got, err := q.Matches(root, QueryOptions{})
			if sourceInfo && (err != nil || len(got) != 3) || !sourceInfo && (got != nil || !errors.Is(err, ErrQueryNoSourceInfo)) {
				t.Fatalf("%s sourceInfo %v: %+v %v", backend, sourceInfo, got, err)
			}
		}
	}
	// Values without nodes cannot record rules, and a rule pattern simply does not match them.
	q, err := CompileStructuralQuery(QueryPattern{Fields: []QueryFieldPattern{{"X", QueryPattern{Rule: "item"}}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := q.Matches(&Node{kind: kindOf("S", ""), Fields: Fields{{"X", 1}}}, QueryOptions{}); err != nil || len(got) != 0 {
		t.Fatalf("struct without nodes: %+v %v", got, err)
	}
}

func TestStructuralQueryConstraintConflicts(t *testing.T) {
	for _, p := range []QueryPattern{
		{Category: "struct", Rule: "r"},
		{Type: "T", Rule: "r"},
		{Type: "T", Elements: []QueryPattern{{}}},
		{Type: "error", Category: "struct"},
		{Value: &QueryPattern{}, Fields: []QueryFieldPattern{{"x", QueryPattern{}}}},
		{Category: "tuple", Value: &QueryPattern{}},
		{Category: "int", Elements: []QueryPattern{{}}},
		{Type: "error", Fields: []QueryFieldPattern{{"other", QueryPattern{}}}},
		{Category: "number"},
	} {
		if _, err := CompileStructuralQuery(p, nil); err == nil {
			t.Errorf("accepted %+v", p)
		}
	}
	for _, p := range []QueryPattern{
		{Category: "node", Rule: "r", Value: &QueryPattern{}, Elements: []QueryPattern{{}}},
		{Type: "error", Fields: []QueryFieldPattern{{"expected", QueryPattern{}}, {"message", QueryPattern{}}}},
		{Category: "list", Elements: []QueryPattern{{}}},
		{Category: "tuple", Elements: []QueryPattern{{}}},
		{Category: "struct", Type: "T", Fields: []QueryFieldPattern{{"x", QueryPattern{}}}},
	} {
		if _, err := CompileStructuralQuery(p, nil); err != nil {
			t.Errorf("rejected %+v: %v", p, err)
		}
	}
}

func TestStructuralQueryAllOrError(t *testing.T) {
	q, err := CompileStructuralQuery(QueryPattern{Capture: "all"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	root := &Node{kind: kindSeq}
	root.Children = []*Node{root}
	got, err := q.Matches(root, QueryOptions{})
	if got != nil || !errors.Is(err, ErrQueryCycle) {
		t.Fatalf("cycle: %v %v", got, err)
	}
	list := &Node{kind: kindListValue}
	list.Children = []*Node{list}
	root.Children = nil
	root.Fields = Fields{{"cycle", list}}
	root.kind = kindOf("S", "")
	got, err = q.Matches(root, QueryOptions{})
	if got != nil || !errors.Is(err, ErrQueryCycle) {
		t.Fatalf("list cycle: %v %v", got, err)
	}
	root = &Node{kind: kindMatch, terminal: true}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err = q.Matches(root, QueryOptions{Context: ctx})
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v %v", got, err)
	}
	// A leaf costs one visit, one pattern, one capture and one match.
	got, err = q.Matches(root, QueryOptions{WorkLimits: WorkLimits{MaxSteps: 4}})
	if err != nil || len(got) != 1 {
		t.Fatalf("exact limit: %v %v", got, err)
	}
	got, err = q.Matches(root, QueryOptions{WorkLimits: WorkLimits{MaxSteps: 3}})
	if got != nil || !errors.Is(err, ErrWorkLimit) {
		t.Fatalf("limit: %v %v", got, err)
	}
}

func TestStructuralQueryCycleThroughValues(t *testing.T) {
	q, err := CompileStructuralQuery(QueryPattern{Capture: "all"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	root := &Node{kind: kindSeq}
	list := &Node{kind: kindListValue, Children: []*Node{root}}
	root.Children = []*Node{list}
	// The repeated node is rejected before its contents are reserved.
	budget := &queryBudget{limits: WorkLimits{MaxSteps: 100}}
	visits := 0
	err = visitQueryGraph(root, budget, func(queryVisit) error {
		visits++
		return nil
	})
	if !errors.Is(err, ErrQueryCycle) || visits != 2 {
		t.Fatalf("first back edge: visits=%d steps=%d err=%v", visits, budget.steps, err)
	}
	results, err := q.Matches(root, QueryOptions{WorkLimits: WorkLimits{MaxSteps: 100}})
	if results != nil || !errors.Is(err, ErrQueryCycle) {
		t.Fatalf("value cycle: %+v, %v", results, err)
	}
	// Go slices are not values of the value model.
	results, err = q.Matches(&Node{kind: kindOf("S", ""), Fields: Fields{{"x", []any{1}}}}, QueryOptions{})
	if results != nil || err == nil || !strings.Contains(err.Error(), "unsupported query value") {
		t.Fatalf("Go slice: %+v, %v", results, err)
	}
}

func TestStructuralQueryValidationAndRuntimeMismatch(t *testing.T) {
	schema := validInMemoryOutputSchema()
	schema.Types[0].Shape.Fields = []SchemaField{{Name: "Value", Shape: SchemaShape{Kind: "scalar", Scalar: "string"}}}
	s := &schema
	if _, err := CompileStructuralQuery(QueryPattern{Type: "Item", Fields: []QueryFieldPattern{{"Absent", QueryPattern{}}}}, s); err == nil {
		t.Fatal("accepted impossible field")
	}
	if _, err := CompileStructuralQuery(QueryPattern{Rule: "main"}, s); err == nil || !strings.Contains(err.Error(), "source information") {
		t.Fatalf("accepted a rule without source information: %v", err)
	}
	schema.SourceInfo = true
	if _, err := CompileStructuralQuery(QueryPattern{Rule: "missing"}, s); err == nil || !strings.Contains(err.Error(), "unknown query rule") {
		t.Fatalf("accepted impossible rule: %v", err)
	}
	if _, err := CompileStructuralQuery(QueryPattern{Type: "Item", Fields: []QueryFieldPattern{{"Value", QueryPattern{Category: "bool"}}}}, s); err == nil {
		t.Fatal("accepted incompatible scalar category")
	}

	p := QueryPattern{Type: "Item", Fields: []QueryFieldPattern{{"Value", QueryPattern{Category: "string", Capture: "value"}}}}
	q, err := CompileStructuralQuery(p, s)
	if err != nil {
		t.Fatal(err)
	}
	p.Fields[0].Pattern.Category = "int"
	got, err := q.Matches(&Node{kind: kindOf("Item", ""), Fields: Fields{{"Value", false}}}, QueryOptions{})
	if err != nil || len(got) != 0 {
		t.Fatalf("runtime mismatch: %v %v", got, err)
	}
	got, err = q.Matches(&Node{kind: kindOf("Item", ""), Fields: Fields{{"Value", "ok"}}}, QueryOptions{})
	if err != nil || len(got) != 1 {
		t.Fatalf("pattern ownership: %v %v", got, err)
	}
}

func TestStructuralQueryRejectsMalformedInMemorySchema(t *testing.T) {
	s := validInMemoryOutputSchema()
	s.Types[1].Shape.Ref = "declared/Missing"
	if _, err := CompileStructuralQuery(QueryPattern{Type: "Item"}, &s); err == nil || !strings.Contains(err.Error(), "unresolved") {
		t.Fatalf("invalid schema error = %v", err)
	}
}

func TestStructuralQuerySharedInlineDAGAbsentField(t *testing.T) {
	s := validInMemoryOutputSchema()
	shared := &SchemaShape{Kind: "scalar", Scalar: "string"}
	for range 28 {
		shared = &SchemaShape{Kind: "union", Alternatives: []SchemaShape{
			{Kind: "optional", Element: shared},
			{Kind: "optional", Element: shared},
		}}
	}
	s.Types[0].Shape = SchemaShape{Kind: "struct", Fields: []SchemaField{{Name: "Deep", Shape: *shared}}}
	if _, err := CompileStructuralQuery(QueryPattern{Type: "Item", Fields: []QueryFieldPattern{{Name: "Absent"}}}, &s); err == nil || !strings.Contains(err.Error(), "has no field") {
		t.Fatalf("shared DAG missing-field error = %v", err)
	}
	deep := QueryPattern{Type: "Item", Fields: []QueryFieldPattern{{Name: "Deep", Pattern: QueryPattern{Fields: []QueryFieldPattern{{Name: "Absent"}}}}}}
	if _, err := CompileStructuralQuery(deep, &s); err == nil || !strings.Contains(err.Error(), "has no fields") {
		t.Fatalf("shared DAG field error = %v", err)
	}
}

func TestStructuralQueryNamedReferenceCycleAbsentField(t *testing.T) {
	s := validInMemoryOutputSchema()
	s.Types[0].Shape = SchemaShape{Kind: "struct", Fields: []SchemaField{{Name: "F", Shape: SchemaShape{Kind: "ref", Ref: "inferred/a"}}}}
	s.Types = append(s.Types,
		SchemaType{ID: "inferred/a", Name: "a", Origin: "inferred", Shape: SchemaShape{Kind: "ref", Ref: "inferred/b"}},
		SchemaType{ID: "inferred/b", Name: "b", Origin: "inferred", Shape: SchemaShape{Kind: "ref", Ref: "inferred/a"}},
		SchemaType{ID: "declared/Other", Name: "Other", Origin: "declared", Shape: SchemaShape{Kind: "struct", Fields: []SchemaField{
			{Name: "Present", Shape: SchemaShape{Kind: "scalar", Scalar: "string"}},
		}}})
	// Rules that only call each other have no values.
	inF := func(p QueryPattern) QueryPattern {
		return QueryPattern{Type: "Item", Fields: []QueryFieldPattern{{Name: "F", Pattern: p}}}
	}
	if _, err := CompileStructuralQuery(inF(QueryPattern{Fields: []QueryFieldPattern{{Name: "Absent"}}}), &s); err == nil || !strings.Contains(err.Error(), "no values") {
		t.Fatalf("named cycle error = %v", err)
	}
	s.Types[len(s.Types)-2].Shape = SchemaShape{Kind: "union", Alternatives: []SchemaShape{
		{Kind: "ref", Ref: "inferred/a"},
		{Kind: "ref", Ref: "declared/Other"},
	}}
	if _, err := CompileStructuralQuery(inF(QueryPattern{Type: "Other", Fields: []QueryFieldPattern{{Name: "Present"}}}), &s); err != nil {
		t.Fatalf("named cycle hid possible field: %v", err)
	}
	if _, err := CompileStructuralQuery(inF(QueryPattern{Fields: []QueryFieldPattern{{Name: "Absent"}}}), &s); err == nil || !strings.Contains(err.Error(), "has no field Absent") {
		t.Fatalf("named cycle missing-field error = %v", err)
	}
}

func TestStructuralQueryOptionalScalarFieldCategory(t *testing.T) {
	scalar := SchemaShape{Kind: "scalar", Scalar: "string"}
	value := validInMemoryOutputSchema()
	value.Types[0].Shape.Fields = []SchemaField{{Name: "Value", Shape: SchemaShape{Kind: "optional", Element: &scalar}}}
	schema := &value
	pattern := func(category string) QueryPattern {
		return QueryPattern{Type: "Item", Fields: []QueryFieldPattern{{
			Name: "Value", Pattern: QueryPattern{Category: category, Capture: "value"},
		}}}
	}
	for _, category := range []string{"bool", "node", "list"} {
		if _, err := CompileStructuralQuery(pattern(category), schema); err == nil {
			t.Errorf("accepted impossible optional-scalar category %q", category)
		}
	}
	for _, category := range []string{"string", "nil"} {
		q, err := CompileStructuralQuery(pattern(category), schema)
		if err != nil {
			t.Fatalf("rejected possible category %q: %v", category, err)
		}
		value := any("ok")
		if category == "nil" {
			value = nil
		}
		matches, err := q.Matches(&Node{kind: kindOf("Item", ""), Fields: Fields{{"Value", value}}}, QueryOptions{})
		if err != nil || len(matches) != 1 || len(matches[0].Captures) != 1 || matches[0].Captures[0].Category != category {
			t.Fatalf("category %q at runtime: %+v, %v", category, matches, err)
		}
	}
}
