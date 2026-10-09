package engine

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

const aliasExportsGrammar = `
type Word terminal
type Other terminal
type Alias = Word
type Chain = Alias
type Single = Word | Word
type U = Word | Other
type V = Word | Other | Word
type W = V
type MaybeUnion = *U
type Words = []Alias
type Count = int
type Maybe = *int
type Mixed = Word | int
type Anything = any
type CST = Seq
type Box struct { Item Chain, Items Words, Choice V, N Count }
type BoxAlias = Box
type Node = Word
type Node_ = Other
type Span = int
def main: Alias = "a"`

// Compile a separate consumer: compiling the generated package alone does not
// detect missing exports, and aliases must preserve assignment compatibility.
func TestGeneratedAliasExports(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	g, err := syntax.Parse(aliasExportsGrammar)
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
	write("go.mod", []byte("module aliasexports\n\ngo 1.24\n"))
	var tests strings.Builder
	tests.WriteString("package aliasexports\nimport (\"testing\"\n")
	var bodies strings.Builder
	for i, convert := range []bool{false, true} {
		pkg := fmt.Sprintf("g%d", i)
		opts := GenOptions{Package: pkg, Start: "main", Types: true, convertTypes: convert}
		code, err := Generate(g, opts)
		if err != nil {
			t.Fatal(err)
		}
		again, err := Generate(g, opts)
		if err != nil || !bytes.Equal(code, again) {
			t.Fatalf("generation is not deterministic: %v", err)
		}
		write(pkg+"/parser.go", code)
		fmt.Fprintf(&tests, "%s \"aliasexports/%s\"\n", pkg, pkg)
		// Every declared name is used, including pointer-valued ordinary aliases,
		// normalized unions, scalar/list/CST aliases and colliding runtime names.
		body := fmt.Sprintf(`
func Test%s(t *testing.T) {
 var word *P.Word = &P.Word{Text:"a"}
 var other *P.Other = &P.Other{Text:"b"}
 var alias P.Alias = word
 var chain P.Chain = alias
 var single P.Single = chain
 var u P.U = word
 var v P.V = u
 var w P.W = other
 var maybeUnion P.MaybeUnion = w
 var words P.Words = []*P.Word{chain}
 var count P.Count = 3
 var maybe P.Maybe = &count
 var mixed P.Mixed = count
 var anything P.Anything = mixed
 var cst P.CST = &P.Node{}
 var box *P.Box = &P.Box{Item:chain, Items:words, Choice:v, N:*maybe}
 var boxAlias P.BoxAlias = box
 var node P.Node_ = word
 var node_ P.Node__ = other
 var span P.Span = 1
 _ = []any{single,maybeUnion,anything,cst,boxAlias,node,node_,span}
 for _,unit := range []P.Unit{P.CodePoints,P.Bytes} {
  var result P.Alias
  var err error
  result,err=P.ParseAST("a",unit)
  if err!=nil || result.Text!="a" {t.Fatalf("parse: %%v",err)}
 }
}
`, strings.ToUpper(pkg))
		bodies.WriteString(strings.ReplaceAll(body, "P.", pkg+"."))
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
			src.WriteString("type Word terminal\n")
			for i := range n {
				fmt.Fprintf(&src, "type Alias%d = Word\n", i)
			}
			src.WriteString("def main: Word = \"a\"\n")
			g, err := syntax.Parse(src.String())
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Generate(g, GenOptions{Package: "bench", Start: "main", Types: true}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
