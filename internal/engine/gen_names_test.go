package engine

import (
	"fmt"
	goast "go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
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

// runtimeNameTypes are exports of the runtime that no generator writes into parser.go, and so
// grammar types may take. emittedNameTypes are those that the generators do write: such types
// are renamed in every grammar, whether or not its output uses the name.
var (
	runtimeNameTypes = []string{"Walk", "SetNodeText", "SetNodeElems", "SetNodeField", "SetNodeStart"}
	emittedNameTypes = []string{"TOperand", "TPubHooks", "TDeref", "NewTError", "NewStreamIterator", "NewNode", "Mark", "TSeq", "Unit"}
)

// TestGeneratedTypesNamedLikeRuntimeExports checks that a type of a typed grammar named like an
// export of the runtime is renamed if the generators write the name into parser.go, and keeps its
// name otherwise, in embedded and imported mode, whatever the rest of the grammar.
func TestGeneratedTypesNamedLikeRuntimeExports(t *testing.T) {
	var src strings.Builder
	for _, name := range append(slices.Clone(runtimeNameTypes), emittedNameTypes...) {
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
				for _, name := range emittedNameTypes {
					if !strings.Contains(parser, "type "+name+"_ struct") {
						t.Errorf("the type %s was not renamed", name)
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

// typeGoNames returns the Go names of the generated struct types by their name with the underscore
// removed.
func typeGoNames(t *testing.T, files []GeneratedFile) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, f := range files {
		if f.Path != "parser.go" {
			continue
		}
		for _, line := range strings.Split(string(f.Content), "\n") {
			if rest, ok := strings.CutPrefix(line, "type "); ok {
				if name, ok := strings.CutSuffix(rest, " struct {"); ok {
					out[strings.TrimSuffix(name, "_")] = name
				}
			}
		}
	}
	return out
}

// TestGeneratedTypeNamesIgnoreTheOutput checks that the Go name of a grammar type does not depend
// on what the output of the grammar uses: grammars that differ only in using a struct rule, or a
// stream, name the types alike under the same options.
func TestGeneratedTypeNamesIgnoreTheOutput(t *testing.T) {
	const types = "type Mark = { Value int }\ntype TSeq = { Value int }\ntype Unit = { Value int }\ntype Walk = { Value int }\ntype NewStreamIterator = { Value int }\ntype Plain = { Value int }\n"
	grammars := map[string]string{
		"trivial":       types + "rule main = \"a\"\n",
		"struct":        types + "type Pair = { A int; B int }\nrule main = \"a\" => Pair{A: 1, B: 2}\n",
		"stream":        types + "rule main = \"a\"* #stream $$\n",
		"unit":          types + "rule main = \"a\" => ()\n",
		"stream struct": types + "type Pair = { A int; B int }\nrule main = item* #stream $$\nrule item = \"a\" => Pair{A: 1, B: 2}\n",
	}
	for _, c := range []struct {
		opts     GenOptions
		grammars []string
	}{
		{GenOptions{Types: true}, []string{"trivial", "struct", "stream", "unit"}},
		{GenOptions{Types: true, Document: true, Observability: true}, []string{"trivial", "struct", "stream", "unit"}},
		{GenOptions{Types: true, Stream: true}, []string{"stream", "stream struct"}},
	} {
		opts := c.opts
		var first map[string]string
		for _, name := range c.grammars {
			g, err := syntax.Parse(grammars[name])
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			o := opts
			o.Module, o.Package, o.Start, o.Runtime = "example.com/same", "same", "main", RuntimeImport
			files, err := Generate(g, o)
			if err != nil {
				t.Fatalf("%s %+v: %v", name, opts, err)
			}
			got := typeGoNames(t, files)
			for _, want := range []string{"Mark_", "TSeq_", "Unit_", "Walk", "NewStreamIterator_", "Plain"} {
				if got[strings.TrimSuffix(want, "_")] != want {
					t.Errorf("%s %+v: the type %s is named %q", name, opts, strings.TrimSuffix(want, "_"), got[strings.TrimSuffix(want, "_")])
				}
			}
			if first == nil {
				first = got
			}
			for k, v := range first {
				if got[k] != v {
					t.Errorf("%s %+v: the type %s is %s, in the first grammar %s", name, opts, k, got[k], v)
				}
			}
		}
	}
}

// TestGeneratedUnitTypeAndGrammarUnit checks that a grammar type named Unit and the type of the
// empty tuple do not both become type Unit, and that the output builds.
func TestGeneratedUnitTypeAndGrammarUnit(t *testing.T) {
	g, err := syntax.Parse("type Unit = { Value int }\nrule main = \"a\" => ()\n")
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(g, GenOptions{Module: "example.com/unit", Package: "unit", Start: "main", Runtime: RuntimeImport, Types: true})
	if err != nil {
		t.Fatal(err)
	}
	var parser string
	for _, f := range files {
		if f.Path == "parser.go" {
			parser = string(f.Content)
		}
	}
	if !strings.Contains(parser, "type Unit_ struct") || !strings.Contains(parser, "type Unit struct{}") {
		t.Errorf("unexpected types:\n%s", parser)
	}
	if testing.Short() {
		return
	}
	dir := t.TempDir()
	writeGenerated(t, dir, "example.com/unit", files)
	goVetAndTest(t, dir, false)
}

// TestCheckNoClash checks that the names parser.go and api.go declare are checked for duplicates.
func TestCheckNoClash(t *testing.T) {
	for _, c := range []struct {
		name     string
		declared []string
		api      string
		want     string
	}{
		{"fine", []string{"A", "B", "_"}, "package p\ntype C int\nvar _ = 1\nvar _ = 2\n", ""},
		{"parser twice", []string{"A", "B", "A"}, "package p\n", "declares A twice"},
		{"api twice", []string{"A"}, "package p\ntype C int\ntype C int\n", "twice in api.go"},
		{"across", []string{"A", "C"}, "package p\ntype C int\n", "rename the type"},
	} {
		err := checkNoClash(c.declared, c.api)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s: got %v, want %q", c.name, err, c.want)
		}
	}
}

// TestEmittedRuntimeNames checks emittedRuntimeNames against the string literals of the
// generators: the runtime exports named in them, other than those in designedRuntimeNames and in
// the code of api.go (gen_runtime.go) and the list itself, are exactly the list.
func TestEmittedRuntimeNames(t *testing.T) {
	info, err := loadRuntime()
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob("gen*.go")
	if err != nil {
		t.Fatal(err)
	}
	words := regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)
	want := map[string]bool{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") || file == "gen_runtime.go" || file == "gen_emitted_names.go" {
			continue
		}
		f, err := goparser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		goast.Inspect(f, func(n goast.Node) bool {
			if lit, ok := n.(*goast.BasicLit); ok && lit.Kind == token.STRING {
				s, _ := strconv.Unquote(lit.Value)
				for _, w := range words.FindAllString(s, -1) {
					if _, ok := info.decls[w]; ok && !designedRuntimeNames[w] {
						want[w] = true
					}
				}
			}
			return true
		})
	}
	have := map[string]bool{}
	for _, name := range emittedRuntimeNames {
		have[name] = true
		if !want[name] {
			t.Errorf("emittedRuntimeNames has %s, which no generator writes", name)
		}
	}
	for name := range want {
		if !have[name] {
			t.Errorf("the generators write %s, which emittedRuntimeNames lacks", name)
		}
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
	for _, name := range runtimeNameTypes {
		if reserved[name] {
			t.Errorf("%s is reserved, but the generated package does not declare it", name)
		}
	}
	for _, name := range append(slices.Clone(emittedNameTypes), "Unit") {
		if !reserved[name] {
			t.Errorf("%s is not reserved", name)
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

// TestGeneratedRichGrammarsWithTypesNamedLikeRuntimeExports builds the parsers of the large grammars
// (struct rules, operators, lists, options, recovery) with every option and an extra type for every
// export of the runtime. A runtime name that the generators write but emittedRuntimeNames lacks lets
// the type capture the references, and the build fails, independently of the check of the list.
func TestGeneratedRichGrammarsWithTypesNamedLikeRuntimeExports(t *testing.T) {
	if testing.Short() {
		t.Skip("builds standalone generated parsers")
	}
	info, err := loadRuntime()
	if err != nil {
		t.Fatal(err)
	}
	declares := regexp.MustCompile(`(?m)^\s*type\s+(\w+)`)
	for _, name := range []string{"json", "csv", "cel", "yaml"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "parsers", name, name+".pego"))
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
			for n := range info.decls {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				if own[n] {
					continue
				}
				// The grammar language reserves some names (Error, ...).
				if g, err := syntax.Parse("type " + n + " = { Value int }\nrule main = \"a\"\n"); err != nil {
					continue
				} else if _, err := Compile(g, Options{}); err != nil {
					continue
				}
				fmt.Fprintf(&src, "type %s = { Value int }\n", n)
			}
			g, err := syntax.Parse(src.String())
			if err != nil {
				t.Fatal(err)
			}
			module := "example.com/richnames/" + name
			files, err := Generate(g, GenOptions{Module: module, Package: name, Start: "main", Runtime: RuntimeImport, Types: true, Recognize: true, Document: true, Observability: true})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			writeGenerated(t, dir, module, files)
			goVetAndTest(t, dir, false)
		})
	}
}
