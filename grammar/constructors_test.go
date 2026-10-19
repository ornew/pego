package grammar

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestConstructorJSONRoundTrip(t *testing.T) {
	g := &Grammar{File: File{Decls: []Decl{
		&RuleDef{Name: "list", Expr: &Literal{Value: "x"}, Action: &ListConstructor{
			Elem: &ListType{Elem: &TypeRef{Name: "Foo"}},
			Items: []Term{&ListConstructor{Elem: &TypeRef{Name: "Foo"}, Items: []Term{
				&New{Type: "Int", Fields: []*FieldInit{{Name: "Text", Value: &StringLit{Value: "0"}}}},
			}}},
		}},
		&RuleDef{Name: "args", Expr: &Literal{Value: "x"}, Action: &New{Type: "Int", Fields: []*FieldInit{
			{Name: "Text", Value: &StringLit{Value: "a"}}, {Name: "Rest", Value: &StringLit{Value: "b"}},
		}}},
		&RuleDef{Name: "empty", Expr: &Literal{Value: "x"}, Action: &ListConstructor{Elem: &TypeRef{Name: "Foo"}, Items: []Term{}}},
	}}}
	data, err := MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"@type": "ListConstructor"`, `"@type": "New"`, `"elem"`, `"items": []`, `"fields"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("JSON lacks %q:\n%s", want, data)
		}
	}
	got, err := DecodeJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g, got) {
		t.Fatalf("constructor JSON round trip mismatch\n%s", data)
	}
}

func TestConstructorValidationChildrenAndCycles(t *testing.T) {
	listGrammar := func(term Term) *Grammar {
		return termGrammar(term)
	}
	for _, tc := range []struct {
		name string
		term Term
		path string
	}{
		{"missing list element type", &ListConstructor{Items: []Term{}}, "$.file.decls[0].action.elem"},
		{"typed nil list item", &ListConstructor{Elem: &TypeRef{Name: "Foo"}, Items: []Term{(*StringLit)(nil)}}, "$.file.decls[0].action.items[0]"},
		{"typed nil field value", &New{Type: "Int", Fields: []*FieldInit{{Name: "Text", Value: (*StringLit)(nil)}}}, "$.file.decls[0].action.fields[0].value"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := listGrammar(tc.term)
			assertValidationError(t, Validate(g), tc.path)
			if _, err := MarshalJSON(g); err == nil && tc.name != "missing list element type" {
				t.Fatal("MarshalJSON accepted an invalid constructor AST")
			}
		})
	}

	cyclic := &ListConstructor{Elem: &TypeRef{Name: "Foo"}}
	cyclic.Items = []Term{cyclic}
	assertValidationError(t, Validate(listGrammar(cyclic)), "$.file.decls[0].action.items[0]")
}

func TestDecodeJSONConstructorRequiresListElementType(t *testing.T) {
	input := `{"file":{"decls":[{"@type":"RuleDef","name":"p","expr":{"@type":"Literal","value":"x"},"action":{"@type":"ListConstructor","items":[]}}]}}`
	got, err := DecodeJSON([]byte(input))
	if got != nil || err == nil {
		t.Fatalf("DecodeJSON accepted missing list element type: %#v, %v", got, err)
	}
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Path != "$.file.decls[0].action.elem" {
		t.Fatalf("got %v; want validation error at $.file.decls[0].action.elem", err)
	}
}

func TestDecodeJSONRejectsTerminalNodes(t *testing.T) {
	for _, input := range []string{
		`{"file":{"decls":[{"@type":"TypeDef","name":"Num","type":{"@type":"TerminalSpec"}}]}}`,
		`{"file":{"decls":[{"@type":"RuleDef","name":"p","expr":{"@type":"Literal","value":"x"},"action":{"@type":"TerminalConstructor","type":"Num","args":[{"@type":"StringLit","value":"x"}]}}]}}`,
	} {
		if g, err := DecodeJSON([]byte(input)); g != nil || err == nil {
			t.Errorf("DecodeJSON accepted %s: %#v", input, g)
		}
	}
}
