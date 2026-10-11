package grammar

import (
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strconv"
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

func TestDecodeJSONStrictAndPermissiveCompatibility(t *testing.T) {
	unknown := []byte(`{"statements":[],"extension":true}`)
	if _, err := DecodeJSON(unknown); err == nil {
		t.Fatal("DecodeJSON accepted an unknown root field")
	} else {
		assertJSONDecodeError(t, err, "$.extension", strings.Index(string(unknown), `"extension"`), "unknown field")
	}
	if _, err := UnmarshalJSON(unknown); err != nil {
		t.Fatalf("legacy UnmarshalJSON rejected an unknown field: %v", err)
	}

	dup := []byte(`{"package":"first","package":"second","statements":[]}`)
	if _, err := DecodeJSON(dup); err == nil {
		t.Fatal("DecodeJSON accepted duplicate keys")
	} else {
		assertJSONDecodeError(t, err, "$.package", strings.LastIndex(string(dup), `"package"`), "duplicate object key")
	}
	g, err := UnmarshalJSON(dup)
	if err != nil || g.Package != "second" {
		t.Fatalf("legacy duplicate last-value-wins behavior changed: grammar=%#v err=%v", g, err)
	}
}

func TestDecodeJSONUnknownFieldsAtASTDepth(t *testing.T) {
	cases := []struct {
		name, input, path, key string
	}{
		{"statement", `{"statements":[{"@type":"RuleDef","name":"r","expr":{"@type":"Literal","value":"x"},"nam":"typo"}]}`, "$.statements[0].nam", `"nam"`},
		{"expression", `{"statements":[{"@type":"RuleDef","name":"r","expr":{"@type":"Literal","value":"x","valu":"typo"}}]}`, "$.statements[0].expr.valu", `"valu"`},
		{"term", `{"statements":[{"@type":"RuleDef","name":"r","expr":{"@type":"Literal","value":"x"},"action":{"@type":"Call","func":"f","args":[],"extra":1}}]}`, "$.statements[0].action.extra", `"extra"`},
		{"type", `{"statements":[{"@type":"TypeDef","name":"T","spec":{"@type":"AliasSpec","type":{"@type":"TypeRef","name":"X","Name":"bad"}}}]}`, `$.statements[0].spec.type.Name`, `"Name"`},
		{"unusual key", `{"statements":[],"bad-key":1}`, `$["bad-key"]`, `"bad-key"`},
		{"unicode key", `{"statements":[],"é":1}`, `$["é"]`, `"é"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeJSON([]byte(tc.input))
			if err == nil {
				t.Fatal("accepted unknown field")
			}
			assertJSONDecodeError(t, err, tc.path, strings.Index(tc.input, tc.key), "unknown field")
		})
	}
}

func TestDecodeJSONDuplicateDecodedKeysAndOffset(t *testing.T) {
	for _, input := range []string{
		`{"statements":[],"package":"x","package":"x"}`,
		`{"statements":[],"package":"x","pack\u0061ge":"y"}`,
		`{"statements":[{"@type":"RuleDef","name":"r","expr":{"@type":"Literal","value":"x","value":"y"}}]}`,
		`{"statements":[{"@type":"RuleDef","name":"r","expr":{"@type":"Literal","value":"x","@type":"Literal"}}]}`,
	} {
		_, err := DecodeJSON([]byte(input))
		if err == nil {
			t.Fatalf("accepted duplicate key in %s", input)
		}
		var got *JSONDecodeError
		if !errors.As(err, &got) || !strings.Contains(got.Msg, "duplicate object key") {
			t.Fatalf("got %T %v, want duplicate JSONDecodeError", err, err)
		}
		if got.Offset < 0 || got.Offset >= len(input) || input[got.Offset] != '"' {
			t.Fatalf("duplicate offset %d does not point to second key token in %s", got.Offset, input)
		}
	}
	first := `{"statements":[{"@type":"RuleDef","name":"r","expr":{"@type":"Literal","value":"x","value":"y"}}],"package":"a","package":"b"}`
	_, err := DecodeJSON([]byte(first))
	var firstErr *JSONDecodeError
	if !errors.As(err, &firstErr) || firstErr.Path != "$.statements[0].expr.value" {
		t.Fatalf("did not report first duplicate in input order: %v", err)
	}
	escaped := `{"statements":[],"package":"x","pack\u0061ge":"y"}`
	_, err = DecodeJSON([]byte(escaped))
	var got *JSONDecodeError
	if !errors.As(err, &got) || got.Offset != strings.Index(escaped, `"pack\u0061ge"`) {
		t.Fatalf("escaped duplicate offset: err=%v, offset=%d", err, got.Offset)
	}
}

func TestDecodeJSONInterfaceRegistryFamilyAndLateType(t *testing.T) {
	valid := []byte(`{"statements":[{"name":"r","expr":{"value":"x","@type":"Literal"},"@type":"RuleDef"}]}`)
	if _, err := DecodeJSON(valid); err != nil {
		t.Fatalf("@type after fields was not decoded: %v", err)
	}
	wrong := []byte(`{"statements":[{"name":"r","expr":{"@type":"Literal","value":"x"},"@type":"Ref"}]}`)
	_, err := DecodeJSON(wrong)
	if err == nil {
		t.Fatal("accepted registered concrete type from the wrong interface family")
	}
	assertJSONDecodeError(t, err, `$.statements[0]["@type"]`, strings.Index(string(wrong), `"@type":"Ref"`), "Ref is not a Statement")

	wrongWithFieldsFirst := []byte(`{"statements":[{"name":"r","expr":{"@type":"Literal","value":"x"},"@type":"Ref"}]}`)
	_, err = DecodeJSON(wrongWithFieldsFirst)
	if err == nil || !strings.Contains(err.Error(), "Ref is not a Statement") {
		t.Fatalf("late cross-family @type: %v", err)
	}
}

func TestDecodeJSONSyntaxTrailingAndValidation(t *testing.T) {
	for _, input := range []string{`{"statements":[]} {}`, `{"statements":[]}x`, `{"statements":[`, `{"statements":[],}`, `{"statements":[,]`} {
		_, err := DecodeJSON([]byte(input))
		if err == nil {
			t.Fatalf("accepted malformed/trailing JSON %q", input)
		}
		var got *JSONDecodeError
		if !errors.As(err, &got) {
			t.Fatalf("strict parse error lacks JSONDecodeError: %T %v", err, err)
		}
		if input == `{"statements":[` {
			if !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("incomplete JSON lost encoding/json cause: %T %v", err, err)
			}
		} else if input != `{"statements":[]} {}` {
			var syntax *json.SyntaxError
			if !errors.As(err, &syntax) {
				t.Fatalf("syntax error lacks encoding/json cause: %T %v", err, err)
			}
		}
	}
	tooDeep := []byte(`{"statements":[],"x":` + strings.Repeat("[", 10001) + "0" + strings.Repeat("]", 10001) + `}`)
	if _, err := DecodeJSON(tooDeep); err == nil {
		t.Fatal("accepted JSON beyond encoding/json's nesting limit")
	} else {
		var syntax *json.SyntaxError
		if !errors.As(err, &syntax) {
			t.Fatalf("nesting error lacks standard syntax cause: %T %v", err, err)
		}
	}
	_, err := DecodeJSON([]byte(`{"statements":[{"@type":"RuleDef","name":"r","expr":null}]}`))
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Path != "$.statements[0].expr" {
		t.Fatalf("strict decoder skipped structural validation: %T %v", err, err)
	}
}

func TestDecodeJSONValidRoundTripCorpus(t *testing.T) {
	g := sampleGrammar()
	data, err := MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeJSON(data)
	if err != nil {
		t.Fatalf("strict round trip: %v", err)
	}
	if !reflect.DeepEqual(g, got) {
		t.Fatalf("strict round trip mismatch\n%s", data)
	}
}

func assertJSONDecodeError(t *testing.T, err error, path string, offset int, message string) {
	t.Helper()
	var got *JSONDecodeError
	if !errors.As(err, &got) {
		t.Fatalf("got %T %v, want *JSONDecodeError", err, err)
	}
	if got.Path != path || got.Offset != offset || !strings.Contains(got.Msg, message) {
		t.Fatalf("got JSONDecodeError{Path:%q Offset:%d Msg:%q}; want path %q offset %d message containing %q", got.Path, got.Offset, got.Msg, path, offset, message)
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

func TestDecodeJSONNullAndIntegerBounds(t *testing.T) {
	_, err := DecodeJSON([]byte("null"))
	var invalid *ValidationError
	if !errors.As(err, &invalid) || invalid.Path != "$" {
		t.Fatalf("null root: %T %v", err, err)
	}
	prefix := `{"statements":[{"@type":"RuleDef","name":"main","expr":{"@type":"Literal","value":"x"},"action":{"@type":"IntLit","value":`
	suffix := `}}]}`
	maxInt := int64(^uint(0) >> 1)
	for _, value := range []int64{maxInt, -maxInt - 1} {
		g, err := DecodeJSON([]byte(prefix + strconv.FormatInt(value, 10) + suffix))
		if err != nil {
			t.Fatalf("native int %d: %v", value, err)
		}
		if got := g.Statements[0].(*RuleDef).Action.(*IntLit).Value; int64(got) != value {
			t.Fatalf("native int changed: got %d want %d", got, value)
		}
	}
	for _, value := range []string{"9223372036854775808", "-9223372036854775809", "1.5"} {
		if _, err := DecodeJSON([]byte(prefix + value + suffix)); err == nil {
			t.Fatalf("accepted nonrepresentable integer %s", value)
		}
	}
	runeOverflow := []byte(`{"statements":[{"@type":"RuleDef","name":"main","expr":{"@type":"CharClass","ranges":[{"lo":2147483648,"hi":2147483648}]}}]}`)
	if _, err := DecodeJSON(runeOverflow); err == nil || !strings.Contains(err.Error(), "out of range for int32") {
		t.Fatalf("rune overflow wrapped before validation: %v", err)
	}
}
