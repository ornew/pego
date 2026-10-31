package engine

import (
	"strings"
	"testing"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/compile"
	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

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
