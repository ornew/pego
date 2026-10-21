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

	"github.com/ornew/pego/internal/syntax"
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
	g, err := syntax.Parse(`type Text = { Text string }
rule main Text = (a:("a"* #atomic) "b") #recover(skip="?") => Text{Text: text($a)}
rule recursive Text = t:(recursive "+" "a" / "a") #atomic => Text{Text: text($t)}
rule expr Text = operators {
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
	g, err := syntax.Parse(`rule main = "a" "b"`)
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

func TestOrdinaryTransformLabelsAndSynchronousLambda(t *testing.T) {
	method := `func e0(p *Parser) (int,bool) {
 p.StepWork()
 c := p.UseCtx(ActCtx{P:p})
 _ = func() int {p.InputWork(1); if c.P.Control != nil {return 1}; return 0}()
 loop: for {if p.Pos>0 {break loop}; p.Pos++; goto done}
 done: return p.Pos,true
 }`
	src, err := specializeOrdinaryMethods([]byte("package fixture\n"+method), method, "p.Control == nil", false)
	if err != nil {
		t.Fatal(err)
	}
	f := ordinaryFunctions(t, src)[ordinaryMethodKey{"Parser", "e0"}]
	guard := f.Body.List[0].(*goast.IfStmt)
	labels, branches := map[string]bool{}, 0
	goast.Inspect(guard.Body, func(n goast.Node) bool {
		switch x := n.(type) {
		case *goast.LabeledStmt:
			labels[x.Label.Name] = true
		case *goast.BranchStmt:
			if x.Label != nil {
				if !strings.HasSuffix(x.Label.Name, "_ordinary") {
					t.Fatal("unrenamed branch")
				}
				branches++
			}
		case *goast.CallExpr:
			if s, ok := x.Fun.(*goast.SelectorExpr); ok && (s.Sel.Name == "StepWork" || s.Sel.Name == "InputWork") {
				t.Fatal("retained work call")
			}
		case *goast.SelectorExpr:
			if ordinaryControl(x) {
				t.Fatal("retained control read")
			}
		}
		return true
	})
	if !labels["loop_ordinary"] || !labels["done_ordinary"] || branches != 2 {
		t.Fatal("label coverage")
	}
}

func TestOrdinaryTransformFailsOnEmitterDrift(t *testing.T) {
	method := `func e0(p *Parser)(int,bool){return 0,true}`
	for _, tc := range []struct{ src, methods string }{
		{"package fixture", method},
		{"package fixture\n" + method, ""},
		{"package fixture\nfunc e0(p *Parser)(int,bool){p.Control=nil;return 0,true}", method},
		{"package fixture\n" + method, `func e0(q *Parser)(int,bool){return 0,true}`},
		{"package fixture\nfunc e0(p *Parser)(int,bool){q:=&p.Control;_=q;return 0,true}", method},
		{"package fixture\nfunc e0(p *Parser)(int,bool){p:=other;_=p;return 0,true}", method},
		{"package fixture\n" + method, method + "\n" + method},
	} {
		if _, err := specializeOrdinaryMethods([]byte(tc.src), tc.methods, "p.Control == nil", false); err == nil {
			t.Fatal("accepted emitter drift")
		}
	}
}

func TestOrdinaryTransformPreservesCommentsAndFormatting(t *testing.T) {
	method := `func e0(p *Parser) (int, bool) {
 // controlled marker
 p.StepWork()
 s := "} {"
 return len(s), true
 }`
	helper := `func (p *Parser) NewNode(v Node) *Node {
 p.StepWork()
 return &v
 }`
	// Large original offsets must not interpret clone positions in the
	// original FileSet or scatter a short helper across artificial lines.
	source := "package fixture\n" + strings.Repeat("// padding\n", 20000) + method + "\n" + helper
	result, err := specializeOrdinaryMethods([]byte(source), method, "p.Control == nil", false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(result), "// controlled marker") != 1 {
		t.Fatal("controlled comment lost or duplicated")
	}
	// The runtime defines the ordinary helpers; the transform leaves runtime methods as they are.
	if strings.Contains(string(result), "func (p *Parser) NewNodeOrdinary") {
		t.Fatal("derived an ordinary helper")
	}
	if got, want := strings.TrimSpace(string(result[strings.Index(string(result), "func (p *Parser) NewNode("):])), "func (p *Parser) NewNode(v Node) *Node {\n\tp.StepWork()\n\treturn &v\n}"; got != want {
		t.Fatalf("unexpected helper formatting:\n%s", got)
	}
	f := ordinaryFunctions(t, result)[ordinaryMethodKey{"Parser", "e0"}]
	ordinary := f.Body.List[0].(*goast.IfStmt).Body
	if ordinaryTokens(t, ordinary) != ordinaryTokens(t, &goast.BlockStmt{List: f.Body.List[1:][1:]}) {
		// The controlled-only initial work call is absent from the ordinary branch.
		t.Fatal("brace-like literal or return expression changed")
	}
}
