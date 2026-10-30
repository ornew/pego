package query_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/internal/value"
	"github.com/ornew/pego/query"
	"github.com/ornew/pego/schema"
)

func TestPublicOutputSchema(t *testing.T) {
	p, err := compileTestSource(`type Item = { Value string }
rule main = item
rule item = x:"a" => Item{Value: text($x)}
rule unused = "z"`, "main")
	if err != nil {
		t.Fatal(err)
	}
	s, err := p.OutputSchema()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := query.Compile(query.Pattern{Type: "Missing"}, s); err == nil {
		t.Fatal("accepted a type absent from the supplied schema")
	}
	if s.Format != "pego-output-schema" || s.Version != 2 || s.Start != "main" || len(s.Rules) != 2 {
		t.Fatalf("default schema: %+v", s)
	}
	first, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		other, err := p.OutputSchema()
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(other)
		if err != nil || !bytes.Equal(first, encoded) {
			t.Fatalf("unstable schema: %s, %v", encoded, err)
		}
	}
	all, err := p.OutputSchema(pego.WithAllSchemaRules())
	if err != nil || len(all.Rules) != 3 {
		t.Fatalf("all rules: %+v, %v", all, err)
	}
	var decoded schema.Output
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatal(err)
	}
	if got, _ := json.Marshal(&decoded); !bytes.Equal(first, got) {
		t.Fatalf("round trip changed schema: %s", got)
	}
	if err := json.Unmarshal([]byte(`{"format":"pego-output-schema","version":3}`), &decoded); err == nil {
		t.Fatal("accepted a future schema version")
	}
	data, err := p.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := pego.LoadParser(data)
	if err != nil {
		t.Fatal(err)
	}
	// A compiled grammar holds no schema in any mode: the schema comes from the grammar source.
	if _, err := loaded.OutputSchema(); !errors.Is(err, pego.ErrSchemaUnavailable) || !strings.Contains(err.Error(), "grammar source") {
		t.Fatalf("loaded parser schema: %v", err)
	}
	for _, mode := range []pego.CompiledMode{pego.CompiledParse, pego.CompiledRecognize, pego.CompiledDocument} {
		data, err := p.Marshal(mode)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := pego.LoadParser(data)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := loaded.OutputSchema(); !errors.Is(err, pego.ErrSchemaUnavailable) || !strings.Contains(err.Error(), "grammar source") {
			t.Fatalf("%s mode: loaded parser schema: %v", mode, err)
		}
	}
	// The source keeps its schema after saving.
	again, err := p.OutputSchema()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := marshalJSON(t, again), marshalJSON(t, s); !bytes.Equal(got, want) {
		t.Fatalf("source schema differs after saving:\n%s\n%s", got, want)
	}
}

func TestPublicStructuralQuerySharedEdgesAndLimits(t *testing.T) {
	p, err := compileTestSource(`rule main = x:"a" "b" => list($x, $x)`, "main")
	if err != nil {
		t.Fatal(err)
	}
	s, err := p.OutputSchema()
	if err != nil {
		t.Fatal(err)
	}
	root, err := p.Parse("ab")
	if err != nil {
		t.Fatal(err)
	}
	token := query.Pattern{Category: "node", Value: &query.Pattern{Category: "string"}, Capture: "token"}
	q, err := query.Compile(token, s)
	if err != nil {
		t.Fatal(err)
	}
	got, err := q.Matches(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("matches: %+v", got)
	}
	want := [][]query.Edge{{{Kind: "element", Index: 0}}, {{Kind: "element", Index: 1}}}
	for i, match := range got {
		if len(match.Captures) != 1 || match.Captures[0].Parent != root || !reflect.DeepEqual(match.Captures[0].Path, want[i]) {
			t.Fatalf("match %d: %+v", i, match)
		}
	}
	if got[0].Captures[0].Value != got[1].Captures[0].Value {
		t.Fatal("shared node was not reported on both incoming edges")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if partial, err := q.Matches(root, query.WithContext(ctx)); partial != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled query: %+v, %v", partial, err)
	}
	if partial, err := q.Matches(root, query.WithLimits(pego.Limits{MaxWork: 1})); partial != nil || !errors.Is(err, pego.ErrLimit) {
		t.Fatalf("limited query: %+v, %v", partial, err)
	}
	var kids []*pego.Node
	for _, c := range root.Elems() {
		kids = append(kids, c)
	}
	value.SetNodeElems(root, append(kids, root))
	if partial, err := q.Matches(root); partial != nil || !errors.Is(err, query.ErrCycle) {
		t.Fatalf("cyclic query: %+v, %v", partial, err)
	}
}

func TestPublicSchemaQueryMatchesUncapturedElements(t *testing.T) {
	p, err := compileTestSource(`rule main = "a" "b"`, "main")
	if err != nil {
		t.Fatal(err)
	}
	s, err := p.OutputSchema()
	if err != nil {
		t.Fatal(err)
	}
	var main *schema.Shape
	for i := range s.Types {
		if s.Types[i].ID == "inferred/main" {
			main = &s.Types[i].Shape
		}
	}
	if main == nil || main.Kind != "node" || main.Value.Kind != "tuple" || len(main.Value.Elements) != 2 || main.Value.Elements[1].Kind != "node" {
		t.Fatalf("schema omits the elements of main: %+v", s.Types)
	}
	q, err := query.Compile(query.Pattern{Category: "node", Value: &query.Pattern{Category: "string"}, Capture: "token"}, s)
	if err != nil {
		t.Fatal(err)
	}
	root, err := p.Parse("ab")
	if err != nil {
		t.Fatal(err)
	}
	matches, err := q.Matches(root)
	if err != nil || len(matches) != 2 {
		t.Fatalf("uncaptured elements: %+v, %v", matches, err)
	}
}

func TestPublicQueryOptionalScalarFieldValidation(t *testing.T) {
	p, err := compileTestSource(`type Item = { Value *string }
rule main = "a" => Item{Value: nil}`, "main")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := p.OutputSchema()
	if err != nil {
		t.Fatal(err)
	}
	pattern := func(category string) query.Pattern {
		return query.Pattern{Type: "Item", Fields: []query.FieldPattern{{
			Name: "Value", Pattern: query.Pattern{Category: category, Capture: "value"},
		}}}
	}
	if _, err := query.Compile(pattern("bool"), schema); err == nil {
		t.Fatal("accepted bool for optional string field")
	}
	stringQuery, err := query.Compile(pattern("string"), schema)
	if err != nil {
		t.Fatal(err)
	}
	nilQuery, err := query.Compile(pattern("nil"), schema)
	if err != nil {
		t.Fatal(err)
	}
	root, err := p.Parse("a")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for name := range root.Fields() {
		names = append(names, name)
	}
	if len(names) != 1 || names[0] != "Value" {
		t.Fatalf("unexpected result fields: %v", names)
	}
	check := func(q *query.Query, want int) {
		t.Helper()
		matches, err := q.Matches(root)
		if err != nil || len(matches) != want {
			t.Fatalf("matches: %+v, %v; want %d", matches, err, want)
		}
	}
	check(nilQuery, 1)
	check(stringQuery, 0)
	value.SetNodeField(root, "Value", "ok")
	check(stringQuery, 1)
	check(nilQuery, 0)
	value.SetNodeField(root, "Value", false)
	check(stringQuery, 0)
	check(nilQuery, 0)
}

func TestPublicQueryScalarAliasFieldValidation(t *testing.T) {
	p, err := compileTestSource(`type Label ~ string
type DisplayLabel ~ Label
type Item = { Value DisplayLabel }
rule main = x:"a" => Item{Value: text($x)}`, "main")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := p.OutputSchema()
	if err != nil {
		t.Fatal(err)
	}
	pattern := func(category string) query.Pattern {
		return query.Pattern{Type: "Item", Fields: []query.FieldPattern{{
			Name: "Value", Pattern: query.Pattern{Category: category, Capture: "value"},
		}}}
	}
	for _, category := range []string{"bool", "nil", "node", "list"} {
		if _, err := query.Compile(pattern(category), schema); err == nil {
			t.Errorf("accepted impossible scalar-alias category %q", category)
		}
	}
	q, err := query.Compile(pattern("string"), schema)
	if err != nil {
		t.Fatal(err)
	}
	root, err := p.Parse("a")
	if err != nil {
		t.Fatal(err)
	}
	matches, err := q.Matches(root)
	if err != nil || len(matches) != 1 || len(matches[0].Captures) != 1 || matches[0].Captures[0].Value != "a" {
		t.Fatalf("scalar alias match: %+v, %v", matches, err)
	}
	value.SetNodeField(root, "Value", false)
	matches, err = q.Matches(root)
	if err != nil || len(matches) != 0 {
		t.Fatalf("mutated scalar alias match: %+v, %v", matches, err)
	}
}

func TestPublicPreparedQuerySchema(t *testing.T) {
	p, err := compileTestSource(`type Item = { Value node[string] }
rule main = x:"a" "b" => Item{Value: $x}`, "main")
	if err != nil {
		t.Fatal(err)
	}
	s, err := p.OutputSchema()
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := query.Prepare(s)
	if err != nil {
		t.Fatal(err)
	}
	root, err := p.Parse("ab")
	if err != nil {
		t.Fatal(err)
	}
	q, err := prepared.Compile(query.Pattern{Category: "node", Value: &query.Pattern{Category: "string"}, Capture: "token"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := q.Matches(root); err != nil || len(got) != 1 {
		t.Fatalf("matches: %+v, %v", got, err)
	}
	if _, err := prepared.Compile(query.Pattern{Type: "Missing"}); err == nil {
		t.Fatal("accepted an unknown type")
	}
	// Rule constraints need a schema with source information.
	if _, err := prepared.Compile(query.Pattern{Rule: "main"}); err == nil {
		t.Fatal("accepted a rule constraint without source information")
	}
	s.Types = nil
	if _, err := prepared.Compile(query.Pattern{Type: "Item"}); err != nil {
		t.Fatalf("prepared schema followed its changed source: %v", err)
	}
	if _, err := query.Prepare(s); err == nil {
		t.Fatal("prepared an invalid schema")
	}
	var none *query.Schema
	if _, err := none.Compile(query.Pattern{Rule: "missing"}); err != nil {
		t.Fatalf("nil Schema checked the rule: %v", err)
	}
	if _, err := new(query.Schema).Compile(query.Pattern{Rule: "missing"}); err != nil {
		t.Fatalf("zero Schema checked the rule: %v", err)
	}
}

func marshalJSON(t *testing.T, s *schema.Output) []byte {
	t.Helper()
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
