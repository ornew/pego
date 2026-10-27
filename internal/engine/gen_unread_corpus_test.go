package engine

import (
	goast "go/ast"
	goparser "go/parser"
	"go/token"
	"go/types"
	"regexp"
	"strings"
	"testing"
)

// localErrors type-checks the Go files of a generated package against empty stand-ins for its
// imports and returns the type errors, such as a variable that is declared and not used or a name
// that is undefined (a removed variable). The imported packages are empty, so errors about names
// taken from them, and about fields and methods of the types built from them, are not reported.
func localErrors(t *testing.T, files []GeneratedFile) []string {
	t.Helper()
	fset := token.NewFileSet()
	var parsed []*goast.File
	for _, f := range files {
		if !strings.HasSuffix(f.Path, ".go") || strings.Contains(f.Path, "/") {
			continue
		}
		pf, err := goparser.ParseFile(fset, f.Path, f.Content, 0)
		if err != nil {
			t.Fatalf("%s: %v", f.Path, err)
		}
		parsed = append(parsed, pf)
	}
	var out []string
	// The imported packages are empty, so a name taken from one is undefined.
	fromStandIn := regexp.MustCompile(`undefined: \w+\.\w+|has no field or method`)
	conf := types.Config{
		Importer: importerFunc(func(path string) (*types.Package, error) {
			name := path[strings.LastIndex(path, "/")+1:]
			p := types.NewPackage(path, name)
			p.MarkComplete()
			return p, nil
		}),
		Error: func(err error) {
			msg := err.Error()
			if !fromStandIn.MatchString(msg) {
				out = append(out, msg)
			}
		},
	}
	conf.Check("p", fset, parsed, nil)
	return out
}

type importerFunc func(path string) (*types.Package, error)

func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }

// blankLinesInFunctions returns the headers of the functions, other than init functions, that
// contain a blank line.
func blankLinesInFunctions(src string) []string {
	var out []string
	fn := ""
	for _, line := range strings.Split(src, "\n") {
		switch {
		case strings.HasPrefix(line, "func "):
			fn = ""
			if strings.HasSuffix(line, "{") && !strings.HasPrefix(line, "func init") {
				fn = line
			}
		case strings.HasPrefix(line, "}"):
			fn = ""
		case fn != "" && strings.TrimSpace(line) == "":
			out = append(out, fn)
			fn = ""
		}
	}
	return out
}

// The generated parsers of every test grammar, in the forms with and without work controls, declare
// every local variable they use and use every local variable they declare.
func TestGeneratedLocalVariables(t *testing.T) {
	forms := []struct {
		name  string
		opts  GenOptions
		blank bool // the form has no blank lines inside functions
	}{
		{"ordinary", GenOptions{Types: true}, true},
		{"work controls", GenOptions{Types: true, WorkControls: true}, false},
		{"document", GenOptions{Types: true, Document: true}, false},
		{"nodes", GenOptions{}, true},
	}
	checked := 0
	cases := genCorpus(t)
	for _, c := range cases {
		g, err := parseTest(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		for _, f := range forms {
			opts := f.opts
			opts.Package, opts.Start, opts.Runtime, opts.Module = "p", "main", RuntimeImport, "example.com/p"
			files, err := Generate(g, opts)
			if err != nil {
				// Every test grammar generates in every form.
				t.Errorf("%s (%s): %v", c.name, f.name, err)
				continue
			}
			checked++
			for _, e := range localErrors(t, files) {
				t.Errorf("%s (%s): %s", c.name, f.name, e)
			}
			for _, fl := range files {
				if fl.Path == "parser.go" && f.blank {
					for _, fn := range blankLinesInFunctions(string(fl.Content)) {
						t.Errorf("%s (%s): blank line in %s", c.name, f.name, fn)
					}
				}
			}
		}
	}
	if want := len(cases) * len(forms); checked != want {
		t.Errorf("checked %d generated packages, want %d", checked, want)
	}
}

// The check reports a variable that is declared and not used (negative control).
func TestLocalErrorsDetectsUnusedVariable(t *testing.T) {
	files := []GeneratedFile{{Path: "p.go", Content: []byte("package p\n\nfunc f() {\n\tvar x int\n\ty := 1\n\t_ = z\n}\n")}}
	got := localErrors(t, files)
	if len(got) != 3 {
		t.Fatalf("errors = %q, want an unused x, an unused y and an undefined z", got)
	}
}
