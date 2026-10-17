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

func TestGeneratedGoDiagnosticWork(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated diagnostic-control consumers")
	}
	g, err := syntax.Parse(`type Word terminal

rule main Word = letter #recover(skip="!")

rule letter Word = "z" / "a" / "é"`)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module diagnosticfixture\n\ngo 1.27.1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, convert := range []bool{false, true} {
		for _, reference := range []bool{false, true} {
			pkg := fmt.Sprintf("diagnostic_%v_%v", convert, reference)
			code, err := Generate(g, GenOptions{Package: pkg, Start: "main", Types: true, convertTypes: convert, referenceWorkControls: reference})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, pkg)
			if err := os.Mkdir(path, 0755); err != nil {
				t.Fatal(err)
			}
			fixture := strings.NewReplacer("PACKAGE", pkg, "REFERENCE", fmt.Sprint(reference)).Replace(generatedGoDiagnosticConsumer)
			for name, data := range map[string][]byte{"parser.go": code, "diagnostic_test.go": []byte(fixture)} {
				if err := os.WriteFile(filepath.Join(path, name), data, 0644); err != nil {
					t.Fatal(err)
				}
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
import("context";"errors";"fmt";"reflect";"slices";"sort";"strings";"testing")
const reference = REFERENCE
func diagnosticParser(limits WorkLimits)*parser{return &parser{control:newWorkControl(nil,limits)}}
func catchFatal(t *testing.T,f func())(err error){t.Helper();defer func(){if v:=recover();v!=nil{fatal,ok:=v.(fatal);if !ok{panic(v)};err=fatal.err}}();f();return nil}
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
  p:=diagnosticParser(WorkLimits{MaxSteps:steps})
  retained:=p.recoveryErrorText(e)
  if retained!=want{t.Fatal("message parity",retained,want)}
  if reference{if p.control.stepsUsed!=0{t.Fatal("reference charged")};return}
  if p.control.stepsUsed!=steps || p.control.inputUsed!=0{t.Fatalf("text work %+v",p.control)}
  p.control=newWorkControl(nil,WorkLimits{MaxSteps:steps-1})
  if err:=catchFatal(t,func(){p.recoveryErrorText(e)});!errors.Is(err,ErrWorkLimit){t.Fatal("one-short text",err)}
  p.control=newWorkControl(nil,WorkLimits{MaxSteps:100000})
  p.recoveryErrorText(&SyntaxError{Line:1,Col:2,Messages:[]string{"other"}})
  if retained!=want || e.Error()!=want{t.Fatal("retained message overwritten")}
 })}
 if reference{return}
 p:=diagnosticParser(WorkLimits{MaxSteps:100000})
 p.control.ctx=&pollingContext{Context:context.Background()}
 var partial string
 if err:=catchFatal(t,func(){partial=p.recoveryErrorText(rows[5])});!errors.Is(err,context.Canceled)||partial!=""{t.Fatal("format cancellation published partial",partial,err)}
}
func TestDiagnosticSortingAndSnapshots(t *testing.T){
 for _,n:=range []int{0,1,2,3,7,8,9,127,128,129,511}{
  values:=make([]string,n)
  for i:=range values{values[i]=[]string{"","é","\xff","z","\xc3\xff",strings.Repeat("p",300)}[(n-i)%6]+strings.Repeat("p",(n-i)%3)}
  want:=slices.Clone(values);sort.Strings(want)
  p:=diagnosticParser(WorkLimits{MaxSteps:1<<50});p.sortExpected(values)
  if !slices.Equal(values,want){t.Fatalf("sort length %d changed",n)}
 }
 if reference{return}
 p:=diagnosticParser(WorkLimits{MaxSteps:128})
 if err:=catchFatal(t,func(){p.sortExpected(make([]string,320))});!errors.Is(err,ErrWorkLimit){t.Fatal("sort moves unbounded",err)}
 ids:=make([]expID,320);for i:=range ids{ids[i]=expID(i)}
 p=diagnosticParser(WorkLimits{MaxSteps:100000})
 kept:=p.keep(ids);before:=slices.Clone(kept)
 p.control=newWorkControl(nil,WorkLimits{MaxSteps:319})
 if err:=catchFatal(t,func(){p.keep(ids)});!errors.Is(err,ErrWorkLimit){t.Fatal("snapshot equality unbounded",err)}
 if !slices.Equal(kept,before){t.Fatal("snapshot overwritten")}
 p=diagnosticParser(WorkLimits{MaxSteps:319})
 if err:=catchFatal(t,func(){p.keep(ids)});!errors.Is(err,ErrWorkLimit){t.Fatal("snapshot copy unbounded",err)}
 p=diagnosticParser(WorkLimits{MaxSteps:100000});p.mergeExpected(0,ids)
 p.control=newWorkControl(nil,WorkLimits{MaxSteps:128})
 if err:=catchFatal(t,func(){p.mergeExpected(0,ids)});!errors.Is(err,ErrWorkLimit){t.Fatal("merged expectations unbounded",err)}
 errorsList:=make([]*SyntaxError,320)
 p=diagnosticParser(WorkLimits{MaxSteps:319})
 if err:=catchFatal(t,func(){p.copyErrors(nil,errorsList)});!errors.Is(err,ErrWorkLimit){t.Fatal("recovered error copies unbounded",err)}
}
func TestRecoveredPublicDiagnostics(t *testing.T){
 for _,unit:=range []Unit{Bytes,CodePoints}{
  retained,err:=Parse("!",WithUnit(unit));var recovered SyntaxErrors
  if !errors.As(err,&recovered)||len(recovered)!=1{t.Fatal("baseline recovery",retained,err)}
  expected:=[]string{"\"a\"","\"z\"","\"é\""}
  if !slices.Equal(recovered[0].Expected,expected){t.Fatalf("recovery sorting changed: %v",recovered[0].Expected)}
  before:=retained.String();message:=err.Error();diagnostic:=*recovered[0]
  if reference{continue}
  for _,ast:=range []bool{false,true}{
   call:=func(steps uint64)(any,error){o:=[]ParseOption{WithUnit(unit),WithWorkLimits(WorkLimits{MaxSteps:steps})};if ast{v,e:=ParseAST("!",o...);return v,e};v,e:=Parse("!",o...);return v,e}
   lo,hi:=uint64(1),uint64(100000)
   for lo<hi{mid:=lo+(hi-lo)/2;_,e:=call(mid);if errors.Is(e,ErrWorkLimit){lo=mid+1}else if errors.As(e,&recovered){hi=mid}else{t.Fatal("unexpected diagnostic outcome",e)}}
   _,err:=call(lo);if !errors.As(err,&recovered){t.Fatal("exact recovery limit",err)}
   v,err:=call(lo-1)
   if (v!=nil && !reflect.ValueOf(v).IsZero()) || !errors.Is(err,ErrWorkLimit){t.Fatal("partial recovery published",v,err)}
  }
  if retained.String()!=before || message!=diagnostic.Error() || !slices.Equal(diagnostic.Expected,expected){t.Fatal("retained recovery changed")}
  if n,err:=Parse("é",WithUnit(unit));err!=nil||n==nil{t.Fatal("valid retry",n,err)}
 }
}
`

func TestGeneratedGoRecoveryMessageWork(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated long recovery-message consumers")
	}
	message := strings.Repeat("details λ; ", 512)
	g, err := syntax.Parse(fmt.Sprintf(`type Doc = { Text Match }
 rule main Doc = x:(("z" @error(message=%q)) #recover(skip="!")) => Doc{Text: $x}`, message))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module recoverytext\n\ngo 1.27.1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, convert := range []bool{false, true} {
		for _, general := range []bool{false, true} {
			pkg := fmt.Sprintf("text_%v_%v", convert, general)
			code, err := Generate(g, GenOptions{Package: pkg, Start: "main", Types: true, Recognize: true, convertTypes: convert, disableTypedRecoveryBodies: general})
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
 for _,unit:=range []Unit{Bytes,CodePoints}{
  retained,err:=Parse("!",WithUnit(unit));var recovered SyntaxErrors
  if !errors.As(err,&recovered)||len(recovered)!=1 || len(recovered[0].Messages)!=1 || recovered[0].Messages[0]!=message{t.Fatal("recovery label changed",err)}
  want:=recovered[0].Error();captured,ok:=retained.Field("Text").(*Node);if !ok || captured.Field("message")!=want{t.Fatalf("recovered message changed: %v",retained)}
  for _,ast:=range []bool{false,true}{
   var v any
   opts:=[]ParseOption{WithUnit(unit),WithWorkLimits(WorkLimits{MaxSteps:256})}
   if ast{v,err=ParseAST("!",opts...)}else{v,err=Parse("!",opts...)}
   if (v!=nil && !reflect.ValueOf(v).IsZero()) || !errors.Is(err,ErrWorkLimit){t.Fatalf("AST=%v long diagnostic copy unbounded: value=%T error=%T",ast,v,err)}
  }
  // Recognition records the error but does not construct a node message.
  err=Recognize("!",WithUnit(unit),WithWorkLimits(WorkLimits{MaxSteps:256}))
  if !errors.As(err,&recovered){t.Fatal("recognizer charged unconstructed node message",err)}
  if captured.Field("message")!=want{t.Fatal("retained message overwritten")}
  if _,err:=ParseAST("!",WithUnit(unit),WithWorkLimits(WorkLimits{MaxSteps:100000}));!errors.As(err,&recovered){t.Fatal("generous recovery retry",err)}
 }
}
`
