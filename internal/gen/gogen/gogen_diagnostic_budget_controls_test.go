package gogen_test

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

func TestGeneratedGoDiagnosticBudget(t *testing.T) {
	t.Parallel()
	gentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated diagnostic-control consumers")
	}
	g, err := parseTest(`type Word = { Text string }

rule main Word = t:(letter #recover(skip="!")) #atomic => Word{Text: text($t)}

rule letter Word = t:("z" / "a" / "é") #atomic => Word{Text: text($t)}`)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gentest.GoMod("diagnosticfixture")), 0644); err != nil {
		t.Fatal(err)
	}
	for _, convert := range []bool{false, true} {
		pkg := fmt.Sprintf("diagnostic_%v", convert)
		code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: pkg, Start: "main", Types: true, ConvertTypes: convert})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, pkg)
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
		fixture := strings.NewReplacer("PACKAGE", pkg).Replace(generatedGoDiagnosticConsumer)
		for name, data := range map[string][]byte{"parser.go": code, "diagnostic_test.go": []byte(fixture)} {
			if err := os.WriteFile(filepath.Join(path, name), data, 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated diagnostic accounting: %v\n%s", err, out)
	}
}

const generatedGoDiagnosticConsumer = `package PACKAGE

import pegort "github.com/ornew/pego/runtime"
import("context";"errors";"fmt";"reflect";"slices";"sort";"strings";"testing")
func diagnosticParser(limits Limits)*pegort.Parser{return &pegort.Parser{Control:pegort.X_newBudgetControl(nil,limits)}}
func catchFatal(t *testing.T,f func())(err error){t.Helper();defer func(){if v:=recover();v!=nil{fatal,ok:=v.(pegort.X_fatal);if !ok{panic(v)};err=fatal.X_err}}();f();return nil}
type pollingContext struct{context.Context}
func(*pollingContext)Done()<-chan struct{}{return make(chan struct{})}
func(*pollingContext)Err()error{return context.Canceled}
func TestDiagnosticTextParityAndBudget(t *testing.T){
 maxInt:=int(^uint(0)>>1)
 rows:=[]*SyntaxError{
  {Line:1,Col:1},
  {Line:0,Col:0,Messages:[]string{""},Expected:[]string{"ignored"}},
  {Line:-1,Col:-2,Messages:[]string{"","two","","é\xff"}},
  {Line:maxInt,Col:-maxInt-1,Expected:[]string{"","x","","é","\xff"}},
  {Line:99,Col:101,Messages:[]string{strings.Repeat("λ\xff",180)}},
  {Line:2,Col:3,Expected:make([]string,320)},
 }
 for i,e:=range rows{t.Run(fmt.Sprint(i),func(t *testing.T){
  want:=e.Error();parts:=e.Messages;if len(parts)==0{parts=e.Expected}
  steps:=uint64(len(want)+2*len(parts))
  p:=diagnosticParser(Limits{Budget:steps})
  retained:=p.RecoveryErrorText(e)
  if retained!=want{t.Fatal("message parity",retained,want)}
  if p.Control.X_budgetUsed()!=steps{t.Fatalf("text spend %+v",p.Control)}
  p.Control=pegort.X_newBudgetControl(nil,Limits{Budget:steps-1})
  if err:=catchFatal(t,func(){p.RecoveryErrorText(e)});!errors.Is(err,ErrLimit){t.Fatal("one-short text",err)}
  p.Control=pegort.X_newBudgetControl(nil,Limits{Budget:100000})
  p.RecoveryErrorText(&SyntaxError{Line:1,Col:2,Messages:[]string{"other"}})
  if retained!=want || e.Error()!=want{t.Fatal("retained message overwritten")}
 })}
 p:=diagnosticParser(Limits{Budget:100000})
 p.Control.X_ctx=&pollingContext{Context:context.Background()}
 var partial string
 if err:=catchFatal(t,func(){partial=p.RecoveryErrorText(rows[5])});!errors.Is(err,context.Canceled)||partial!=""{t.Fatal("format cancellation published partial",partial,err)}
}
func TestDiagnosticSortingAndSnapshots(t *testing.T){
 for _,n:=range []int{0,1,2,3,7,8,9,127,128,129,511}{
  values:=make([]string,n)
  for i:=range values{values[i]=[]string{"","é","\xff","z","\xc3\xff",strings.Repeat("p",300)}[(n-i)%6]+strings.Repeat("p",(n-i)%3)}
  want:=slices.Clone(values);sort.Strings(want)
  p:=diagnosticParser(Limits{Budget:1<<50});p.X_sortExpected(values)
  if !slices.Equal(values,want){t.Fatalf("sort length %d changed",n)}
 }
 p:=diagnosticParser(Limits{Budget:128})
 if err:=catchFatal(t,func(){p.X_sortExpected(make([]string,320))});!errors.Is(err,ErrLimit){t.Fatal("sort moves unbounded",err)}
 ids:=make([]pegort.ExpID,320);for i:=range ids{ids[i]=pegort.ExpID(i)}
 p=diagnosticParser(Limits{Budget:100000})
 kept:=p.Keep(ids);before:=slices.Clone(kept)
 p.Control=pegort.X_newBudgetControl(nil,Limits{Budget:319})
 if err:=catchFatal(t,func(){p.Keep(ids)});!errors.Is(err,ErrLimit){t.Fatal("snapshot equality unbounded",err)}
 if !slices.Equal(kept,before){t.Fatal("snapshot overwritten")}
 p=diagnosticParser(Limits{Budget:319})
 if err:=catchFatal(t,func(){p.Keep(ids)});!errors.Is(err,ErrLimit){t.Fatal("snapshot copy unbounded",err)}
 p=diagnosticParser(Limits{Budget:100000});p.MergeExpected(0,ids)
 p.Control=pegort.X_newBudgetControl(nil,Limits{Budget:128})
 if err:=catchFatal(t,func(){p.MergeExpected(0,ids)});!errors.Is(err,ErrLimit){t.Fatal("merged expectations unbounded",err)}
 errorsList:=make([]*SyntaxError,320)
 p=diagnosticParser(Limits{Budget:319})
 if err:=catchFatal(t,func(){p.X_copyErrors(nil,errorsList)});!errors.Is(err,ErrLimit){t.Fatal("recovered error copies unbounded",err)}
}
func TestRecoveredPublicDiagnostics(t *testing.T){
 for _,unit:=range []PositionUnit{Bytes,CodePoints}{
  retained,err:=Parse("!",WithUnit(unit));var recovered SyntaxErrors
  if !errors.As(err,&recovered)||len(recovered)!=1{t.Fatal("baseline recovery",retained,err)}
  expected:=[]string{"\"a\"","\"z\"","\"é\""}
  if !slices.Equal(recovered[0].Expected,expected){t.Fatalf("recovery sorting changed: %v",recovered[0].Expected)}
  before:=retained.String();message:=err.Error();diagnostic:=*recovered[0]
  for _,ast:=range []bool{false,true}{
   call:=func(steps uint64)(any,error){o:=[]ParseOption{WithUnit(unit),WithLimits(Limits{Budget:steps})};if ast{v,e:=ParseAST("!",o...);return v,e};v,e:=Parse("!",o...);return v,e}
   lo,hi:=uint64(1),uint64(100000)
   for lo<hi{mid:=lo+(hi-lo)/2;_,e:=call(mid);if errors.Is(e,ErrLimit){lo=mid+1}else if errors.As(e,&recovered){hi=mid}else{t.Fatal("unexpected diagnostic outcome",e)}}
   _,err:=call(lo);if !errors.As(err,&recovered){t.Fatal("exact recovery limit",err)}
   v,err:=call(lo-1)
   if (v!=nil && !reflect.ValueOf(v).IsZero()) || !errors.Is(err,ErrLimit){t.Fatal("partial recovery published",v,err)}
  }
  if retained.String()!=before || message!=diagnostic.Error() || !slices.Equal(diagnostic.Expected,expected){t.Fatal("retained recovery changed")}
  if n,err:=Parse("é",WithUnit(unit));err!=nil||n==nil{t.Fatal("valid retry",n,err)}
 }
}
`

func TestGeneratedGoRecoveryMessageBudget(t *testing.T) {
	t.Parallel()
	gentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated long recovery-message consumers")
	}
	message := strings.Repeat("details λ; ", 512)
	g, err := parseTest(fmt.Sprintf(`type Text ~ node[string] | error
type Doc = { Text Text }
 rule main Doc = x:(("z" @error(message=%q)) #recover(skip="!")) => Doc{Text: $x}`, message))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gentest.GoMod("recoverytext")), 0644); err != nil {
		t.Fatal(err)
	}
	for _, convert := range []bool{false, true} {
		pkg := fmt.Sprintf("text_%v", convert)
		code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: pkg, Start: "main", Types: true, Recognize: true, ConvertTypes: convert})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, pkg)
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
		fixture := strings.NewReplacer("PACKAGE", pkg, "LONG_MESSAGE", fmt.Sprintf("%q", message)).Replace(generatedGoRecoveryTextConsumer)
		for name, data := range map[string][]byte{"parser.go": code, "text_test.go": []byte(fixture)} {
			if err := os.WriteFile(filepath.Join(path, name), data, 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated long recovery message: %v\n%s", err, out)
	}
}

const generatedGoRecoveryTextConsumer = `package PACKAGE
import("errors";"reflect";"testing")
func TestLongRecoveredMessage(t *testing.T){
 message:=LONG_MESSAGE
 for _,unit:=range []PositionUnit{Bytes,CodePoints}{
  retained,err:=Parse("!",WithUnit(unit));var recovered SyntaxErrors
  if !errors.As(err,&recovered)||len(recovered)!=1 || len(recovered[0].Messages)!=1 || recovered[0].Messages[0]!=message{t.Fatal("recovery label changed",err)}
  want:=recovered[0].Error();captured,ok:=retained.Field("Text").(*Node);if !ok || captured.Kind()!=KindError || captured.Text()!=want{t.Fatalf("recovered message changed: %v",retained)}
  for _,ast:=range []bool{false,true}{
   var v any
   opts:=[]ParseOption{WithUnit(unit),WithLimits(Limits{Budget:256})}
   if ast{v,err=ParseAST("!",opts...)}else{v,err=Parse("!",opts...)}
   if (v!=nil && !reflect.ValueOf(v).IsZero()) || !errors.Is(err,ErrLimit){t.Fatalf("AST=%v long diagnostic copy unbounded: value=%T error=%T",ast,v,err)}
  }
  // Recognition records the error but does not construct a node message.
  err=Recognize("!",WithUnit(unit),WithLimits(Limits{Budget:256}))
  if !errors.As(err,&recovered){t.Fatal("recognizer charged unconstructed node message",err)}
  if captured.Text()!=want{t.Fatal("retained message overwritten")}
  if _,err:=ParseAST("!",WithUnit(unit),WithLimits(Limits{Budget:100000}));!errors.As(err,&recovered){t.Fatal("generous recovery retry",err)}
 }
}
`
