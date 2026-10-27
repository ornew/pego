package engine

import (
	goast "go/ast"
	goparser "go/parser"
	"go/token"
	"testing"
)

// Every generated rule-body function (a top-level function charging StepWork first) must have the
// parser parameter p, so that no body silently misses its ordinary specialization.
//
// A body whose ordinary branch the generator writes is specialized and counted as such. In the builds without
// Documents the fixture's rules (a recovering rule, a leader and an Operators
// Rule) print from Flow IR, which charges no work in an ordinary parse and so has no StepWork to specialize;
// those are counted as Flow bodies (functions that take p and return two values). Either way some body must
// exist, and no body may start with StepWork.
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
			bodies, flowBodies := 0, 0
			for _, decl := range file.Decls {
				fn, ok := decl.(*goast.FuncDecl)
				if !ok || fn.Recv != nil || fn.Body == nil || len(fn.Body.List) == 0 {
					continue
				}
				stmt, ok := fn.Body.List[0].(*goast.ExprStmt)
				if _, isBody := ordinaryMethodIdentity(fn); isBody && fn.Type.Results != nil && len(fn.Type.Results.List) == 2 && !startsWithStepWork(stmt, ok) {
					// A body printed from Flow IR charges no work in an ordinary parse, so it has nothing
					// to specialize.
					if _, isIf := fn.Body.List[0].(*goast.IfStmt); !isIf {
						flowBodies++
						continue
					}
				}
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
			if bodies+flowBodies == 0 {
				t.Fatalf("types=%v document=%v: no rule bodies found", types, document)
			}
			t.Logf("types=%v document=%v: %d specialized direct bodies, %d Flow bodies", types, document, bodies, flowBodies)
		}
	}
}

// startsWithStepWork reports whether the statement stmt (valid if ok) is a call of StepWork.
func startsWithStepWork(stmt *goast.ExprStmt, ok bool) bool {
	if !ok {
		return false
	}
	call, isCall := stmt.X.(*goast.CallExpr)
	if !isCall {
		return false
	}
	sel, isSel := call.Fun.(*goast.SelectorExpr)
	return isSel && sel.Sel.Name == "StepWork"
}
