package engine

import (
	goast "go/ast"
	goparser "go/parser"
	"go/printer"
	"go/token"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
)

// A generated parser has one form of its rule bodies. With work controls its bodies charge steps
// and no body starts with a branch that chooses between forms at run time; without them no body
// charges or mentions a control. A parser with work controls or a Document calls no helper that has
// an Ordinary form (NewNodeOrdinary, NextOrdinary, ...), which belongs to the form without both.
func TestGeneratedRuleBodiesHaveOneForm(t *testing.T) {
	eachMode(t, testGeneratedRuleBodiesHaveOneForm)
}

const oneFormGrammar = `type Text = { Text string }
rule main Text = (a:("a"* #atomic) "b") #recover(skip="?") => Text{Text: text($a)}
rule recursive Text = t:(recursive "+" "a" / "a") #atomic => Text{Text: text($t)}
rule expr Text = #operators {
 operand main
 level sum { infix left "+" => $lhs }
}`

// oneFormProblems checks the rule bodies of the generated source src against the form that
// controls (work controls) and document select, and reports the bodies it found, the bodies that
// charge, and what contradicts the form.
func oneFormProblems(t *testing.T, src []byte, controls, document bool) (problems []string, bodies, charging int) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := goparser.ParseFile(fset, "generated.go", src, goparser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	plain := !controls && !document
	for _, decl := range file.Decls {
		fn, ok := decl.(*goast.FuncDecl)
		if !ok || fn.Body == nil || len(fn.Body.List) == 0 || fn.Type.Results == nil || len(fn.Type.Results.List) != 2 {
			continue
		}
		if _, isBody := parserFunctionKey(fn); !isBody {
			continue
		}
		bodies++
		if guard, isIf := fn.Body.List[0].(*goast.IfStmt); isIf && guard.Else == nil {
			var cond strings.Builder
			if err := printer.Fprint(&cond, fset, guard.Cond); err != nil {
				t.Fatal(err)
			}
			switch cond.String() {
			case "p.Control == nil", "p.Document == nil", "p.Control == nil && p.Document == nil":
				problems = append(problems, fn.Name.Name+" starts with the branch "+cond.String()+" between forms")
			}
		}
		charges, controlRead, ordinaryCall := false, false, ""
		goast.Inspect(fn.Body, func(n goast.Node) bool {
			if sel, ok := n.(*goast.SelectorExpr); ok {
				charges = charges || sel.Sel.Name == "StepWork"
				controlRead = controlRead || sel.Sel.Name == "Control"
				if strings.HasSuffix(sel.Sel.Name, "Ordinary") {
					ordinaryCall = sel.Sel.Name
				}
			}
			return true
		})
		if charges {
			charging++
		}
		if !controls && (charges || controlRead) {
			problems = append(problems, fn.Name.Name+" charges or tests a control without work controls")
		}
		if !plain && ordinaryCall != "" {
			problems = append(problems, fn.Name.Name+" calls the ordinary helper "+ordinaryCall+" in a form with work controls or a Document")
		}
	}
	if controls && charging == 0 {
		problems = append(problems, "no rule body charges a step with work controls")
	}
	return problems, bodies, charging
}

func testGeneratedRuleBodiesHaveOneForm(t *testing.T) {
	g, err := parseTest(oneFormGrammar)
	if err != nil {
		t.Fatal(err)
	}
	for _, types := range []bool{false, true} {
		for _, document := range []bool{false, true} {
			opts := gen.GenOptions{Package: "ordinaryfixture", Types: types, Document: document, Recognize: true, Start: "main"}
			src, err := generateCode(g, opts)
			if err != nil {
				t.Fatal(err)
			}
			problems, bodies, charging := oneFormProblems(t, src, fixtureWorkControls(), document)
			for _, p := range problems {
				t.Errorf("types=%v document=%v: %s", types, document, p)
			}
			if bodies == 0 {
				t.Fatalf("types=%v document=%v: no rule bodies found", types, document)
			}
			t.Logf("types=%v document=%v: %d rule bodies, %d charging", types, document, bodies, charging)
		}
	}
}

// TestGeneratedRuleBodiesHaveOneFormNegativeControls checks that the check notices the other form:
// the ordinary form judged as controlled, the controlled form as ordinary, and a Document form
// that calls an ordinary helper.
func TestGeneratedRuleBodiesHaveOneFormNegativeControls(t *testing.T) {
	g, err := parseTest(oneFormGrammar)
	if err != nil {
		t.Fatal(err)
	}
	gen := func(controls, document bool) []byte {
		files, err := Generate(g, gen.GenOptions{Package: "ordinaryfixture", Module: "example.com/generated", Runtime: gen.RuntimeImport, Types: true, Document: document, Recognize: true, Start: "main", WorkControls: controls})
		if err != nil {
			t.Fatal(err)
		}
		return mergeGenerated(files)
	}
	plain, controlled, doc := gen(false, false), gen(true, false), gen(false, true)
	for _, c := range []struct {
		name               string
		src                []byte
		controls, document bool
		want               string
	}{
		{"the ordinary form as controlled", plain, true, false, "no rule body charges"},
		{"the controlled form as ordinary", controlled, false, false, "charges or tests a control"},
		{"the plain form as a Document form", plain, false, true, "calls the ordinary helper"},
		{"the controlled form with an Ordinary helper", append(controlled[:len(controlled):len(controlled)], []byte("\nfunc badBody(p *Parser) (*Node, bool) { return p.NewNodeOrdinary(\"x\", 0, nil), true }\n")...), true, false, "calls the ordinary helper"},
	} {
		problems, _, _ := oneFormProblems(t, c.src, c.controls, c.document)
		found := false
		for _, p := range problems {
			found = found || strings.Contains(p, c.want)
		}
		if !found {
			t.Errorf("%s: problems %q lack %q", c.name, problems, c.want)
		}
	}
	if problems, _, _ := oneFormProblems(t, doc, false, true); len(problems) != 0 {
		t.Errorf("the Document form: %q", problems)
	}
}

// parserFunction identifies a generated execution function by the parser type of its first
// parameter, which is named p.
type parserFunction struct{ receiver, name string }

// parserFunctionKey recognizes a rule body or other generated function whose first parameter
// is the parser p, a *Parser or a *TParser.
func parserFunctionKey(fn *goast.FuncDecl) (parserFunction, bool) {
	if fn.Recv != nil || fn.Type.Params == nil || len(fn.Type.Params.List) == 0 {
		return parserFunction{}, false
	}
	first := fn.Type.Params.List[0]
	ptr, ok := first.Type.(*goast.StarExpr)
	if !ok {
		return parserFunction{}, false
	}
	// The finished source names the runtime's types with its package, the execution source
	// without.
	var name string
	switch x := ptr.X.(type) {
	case *goast.Ident:
		name = x.Name
	case *goast.SelectorExpr:
		name = x.Sel.Name
	}
	if (name != "Parser" && name != "TParser") || len(first.Names) != 1 || first.Names[0].Name != "p" {
		return parserFunction{}, false
	}
	return parserFunction{name, fn.Name.Name}, true
}
