package grammar

import (
	"reflect"
	"strings"
	"testing"
)

func sampleGrammar() *Grammar {
	return &Grammar{
		Package: "calc",
		Statements: []Statement{
			&TypeDef{Name: "Number", Spec: &TerminalSpec{}},
			&TypeDef{Name: "Op", Spec: &StructSpec{Fields: []*Field{
				{Name: "Left", Type: &TypeRef{Name: "Node"}},
				{Name: "Args", Type: &ListType{Elem: &OptionalType{Elem: &TypeRef{Name: "Node"}}}},
			}}},
			&TypeDef{Name: "Node", Spec: &AliasSpec{Type: &UnionType{Types: []TypeExpr{
				&TypeRef{Name: "Op"}, &TypeRef{Name: "Number"},
			}}}},
			&RuleDef{
				Name: "expr",
				Type: &TypeRef{Name: "Node"},
				Expr: &Seq{Items: []Expr{
					&Capture{Name: "l", Expr: &Ref{Name: "term", Level: "add"}},
					&Repeat{Expr: &CharClass{Ranges: []CharRange{{Lo: 'a', Hi: 'z'}}, Negated: true}, Min: 1, Max: -1},
					&Optional{Expr: &Choice{Alts: []Expr{&Literal{Value: "+"}, &Any{}, &Cut{}}}},
					&Not{Expr: &And{Expr: &Atomic{Expr: &Discard{Expr: &Top{}}}}},
					&Predicate{Term: &Binary{Op: ">", L: &Call{Func: "len", Args: []Term{&CaptureRef{Name: "l"}}}, R: &VarRef{Name: "indent"}}},
					&Attributed{Expr: &Bottom{}, Attrs: []*Attribute{{Name: "error", Args: []*AttrArg{{Name: "message", Value: &Literal{Value: "x"}}}}}},
				}},
				Action: &Call{Func: "foldl", Args: []Term{
					&CaptureRef{Name: "l"},
					&IndexRef{Index: 2},
					&Lambda{Params: []string{"acc", "i"}, Body: &New{Type: "Op", Fields: []*FieldInit{
						{Name: "Left", Value: &Member{X: &CaptureRef{Name: "acc"}, Name: "Left"}},
					}}},
				}},
			},
			&RuleDef{Name: "p", Expr: &Pratt{
				Skip:     &Ref{Name: "ws"},
				Operands: []*PrattOperand{{Expr: &Ref{Name: "num"}}},
				Levels: []*PrattLevel{{Name: "add", Operators: []*PrattOperator{
					{Kind: Infix, Assoc: AssocLeft, Expr: &Literal{Value: "+"}, Action: &Unary{Op: "-", X: &IntLit{Value: 1}}},
				}}},
			}},
		},
	}
}

func TestJSONRoundTrip(t *testing.T) {
	g := sampleGrammar()
	data, err := MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalJSON(data)
	if err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, data)
	}
	if !reflect.DeepEqual(g, got) {
		t.Errorf("round trip mismatch\n%s", data)
	}
}

func TestJSONFormat(t *testing.T) {
	data, err := MarshalJSON(&Grammar{Statements: []Statement{
		&RuleDef{Name: "a", Expr: &Literal{Value: "x"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"@type": "RuleDef"`, `"@type": "Literal"`, `"value": "x"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing %s in\n%s", want, data)
		}
	}
	if strings.Contains(string(data), `"action"`) {
		t.Errorf("empty action should be omitted\n%s", data)
	}
}

func TestJSONErrors(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"unknown type", `{"statements":[{"@type":"Nope"}]}`, `unknown @type "Nope"`},
		{"wrong kind", `{"statements":[{"@type":"Literal","value":"x"}]}`, `Literal is not a Statement`},
		{"bad field", `{"statements":[{"@type":"RuleDef","name":1}]}`, `$.statements[0].name: expected a string`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := UnmarshalJSON([]byte(tc.in))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want error containing %q", err, tc.want)
			}
		})
	}
}

func TestJSONTrailingData(t *testing.T) {
	for _, suffix := range []string{"TRAILING", "{}", "null", "true"} {
		if _, err := UnmarshalJSON([]byte(`{"statements":[]}` + suffix)); err == nil {
			t.Errorf("accepted trailing %q", suffix)
		}
	}
}

func BenchmarkJSONIntake(b *testing.B) {
	data, err := MarshalJSON(sampleGrammar())
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := UnmarshalJSON(data); err != nil {
			b.Fatal(err)
		}
	}
}
