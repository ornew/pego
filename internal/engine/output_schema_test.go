package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

func TestOutputSchemaPrototype(t *testing.T) {
	g, err := syntax.Parse(`type Item struct { Value string; Next *Item; Count int; Flag bool; Values []Match }
type Unused terminal
type Either = Item | Match
rule main = item
rule item = x:"a" => Item{Value: text($x), Next: nil, Count: 0, Flag: false, Values: list($x)}
rule unused = "z"`)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Compile(g, Options{})
	if err != nil {
		t.Fatal(err)
	}
	s, err := p.OutputSchemaPrototype("main", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Rules) != 2 {
		t.Fatalf("rules: %+v", s.Rules)
	}
	for _, typ := range s.Types {
		if typ.Name == "Unused" || typ.Name == "Either" {
			t.Fatalf("unreachable type: %s", typ.Name)
		}
	}
	first, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		next, err := p.OutputSchemaPrototype("main", false)
		if err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(next)
		if err != nil || !bytes.Equal(first, got) {
			t.Fatalf("nondeterministic schema: %s", got)
		}
	}
	var decoded OutputSchema
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatal(err)
	}
	roundtrip, _ := json.Marshal(decoded)
	if !bytes.Equal(first, roundtrip) {
		t.Fatal("schema roundtrip")
	}
	if err := json.Unmarshal([]byte(`{"format":"pego-output-schema","version":2}`), &decoded); err == nil {
		t.Fatal("accepted future version")
	}
	all, err := p.OutputSchemaPrototype("main", true)
	if err != nil || len(all.Rules) != 3 {
		t.Fatalf("all rules: %+v %v", all, err)
	}
	p.typed = nil
	if _, err := p.OutputSchemaPrototype("main", false); !errors.Is(err, ErrSchemaUnavailable) {
		t.Fatal(err)
	}
}
func TestOutputSchemaInferredCaptureAndDynamic(t *testing.T) {
	shape, err := schemaInferred(recordTy{fields: map[string]ty{"z": tyAny, "a": optTy{listTy{tyString}}}})
	if err != nil {
		t.Fatal(err)
	}
	if shape.Kind != "cst" || shape.Fields[0].Name != "a" || shape.Fields[1].Shape.Kind != "any" || shape.Fields[1].Origin != "capture" {
		t.Fatalf("shape %+v", shape)
	}
	unionShape, err := schemaInferred(unionTy{[]ty{tyString, tyInt}})
	if err != nil || len(unionShape.Alternatives) != 2 {
		t.Fatalf("union %+v %v", unionShape, err)
	}
}
