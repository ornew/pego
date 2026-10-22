package engine

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const aliasExportsGrammar = `type Word = { Text string }
type Other = { Text string }
type Alias ~ Word
type Chain ~ Alias
type Single ~ Word | Word
type U ~ Word | Other
type V ~ Word | Other | Word
type W ~ V
type MaybeUnion ~ *U
type Words ~ []Alias
type Count ~ int
type Maybe ~ *int
type Mixed ~ Word | int
type Anything ~ _
type CST ~ node[_]
type Box = { Item Chain; Items Words; Choice V; N Count }
type BoxAlias ~ Box
type Node ~ Word
type Node_ ~ Other
type Span ~ int
rule main Alias = t:("a") #atomic => Word{Text: text($t)}`

// Compile a separate consumer: compiling the generated package alone does not
// detect missing exports, and aliases must preserve assignment compatibility.
func TestGeneratedAliasExports(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	g, err := parseTest(aliasExportsGrammar)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write := func(name string, data []byte) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", []byte(testGoMod("aliasexports")))
	var tests strings.Builder
	tests.WriteString("package aliasexports\nimport (\"testing\"\n")
	var bodies strings.Builder
	for i, convert := range []bool{false, true} {
		pkg := fmt.Sprintf("g%d", i)
		opts := GenOptions{Package: pkg, Module: "aliasexports/" + pkg, Runtime: RuntimeImport, Start: "main", Types: true, convertTypes: convert}
		files, err := Generate(g, opts)
		if err != nil {
			t.Fatal(err)
		}
		again, err := Generate(g, opts)
		if err != nil || !slices.EqualFunc(files, again, func(a, b GeneratedFile) bool { return a.Path == b.Path && bytes.Equal(a.Content, b.Content) }) {
			t.Fatalf("generation is not deterministic: %v", err)
		}
		for _, f := range files {
			write(pkg+"/"+f.Path, f.Content)
		}
		fmt.Fprintf(&tests, "%s \"aliasexports/%s\"\n%s \"aliasexports/%s/types\"\n", pkg, pkg, "t"+pkg, pkg)
		// Every declared name is used, including pointer-valued ordinary aliases,
		// normalized unions, scalar/list/CST aliases and colliding runtime names.
		body := fmt.Sprintf(`
func Test%s(t *testing.T) {
 var word *T.Word = &T.Word{Text:"a"}
 var other *T.Other = &T.Other{Text:"b"}
 var alias T.Alias = word
 var chain T.Chain = alias
 var single T.Single = chain
 var u T.U = word
 var v T.V = u
 var w T.W = other
 var maybeUnion T.MaybeUnion = w
 var words T.Words = []*T.Word{chain}
 var count T.Count = 3
 var maybe T.Maybe = &count
 var mixed T.Mixed = count
 var anything T.Anything = mixed
 var cst T.CST = &T.TNode[any]{}
 var box *T.Box = &T.Box{Item:chain, Items:words, Choice:v, N:*maybe}
 var boxAlias T.BoxAlias = box
 var node T.Node = word
 var node_ T.Node_ = other
 var span T.Span = 1
 var runtimeNode *P.Node
 _ = []any{single,maybeUnion,anything,cst,boxAlias,node,node_,span,runtimeNode}
 for _,unit := range []P.PositionUnit{P.CodePoints,P.Bytes} {
  var result T.Alias
  var err error
  result,err=P.ParseAST("a",P.WithUnit(unit))
  if err!=nil || result.Text!="a" {t.Fatalf("parse: %%v",err)}
 }
}
`, strings.ToUpper(pkg))
		bodies.WriteString(strings.ReplaceAll(strings.ReplaceAll(body, "P.", pkg+"."), "T.", "t"+pkg+"."))
	}
	tests.WriteString(")\n" + bodies.String())
	write("exports_test.go", []byte(tests.String()))
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("alias consumer: %v\n%s", err, out)
	}
}

func BenchmarkGenerateAliasExports(b *testing.B) {
	for _, n := range []int{1, 100} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			var src strings.Builder
			src.WriteString("type Word = { Text string }\n")
			for i := range n {
				fmt.Fprintf(&src, "type Alias%d = Word\n", i)
			}
			src.WriteString("rule main Word = \"a\"\n")
			g, err := parseTest(src.String())
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := generateCode(g, GenOptions{Package: "bench", Start: "main", Types: true}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
