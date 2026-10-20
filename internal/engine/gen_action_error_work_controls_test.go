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

func TestGeneratedGoActionErrorWork(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated action-error consumers")
	}
	rule, variable := strings.Repeat("r", 8192), strings.Repeat("u", 8192)
	cases := []struct {
		name, start, grammar, want string
		recognize                  bool
	}{
		{"prefix", rule, `type A = { N int }
 rule ` + rule + ` A = a:"a" #cut => A{N:1 / (len($a)-1)}`, "action in " + rule + ": division by zero", false},
		{"predicate", "main", `type Word = { Text string }
 rule main Word = (({` + variable + `} x:leaf) / x:leaf) #cut => $x
 rule leaf Word = t:("a") #atomic => Word{Text: text($t)}`, "", true},
		{"captured", "main", `type A = { N int }
 rule main A = x:` + rule + ` {$x != nil} $$ #cut => $x
 rule ` + rule + ` A = a:"a" #cut => A{N:1 / (len($a)-1)}`, "action in " + rule + ": division by zero", true},
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module actionerrorfixture\n\ngo 1.27.1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		g, err := syntax.Parse(c.grammar)
		if err != nil {
			t.Fatal(err)
		}
		for _, convert := range []bool{false, true} {
			for _, general := range []bool{false, true} {
				pkg := fmt.Sprintf("action_%s_%v_%v", c.name, convert, general)
				code, err := Generate(g, GenOptions{Package: pkg, Start: c.start, Types: true, Recognize: true, convertTypes: convert, disableTypedCuts: general})
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(dir, pkg)
				if err := os.Mkdir(path, 0755); err != nil {
					t.Fatal(err)
				}
				consumer := strings.NewReplacer("PACKAGE", pkg, "WANT_ERROR", fmt.Sprintf("%q", c.want), "CHECK_RECOGNIZE", fmt.Sprint(c.recognize)).Replace(generatedGoActionErrorConsumer)
				for name, data := range map[string][]byte{"parser.go": code, "action_test.go": []byte(consumer)} {
					if err := os.WriteFile(filepath.Join(path, name), data, 0644); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated action-error accounting: %v\n%s", err, out)
	}
}

const generatedGoActionErrorConsumer = `package PACKAGE
import("context";"errors";"reflect";"strings";"testing")
const wantError=WANT_ERROR
const recognize=CHECK_RECOGNIZE
func actionCall(mode int,opts...ParseOption)(any,error){switch mode{case 0:n,e:=Parse("a",opts...);return n,e;case 1:n,e:=ParseAST("a",opts...);return n,e;default:return nil,Recognize("a",opts...)}}
func actionZero(v any)bool{return v==nil || reflect.ValueOf(v).IsZero()}
func TestPublicActionErrorAccounting(t *testing.T){
 modes:=[]int{0,1};if recognize{modes=append(modes,2)}
 for _,mode:=range modes{
  n,e:=actionCall(mode);if wantError==""{if e!=nil || mode!=2 && actionZero(n){t.Fatal("predicate fallback baseline",n,e)}}else if e==nil || e.Error()!=wantError || !actionZero(n) || errors.Unwrap(e)!=nil{t.Fatal("baseline error contract",n,e)}
  _,large:=actionCall(mode,WithWorkLimits(WorkLimits{MaxSteps:1<<50}));if (large==nil)!=(e==nil) || large!=nil && large.Error()!=e.Error(){t.Fatal("generous controls changed error",large,e)}
  partial,limited:=actionCall(mode,WithWorkLimits(WorkLimits{MaxSteps:256}));if !errors.Is(limited,ErrWorkLimit) || !actionZero(partial){t.Errorf("long action message bypassed budget mode=%d result=%T error=%v",mode,partial,limited)}
  ctx,cancel:=context.WithCancel(context.Background());cancel();partial,cancelled:=actionCall(mode,WithContext(ctx));if !errors.Is(cancelled,context.Canceled) || !actionZero(partial){t.Fatal("cancelled result",partial,cancelled)}
  retry,retryErr:=actionCall(mode);if (retryErr==nil)!=(e==nil) || retryErr!=nil && retryErr.Error()!=e.Error() || !reflect.DeepEqual(retry,n){t.Fatal("action error retry changed output",retryErr)}
 }
}
func catchActionText(f func())(text string,err error){defer func(){if x:=recover();x!=nil{switch x:=x.(type){case evalError:text=x.msg;case fatal:err=x.err;default:panic(x)}}}();f();return}
func TestActionTextFragmentsAndFlatPrefix(t *testing.T){
 for _,parts:=range [][]string{{""},{"","",""},{"a","","λ\xff"},{strings.Repeat("n",8192)}}{
  want:=strings.Join(parts,"");steps:=uint64(len(want)+2*len(parts));p:=&parser{control:newWorkControl(nil,WorkLimits{MaxSteps:steps})}
  retained:=p.actionErrorText(parts...);if retained!=want || p.control.stepsUsed()!=steps || p.control.inputUsed()!=0{t.Fatal("fragment text/counters")}
  p.control=newWorkControl(nil,WorkLimits{MaxSteps:steps-1});var partial string
  _,e:=catchActionText(func(){partial=p.actionErrorText(parts...)});if !errors.Is(e,ErrWorkLimit) || partial!=""{t.Fatal("one-short message",e)}
  p.control=newWorkControl(nil,WorkLimits{MaxSteps:100});if p.actionErrorText("retry")!="retry" || retained!=want{t.Fatal("retained message changed")}
 }
 where:=strings.Repeat("rule",512);message:="division by zero";want:="action in "+where+": "+message
 p:=&parser{control:newWorkControl(nil,WorkLimits{MaxSteps:uint64(len(want)+8)})};e:=p.wrapEvalError(where,message)
 if e.Error()!=want || errors.Unwrap(e)!=nil || p.control.stepsUsed()!=uint64(len(want)+8){t.Fatal("flat wrapper contract",e)}
 p.control=newWorkControl(nil,WorkLimits{MaxSteps:uint64(len(want)+7)});_,e=catchActionText(func(){p.wrapEvalError(where,message)});if !errors.Is(e,ErrWorkLimit){t.Fatal("one-short wrapper",e)}
}
func TestActionHelperErrorsAndShortCircuit(t *testing.T){
 long:=strings.Repeat("Kind",2048);n:=&Node{kind:kindOf(long,"")};tn:=&tnode{typ:"tuple",kids:make([]any,2048)}
 for _,typed:=range []bool{false,true}{
  p:=&parser{};tp:=&tparser{parser:p};called:=0
  cases:=[]func(){
   func(){if typed{(&tctx{p:tp}).unary("-",tn)}else{(&actx{p:p}).unary("-",n)}},
   func(){if typed{(&tctx{p:tp}).binary("+",tn,1)}else{(&actx{p:p}).binary("+",n,1)}},
   func(){right:=func()any{called++;return true};if typed{(&tctx{p:tp}).and(tn,right)}else{(&actx{p:p}).and(n,right)}},
   func(){right:=func()any{called++;return false};if typed{(&tctx{p:tp}).or(tn,right)}else{(&actx{p:p}).or(n,right)}},
   func(){if typed{(&tctx{p:tp}).listItems(long,1)}else{(&actx{p:p}).listItems(long,1)}},
   func(){fn:=func(...any)any{return nil};if typed{(&tctx{p:tp}).asNode(long,fn)}else{(&actx{p:p}).asNode(long,fn)}},
  }
  for _,run:=range cases{
   p.control=nil;ordinary,e:=catchActionText(run);if e!=nil || ordinary==""{t.Fatal("ordinary helper error",e)}
   p.control=newWorkControl(nil,WorkLimits{MaxSteps:1<<50});active,e:=catchActionText(run);if e!=nil || active!=ordinary{t.Fatal("active helper error changed",e)}
   p.control=newWorkControl(nil,WorkLimits{MaxSteps:256});partial,e:=catchActionText(run);if partial!="" || !errors.Is(e,ErrWorkLimit) || p.control.inputUsed()!=0{t.Fatal("helper error unbounded",e)}
  }
  if called!=0{t.Fatal("invalid boolean operand evaluated RHS")}
  p.control=newWorkControl(nil,WorkLimits{MaxSteps:1<<50});called=0
  right:=func()any{called++;return true};var and,or any
  if typed{c:=&tctx{p:tp};and=c.and(false,right);or=c.or(true,right)}else{c:=&actx{p:p};and=c.and(false,right);or=c.or(true,right)}
  if and!=false || or!=true || called!=0{t.Fatal("valid short circuit changed")}
 }
}
`
