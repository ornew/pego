package pego_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ornew/pego"
)

func diffSchema(types []pego.SchemaType, rules []pego.SchemaRule) *pego.OutputSchema {
	types = append([]pego.SchemaType{}, types...)
	rules = append([]pego.SchemaRule{}, rules...)
	if len(rules) == 0 {
		rules = append(rules, pego.SchemaRule{Name: "main", Origin: "inferred", Result: pego.SchemaShape{Kind: "nil"}})
	}
	start := "main"
	for i := range rules {
		rule := &rules[i]
		if rule.Name == "" {
			continue
		}
		if i == 0 {
			start = rule.Name
		}
		id := "inferred/" + rule.Name
		if rule.Result.Kind == "ref" && rule.Result.Ref == id {
			continue
		}
		found := false
		for _, entry := range types {
			found = found || entry.ID == id
		}
		if !found {
			types = append(types, pego.SchemaType{ID: id, Name: rule.Name, Origin: "inferred", Shape: rule.Result})
		}
		rule.Result = pego.SchemaShape{Kind: "ref", Ref: id}
	}
	return &pego.OutputSchema{
		Format: "pego-output-schema", Version: 2, Start: start,
		PositionUnitIndependent: true, Types: types, Rules: rules,
	}
}

func TestDiffOutputSchemasIgnoresUnorderedEntries(t *testing.T) {
	stringShape := pego.SchemaShape{Kind: "scalar", Scalar: "string"}
	boolShape := pego.SchemaShape{Kind: "scalar", Scalar: "bool"}
	item := pego.SchemaType{ID: "declared/Item", Name: "Item", Origin: "declared", Shape: pego.SchemaShape{
		Kind: "struct", Fields: []pego.SchemaField{
			{Name: "Z", Shape: stringShape},
			{Name: "A", Shape: pego.SchemaShape{Kind: "join", Alternatives: []pego.SchemaShape{stringShape, boolShape}}},
		},
	}}
	item.Shape.Fields = append(item.Shape.Fields, pego.SchemaField{Name: "N", Shape: pego.SchemaShape{
		Kind: "node", Value: &pego.SchemaShape{Kind: "tuple", Elements: []pego.SchemaShape{stringShape, stringShape}},
		Captures: []pego.SchemaCapture{
			{Name: "y", Paths: [][]int{{1}, {0}}, Shape: stringShape},
			{Name: "x", Side: true, Shape: boolShape},
		},
	}})
	other := pego.SchemaType{ID: "inferred/main", Name: "main", Origin: "inferred", Shape: pego.SchemaShape{Kind: "ref", Ref: item.ID}}
	rule := pego.SchemaRule{Name: "main", Origin: "inferred", Result: pego.SchemaShape{Kind: "ref", Ref: other.ID}}
	left := diffSchema([]pego.SchemaType{item, other}, []pego.SchemaRule{rule, {Name: "other", Origin: "inferred", Result: stringShape}})
	itemReordered := item
	node := item.Shape.Fields[2].Shape
	node.Captures = []pego.SchemaCapture{node.Captures[1], node.Captures[0]}
	node.Captures[1].Paths = [][]int{{0}, {1}}
	itemReordered.Shape.Fields = []pego.SchemaField{item.Shape.Fields[1], {Name: "N", Shape: node}, item.Shape.Fields[0]}
	itemReordered.Shape.Fields[0].Shape.Alternatives = []pego.SchemaShape{boolShape, stringShape}
	right := diffSchema([]pego.SchemaType{left.Types[2], other, itemReordered}, []pego.SchemaRule{left.Rules[1], rule})
	right.Start = "other" // Envelope metadata is outside the entry diff.
	diff, err := pego.DiffOutputSchemas(left, right)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Types) != 0 || len(diff.Rules) != 0 {
		t.Fatalf("reordering changed semantic contents: %+v", diff)
	}
	// Tuple elements are ordered.
	node.Value = &pego.SchemaShape{Kind: "tuple", Elements: []pego.SchemaShape{stringShape, boolShape}}
	itemReordered.Shape.Fields[1].Shape = node
	right = diffSchema([]pego.SchemaType{left.Types[2], other, itemReordered}, []pego.SchemaRule{left.Rules[1], rule})
	if diff, err = pego.DiffOutputSchemas(left, right); err != nil || len(diff.Types) != 1 || diff.Types[0].ID != "declared/Item" {
		t.Fatalf("element change: %+v %v", diff, err)
	}
}

func TestDiffOutputSchemasReportsSortedEntryChanges(t *testing.T) {
	ref := pego.SchemaShape{Kind: "ref", Ref: "declared/Recursive"}
	empty := pego.SchemaShape{Kind: "struct"}
	next := func(ref *pego.SchemaShape) pego.SchemaShape {
		return pego.SchemaShape{Kind: "struct", Fields: []pego.SchemaField{{Name: "Next", Shape: pego.SchemaShape{Kind: "optional", Element: ref}}}}
	}
	old := diffSchema([]pego.SchemaType{
		{ID: "declared/Z", Name: "Z", Origin: "declared", Shape: empty},
		{ID: "declared/Recursive", Name: "Recursive", Origin: "declared", Shape: next(&ref)},
	}, []pego.SchemaRule{
		{Name: "z", Origin: "inferred", Result: ref},
		{Name: "m", Origin: "inferred", Result: pego.SchemaShape{Kind: "nil"}},
	})
	newRef := pego.SchemaShape{Kind: "ref", Ref: "declared/Changed"}
	newer := diffSchema([]pego.SchemaType{
		{ID: "declared/Recursive", Name: "Recursive", Origin: "declared", Shape: next(&newRef)},
		{ID: "declared/A", Name: "A", Origin: "declared", Shape: empty},
		{ID: "declared/Changed", Name: "Changed", Origin: "declared", Shape: empty},
	}, []pego.SchemaRule{
		{Name: "a", Origin: "inferred", Result: ref},
		{Name: "m", Origin: "declared", Result: pego.SchemaShape{Kind: "nil"}},
	})
	diff, err := pego.DiffOutputSchemas(old, newer)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Types) != 6 || diff.Types[0].ID != "declared/A" || diff.Types[0].Kind != pego.SchemaAdded || diff.Types[0].Before != nil || diff.Types[0].After == nil ||
		diff.Types[1].ID != "declared/Changed" || diff.Types[1].Kind != pego.SchemaAdded ||
		diff.Types[2].ID != "declared/Recursive" || diff.Types[2].Kind != pego.SchemaChanged || diff.Types[2].Before == nil || diff.Types[2].After == nil ||
		diff.Types[3].ID != "declared/Z" || diff.Types[3].Kind != pego.SchemaRemoved || diff.Types[3].Before == nil || diff.Types[3].After != nil ||
		diff.Types[4].ID != "inferred/a" || diff.Types[4].Kind != pego.SchemaAdded ||
		diff.Types[5].ID != "inferred/z" || diff.Types[5].Kind != pego.SchemaRemoved {
		t.Fatalf("type changes: %+v", diff.Types)
	}
	if diff.Types[2].Before.Shape.Fields[0].Shape.Element.Ref != "declared/Recursive" || diff.Types[2].After.Shape.Fields[0].Shape.Element.Ref != "declared/Changed" {
		t.Fatalf("named recursion was expanded or lost: %+v", diff.Types[2])
	}
	if len(diff.Rules) != 3 || diff.Rules[0].ID != "a" || diff.Rules[0].Kind != pego.SchemaAdded ||
		diff.Rules[1].ID != "m" || diff.Rules[1].Kind != pego.SchemaChanged ||
		diff.Rules[2].ID != "z" || diff.Rules[2].Kind != pego.SchemaRemoved {
		t.Fatalf("rule changes: %+v", diff.Rules)
	}
	first, err := json.Marshal(diff)
	if err != nil {
		t.Fatal(err)
	}
	newer.Types[0], newer.Types[1] = newer.Types[1], newer.Types[0]
	newer.Rules[0], newer.Rules[1] = newer.Rules[1], newer.Rules[0]
	again, err := pego.DiffOutputSchemas(old, newer)
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(again)
	if err != nil || string(first) != string(second) {
		t.Fatalf("unstable diff: %s / %s, %v", first, second, err)
	}
}

func TestDiffOutputSchemasRejectsMalformedIdentities(t *testing.T) {
	valid := diffSchema(nil, nil)
	cases := []struct {
		name string
		bad  *pego.OutputSchema
		want string
	}{
		{"nil", nil, "nil output schema"},
		{"version", &pego.OutputSchema{Format: "pego-output-schema", Version: 3}, "unsupported output schema"},
		{"empty type ID", diffSchema([]pego.SchemaType{{Name: "T", Origin: "declared", Shape: pego.SchemaShape{Kind: "struct"}}}, nil), "empty output schema type ID"},
		{"wrong namespace", diffSchema([]pego.SchemaType{{ID: "inferred/T", Name: "T", Origin: "declared", Shape: pego.SchemaShape{Kind: "struct"}}}, nil), "type identity"},
		{"duplicate type ID", diffSchema([]pego.SchemaType{{ID: "declared/T", Name: "T", Origin: "declared", Shape: pego.SchemaShape{Kind: "struct"}}, {ID: "declared/T", Name: "T", Origin: "declared", Shape: pego.SchemaShape{Kind: "struct"}}}, nil), "duplicate"},
		{"empty rule ID", diffSchema(nil, []pego.SchemaRule{{Origin: "inferred", Result: pego.SchemaShape{Kind: "nil"}}}), "empty output schema rule"},
		{"duplicate rule ID", diffSchema(nil, []pego.SchemaRule{{Name: "main", Origin: "inferred", Result: pego.SchemaShape{Kind: "nil"}}, {Name: "main", Origin: "inferred", Result: pego.SchemaShape{Kind: "nil"}}}), "duplicate"},
		{"declared non-struct", diffSchema([]pego.SchemaType{{ID: "declared/T", Name: "T", Origin: "declared", Shape: pego.SchemaShape{Kind: "nil"}}}, nil), "must be a struct"},
		{"empty ref", diffSchema(nil, []pego.SchemaRule{{Name: "main", Origin: "inferred", Result: pego.SchemaShape{Kind: "ref"}}}), "empty output schema reference"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if diff, err := pego.DiffOutputSchemas(valid, tc.bad); diff != nil || err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("diff=%+v, err=%v; want %q", diff, err, tc.want)
			}
		})
	}
	cycle := &pego.SchemaShape{Kind: "optional"}
	cycle.Element = cycle
	bad := diffSchema([]pego.SchemaType{{ID: "inferred/T", Name: "T", Origin: "inferred", Shape: *cycle}}, nil)
	if diff, err := pego.DiffOutputSchemas(bad, valid); diff != nil || err == nil || !strings.Contains(err.Error(), "cyclic inline") {
		t.Fatalf("cyclic shape: diff=%+v, err=%v", diff, err)
	}
}

func TestDiffOutputSchemasRejectsValueSliceCycles(t *testing.T) {
	valid := diffSchema(nil, nil)
	alternatives := make([]pego.SchemaShape, 1)
	alternatives[0] = pego.SchemaShape{Kind: "join", Alternatives: alternatives}
	fields := make([]pego.SchemaField, 1)
	fields[0] = pego.SchemaField{Name: "Self", Shape: pego.SchemaShape{Kind: "struct", Fields: fields}}
	for _, tc := range []struct {
		name  string
		shape pego.SchemaShape
	}{
		{"alternatives", alternatives[0]},
		{"fields", fields[0].Shape},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := diffSchema([]pego.SchemaType{{ID: "inferred/T", Name: "T", Origin: "inferred", Shape: tc.shape}}, nil)
			if diff, err := pego.DiffOutputSchemas(bad, valid); diff != nil || err == nil || !strings.Contains(err.Error(), "cyclic inline") {
				t.Fatalf("value-slice cycle: diff=%+v, err=%v", diff, err)
			}
		})
	}

	// Reusing an acyclic slice in separate branches is a DAG, not a cycle.
	shared := []pego.SchemaShape{{Kind: "scalar", Scalar: "string"}}
	branch := pego.SchemaShape{Kind: "join", Alternatives: shared}
	dag := diffSchema([]pego.SchemaType{{ID: "inferred/T", Name: "T", Origin: "inferred", Shape: pego.SchemaShape{Kind: "join", Alternatives: []pego.SchemaShape{branch, branch}}}}, nil)
	if diff, err := pego.DiffOutputSchemas(valid, dag); err != nil || diff == nil || len(diff.Types) != 1 {
		t.Fatalf("acyclic sharing: diff=%+v, err=%v", diff, err)
	}
}

func TestDiffOutputSchemasRejectsUnencodableDepth(t *testing.T) {
	shape := &pego.SchemaShape{Kind: "nil"}
	for i := 0; i < 10001; i++ {
		shape = &pego.SchemaShape{Kind: "optional", Element: shape}
	}
	deep := diffSchema([]pego.SchemaType{{ID: "inferred/T", Name: "T", Origin: "inferred", Shape: *shape}}, nil)
	if diff, err := pego.DiffOutputSchemas(diffSchema(nil, nil), deep); diff != nil || err == nil || !strings.Contains(err.Error(), "encode canonical entry") {
		t.Fatalf("unencodable depth: diff=%+v, err=%v", diff, err)
	}
}

func TestDiffOutputSchemasValidatesBothInputs(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*pego.OutputSchema)
	}{
		{"missing start", func(s *pego.OutputSchema) { s.Start = "absent" }},
		{"unresolved reference", func(s *pego.OutputSchema) { s.Types[0].Shape = pego.SchemaShape{Kind: "ref", Ref: "declared/Missing"} }},
		{"noncanonical result", func(s *pego.OutputSchema) { s.Rules[0].Result = pego.SchemaShape{Kind: "nil"} }},
		{"retired namespace", func(s *pego.OutputSchema) {
			s.Types = append(s.Types, pego.SchemaType{ID: "cst/Match", Name: "Match", Origin: "cst", Shape: pego.SchemaShape{Kind: "struct"}})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			valid, bad := diffSchema(nil, nil), diffSchema(nil, nil)
			tc.edit(bad)
			for _, pair := range []struct {
				before, after *pego.OutputSchema
				prefix        string
			}{{bad, valid, "before schema:"}, {valid, bad, "after schema:"}} {
				if diff, err := pego.DiffOutputSchemas(pair.before, pair.after); diff != nil || err == nil || !strings.HasPrefix(err.Error(), pair.prefix) {
					t.Fatalf("diff=%+v err=%v; want %s", diff, err, pair.prefix)
				}
			}
		})
	}
}

func sharedDAGSchema(depth int, scalar string, reverse bool) (*pego.OutputSchema, *pego.SchemaShape) {
	leaf := &pego.SchemaShape{Kind: "scalar", Scalar: scalar}
	shape := leaf
	for range depth {
		alternatives := []pego.SchemaShape{{Kind: "optional", Element: shape}, {Kind: "list", Element: shape}}
		if reverse {
			alternatives[0], alternatives[1] = alternatives[1], alternatives[0]
		}
		shape = &pego.SchemaShape{Kind: "join", Alternatives: alternatives}
	}
	return diffSchema([]pego.SchemaType{{ID: "inferred/DAG", Name: "DAG", Origin: "inferred", Shape: *shape}}, nil), leaf
}

func TestDiffOutputSchemasPreservesSharedDAGs(t *testing.T) {
	left, _ := sharedDAGSchema(28, "string", false)
	right, _ := sharedDAGSchema(28, "string", true)
	if diff, err := pego.DiffOutputSchemas(left, right); err != nil || len(diff.Types) != 0 || len(diff.Rules) != 0 {
		t.Fatalf("equal shared DAGs: diff=%+v err=%v", diff, err)
	}
	changed, leaf := sharedDAGSchema(28, "bool", true)
	diff, err := pego.DiffOutputSchemas(left, changed)
	if err != nil || len(diff.Types) != 1 || diff.Types[0].Kind != pego.SchemaChanged {
		t.Fatalf("changed shared DAG: diff=%+v err=%v", diff, err)
	}
	leaf.Scalar = "int"
	shape := &diff.Types[0].After.Shape
	for range 28 {
		if len(shape.Alternatives) != 2 || shape.Alternatives[0].Element != shape.Alternatives[1].Element {
			t.Fatal("canonical copy expanded or lost shared children")
		}
		shape = shape.Alternatives[0].Element
	}
	if shape.Scalar != "bool" {
		t.Fatal("input mutation changed the returned canonical copy")
	}
}

func TestDiffOutputSchemasRejectsDeepSharedReuse(t *testing.T) {
	shared := &pego.SchemaShape{Kind: "nil"}
	for range 9000 {
		shared = &pego.SchemaShape{Kind: "optional", Element: shared}
	}
	deep := shared
	for range 1001 {
		deep = &pego.SchemaShape{Kind: "optional", Element: deep}
	}
	// The shallow entry primes the canonical-copy cache. Reusing its subtree
	// under a deeper path must still enforce the encoding depth limit.
	s := diffSchema([]pego.SchemaType{
		{ID: "inferred/A", Name: "A", Origin: "inferred", Shape: *shared},
		{ID: "inferred/B", Name: "B", Origin: "inferred", Shape: *deep},
	}, nil)
	if got, err := pego.DiffOutputSchemas(s, diffSchema(nil, nil)); got != nil || err == nil || !strings.Contains(err.Error(), "encode canonical entry") {
		t.Fatalf("deep shared reuse: diff=%+v err=%v", got, err)
	}
}
