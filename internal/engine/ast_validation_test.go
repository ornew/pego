package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

func TestCompileMalformedAST(t *testing.T) {
	for _, tc := range []struct {
		name string
		g    *grammar.Grammar
		want string
	}{
		{"nil grammar", nil, "$"},
		{"negative index", &grammar.Grammar{Statements: []grammar.Statement{&grammar.RuleDef{Name: "p", Expr: &grammar.Literal{Value: "a"}, Action: &grammar.IndexRef{Index: -1}}}}, "$.statements[0].action.index"},
		{"nil field", &grammar.Grammar{Statements: []grammar.Statement{&grammar.TypeDef{Name: "T", Spec: &grammar.StructSpec{Fields: []*grammar.Field{nil}}}}}, "$.statements[0].spec.fields[0]"},
		{"reversed repeat", &grammar.Grammar{Statements: []grammar.Statement{&grammar.RuleDef{Name: "p", Expr: &grammar.Repeat{Expr: &grammar.Literal{Value: "a"}, Min: 2, Max: 1}}}}, "$.statements[0].expr.max"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, opts := range []Options{{}, {NoTypeCheck: true}} {
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Errorf("Compile panicked: %v", r)
						}
					}()
					p, err := Compile(tc.g, opts)
					if p != nil || err == nil || !strings.Contains(err.Error(), tc.want) {
						t.Errorf("got program %v, error %v; want %s", p != nil, err, tc.want)
					}
				}()
			}
		})
	}
}

func TestMalformedASTEntryPointsAndPosition(t *testing.T) {
	e := &grammar.Optional{Pos: grammar.Pos{Line: 3, Col: 4}}
	e.Expr = e
	g := &grammar.Grammar{Statements: []grammar.Statement{&grammar.RuleDef{Name: "p", Expr: e}}}
	for name, run := range map[string]func() error{
		"compile":            func() error { _, err := Compile(g, Options{}); return err },
		"saved AST rebuild":  func() error { _, err := build(g, Options{}, []ruleFlags{{}}); return err },
		"Go generator":       func() error { _, err := Generate(g, GenOptions{Start: "p", Package: "p"}); return err },
		"typed Go generator": func() error { _, err := Generate(g, GenOptions{Start: "p", Package: "p", Types: true}); return err },
		"TS generator":       func() error { _, err := GenerateTS(g, GenOptions{Start: "p"}); return err },
	} {
		t.Run(name, func(t *testing.T) {
			err := run()
			if errs, ok := err.(ErrorList); !ok || len(errs) != 1 || errs[0].Pos != e.Pos || !strings.Contains(err.Error(), "$.statements[0].expr.expr") {
				t.Fatalf("got %v; want positioned pointer-cycle error", err)
			}
		})
	}
}
