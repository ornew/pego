package schema_test

import (
	"slices"
	"testing"

	"github.com/ornew/zeroparse/schema"
)

func shapeOf(kind string) schema.Shape { return schema.Shape{Kind: kind} }

func scalarOf(name string) schema.Shape { return schema.Shape{Kind: "scalar", Scalar: name} }

func structOf(fields ...schema.Field) schema.Shape {
	return schema.Shape{Kind: "struct", Fields: fields}
}

func fieldOf(name string, s schema.Shape) schema.Field { return schema.Field{Name: name, Shape: s} }

func listOf(s schema.Shape) schema.Shape     { return schema.Shape{Kind: "list", Element: &s} }
func optionalOf(s schema.Shape) schema.Shape { return schema.Shape{Kind: "optional", Element: &s} }
func joinOf(s ...schema.Shape) schema.Shape  { return schema.Shape{Kind: "join", Alternatives: s} }
func tupleOf(s ...schema.Shape) schema.Shape { return schema.Shape{Kind: "tuple", Elements: s} }
func nodeOf(v schema.Shape, c ...schema.Capture) schema.Shape {
	return schema.Shape{Kind: "node", Value: &v, Captures: c}
}

func declared(s schema.Shape) *schema.Output {
	return diffSchema([]schema.Type{
		{ID: "declared/T", Name: "T", Origin: "declared", Shape: s},
		{ID: "declared/U", Name: "U", Origin: "declared", Shape: structOf()},
	}, nil)
}

type classCase struct {
	name       string
	before     schema.Shape
	after      schema.Shape
	class      schema.Class
	rule, path string
	// want is the exact impact list when it is not the single impact of class, rule and path.
	want []schema.Impact
}

func TestCompareClassifiesShapeChanges(t *testing.T) {
	i, s, b := scalarOf("int"), scalarOf("string"), scalarOf("bool")
	cases := []classCase{
		{"field added", structOf(fieldOf("a", i)), structOf(fieldOf("a", i), fieldOf("b", s)), schema.Compatible, schema.RuleFieldAdded, "$.fields.b", nil},
		{"field removed", structOf(fieldOf("a", i), fieldOf("b", s)), structOf(fieldOf("a", i)), schema.Breaking, schema.RuleFieldRemoved, "$.fields.b", nil},
		{"scalar changed", structOf(fieldOf("a", i)), structOf(fieldOf("a", s)), schema.Breaking, schema.RuleScalarChanged, "$.fields.a", nil},
		{"kind changed", structOf(fieldOf("a", i)), structOf(fieldOf("a", listOf(i))), schema.Breaking, schema.RuleKindChanged, "$.fields.a", nil},
		{"reference changed", structOf(fieldOf("a", schema.Shape{Kind: "ref", Ref: "declared/T"})), structOf(fieldOf("a", schema.Shape{Kind: "ref", Ref: "declared/U"})), schema.Breaking, schema.RuleReferenceChanged, "$.fields.a", nil},
		{"alternative added", structOf(fieldOf("a", joinOf(i, s))), structOf(fieldOf("a", joinOf(i, s, b))), schema.Breaking, schema.RuleAlternativeAdded, "$.fields.a.alternatives", nil},
		{"alternative removed", structOf(fieldOf("a", joinOf(i, s, b))), structOf(fieldOf("a", joinOf(i, s))), schema.Breaking, schema.RuleAlternativeRemoved, "$.fields.a.alternatives", nil},
		{"join narrowed to a single type", structOf(fieldOf("a", joinOf(i, s))), structOf(fieldOf("a", i)), schema.Breaking, schema.RuleKindChanged, "$.fields.a", nil},
		{"optional made required", structOf(fieldOf("a", optionalOf(i))), structOf(fieldOf("a", i)), schema.Breaking, schema.RuleKindChanged, "$.fields.a", nil},
		{"required made optional", structOf(fieldOf("a", i)), structOf(fieldOf("a", optionalOf(i))), schema.Breaking, schema.RuleKindChanged, "$.fields.a", nil},
		{"type widened to top", structOf(fieldOf("a", i)), structOf(fieldOf("a", shapeOf("top"))), schema.Breaking, schema.RuleKindChanged, "$.fields.a", nil},
		{"error alternative added", structOf(fieldOf("a", joinOf(i, s))), structOf(fieldOf("a", joinOf(i, s, shapeOf("error")))), schema.Breaking, schema.RuleAlternativeAdded, "$.fields.a.alternatives", nil},
		{"missing alternative added", structOf(fieldOf("a", joinOf(i, s))), structOf(fieldOf("a", joinOf(i, s, shapeOf("missing")))), schema.Breaking, schema.RuleAlternativeAdded, "$.fields.a.alternatives", nil},
		{"list element changed", structOf(fieldOf("a", listOf(i))), structOf(fieldOf("a", listOf(s))), schema.Breaking, schema.RuleScalarChanged, "$.fields.a.element", nil},
		{"optional element changed", structOf(fieldOf("a", optionalOf(i))), structOf(fieldOf("a", optionalOf(s))), schema.Breaking, schema.RuleScalarChanged, "$.fields.a.element", nil},
		{"tuple element appended", structOf(fieldOf("a", tupleOf(i))), structOf(fieldOf("a", tupleOf(i, s))), schema.Breaking, schema.RuleTupleArityChanged, "$.fields.a", nil},
		{"tuple element removed", structOf(fieldOf("a", tupleOf(i, s))), structOf(fieldOf("a", tupleOf(i))), schema.Breaking, schema.RuleTupleArityChanged, "$.fields.a", nil},
		{"tuple elements reordered", structOf(fieldOf("a", tupleOf(i, s))), structOf(fieldOf("a", tupleOf(s, i))), schema.Breaking, "", "", []schema.Impact{
			{Path: "$.fields.a[0]", Class: schema.Breaking, Rule: schema.RuleScalarChanged},
			{Path: "$.fields.a[1]", Class: schema.Breaking, Rule: schema.RuleScalarChanged}}},
		{"node value changed", structOf(fieldOf("a", nodeOf(i))), structOf(fieldOf("a", nodeOf(s))), schema.Breaking, schema.RuleScalarChanged, "$.fields.a.value", nil},
		{"capture added", structOf(fieldOf("a", nodeOf(tupleOf(i)))), structOf(fieldOf("a", nodeOf(tupleOf(i), schema.Capture{Name: "x", Paths: [][]int{{0}}, Shape: i}))), schema.Compatible, schema.RuleCaptureAdded, "$.fields.a.captures.x", nil},
		{"capture removed", structOf(fieldOf("a", nodeOf(tupleOf(i), schema.Capture{Name: "x", Paths: [][]int{{0}}, Shape: i}))), structOf(fieldOf("a", nodeOf(tupleOf(i)))), schema.Breaking, schema.RuleCaptureRemoved, "$.fields.a.captures.x", nil},
		{"capture shape changed", structOf(fieldOf("a", nodeOf(tupleOf(i), schema.Capture{Name: "x", Paths: [][]int{{0}}, Shape: i}))), structOf(fieldOf("a", nodeOf(tupleOf(i), schema.Capture{Name: "x", Paths: [][]int{{0}}, Shape: s}))), schema.Breaking, schema.RuleScalarChanged, "$.fields.a.captures.x", nil},
		{"capture path changed", structOf(fieldOf("a", nodeOf(tupleOf(i, i), schema.Capture{Name: "x", Paths: [][]int{{0}}, Shape: i}))), structOf(fieldOf("a", nodeOf(tupleOf(i, i), schema.Capture{Name: "x", Paths: [][]int{{1}}, Shape: i}))), schema.Breaking, schema.RuleCaptureMoved, "$.fields.a.captures.x", nil},
		{"capture side changed", structOf(fieldOf("a", nodeOf(tupleOf(i), schema.Capture{Name: "x", Paths: [][]int{{0}}, Shape: i}))), structOf(fieldOf("a", nodeOf(tupleOf(i), schema.Capture{Name: "x", Paths: [][]int{{0}}, Side: true, Shape: i}))), schema.Breaking, schema.RuleCaptureMoved, "$.fields.a.captures.x", nil},
		// A removal nested in a list element is found through the element.
		{"nested field removed", structOf(fieldOf("a", listOf(structOf(fieldOf("p", i), fieldOf("q", i))))), structOf(fieldOf("a", listOf(structOf(fieldOf("p", i))))), schema.Breaking, schema.RuleFieldRemoved, "$.fields.a.element.fields.q", nil},
		// A nested addition stays compatible.
		{"nested field added", structOf(fieldOf("a", listOf(structOf(fieldOf("p", i))))), structOf(fieldOf("a", listOf(structOf(fieldOf("p", i), fieldOf("q", i))))), schema.Compatible, schema.RuleFieldAdded, "$.fields.a.element.fields.q", nil},
		{"alternative added in list", structOf(fieldOf("a", listOf(joinOf(i, s)))), structOf(fieldOf("a", listOf(joinOf(i, s, b)))), schema.Breaking, schema.RuleAlternativeAdded, "$.fields.a.element.alternatives", nil},
		{"alternative removed in list", structOf(fieldOf("a", listOf(joinOf(i, s, b)))), structOf(fieldOf("a", listOf(joinOf(i, s)))), schema.Breaking, schema.RuleAlternativeRemoved, "$.fields.a.element.alternatives", nil},
		{"alternative added in optional", structOf(fieldOf("a", optionalOf(joinOf(i, s)))), structOf(fieldOf("a", optionalOf(joinOf(i, s, b)))), schema.Breaking, schema.RuleAlternativeAdded, "$.fields.a.element.alternatives", nil},
		{"alternative removed in optional", structOf(fieldOf("a", optionalOf(joinOf(i, s, b)))), structOf(fieldOf("a", optionalOf(joinOf(i, s)))), schema.Breaking, schema.RuleAlternativeRemoved, "$.fields.a.element.alternatives", nil},
		{"deep node, tuple and capture", structOf(fieldOf("a", listOf(nodeOf(tupleOf(i, nodeOf(tupleOf(s), schema.Capture{Name: "y", Paths: [][]int{{0}}, Shape: i})), schema.Capture{Name: "x", Paths: [][]int{{1}}, Shape: s})))),
			structOf(fieldOf("a", listOf(nodeOf(tupleOf(i, nodeOf(tupleOf(s), schema.Capture{Name: "y", Paths: [][]int{{0}}, Shape: b})), schema.Capture{Name: "x", Paths: [][]int{{1}}, Shape: s})))),
			schema.Breaking, schema.RuleScalarChanged, "$.fields.a.element.value[1].captures.y", nil},
		{"deep capture added", structOf(fieldOf("a", listOf(nodeOf(tupleOf(i, nodeOf(tupleOf(s))))))),
			structOf(fieldOf("a", listOf(nodeOf(tupleOf(i, nodeOf(tupleOf(s), schema.Capture{Name: "y", Paths: [][]int{{0}}, Shape: i})))))),
			schema.Compatible, schema.RuleCaptureAdded, "$.fields.a.element.value[1].captures.y", nil},
		{"reference in list changed", structOf(fieldOf("a", listOf(schema.Shape{Kind: "ref", Ref: "declared/T"}))), structOf(fieldOf("a", listOf(schema.Shape{Kind: "ref", Ref: "declared/U"}))), schema.Breaking, schema.RuleReferenceChanged, "$.fields.a.element", nil},
		{"mixed field removed and added", structOf(fieldOf("a", i), fieldOf("b", i)), structOf(fieldOf("a", i), fieldOf("c", i)), schema.Breaking, "", "", []schema.Impact{
			{Path: "$.fields.b", Class: schema.Breaking, Rule: schema.RuleFieldRemoved},
			{Path: "$.fields.c", Class: schema.Compatible, Rule: schema.RuleFieldAdded}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diff, err := schema.Compare(declared(tc.before), declared(tc.after))
			if err != nil {
				t.Fatal(err)
			}
			if len(diff.Types) != 1 || diff.Types[0].ID != "declared/T" || len(diff.Rules) != 0 {
				t.Fatalf("diff: %+v", diff)
			}
			c := diff.Types[0]
			want := tc.want
			if want == nil {
				want = []schema.Impact{{Path: tc.path, Class: tc.class, Rule: tc.rule}}
			}
			if c.Kind != schema.Changed || c.Class != tc.class || !slices.Equal(c.Impacts, want) {
				t.Fatalf("got %s %+v, want %s %+v", c.Class, c.Impacts, tc.class, want)
			}
			// Negative control: no impact has the other class, unless the case lists a mix.
			for _, im := range c.Impacts {
				if tc.want == nil && im.Class != tc.class {
					t.Errorf("impact %+v has the opposite class", im)
				}
			}
			// Swapping the operands reports the inverse: added becomes removed.
			back, err := schema.Compare(declared(tc.after), declared(tc.before))
			if err != nil || len(back.Types) != 1 || len(back.Types[0].Impacts) == 0 {
				t.Fatalf("reverse diff: %+v %v", back, err)
			}
		})
	}
}

func TestCompareClassifiesEntryChanges(t *testing.T) {
	i := scalarOf("int")
	base := diffSchema([]schema.Type{{ID: "declared/T", Name: "T", Origin: "declared", Shape: structOf(fieldOf("a", i))}},
		[]schema.Rule{{Name: "main", Origin: "inferred", Result: shapeOf("nil")}, {Name: "other", Origin: "inferred", Result: i}})
	plus := diffSchema([]schema.Type{
		{ID: "declared/T", Name: "T", Origin: "declared", Shape: structOf(fieldOf("a", i))},
		{ID: "declared/U", Name: "U", Origin: "declared", Shape: structOf()},
	}, []schema.Rule{{Name: "main", Origin: "inferred", Result: shapeOf("nil")}, {Name: "other", Origin: "inferred", Result: i}, {Name: "third", Origin: "inferred", Result: i}})
	minus := diffSchema(nil, []schema.Rule{{Name: "main", Origin: "inferred", Result: shapeOf("nil")}})
	flipped := diffSchema([]schema.Type{{ID: "declared/T", Name: "T", Origin: "declared", Shape: structOf(fieldOf("a", i))}},
		[]schema.Rule{{Name: "main", Origin: "declared", Result: shapeOf("nil")}, {Name: "other", Origin: "inferred", Result: i}})
	// Added entries are compatible; removed entries are breaking, even a type nothing refers to.
	check := func(name string, before, after *schema.Output, types, rules map[string][2]string) {
		t.Helper()
		diff, err := schema.Compare(before, after)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string][2]string{}
		for _, c := range diff.Types {
			if len(c.Impacts) == 0 || c.Impacts[0].Class != c.Class {
				t.Errorf("%s: type %s without matching impact: %+v", name, c.ID, c)
			}
			got[c.ID] = [2]string{string(c.Class), c.Impacts[0].Rule}
		}
		for id, want := range types {
			if got[id] != want {
				t.Errorf("%s: type %s = %v, want %v", name, id, got[id], want)
			}
		}
		got = map[string][2]string{}
		for _, c := range diff.Rules {
			got[c.ID] = [2]string{string(c.Class), c.Impacts[0].Rule}
		}
		for id, want := range rules {
			if got[id] != want {
				t.Errorf("%s: rule %s = %v, want %v", name, id, got[id], want)
			}
		}
	}
	check("added", base, plus,
		map[string][2]string{"declared/U": {"compatible", schema.RuleTypeAdded}, "inferred/third": {"compatible", schema.RuleTypeAdded}},
		map[string][2]string{"third": {"compatible", schema.RuleRuleAdded}})
	check("removed", base, minus,
		map[string][2]string{"declared/T": {"breaking", schema.RuleTypeRemoved}, "inferred/other": {"breaking", schema.RuleTypeRemoved}},
		map[string][2]string{"other": {"breaking", schema.RuleRuleRemoved}})
	// The origin of a rule flips with the same shape and ID: compatible.
	check("origin", base, flipped, nil, map[string][2]string{"main": {"compatible", schema.RuleOriginChanged}})
}

// Every changed entry says why.
func TestCompareChangedEntriesHaveImpacts(t *testing.T) {
	a := diffSchema([]schema.Type{{ID: "declared/T", Name: "T", Origin: "declared", Shape: structOf(fieldOf("a", scalarOf("int")))}}, nil)
	b := diffSchema([]schema.Type{{ID: "declared/T", Name: "T", Origin: "declared", Shape: structOf(fieldOf("a", scalarOf("bool")))}},
		[]schema.Rule{{Name: "main", Origin: "declared", Result: shapeOf("nil")}})
	diff, err := schema.Compare(a, b)
	if err != nil || len(diff.Types)+len(diff.Rules) < 2 {
		t.Fatalf("%+v %v", diff, err)
	}
	for _, c := range diff.Types {
		if len(c.Impacts) == 0 || c.Class == "" {
			t.Errorf("type %s: %+v", c.ID, c)
		}
	}
	for _, c := range diff.Rules {
		if len(c.Impacts) == 0 || c.Class == "" {
			t.Errorf("rule %s: %+v", c.ID, c)
		}
	}
}

func TestCompareClassIsBreakingIfAnyImpactIs(t *testing.T) {
	i := scalarOf("int")
	before := declared(structOf(fieldOf("a", i), fieldOf("b", i)))
	after := declared(structOf(fieldOf("a", i), fieldOf("c", i)))
	diff, err := schema.Compare(before, after)
	if err != nil {
		t.Fatal(err)
	}
	c := diff.Types[0]
	want := []schema.Impact{
		{Path: "$.fields.b", Class: schema.Breaking, Rule: schema.RuleFieldRemoved},
		{Path: "$.fields.c", Class: schema.Compatible, Rule: schema.RuleFieldAdded},
	}
	if c.Class != schema.Breaking || !slices.Equal(c.Impacts, want) {
		t.Fatalf("got %s %+v", c.Class, c.Impacts)
	}
}
