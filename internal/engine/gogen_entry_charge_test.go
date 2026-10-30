package engine

import (
	"bytes"
	goast "go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestGeneratedRuleEntryCharge checks, on the checked-in parsers generated with work controls, that every
// matcher function of the Node and typed tables (rule bodies, framed bodies, flat-rule methods and the
// lines and skips of Operators Rules) begins with one Work and has no other Work before its first
// loop; a typed wrapper u<N> or i<N> that only saves and restores state around a call of another matcher
// function charges nothing itself. The lowered function of Flow IR charges no entry of its own (flow.Func.Entered); the method that holds it does, so a rule
// whose method lost the charge would consume input uncharged without Verify noticing.
func TestGeneratedRuleEntryCharge(t *testing.T) {
	t.Parallel()
	name := regexp.MustCompile(`^[eiqrsuv][0-9]+$`)
	wrapper := regexp.MustCompile(`^[iu][0-9]+$`) // typed wrappers: state around a call of the charging function
	var paths []string
	for _, pattern := range []string{"../../parsers/*/parser.go", "../bench/gen/*/parser.go"} {
		m, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range m {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(src, []byte(".Work()")) {
				paths = append(paths, path)
			}
		}
	}
	if len(paths) == 0 {
		t.Fatal("no checked-in parser with work controls found")
	}
	for _, path := range paths {
		f, err := goparser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, d := range f.Decls {
			fn, ok := d.(*goast.FuncDecl)
			if !ok || fn.Recv != nil || !name.MatchString(fn.Name.Name) || fn.Type.Results == nil || len(fn.Type.Results.List) != 2 {
				continue
			}
			n++
			if !startsWithStep(fn.Body) && !(wrapper.MatchString(fn.Name.Name) && delegates(fn.Body, name) && !hasStep(fn.Body)) {
				t.Errorf("%s: %s does not begin with p.Work() or delegate to a matcher function that does", path, fn.Name.Name)
			}
			steps := 0
			goast.Inspect(fn.Body, func(x goast.Node) bool {
				if _, loop := x.(*goast.ForStmt); loop {
					return false
				}
				if isStepCall(x) {
					steps++
				}
				return true
			})
			if steps > 1 {
				t.Errorf("%s: %s charges %d steps outside its loops, want the entry only", path, fn.Name.Name, steps)
			}
		}
		if n == 0 {
			t.Errorf("%s: no matcher functions found", path)
		}
	}
}

func isStepCall(x goast.Node) bool {
	c, ok := x.(*goast.CallExpr)
	if !ok {
		return false
	}
	s, ok := c.Fun.(*goast.SelectorExpr)
	return ok && s.Sel.Name == "Work"
}

func startsWithStep(b *goast.BlockStmt) bool {
	for _, s := range b.List {
		if e, ok := s.(*goast.ExprStmt); ok {
			return isStepCall(e.X)
		}
		if _, ok := s.(*goast.DeclStmt); ok {
			continue
		}
		return false
	}
	return false
}

// hasStep reports whether b contains a Work call.
func hasStep(b *goast.BlockStmt) bool {
	found := false
	goast.Inspect(b, func(x goast.Node) bool {
		found = found || isStepCall(x)
		return !found
	})
	return found
}

// delegates reports whether b calls a function named like a matcher function: the typed wrappers of a rule
// (the wrappers u<N> and i<N>) save and restore state around a call of the function that charges.
func delegates(b *goast.BlockStmt, name *regexp.Regexp) bool {
	found := false
	goast.Inspect(b, func(x goast.Node) bool {
		if c, ok := x.(*goast.CallExpr); ok {
			if id, ok := c.Fun.(*goast.Ident); ok && name.MatchString(id.Name) {
				found = true
			}
		}
		return !found
	})
	return found
}
