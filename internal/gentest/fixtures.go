package gentest

import (
	"bytes"
	goast "go/ast"
	goparser "go/parser"
	"go/scanner"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gen/gogen"
)

// The generated fixtures are built in two forms: with budget controls, so that the tests of
// cancellation and limits can use them, and without, the form that most parsers have. Tests that
// build fixtures run in both forms (EachMode); tests that use a control (WithContext,
// WithLimits) run with controls only (NeedsBudgetControls); tests of the ordinary-only output
// pin the ordinary form (PinOrdinary).
//
// A form is "0" with budget controls and "1" without. Each test records its form by name, and its
// subtests inherit it, so that tests in either form run in parallel. Set in the environment of go
// test, modeEnv restricts the run to one form: EachMode then runs its body once, in that form,
// and the tests that need controls skip under "1".
const modeEnv = "ZP_TEST_ORDINARY"

// EnvForm is the form to which the environment restricts the run, or "".
var EnvForm = os.Getenv(modeEnv)

// fixtureForms maps the name of a test to the form it set.
var fixtureForms sync.Map

// SetFixtureForm sets the form of the fixtures of t and its subtests.
func SetFixtureForm(t testing.TB, form string) {
	name := t.Name()
	fixtureForms.Store(name, form)
	t.Cleanup(func() { fixtureForms.Delete(name) })
}

// FixtureForm returns the form of the fixtures of t: the one that t or the nearest test above it
// set, else the one of the environment, else "".
func FixtureForm(t testing.TB) string {
	if form, ok := byTest(&fixtureForms, t); ok {
		return form.(string)
	}
	return EnvForm
}

// byTest returns the value that m holds for t or, failing that, for the nearest test above it.
func byTest(m *sync.Map, t testing.TB) (any, bool) {
	for name := t.Name(); ; {
		if v, ok := m.Load(name); ok {
			return v, true
		}
		i := strings.LastIndexByte(name, '/')
		if i < 0 {
			return nil, false
		}
		name = name[:i]
	}
}

// genHooks maps the name of a test to the hooks of the generator that it set.
var genHooks sync.Map

// SetGenHooks makes the Go parsers that t and its subtests generate use h, so that tests that
// observe the generator run in parallel.
func SetGenHooks(t testing.TB, h *gogen.Hooks) {
	name := t.Name()
	genHooks.Store(name, h)
	t.Cleanup(func() { genHooks.Delete(name) })
}

// testGenHooks returns the hooks of the generator for t, or nil.
func testGenHooks(t testing.TB) *gogen.Hooks {
	if h, ok := byTest(&genHooks, t); ok {
		return h.(*gogen.Hooks)
	}
	return nil
}

// FixtureBudgetControls reports whether the generated fixtures of t have budget controls. A test that
// builds fixtures reaches it inside EachMode, NeedsBudgetControls or a pinned form; one that does
// not is recorded and fails the run (see TestMain), because it would exercise one form only.
func FixtureBudgetControls(t testing.TB) bool {
	switch FixtureForm(t) {
	case "0":
		return true
	case "1":
		return false
	}
	noteUnwrappedFixture(t)
	return true
}

var (
	unwrappedMu    sync.Mutex
	unwrappedTests = map[string]bool{}
)

// noteUnwrappedFixture records the test that asks for a fixture without a form. Benchmarks are
// not recorded.
func noteUnwrappedFixture(t testing.TB) {
	if _, ok := t.(*testing.T); !ok {
		return
	}
	name, _, _ := strings.Cut(t.Name(), "/")
	unwrappedMu.Lock()
	unwrappedTests[name] = true
	unwrappedMu.Unlock()
}

// UnwrappedFixtureTests returns the sorted names of the tests recorded by noteUnwrappedFixture.
func UnwrappedFixtureTests() []string {
	unwrappedMu.Lock()
	defer unwrappedMu.Unlock()
	return slices.Sorted(maps.Keys(unwrappedTests))
}

// fixtureModes are the forms in which EachMode runs a test, by subtest name.
var fixtureModes = []struct{ name, value string }{{"controls", "0"}, {"ordinary", "1"}}

// EachMode runs body once per form of the generated fixtures, as the parallel subtests "controls"
// and "ordinary", so it must be the caller's last statement. When modeEnv is set in the
// environment, it runs body once, in that form.
func EachMode(t *testing.T, body func(t *testing.T)) {
	t.Helper()
	if EnvForm != "" {
		body(t)
		return
	}
	for _, m := range fixtureModes {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			SetFixtureForm(t, m.value)
			body(t)
		})
	}
}

// NeedsBudgetControls makes the fixtures of a test that passes a control option have budget
// controls, and skips it when they are restricted to the ordinary form.
func NeedsBudgetControls(t testing.TB) {
	t.Helper()
	if EnvForm == "1" {
		t.Skip("uses budget controls; the fixtures are generated without them")
	}
	SetFixtureForm(t, "0")
}

// PinOrdinary makes the fixtures of a test of the ordinary-only output have no budget controls.
func PinOrdinary(t testing.TB) {
	SetFixtureForm(t, "1")
}

var (
	runtimeCopyOnce sync.Once
	runtimeCopyDir  string
)

// RuntimeCopy returns the root of a module that holds a copy of the runtime package with the
// identifiers that testdata/export_names.txt lists renamed (see ExportNames).
func RuntimeCopy() string {
	runtimeCopyOnce.Do(func() {
		dir, err := os.MkdirTemp("", "zprt-copy")
		if err != nil {
			panic(err)
		}
		writeRuntimeCopy(dir, nil, nil)
		runtimeCopyDir = dir
	})
	return runtimeCopyDir
}

// RuntimeCopyWith returns a copy of the runtime like RuntimeCopy that has patch applied to the
// source of each file (by name) and the extra files, which are given as name and source.
func RuntimeCopyWith(t testing.TB, patch func(name, src string) string, extra map[string]string) string {
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
	must(os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/ornew/zeroparse\n\ngo 1.27.1\n"), 0o644))
	rt := filepath.Join(dir, "runtime")
	must(os.MkdirAll(rt, 0o755))
	src := filepath.Join(moduleRoot(), "runtime")
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
		must(os.WriteFile(filepath.Join(rt, e.Name()), ExportNames(data), 0o644))
	}
	must(os.WriteFile(filepath.Join(rt, "zz_testing.go"), ExportNames([]byte(runtimeTestingShim)), 0o644))
	for name, data := range extra {
		must(os.WriteFile(filepath.Join(rt, name), ExportNames([]byte(data)), 0o644))
	}
}

// GoMod returns the go.mod of a test module that imports the runtime from a copy of this
// checkout's runtime package.
func GoMod(module string) string {
	return GoModAt(module, RuntimeCopy())
}

// GoModAt returns the go.mod of a test module that imports the runtime from the module in dir.
func GoModAt(module, dir string) string {
	return "module " + module + "\n\ngo 1.27.1\n\nrequire github.com/ornew/zeroparse v0.0.0\n\nreplace github.com/ornew/zeroparse => " + dir + "\n"
}

// WriteGenerated writes the files of a generated package into dir. A go.mod for the module is
// written when dir has none.
func WriteGenerated(t testing.TB, dir, module string, files []gen.GeneratedFile) {
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
		if err := os.WriteFile(mod, []byte(GoMod(module)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// GeneratePackage generates a parser that imports the runtime from this module as the package
// gentest/<pkg>, writes its files (parser.go, api.go and types/types.go) below dir/<pkg> in the
// module of dir, and returns its parser.go. Unlike GenerateCode, it keeps the types package
// apart, so that grammar types named like the names of the parser package compile.
func GeneratePackage(t testing.TB, dir, pkg string, g *grammar.Grammar, opts gen.GenOptions) string {
	t.Helper()
	return GeneratePackageIn(t, dir, "gentest", pkg, g, opts)
}

// GeneratePackageIn is GeneratePackage for a test module whose path is root.
func GeneratePackageIn(t testing.TB, dir, root, pkg string, g *grammar.Grammar, opts gen.GenOptions) string {
	t.Helper()
	return generatePackage(t, dir, root, pkg, g, opts, Generate)
}

// GeneratePackageRaw is GeneratePackage with the options as given: the parser records
// expectations only with opts.Expectations.
func GeneratePackageRaw(t testing.TB, dir, pkg string, g *grammar.Grammar, opts gen.GenOptions) string {
	t.Helper()
	return generatePackage(t, dir, "gentest", pkg, g, opts, GenerateRaw)
}

func generatePackage(t testing.TB, dir, root, pkg string, g *grammar.Grammar, opts gen.GenOptions, generate func(testing.TB, *grammar.Grammar, gen.GenOptions) ([]gen.GeneratedFile, error)) string {
	t.Helper()
	opts.Runtime = gen.RuntimeImport
	opts.BudgetControls = FixtureBudgetControls(t)
	opts.Package, opts.Module = pkg, root+"/"+pkg
	opts.KeepComments = true
	files, err := generate(t, g, opts)
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

// GenerateCode generates a parser that imports the runtime from this module and returns its
// parser.go and api.go as one source file, which tests write as parser.go.
func GenerateCode(t testing.TB, g *grammar.Grammar, opts gen.GenOptions) ([]byte, error) {
	opts.Runtime = gen.RuntimeImport
	opts.KeepComments = true
	opts.BudgetControls = FixtureBudgetControls(t)
	if opts.Module == "" && opts.Types {
		opts.Module = "example.com/generated"
	}
	files, err := Generate(t, g, opts)
	if err != nil {
		return nil, err
	}
	code := MergeGenerated(files)
	if MutateCode != nil {
		code = MutateCode(code)
	}
	return code, nil
}

// MergeGenerated joins the Go files of a generated package into one file. The types package of
// a typed parser (types/types.go) joins the file too, with the references to it unqualified, so
// that the fixtures of the tests name the types without importing the package.
func MergeGenerated(files []gen.GeneratedFile) []byte {
	var parts []gen.GeneratedFile
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
			if imp.Name != nil && imp.Name.Name == gogen.TypesAlias {
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
			if id, ok := sel.X.(*goast.Ident); ok && id.Name == gogen.TypesAlias && id.Obj == nil {
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
	data, err := os.ReadFile(filepath.Join(moduleRoot(), "internal", "gentest", "testdata", "export_names.txt"))
	if err != nil {
		panic(err)
	}
	names := map[string]bool{}
	for _, f := range strings.Fields(string(data)) {
		names[f] = true
	}
	return names
}

// ExportNames renames the identifiers of src that exportedNames lists to X_name, which makes the
// package-level declarations, fields and methods of the runtime copy reachable from the
// fixtures. The renaming is consistent within the package, so it does not change what the code does.
func ExportNames(src []byte) []byte {
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
