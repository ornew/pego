package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/ornew/pego/grammar"
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
	byID := make(map[string]SchemaType, len(s.Types))
	for _, typ := range s.Types {
		byID[typ.ID] = typ
	}
	item := byID["declared/Item"]
	if item.Origin != "declared" || item.Shape.Kind != "struct" || len(item.Shape.Fields) != 5 {
		t.Fatalf("declared item shape: %+v", item)
	}
	fields := make(map[string]SchemaShape, len(item.Shape.Fields))
	for _, field := range item.Shape.Fields {
		if field.Origin != "declared" {
			t.Fatalf("declared field origin: %+v", field)
		}
		fields[field.Name] = field.Shape
	}
	if fields["Value"].Kind != "scalar" || fields["Value"].Scalar != "string" ||
		fields["Count"].Kind != "scalar" || fields["Count"].Scalar != "int" ||
		fields["Flag"].Kind != "scalar" || fields["Flag"].Scalar != "bool" {
		t.Fatalf("scalar fields: %+v", fields)
	}
	if next := fields["Next"]; next.Kind != "optional" || next.Element == nil || next.Element.Kind != "ref" || next.Element.Ref != "declared/Item" {
		t.Fatalf("recursive optional field: %+v", next)
	}
	if values := fields["Values"]; values.Kind != "list" || values.Element == nil || values.Element.Kind != "ref" || values.Element.Ref != "cst/Match" {
		t.Fatalf("CST list field: %+v", values)
	}
	if inferred := byID["inferred/item"].Shape; inferred.Kind != "ref" || inferred.Ref != "declared/Item" {
		t.Fatalf("inferred rule identity: %+v", byID["inferred/item"])
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
	if !reflect.DeepEqual(s, &decoded) {
		t.Fatalf("schema semantics changed after round trip: before=%+v after=%+v", s, &decoded)
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
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		if _, err := p.ParseWith("main", "a", ParseOptions{Backend: backend}); err != nil {
			t.Fatalf("%s parse: %v", backend, err)
		}
		byBackend, err := p.OutputSchemaPrototype("main", false)
		if err != nil {
			t.Fatalf("%s schema: %v", backend, err)
		}
		if !reflect.DeepEqual(s, byBackend) {
			t.Fatalf("%s changed schema after parse", backend)
		}
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

func TestOutputSchemaRejectsUnresolvedShapes(t *testing.T) {
	p := &Program{types: map[string]grammar.TypeSpec{"Known": &grammar.TerminalSpec{}}}
	for _, expr := range []grammar.TypeExpr{
		&grammar.TypeRef{Name: "Missing"},
		&grammar.ListType{Elem: &grammar.OptionalType{Elem: &grammar.TypeRef{Name: "Missing"}}},
		&grammar.UnionType{Types: []grammar.TypeExpr{&grammar.TypeRef{Name: "Known"}, &grammar.TypeRef{Name: "Missing"}}},
		&grammar.TypeGroup{},
	} {
		if shape, err := p.schemaDeclared(expr); err == nil {
			t.Errorf("accepted unresolved declared shape %T: %+v", expr, shape)
		}
	}
	for _, inferred := range []ty{tyNever, basicTy("unknown"), namedTy{name: "Known", kind: '?'}, (*listTy)(nil)} {
		if shape, err := schemaInferred(inferred); err == nil {
			t.Errorf("accepted unresolved inferred shape %T: %+v", inferred, shape)
		}
	}
}

func TestOutputSchemaAnyAndValueFreeRepeat(t *testing.T) {
	g, err := syntax.Parse(`type Open = any
type Bag struct { Values []any }
rule main = (-"a")*`)
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
	byID := make(map[string]SchemaType, len(s.Types))
	for _, typ := range s.Types {
		byID[typ.ID] = typ
	}
	if got := byID["declared/Open"].Shape; got.Kind != "any" {
		t.Fatalf("any alias: %+v", got)
	}
	if got := byID["declared/Bag"].Shape.Fields[0].Shape; got.Kind != "list" || got.Element == nil || got.Element.Kind != "any" {
		t.Fatalf("any list field: %+v", got)
	}
	if got := byID["inferred/main"].Shape; got.Kind != "list" || got.Element == nil || got.Element.Kind != "nil" {
		t.Fatalf("value-free repeat: %+v", got)
	}
}
