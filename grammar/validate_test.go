package grammar

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func expressionGrammar(e Expr) *Grammar {
	return &Grammar{File: File{Decls: []Decl{&RuleDef{Name: "p", Expr: e}}}}
}
func termGrammar(t Term) *Grammar {
	return &Grammar{File: File{Decls: []Decl{&RuleDef{Name: "p", Expr: &Literal{Value: "a"}, Action: t}}}}
}
func typeGrammar(t TypeExpr) *Grammar {
	return &Grammar{File: File{Decls: []Decl{&TypeAlias{Name: "T", Type: t}}}}
}

func TestValidateRequiredChildren(t *testing.T) {
	for _, tc := range []struct {
		name string
		g    *Grammar
		path string
	}{
		{"statement", &Grammar{File: File{Decls: []Decl{nil}}}, "$.file.decls[0]"},
		{"type spec", &Grammar{File: File{Decls: []Decl{&TypeDef{Name: "T"}}}}, "$.file.decls[0].type"},
		{"rule body", expressionGrammar(nil), "$.file.decls[0].expr"},
		{"struct field", &Grammar{File: File{Decls: []Decl{&TypeDef{Name: "T", Type: &StructType{Fields: []*Field{nil}}}}}}, "$.file.decls[0].type.fields[0]"},
		{"field type", &Grammar{File: File{Decls: []Decl{&TypeDef{Name: "T", Type: &StructType{Fields: []*Field{{Name: "F"}}}}}}}, "$.file.decls[0].type.fields[0].type"},
		{"alias", typeGrammar(nil), "$.file.decls[0].type"},
		{"type group", typeGrammar(&TypeGroup{}), "$.file.decls[0].type.type"},
		{"term group", termGrammar(&TermGroup{}), "$.file.decls[0].action.term"},
		{"list", typeGrammar(&ListType{}), "$.file.decls[0].type.elem"},
		{"optional type", typeGrammar(&OptionalType{}), "$.file.decls[0].type.elem"},
		{"union", typeGrammar(&UnionType{Types: []TypeExpr{nil}}), "$.file.decls[0].type.types[0]"},
		{"sequence", expressionGrammar(&Seq{Items: []Expr{nil}}), "$.file.decls[0].expr.items[0]"},
		{"choice", expressionGrammar(&Choice{Alts: []Expr{nil}}), "$.file.decls[0].expr.alts[0]"},
		{"repeat", expressionGrammar(&Repeat{}), "$.file.decls[0].expr.expr"},
		{"optional", expressionGrammar(&Optional{}), "$.file.decls[0].expr.expr"},
		{"and", expressionGrammar(&And{}), "$.file.decls[0].expr.expr"},
		{"not", expressionGrammar(&Not{}), "$.file.decls[0].expr.expr"},
		{"atomic", expressionGrammar(&Atomic{}), "$.file.decls[0].expr.expr"},
		{"discard", expressionGrammar(&Discard{}), "$.file.decls[0].expr.expr"},
		{"capture", expressionGrammar(&Capture{Name: "value"}), "$.file.decls[0].expr.expr"},
		{"predicate", expressionGrammar(&Predicate{}), "$.file.decls[0].expr.term"},
		{"attributed expression", expressionGrammar(&Attributed{}), "$.file.decls[0].expr.expr"},
		{"attribute", expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{nil}}), "$.file.decls[0].expr.attrs[0]"},
		{"attribute argument", expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{{Name: "error", Args: []any{&Repeat{}}}}}), "$.file.decls[0].expr.attrs[0].args[0].expr"},
		{"attribute value", expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{{Name: "error", Args: []any{&IntLit{Value: 1}}}}}), "$.file.decls[0].expr.attrs[0].args[0]"},
		{"operand", expressionGrammar(&OperatorsRule{Operands: []*OperatorsRuleOperand{nil}}), "$.file.decls[0].expr.operands[0]"},
		{"operand expression", expressionGrammar(&OperatorsRule{Operands: []*OperatorsRuleOperand{{}}}), "$.file.decls[0].expr.operands[0].expr"},
		{"level", expressionGrammar(&OperatorsRule{Levels: []*OperatorsRuleLevel{nil}}), "$.file.decls[0].expr.levels[0]"},
		{"operator", expressionGrammar(&OperatorsRule{Levels: []*OperatorsRuleLevel{{Operators: []*OperatorsRuleOperator{nil}}}}), "$.file.decls[0].expr.levels[0].operators[0]"},
		{"operator expression", expressionGrammar(&OperatorsRule{Levels: []*OperatorsRuleLevel{{Operators: []*OperatorsRuleOperator{{Kind: Infix, Assoc: AssocLeft}}}}}), "$.file.decls[0].expr.levels[0].operators[0].expr"},
		{"member", termGrammar(&Member{Name: "Value"}), "$.file.decls[0].action.x"},
		{"field initializer", termGrammar(&New{Type: "T", Fields: []*FieldInit{nil}}), "$.file.decls[0].action.fields[0]"},
		{"field value", termGrammar(&New{Type: "T", Fields: []*FieldInit{{Name: "F"}}}), "$.file.decls[0].action.fields[0].value"},
		{"call", termGrammar(&Call{Func: "len", Args: []Term{nil}}), "$.file.decls[0].action.args[0]"},
		{"tuple literal", termGrammar(&TupleLit{Elems: []Term{nil}}), "$.file.decls[0].action.elems[0]"},
		{"lambda", termGrammar(&Lambda{}), "$.file.decls[0].action.body"},
		{"binary left", termGrammar(&Binary{Op: "+", R: &IntLit{Value: 1}}), "$.file.decls[0].action.l"},
		{"binary right", termGrammar(&Binary{Op: "+", L: &IntLit{Value: 1}}), "$.file.decls[0].action.r"},
		{"unary", termGrammar(&Unary{Op: "-"}), "$.file.decls[0].action.x"},
		{"assign", expressionGrammar(&Predicate{Term: &Assign{Name: "x"}}), "$.file.decls[0].expr.term.value"},
		{"conflicting rule default", &Grammar{File: File{Decls: []Decl{&RuleDef{Name: "main", Expr: &Top{}, DefaultAction: true, Action: &NilLit{}}}}}, "$.file.decls[0].action"},
		{"conflicting operand default", expressionGrammar(&OperatorsRule{Operands: []*OperatorsRuleOperand{{Expr: &Top{}, DefaultAction: true, Action: &NilLit{}}}}), "$.file.decls[0].expr.operands[0].action"},
		{"conflicting operator default", expressionGrammar(&OperatorsRule{Levels: []*OperatorsRuleLevel{{Operators: []*OperatorsRuleOperator{{Expr: &Top{}, DefaultAction: true, Action: &NilLit{}}}}}}), "$.file.decls[0].expr.levels[0].operators[0].action"},
		{"recovery body", expressionGrammar(&Recovery{Skip: &Top{}}), "$.file.decls[0].expr.expr"},
		{"recovery skip", expressionGrammar(&Recovery{Expr: &Top{}}), "$.file.decls[0].expr.skip"},
		{"stream body", expressionGrammar(&Stream{}), "$.file.decls[0].expr.expr"},
		{"negative index", termGrammar(&IndexRef{Index: -1}), "$.file.decls[0].action.index"},
		{"negative minimum", expressionGrammar(&Repeat{Expr: &Top{}, Min: -1, Max: -1}), "$.file.decls[0].expr.min"},
		{"reversed bounds", expressionGrammar(&Repeat{Expr: &Top{}, Min: 2, Max: 1}), "$.file.decls[0].expr.max"},
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
		case Decl:
			g = &Grammar{File: File{Decls: []Decl{n}}}
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
		{"attribute expression", expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{{Name: "recover", KwArgs: map[string]any{"skip": (*Ref)(nil)}}}}), `$.file.decls[0].expr.attrs[0].kwArgs["skip"]`},
		{"rule type", &Grammar{File: File{Decls: []Decl{&RuleDef{Name: "p", Type: (*TypeRef)(nil), Expr: &Top{}}}}}, "$.file.decls[0].type"},
		{"skip", expressionGrammar(&OperatorsRule{Skip: (*Top)(nil)}), "$.file.decls[0].expr.skip"},
		{"operand action", expressionGrammar(&OperatorsRule{Operands: []*OperatorsRuleOperand{{Expr: &Top{}, Action: (*NilLit)(nil)}}}), "$.file.decls[0].expr.operands[0].action"},
		{"operator action", expressionGrammar(&OperatorsRule{Levels: []*OperatorsRuleLevel{{Operators: []*OperatorsRuleOperator{{Expr: &Top{}, Action: (*NilLit)(nil)}}}}}), "$.file.decls[0].expr.levels[0].operators[0].action"},
	} {
		t.Run(tc.name, func(t *testing.T) { assertValidationError(t, Validate(tc.g), tc.path) })
	}
}

func TestValidateCyclesAndSharing(t *testing.T) {
	expr := &Optional{}
	expr.Expr = expr
	assertValidationError(t, Validate(expressionGrammar(expr)), "$.file.decls[0].expr.expr")
	typ := &ListType{}
	typ.Elem = typ
	assertValidationError(t, Validate(typeGrammar(typ)), "$.file.decls[0].type.elem")
	term := &Unary{Op: "-"}
	term.X = &Member{X: term, Name: "F"}
	assertValidationError(t, Validate(termGrammar(term)), "$.file.decls[0].action.x.x")
	groupType := &TypeGroup{}
	groupType.Type = groupType
	assertValidationError(t, Validate(typeGrammar(groupType)), "$.file.decls[0].type.type")
	groupTerm := &TermGroup{}
	groupTerm.Term = groupTerm
	assertValidationError(t, Validate(termGrammar(groupTerm)), "$.file.decls[0].action.term")
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
	g := expressionGrammar(&Seq{Items: []Expr{&Repeat{SourceInfo: SourceInfo{Pos: Pos{Line: 4, Col: 7}}, Min: -1}, nil}})
	assertValidationError(t, Validate(g), "$.file.decls[0].expr.items[0].min")
	e := Validate(g).(*ValidationError)
	if e.Pos != (Pos{Line: 4, Col: 7}) || !strings.HasPrefix(e.Error(), "4:7: ") {
		t.Fatal(e)
	}
	rd := g.File.Decls[0].(*RuleDef)
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
		expressionGrammar(&OperatorsRule{Operands: []*OperatorsRuleOperand{{Expr: &Top{}}}, Levels: []*OperatorsRuleLevel{{}}}),
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

func TestValidateASCIIIdentifiers(t *testing.T) {
	for _, tc := range []struct {
		name string
		g    *Grammar
		path string
	}{
		{"empty rule", &Grammar{File: File{Decls: []Decl{&RuleDef{Expr: &Top{}}}}}, "$.file.decls[0].name"},
		{"empty reference", expressionGrammar(&Ref{}), "$.file.decls[0].expr.name"},
		{"empty capture", expressionGrammar(&Capture{Expr: &Top{}}), "$.file.decls[0].expr.name"},
		{"empty attribute", expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{{}}}), "$.file.decls[0].expr.attrs[0].name"},
		{"empty attribute argument", expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{{Name: "error", KwArgs: map[string]any{"": "x"}}}}), "$.file.decls[0].expr.attrs[0].kwArgs[\"\"]"},
		{"empty lambda parameter", termGrammar(&Lambda{Params: []string{""}, Body: &IntLit{Value: 1}}), "$.file.decls[0].action.params[0]"},
		{"package", &Grammar{File: File{Pkg: Pkg{Name: "名前"}}}, "$.file.pkg.name"},
		{"rule", &Grammar{File: File{Decls: []Decl{&RuleDef{Name: "café", Expr: &Top{}}}}}, "$.file.decls[0].name"},
		{"reference", expressionGrammar(&Ref{Name: "é"}), "$.file.decls[0].expr.name"},
		{"level", expressionGrammar(&Ref{Name: "expr", Level: "名"}), "$.file.decls[0].expr.level"},
		{"attribute argument", expressionGrammar(&Attributed{Expr: &Top{}, Attrs: []*Attribute{{Name: "error", KwArgs: map[string]any{"méssage": "x"}}}}), "$.file.decls[0].expr.attrs[0].kwArgs[\"méssage\"]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertValidationError(t, Validate(tc.g), tc.path)
		})
	}
	if err := Validate(expressionGrammar(&Literal{Value: "名前😀"})); err != nil {
		t.Fatalf("Unicode literal rejected: %v", err)
	}
	if err := Validate(expressionGrammar(&OperatorsRule{Operands: []*OperatorsRuleOperand{{Expr: &Top{}}}, Levels: []*OperatorsRuleLevel{{}}})); err != nil {
		t.Fatalf("unnamed level rejected: %v", err)
	}
}

func TestJSONMalformedAndOverflow(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"null", "$"},
		{`{"file":{"decls":[{"@type":"RuleDef","name":"p"}]}}`, "$.file.decls[0].expr"},
		{`{"file":{"decls":[{"@type":"TypeDef","name":"T","type":{"@type":"StructType","fields":[null]}}]}}`, "$.file.decls[0].type.fields[0]"},
		{`{"file":{"decls":[{"@type":"RuleDef","name":"p","expr":{"@type":"CharClass","ranges":[{"lo":4294967296,"hi":97}]}}]}}`, "$.file.decls[0].expr.ranges[0].lo"},
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
