package gogen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

// TestGeneratedTypedFieldsNamedLikeRuntimeMethods checks that a struct field may be named like a
// method of the generated typed values (TName, TSpan, ...): the Go field gets another name, while
// the value reads back under the grammar name.
func TestGeneratedTypedFieldsNamedLikeRuntimeMethods(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedTypedFieldsNamedLikeRuntimeMethods)
}

func testGeneratedTypedFieldsNamedLikeRuntimeMethods(t *testing.T) {
	if testing.Short() {
		t.Skip("builds standalone generated parsers")
	}
	const src = `type Pair = { TName string; TText string; TKids string; TStruct string; TFresh string; TSetFresh string; TField string; TFieldSpend string; TSpan string; TSetSpan string; TStrField string }
rule main Pair = a:"a" => Pair{TName: "n", TText: "t", TKids: "k", TStruct: "s", TFresh: "f", TSetFresh: "sf", TField: "fd", TFieldSpend: "fw", TSpan: "sp", TSetSpan: "ss", TStrField: "sr"}`
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write := func(name, text string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", gentest.GoMod("fieldclash"))
	for _, c := range []struct {
		pkg      string
		document bool
	}{{"plain", false}, {"document", true}} {
		code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: c.pkg, Start: "main", Types: true, Document: c.document})
		if err != nil {
			t.Fatalf("%s: %v", c.pkg, err)
		}
		write(c.pkg+"/parser.go", string(code))
		test := strings.ReplaceAll(fieldClashTest, "PACKAGE", c.pkg)
		if c.document {
			test += fieldClashDocumentTest
		}
		write(c.pkg+"/parser_test.go", test)
	}
	for _, args := range [][]string{{"vet", "./..."}, {"test", "./..."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=-mod=mod -trimpath")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s: %v\n%s", args[0], err, out)
		}
	}
}

const fieldClashTest = `package PACKAGE
import "testing"
func TestFieldNames(t *testing.T){
 v,err:=ParseAST("a")
 if err!=nil{t.Fatal(err)}
 p:=v
 if p.TName_!="n"||p.TText_!="t"||p.TKids_!="k"||p.TStruct_!="s"||p.TFresh_!="f"||p.TSetFresh_!="sf"||p.TField_!="fd"||p.TFieldSpend_!="fw"||p.TSpan_!="sp"||p.TSetSpan_!="ss"||p.TStrField_!="sr"{t.Fatalf("fields %+v",p)}
 for name,w:=range map[string]string{"TName":"n","TText":"t","TKids":"k","TStruct":"s","TFresh":"f","TSetFresh":"sf","TField":"fd","TFieldSpend":"fw","TSpan":"sp","TSetSpan":"ss","TStrField":"sr"}{
  got,ok:=p.TField(name)
  if !ok||got!=w{t.Fatalf("TField(%q) = %v, %v; want %q",name,got,ok,w)}
 }
}
`

const fieldClashDocumentTest = `
func TestFieldNamesDocument(t *testing.T){
 d,err:=NewDocument("a")
 if err!=nil{t.Fatal(err)}
 v,err:=d.ParseAST()
 if err!=nil{t.Fatal(err)}
 if p:=v;p.TName_!="n"||p.TSetSpan_!="ss"{t.Fatalf("fields %+v",p)}
 if err:=d.Edit(0,1,"a");err!=nil{t.Fatal(err)}
 v,err=d.ParseAST()
 if err!=nil||v.TKids_!="k"{t.Fatalf("after edit: %v %v",v,err)}
}
`
