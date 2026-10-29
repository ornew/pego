package schema_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ornew/pego/schema"
)

func diffSchema(types []schema.Type, rules []schema.Rule) *schema.Output {
	types = append([]schema.Type{}, types...)
	rules = append([]schema.Rule{}, rules...)
	if len(rules) == 0 {
		rules = append(rules, schema.Rule{Name: "main", Origin: "inferred", Result: schema.Shape{Kind: "nil"}})
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
			types = append(types, schema.Type{ID: id, Name: rule.Name, Origin: "inferred", Shape: rule.Result})
		}
		rule.Result = schema.Shape{Kind: "ref", Ref: id}
	}
	return &schema.Output{
		Format: "pego-output-schema", Version: 2, Start: start,
		PositionUnitIndependent: true, Types: types, Rules: rules,
	}
}

func TestCompareIgnoresUnorderedEntries(t *testing.T) {
	stringShape := schema.Shape{Kind: "scalar", Scalar: "string"}
	boolShape := schema.Shape{Kind: "scalar", Scalar: "bool"}
	item := schema.Type{ID: "declared/Item", Name: "Item", Origin: "declared", Shape: schema.Shape{
		Kind: "struct", Fields: []schema.Field{
			{Name: "Z", Shape: stringShape},
			{Name: "A", Shape: schema.Shape{Kind: "join", Alternatives: []schema.Shape{stringShape, boolShape}}},
		},
	}}
	item.Shape.Fields = append(item.Shape.Fields, schema.Field{Name: "N", Shape: schema.Shape{
		Kind: "node", Value: &schema.Shape{Kind: "tuple", Elements: []schema.Shape{stringShape, stringShape}},
		Captures: []schema.Capture{
			{Name: "y", Paths: [][]int{{1}, {0}}, Shape: stringShape},
			{Name: "x", Side: true, Shape: boolShape},
		},
	}})
	other := schema.Type{ID: "inferred/main", Name: "main", Origin: "inferred", Shape: schema.Shape{Kind: "ref", Ref: item.ID}}
	rule := schema.Rule{Name: "main", Origin: "inferred", Result: schema.Shape{Kind: "ref", Ref: other.ID}}
	left := diffSchema([]schema.Type{item, other}, []schema.Rule{rule, {Name: "other", Origin: "inferred", Result: stringShape}})
	itemReordered := item
	node := item.Shape.Fields[2].Shape
	node.Captures = []schema.Capture{node.Captures[1], node.Captures[0]}
	node.Captures[1].Paths = [][]int{{0}, {1}}
	itemReordered.Shape.Fields = []schema.Field{item.Shape.Fields[1], {Name: "N", Shape: node}, item.Shape.Fields[0]}
	itemReordered.Shape.Fields[0].Shape.Alternatives = []schema.Shape{boolShape, stringShape}
	right := diffSchema([]schema.Type{left.Types[2], other, itemReordered}, []schema.Rule{left.Rules[1], rule})
	right.Start = "other" // Envelope metadata is outside the entry diff.
	diff, err := schema.Compare(left, right)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Types) != 0 || len(diff.Rules) != 0 {
		t.Fatalf("reordering changed semantic contents: %+v", diff)
	}
	// Tuple elements are ordered.
	node.Value = &schema.Shape{Kind: "tuple", Elements: []schema.Shape{stringShape, boolShape}}
	itemReordered.Shape.Fields[1].Shape = node
	right = diffSchema([]schema.Type{left.Types[2], other, itemReordered}, []schema.Rule{left.Rules[1], rule})
	if diff, err = schema.Compare(left, right); err != nil || len(diff.Types) != 1 || diff.Types[0].ID != "declared/Item" {
		t.Fatalf("element change: %+v %v", diff, err)
	}
}

func TestCompareReportsSortedEntryChanges(t *testing.T) {
	ref := schema.Shape{Kind: "ref", Ref: "declared/Recursive"}
	empty := schema.Shape{Kind: "struct"}
	next := func(ref *schema.Shape) schema.Shape {
		return schema.Shape{Kind: "struct", Fields: []schema.Field{{Name: "Next", Shape: schema.Shape{Kind: "optional", Element: ref}}}}
	}
	old := diffSchema([]schema.Type{
		{ID: "declared/Z", Name: "Z", Origin: "declared", Shape: empty},
		{ID: "declared/Recursive", Name: "Recursive", Origin: "declared", Shape: next(&ref)},
	}, []schema.Rule{
		{Name: "z", Origin: "inferred", Result: ref},
		{Name: "m", Origin: "inferred", Result: schema.Shape{Kind: "nil"}},
	})
	newRef := schema.Shape{Kind: "ref", Ref: "declared/Changed"}
	newer := diffSchema([]schema.Type{
		{ID: "declared/Recursive", Name: "Recursive", Origin: "declared", Shape: next(&newRef)},
		{ID: "declared/A", Name: "A", Origin: "declared", Shape: empty},
		{ID: "declared/Changed", Name: "Changed", Origin: "declared", Shape: empty},
	}, []schema.Rule{
		{Name: "a", Origin: "inferred", Result: ref},
		{Name: "m", Origin: "declared", Result: schema.Shape{Kind: "nil"}},
	})
	diff, err := schema.Compare(old, newer)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Types) != 6 || diff.Types[0].ID != "declared/A" || diff.Types[0].Kind != schema.Added || diff.Types[0].Before != nil || diff.Types[0].After == nil ||
		diff.Types[1].ID != "declared/Changed" || diff.Types[1].Kind != schema.Added ||
		diff.Types[2].ID != "declared/Recursive" || diff.Types[2].Kind != schema.Changed || diff.Types[2].Before == nil || diff.Types[2].After == nil ||
		diff.Types[3].ID != "declared/Z" || diff.Types[3].Kind != schema.Removed || diff.Types[3].Before == nil || diff.Types[3].After != nil ||
		diff.Types[4].ID != "inferred/a" || diff.Types[4].Kind != schema.Added ||
		diff.Types[5].ID != "inferred/z" || diff.Types[5].Kind != schema.Removed {
		t.Fatalf("type changes: %+v", diff.Types)
	}
	if diff.Types[2].Before.Shape.Fields[0].Shape.Element.Ref != "declared/Recursive" || diff.Types[2].After.Shape.Fields[0].Shape.Element.Ref != "declared/Changed" {
		t.Fatalf("named recursion was expanded or lost: %+v", diff.Types[2])
	}
	if len(diff.Rules) != 3 || diff.Rules[0].ID != "a" || diff.Rules[0].Kind != schema.Added ||
		diff.Rules[1].ID != "m" || diff.Rules[1].Kind != schema.Changed ||
		diff.Rules[2].ID != "z" || diff.Rules[2].Kind != schema.Removed {
		t.Fatalf("rule changes: %+v", diff.Rules)
	}
	first, err := json.Marshal(diff)
	if err != nil {
		t.Fatal(err)
	}
	newer.Types[0], newer.Types[1] = newer.Types[1], newer.Types[0]
	newer.Rules[0], newer.Rules[1] = newer.Rules[1], newer.Rules[0]
	again, err := schema.Compare(old, newer)
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(again)
	if err != nil || string(first) != string(second) {
		t.Fatalf("unstable diff: %s / %s, %v", first, second, err)
	}
}

func TestCompareRejectsMalformedIdentities(t *testing.T) {
	valid := diffSchema(nil, nil)
	cases := []struct {
		name string
		bad  *schema.Output
		want string
	}{
		{"nil", nil, "nil output schema"},
		{"version", &schema.Output{Format: "pego-output-schema", Version: 3}, "unsupported output schema"},
		{"empty type ID", diffSchema([]schema.Type{{Name: "T", Origin: "declared", Shape: schema.Shape{Kind: "struct"}}}, nil), "empty output schema type ID"},
		{"wrong namespace", diffSchema([]schema.Type{{ID: "inferred/T", Name: "T", Origin: "declared", Shape: schema.Shape{Kind: "struct"}}}, nil), "type identity"},
		{"duplicate type ID", diffSchema([]schema.Type{{ID: "declared/T", Name: "T", Origin: "declared", Shape: schema.Shape{Kind: "struct"}}, {ID: "declared/T", Name: "T", Origin: "declared", Shape: schema.Shape{Kind: "struct"}}}, nil), "duplicate"},
		{"empty rule ID", diffSchema(nil, []schema.Rule{{Origin: "inferred", Result: schema.Shape{Kind: "nil"}}}), "empty output schema rule"},
		{"duplicate rule ID", diffSchema(nil, []schema.Rule{{Name: "main", Origin: "inferred", Result: schema.Shape{Kind: "nil"}}, {Name: "main", Origin: "inferred", Result: schema.Shape{Kind: "nil"}}}), "duplicate"},
		{"declared non-struct", diffSchema([]schema.Type{{ID: "declared/T", Name: "T", Origin: "declared", Shape: schema.Shape{Kind: "nil"}}}, nil), "must be a struct"},
		{"empty ref", diffSchema(nil, []schema.Rule{{Name: "main", Origin: "inferred", Result: schema.Shape{Kind: "ref"}}}), "empty output schema reference"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if diff, err := schema.Compare(valid, tc.bad); diff != nil || err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("diff=%+v, err=%v; want %q", diff, err, tc.want)
			}
		})
	}
	cycle := &schema.Shape{Kind: "optional"}
	cycle.Element = cycle
	bad := diffSchema([]schema.Type{{ID: "inferred/T", Name: "T", Origin: "inferred", Shape: *cycle}}, nil)
	if diff, err := schema.Compare(bad, valid); diff != nil || err == nil || !strings.Contains(err.Error(), "cyclic inline") {
		t.Fatalf("cyclic shape: diff=%+v, err=%v", diff, err)
	}
}

func TestCompareRejectsValueSliceCycles(t *testing.T) {
	valid := diffSchema(nil, nil)
	alternatives := make([]schema.Shape, 1)
	alternatives[0] = schema.Shape{Kind: "join", Alternatives: alternatives}
	fields := make([]schema.Field, 1)
	fields[0] = schema.Field{Name: "Self", Shape: schema.Shape{Kind: "struct", Fields: fields}}
	for _, tc := range []struct {
		name  string
		shape schema.Shape
	}{
		{"alternatives", alternatives[0]},
		{"fields", fields[0].Shape},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := diffSchema([]schema.Type{{ID: "inferred/T", Name: "T", Origin: "inferred", Shape: tc.shape}}, nil)
			if diff, err := schema.Compare(bad, valid); diff != nil || err == nil || !strings.Contains(err.Error(), "cyclic inline") {
				t.Fatalf("value-slice cycle: diff=%+v, err=%v", diff, err)
			}
		})
	}

	// Reusing an acyclic slice in separate branches is a DAG, not a cycle.
	shared := []schema.Shape{{Kind: "scalar", Scalar: "string"}}
	branch := schema.Shape{Kind: "join", Alternatives: shared}
	dag := diffSchema([]schema.Type{{ID: "inferred/T", Name: "T", Origin: "inferred", Shape: schema.Shape{Kind: "join", Alternatives: []schema.Shape{branch, branch}}}}, nil)
	if diff, err := schema.Compare(valid, dag); err != nil || diff == nil || len(diff.Types) != 1 {
		t.Fatalf("acyclic sharing: diff=%+v, err=%v", diff, err)
	}
}

func TestCompareRejectsUnencodableDepth(t *testing.T) {
	shape := &schema.Shape{Kind: "nil"}
	for i := 0; i < 10001; i++ {
		shape = &schema.Shape{Kind: "optional", Element: shape}
	}
	deep := diffSchema([]schema.Type{{ID: "inferred/T", Name: "T", Origin: "inferred", Shape: *shape}}, nil)
	if diff, err := schema.Compare(diffSchema(nil, nil), deep); diff != nil || err == nil || !strings.Contains(err.Error(), "encode canonical entry") {
		t.Fatalf("unencodable depth: diff=%+v, err=%v", diff, err)
	}
}

func TestCompareValidatesBothInputs(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*schema.Output)
	}{
		{"missing start", func(s *schema.Output) { s.Start = "absent" }},
		{"unresolved reference", func(s *schema.Output) { s.Types[0].Shape = schema.Shape{Kind: "ref", Ref: "declared/Missing"} }},
		{"noncanonical result", func(s *schema.Output) { s.Rules[0].Result = schema.Shape{Kind: "nil"} }},
		{"retired namespace", func(s *schema.Output) {
			s.Types = append(s.Types, schema.Type{ID: "cst/Match", Name: "Match", Origin: "cst", Shape: schema.Shape{Kind: "struct"}})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			valid, bad := diffSchema(nil, nil), diffSchema(nil, nil)
			tc.edit(bad)
			for _, pair := range []struct {
				before, after *schema.Output
				prefix        string
			}{{bad, valid, "before schema:"}, {valid, bad, "after schema:"}} {
				if diff, err := schema.Compare(pair.before, pair.after); diff != nil || err == nil || !strings.HasPrefix(err.Error(), pair.prefix) {
					t.Fatalf("diff=%+v err=%v; want %s", diff, err, pair.prefix)
				}
			}
		})
	}
}

func sharedDAGSchema(depth int, scalar string, reverse bool) (*schema.Output, *schema.Shape) {
	leaf := &schema.Shape{Kind: "scalar", Scalar: scalar}
	shape := leaf
	for range depth {
		alternatives := []schema.Shape{{Kind: "optional", Element: shape}, {Kind: "list", Element: shape}}
		if reverse {
			alternatives[0], alternatives[1] = alternatives[1], alternatives[0]
		}
		shape = &schema.Shape{Kind: "join", Alternatives: alternatives}
	}
	return diffSchema([]schema.Type{{ID: "inferred/DAG", Name: "DAG", Origin: "inferred", Shape: *shape}}, nil), leaf
}

func TestComparePreservesSharedDAGs(t *testing.T) {
	left, _ := sharedDAGSchema(28, "string", false)
	right, _ := sharedDAGSchema(28, "string", true)
	if diff, err := schema.Compare(left, right); err != nil || len(diff.Types) != 0 || len(diff.Rules) != 0 {
		t.Fatalf("equal shared DAGs: diff=%+v err=%v", diff, err)
	}
	changed, leaf := sharedDAGSchema(28, "bool", true)
	diff, err := schema.Compare(left, changed)
	if err != nil || len(diff.Types) != 1 || diff.Types[0].Kind != schema.Changed {
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

func TestCompareRejectsDeepSharedReuse(t *testing.T) {
	shared := &schema.Shape{Kind: "nil"}
	for range 9000 {
		shared = &schema.Shape{Kind: "optional", Element: shared}
	}
	deep := shared
	for range 1001 {
		deep = &schema.Shape{Kind: "optional", Element: deep}
	}
	// The shallow entry primes the canonical-copy cache. Reusing its subtree
	// under a deeper path must still enforce the encoding depth limit.
	s := diffSchema([]schema.Type{
		{ID: "inferred/A", Name: "A", Origin: "inferred", Shape: *shared},
		{ID: "inferred/B", Name: "B", Origin: "inferred", Shape: *deep},
	}, nil)
	if got, err := schema.Compare(s, diffSchema(nil, nil)); got != nil || err == nil || !strings.Contains(err.Error(), "encode canonical entry") {
		t.Fatalf("deep shared reuse: diff=%+v err=%v", got, err)
	}
}
