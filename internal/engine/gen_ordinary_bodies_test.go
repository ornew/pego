package engine

import (
	goast "go/ast"
	goparser "go/parser"
	"go/token"
	"testing"
)

// Every generated rule-body function (a top-level function charging StepWork first) must have the
// parser parameter p, so that no body silently misses its ordinary specialization.
func TestGeneratedRuleBodiesAreSpecialized(t *testing.T) {
	eachMode(t, testGeneratedRuleBodiesAreSpecialized)
}

func testGeneratedRuleBodiesAreSpecialized(t *testing.T) {
	g, err := parseTest(`type Text = { Text string }
rule main Text = (a:("a"* #atomic) "b") #recover(skip="?") => Text{Text: text($a)}
rule recursive Text = t:(recursive "+" "a" / "a") #atomic => Text{Text: text($t)}
rule expr Text = #operators {
 operand main
 level sum { infix left "+" => $lhs }
}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, types := range []bool{false, true} {
		for _, document := range []bool{false, true} {
			opts := GenOptions{Package: "ordinaryfixture", Types: types, Document: document, Recognize: true, Start: "main"}
			src, err := generateCode(g, opts)
			if err != nil {
				t.Fatal(err)
			}
			file, err := goparser.ParseFile(token.NewFileSet(), "generated.go", src, goparser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			bodies := 0
			for _, decl := range file.Decls {
				fn, ok := decl.(*goast.FuncDecl)
				if !ok || fn.Recv != nil || fn.Body == nil || len(fn.Body.List) == 0 {
					continue
				}
				stmt, ok := fn.Body.List[0].(*goast.ExprStmt)
				if !ok {
					// A specialized body starts with its ordinary branch.
					if guard, isIf := fn.Body.List[0].(*goast.IfStmt); isIf && fn.Type.Results != nil && len(fn.Type.Results.List) == 2 {
						if _, ok := ordinaryMethodIdentity(fn); ok && guard.Else == nil {
							bodies++
						}
					}
					continue
				}
				call, ok := stmt.X.(*goast.CallExpr)
				if !ok {
					continue
				}
				if sel, ok := call.Fun.(*goast.SelectorExpr); !ok || sel.Sel.Name != "StepWork" {
					continue
				}
				if fn.Type.Results == nil || len(fn.Type.Results.List) != 2 {
					continue
				}
				// A body that still starts with StepWork was not given an ordinary branch.
				t.Errorf("types=%v document=%v: rule body %s is not specialized", types, document, fn.Name.Name)
			}
			if bodies == 0 {
				t.Fatalf("types=%v document=%v: no specialized rule bodies found", types, document)
			}
		}
	}
}
