package parsers_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/gen"
)

// helperName matches the names of the generated helpers: eN are the methods of expressions and qN the
// functions of rules that a recognizer calls without a method of its own.
var helperName = regexp.MustCompile(`^[eq][0-9]+$`)

// unreferencedHelpers returns the eN and qN functions of the Go file src that no identifier of the file
// refers to, other than the function's own name.
func unreferencedHelpers(t *testing.T, name string, src []byte) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	uses := map[string]int{}
	ast.Inspect(f, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			uses[id.Name]++
		}
		return true
	})
	var unused []string
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || !helperName.MatchString(fn.Name.Name) {
			continue
		}
		if uses[fn.Name.Name] == 1 {
			unused = append(unused, fn.Name.Name)
		}
	}
	sort.Strings(unused)
	return unused
}

// TestRecognizerHelpersReferenced checks that the node recognizer of a grammar prints no helper that
// nothing calls. The recognizer's rules of twinned originals and of the start rule have no call site
// (their calls go to the value-free twin or to the entry point), so a wrapper of theirs is dead.
func TestRecognizerHelpersReferenced(t *testing.T) {
	if testing.Short() {
		t.Skip("generates the duckdb grammar")
	}
	for _, g := range []struct{ name, path string }{
		{"json", "json/json.zp"},
		{"duckdb", "duckdb/duckdb.zp"},
	} {
		src, err := os.ReadFile(g.path)
		if err != nil {
			t.Fatal(err)
		}
		gr, err := zeroparse.ParseGrammar(string(src))
		if err != nil {
			t.Fatalf("%s: %v", g.path, err)
		}
		files, err := gen.Go{Module: "gentest/" + g.name, Runtime: gen.RuntimeImport, Recognize: true}.Generate(gr, "")
		if err != nil {
			t.Fatalf("%s: %v", g.path, err)
		}
		for _, f := range files {
			if filepath.Ext(f.Path) != ".go" {
				continue
			}
			if unused := unreferencedHelpers(t, f.Path, f.Content); len(unused) > 0 {
				t.Errorf("%s node recognizer %s: %d unreferenced helpers %v", g.name, f.Path, len(unused), unused)
			}
		}
	}
}
