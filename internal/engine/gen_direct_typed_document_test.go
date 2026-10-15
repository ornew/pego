package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

func TestGenerateDirectTypedDocument(t *testing.T) {
	if testing.Short() {
		t.Skip("builds standalone generated parsers")
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
	write("go.mod", "module directtypeddocument\n\ngo 1.27.1\n")
	for i, c := range []struct {
		grammar string
		inputs  []string
	}{
		{`type Token terminal
rule main []Token = token*
rule token Token = [aé😀]`, []string{"", "aaé😀", "b", "a\xff"}},
		{`type Token terminal
rule main Token = t:token [bc]? => $t
rule token Token = "a" &"b" / "a"`, []string{"ab", "ac", "a", "ad"}},
		{`type Token terminal
rule main Token = t:(token #recover(skip=[^;]+ ";")) => $t
rule token Token = "a;" @error(message="expected token")`, []string{"a;", "?;", "??;", "?"}},
		{`type Token terminal
rule main Token = "a" !.`, []string{"a", "ab", ""}},
		{`type Token terminal
type Count struct { N int }
rule main Count = {x := 0} token* => Count{N: x}
rule token Token = {x < 2} "a" {x := x+1}`, []string{"", "a", "aa", "aaa"}},
		{`type Token terminal
rule main Token = main "+" atom / atom
rule atom Token = [aé😀]`, []string{"a", "a+a", "é+a+😀", "a+", "+a"}},
		{`type Atom terminal
type Sum struct { L Expr; R Expr }
type Expr = Atom | Sum
rule main Expr = operators {
 operand atom
 operand "(" x:main ")" => $x
 level add { infix left "+" => Sum{L: $lhs, R: $rhs} }
}
rule atom Atom = [aé😀]`, []string{"a", "a+a", "(a+a)+é", "a+", "(a"}},
		{`type Token terminal
rule main Token = expression
rule expression Token = operators {
 operand atom
}
rule atom Token = [aé😀]`, []string{"a", "é", "😀", "", "aa"}},
		{`type Token terminal
rule main *Token = token?
rule token Token = "é"`, []string{"", "é", "a"}},
		{`type Text struct { S string }
rule main Text = [aé😀]* #atomic => Text{S: text($1)}`, []string{"", "aé😀", "b", "\xff"}},
	} {
		pkg := fmt.Sprintf("case%d", i)
		g, err := syntax.Parse(c.grammar)
		if err != nil {
			t.Fatalf("%s: %v", pkg, err)
		}
		code, err := Generate(g, GenOptions{Package: pkg, Start: "main", Types: true, Document: true})
		if err != nil {
			t.Fatalf("%s: %v", pkg, err)
		}
		if !strings.Contains(string(code), "return typedDocumentParse(") {
			t.Fatalf("%s uses conversion instead of direct matching", pkg)
		}
		write(pkg+"/parser.go", string(code))
		reference, err := Generate(g, GenOptions{Package: "reference", Start: "main", Types: true, Document: true, disableDirectTypedDocument: true})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(reference), "typedDocumentState") {
			t.Fatal("reference contains direct matching")
		}
		write(pkg+"/reference/parser.go", string(reference))
		test := strings.ReplaceAll(directTypedDocumentTests, "PACKAGE", pkg)
		test = strings.ReplaceAll(test, "INPUTS", fmt.Sprintf("%#v", c.inputs))
		write(pkg+"/parser_test.go", test)
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=-mod=mod")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("direct typed Document: %v\n%s", err, out)
	}
}

const directTypedDocumentTests = `package PACKAGE
import (
 "context"
 "encoding/json"
 "errors"
 "math/rand"
 "reflect"
 "strings"
 "testing"
 "unicode/utf8"
 reference "directtypeddocument/PACKAGE/reference"
)
var inputs=INPUTS
func jsonValue(t *testing.T,v any)string {t.Helper();b,err:=json.Marshal(v);if err!=nil{t.Fatal(err)};return string(b)}
func errorValue(err error)string {if err==nil{return ""};return err.Error()}
func assertDirect(t *testing.T,d *Document,unit Unit){
 t.Helper()
 got,gotErr:=d.ParseAST()
 want,wantErr:=ParseAST(d.Text(),WithUnit(unit))
 if !reflect.DeepEqual(got,want)||!reflect.DeepEqual(gotErr,wantErr){t.Fatalf("unit %v input %q: Document %#v / %#v; batch %#v / %#v",unit,d.Text(),got,gotErr,want,wantErr)}
 ref,refErr:=reference.ParseAST(d.Text(),reference.WithUnit(reference.Unit(unit)))
 if jsonValue(t,got)!=jsonValue(t,ref)||errorValue(gotErr)!=errorValue(refErr){t.Fatalf("unit %v input %q differs from conversion: %s / %s; %v / %v",unit,d.Text(),jsonValue(t,got),jsonValue(t,ref),gotErr,refErr)}
 if _,ok:=d.typedMemo.(*typedDocumentState);!ok{t.Fatal("direct typed cache not retained")}
}
func TestEditsAndReuse(t *testing.T){
 for _,unit:=range []Unit{CodePoints,Bytes}{
  for _,input:=range inputs{
   d,err:=NewDocument(input,WithUnit(unit));if err!=nil{t.Fatal(err)}
   assertDirect(t,d,unit)
   assertDirect(t,d,unit)
   if d.Stats().Reused==0{t.Fatal("warm parse did not reuse root")}
   // Node and typed memo tables may coexist but must not share mutable values.
   _,_=d.Parse()
   assertDirect(t,d,unit)
   rng:=rand.New(rand.NewSource(901))
   for i:=0;i<35;i++{
    boundaries:=[]int{0};for off:=range d.Text(){if off>0{boundaries=append(boundaries,off)}};if len(d.Text())>0{boundaries=append(boundaries,len(d.Text()))}
    left:=rng.Intn(len(boundaries));right:=left+rng.Intn(len(boundaries)-left)
    start,end:=boundaries[left],boundaries[right]
    if unit==CodePoints{start=utf8.RuneCountInString(d.Text()[:start]);end=utf8.RuneCountInString(d.Text()[:end])}
    insert:=[]string{"","a","b","é","😀","+",";","\xff"}[rng.Intn(8)]
    if err:=d.Edit(start,end,insert);err!=nil{t.Fatal(err)}
    assertDirect(t,d,unit)
   }
  }
 }
}
func TestControlsAndInvalidOptions(t *testing.T){
 d,err:=NewDocument(inputs[0]);if err!=nil{t.Fatal(err)}
 assertDirect(t,d,CodePoints)
 old:=d.typedMemo
 if _,err:=d.ParseAST(WithMaxDepth(-1));err==nil||d.typedMemo!=old{t.Fatal("invalid options changed cache")}
 if err:=d.Edit(-1,0,"");err==nil||d.typedMemo!=old{t.Fatal("invalid edit changed cache")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=d.ParseAST(WithContext(ctx));!errors.Is(err,context.Canceled)||d.typedMemo!=nil{t.Fatalf("cancellation cleanup: %v",err)}
 assertDirect(t,d,CodePoints)
 if _,err:=d.ParseAST(WithWorkLimits(WorkLimits{MaxSteps:1}));!errors.Is(err,ErrWorkLimit)||d.typedMemo!=nil{t.Fatalf("budget cleanup: %v",err)}
 assertDirect(t,d,CodePoints)
}
func poisonValue(v reflect.Value,seen map[uintptr]bool){
 if !v.IsValid(){return}
 switch v.Kind(){
 case reflect.Interface:if !v.IsNil(){poisonValue(v.Elem(),seen)}
 case reflect.Pointer:
  if v.IsNil()||seen[v.Pointer()]{return};seen[v.Pointer()]=true;poisonValue(v.Elem(),seen)
 case reflect.Struct:for i:=0;i<v.NumField();i++{poisonValue(v.Field(i),seen)}
 case reflect.Slice:
  for i:=0;i<v.Len();i++{poisonValue(v.Index(i),seen);if v.Index(i).CanSet(){v.Index(i).SetZero()}}
 case reflect.String:if v.CanSet(){v.SetString("caller mutation")}
 case reflect.Int:if v.CanSet(){v.SetInt(-999)}
 case reflect.Bool:if v.CanSet(){v.SetBool(!v.Bool())}
 }
}
func TestReturnedValuesAreOwned(t *testing.T){
 for _,input:=range inputs{
  d,err:=NewDocument(input);if err!=nil{t.Fatal(err)}
  value,_:=d.ParseAST()
  poisonValue(reflect.ValueOf(value),make(map[uintptr]bool))
  assertDirect(t,d,CodePoints)
 }
}
type syntaxErrorContext struct {context.Context; polls int; err error; done chan struct{}}
func(c *syntaxErrorContext)Done()<-chan struct{}{return c.done}
func(c *syntaxErrorContext)Err()error{c.polls++;if c.polls==2{return c.err};return nil}
func TestInterruptionWithSyntaxErrorPayload(t *testing.T){
 for _,failure:=range []error{&SyntaxError{Messages:[]string{"context failure"}},SyntaxErrors{&SyntaxError{Messages:[]string{"context failures"}}}}{
  d,err:=NewDocument(inputs[0]);if err!=nil{t.Fatal(err)}
  assertDirect(t,d,CodePoints)
  ctx:=&syntaxErrorContext{Context:context.Background(),err:failure,done:make(chan struct{})}
  _,err=d.ParseAST(WithContext(ctx))
  if errorValue(err)!=errorValue(failure)||d.typedMemo!=nil{t.Fatalf("interrupted cache retained: %v",err)}
  assertDirect(t,d,CodePoints)
 }
}
func TestReturnedDiagnosticsAreOwned(t *testing.T){
 for _,input:=range inputs{
  d,err:=NewDocument(input);if err!=nil{t.Fatal(err)}
  _,err=d.ParseAST()
  switch e:=err.(type){
  case SyntaxErrors:
   for _,item:=range e{item.Expected=[]string{"mutated"};item.Messages=[]string{"mutated"}}
   clear(e)
  case *SyntaxError:e.Expected=[]string{"mutated"};e.Messages=[]string{"mutated"}
  }
  _,err=d.ParseAST()
  if err!=nil&&strings.Contains(err.Error(),"mutated"){t.Fatal("caller mutation changed memo diagnostics")}
  assertDirect(t,d,CodePoints)
 }
}
`
