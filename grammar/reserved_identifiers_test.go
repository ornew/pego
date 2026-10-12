package grammar

import (
	"strings"
	"testing"
)

func TestReservedASTIdentifiers(t *testing.T) {
	cases := []*Grammar{
		{Package: "_pkg"},
		{Statements: []Statement{&RuleDef{Name: "_rule", Expr: &Top{}}}},
		{Statements: []Statement{&RuleDef{Name: "main", Expr: &Ref{Name: "_callee"}}}},
		{Statements: []Statement{&RuleDef{Name: "main", Expr: &Capture{Name: "_capture", Expr: &Top{}}}}},
		{Statements: []Statement{&RuleDef{Name: "main", Expr: &Predicate{Term: &Assign{Name: "_var", Value: &IntLit{Value: 0}}}}}},
		{Statements: []Statement{&RuleDef{Name: "main", Expr: &Top{}, Action: &Lambda{Params: []string{"_arg"}, Body: &NilLit{}}}}},
		{Statements: []Statement{&RuleDef{Name: "main", Expr: &Attributed{Expr: &Top{}, Attrs: []*Attribute{{Name: "error", KwArgs: map[string]any{"_b": "x", "_a": "y"}}}}}}},
	}
	for _, g := range cases {
		err := Validate(g)
		if err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Fatalf("accepted reserved AST: %v", err)
		}
		raw, err := MarshalJSON(g)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := UnmarshalJSON(raw); err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Fatalf("accepted reserved JSON: %v", err)
		}
	}

	g := &Grammar{Statements: []Statement{&RuleDef{Name: "main", Expr: &Literal{Value: "_data"}, Action: &StringLit{Value: "_data"}}}}
	if err := Validate(g); err != nil {
		t.Fatal(err)
	}
}
