package gen_test

import (
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/starttest"
)

const streamRecoveryError = "#recover cannot wrap a #stream repetition; recover inside each element instead"

func TestStreamRecoveryRejected(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testStreamRecoveryRejected)
}

func testStreamRecoveryRejected(t *testing.T) {
	for _, pragmas := range []string{`#stream #recover(skip=.*)`, `#recover(skip=.*) #stream`} {
		src := `rule main = "ab" #repeat(2) ` + pragmas + ` $$`
		g, err := parseTest(src)
		if err != nil {
			t.Fatal(err)
		}
		for _, opts := range []compile.Options{{}, {Recognize: true}} {
			if _, err := engine.Compile(g, opts); err == nil || !strings.Contains(err.Error(), streamRecoveryError) {
				t.Errorf("%s: %v", src, err)
			}
		}
		for name, generate := range map[string]func() error{
			"Go": func() error {
				_, err := gentest.GenerateCode(t, g, gen.GenOptions{Start: "main", Package: "test"})
				return err
			},
			"typed Go": func() error {
				_, err := gentest.GenerateCode(t, g, gen.GenOptions{Start: "main", Package: "test", Types: true})
				return err
			},
			"TS": func() error { _, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "main"}); return err },
		} {
			if err := generate(); err == nil || !strings.Contains(err.Error(), streamRecoveryError) {
				t.Errorf("%s generation: %v", name, err)
			}
		}
	}
	pos := grammar.Pos{Line: 3, Col: 10}
	g := &grammar.Grammar{File: grammar.File{Decls: []grammar.Decl{&grammar.RuleDef{Name: "main", Expr: &grammar.Recovery{SourceInfo: grammar.SourceInfo{Pos: pos}, Expr: &grammar.Stream{Expr: &grammar.Repeat{
		Expr: &grammar.Literal{Value: "ab"}, Min: 2, Max: 2,
	}},
		Skip: &grammar.Repeat{Expr: &grammar.Any{}, Min: 0, Max: -1},
	}}}}}
	starttest.MarkAll(g)
	_, err := engine.Compile(g, compile.Options{})
	if errs, ok := err.(compile.ErrorList); !ok || len(errs) != 1 || errs[0].Pos != pos || !strings.Contains(err.Error(), streamRecoveryError) {
		t.Errorf("public AST: %v; want positioned recovery error", err)
	}
}
