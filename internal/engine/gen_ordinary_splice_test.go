package engine

import (
	"bytes"
	"fmt"
	goast "go/ast"
	"go/format"
	goparser "go/parser"
	"go/scanner"
	"go/token"
	"strings"
	"testing"
)

// Compare token streams rather than positions changed by emitted formatting.
func ordinaryTokens(t *testing.T, node goast.Node) string {
	t.Helper()
	var b bytes.Buffer
	if err := format.Node(&b, token.NewFileSet(), node); err != nil {
		t.Fatal(err)
	}
	fs := token.NewFileSet()
	f := fs.AddFile("body.go", -1, b.Len())
	var scan scanner.Scanner
	scan.Init(f, b.Bytes(), func(pos token.Position, msg string) { t.Fatalf("%s: %s", pos, msg) }, 0)
	var result strings.Builder
	for {
		_, tok, lit := scan.Scan()
		if tok == token.EOF {
			break
		}
		fmt.Fprintf(&result, "%d:%q;", tok, lit)
	}
	return result.String()
}

func ordinaryFunctions(t *testing.T, src []byte) map[ordinaryMethodKey]*goast.FuncDecl {
	t.Helper()
	file, err := goparser.ParseFile(token.NewFileSet(), "goparser.go", src, goparser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[ordinaryMethodKey]*goast.FuncDecl)
	for _, decl := range file.Decls {
		if fn, ok := decl.(*goast.FuncDecl); ok {
			if key, ok := ordinaryMethodIdentity(fn); ok {
				out[key] = fn
			}
		}
	}
	return out
}

func TestGeneratedOrdinaryControlledBodiesPreserved(t *testing.T) {
	needsWorkControls(t)
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
		for _, convert := range []bool{false, true} {
			opts := GenOptions{Package: "ordinaryfixture", Types: types, Recognize: true, Start: "main", convertTypes: convert}
			specialized, err := generateCode(g, opts)
			if err != nil {
				t.Fatal(err)
			}
			opts.disableOrdinaryWorkSpecialization = true
			reference, err := generateCode(g, opts)
			if err != nil {
				t.Fatal(err)
			}
			actual, original := ordinaryFunctions(t, specialized), ordinaryFunctions(t, reference)
			branches := 0
			for key, want := range original {
				got := actual[key]
				if got == nil {
					t.Fatalf("missing original method %v", key)
				}
				if ordinaryTokens(t, got.Body) == ordinaryTokens(t, want.Body) {
					continue
				}
				if len(got.Body.List) < 2 {
					t.Fatalf("missing controlled body %v", key)
				}
				guard, ok := got.Body.List[0].(*goast.IfStmt)
				if !ok || guard.Else != nil || ordinaryTokens(t, guard.Cond) != ordinaryTokens(t, &goast.BinaryExpr{X: &goast.SelectorExpr{X: goast.NewIdent("p"), Sel: goast.NewIdent("Control")}, Op: token.EQL, Y: goast.NewIdent("nil")}) {
					t.Fatalf("unexpected dispatch %v", key)
				}
				tail := &goast.BlockStmt{List: got.Body.List[1:]}
				if ordinaryTokens(t, tail) != ordinaryTokens(t, want.Body) {
					t.Fatalf("controlled statements changed %v", key)
				}
				branches++
			}
			if branches == 0 {
				t.Fatal("no ordinary branches tested")
			}
			// The helpers the branches call are methods of the runtime.
			for _, name := range []string{"NewNodeOrdinary", "SetCaptureOrdinary", "ExpectOrdinary"} {
				if name == "NewNodeOrdinary" {
					// The nodes of matches and sequences are built by NewTerminal and NewBranch.
					name = "NewTerminalOrdinary"
				}
				if !strings.Contains(string(specialized), "."+name+"(") {
					t.Fatalf("missing helper %s", name)
				}
			}
		}
	}
}

// A Document parse records the input it examines, so a parser with Documents runs the ordinary
// branch only outside a Document.
func TestGeneratedOrdinaryGuardExcludesDocuments(t *testing.T) {
	needsWorkControls(t)
	g, err := parseTest(`rule main = "a" "b"`)
	if err != nil {
		t.Fatal(err)
	}
	for _, document := range []bool{false, true} {
		src, err := generateCode(g, GenOptions{Package: "ordinaryguard", Start: "main", Document: document})
		if err != nil {
			t.Fatal(err)
		}
		guarded := strings.Contains(string(src), "if p.Control == nil && p.Document == nil {")
		if guarded != document || !strings.Contains(string(src), "if p.Control == nil") {
			t.Fatalf("Document %v: unexpected ordinary guard", document)
		}
	}
}

// pairedFixture writes a function whose charge is a fragment, the way the printer does.
func pairedFixture(g *generator) string {
	return "func e0(p *Parser) (int, bool) {\n" + g.formRest("p.StepWork()\n", "", "") + "// marker\ns := \"} {\"\nreturn len(s), true\n}\n"
}

func TestSpliceOrdinaryBodiesPlacesTheOrdinaryForm(t *testing.T) {
	helper := `func (p *Parser) NewNode(v Node) *Node {
 p.StepWork()
 return &v
 }`
	g := &generator{pairs: true}
	paired := pairedFixture(g)
	controlled := selectForm(paired, formControlled)
	// Large original offsets must not interpret positions of the ordinary text in the file set of
	// the controlled text.
	source := "package fixture\n" + strings.Repeat("// padding\n", 20000) + controlled + "\n" + helper
	result, err := spliceOrdinaryBodies([]byte(source), "fixture", paired, controlled, "p.Control == nil", false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(result), "// marker") != 1 {
		t.Fatal("controlled comment lost or duplicated")
	}
	if strings.Contains(string(result), "func (p *Parser) NewNodeOrdinary") {
		t.Fatal("derived an ordinary helper")
	}
	if got, want := strings.TrimSpace(string(result[strings.Index(string(result), "func (p *Parser) NewNode("):])), "func (p *Parser) NewNode(v Node) *Node {\n\tp.StepWork()\n\treturn &v\n}"; got != want {
		t.Fatalf("unexpected helper formatting:\n%s", got)
	}
	f := ordinaryFunctions(t, result)[ordinaryMethodKey{"Parser", "e0"}]
	ordinary := f.Body.List[0].(*goast.IfStmt).Body
	if ordinaryTokens(t, ordinary) != ordinaryTokens(t, &goast.BlockStmt{List: f.Body.List[1:][1:]}) {
		// The controlled-only charge is absent from the ordinary branch.
		t.Fatal("brace-like literal or return expression changed")
	}
}

func TestSpliceOrdinaryBodiesFailsOnEmitterDrift(t *testing.T) {
	g := &generator{pairs: true}
	paired := pairedFixture(g)
	controlled := selectForm(paired, formControlled)
	other := `func e1(p *Parser)(int,bool){return 0,true}`
	for _, tc := range []struct{ name, src, paired, methods, want string }{
		{"function missing from the source", "package fixture", paired, controlled, "missing generated function Parser.e0"},
		{"no execution function", "package fixture\n" + controlled, paired, "", "missing generated execution functions"},
		{"renamed parameter", "package fixture\n" + controlled, paired, `func e0(q *Parser)(int,bool){return 0,true}`, "missing generated execution functions"},
		{"duplicate execution function", "package fixture\n" + controlled, paired, controlled + "\n" + controlled, "duplicate generated execution function e0"},
		{"function missing from the source of another", "package fixture\n" + controlled, paired, controlled + other, "missing generated function Parser.e1"},
		{"function missing from the paired text", "package fixture\n" + controlled, other, controlled, "missing ordinary form of generated function e0"},
	} {
		_, err := spliceOrdinaryBodies([]byte(tc.src), "fixture", tc.paired, tc.methods, "p.Control == nil", false)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %v, want %q", tc.name, err, tc.want)
		}
	}
}

// A parser without work controls has no controlled body next to the branch: a Document runs the rest
// form of the body.
func TestSpliceOrdinaryBodiesDocumentOnly(t *testing.T) {
	g := &generator{pairs: true}
	paired := "func e0(p *Parser) (int, bool) {\n" + g.formRest("p.StepWork()\n", "", "") +
		g.formRest("n := p.Control\n", "n := nilControl\n", "n := restControl\n") + "return 0, true\n}\n"
	controlled := selectForm(paired, formControlled)
	result, err := spliceOrdinaryBodies([]byte("package fixture\n"+controlled), "fixture", paired, controlled, "p.Document == nil", true)
	if err != nil {
		t.Fatal(err)
	}
	f := ordinaryFunctions(t, result)[ordinaryMethodKey{"Parser", "e0"}]
	branch, ok := f.Body.List[0].(*goast.IfStmt)
	if !ok || !strings.Contains(ordinaryTokens(t, branch.Cond), "Document") {
		t.Fatalf("the body does not start with the branch under p.Document == nil:\n%s", result)
	}
	got := ordinaryTokens(t, branch.Body)
	if !strings.Contains(got, "nilControl") || strings.Contains(got, "restControl") || strings.Contains(got, "Control ;") {
		t.Errorf("unexpected ordinary branch: %s", got)
	}
	rest := ordinaryTokens(t, &goast.BlockStmt{List: f.Body.List[1:]})
	if !strings.Contains(rest, "restControl") || strings.Contains(rest, "StepWork") || strings.Contains(rest, "nilControl") {
		t.Errorf("unexpected rest form: %s", rest)
	}
	if len(f.Body.List) != 3 {
		t.Errorf("the body has %d statements, want the branch and the two of the rest form", len(f.Body.List))
	}
}

func TestSpliceOrdinaryBodiesMissingRestForm(t *testing.T) {
	// A fragment that holds a whole function lets the forms disagree about which functions exist.
	fn := func(name string) string { return "func " + name + "(p *Parser) (int, bool) {\nreturn 0, true\n}\n" }
	paired := formStart + fn("e0") + formOrdinary + fn("e0") + formRest + fn("e1") + formEnd
	controlled := selectForm(paired, formControlled)
	_, err := spliceOrdinaryBodies([]byte("package fixture\n"+controlled), "fixture", paired, controlled, "p.Document == nil", true)
	if err == nil || !strings.Contains(err.Error(), "missing rest form of generated function e0") {
		t.Errorf("error %v, want the missing rest form", err)
	}
}
