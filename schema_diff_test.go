package pego_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ornew/pego"
)

func diffSchema(types []pego.SchemaType, rules []pego.SchemaRule) *pego.OutputSchema {
	return &pego.OutputSchema{
		Format: "pego-output-schema", Version: 1, Start: "main",
		PositionUnitIndependent: true, Types: types, Rules: rules,
	}
}

func TestDiffOutputSchemasIgnoresUnorderedEntries(t *testing.T) {
	stringShape := pego.SchemaShape{Kind: "scalar", Scalar: "string"}
	boolShape := pego.SchemaShape{Kind: "scalar", Scalar: "bool"}
	item := pego.SchemaType{ID: "declared/Item", Name: "Item", Origin: "declared", Shape: pego.SchemaShape{
		Kind: "struct", Fields: []pego.SchemaField{
			{Name: "Z", Origin: "declared", Shape: stringShape},
			{Name: "A", Origin: "declared", Shape: pego.SchemaShape{Kind: "union", Alternatives: []pego.SchemaShape{stringShape, boolShape}}},
		},
	}}
	other := pego.SchemaType{ID: "inferred/main", Name: "main", Origin: "inferred", Shape: pego.SchemaShape{Kind: "ref", Ref: item.ID}}
	rule := pego.SchemaRule{Name: "main", Origin: "inferred", Result: pego.SchemaShape{Kind: "ref", Ref: other.ID}}
	left := diffSchema([]pego.SchemaType{item, other}, []pego.SchemaRule{rule, {Name: "other", Origin: "inferred", Result: stringShape}})
	itemReordered := item
	itemReordered.Shape.Fields = []pego.SchemaField{item.Shape.Fields[1], item.Shape.Fields[0]}
	itemReordered.Shape.Fields[0].Shape.Alternatives = []pego.SchemaShape{boolShape, stringShape}
	right := diffSchema([]pego.SchemaType{other, itemReordered}, []pego.SchemaRule{left.Rules[1], rule})
	right.Start = "other" // Envelope metadata is outside the entry diff.
	diff, err := pego.DiffOutputSchemas(left, right)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Types) != 0 || len(diff.Rules) != 0 {
		t.Fatalf("reordering changed semantic contents: %+v", diff)
	}
}

func TestDiffOutputSchemasReportsSortedEntryChanges(t *testing.T) {
	ref := pego.SchemaShape{Kind: "ref", Ref: "declared/Recursive"}
	old := diffSchema([]pego.SchemaType{
		{ID: "declared/Z", Name: "Z", Origin: "declared", Shape: pego.SchemaShape{Kind: "terminal"}},
		{ID: "declared/Recursive", Name: "Recursive", Origin: "declared", Shape: pego.SchemaShape{Kind: "optional", Element: &ref}},
	}, []pego.SchemaRule{
		{Name: "z", Origin: "inferred", Result: ref},
		{Name: "m", Origin: "inferred", Result: pego.SchemaShape{Kind: "nil"}},
	})
	newRef := pego.SchemaShape{Kind: "ref", Ref: "declared/Changed"}
	newer := diffSchema([]pego.SchemaType{
		{ID: "declared/Recursive", Name: "Recursive", Origin: "declared", Shape: pego.SchemaShape{Kind: "optional", Element: &newRef}},
		{ID: "declared/A", Name: "A", Origin: "declared", Shape: pego.SchemaShape{Kind: "terminal"}},
	}, []pego.SchemaRule{
		{Name: "a", Origin: "inferred", Result: ref},
		{Name: "m", Origin: "declared", Result: pego.SchemaShape{Kind: "nil"}},
	})
	diff, err := pego.DiffOutputSchemas(old, newer)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Types) != 3 || diff.Types[0].ID != "declared/A" || diff.Types[0].Kind != pego.SchemaAdded || diff.Types[0].Before != nil || diff.Types[0].After == nil ||
		diff.Types[1].ID != "declared/Recursive" || diff.Types[1].Kind != pego.SchemaChanged || diff.Types[1].Before == nil || diff.Types[1].After == nil ||
		diff.Types[2].ID != "declared/Z" || diff.Types[2].Kind != pego.SchemaRemoved || diff.Types[2].Before == nil || diff.Types[2].After != nil {
		t.Fatalf("type changes: %+v", diff.Types)
	}
	if diff.Types[1].Before.Shape.Element.Ref != "declared/Recursive" || diff.Types[1].After.Shape.Element.Ref != "declared/Changed" {
		t.Fatalf("named recursion was expanded or lost: %+v", diff.Types[1])
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
		{"version", &pego.OutputSchema{Format: "pego-output-schema", Version: 2}, "unsupported output schema"},
		{"empty type ID", diffSchema([]pego.SchemaType{{Name: "T", Origin: "declared", Shape: pego.SchemaShape{Kind: "terminal"}}}, nil), "invalid type identity"},
		{"wrong namespace", diffSchema([]pego.SchemaType{{ID: "inferred/T", Name: "T", Origin: "declared", Shape: pego.SchemaShape{Kind: "terminal"}}}, nil), "invalid type identity"},
		{"duplicate type ID", diffSchema([]pego.SchemaType{{ID: "declared/T", Name: "T", Origin: "declared", Shape: pego.SchemaShape{Kind: "terminal"}}, {ID: "declared/T", Name: "T", Origin: "declared", Shape: pego.SchemaShape{Kind: "terminal"}}}, nil), "duplicate type ID"},
		{"empty rule ID", diffSchema(nil, []pego.SchemaRule{{Origin: "inferred", Result: pego.SchemaShape{Kind: "nil"}}}), "invalid rule identity"},
		{"duplicate rule ID", diffSchema(nil, []pego.SchemaRule{{Name: "main", Origin: "inferred", Result: pego.SchemaShape{Kind: "nil"}}, {Name: "main", Origin: "inferred", Result: pego.SchemaShape{Kind: "nil"}}}), "duplicate rule ID"},
		{"empty ref", diffSchema(nil, []pego.SchemaRule{{Name: "main", Origin: "inferred", Result: pego.SchemaShape{Kind: "ref"}}}), "reference has no ID"},
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
	bad := diffSchema([]pego.SchemaType{{ID: "declared/T", Name: "T", Origin: "declared", Shape: *cycle}}, nil)
	if diff, err := pego.DiffOutputSchemas(bad, valid); diff != nil || err == nil || !strings.Contains(err.Error(), "cyclic inline shape") {
		t.Fatalf("cyclic shape: diff=%+v, err=%v", diff, err)
	}
}

func TestDiffOutputSchemasRejectsValueSliceCycles(t *testing.T) {
	valid := diffSchema(nil, nil)
	alternatives := make([]pego.SchemaShape, 1)
	alternatives[0] = pego.SchemaShape{Kind: "union", Alternatives: alternatives}
	fields := make([]pego.SchemaField, 1)
	fields[0] = pego.SchemaField{Name: "Self", Origin: "declared", Shape: pego.SchemaShape{Kind: "struct", Fields: fields}}
	for _, tc := range []struct {
		name  string
		shape pego.SchemaShape
	}{
		{"alternatives", alternatives[0]},
		{"fields", fields[0].Shape},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := diffSchema([]pego.SchemaType{{ID: "declared/T", Name: "T", Origin: "declared", Shape: tc.shape}}, nil)
			if diff, err := pego.DiffOutputSchemas(bad, valid); diff != nil || err == nil || !strings.Contains(err.Error(), "cyclic inline shape") {
				t.Fatalf("value-slice cycle: diff=%+v, err=%v", diff, err)
			}
		})
	}

	// Reusing an acyclic slice in separate branches is a DAG, not a cycle.
	shared := []pego.SchemaShape{{Kind: "scalar", Scalar: "string"}}
	branch := pego.SchemaShape{Kind: "union", Alternatives: shared}
	dag := diffSchema([]pego.SchemaType{{ID: "declared/T", Name: "T", Origin: "declared", Shape: pego.SchemaShape{Kind: "union", Alternatives: []pego.SchemaShape{branch, branch}}}}, nil)
	if diff, err := pego.DiffOutputSchemas(valid, dag); err != nil || diff == nil || len(diff.Types) != 1 {
		t.Fatalf("acyclic sharing: diff=%+v, err=%v", diff, err)
	}
}

func TestDiffOutputSchemasRejectsUnencodableDepth(t *testing.T) {
	shape := &pego.SchemaShape{Kind: "nil"}
	for i := 0; i < 10001; i++ {
		shape = &pego.SchemaShape{Kind: "optional", Element: shape}
	}
	deep := diffSchema([]pego.SchemaType{{ID: "declared/T", Name: "T", Origin: "declared", Shape: *shape}}, nil)
	if diff, err := pego.DiffOutputSchemas(diffSchema(nil, nil), deep); diff != nil || err == nil || !strings.Contains(err.Error(), "encode canonical entry") {
		t.Fatalf("unencodable depth: diff=%+v, err=%v", diff, err)
	}
}
