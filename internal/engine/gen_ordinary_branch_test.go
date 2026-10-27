package engine

import (
	"bytes"
	goast "go/ast"
	"go/format"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ordinaryBranchProblems reports what an ordinary branch of the generated source may not contain: a
// read of the control, a charge or a recording helper. The only charge left is the one directly
// after a label, which the ordinary form keeps. Every label of the branch ends in _ordinary.
func ordinaryBranchProblems(t *testing.T, src []byte, guard string) (problems []string, branches int) {
	t.Helper()
	for key, fn := range ordinaryFunctions(t, src) {
		if len(fn.Body.List) == 0 {
			continue
		}
		branch, ok := fn.Body.List[0].(*goast.IfStmt)
		var cond bytes.Buffer
		if ok {
			format.Node(&cond, token.NewFileSet(), branch.Cond)
		}
		if !ok || cond.String() != guard {
			problems = append(problems, key.name+": no branch under "+guard)
			continue
		}
		branches++
		problem := func(what string) { problems = append(problems, key.name+": "+what) }
		afterLabel := map[goast.Node]bool{}
		goast.Inspect(branch.Body, func(n goast.Node) bool {
			if l, ok := n.(*goast.LabeledStmt); ok {
				afterLabel[l.Stmt] = true
			}
			return true
		})
		goast.Inspect(branch.Body, func(n goast.Node) bool {
			switch x := n.(type) {
			case *goast.SelectorExpr:
				if x.Sel.Name == "Control" {
					problem("reads the control")
				}
			case *goast.Ident:
				if x.Name == "RejectWorkClass" || x.Name == "WorkControlsEnabled" {
					problem("uses " + x.Name)
				}
			case *goast.CallExpr:
				sel, ok := x.Fun.(*goast.SelectorExpr)
				if !ok {
					break
				}
				if parserReceiver(sel.X) && ordinaryHelperName(sel.Sel.Name) {
					problem("calls the recording " + sel.Sel.Name)
				}
				if sel.Sel.Name == "InputWork" || sel.Sel.Name == "StepWork" && !afterLabel[n] {
					problem("charges " + sel.Sel.Name)
				}
			case *goast.ExprStmt:
				// A charge directly after a label is the one the ordinary form keeps.
				if call, ok := x.X.(*goast.CallExpr); ok && afterLabel[x] {
					afterLabel[call] = true
				}
			case *goast.LabeledStmt:
				if !strings.HasSuffix(x.Label.Name, "_ordinary") {
					problem("label " + x.Label.Name)
				}
			case *goast.BranchStmt:
				if x.Label != nil && !strings.HasSuffix(x.Label.Name, "_ordinary") {
					problem("goto " + x.Label.Name)
				}
			}
			return true
		})
	}
	return problems, branches
}

func ordinaryBranchCorpus(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, path := range []string{"examples/calculator/calc.pego", "examples/minilang/minilang.pego", "examples/outline/outline.pego", "parsers/json/json.pego", "parsers/csv/csv.pego", "parsers/cel/cel.pego", "parsers/xml/xml.pego"} {
		b, err := os.ReadFile(filepath.Join(repoRoot(), path))
		if err != nil {
			t.Fatal(err)
		}
		out[path] = string(b)
	}
	return out
}

func checkOrdinaryBranches(t *testing.T) (problems []string, branches int) {
	t.Helper()
	forms := []struct {
		name  string
		opts  GenOptions
		guard string
	}{
		{"work controls", GenOptions{WorkControls: true}, "p.Control == nil"},
		{"work controls, types, recognize", GenOptions{WorkControls: true, Types: true, Recognize: true}, "p.Control == nil"},
		{"work controls, Document", GenOptions{WorkControls: true, Document: true}, "p.Control == nil && p.Document == nil"},
		{"work controls, Document, types", GenOptions{WorkControls: true, Document: true, Types: true}, "p.Control == nil && p.Document == nil"},
		{"Document", GenOptions{Document: true}, "p.Document == nil"},
		{"Document, types, recognize", GenOptions{Document: true, Types: true, Recognize: true}, "p.Document == nil"},
	}
	for path, src := range ordinaryBranchCorpus(t) {
		g, err := parseTest(src)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range forms {
			opts := f.opts
			opts.Package, opts.Runtime, opts.Module = "branchcheck", RuntimeImport, "example.com/generated"
			files, err := Generate(g, opts)
			if err != nil {
				t.Fatalf("%s %s: %v", path, f.name, err)
			}
			for _, file := range files {
				if file.Path != "parser.go" {
					continue
				}
				ps, n := ordinaryBranchProblems(t, file.Content, f.guard)
				for _, p := range ps {
					problems = append(problems, path+" ["+f.name+"] "+p)
				}
				branches += n
			}
		}
	}
	return problems, branches
}

// The ordinary branch of every execution function of a parser with work controls or Documents has no
// control read, charge or recording helper, and its labels are its own.
func TestGeneratedOrdinaryBranchesAreOrdinary(t *testing.T) {
	problems, branches := checkOrdinaryBranches(t)
	if branches == 0 {
		t.Fatal("no ordinary branch was checked")
	}
	for _, p := range problems {
		t.Error(p)
	}
}

// Breaking the ordinary alternative of one kind of fragment must be noticed.
func TestGeneratedOrdinaryBranchesNegativeControls(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutant func(controlled, ordinary string) string
		want   string
	}{
		{"class reject keeps the control", func(c, o string) string {
			if strings.Contains(c, "RejectWorkClass") {
				return c
			}
			return o
		}, "reads the control"},
		{"helper not renamed", func(c, o string) string {
			if o == c+"Ordinary" && ordinaryHelperName(c) {
				return c
			}
			return o
		}, "calls the recording"},
		{"charge kept", func(c, o string) string {
			if o == "" && strings.Contains(c, "StepWork") {
				return c
			}
			return o
		}, "charges StepWork"},
		{"label not renamed", func(c, o string) string {
			if o == c+"_ordinary" {
				return c
			}
			return o
		}, "label "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			formMutant = tc.mutant
			defer func() { formMutant = nil }()
			problems, _ := checkOrdinaryBranches(t)
			for _, p := range problems {
				if strings.Contains(p, tc.want) {
					return
				}
			}
			t.Fatalf("the mutant was not noticed (%d other problems)", len(problems))
		})
	}
}

// parserReceiver reports whether x is the parser of a generated function, p or c.P.
func parserReceiver(x goast.Expr) bool {
	if id, ok := x.(*goast.Ident); ok {
		return id.Name == "p"
	}
	s, ok := x.(*goast.SelectorExpr)
	if !ok || s.Sel.Name != "P" {
		return false
	}
	id, ok := s.X.(*goast.Ident)
	return ok && id.Name == "c"
}
