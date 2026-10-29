package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

func TestGenerateDirectTypedDocument(t *testing.T) {
	t.Parallel()
	testDirectTypedDocument(t, gen.GenOptions{})
}

// testDirectTypedDocument builds typed Document parsers generated with the options extra, which
// may set private generator options, and checks them against parsers that convert Node values.
func testDirectTypedDocument(t *testing.T, extra gen.GenOptions) {
	gentest.NeedsWorkControls(t)
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
	write("go.mod", gentest.GoMod("directtypeddocument"))
	for i, c := range []struct {
		grammar string
		inputs  []string
	}{
		{`type Token = { Text string }
rule main []Token = xs:token* => $xs.Value
rule token Token = t:([aé😀]) #atomic => Token{Text: text($t)}`, []string{"", "aaé😀", "b", "a\xff"}},
		{`type Token = { Text string }
rule main Token = t:token [bc]? => $t
rule token Token = t:("a" &"b" / "a") #atomic => Token{Text: text($t)}`, []string{"ab", "ac", "a", "ad"}},
		{`type Token = { Text string }
rule main Token | error = t:(token #recover(skip=[^;]+ ";")) => $t
rule token Token = t:("a;" @error(message="expected token")) #atomic => Token{Text: text($t)}`, []string{"a;", "?;", "??;", "?"}},
		{`type Token = { Text string }
rule main Token = t:("a" !.) #atomic => Token{Text: text($t)}`, []string{"a", "ab", ""}},
		{`type Token = { Text string }
type Count = { N int }
var x int
rule main Count = {x := 0} token* => Count{N: x}
rule token Token = t:({x < 2} "a" {x := x+1}) #atomic => Token{Text: text($t)}`, []string{"", "a", "aa", "aaa"}},
		{`type Token = { Text string }
rule main Token = t:(main "+" atom / atom) #atomic => Token{Text: text($t)}
rule atom Token = t:([aé😀]) #atomic => Token{Text: text($t)}`, []string{"a", "a+a", "é+a+😀", "a+", "+a"}},
		{`type Atom = { Text string }
type Sum = { L Expr; R Expr }
type Expr ~ Atom | Sum
rule main Expr = #operators {
 operand atom
 operand "(" x:main ")" => $x
 level add { infix left "+" => Sum{L: $lhs, R: $rhs} }
}
rule atom Atom = t:([aé😀]) #atomic => Atom{Text: text($t)}`, []string{"a", "a+a", "(a+a)+é", "a+", "(a"}},
		{`type Token = { Text string }
rule main Token = expression
rule expression Token = #operators {
 operand atom
}
rule atom Token = t:([aé😀]) #atomic => Token{Text: text($t)}`, []string{"a", "é", "😀", "", "aa"}},
		{`type Token = { Text string }
rule main *Token = token?
rule token Token = t:("é") #atomic => Token{Text: text($t)}`, []string{"", "é", "a"}},
		{`type Text = { S string }
rule main Text = v1:[aé😀]* #atomic => Text{S: text($v1)}`, []string{"", "aé😀", "b", "\xff"}},
	} {
		pkg := fmt.Sprintf("case%d", i)
		g, err := parseTest(c.grammar)
		if err != nil {
			t.Fatalf("%s: %v", pkg, err)
		}
		opts := extra
		opts.Package, opts.Start, opts.Types, opts.Document = pkg, "main", true, true
		code, err := gentest.GenerateCode(t, g, opts)
		if err != nil {
			t.Fatalf("%s: %v", pkg, err)
		}
		if !strings.Contains(string(code), "return rt.TypedDocumentParse(") {
			t.Fatalf("%s uses conversion instead of direct matching", pkg)
		}
		write(pkg+"/parser.go", string(code))
		reference, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "reference", Start: "main", Types: true, Document: true, ConvertTypes: true})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(reference), "return rt.TypedDocumentParse(") {
			t.Fatal("reference contains direct matching")
		}
		write(pkg+"/reference/parser.go", string(reference))
		test := strings.ReplaceAll(directTypedDocumentTests, "PACKAGE", pkg)
		test = strings.ReplaceAll(test, "INPUTS", fmt.Sprintf("%#v", c.inputs))
		write(pkg+"/parser_test.go", test)
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=-mod=mod -trimpath")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("direct typed Document: %v\n%s", err, out)
	}
}

const directTypedDocumentTests = `package PACKAGE
import (
	pegort "github.com/ornew/pego/runtime"
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
func assertDirect(t *testing.T,d *Document,unit PositionUnit){
 t.Helper()
 got,gotErr:=d.ParseAST()
 want,wantErr:=ParseAST(d.Text(),WithUnit(unit))
 if !reflect.DeepEqual(got,want)||!reflect.DeepEqual(gotErr,wantErr){t.Fatalf("unit %v input %q: Document %#v / %#v; batch %#v / %#v",unit,d.Text(),got,gotErr,want,wantErr)}
 ref,refErr:=reference.ParseAST(d.Text(),reference.WithUnit(reference.PositionUnit(unit)))
 if jsonValue(t,got)!=jsonValue(t,ref)||errorValue(gotErr)!=errorValue(refErr){t.Fatalf("unit %v input %q differs from conversion: %s / %s; %v / %v",unit,d.Text(),jsonValue(t,got),jsonValue(t,ref),gotErr,refErr)}
 if _,ok:=d.X_typedMemo.(*pegort.X_typedDocumentState);!ok{t.Fatal("direct typed cache not retained")}
}
func TestEditsAndReuse(t *testing.T){
 for _,unit:=range []PositionUnit{CodePoints,Bytes}{
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
type admissionPanicContext struct {context.Context; token any; donePanic bool; done chan struct{}}
func(c *admissionPanicContext)Done()<-chan struct{}{if c.donePanic{panic(c.token)};return c.done}
func(c *admissionPanicContext)Err()error{panic(c.token)}
func TestControlsAndInvalidOptions(t *testing.T){
 for _,unit:=range []PositionUnit{CodePoints,Bytes}{
  d,err:=NewDocument(inputs[0],WithUnit(unit));if err!=nil{t.Fatal(err)}
  assertDirect(t,d,unit)
  assertDirect(t,d,unit)
  before,old:=d.Stats(),d.X_typedMemo
  if before.Evaluated!=0||before.Reused!=1{t.Fatal("root was not warm",before)}
  unchanged:=func(){t.Helper();if d.X_typedMemo!=old||d.Stats()!=before{t.Fatal("pre-admission failure changed cache or stats",d.Stats())}}
  reused:=func(){t.Helper();assertDirect(t,d,unit);if d.Stats()!=before{t.Fatal("valid cache was not reused after pre-admission failure",d.Stats())}}
  if _,err:=d.ParseAST(WithMaxDepth(-1));err==nil{t.Fatal("invalid depth accepted")};unchanged();reused()
  if _,err:=d.ParseAST(WithUnit(PositionUnit(1-int(unit))));err==nil{t.Fatal("unit change accepted")};unchanged();reused()
  if err:=d.Edit(-1,0,"");err==nil{t.Fatal("invalid edit accepted")};unchanged();reused()
  ctx,cancel:=context.WithCancel(context.Background());cancel()
  if _,err:=d.ParseAST(WithContext(ctx));!errors.Is(err,context.Canceled){t.Fatalf("pre-admission cancellation: %v",err)};unchanged();reused()
  for _,donePanic:=range []bool{false,true}{
   token:=new(int);ctx:=&admissionPanicContext{Context:context.Background(),token:token,donePanic:donePanic,done:make(chan struct{})}
   func(){defer func(){if recover()!=token{t.Fatal("context panic identity changed")}}();d.ParseAST(WithContext(ctx));t.Fatal("context did not panic")}()
   unchanged();reused()
  }
  if _,err:=d.ParseAST(WithWorkLimits(WorkLimits{MaxSteps:1}));!errors.Is(err,ErrWorkLimit)||d.X_typedMemo!=nil{t.Fatalf("execution budget cleanup: %v",err)}
  assertDirect(t,d,unit)
 }
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
// TestReturnedValuesAreReadOnlyShared pins the contract that results are shared, read-only values,
// as Node values are: a caller that mutates a result may change what later parses return (see
// TestSharedResultMutationIsVisibleLater in the shared contract tests), so this test does not
// require the Document to stay correct after such a mutation. It requires what the contract
// promises: CloneAST returns a copy that mutating the result cannot reach.
func TestReturnedValuesAreReadOnlyShared(t *testing.T){
 for _,input:=range inputs{
  d,err:=NewDocument(input);if err!=nil{t.Fatal(err)}
  value,_:=d.ParseAST()
  want,_:=ParseAST(d.Text(),WithUnit(CodePoints))
  detached:=CloneAST(value)
  poisonValue(reflect.ValueOf(value),make(map[uintptr]bool))
  if !reflect.DeepEqual(detached,want){t.Fatalf("input %q: CloneAST changed when the result was mutated",input)}
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
  if errorValue(err)!=errorValue(failure)||d.X_typedMemo!=nil{t.Fatalf("interrupted cache retained: %v",err)}
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
