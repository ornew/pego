package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/schema"
)

const missingSchemaGrammar = `type P = { M missing; E error; O *missing; L []missing }
rule main = "(" semi ")" $$ => P{}
rule semi = ";" #missing`

func missingSchema(t *testing.T) *schema.Output {
	t.Helper()
	g, err := parseTest(missingSchemaGrammar)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Compile(g, compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	s, err := outputSchemaPrototype(p, "main", true)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// A missing token has its own shape kind, apart from error, wherever the type missing is written.
func TestOutputSchemaMissing(t *testing.T) {
	s := missingSchema(t)
	byName := map[string]schema.Shape{}
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
	var back schema.Output
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
