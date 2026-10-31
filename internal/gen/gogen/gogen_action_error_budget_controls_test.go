package gogen_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

func TestGeneratedGoActionErrorBudget(t *testing.T) {
	t.Parallel()
	gentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated action-error consumers")
	}
	rule, variable := strings.Repeat("r", 8192), strings.Repeat("u", 8192)
	cases := []struct {
		name, start, grammar, want string
		recognize                  bool
		// nameFree marks the grammar whose long variable name costs nothing: a variable is a slot.
		nameFree bool
	}{
		{"prefix", rule, `type A = { N int }
 rule ` + rule + ` A = a:"a" #cut => A{N:1 / (len($a)-1)}`, "action in " + rule + ": division by zero", false, false},
		{"predicate", "main", `type Word = { Text string }
 var ` + variable + ` bool
 rule main Word = (({` + variable + `} x:leaf) / x:leaf) #cut => $x
 rule leaf Word = t:("a") #atomic => Word{Text: text($t)}`, "", true, true},
		{"captured", "main", `type A = { N int }
 rule main A = x:` + rule + ` {$x != nil} $$ #cut => $x
 rule ` + rule + ` A = a:"a" #cut => A{N:1 / (len($a)-1)}`, "action in " + rule + ": division by zero", true, false},
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gentest.GoMod("actionerrorfixture")), 0644); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		g, err := parseTest(c.grammar)
		if err != nil {
			t.Fatal(err)
		}
		for _, convert := range []bool{false, true} {
			pkg := fmt.Sprintf("action_%s_%v", c.name, convert)
			code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: pkg, Start: c.start, Types: true, Recognize: true, ConvertTypes: convert})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, pkg)
			if err := os.Mkdir(path, 0755); err != nil {
				t.Fatal(err)
			}
			consumer := strings.NewReplacer("PACKAGE", pkg, "WANT_ERROR", fmt.Sprintf("%q", c.want), "CHECK_RECOGNIZE", fmt.Sprint(c.recognize), "NAME_FREE", fmt.Sprint(c.nameFree)).Replace(generatedGoActionErrorConsumer)
			for name, data := range map[string][]byte{"parser.go": code, "action_test.go": []byte(consumer)} {
				if err := os.WriteFile(filepath.Join(path, name), data, 0644); err != nil {
					t.Fatal(err)
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

import zprt "github.com/ornew/zeroparse/runtime"
import("context";"errors";"reflect";"strings";"testing")
const wantError=WANT_ERROR
const recognize=CHECK_RECOGNIZE
const nameFree=NAME_FREE
func actionCall(mode int,opts...ParseOption)(any,error){switch mode{case 0:n,e:=Parse("a",opts...);return n,e;case 1:n,e:=ParseAST("a",opts...);return n,e;default:return nil,Recognize("a",opts...)}}
func actionZero(v any)bool{return v==nil || reflect.ValueOf(v).IsZero()}
func TestPublicActionErrorAccounting(t *testing.T){
 modes:=[]int{0,1};if recognize{modes=append(modes,2)}
 for _,mode:=range modes{
  n,e:=actionCall(mode);if wantError==""{if e!=nil || mode!=2 && actionZero(n){t.Fatal("predicate fallback baseline",n,e)}}else if e==nil || e.Error()!=wantError || !actionZero(n) || errors.Unwrap(e)!=nil{t.Fatal("baseline error contract",n,e)}
  _,large:=actionCall(mode,WithLimits(Limits{Budget:1<<50}));if (large==nil)!=(e==nil) || large!=nil && large.Error()!=e.Error(){t.Fatal("generous controls changed error",large,e)}
  partial,limited:=actionCall(mode,WithLimits(Limits{Budget:256}))
  if nameFree{if limited!=nil || mode!=2 && actionZero(partial){t.Errorf("a long variable name cost budget mode=%d result=%T error=%v",mode,partial,limited)}}else if !errors.Is(limited,ErrLimit) || !actionZero(partial){t.Errorf("long action message bypassed budget mode=%d result=%T error=%v",mode,partial,limited)}
  ctx,cancel:=context.WithCancel(context.Background());cancel();partial,cancelled:=actionCall(mode,WithContext(ctx));if !errors.Is(cancelled,context.Canceled) || !actionZero(partial){t.Fatal("cancelled result",partial,cancelled)}
  retry,retryErr:=actionCall(mode);if (retryErr==nil)!=(e==nil) || retryErr!=nil && retryErr.Error()!=e.Error() || !reflect.DeepEqual(retry,n){t.Fatal("action error retry changed output",retryErr)}
 }
}
func catchActionText(f func())(text string,err error){defer func(){if x:=recover();x!=nil{switch x:=x.(type){case zprt.X_evalError:text=x.X_msg;case zprt.X_fatal:err=x.X_err;default:panic(x)}}}();f();return}
func TestActionTextFragmentsAndFlatPrefix(t *testing.T){
 for _,parts:=range [][]string{{""},{"","",""},{"a","","λ\xff"},{strings.Repeat("n",8192)}}{
  want:=strings.Join(parts,"");steps:=uint64(len(want)+2*len(parts));p:=&zprt.Parser{Control:zprt.X_newBudgetControl(nil,Limits{Budget:steps})}
  retained:=p.X_actionErrorText(parts...);if retained!=want || p.Control.X_budgetUsed()!=steps{t.Fatal("fragment text/counters")}
  p.Control=zprt.X_newBudgetControl(nil,Limits{Budget:steps-1});var partial string
  _,e:=catchActionText(func(){partial=p.X_actionErrorText(parts...)});if !errors.Is(e,ErrLimit) || partial!=""{t.Fatal("one-short message",e)}
  p.Control=zprt.X_newBudgetControl(nil,Limits{Budget:100});if p.X_actionErrorText("retry")!="retry" || retained!=want{t.Fatal("retained message changed")}
 }
 where:=strings.Repeat("rule",512);message:="division by zero";want:="action in "+where+": "+message
 p:=&zprt.Parser{Control:zprt.X_newBudgetControl(nil,Limits{Budget:uint64(len(want)+8)})};e:=p.X_wrapEvalError(where,message)
 if e.Error()!=want || errors.Unwrap(e)!=nil || p.Control.X_budgetUsed()!=uint64(len(want)+8){t.Fatal("flat wrapper contract",e)}
 p.Control=zprt.X_newBudgetControl(nil,Limits{Budget:uint64(len(want)+7)});_,e=catchActionText(func(){p.X_wrapEvalError(where,message)});if !errors.Is(e,ErrLimit){t.Fatal("one-short wrapper",e)}
}
func TestActionHelperErrorsAndShortCircuit(t *testing.T){
 long:=strings.Repeat("Kind",2048);n:=&Node{RawKind:zprt.KindOf(long,"")};tn:=&zprt.TSeq{Typ:"tuple",Kids:make([]any,2048)}
 for _,typed:=range []bool{false,true}{
  p:=&zprt.Parser{};tp:=&zprt.TParser{Parser:p};called:=0
  cases:=[]func(){
   func(){if typed{(&zprt.TActCtx{P:tp}).Unary("-",tn)}else{(&zprt.ActCtx{P:p}).Unary("-",n)}},
   func(){if typed{(&zprt.TActCtx{P:tp}).Binary("+",tn,1)}else{(&zprt.ActCtx{P:p}).Binary("+",n,1)}},
   func(){right:=func()any{called++;return true};if typed{(&zprt.TActCtx{P:tp}).And(tn,right)}else{(&zprt.ActCtx{P:p}).And(n,right)}},
   func(){right:=func()any{called++;return false};if typed{(&zprt.TActCtx{P:tp}).Or(tn,right)}else{(&zprt.ActCtx{P:p}).Or(n,right)}},
   func(){if typed{(&zprt.TActCtx{P:tp}).ListItems(long,1)}else{(&zprt.ActCtx{P:p}).ListItems(long,1)}},
   func(){fn:=func(...any)any{return nil};if typed{(&zprt.TActCtx{P:tp}).X_asNode(long,fn)}else{(&zprt.ActCtx{P:p}).X_asNode(long,fn)}},
  }
  for _,run:=range cases{
   p.Control=nil;ordinary,e:=catchActionText(run);if e!=nil || ordinary==""{t.Fatal("ordinary helper error",e)}
   p.Control=zprt.X_newBudgetControl(nil,Limits{Budget:1<<50});active,e:=catchActionText(run);if e!=nil || active!=ordinary{t.Fatal("active helper error changed",e)}
   p.Control=zprt.X_newBudgetControl(nil,Limits{Budget:256});partial,e:=catchActionText(run);if partial!="" || !errors.Is(e,ErrLimit){t.Fatal("helper error unbounded",e)}
  }
  if called!=0{t.Fatal("invalid boolean operand evaluated RHS")}
  p.Control=zprt.X_newBudgetControl(nil,Limits{Budget:1<<50});called=0
  right:=func()any{called++;return true};var and,or any
  if typed{c:=&zprt.TActCtx{P:tp};and=c.And(false,right);or=c.Or(true,right)}else{c:=&zprt.ActCtx{P:p};and=c.And(false,right);or=c.Or(true,right)}
  if and!=false || or!=true || called!=0{t.Fatal("valid short circuit changed")}
 }
}
`
