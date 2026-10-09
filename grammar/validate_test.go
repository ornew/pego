package grammar

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func expressionGrammar(e Expr) *Grammar {
	return &Grammar{Statements: []Statement{&RuleDef{Name: "p", Expr: e}}}
}
func termGrammar(t Term) *Grammar {
	return &Grammar{Statements: []Statement{&RuleDef{Name: "p", Expr: &Literal{Value: "a"}, Action: t}}}
}
func typeGrammar(t TypeExpr) *Grammar {
	return &Grammar{Statements: []Statement{&TypeDef{Name: "T", Spec: &AliasSpec{Type: t}}}}
}

func TestValidateRequiredChildren(t *testing.T) {
	for _, tc := range []struct {
		name string
		g    *Grammar
		path string
	}{
		{"statement", &Grammar{Statements: []Statement{nil}}, "$.statements[0]"},
		{"type spec", &Grammar{Statements: []Statement{&TypeDef{Name: "T"}}}, "$.statements[0].spec"},
		{"rule body", expressionGrammar(nil), "$.statements[0].expr"},
		{"struct field", &Grammar{Statements: []Statement{&TypeDef{Name: "T", Spec: &StructSpec{Fields: []*Field{nil}}}}}, "$.statements[0].spec.fields[0]"},
		{"field type", &Grammar{Statements: []Statement{&TypeDef{Name: "T", Spec: &StructSpec{Fields: []*Field{{Name: "F"}}}}}}, "$.statements[0].spec.fields[0].type"},
		{"alias", typeGrammar(nil), "$.statements[0].spec.type"},
		{"list", typeGrammar(&ListType{}), "$.statements[0].spec.type.elem"},
		{"optional type", typeGrammar(&OptionalType{}), "$.statements[0].spec.type.elem"},
		{"union", typeGrammar(&UnionType{Types: []TypeExpr{nil}}), "$.statements[0].spec.type.types[0]"},
		{"sequence", expressionGrammar(&Seq{Items: []Expr{nil}}), "$.statements[0].expr.items[0]"},
		{"choice", expressionGrammar(&Choice{Alts: []Expr{nil}}), "$.statements[0].expr.alts[0]"},
		{"repeat", expressionGrammar(&Repeat{}), "$.statements[0].expr.expr"},
		{"optional", expressionGrammar(&Optional{}), "$.statements[0].expr.expr"},
		{"and", expressionGrammar(&And{}), "$.statements[0].expr.expr"},
		{"not", expressionGrammar(&Not{}), "$.statements[0].expr.expr"},
		{"atomic", expressionGrammar(&Atomic{}), "$.statements[0].expr.expr"},
		{"discard", expressionGrammar(&Discard{}), "$.statements[0].expr.expr"},
		{"capture", expressionGrammar(&Capture{}), "$.statements[0].expr.expr"},
		{"predicate", expressionGrammar(&Predicate{}), "$.statements[0].expr.term"},
		{"attributed expression", expressionGrammar(&Attributed{}), "$.statements[0].expr.expr"},
		{"attribute", expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{nil}}), "$.statements[0].expr.attrs[0]"},
		{"attribute argument", expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{{Name: "error", Args: []*AttrArg{nil}}}}), "$.statements[0].expr.attrs[0].args[0]"},
		{"attribute value", expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{{Name: "error", Args: []*AttrArg{{Name: "message"}}}}}), "$.statements[0].expr.attrs[0].args[0].value"},
		{"operand", expressionGrammar(&Pratt{Operands: []*PrattOperand{nil}}), "$.statements[0].expr.operands[0]"},
		{"operand expression", expressionGrammar(&Pratt{Operands: []*PrattOperand{{}}}), "$.statements[0].expr.operands[0].expr"},
		{"level", expressionGrammar(&Pratt{Levels: []*PrattLevel{nil}}), "$.statements[0].expr.levels[0]"},
		{"operator", expressionGrammar(&Pratt{Levels: []*PrattLevel{{Operators: []*PrattOperator{nil}}}}), "$.statements[0].expr.levels[0].operators[0]"},
		{"operator expression", expressionGrammar(&Pratt{Levels: []*PrattLevel{{Operators: []*PrattOperator{{Kind: Infix, Assoc: AssocLeft}}}}}), "$.statements[0].expr.levels[0].operators[0].expr"},
		{"member", termGrammar(&Member{}), "$.statements[0].action.x"},
		{"field initializer", termGrammar(&New{Type: "T", Fields: []*FieldInit{nil}}), "$.statements[0].action.fields[0]"},
		{"field value", termGrammar(&New{Type: "T", Fields: []*FieldInit{{Name: "F"}}}), "$.statements[0].action.fields[0].value"},
		{"call", termGrammar(&Call{Func: "len", Args: []Term{nil}}), "$.statements[0].action.args[0]"},
		{"lambda", termGrammar(&Lambda{}), "$.statements[0].action.body"},
		{"binary left", termGrammar(&Binary{Op: "+", R: &IntLit{Value: 1}}), "$.statements[0].action.l"},
		{"binary right", termGrammar(&Binary{Op: "+", L: &IntLit{Value: 1}}), "$.statements[0].action.r"},
		{"unary", termGrammar(&Unary{Op: "-"}), "$.statements[0].action.x"},
		{"assign", expressionGrammar(&Predicate{Term: &Assign{Name: "x"}}), "$.statements[0].expr.term.value"},
		{"negative index", termGrammar(&IndexRef{Index: -1}), "$.statements[0].action.index"},
		{"negative minimum", expressionGrammar(&Repeat{Expr: &Top{}, Min: -1, Max: -1}), "$.statements[0].expr.min"},
		{"reversed bounds", expressionGrammar(&Repeat{Expr: &Top{}, Min: 2, Max: 1}), "$.statements[0].expr.max"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertValidationError(t, Validate(tc.g), tc.path)
			data, err := MarshalJSON(tc.g)
			if err != nil {
				t.Fatal(err)
			}
			got, err := UnmarshalJSON(data)
			if got != nil {
				t.Fatal("invalid grammar returned")
			}
			assertValidationError(t, err, tc.path)
		})
	}
}

func assertValidationError(t *testing.T, err error, path string) {
	t.Helper()
	var e *ValidationError
	if !errors.As(err, &e) || e.Path != path {
		t.Fatalf("got %v; want ValidationError at %s", err, path)
	}
}

func TestValidateTypedNil(t *testing.T) {
	// Cover every registered interface node, including leaf nodes whose
	// pointers were previously dereferenced by formatting or compilation.
	for name, typ := range registry {
		node := reflect.Zero(reflect.PointerTo(typ)).Interface()
		var g *Grammar
		switch n := node.(type) {
		case Statement:
			g = &Grammar{Statements: []Statement{n}}
		case TypeSpec:
			g = &Grammar{Statements: []Statement{&TypeDef{Name: "T", Spec: n}}}
		case TypeExpr:
			g = typeGrammar(n)
		case Expr:
			g = expressionGrammar(n)
		case Term:
			g = termGrammar(n)
		default:
			t.Fatalf("uncovered node type %s", name)
		}
		t.Run(name, func(t *testing.T) {
			var e *ValidationError
			if !errors.As(Validate(g), &e) || !strings.Contains(e.Msg, "nil") {
				t.Fatal("typed nil was not rejected")
			}
		})
	}
	for _, tc := range []struct {
		name string
		g    *Grammar
		path string
	}{
		{"rule type", &Grammar{Statements: []Statement{&RuleDef{Name: "p", Type: (*TypeRef)(nil), Expr: &Top{}}}}, "$.statements[0].type"},
		{"skip", expressionGrammar(&Pratt{Skip: (*Top)(nil)}), "$.statements[0].expr.skip"},
		{"operand action", expressionGrammar(&Pratt{Operands: []*PrattOperand{{Expr: &Top{}, Action: (*NilLit)(nil)}}}), "$.statements[0].expr.operands[0].action"},
		{"operator action", expressionGrammar(&Pratt{Levels: []*PrattLevel{{Operators: []*PrattOperator{{Expr: &Top{}, Action: (*NilLit)(nil)}}}}}), "$.statements[0].expr.levels[0].operators[0].action"},
	} {
		t.Run(tc.name, func(t *testing.T) { assertValidationError(t, Validate(tc.g), tc.path) })
	}
}

func TestValidateCyclesAndSharing(t *testing.T) {
	expr := &Optional{}
	expr.Expr = expr
	assertValidationError(t, Validate(expressionGrammar(expr)), "$.statements[0].expr.expr")
	typ := &ListType{}
	typ.Elem = typ
	assertValidationError(t, Validate(typeGrammar(typ)), "$.statements[0].spec.type.elem")
	term := &Unary{Op: "-"}
	term.X = &Member{X: term, Name: "F"}
	assertValidationError(t, Validate(termGrammar(term)), "$.statements[0].action.x.x")
	shared := &Optional{Expr: &Literal{Value: "a"}}
	g := expressionGrammar(&Seq{Items: []Expr{shared, shared}})
	before := Format(g)
	if err := Validate(g); err != nil {
		t.Fatal(err)
	}
	if Format(g) != before {
		t.Fatal("validation mutated grammar")
	}
}

func TestValidatePositionAndOrder(t *testing.T) {
	g := expressionGrammar(&Seq{Items: []Expr{&Repeat{Pos: Pos{Line: 4, Col: 7}, Min: -1}, nil}})
	assertValidationError(t, Validate(g), "$.statements[0].expr.items[0].min")
	e := Validate(g).(*ValidationError)
	if e.Pos != (Pos{Line: 4, Col: 7}) || !strings.HasPrefix(e.Error(), "4:7: ") {
		t.Fatal(e)
	}
	rd := g.Statements[0].(*RuleDef)
	rd.Pos = Pos{Line: 2, Col: 3}
	rd.Expr = &Optional{}
	if e := Validate(g).(*ValidationError); e.Pos != rd.Pos {
		t.Fatal(e)
	}
}

func TestValidateCompatibility(t *testing.T) {
	for _, g := range []*Grammar{
		{}, expressionGrammar(&Seq{}), expressionGrammar(&Choice{}),
		typeGrammar(&UnionType{}), typeGrammar(&UnionType{Types: []TypeExpr{&TypeRef{Name: "int"}}}),
		expressionGrammar(&Repeat{Expr: &Top{}, Max: -2}),
		expressionGrammar(&Repeat{Expr: &Top{}, Min: 0, Max: 0}),
		expressionGrammar(&Repeat{Expr: &Top{}, Min: 2, Max: 2}),
		termGrammar(&IndexRef{Index: 0}), termGrammar(&IndexRef{Index: 2}),
		expressionGrammar(&Pratt{Operands: []*PrattOperand{{Expr: &Top{}}}, Levels: []*PrattLevel{{}}}),
		sampleGrammar(),
	} {
		if err := Validate(g); err != nil {
			t.Fatal(err)
		}
	}
	// Validation is iterative, independent of nesting depth.
	var expr Expr = &Top{}
	for i := 0; i < 50000; i++ {
		expr = &Optional{Expr: expr}
	}
	if err := Validate(expressionGrammar(expr)); err != nil {
		t.Fatal(err)
	}
}

func TestJSONMalformedAndOverflow(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"null", "$"},
		{`{"statements":[{"@type":"RuleDef","name":"p"}]}`, "$.statements[0].expr"},
		{`{"statements":[{"@type":"TypeDef","name":"T","spec":{"@type":"StructSpec","fields":[null]}}]}`, "$.statements[0].spec.fields[0]"},
		{`{"statements":[{"@type":"RuleDef","name":"p","expr":{"@type":"CharClass","ranges":[{"lo":4294967296,"hi":97}]}}]}`, "$.statements[0].expr.ranges[0].lo"},
	} {
		if _, err := UnmarshalJSON([]byte(tc.input)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("got %v; want %s", err, tc.want)
		}
	}
	if _, err := UnmarshalJSON([]byte("{\"statements\":[]} \n\t")); err != nil {
		t.Fatal(err)
	}
	// Existing permissive unknown-field policy is preserved.
	if _, err := UnmarshalJSON([]byte(`{"unknown":true,"statements":[]}`)); err != nil {
		t.Fatal(err)
	}
}
