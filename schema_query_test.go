package pego_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/ornew/pego"
)

func TestPublicOutputSchema(t *testing.T) {
	p, err := pego.CompileSource(`type Item = { Value string }
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
	if _, err := pego.CompileStructuralQuery(pego.QueryPattern{Type: "Missing"}, s); err == nil {
		t.Fatal("accepted a type absent from the supplied schema")
	}
	if s.Format != "pego-output-schema" || s.Version != 1 || s.Start != "main" || len(s.Rules) != 2 {
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
	var decoded pego.OutputSchema
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatal(err)
	}
	if got, _ := json.Marshal(&decoded); !bytes.Equal(first, got) {
		t.Fatalf("round trip changed schema: %s", got)
	}
	if err := json.Unmarshal([]byte(`{"format":"pego-output-schema","version":2}`), &decoded); err == nil {
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
	if _, err := loaded.OutputSchema(); !errors.Is(err, pego.ErrSchemaUnavailable) {
		t.Fatalf("loaded parser schema: %v", err)
	}
}

func TestPublicStructuralQuerySharedEdgesAndLimits(t *testing.T) {
	p, err := pego.CompileSource(`rule main = x:"a" "b"`, "main")
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
	q, err := pego.CompileStructuralQuery(pego.QueryPattern{Type: "Match", Capture: "token"}, s)
	if err != nil {
		t.Fatal(err)
	}
	got, err := q.Matches(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("matches: %+v", got)
	}
	want := [][]pego.QueryEdge{{{Kind: "child", Index: 0}}, {{Kind: "child", Index: 1}}, {{Kind: "field", Name: "x"}}}
	for i, match := range got {
		if len(match.Captures) != 1 || match.Captures[0].Parent != root || !reflect.DeepEqual(match.Captures[0].Path, want[i]) {
			t.Fatalf("match %d: %+v", i, match)
		}
	}
	if got[0].Captures[0].Value != got[2].Captures[0].Value {
		t.Fatal("shared node was not reported on both incoming edges")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if partial, err := q.Matches(root, pego.WithQueryContext(ctx)); partial != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled query: %+v, %v", partial, err)
	}
	if partial, err := q.Matches(root, pego.WithQueryWorkLimits(pego.WorkLimits{MaxSteps: 1})); partial != nil || !errors.Is(err, pego.ErrWorkLimit) {
		t.Fatalf("limited query: %+v, %v", partial, err)
	}
	root.Children = append(root.Children, root)
	if partial, err := q.Matches(root); partial != nil || !errors.Is(err, pego.ErrQueryCycle) {
		t.Fatalf("cyclic query: %+v, %v", partial, err)
	}
}

func TestPublicSchemaQueryMatchesUncapturedCSTChild(t *testing.T) {
	p, err := pego.CompileSource(`rule main = "a" "b"`, "main")
	if err != nil {
		t.Fatal(err)
	}
	s, err := p.OutputSchema()
	if err != nil {
		t.Fatal(err)
	}
	foundMatch := false
	for _, typ := range s.Types {
		if typ.ID == "cst/Match" {
			foundMatch = true
			break
		}
	}
	if !foundMatch {
		t.Fatal("schema omits Match behind a generic CST child")
	}
	q, err := pego.CompileStructuralQuery(pego.QueryPattern{Type: "Match", Capture: "token"}, s)
	if err != nil {
		t.Fatal(err)
	}
	root, err := p.Parse("ab")
	if err != nil {
		t.Fatal(err)
	}
	matches, err := q.Matches(root)
	if err != nil || len(matches) != 2 {
		t.Fatalf("uncaptured children: %+v, %v", matches, err)
	}
}

func TestPublicQueryOptionalScalarFieldValidation(t *testing.T) {
	p, err := pego.CompileSource(`type Item = { Value *string }
rule main = "a" => Item{Value: nil}`, "main")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := p.OutputSchema()
	if err != nil {
		t.Fatal(err)
	}
	pattern := func(category string) pego.QueryPattern {
		return pego.QueryPattern{Type: "Item", Fields: []pego.QueryFieldPattern{{
			Name: "Value", Pattern: pego.QueryPattern{Category: category, Capture: "value"},
		}}}
	}
	if _, err := pego.CompileStructuralQuery(pattern("bool"), schema); err == nil {
		t.Fatal("accepted bool for optional string field")
	}
	stringQuery, err := pego.CompileStructuralQuery(pattern("string"), schema)
	if err != nil {
		t.Fatal(err)
	}
	nilQuery, err := pego.CompileStructuralQuery(pattern("nil"), schema)
	if err != nil {
		t.Fatal(err)
	}
	root, err := p.Parse("a")
	if err != nil {
		t.Fatal(err)
	}
	if len(root.Fields) != 1 || root.Fields[0].Name != "Value" {
		t.Fatalf("unexpected result fields: %+v", root.Fields)
	}
	check := func(q *pego.StructuralQuery, want int) {
		t.Helper()
		matches, err := q.Matches(root)
		if err != nil || len(matches) != want {
			t.Fatalf("matches: %+v, %v; want %d", matches, err, want)
		}
	}
	check(nilQuery, 1)
	check(stringQuery, 0)
	root.Fields[0].Value = "ok"
	check(stringQuery, 1)
	check(nilQuery, 0)
	root.Fields[0].Value = false
	check(stringQuery, 0)
	check(nilQuery, 0)
}

func TestPublicQueryScalarAliasFieldValidation(t *testing.T) {
	p, err := pego.CompileSource(`type Label ~ string
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
	pattern := func(category string) pego.QueryPattern {
		return pego.QueryPattern{Type: "Item", Fields: []pego.QueryFieldPattern{{
			Name: "Value", Pattern: pego.QueryPattern{Category: category, Capture: "value"},
		}}}
	}
	for _, category := range []string{"bool", "nil", "node", "list"} {
		if _, err := pego.CompileStructuralQuery(pattern(category), schema); err == nil {
			t.Errorf("accepted impossible scalar-alias category %q", category)
		}
	}
	query, err := pego.CompileStructuralQuery(pattern("string"), schema)
	if err != nil {
		t.Fatal(err)
	}
	root, err := p.Parse("a")
	if err != nil {
		t.Fatal(err)
	}
	matches, err := query.Matches(root)
	if err != nil || len(matches) != 1 || len(matches[0].Captures) != 1 || matches[0].Captures[0].Value != "a" {
		t.Fatalf("scalar alias match: %+v, %v", matches, err)
	}
	root.Fields[0].Value = false
	matches, err = query.Matches(root)
	if err != nil || len(matches) != 0 {
		t.Fatalf("mutated scalar alias match: %+v, %v", matches, err)
	}
}

func TestPublicPreparedQuerySchema(t *testing.T) {
	p, err := pego.CompileSource(`rule main = x:"a" "b"`, "main")
	if err != nil {
		t.Fatal(err)
	}
	s, err := p.OutputSchema()
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := pego.PrepareQuerySchema(s)
	if err != nil {
		t.Fatal(err)
	}
	root, err := p.Parse("ab")
	if err != nil {
		t.Fatal(err)
	}
	q, err := prepared.CompileStructuralQuery(pego.QueryPattern{Type: "Match", Capture: "token"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := q.Matches(root); err != nil || len(got) != 3 {
		t.Fatalf("matches: %+v, %v", got, err)
	}
	if _, err := prepared.CompileStructuralQuery(pego.QueryPattern{Rule: "missing"}); err == nil {
		t.Fatal("accepted an unknown rule")
	}
	s.Rules = nil
	if _, err := prepared.CompileStructuralQuery(pego.QueryPattern{Rule: "main"}); err != nil {
		t.Fatalf("prepared schema followed its changed source: %v", err)
	}
	if _, err := pego.PrepareQuerySchema(s); err == nil {
		t.Fatal("prepared an invalid schema")
	}
	var none *pego.QuerySchema
	if _, err := none.CompileStructuralQuery(pego.QueryPattern{Rule: "missing"}); err != nil {
		t.Fatalf("nil QuerySchema checked the rule: %v", err)
	}
	if _, err := new(pego.QuerySchema).CompileStructuralQuery(pego.QueryPattern{Rule: "missing"}); err != nil {
		t.Fatalf("zero QuerySchema checked the rule: %v", err)
	}
}
