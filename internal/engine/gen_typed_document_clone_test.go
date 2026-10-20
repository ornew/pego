package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

func TestGeneratedTypedDocumentPublicationClone(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a standalone generated parser")
	}
	g, err := syntax.Parse(`type Token = { Text string }
type Document = { Text string }
type ParseStats = { Text string }
type Span = { Text string }
type Link = { Label *string; Next *Link }
type Alt ~ Token | Link
type Box = { First Token; Again Token; Items []Token; AgainItems []Token; Optional []*Token; Nested [][]Token; Empty []Token; NilList []Token; Choice Alt; Link *Link }
rule main = x:token => Box{First: $x, Again: $x, Items: []Token{$x, $x}, Optional: []*Token{nil, $x}, Nested: [][]Token{[]Token{$x}}, Choice: $x}
rule token Token = t:("x") #atomic => Token{Text: text($t)}`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := generateCode(g, GenOptions{Package: "fixture", Start: "main", Types: true, Document: true})
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := generateCode(g, GenOptions{Package: "fixture", Start: "main", Types: true})
	if err != nil || strings.Contains(string(ordinary), "type typedDocumentClone") {
		t.Fatalf("default generation includes Document cloning: %v", err)
	}
	dir := t.TempDir()
	for name, text := range map[string]string{
		"go.mod":    testGoMod("typeddocumentpublication"),
		"parser.go": string(code),
		"parser_test.go": `package fixture
import (
	pegort "github.com/ornew/pego/runtime"
 "context"
 "errors"
 "testing"
)
func TestPublicationOwnershipAndAliases(t *testing.T) {
 token := &Token{Text:"private"}
 label := "private"
 link := &Link{Label:&label}
 link.Next = link
 items := make([]*Token,2,4)
 items[0],items[1]=token,token
 input := &Box{First:token,Again:token,Items:items,AgainItems:items,Optional:[]*Token{nil,token},Nested:[][]*Token{items},Empty:make([]*Token,0,8),Choice:token,Link:link}
 c := &typedDocumentClone{}
 output := c.toBox(input)
 c.Finish()
 c.Release()
 if output==input || output.First==token || output.First!=output.Again || output.Items[0]!=output.First || output.Optional[0]!=nil || output.Optional[1]!=output.First || output.Choice.(*Token)!=output.First {t.Fatal("typed identity changed")}
 if &output.Items[0]!=&output.AgainItems[0] || &output.Items[0]!=&output.Nested[0][0] {t.Fatal("shared slice views changed")}
 if output.Empty==nil || output.NilList!=nil {t.Fatal("nil and empty changed")}
 if output.Link==link || output.Link.Next!=output.Link || output.Link.Label==&label {t.Fatal("recursive graph or scalar pointer changed")}
 output.First.Text="public"
 *output.Link.Label="public"
 if token.Text!="private" || label!="private" {t.Fatal("publication exposed memo values")}
 if c.Control!=nil || c.Values!=nil || c.Slices!=nil || c.Tasks!=nil {t.Fatal("publication retained private metadata")}
}
func TestPublicationDeepGraph(t *testing.T) {
 var input *Link
 for i:=0;i<100001;i++ {input=&Link{Next:input}}
 c := &typedDocumentClone{}
 output:=c.toLink(input)
 c.Finish()
 c.Release()
 count:=0
 for output!=nil {count++;output=output.Next}
 if count!=100001 {t.Fatalf("deep clone length: %d",count)}
}
func cloneWithControl(input *Box, c *typedDocumentClone,beforeFinish ...func())(output *Box,err error) {
 defer pegort.X_recoverParse(&output,&err)
 defer c.Release()
 output=c.toBox(input)
 if len(beforeFinish)>0 {beforeFinish[0]()}
 c.Finish()
 return output,nil
}
func TestPublicationControls(t *testing.T) {
 input:=&Box{First:&Token{Text:"private"},Items:make([]*Token,1000)}
 c:=&typedDocumentClone{TypedDocumentCloneBase: pegort.TypedDocumentCloneBase{Control: pegort.X_newWorkControl(nil,WorkLimits{MaxSteps:1})}}
 if output,err:=cloneWithControl(input,c);output!=nil || !errors.Is(err,ErrWorkLimit) {t.Fatalf("budget: %v %v",output,err)}
 if c.Control!=nil || c.Values!=nil || c.Slices!=nil || c.Tasks!=nil {t.Fatal("interrupted clone retained state")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 c=&typedDocumentClone{TypedDocumentCloneBase: pegort.TypedDocumentCloneBase{Control: pegort.X_newWorkControl(ctx,WorkLimits{})}}
 if output,err:=cloneWithControl(input,c);output!=nil || !errors.Is(err,context.Canceled) {t.Fatalf("cancel: %v %v",output,err)}
 ctx,cancel=context.WithCancel(context.Background())
 c=&typedDocumentClone{TypedDocumentCloneBase: pegort.TypedDocumentCloneBase{Control: pegort.X_newWorkControl(ctx,WorkLimits{})}}
 if output,err:=cloneWithControl(&Box{First:&Token{}},c,cancel);output!=nil || !errors.Is(err,context.Canceled) {t.Fatalf("final cancellation poll: %v %v",output,err)}
 if output,err:=cloneWithControl(input,&typedDocumentClone{});output==nil || err!=nil {t.Fatalf("retry: %v",err)}
}
func TestPublicationCollidingNames(t *testing.T) {
 c:=&typedDocumentClone{}
 if c.toDocument_(&Document_{Text:"m"}).Text!="m" || c.toParseStats_(&ParseStats_{Text:"e"}).Text!="e" || c.toSpan(&Span{Text:"s"}).Text!="s" {t.Fatal("runtime name collision")}
 c.Finish();c.Release()
}
`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=-mod=mod -trimpath")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("typed Document publication: %v\n%s", err, output)
	}
}
