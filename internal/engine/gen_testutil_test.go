package engine

import (
	"bytes"
	goast "go/ast"
	goparser "go/parser"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ornew/pego/grammar"
)

// The engine tests build the programs they generate in temporary directories. The generated code
// imports the runtime package of this module, so their go.mod files require and replace it.

// fixtureWorkControls reports whether the generated fixtures have work controls. They do by default, so
// that the tests of cancellation and limits can use them. With PEGO_TEST_ORDINARY=1 they are generated
// without, the form that most parsers have, and the tests that use a control (WithContext,
// WithWorkLimits) skip (see needsWorkControls); the rest of the suite then checks the ordinary-only output.
func fixtureWorkControls() bool { return os.Getenv("PEGO_TEST_ORDINARY") == "" }

// needsWorkControls skips a test that passes a control option to its fixtures when the fixtures are
// generated without work controls.
func needsWorkControls(t testing.TB) {
	t.Helper()
	if !fixtureWorkControls() {
		t.Skip("uses work controls; the fixtures are generated without them")
	}
}

func init() {
	SetRuntimeSource(os.DirFS(filepath.Join("..", "..", "runtime")))
}

// repoRoot returns the root directory of this module.
func repoRoot() string {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		panic(err)
	}
	return root
}

var (
	runtimeCopyOnce sync.Once
	runtimeCopyDir  string
)

// runtimeCopy returns the root of a module that holds a copy of the runtime package with the
// identifiers that testdata/export_names.txt lists renamed (see exportNames).
func runtimeCopy() string {
	runtimeCopyOnce.Do(func() {
		dir, err := os.MkdirTemp("", "pegort-copy")
		if err != nil {
			panic(err)
		}
		writeRuntimeCopy(dir, nil, nil)
		runtimeCopyDir = dir
	})
	return runtimeCopyDir
}

// runtimeCopyWith returns a copy of the runtime like runtimeCopy that has patch applied to the
// source of each file (by name) and the extra files, which are given as name and source.
func runtimeCopyWith(t testing.TB, patch func(name, src string) string, extra map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	writeRuntimeCopy(dir, patch, extra)
	return dir
}

func writeRuntimeCopy(dir string, patch func(name, src string) string, extra map[string]string) {
	must := func(err error) {
		if err != nil {
			panic(err)
		}
	}
	must(os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/ornew/pego\n\ngo 1.27.1\n"), 0o644))
	rt := filepath.Join(dir, "runtime")
	must(os.MkdirAll(rt, 0o755))
	src := filepath.Join(repoRoot(), "runtime")
	entries, err := os.ReadDir(src)
	must(err)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		must(err)
		if patch != nil {
			data = []byte(patch(e.Name(), string(data)))
		}
		must(os.WriteFile(filepath.Join(rt, e.Name()), exportNames(data), 0o644))
	}
	must(os.WriteFile(filepath.Join(rt, "zz_testing.go"), exportNames([]byte(runtimeTestingShim)), 0o644))
	for name, data := range extra {
		must(os.WriteFile(filepath.Join(rt, name), exportNames([]byte(data)), 0o644))
	}
}

// testGoMod returns the go.mod of a test module that imports the runtime from a copy of this
// checkout's runtime package.
func testGoMod(module string) string {
	return testGoModAt(module, runtimeCopy())
}

// testGoModAt returns the go.mod of a test module that imports the runtime from the module in dir.
func testGoModAt(module, dir string) string {
	return "module " + module + "\n\ngo 1.27.1\n\nrequire github.com/ornew/pego v0.0.0\n\nreplace github.com/ornew/pego => " + dir + "\n"
}

// writeGenerated writes the files of a generated package into dir. A go.mod for the module is
// written when dir has none.
func writeGenerated(t testing.TB, dir, module string, files []GeneratedFile) {
	t.Helper()
	for _, f := range files {
		path := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, f.Content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mod := filepath.Join(dir, "go.mod")
	if _, err := os.Stat(mod); err != nil {
		if err := os.WriteFile(mod, []byte(testGoMod(module)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// generatePackage generates a parser that imports the runtime from this module as the package
// gentest/<pkg>, writes its files (parser.go, api.go and types/types.go) below dir/<pkg> in the
// module of dir, and returns its parser.go. Unlike generateCode, it keeps the types package
// apart, so that grammar types named like the names of the parser package compile.
func generatePackage(t testing.TB, dir, pkg string, g *grammar.Grammar, opts GenOptions) string {
	t.Helper()
	return generatePackageIn(t, dir, "gentest", pkg, g, opts)
}

// generatePackageIn is generatePackage for a test module whose path is root.
func generatePackageIn(t testing.TB, dir, root, pkg string, g *grammar.Grammar, opts GenOptions) string {
	t.Helper()
	opts.Runtime = RuntimeImport
	opts.WorkControls = fixtureWorkControls()
	opts.Package, opts.Module = pkg, root+"/"+pkg
	opts.keepComments = true
	files, err := Generate(g, opts)
	if err != nil {
		t.Fatal(err)
	}
	var parser string
	for _, f := range files {
		path := filepath.Join(dir, pkg, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, f.Content, 0o644); err != nil {
			t.Fatal(err)
		}
		if f.Path == "parser.go" {
			parser = string(f.Content)
		}
	}
	return parser
}

// generateCode generates a parser that imports the runtime from this module and returns its
// parser.go and api.go as one source file, which tests write as parser.go.
func generateCode(g *grammar.Grammar, opts GenOptions) ([]byte, error) {
	opts.Runtime = RuntimeImport
	opts.keepComments = true
	opts.WorkControls = fixtureWorkControls()
	if opts.Module == "" && opts.Types {
		opts.Module = "example.com/generated"
	}
	files, err := Generate(g, opts)
	if err != nil {
		return nil, err
	}
	return mergeGenerated(files), nil
}

// mergeGenerated joins the Go files of a generated package into one file. The types package of
// a typed parser (types/types.go) joins the file too, with the references to it unqualified, so
// that the fixtures of the tests name the types without importing the package.
func mergeGenerated(files []GeneratedFile) []byte {
	var parts []GeneratedFile
	for _, f := range files {
		switch {
		case f.Path == "types/types.go":
			parts = append(parts, f)
		case strings.HasSuffix(f.Path, ".go") && !strings.Contains(f.Path, "/"):
			f.Content = unqualifyTypes(f.Content)
			parts = append(parts, f)
		}
	}
	if len(parts) == 1 {
		return parts[0].Content
	}
	var imports []string
	seen := map[string]bool{}
	var bodies [][]byte
	var head []byte
	for i, f := range parts {
		fset := token.NewFileSet()
		file, err := goparser.ParseFile(fset, f.Path, f.Content, goparser.ImportsOnly|goparser.ParseComments)
		if err != nil {
			panic(err)
		}
		src := f.Content
		type span struct{ lo, hi int }
		var cut []span
		for _, imp := range file.Imports {
			if imp.Name != nil && imp.Name.Name == typesAlias {
				continue
			}
			text := string(src[fset.Position(imp.Pos()).Offset:fset.Position(imp.End()).Offset])
			if !seen[text] {
				seen[text] = true
				imports = append(imports, text)
			}
		}
		// The import declarations occupy the region from the first to the last import spec,
		// widened to the declaration keywords and parentheses.
		pkgEnd := fset.Position(file.Name.End()).Offset
		rest := src[pkgEnd:]
		start, end := importRegion(rest)
		cut = append(cut, span{pkgEnd + start, pkgEnd + end})
		if i == 0 {
			head = append(head, src[:pkgEnd]...)
		}
		var body []byte
		body = append(body, src[pkgEnd:cut[0].lo]...)
		body = append(body, src[cut[0].hi:]...)
		bodies = append(bodies, body)
	}
	var out bytes.Buffer
	out.Write(head)
	out.WriteString("\n\nimport (\n")
	for _, s := range imports {
		out.WriteString("\t" + s + "\n")
	}
	out.WriteString(")\n")
	for _, b := range bodies {
		out.Write(b)
		out.WriteString("\n")
	}
	return out.Bytes()
}

// unqualifyTypes removes the qualifier of the references to the types package (types.Name) from
// the source of a file of the parser package.
func unqualifyTypes(src []byte) []byte {
	fset := token.NewFileSet()
	file, err := goparser.ParseFile(fset, "src.go", src, goparser.ParseComments)
	if err != nil {
		panic(err)
	}
	var cuts [][2]int
	goast.Inspect(file, func(n goast.Node) bool {
		if sel, ok := n.(*goast.SelectorExpr); ok {
			if id, ok := sel.X.(*goast.Ident); ok && id.Name == typesAlias && id.Obj == nil {
				cuts = append(cuts, [2]int{fset.Position(id.Pos()).Offset, fset.Position(sel.Sel.Pos()).Offset})
			}
		}
		return true
	})
	for i := len(cuts) - 1; i >= 0; i-- {
		src = append(src[:cuts[i][0]:cuts[i][0]], src[cuts[i][1]:]...)
	}
	return src
}

// runtimeSource is the text of the runtime package, for tests that check its source.
var runtimeSource = func() string {
	paths, err := filepath.Glob(filepath.Join("..", "..", "runtime", "*.go"))
	if err != nil {
		panic(err)
	}
	var b strings.Builder
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			panic(err)
		}
		b.Write(data)
	}
	return b.String()
}()

// importRegion returns the offsets of the run of import declarations at the start of rest, which
// follows the package clause.
func importRegion(rest []byte) (lo, hi int) {
	s := string(rest)
	i := strings.Index(s, "import")
	if i < 0 {
		return 0, 0
	}
	lo = i
	pos := i
	for {
		trimmed := strings.TrimLeft(s[pos:], " \t\r\n")
		if !strings.HasPrefix(trimmed, "import") {
			break
		}
		at := len(s) - len(trimmed)
		after := strings.TrimLeft(s[at+len("import"):], " \t")
		if strings.HasPrefix(after, "(") {
			close := strings.Index(s[at:], "\n)")
			pos = at + close + 2
		} else {
			nl := strings.Index(s[at:], "\n")
			pos = at + nl
		}
		hi = pos
	}
	return lo, hi
}

// exportedNames returns the identifiers that testdata/export_names.txt lists. Fixtures that check
// the internals of the runtime refer to them with the prefix X_.
func exportedNames() map[string]bool {
	data, err := os.ReadFile(filepath.Join("testdata", "export_names.txt"))
	if err != nil {
		panic(err)
	}
	names := map[string]bool{}
	for _, f := range strings.Fields(string(data)) {
		names[f] = true
	}
	return names
}

// exportNames renames the identifiers of src that exportedNames lists to X_name, which makes the
// package-level declarations, fields and methods of the runtime copy reachable from the
// fixtures. The renaming is consistent within the package, so it does not change what the code does.
func exportNames(src []byte) []byte {
	names := exportedNames()
	fset := token.NewFileSet()
	file := fset.AddFile("", fset.Base(), len(src))
	var sc scanner.Scanner
	sc.Init(file, src, nil, scanner.ScanComments)
	var out bytes.Buffer
	last := 0
	for {
		pos, tok, lit := sc.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.IDENT && names[lit] {
			off := file.Offset(pos)
			out.Write(src[last:off])
			out.WriteString("X_" + lit)
			last = off + len(lit)
		}
	}
	out.Write(src[last:])
	return out.Bytes()
}

// runtimeTestingShim is added to every runtime copy for the tests that set the options of a parse
// directly.
const runtimeTestingShim = `package runtime

import "context"

// SetContext sets the context of the parse.
func (o *parseOptions) SetContext(c context.Context) { o.context = c }
`
