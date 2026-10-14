package engine

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestStructuralQuerySharedEdgesAndCaptures(t *testing.T) {
	leaf := &Node{kind: kindOf("Identifier", "identifier"), Fields: Fields{{"Text", "hello"}}}
	root := &Node{kind: kindOf("Pair", ""), Children: []*Node{leaf}, Fields: Fields{{"z", []any{leaf}}, {"a", leaf}}}
	q, err := CompileStructuralQuery(QueryPattern{Type: "Identifier", Rule: "identifier", Capture: "name", Fields: []QueryFieldPattern{{"Text", QueryPattern{Category: "string", Capture: "text"}}}}, nil)
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
	want := [][]QueryEdge{{{Kind: "child", Index: 0}}, {{Kind: "field", Name: "a"}}, {{Kind: "field", Name: "z"}, {Kind: "list", Index: 0}}}
	for i, m := range got {
		if len(m.Captures) != 2 || m.Captures[0].Value != leaf || m.Captures[0].Parent != root || !reflect.DeepEqual(m.Captures[0].Path, want[i]) {
			t.Fatalf("capture %d: %+v", i, m)
		}
		if m.Captures[1].Value != "hello" || m.Captures[1].Category != "string" || m.Captures[1].Parent != leaf {
			t.Fatalf("scalar capture %+v", m)
		}
	}
}
func TestStructuralQueryAllOrError(t *testing.T) {
	q, err := CompileStructuralQuery(QueryPattern{Capture: "all"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	root := &Node{kind: kindMatch}
	root.Children = []*Node{root}
	got, err := q.Matches(root, QueryOptions{})
	if got != nil || !errors.Is(err, ErrQueryCycle) {
		t.Fatalf("cycle: %v %v", got, err)
	}
	list := make([]any, 1)
	list[0] = list
	root.Children = nil
	root.Fields = Fields{{"cycle", list}}
	got, err = q.Matches(root, QueryOptions{})
	if got != nil || !errors.Is(err, ErrQueryCycle) {
		t.Fatalf("list cycle: %v %v", got, err)
	}
	root.Fields = nil
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

func TestStructuralQueryListCycleAcrossCapacityViews(t *testing.T) {
	q, err := CompileStructuralQuery(QueryPattern{Capture: "all"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	outer := make([]any, 1, 2)
	outer[0] = outer[:1:1] // Same first element and length, different capacity.
	root := &Node{kind: kindMatch, Fields: Fields{{"cycle", outer}}}
	// The first repeated active view is the inner slice. The traversal must
	// reject it before visiting that edge or reserving work for its contents.
	budget := &queryBudget{limits: WorkLimits{MaxSteps: 11}}
	visits := 0
	err = visitQueryGraph(root, budget, func(queryVisit) error {
		visits++
		return nil
	})
	if !errors.Is(err, ErrQueryCycle) || visits != 2 || budget.steps != 11 {
		t.Fatalf("first back edge: visits=%d steps=%d err=%v", visits, budget.steps, err)
	}
	results, err := q.Matches(root, QueryOptions{WorkLimits: WorkLimits{MaxSteps: 100}})
	if results != nil || !errors.Is(err, ErrQueryCycle) {
		t.Fatalf("capacity-view cycle: %+v, %v", results, err)
	}
}
func TestStructuralQueryValidationAndRuntimeMismatch(t *testing.T) {
	s := &OutputSchema{Format: "pego-output-schema", Version: 1, Types: []SchemaType{{ID: "declared/Item", Name: "Item", Origin: "declared", Shape: SchemaShape{Kind: "struct", Fields: []SchemaField{{Name: "Value", Shape: SchemaShape{Kind: "scalar", Scalar: "string"}}}}}}, Rules: []SchemaRule{{Name: "main"}}}
	if _, err := CompileStructuralQuery(QueryPattern{Type: "Item", Fields: []QueryFieldPattern{{"Absent", QueryPattern{}}}}, s); err == nil {
		t.Fatal("accepted impossible field")
	}
	if _, err := CompileStructuralQuery(QueryPattern{Rule: "missing"}, s); err == nil {
		t.Fatal("accepted impossible rule")
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

func TestStructuralQueryOptionalScalarFieldCategory(t *testing.T) {
	scalar := SchemaShape{Kind: "scalar", Scalar: "string"}
	schema := &OutputSchema{
		Format: "pego-output-schema", Version: 1,
		Types: []SchemaType{{
			ID: "declared/Item", Name: "Item", Origin: "declared",
			Shape: SchemaShape{Kind: "struct", Fields: []SchemaField{{
				Name: "Value", Shape: SchemaShape{Kind: "optional", Element: &scalar},
			}}},
		}},
	}
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
