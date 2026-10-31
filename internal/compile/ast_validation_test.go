package compile

import (
	"strings"
	"testing"

	"github.com/ornew/zeroparse/grammar"
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
			for _, opts := range []Options{{}} {
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Errorf("Compile panicked: %v", r)
						}
					}()
					p, err := compileStatic(tc.g, opts, nil)
					if p != nil || err == nil || !strings.Contains(err.Error(), tc.want) {
						t.Errorf("got program %v, error %v; want %s", p != nil, err, tc.want)
					}
				}()
			}
		})
	}
}
