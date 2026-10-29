package gogen_test

import (
	"fmt"
	goast "go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gen/gogen"
	"github.com/ornew/pego/internal/gentest"
)

// runtimeNameTypes are names that the runtime and the generated parser package declare. A type of a
// typed grammar may take each of them: the types are declared in the package types, apart from
// both.
var runtimeNameTypes = []string{
	"Walk", "SetNodeText", "SetNodeElems", "SetNodeField", "SetNodeStart", "TOperand", "TPubHooks", "TDeref",
	"NewTError", "NewStreamIterator", "NewNode", "Mark", "TSeq", "Node", "Parser", "Span", "Rule", "Parse", "ParseAST",
	"Document", "ParseStats", "StreamValue", "ParseOption",
}

// typesFileOf returns the source of types/types.go of the generated files.
func typesFileOf(t *testing.T, files []gen.GeneratedFile) string {
	t.Helper()
	for _, f := range files {
		if f.Path == "types/types.go" {
			return string(f.Content)
		}
	}
	t.Fatal("no types/types.go")
	return ""
}

// TestGeneratedTypesNamedLikeRuntimeExports checks that a type of a typed grammar named like a
// name of the runtime or of the parser package keeps its name, in embedded and imported mode, and
// that the parser builds and works: the type is the one of the package types, and the name in
// the parser package is still the runtime's.
func TestGeneratedTypesNamedLikeRuntimeExports(t *testing.T) {
	t.Parallel()
	var src strings.Builder
	for _, name := range runtimeNameTypes {
		fmt.Fprintf(&src, "type %s = { Value int }\n", name)
	}
	src.WriteString("rule main Node = \"a\" => Node{Value: 7}\n")
	g, err := parseTest(src.String())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		opts gen.GenOptions
	}{
		{"types", gen.GenOptions{Types: true}},
		{"document observability", gen.GenOptions{Types: true, Document: true, Observability: true, Recognize: true}},
	} {
		for _, mode := range []gen.RuntimeMode{gen.RuntimeEmbed, gen.RuntimeImport} {
			t.Run(c.name+" "+mode.String(), func(t *testing.T) {
				opts := c.opts
				opts.Module, opts.Package, opts.Start, opts.Runtime = "example.com/names", "names", "main", mode
				files, err := gentest.Generate(t, g, opts)
				if err != nil {
					t.Fatal(err)
				}
				typesSrc := typesFileOf(t, files)
				for _, name := range runtimeNameTypes {
					if !strings.Contains(typesSrc, "type "+name+" struct") {
						t.Errorf("the type %s is not declared under its name", name)
					}
				}
				if testing.Short() {
					return
				}
				dir := t.TempDir()
				gentest.WriteGenerated(t, dir, "example.com/names", files)
				if err := os.WriteFile(filepath.Join(dir, "names_test.go"), []byte(`package names

import (
	"testing"

	"example.com/names/types"
)

func TestNames(t *testing.T) {
	v, err := ParseAST("a")
	if err != nil {
		t.Fatal(err)
	}
	var typed *types.Node = v // the user type
	if typed.Value != 7 {
		t.Errorf("Value = %d", typed.Value)
	}
	var span types.Span
	_ = span.Value // the user type Span has a field Value; the range is the embedded runtime Span
	n, err := Parse("a")
	if err != nil {
		t.Fatal(err)
	}
	var node *Node = n // the runtime's Node
	if node == nil {
		t.Error("no node")
	}
	var _ Span // the runtime's Span
	if got, ok := typed.TField("Value"); !ok || got != 7 {
		t.Errorf("TField(Value) = %v, %v", got, ok)
	}
}
`), 0o644); err != nil {
					t.Fatal(err)
				}
				gentest.GoVetAndTest(t, dir, true)
			})
		}
	}
}

// typeNamesOf returns the names of the struct types that types/types.go declares.
func typeNamesOf(t *testing.T, files []gen.GeneratedFile) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, line := range strings.Split(typesFileOf(t, files), "\n") {
		if rest, ok := strings.CutPrefix(line, "type "); ok {
			if name, ok := strings.CutSuffix(rest, " struct {"); ok {
				out[name] = true
			}
		}
	}
	return out
}

// TestGeneratedTypeNamesIgnoreTheOutput checks that the Go name of a grammar type is its name
// whatever the output of the grammar uses: grammars that differ only in using a struct rule, or a
// stream, name the types alike under the same options.
func TestGeneratedTypeNamesIgnoreTheOutput(t *testing.T) {
	t.Parallel()
	const types = "type Mark = { Value int }\ntype TSeq = { Value int }\ntype Walk = { Value int }\ntype NewStreamIterator = { Value int }\ntype Plain = { Value int }\n"
	grammars := map[string]string{
		"trivial":       types + "rule main = \"a\"\n",
		"struct":        types + "type Pair = { A int; B int }\nrule main = \"a\" => Pair{A: 1, B: 2}\n",
		"stream":        types + "rule main = \"a\"* #stream $$\n",
		"unit":          types + "rule main = \"a\" => ()\n",
		"stream struct": types + "type Pair = { A int; B int }\nrule main = item* #stream $$\nrule item = \"a\" => Pair{A: 1, B: 2}\n",
	}
	for _, c := range []struct {
		opts     gen.GenOptions
		grammars []string
	}{
		{gen.GenOptions{Types: true}, []string{"trivial", "struct", "stream", "unit"}},
		{gen.GenOptions{Types: true, Document: true, Observability: true}, []string{"trivial", "struct", "stream", "unit"}},
		{gen.GenOptions{Types: true, Stream: true}, []string{"stream", "stream struct"}},
	} {
		opts := c.opts
		for _, name := range c.grammars {
			g, err := parseTest(grammars[name])
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			o := opts
			o.Module, o.Package, o.Start, o.Runtime = "example.com/same", "same", "main", gen.RuntimeImport
			files, err := gentest.Generate(t, g, o)
			if err != nil {
				t.Fatalf("%s %+v: %v", name, opts, err)
			}
			got := typeNamesOf(t, files)
			for _, want := range []string{"Mark", "TSeq", "Walk", "NewStreamIterator", "Plain"} {
				if !got[want] {
					t.Errorf("%s %+v: the type %s is not declared under its name", name, opts, want)
				}
			}
			for name := range got {
				if strings.HasSuffix(name, "_") {
					t.Errorf("%s %+v: the type %s was renamed", name, opts, name)
				}
			}
		}
	}
}

// TestGeneratedTypeNamedLikeTypesPackageName checks that a grammar type that has the name of a type
// the types package declares for the typed values is rejected, and that the name is free when the
// output does not declare the type.
func TestGeneratedTypeNamedLikeTypesPackageName(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, grammar string
		rejected      bool
	}{
		{"TNode", "type TNode = { Value int }\nrule main = \"a\"\n", true},
		{"hooks", "type PegoHooks = { Value int }\nrule main = \"a\"\n", true},
		{"unit used", "type Unit = { Value int }\nrule main = \"a\" => ()\n", true},
		{"unit unused", "type Unit = { Value int }\nrule main = \"a\"\n", false},
		{"tuple used", "type Tuple2 = { Value int }\nrule main = \"a\" \"b\" => (1, 2)\n", true},
		{"tuple unused", "type Tuple2 = { Value int }\nrule main = \"a\"\n", false},
		{"tuple of another size", "type Tuple3 = { Value int }\nrule main = \"a\" \"b\" => (1, 2)\n", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			g, err := parseTest(c.grammar)
			if err != nil {
				t.Fatal(err)
			}
			files, err := gentest.Generate(t, g, gen.GenOptions{Module: "example.com/clash", Package: "clash", Start: "main", Runtime: gen.RuntimeImport, Types: true})
			if c.rejected {
				if err == nil || !strings.Contains(err.Error(), "rename the type") {
					t.Fatalf("got %v, want an error that asks to rename the type", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if testing.Short() {
				return
			}
			dir := t.TempDir()
			gentest.WriteGenerated(t, dir, "example.com/clash", files)
			gentest.GoVetAndTest(t, dir, false)
		})
	}
}

// TestGeneratedTypesPackageNamesTheRuntimeExplicitly checks that types/types.go leaves no name
// unresolved that is an export of the runtime: it refers to the runtime by its package name, so a
// type of the grammar named like an export cannot capture the reference.
func TestGeneratedTypesPackageNamesTheRuntimeExplicitly(t *testing.T) {
	t.Parallel()
	info, err := gogen.LoadRuntime()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"json", "csv", "cel", "yaml"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "..", "parsers", name, name+".pego"))
			if err != nil {
				t.Fatal(err)
			}
			g, err := parseTest(string(data))
			if err != nil {
				t.Fatal(err)
			}
			files, err := gentest.Generate(t, g, gen.GenOptions{Module: "example.com/explicit/" + name, Package: name, Start: "main", Runtime: gen.RuntimeImport, Types: true, Recognize: true, Document: true, Observability: true})
			if err != nil {
				t.Fatal(err)
			}
			f, err := goparser.ParseFile(token.NewFileSet(), "types.go", typesFileOf(t, files), 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range f.Unresolved {
				if _, ok := info.Decls[id.Name]; ok {
					t.Errorf("types/types.go leaves %s to the runtime unqualified", id.Name)
				}
			}
		})
	}
}

// TestGeneratedTypesNamedLikeEveryRuntimeExport generates a typed grammar with a type for every
// export of the runtime, with every option that adds names to the package, and builds it. Every
// type keeps its name, except the one that the types package declares itself (TNode). No grammar
// may make the generator fail.
func TestGeneratedTypesNamedLikeEveryRuntimeExport(t *testing.T) {
	t.Parallel()
	info, err := gogen.LoadRuntime()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for name := range info.Decls {
		if name == "TNode" {
			continue
		}
		// The grammar language reserves some names (Error, ...).
		if g, err := parseTest("type " + name + " = { Value int }\nrule main = \"a\"\n"); err == nil {
			if _, err := engine.Compile(g, compile.Options{}); err == nil {
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	if len(names) < 100 {
		t.Fatalf("only %d runtime exports can be type names", len(names))
	}
	var src strings.Builder
	for _, name := range names {
		fmt.Fprintf(&src, "type %s = { Value int }\n", name)
	}
	src.WriteString("rule main = \"a\"* #stream $$\n")
	g, err := parseTest(src.String())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		opts gen.GenOptions
	}{
		{"types", gen.GenOptions{Types: true}},
		{"stream", gen.GenOptions{Types: true, Stream: true}},
		{"all", gen.GenOptions{Types: true, Document: true, Observability: true, Recognize: true, Stream: true}},
	} {
		t.Run(c.name, func(t *testing.T) {
			opts := c.opts
			opts.Module, opts.Package, opts.Start, opts.Runtime = "example.com/every", "every", "main", gen.RuntimeImport
			files, err := gentest.Generate(t, g, opts)
			if err != nil {
				t.Fatal(err)
			}
			declared := typeNamesOf(t, files)
			for _, name := range names {
				if !declared[name] {
					t.Errorf("the type %s is not declared under its name", name)
				}
			}
			if testing.Short() {
				return
			}
			dir := t.TempDir()
			gentest.WriteGenerated(t, dir, "example.com/every", files)
			gentest.GoVetAndTest(t, dir, false)
		})
	}
}

// TestParserAndAPIDeclareDistinctNames checks that parser.go and api.go declare no name twice or
// in common, whatever options are on: no type of the grammar is among them, so the names are
// the generator's own.
func TestParserAndAPIDeclareDistinctNames(t *testing.T) {
	t.Parallel()
	g, err := parseTest("type Pair = { A int; B int }\nrule main = item* #stream $$\nrule item = \"a\" => Pair{A: 1, B: 2}\n")
	if err != nil {
		t.Fatal(err)
	}
	files, err := gentest.Generate(t, g, gen.GenOptions{Module: "example.com/distinct", Start: "main", Runtime: gen.RuntimeImport, Types: true, Document: true, Observability: true, Stream: true, Recognize: true})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	var apiNames []string
	for _, f := range files {
		if f.Path != "parser.go" && f.Path != "api.go" {
			continue
		}
		declared, err := gogen.DeclaredNames(string(f.Content))
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range declared {
			if name == "_" {
				continue
			}
			if other, ok := seen[name]; ok {
				t.Errorf("%s declares %s, which %s declares too", f.Path, name, other)
			}
			seen[name] = f.Path
			if f.Path == "api.go" {
				apiNames = append(apiNames, name)
			}
		}
	}
	if !slices.Contains(apiNames, "Walk") {
		t.Errorf("api.go declares %v, want Walk", apiNames)
	}
	if !slices.Contains(apiNames, "Span") {
		t.Error("api.go does not declare Span")
	}
}

// TestGeneratedTypedFieldNamedRange checks that a struct field named Range does not hide the Range
// method of the embedded Span: the field is renamed and still reads back under its grammar name.
func TestGeneratedTypedFieldNamedRange(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds standalone generated parsers")
	}
	g, err := parseTest("type R = { Range int }\nrule main R = [0-9]+ #atomic => R{Range: 7}\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []gen.RuntimeMode{gen.RuntimeEmbed, gen.RuntimeImport} {
		t.Run(mode.String(), func(t *testing.T) {
			files, err := gentest.Generate(t, g, gen.GenOptions{Module: "example.com/rng", Package: "rng", Start: "main", Runtime: mode, Types: true})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			gentest.WriteGenerated(t, dir, "example.com/rng", files)
			if err := os.WriteFile(filepath.Join(dir, "rng_test.go"), []byte(`package rng

import "testing"

func TestRange(t *testing.T) {
	v, err := ParseAST("12")
	if err != nil {
		t.Fatal(err)
	}
	if start, end := v.Span.Range(); start != 0 || end != 2 {
		t.Errorf("Span.Range() = %d, %d", start, end)
	}
	if v.Range_ != 7 {
		t.Errorf("Range_ = %d", v.Range_)
	}
	if got, ok := v.TField("Range"); !ok || got != 7 {
		t.Errorf("TField(Range) = %v, %v", got, ok)
	}
}
`), 0o644); err != nil {
				t.Fatal(err)
			}
			gentest.GoVetAndTest(t, dir, true)
		})
	}
}

// TestDefaultPackageName checks the package name taken from a module path.
func TestDefaultPackageName(t *testing.T) {
	t.Parallel()
	for module, want := range map[string]string{
		"example.com/foo":          "foo",
		"example.com/foo/v2":       "foo",
		"example.com/foo/v10":      "foo",
		"example.com/foo/v1":       "v1",
		"example.com/foo/v0":       "v0",
		"example.com/foo/v2x":      "v2x",
		"example.com/foo/version":  "version",
		"v2":                       "v2",
		"foo":                      "foo",
		"example.com/my-parser":    "my-parser",
		"example.com/my-parser/v3": "my-parser",
	} {
		if got := gogen.DefaultPackageName(module); got != want {
			t.Errorf("DefaultPackageName(%q) = %q, want %q", module, got, want)
		}
	}
	g, err := parseTest("rule main = \"a\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gentest.Generate(t, g, gen.GenOptions{Module: "example.com/my-parser", Start: "main"}); err == nil || !strings.Contains(err.Error(), "-pkg") {
		t.Errorf("a module with a hyphen: got %v, want an error that suggests -pkg", err)
	}
	if _, err := gentest.Generate(t, g, gen.GenOptions{Module: "example.com/my-parser", Package: "myparser", Start: "main"}); err != nil {
		t.Errorf("with a package name: %v", err)
	}
	if _, err := gentest.Generate(t, g, gen.GenOptions{Package: "myparser", Start: "main", Runtime: gen.RuntimeImport}); err != nil {
		t.Errorf("import mode needs no module: %v", err)
	}
	if _, err := gentest.Generate(t, g, gen.GenOptions{Package: "myparser", Start: "main"}); err == nil {
		t.Error("embed mode without a module was accepted")
	}
}

// TestQualifyRuntimeConstantKeys checks that the runtime constants used as keys of composite
// literals are qualified whatever the literal's type is spelled like, while field names of structs
// are not.
func TestQualifyRuntimeConstantKeys(t *testing.T) {
	t.Parallel()
	info, err := gogen.LoadRuntime()
	if err != nil {
		t.Fatal(err)
	}
	const body = `
type kinds map[NodeKind]string
type pair struct{ KindString, KindTuple int }
type table [8]string
var a = map[NodeKind]string{KindString: "s"}
var b = [...]string{KindTuple: "t"}
var c = []string{KindList: "l"}
var d = kinds{KindError: "e"}
var e = table{KindStruct: "x"}
var f = pair{KindString: 1, KindTuple: 2}
var g = &pair{KindString: 1}
var h = []*pair{{KindString: 1}}
var i = map[string]map[NodeKind]int{"a": {KindString: 1}}
var j = map[NodeKind]map[NodeKind]int{KindString: {KindTuple: 1}}
var k = map[[2]NodeKind]int{{KindString, KindTuple}: 1}
var l = []map[NodeKind]int{{KindList: 1}}
var m = map[NodeKind][]pair{KindList: {{KindString: 1}}}
var n = Fields{{Name: "x"}}
var o = [2]NodeKind{1: KindTuple}
`
	out, err := gogen.QualifyRuntime(gogen.RuntimeImportPath, body, info, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"rt.KindString: \"s\"", "rt.KindTuple: \"t\"", "rt.KindList: \"l\"", "rt.KindError: \"e\"", "rt.KindStruct: \"x\"",
		"{rt.KindString: {rt.KindTuple: 1}", "{\"a\": {rt.KindString: 1}", "{{rt.KindList: 1}}", "{rt.KindList: {{KindString: 1}}}",
		"pair{KindString: 1, KindTuple: 2}", "&pair{KindString: 1}", "[]*pair{{KindString: 1}}",
	} {
		if !strings.Contains(strings.ReplaceAll(out, "\n", ""), want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	// A literal of a type of the types package (types.Name) is resolved through its declaration
	// there: the keys of a map are runtime constants, the field names of a struct are not.
	decls := map[string]goast.Expr{}
	f, err := goparser.ParseFile(token.NewFileSet(), "types.go", `package p
type Table map[rt.NodeKind]string
type Pair struct{ KindString, KindTuple int }`, goparser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.Decls {
		for _, s := range d.(*goast.GenDecl).Specs {
			decls[s.(*goast.TypeSpec).Name.Name] = s.(*goast.TypeSpec).Type
		}
	}
	tp := &gogen.TypesPackage{ImportPath: "example.com/m/types", Declared: func(string) bool { return false }, Types: decls}
	out, err = gogen.QualifyRuntime(gogen.RuntimeImportPath, `
var a = types.Table{KindString: "s"}
var b = &types.Pair{KindString: 1}
var c = []types.Table{{KindList: "l"}}
`, info, tp)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`types.Table{rt.KindString: "s"}`, "&types.Pair{KindString: 1}", `[]types.Table{{rt.KindList: "l"}}`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

// TestEmbeddedModeCompilesRichGrammars builds the parsers of larger grammars, with the options
// that add the most generated code, in embedded mode, which needs no other module. It catches an
// unqualified or doubly qualified runtime name in any literal form the generator emits.
func TestEmbeddedModeCompilesRichGrammars(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds standalone generated parsers")
	}
	for _, name := range []string{"json", "csv", "cel", "yaml"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "..", "parsers", name, name+".pego"))
			if err != nil {
				t.Fatal(err)
			}
			g, err := parseTest(string(data))
			if err != nil {
				t.Fatal(err)
			}
			module := "example.com/rich/" + name
			files, err := gentest.Generate(t, g, gen.GenOptions{Module: module, Package: name, Start: "main", Runtime: gen.RuntimeEmbed, Types: true, Recognize: true, Document: true, Observability: true})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			for _, f := range files {
				path := filepath.Join(dir, filepath.FromSlash(f.Path))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, f.Content, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			// Embedded mode depends on the standard library only: no replace directive.
			if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+module+"\n\ngo 1.27.1\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			gentest.GoVetAndTest(t, dir, false)
		})
	}
}

// TestGeneratedRichGrammarsWithTypesNamedLikeRuntimeExports builds the parsers of the large grammars
// (struct rules, operators, lists, options, recovery) with every option and an extra type for every
// export of the runtime. A runtime name that the generators write but emittedRuntimeNames lacks lets
// the type capture the references, and the build fails, independently of the check of the list.
func TestGeneratedRichGrammarsWithTypesNamedLikeRuntimeExports(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds standalone generated parsers")
	}
	info, err := gogen.LoadRuntime()
	if err != nil {
		t.Fatal(err)
	}
	declares := regexp.MustCompile(`(?m)^\s*type\s+(\w+)`)
	for _, name := range []string{"json", "csv", "cel", "yaml"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "..", "parsers", name, name+".pego"))
			if err != nil {
				t.Fatal(err)
			}
			own := map[string]bool{}
			for _, m := range declares.FindAllStringSubmatch(string(data), -1) {
				own[m[1]] = true
			}
			var src strings.Builder
			src.Write(data)
			src.WriteString("\n")
			var names []string
			for n := range info.Decls {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				if own[n] || n == "TNode" {
					continue
				}
				// The grammar language reserves some names (Error, ...).
				if g, err := parseTest("type " + n + " = { Value int }\nrule main = \"a\"\n"); err != nil {
					continue
				} else if _, err := engine.Compile(g, compile.Options{}); err != nil {
					continue
				}
				fmt.Fprintf(&src, "type %s = { Value int }\n", n)
			}
			g, err := parseTest(src.String())
			if err != nil {
				t.Fatal(err)
			}
			module := "example.com/richnames/" + name
			files, err := gentest.Generate(t, g, gen.GenOptions{Module: module, Package: name, Start: "main", Runtime: gen.RuntimeImport, Types: true, Recognize: true, Document: true, Observability: true})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			gentest.WriteGenerated(t, dir, module, files)
			gentest.GoVetAndTest(t, dir, false)
		})
	}
}
