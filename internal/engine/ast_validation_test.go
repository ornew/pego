package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

func TestCompileMalformedAST(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		g    *grammar.Grammar
		want string
	}{
		{"nil grammar", nil, "$"},
		{"negative index", &grammar.Grammar{File: grammar.File{Decls: []grammar.Decl{&grammar.RuleDef{Name: "p", Expr: &grammar.Literal{Value: "a"}, Action: &grammar.IndexRef{Index: -1}}}}}, "$.file.decls[0].action.index"},
		{"nil field", &grammar.Grammar{File: grammar.File{Decls: []grammar.Decl{&grammar.TypeDef{Name: "T", Type: &grammar.StructType{Fields: []*grammar.Field{nil}}}}}}, "$.file.decls[0].type.fields[0]"},
		{"reversed repeat", &grammar.Grammar{File: grammar.File{Decls: []grammar.Decl{&grammar.RuleDef{Name: "p", Expr: &grammar.Repeat{Expr: &grammar.Literal{Value: "a"}, Min: 2, Max: 1}}}}}, "$.file.decls[0].expr.max"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, opts := range []compile.Options{{}} {
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
	t.Parallel()
	gentest.EachMode(t, testMalformedASTEntryPointsAndPosition)
}

func testMalformedASTEntryPointsAndPosition(t *testing.T) {
	e := &grammar.Optional{SourceInfo: grammar.SourceInfo{Pos: grammar.Pos{Line: 3, Col: 4}}}
	e.Expr = e
	g := &grammar.Grammar{File: grammar.File{Decls: []grammar.Decl{&grammar.RuleDef{Name: "p", Expr: e}}}}
	for name, run := range map[string]func() error{
		"compile":           func() error { _, err := Compile(g, compile.Options{}); return err },
		"saved AST rebuild": func() error { _, err := build(g, compile.Options{}, []compile.RuleFlags{{}}); return err },
		"Go generator": func() error {
			_, err := gentest.GenerateCode(t, g, gen.GenOptions{Start: "p", Package: "p"})
			return err
		},
		"typed Go generator": func() error {
			_, err := gentest.GenerateCode(t, g, gen.GenOptions{Start: "p", Package: "p", Types: true})
			return err
		},
		"TS generator": func() error { _, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "p"}); return err },
	} {
		t.Run(name, func(t *testing.T) {
			err := run()
			if errs, ok := err.(compile.ErrorList); !ok || len(errs) != 1 || errs[0].Pos != e.Pos || !strings.Contains(err.Error(), "$.file.decls[0].expr.expr") {
				t.Fatalf("got %v; want positioned pointer-cycle error", err)
			}
		})
	}
}
