package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// goVetAndTest runs go vet and go test in the module in dir.
func goVetAndTest(t *testing.T, dir string, test bool) {
	t.Helper()
	args := [][]string{{"vet", "./..."}}
	if test {
		args = append(args, []string{"test", "-count=1", "./..."})
	}
	for _, a := range args {
		cmd := exec.Command("go", a...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=-mod=mod -trimpath")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s: %v\n%s", a[0], err, out)
		}
	}
}

// runtimeNameTypes are exports of the runtime that grammar types commonly take, among them those
// that the generated package does not declare and so must not be renamed.
var runtimeNameTypes = []string{"Walk", "TOperand", "TPubHooks", "TDeref", "NewTError", "NewStreamIterator", "NewNode", "SetNodeText", "SetNodeElems", "SetNodeField", "SetNodeStart"}

// TestGeneratedTypesNamedLikeRuntimeExports checks that a type of a typed grammar may be named like
// an export of the runtime, in embedded and imported mode: the generated code refers to the runtime
// by qualified names, so only the names the generated package declares itself force a rename.
func TestGeneratedTypesNamedLikeRuntimeExports(t *testing.T) {
	var src strings.Builder
	for _, name := range runtimeNameTypes {
		fmt.Fprintf(&src, "type %s = { Value int }\n", name)
	}
	src.WriteString("rule main = \"a\"\n")
	g, err := syntax.Parse(src.String())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		opts GenOptions
	}{
		{"types", GenOptions{Types: true}},
		{"document observability", GenOptions{Types: true, Document: true, Observability: true, Recognize: true}},
	} {
		for _, mode := range []RuntimeMode{RuntimeEmbed, RuntimeImport} {
			t.Run(c.name+" "+mode.String(), func(t *testing.T) {
				opts := c.opts
				opts.Module, opts.Package, opts.Start, opts.Runtime = "example.com/names", "names", "main", mode
				files, err := Generate(g, opts)
				if err != nil {
					t.Fatal(err)
				}
				var parser string
				for _, f := range files {
					if f.Path == "parser.go" {
						parser = string(f.Content)
					}
				}
				for _, name := range runtimeNameTypes {
					if !strings.Contains(parser, "type "+name+" struct") {
						t.Errorf("the type %s was renamed", name)
					}
				}
				if testing.Short() {
					return
				}
				dir := t.TempDir()
				writeGenerated(t, dir, "example.com/names", files)
				goVetAndTest(t, dir, false)
			})
		}
	}
}

// TestGeneratedTypesNamedLikeUsedRuntimeExports checks that a type named like an export that the
// generated code uses is renamed, and the parser builds: in the stream parser that is
// NewStreamIterator, in the Document parser the type Document.
func TestGeneratedTypesNamedLikeUsedRuntimeExports(t *testing.T) {
	g, err := syntax.Parse("type NewStreamIterator = { Value int }\ntype Walk = { Value int }\nrule main = \"a\"* #stream $$\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []RuntimeMode{RuntimeEmbed, RuntimeImport} {
		t.Run(mode.String(), func(t *testing.T) {
			files, err := Generate(g, GenOptions{Module: "example.com/used", Package: "used", Start: "main", Runtime: mode, Types: true, Stream: true})
			if err != nil {
				t.Fatal(err)
			}
			var parser string
			for _, f := range files {
				if f.Path == "parser.go" {
					parser = string(f.Content)
				}
			}
			if !strings.Contains(parser, "type NewStreamIterator_ struct") || !strings.Contains(parser, "type Walk struct") {
				t.Errorf("unexpected type names:\n%s", parser)
			}
			if testing.Short() {
				return
			}
			dir := t.TempDir()
			writeGenerated(t, dir, "example.com/used", files)
			goVetAndTest(t, dir, false)
		})
	}
}

// TestGeneratedTypesNamedLikeEveryRuntimeExport generates a typed grammar with a type for every
// export of the runtime, with every option that adds names to the package, and builds it. A name
// that the generated package declares must be renamed; any other type keeps its name. No grammar
// may make the generator fail.
func TestGeneratedTypesNamedLikeEveryRuntimeExport(t *testing.T) {
	info, err := loadRuntime()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for name := range info.decls {
		// The grammar language reserves some names (Error, ...).
		if g, err := syntax.Parse("type " + name + " = { Value int }\nrule main = \"a\"\n"); err == nil {
			if _, err := Compile(g, Options{}); err == nil {
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
	g, err := syntax.Parse(src.String())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		opts GenOptions
	}{
		{"types", GenOptions{Types: true}},
		{"stream", GenOptions{Types: true, Stream: true}},
		{"all", GenOptions{Types: true, Document: true, Observability: true, Recognize: true, Stream: true}},
	} {
		t.Run(c.name, func(t *testing.T) {
			opts := c.opts
			opts.Module, opts.Package, opts.Start, opts.Runtime = "example.com/every", "every", "main", RuntimeImport
			files, err := Generate(g, opts)
			if err != nil {
				t.Fatal(err)
			}
			reserved := opts.reservedNames("")
			var parser string
			for _, f := range files {
				if f.Path == "parser.go" {
					parser = string(f.Content)
				}
			}
			for _, name := range names {
				if reserved[name] && !slices.Contains(opts.typedEntryNames(), name) && strings.Contains(parser, "type "+name+" struct") {
					t.Errorf("the type %s clashes with the generated package", name)
				}
			}
			if testing.Short() {
				return
			}
			dir := t.TempDir()
			writeGenerated(t, dir, "example.com/every", files)
			goVetAndTest(t, dir, false)
		})
	}
}

// TestReservedNamesFollowTheAPI checks that the names typed values avoid include every name that
// api.go declares, whatever options are on, and none that the runtime keeps to itself.
func TestReservedNamesFollowTheAPI(t *testing.T) {
	info, err := loadRuntime()
	if err != nil {
		t.Fatal(err)
	}
	opts := GenOptions{Types: true, Document: true, Observability: true, Stream: true, Package: "p"}
	api, err := opts.apiSource(info, false)
	if err != nil {
		t.Fatal(err)
	}
	declared, err := declaredNames(api)
	if err != nil {
		t.Fatal(err)
	}
	reserved := opts.reservedNames("")
	for _, name := range declared {
		if !reserved[name] && name != "Span" && name != "_" {
			t.Errorf("api.go declares %s, which typed values may take", name)
		}
	}
	for _, name := range []string{"Walk", "TOperand", "TPubHooks", "NewNode"} {
		if reserved[name] {
			t.Errorf("%s is reserved, but the generated package does not declare it", name)
		}
	}
	if !reserved["WalkTree"] {
		t.Error("WalkTree is not reserved")
	}
	if !opts.reservedNames("func Parse() {}\nvar rules []int\n")["rules"] {
		t.Error("the declarations of the generated head are not reserved")
	}
	if !slices.Contains(declared, "WalkTree") {
		t.Error("api.go does not declare WalkTree")
	}
}

// TestGeneratedTypedFieldNamedRange checks that a struct field named Range does not hide the Range
// method of the embedded Span: the field is renamed and still reads back under its grammar name.
func TestGeneratedTypedFieldNamedRange(t *testing.T) {
	if testing.Short() {
		t.Skip("builds standalone generated parsers")
	}
	g, err := syntax.Parse("type R = { Range int }\nrule main R = [0-9]+ #atomic => R{Range: 7}\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []RuntimeMode{RuntimeEmbed, RuntimeImport} {
		t.Run(mode.String(), func(t *testing.T) {
			files, err := Generate(g, GenOptions{Module: "example.com/rng", Package: "rng", Start: "main", Runtime: mode, Types: true})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			writeGenerated(t, dir, "example.com/rng", files)
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
			goVetAndTest(t, dir, true)
		})
	}
}

// TestDefaultPackageName checks the package name taken from a module path.
func TestDefaultPackageName(t *testing.T) {
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
		if got := DefaultPackageName(module); got != want {
			t.Errorf("DefaultPackageName(%q) = %q, want %q", module, got, want)
		}
	}
	g, err := syntax.Parse("rule main = \"a\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(g, GenOptions{Module: "example.com/my-parser", Start: "main"}); err == nil || !strings.Contains(err.Error(), "-pkg") {
		t.Errorf("a module with a hyphen: got %v, want an error that suggests -pkg", err)
	}
	if _, err := Generate(g, GenOptions{Module: "example.com/my-parser", Package: "myparser", Start: "main"}); err != nil {
		t.Errorf("with a package name: %v", err)
	}
	if _, err := Generate(g, GenOptions{Package: "myparser", Start: "main", Runtime: RuntimeImport}); err != nil {
		t.Errorf("import mode needs no module: %v", err)
	}
	if _, err := Generate(g, GenOptions{Package: "myparser", Start: "main"}); err == nil {
		t.Error("embed mode without a module was accepted")
	}
}

// TestQualifyRuntimeConstantKeys checks that the runtime constants used as keys of composite
// literals are qualified whatever the literal's type is spelled like, while field names of structs
// are not.
func TestQualifyRuntimeConstantKeys(t *testing.T) {
	info, err := loadRuntime()
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
	out, _, err := qualifyRuntime("p", RuntimeImportPath, body, info)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"pegort.KindString: \"s\"", "pegort.KindTuple: \"t\"", "pegort.KindList: \"l\"", "pegort.KindError: \"e\"", "pegort.KindStruct: \"x\"",
		"{pegort.KindString: {pegort.KindTuple: 1}", "{\"a\": {pegort.KindString: 1}", "{{pegort.KindList: 1}}", "{pegort.KindList: {{KindString: 1}}}",
		"pair{KindString: 1, KindTuple: 2}", "&pair{KindString: 1}", "[]*pair{{KindString: 1}}",
	} {
		if !strings.Contains(strings.ReplaceAll(out, "\n", ""), want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

// TestEmbeddedModeCompilesRichGrammars builds the parsers of larger grammars, with the options
// that add the most generated code, in embedded mode, which needs no other module. It catches an
// unqualified or doubly qualified runtime name in any literal form the generator emits.
func TestEmbeddedModeCompilesRichGrammars(t *testing.T) {
	if testing.Short() {
		t.Skip("builds standalone generated parsers")
	}
	for _, name := range []string{"json", "csv", "cel", "yaml"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "parsers", name, name+".pego"))
			if err != nil {
				t.Fatal(err)
			}
			g, err := syntax.Parse(string(data))
			if err != nil {
				t.Fatal(err)
			}
			module := "example.com/rich/" + name
			files, err := Generate(g, GenOptions{Module: module, Package: name, Start: "main", Runtime: RuntimeEmbed, Types: true, Recognize: true, Document: true, Observability: true})
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
			goVetAndTest(t, dir, false)
		})
	}
}
