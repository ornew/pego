package engine

import (
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestQuerySchemaRejectsInvalidSchema(t *testing.T) {
	s := validInMemoryOutputSchema()
	s.Types[1].Shape.Ref = "declared/Missing"
	if q, err := PrepareQuerySchema(&s); q != nil || err == nil || !strings.Contains(err.Error(), "invalid query schema: unresolved") {
		t.Fatalf("PrepareQuerySchema = %v, %v", q, err)
	}
	if q, err := PrepareQuerySchema(nil); q != nil || err == nil {
		t.Fatalf("PrepareQuerySchema(nil) = %v, %v", q, err)
	}
}

// TestQuerySchemaOwnsCopy checks that changing a schema after preparing it,
// even into an invalid one, changes nothing for the prepared schema.
func TestQuerySchemaOwnsCopy(t *testing.T) {
	s := validInMemoryOutputSchema()
	s.Types[0].Shape = SchemaShape{Kind: "struct", Fields: []SchemaField{{Name: "Value", Shape: SchemaShape{Kind: "optional", Element: &SchemaShape{Kind: "scalar", Scalar: "string"}}}}}
	q, err := PrepareQuerySchema(&s)
	if err != nil {
		t.Fatal(err)
	}
	want := copyOutputSchema(&s)
	if !reflect.DeepEqual(want, q.s) {
		t.Fatalf("prepared schema differs from its source")
	}
	value := QueryPattern{Type: "Item", Fields: []QueryFieldPattern{{Name: "Value", Pattern: QueryPattern{Category: "string"}}}}

	s.Types[0].Name = "Renamed"
	s.Types[0].Shape.Fields[0].Name = "Other"
	s.Types[0].Shape.Fields[0].Shape.Element.Scalar = "bool"
	s.Types[1].Shape.Ref = "declared/Missing"
	s.Rules[0].Name = "renamed"
	if !reflect.DeepEqual(want, q.s) {
		t.Fatalf("changing the source changed the prepared schema")
	}
	if _, err := q.CompileStructuralQuery(value); err != nil {
		t.Fatalf("prepared schema followed the changed source: %v", err)
	}
	if _, err := q.CompileStructuralQuery(QueryPattern{Rule: s.Rules[0].Name}); err == nil {
		t.Fatal("prepared schema knows the renamed rule")
	}
}

// TestQuerySchemaCopySharesDAG checks that the copy of an inline DAG keeps its
// sharing: copying every path would take 2^28 shapes here.
func TestQuerySchemaCopySharesDAG(t *testing.T) {
	s := validInMemoryOutputSchema()
	shared := &SchemaShape{Kind: "scalar", Scalar: "string"}
	for range 28 {
		shared = &SchemaShape{Kind: "union", Alternatives: []SchemaShape{
			{Kind: "optional", Element: shared},
			{Kind: "optional", Element: shared},
		}}
	}
	s.Types[1].Shape = *shared
	// A slice shared by two shapes is shared in the copy as well.
	alts := []SchemaShape{{Kind: "nil"}, {Kind: "scalar", Scalar: "int"}}
	s.Types = append(s.Types, SchemaType{ID: "declared/Other", Name: "Other", Origin: "declared", Shape: SchemaShape{Kind: "struct", Fields: []SchemaField{
		{Name: "A", Shape: SchemaShape{Kind: "list", Element: &SchemaShape{Kind: "union", Alternatives: alts}}},
		{Name: "B", Shape: SchemaShape{Kind: "union", Alternatives: alts}},
	}}})

	q, err := PrepareQuerySchema(&s)
	if err != nil {
		t.Fatal(err)
	}
	c := &q.s.Types[1].Shape
	if c.Alternatives[0].Element != c.Alternatives[1].Element || c.Alternatives[0].Element == s.Types[1].Shape.Alternatives[0].Element {
		t.Fatal("the copy does not share the DAG's shapes")
	}
	fields := q.s.Types[2].Shape.Fields
	got := fields[0].Shape.Element.Alternatives
	if &got[0] != &fields[1].Shape.Alternatives[0] || &got[0] == &alts[0] {
		t.Fatal("the copy does not share the slice")
	}
	if _, err := q.CompileStructuralQuery(QueryPattern{Type: "Item", Fields: []QueryFieldPattern{{Name: "Absent"}}}); err == nil || !strings.Contains(err.Error(), "has no field") {
		t.Fatalf("shared DAG missing-field error = %v", err)
	}
}

func TestQuerySchemaNil(t *testing.T) {
	var q *QuerySchema
	sq, err := q.CompileStructuralQuery(QueryPattern{Type: "Anything", Fields: []QueryFieldPattern{{Name: "X"}}})
	if err != nil || sq == nil {
		t.Fatalf("nil QuerySchema = %v, %v", sq, err)
	}
	if _, err := q.CompileStructuralQuery(QueryPattern{Category: "unknown"}); err == nil {
		t.Fatal("nil QuerySchema accepted an unknown category")
	}
}

// TestQuerySchemaConcurrentCompile compiles queries from several goroutines;
// run it with -race.
func TestQuerySchemaConcurrentCompile(t *testing.T) {
	q, err := PrepareQuerySchema(queryShapeSchema(t))
	if err != nil {
		t.Fatal(err)
	}
	patterns := []QueryPattern{
		{Type: "Pair", Fields: []QueryFieldPattern{field("Value", stringNode)}},
		{Type: "Box", Fields: []QueryFieldPattern{field("Inner", QueryPattern{Fields: []QueryFieldPattern{field("Value", QueryPattern{Type: "Pair", Fields: []QueryFieldPattern{field("Value", stringNode)}})}})}},
		{Type: "Pair", Fields: []QueryFieldPattern{field("Value", QueryPattern{Type: "Box"})}},
	}
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 50 {
				p := patterns[(g+i)%len(patterns)]
				_, err := q.CompileStructuralQuery(p)
				if (err == nil) != (p.Fields[0].Pattern.Type != "Box") {
					t.Errorf("pattern %d: %v", (g+i)%len(patterns), err)
					return
				}
			}
		})
	}
	wg.Wait()
}

// TestQuerySchemaIndexesDeclaredNames checks that query types name declared
// struct types only: a rule's inferred type of the same name is not a type.
func TestQuerySchemaIndexesDeclaredNames(t *testing.T) {
	s := &OutputSchema{Types: []SchemaType{
		{ID: "declared/A", Name: "A", Origin: "declared"},
		{ID: "inferred/Error", Name: "Error", Origin: "inferred"},
		{ID: "declared/Error", Name: "Error", Origin: "declared"},
		{ID: "inferred/B", Name: "B", Origin: "inferred"},
	}}
	q := indexQuerySchema(s)
	if q.byName["A"] != &s.Types[0] || q.byName["Error"] != &s.Types[2] || q.byName["B"] != nil || len(q.byName) != 2 {
		t.Fatalf("byName = %v", q.byName)
	}
	for id, i := range map[string]int{"declared/A": 0, "inferred/Error": 1, "declared/Error": 2, "inferred/B": 3} {
		if q.byID[id] != &s.Types[i] {
			t.Fatalf("byID[%s] = %v", id, q.byID[id])
		}
	}
}
