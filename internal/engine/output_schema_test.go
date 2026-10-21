package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/syntax"
)

func TestOutputSchemaPrototype(t *testing.T) {
	g, err := syntax.Parse(`type Item = { Value string; Next *Item; Count int; Flag bool; Values []node[string] }
type Unused = { Text string }
type Either ~ Item | node[string]
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
	if values := fields["Values"]; values.Kind != "list" || values.Element == nil || values.Element.Kind != "node" ||
		values.Element.Value == nil || values.Element.Value.Kind != "scalar" || values.Element.Value.Scalar != "string" {
		t.Fatalf("string node list field: %+v", values)
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
	if err := json.Unmarshal([]byte(`{"format":"pego-output-schema","version":3}`), &decoded); err == nil {
		t.Fatal("accepted future version")
	}
	if err := json.Unmarshal([]byte(`{"format":"pego-output-schema","version":1}`), &decoded); err == nil {
		t.Fatal("accepted version 1")
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

func TestOutputSchemaEmptyJoinRoundTrip(t *testing.T) {
	// A field cannot have a type without values, but a rule can: one that never succeeds.
	if message := compileError(t, `type Holder = { Never _|_ }
rule main = "a"`); !strings.Contains(message, "no values") {
		t.Fatalf("field without values: %s", message)
	}
	empty, err := schemaValue(tyBottom)
	if err != nil {
		t.Fatal(err)
	}
	schema := validInMemoryOutputSchema()
	schema.Types[1].Shape = empty
	if err := schema.Validate(); err != nil {
		t.Fatalf("valid empty join schema: %v", err)
	}
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	var decoded OutputSchema
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode valid empty join: %v", err)
	}
	if !reflect.DeepEqual(schema, decoded) || decoded.Types[1].Shape.Kind != "join" || len(decoded.Types[1].Shape.Alternatives) != 0 {
		t.Fatalf("empty join round trip changed schema: before=%+v after=%+v", schema, decoded)
	}
}

func TestOutputSchemaInferredCaptureAndDynamic(t *testing.T) {
	shape, err := schemaValue(nodeTy{
		value: tupleTy{[]ty{tyStringNode, tyTop, optTy{listTy{tyString}}}},
		caps: []capIndex{
			{name: "z", paths: [][]int{{1}}, t: tyTop},
			{name: "a", paths: [][]int{{2}, {0}}, t: optTy{listTy{tyString}}},
			{name: "s", side: true, t: tyStringNode},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if shape.Kind != "node" || shape.Value.Kind != "tuple" || len(shape.Value.Elements) != 3 || shape.Value.Elements[1].Kind != "top" ||
		len(shape.Captures) != 3 || shape.Captures[0].Name != "z" || shape.Captures[0].Shape.Kind != "top" ||
		!reflect.DeepEqual(shape.Captures[1].Paths, [][]int{{0}, {2}}) || !shape.Captures[2].Side || shape.Captures[2].Paths != nil {
		t.Fatalf("shape %+v", shape)
	}
	joinShape, err := schemaValue(joinTy{[]ty{tyString, tyInt}})
	if err != nil || len(joinShape.Alternatives) != 2 {
		t.Fatalf("join %+v %v", joinShape, err)
	}
}

func TestOutputSchemaRejectsUnresolvedShapes(t *testing.T) {
	p := &Program{types: map[string]grammar.TypeDecl{"Known": &grammar.TypeDef{Name: "Known", Type: &grammar.StructType{}}}}
	// The checker reports undefined names, which resolve to _ so that checking can go on; a
	// checked program has none. *_ is _, since _ includes nil.
	for _, expr := range []grammar.TypeExpr{
		&grammar.TypeRef{Name: "UnknownShape"},
		&grammar.ListType{Elem: &grammar.OptionalType{Elem: &grammar.TypeRef{Name: "UnknownShape"}}},
		&grammar.JoinType{Types: []grammar.TypeExpr{&grammar.TypeRef{Name: "Known"}, &grammar.TypeRef{Name: "UnknownShape"}}},
		&grammar.TypeGroup{},
	} {
		shape, err := p.schemaDeclared(expr)
		if err != nil || shape.Kind != "top" && (shape.Kind != "list" || shape.Element.Kind != "top") {
			data, _ := json.Marshal(shape)
			t.Errorf("unresolved declared shape %T: %s %v", expr, data, err)
		}
	}
	if message := compileError(t, "type T = { F UnknownShape }\nrule main = \"a\""); !strings.Contains(message, "UnknownShape") {
		t.Errorf("undefined field type: %s", message)
	}
	for _, inferred := range []ty{basicTy("other"), namedTy{name: "Known", kind: '?'}, (*listTy)(nil)} {
		if shape, err := schemaValue(inferred); err == nil {
			t.Errorf("accepted unresolved inferred shape %T: %+v", inferred, shape)
		}
	}
	if shape, err := schemaValue(tyBottom); err != nil || shape.Kind != "join" || len(shape.Alternatives) != 0 {
		t.Errorf("_|_ is not the empty join: %+v %v", shape, err)
	}
}

func TestOutputSchemaAnyAndValueFreeRepeat(t *testing.T) {
	g, err := syntax.Parse(`type Open ~ _
type Bag = { Values []_ }
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
	if got, ok := byID["declared/Open"]; ok {
		t.Fatalf("exported the alias Open as a type: %+v", got)
	}
	if got := byID["declared/Bag"].Shape.Fields[0].Shape; got.Kind != "list" || got.Element == nil || got.Element.Kind != "top" {
		t.Fatalf("any list field: %+v", got)
	}
	if got := byID["inferred/main"].Shape; got.Kind != "node" || got.Value.Kind != "list" || got.Value.Element.Kind != "node" || got.Value.Element.Value.Kind != "tuple" || len(got.Value.Element.Value.Elements) != 0 {
		data, _ := json.Marshal(got)
		t.Fatalf("value-free repeat: %s", data)
	}
}
