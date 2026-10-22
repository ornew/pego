package engine

import (
	"encoding/json"
	"strings"
	"testing"
)

const missingSchemaGrammar = `type P = { M missing; E error; O *missing; L []missing }
rule main = "(" semi ")" $$ => P{}
rule semi = ";" #missing`

func missingSchema(t *testing.T) *OutputSchema {
	t.Helper()
	g, err := parseTest(missingSchemaGrammar)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Compile(g, Options{})
	if err != nil {
		t.Fatal(err)
	}
	s, err := p.OutputSchemaPrototype("main", true)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// A missing token has its own shape kind, apart from error, wherever the type missing is written.
func TestOutputSchemaMissing(t *testing.T) {
	s := missingSchema(t)
	byName := map[string]SchemaShape{}
	for _, typ := range s.Types {
		if typ.ID == "declared/P" {
			for _, f := range typ.Shape.Fields {
				byName[f.Name] = f.Shape
			}
		}
	}
	if got := byName["M"]; got.Kind != "missing" {
		t.Errorf("M missing: %+v", got)
	}
	if got := byName["E"]; got.Kind != "error" {
		t.Errorf("E error: %+v", got)
	}
	if got := byName["O"]; got.Kind != "optional" || got.Element.Kind != "missing" {
		t.Errorf("O *missing: %+v", got)
	}
	if got := byName["L"]; got.Kind != "list" || got.Element.Kind != "missing" {
		t.Errorf("L []missing: %+v", got)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `{"name":"M","shape":{"kind":"missing"}}`) {
		t.Errorf("JSON: %s", data)
	}
	var back OutputSchema
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if again, err := json.Marshal(back); err != nil || string(again) != string(data) {
		t.Errorf("round trip: %v\n%s\n%s", err, again, data)
	}
}

// The shape missing has no members.
func TestOutputSchemaMissingRejectsMembers(t *testing.T) {
	s := missingSchema(t)
	for i := range s.Types {
		for j := range s.Types[i].Shape.Fields {
			if f := &s.Types[i].Shape.Fields[j]; f.Name == "M" {
				f.Shape.Scalar = "int"
			}
		}
	}
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "unexpected output schema members on missing shape") {
		t.Errorf("members on missing: %v", err)
	}
}

// Patterns of errors apply to the values of the shape missing, and others do not.
func TestStructuralQueryMissingShape(t *testing.T) {
	s := missingSchema(t)
	field := func(pattern QueryPattern) QueryPattern {
		return QueryPattern{Type: "P", Fields: []QueryFieldPattern{{Name: "M", Pattern: pattern}}}
	}
	for _, good := range []QueryPattern{
		field(QueryPattern{Category: "error"}),
		field(QueryPattern{Type: "error"}),
		field(QueryPattern{Category: "error", Fields: []QueryFieldPattern{{Name: "message", Pattern: QueryPattern{Category: "string"}}}}),
	} {
		if _, err := CompileStructuralQuery(good, s); err != nil {
			t.Errorf("rejected %+v: %v", good, err)
		}
	}
	for _, bad := range []QueryPattern{
		field(QueryPattern{Category: "string"}),
		field(QueryPattern{Category: "nil"}),
		field(QueryPattern{Type: "P"}),
		field(QueryPattern{Category: "error", Fields: []QueryFieldPattern{{Name: "other"}}}),
	} {
		if _, err := CompileStructuralQuery(bad, s); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	_, err := CompileStructuralQuery(field(QueryPattern{Category: "string"}), s)
	if err == nil || !strings.Contains(err.Error(), "value is missing, not string") {
		t.Errorf("reason: %v", err)
	}
}
