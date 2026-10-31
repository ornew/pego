package gogen_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

func TestGeneratedGoBudgetControls(t *testing.T) {
	t.Parallel()
	gentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated budget-control consumers")
	}
	g, err := parseTest(`type Word = { Text string }

rule main Word = t:("a"* "é") #atomic => Word{Text: text($t)}

rule recovering Word = t:(("a"* "é") #recover(skip="?")) #atomic => Word{Text: text($t)}

rule predicate Word = t:(!("a"* "é") "a"* "b") #atomic => Word{Text: text($t)}`)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gentest.GoMod("workfixture")), 0644); err != nil {
		t.Fatal(err)
	}
	for _, convert := range []bool{false, true} {
		pkg := fmt.Sprintf("budget_%v", convert)
		code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: pkg, Start: "main", Types: true, Recognize: true, ConvertTypes: convert})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, pkg)
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
		fixture := strings.ReplaceAll(generatedGoBudgetConsumer, "PACKAGE", pkg)
		for name, data := range map[string][]byte{"parser.go": code, "budget_test.go": []byte(fixture)} {
			if err := os.WriteFile(filepath.Join(path, name), data, 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated budget controls: %v\n%s", err, out)
	}
}

const generatedGoBudgetConsumer = `package PACKAGE

import zprt "github.com/ornew/zeroparse/runtime"
import("context";"errors";"fmt";"reflect";"strings";"sync";"testing";"time")

type invocation struct{name string; call func(string,...ParseOption)(any,error)}
func routes() []invocation {return []invocation{
 {"Node",func(s string,o ...ParseOption)(any,error){v,e:=Parse(s,o...);return v,e}},
 {"named Node",func(s string,o ...ParseOption)(any,error){v,e:=ParseRule("main",s,o...);return v,e}},
 {"AST",func(s string,o ...ParseOption)(any,error){v,e:=ParseAST(s,o...);return v,e}},
 {"named AST",func(s string,o ...ParseOption)(any,error){return ParseRuleAST("main",s,o...)}},
 {"recognize",func(s string,o ...ParseOption)(any,error){return nil,Recognize(s,o...)}},
}}
func isZero(v any) bool{return v==nil || reflect.ValueOf(v).IsZero()}
func TestControls(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 expired,stop:=context.WithDeadline(context.Background(),time.Unix(0,0));defer stop()
 input:=strings.Repeat("a",300)+"é"
 for _,r:=range routes(){for _,unit:=range []PositionUnit{CodePoints,Bytes}{t.Run(r.name+string(rune('0'+unit)),func(t *testing.T){
  calls:=0;option:=func(o *zprt.X_parseOptions){calls++;o.X_unit=unit}
  v,err:=r.call(input,option)
  if err!=nil || calls!=1 {t.Fatalf("baseline callbacks=%d err=%v",calls,err)}
  before:=v
  for _,c:=range []context.Context{ctx,expired}{
   v,err=r.call(input,option,WithContext(c));if !isZero(v) || !errors.Is(err,c.Err()){t.Fatalf("pre-cancel value=%v err=%v",v,err)}
  }
  for range 1{
   limit:=func(n uint64) ParseOption {return WithLimits(Limits{Budget:n})}
   lo,hi:=uint64(1),uint64(1<<20)
   if _,err=r.call(input,WithUnit(unit),limit(hi));err!=nil{t.Fatal(err)}
   for lo<hi {mid:=lo+(hi-lo)/2;_,e:=r.call(input,WithUnit(unit),limit(mid));if e==nil{hi=mid}else if errors.Is(e,ErrLimit){lo=mid+1}else{t.Fatal(e)}}
   if lo<=1 {t.Fatal("spend not charged")}
   v,err=r.call(input,WithUnit(unit),limit(lo));if err!=nil{t.Fatalf("exact ceiling=%d: %v",lo,err)}
   v,err=r.call(input,WithUnit(unit),limit(lo-1));var exhausted *LimitError
   if !isZero(v) || !errors.As(err,&exhausted) || !errors.Is(err,ErrLimit){t.Fatalf("one short value=%v err=%v",v,err)}
   if exhausted.Limit!=lo-1 || exhausted.Requested==0 || exhausted.Used>exhausted.Limit || exhausted.Requested<=exhausted.Limit-exhausted.Used {t.Fatalf("reservation metadata: %+v",exhausted)}
   if exhausted.Kind!=LimitBudget {t.Fatalf("wrong counter: %+v",exhausted)}
  }
  if _,err=r.call(input,option,WithLimits(Limits{}),WithContext(nil));err!=nil{t.Fatal("retry",err)}
  _=before
 })}}
}
// MaxInput bounds the input in the position unit for every entry point: zero and Unlimited set no
// bound, an input of exactly MaxInput units parses, and a longer one fails with the LimitInput kind
// and its length, before any parsing.
func TestMaxInput(t *testing.T){
 input:=strings.Repeat("a",300)+"é"
 for _,r:=range routes(){for _,unit:=range []PositionUnit{CodePoints,Bytes}{t.Run(r.name+string(rune('0'+unit)),func(t *testing.T){
  n:=uint64(301);if unit==Bytes{n=302}
  for _,max:=range []uint64{0,Unlimited,n,n+1}{
   if _,err:=r.call(input,WithUnit(unit),WithLimits(Limits{MaxInput:max}));err!=nil{t.Fatalf("MaxInput %d: %v",max,err)}
  }
  v,err:=r.call(input,WithUnit(unit),WithLimits(Limits{MaxInput:n-1}));var le *LimitError
  if !isZero(v) || !errors.As(err,&le) || !errors.Is(err,ErrLimit){t.Fatalf("one over: value=%v err=%v",v,err)}
  if le.Kind!=LimitInput || le.Limit!=n-1 || le.Requested!=n || le.Used!=0 {t.Fatalf("metadata: %+v",le)}
  if want:=fmt.Sprintf("parse input limit exceeded: limit %d, length %d",n-1,n);err.Error()!=want{t.Fatalf("text %q, want %q",err.Error(),want)}
  ctx,cancel:=context.WithCancel(context.Background());cancel()
  if _,err=r.call(input,WithUnit(unit),WithContext(ctx),WithLimits(Limits{MaxInput:1}));!errors.Is(err,ErrLimit){t.Fatalf("checked before the context: %v",err)}
 })}}
}
func TestValidationAndOptions(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 for _,r:=range routes(){
  _,err:=r.call("?",WithContext(ctx),WithMaxDepth(-1));if err==nil || !strings.Contains(err.Error(),"max depth"){t.Fatalf("configuration precedence: %v",err)}
  sentinel:=new(int)
  func(){defer func(){if got:=recover();got!=sentinel{t.Fatalf("option panic %v",got)}}();r.call("?",func(*zprt.X_parseOptions){panic(sentinel)})}()
 }
 for _,call:=range []func()error{func()error{_,e:=ParseRule("missing","?",WithContext(ctx));return e},func()error{_,e:=ParseRuleAST("missing","?",WithContext(ctx));return e}} {
  err:=call();if err==nil || errors.Is(err,context.Canceled) {t.Fatalf("name validation: %v",err)}
 }
 for _,r:=range routes(){
  calls:=0
  option:=func(o *zprt.X_parseOptions){calls++;o.X_limits=Limits{Budget:100000}}
  _,err:=r.call("é",WithContext(ctx),WithContext(context.Background()),WithLimits(Limits{Budget:1}),option)
  if err!=nil || calls!=1 {t.Fatalf("last effective options: %v, callbacks %d",err,calls)}
 }
}
type panicContext struct{context.Context;value any;calls,after int}
func(c *panicContext)Done() <-chan struct{}{return make(chan struct{})}
func(c *panicContext)Err()error{c.calls++;if c.calls>=c.after{panic(c.value)};return nil}
func TestPanicAndReuse(t *testing.T){
 for _,r:=range routes(){for _,after:=range []int{1,3,6}{
  marker:=new(int);ctx:=&panicContext{Context:context.Background(),value:marker,after:after}
  func(){defer func(){if got:=recover();got!=marker{t.Fatalf("%s context panic after %d: %v",r.name,after,got)}}();r.call(strings.Repeat("a",10000)+"é",WithContext(ctx))}()
  before:=ctx.calls
  if _,err:=r.call("é");err!=nil{t.Fatal(err)}
  if ctx.calls!=before{t.Fatal("context retained across pooled retry")}
 }}
}
func TestFatalNotRecovered(t *testing.T){
 input:=strings.Repeat("a",10000)+"b"
 for _,name:=range []string{"recovering","predicate"}{for _,ast:=range []bool{false,true}{
  var v any;var err error
  if ast{v,err=ParseRuleAST(name,input,WithUnit(Bytes),WithLimits(Limits{Budget:40}))}else{v,err=ParseRule(name,input,WithUnit(Bytes),WithLimits(Limits{Budget:40}))}
  if !isZero(v)||!errors.Is(err,ErrLimit){t.Fatalf("%s ast=%v swallowed interruption value=%v err=%v",name,ast,v,err)}
 }}
}
func TestStringAndMalformedTextBudget(t *testing.T){
 text:=strings.Repeat("a",300)+"é"
 p:=&zprt.Parser{Control:zprt.X_newBudgetControl(context.Background(),Limits{Budget:100000})}
 c:=&zprt.ActCtx{P:p}
 for _,op:=range []string{"==","!=","<","<=",">",">=","+"}{
  got:=c.Binary(op,text,text)
  want:=zprt.X_rtBinary(op,text,text)
  if !reflect.DeepEqual(got,want){t.Fatalf("string %s: %v want %v",op,got,want)}
 }
 if p.Control.X_budgetUsed()==0{t.Fatal("string spend not charged")}
 for _,invoke:=range []func(*zprt.Parser){
  func(p *zprt.Parser){(&zprt.ActCtx{P:p}).Binary("==",text,text)},
  func(p *zprt.Parser){(&zprt.ActCtx{P:p}).Binary("+",text,text)},
  func(p *zprt.Parser){p.X_unitLen(text)},
 }{
  p:=&zprt.Parser{Control:zprt.X_newBudgetControl(context.Background(),Limits{Budget:127})}
  func(){defer func(){f,ok:=recover().(zprt.X_fatal);if !ok || !errors.Is(f.X_err,ErrLimit){t.Fatal("string spend not bounded",f)}}();invoke(p)}()
 }
 // Malformed-source normalization must account for both sizing and copying.
 runes:=[]rune(strings.Repeat("�",300))
 p=&zprt.Parser{In:runes,Control:zprt.X_newBudgetControl(context.Background(),Limits{Budget: 1200})}
 if got:=p.Text(0,len(runes));got!=string(runes){t.Fatal("normalized text mismatch")}
 if p.Control.X_budgetUsed()!=1200{t.Fatalf("normalized counters: %+v",p.Control)}
 p=&zprt.Parser{In:runes,Control:zprt.X_newBudgetControl(context.Background(),Limits{Budget:1199})}
 func(){defer func(){f,ok:=recover().(zprt.X_fatal);if !ok || !errors.Is(f.X_err,ErrLimit){t.Fatal("normalized text unbounded",f)}}();p.Text(0,len(runes))}()
}

func TestFailureDiagnosticsBounded(t *testing.T){
 input:="?"+strings.Repeat("\n",6000)
 for _,r:=range routes(){for _,steps:=range []bool{false,true}{
  limits:=Limits{Budget:128};if steps{limits=Limits{Budget:128}}
  v,err:=r.call(input,WithUnit(Bytes),WithLimits(limits))
  if !isZero(v) || !errors.Is(err,ErrLimit){t.Errorf("%s steps=%v early-failure diagnosis escaped budget: %v %v",r.name,steps,v,err)}
  if _,err=r.call("é");err!=nil{t.Fatal("retry after diagnostic interruption",err)}
 }}
}
func TestLineDiagnosticReservations(t *testing.T){
 for _,unit:=range []PositionUnit{Bytes,CodePoints}{
  input:=strings.Repeat("é\n",300)
  makeParser:=func(limit uint64)*zprt.Parser{
   p:=&zprt.Parser{X_unit:unit,X_bs:input,In:[]rune(input)};p.X_n=len(p.In);if unit==Bytes{p.X_n=len(input)}
   p.Control=zprt.X_newBudgetControl(context.Background(),Limits{Budget:limit})
   return p
  }
  p:=makeParser(100000);e:=p.MakeError(p.X_n-1,nil)
  if e.Line!=300 || e.Col!=map[PositionUnit]int{Bytes:3,CodePoints:2}[unit]{t.Fatal("line coordinate changed",e)}
  used:=p.Control.X_budgetUsed()
  if used<uint64(p.X_n){t.Errorf("diagnostic input uncharged: %d for %d",used,p.X_n)}
  p=makeParser(used)
  if e:=p.MakeError(p.X_n-1,nil);e.Line!=300{t.Fatal("exact diagnostic ceiling",e)}
  p=makeParser(used-1)
  func(){defer func(){f,ok:=recover().(zprt.X_fatal);var exhausted *LimitError;if !ok || !errors.As(f.X_err,&exhausted) || exhausted.Kind!=LimitBudget{t.Error("one-short diagnostic unbounded",f)}}();p.MakeError(p.X_n-1,nil)}()
 }
}

func TestConcurrentLimits(t *testing.T){
 var wg sync.WaitGroup
 for worker:=0;worker<8;worker++{wg.Go(func(){for repeat:=0;repeat<8;repeat++{for _,r:=range routes(){
  if _,err:=r.call("é",WithLimits(Limits{Budget:10000}));err!=nil{t.Error(err)}
  if _,err:=r.call(strings.Repeat("a",1000)+"é",WithLimits(Limits{Budget:3}));!errors.Is(err,ErrLimit){t.Errorf("independent limit: %v",err)}
 }}})};wg.Wait()
}
`

func TestGeneratedGoConversionBudget(t *testing.T) {
	t.Parallel()
	gentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated conversion consumers")
	}
	g, err := parseTest(`type Leaf = { Text string }

type Branch = {
    Leaves []Leaf
}

rule main = xs:(x:leaf)+ => Branch{Leaves: map($xs, (x) => x.$x)}

rule leaf Leaf = t:("é") #atomic => Leaf{Text: text($t)}`)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gentest.GoMod("gentest")), 0644); err != nil {
		t.Fatal(err)
	}
	pkg := "conversion"
	gentest.GeneratePackage(t, dir, pkg, g, gen.GenOptions{Start: "main", Types: true, ConvertTypes: true})
	fixture := strings.ReplaceAll(generatedGoConversionConsumer, "PACKAGE", pkg)
	if err := os.WriteFile(filepath.Join(dir, pkg, "conversion_test.go"), []byte(fixture), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated conversion spend: %v\n%s", err, out)
	}
}

const generatedGoConversionConsumer = `package PACKAGE

import zprt "github.com/ornew/zeroparse/runtime"
import("context";"errors";"reflect";"strings";"testing";"gentest/PACKAGE/types")
func TestConversionVisits(t *testing.T){
 n,err:=Parse(strings.Repeat("é",100));if err!=nil{t.Fatal(err)}
 c:=zprt.X_newBudgetControl(context.Background(),Limits{Budget:100000})
 a:=astConv{control:c};v:=a.toTypeBranch(n)
 if v==nil || len(v.Leaves)!=100{t.Fatal("conversion failed")}
 // Branch dispatch, field scan, list dispatch, each list edge, and for each leaf its dispatch
 // and the scan of its Text field.
 if c.X_budgetUsed()!=303{t.Fatalf("conversion spend: %d",c.X_budgetUsed())}
 for _,ceiling:=range []uint64{1,2,3,4,302}{
  c:=zprt.X_newBudgetControl(context.Background(),Limits{Budget:ceiling})
  func(){defer func(){f,ok:=recover().(zprt.X_fatal);var e *LimitError;if !ok || !errors.As(f.X_err,&e) || e.Kind!=LimitBudget || e.Used!=ceiling || e.Requested!=1{t.Fatalf("conversion ceiling %d interruption: %+v",ceiling,f)}}();(&astConv{control:c}).toTypeBranch(n)}()
 }
}
func TestParseConversionSharesCounter(t *testing.T){
 input:=strings.Repeat("é",100)
 before:=uint64(0)
 conv:=func(n *Node,c *zprt.BudgetControl,parseErr error)*types.Branch{before=c.X_budgetUsed();v:=(&astConv{control:c}).toTypeBranch(n);if c.X_budgetUsed()!=before+303{t.Fatal("conversion counter changed independently")};return v}
 o:=zprt.X_parseOptions{X_expectations: true, X_maxDepth:zpGrammar.X_maxDepth,X_limits:Limits{Budget:100000}}
 retained,err:=zprt.RunParseConverted(rules[0],input,o,conv)
 if err!=nil || retained==nil{t.Fatal(err)}
 snapshot:=*retained.Leaves[0]
 total:=before+303
 for _,limit:=range []uint64{before+1,total-1,total}{
  o.X_limits.Budget=limit
  got,err:=zprt.RunParseConverted(rules[0],input,o,func(n *Node,c *zprt.BudgetControl,parseErr error)*types.Branch{return (&astConv{control:c}).toTypeBranch(n)})
  if limit==total {if err!=nil || len(got.Leaves)!=100{t.Fatal("exact conversion ceiling",err)}}else if got!=nil || !errors.Is(err,ErrLimit){t.Fatalf("partial conversion leaked at %d: %v %v",limit,got,err)}
 }
 if !reflect.DeepEqual(*retained.Leaves[0],snapshot){t.Fatal("earlier result was overwritten")}
 // Cancellation raised inside conversion is surfaced at the same outer boundary.
 ctx,cancel:=context.WithCancel(context.Background());o.SetContext(ctx);o.X_limits=Limits{}
 v,err:=zprt.RunParseConverted(rules[0],input,o,func(n *Node,c *zprt.BudgetControl,parseErr error)*types.Branch{cancel();c.Check();return (&astConv{control:c}).toTypeBranch(n)})
 if v!=nil || !errors.Is(err,context.Canceled){t.Fatalf("conversion cancellation: %v %v",v,err)}
 if _,err:=ParseAST(input);err!=nil{t.Fatal("retry",err)}
}
`

func TestGeneratedGoBudgetControlNameCollisions(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds generated public-name collision consumers")
	}
	g, err := parseTest(`type Limits = { Text string }

type Limits_ = { Text string }

type LimitError = {
    Value Limits
}

type LimitKind ~ Limits

type WithContext ~ Limits

type WithLimits ~ Limits

type ErrLimit ~ Limits

type LimitBudget ~ Limits

type LimitInput ~ Limits

rule main Limits = t:("é") #atomic => Limits{Text: text($t)}`)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gentest.GoMod("budgetcollision")), 0644); err != nil {
		t.Fatal(err)
	}
	for _, convert := range []bool{false, true} {
		pkg := fmt.Sprintf("collision_%v", convert)
		opts := gen.GenOptions{Package: pkg, Module: "budgetcollision/" + pkg, Runtime: gen.RuntimeImport, Start: "main", Types: true, ConvertTypes: convert, BudgetControls: true}
		files, err := gentest.Generate(t, g, opts)
		if err != nil {
			t.Fatal(err)
		}
		again, err := gentest.Generate(t, g, opts)
		if err != nil || !slices.EqualFunc(files, again, func(a, b gen.GeneratedFile) bool { return a.Path == b.Path && string(a.Content) == string(b.Content) }) {
			t.Fatal("unstable generation")
		}
		fixture := strings.ReplaceAll(generatedGoBudgetCollisionConsumer, "PACKAGE", pkg)
		files = append(files, gen.GeneratedFile{Path: "consumer_test.go", Content: []byte(fixture)})
		for _, f := range files {
			path := filepath.Join(dir, pkg, filepath.FromSlash(f.Path))
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, f.Content, 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("budget-control name collision: %v\n%s", err, out)
	}
}

const generatedGoBudgetCollisionConsumer = `package PACKAGE_test
import("context";"errors";"testing";p "budgetcollision/PACKAGE";g "budgetcollision/PACKAGE/types")
func TestPublicNames(t *testing.T){
 var parse func(string,...p.ParseOption)(*g.Limits,error)=p.ParseAST
 v,err:=parse("é",p.WithContext(context.Background()),p.WithLimits(p.Limits{Budget:1000}),p.WithUnit(p.Bytes))
 if err!=nil || v.Text!="é" || v.End!=2{t.Fatal(v,err)}
 var second *g.Limits_=&g.Limits_{}
 var box *g.LimitError=&g.LimitError{Value:v}
 var kind g.LimitKind=v
 var ctx g.WithContext=v
 var limits g.WithLimits=v
 var sentinel g.ErrLimit=v
 var spend g.LimitBudget=v
 var length g.LimitInput=v
 _,_,_,_,_,_,_,_=second,box,kind,ctx,limits,sentinel,spend,length
 _,err=parse("é",p.WithLimits(p.Limits{Budget:1}))
 var exhausted *p.LimitError
 if !errors.Is(err,p.ErrLimit) || !errors.As(err,&exhausted){t.Fatal(err)}
 var counter p.LimitKind=exhausted.Kind
 if counter!=p.LimitBudget{t.Fatal(counter)}
}
`

func TestGeneratedGoNilBudgetPublication(t *testing.T) {
	t.Parallel()
	gentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds typed nil publication consumer")
	}
	g, err := parseTest(`type Word = { Text string }

type Maybe ~ *Word

rule main Maybe = x:letter? => $x

rule letter Word = t:("é") #atomic => Word{Text: text($t)}`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "nilpublication", Start: "main", Types: true})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range map[string][]byte{"go.mod": []byte(gentest.GoMod("nilpublication")), "parser.go": code, "nil_test.go": []byte(generatedGoNilPublicationConsumer)} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("typed nil publication: %v\n%s", err, out)
	}
}

const generatedGoNilPublicationConsumer = `package nilpublication

import zprt "github.com/ornew/zeroparse/runtime"
import("context";"errors";"reflect";"testing")
func TestNilPublication(t *testing.T){
 original:=trules[0].Entry;defer func(){trules[0].Entry=original}()
 for _,named:=range []bool{false,true}{for _,ok:=range []bool{false,true}{
  ctx,cancel:=context.WithCancel(context.Background())
  // Cancel exactly at the last execution operation, with no guessed poll count.
  trules[0].Entry=func(p *zprt.TParser)(any,bool){cancel();return nil,ok}
  var v any;var err error
  if named{v,err=ParseRuleAST("main","",WithContext(ctx))}else{v,err=ParseAST("",WithContext(ctx))}
  if (v!=nil && !reflect.ValueOf(v).IsZero()) || !errors.Is(err,context.Canceled){t.Errorf("named=%v successful=%v value=%v err=%v",named,ok,v,err)}
 }}
 for _,named:=range []bool{false,true}{
  ctx,cancel:=context.WithCancel(context.Background())
  established:=&LimitError{Kind:LimitBudget,Limit:1,Used:1,Requested:1}
  trules[0].Entry=func(p *zprt.TParser)(any,bool){cancel();panic(zprt.X_fatal{X_err:established})}
  var err error
  if named{_,err=ParseRuleAST("main","",WithContext(ctx))}else{_,err=ParseAST("",WithContext(ctx))}
  if err!=established{t.Fatalf("established fatal was masked: %v",err)}
 }
 trules[0].Entry=original
 if v,err:=ParseAST("");v!=nil || err!=nil{t.Fatalf("nil success retry: %v %v",v,err)}
 if v,err:=ParseAST("é");v==nil || v.Text!="é" || err!=nil{t.Fatalf("value retry: %v %v",v,err)}
}

type clonePhaseContext struct{context.Context;control *zprt.BudgetControl;marker any;calls int}
func(c *clonePhaseContext)Done()<-chan struct{}{return make(chan struct{})}
func(c *clonePhaseContext)Err()error{
 c.calls++
 if c.control!=nil && c.control.X_budgetUsed()>=256 {if c.marker!=nil{panic(c.marker)};return context.Canceled}
 return nil
}
func TestRecoveredNilClonePublication(t *testing.T){
 original:=trules[0].Entry;defer func(){trules[0].Entry=original}()
 recovered:=make([]*SyntaxError,300)
 for i:=range recovered{recovered[i]=&SyntaxError{Line:i+1,Col:1}}
 for _,named:=range []bool{false,true}{for _,panicMode:=range []bool{false,true}{
  ctx:=&clonePhaseContext{Context:context.Background()};if panicMode{ctx.marker=new(int)}
  trules[0].Entry=func(p *zprt.TParser)(any,bool){ctx.control=p.Control;p.Recovered=recovered;return nil,true}
  call:=func()(any,error){if named{return ParseRuleAST("main","",WithContext(ctx))};return ParseAST("",WithContext(ctx))}
  if panicMode{func(){defer func(){if got:=recover();got!=ctx.marker{t.Errorf("clone panic missed or changed: %T",got)}}();call()}()}else{
   v,err:=call()
   if (v!=nil && !reflect.ValueOf(v).IsZero()) || !errors.Is(err,context.Canceled){t.Errorf("nil clone cancellation missed named=%v: value=%T error=%T",named,v,err)}
  }
  before:=ctx.calls;trules[0].Entry=original
  if v,err:=ParseAST("");v!=nil || err!=nil{t.Fatal("nil retry after clone interruption",err)}
  if v,err:=ParseAST("é");v==nil || err!=nil{t.Fatal("value retry after clone interruption",err)}
  if ctx.calls!=before{t.Fatal("clone context retained by parser")}
 }}
}

`

func TestGeneratedGoLegacyNilControls(t *testing.T) {
	t.Parallel()
	gentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds configured legacy nil consumers")
	}
	g, err := parseTest(`type Word = { Text string }

rule main _ = x:leaf? => $x

rule leaf Word = t:("é") #atomic => Word{Text: text($t)}`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "legacynil", Start: "main", Types: true})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range map[string][]byte{"go.mod": []byte(gentest.GoMod("legacynil")), "parser.go": code, "nil_test.go": []byte(generatedGoLegacyNilConsumer)} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("legacy nil controls: %v\n%s", err, out)
	}
}

const generatedGoLegacyNilConsumer = `package legacynil
import("context";"errors";"testing")
type ownedErrorContext struct{context.Context;err error}
func(c *ownedErrorContext)Done()<-chan struct{}{return make(chan struct{})}
func(c *ownedErrorContext)Err()error{return c.err}
func TestLegacyAndInterruptedNil(t *testing.T){
 for _,input:=range []string{"","?"}{
  got,err:=ParseAST(input)
  if got!=nil{t.Fatalf("absent value is not nil: %T %v",got,got)}
  if (err==nil)!=(input==""){t.Fatal("legacy syntax failure",err)}
  got,err=ParseRuleAST("main",input)
  if got!=nil || (err==nil)!=(input==""){t.Fatalf("named zero result changed: %T %v %v",got,got,err)}
 }
 got,err:=ParseAST("",WithMaxDepth(-1))
 if err==nil || got!=nil{t.Fatalf("ordinary option error mapping: %T %v %v",got,got,err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 owned:=errors.New("caller-owned context interruption")
 for _,r:=range []func(string,...ParseOption)(any,error){ParseAST,func(s string,o ...ParseOption)(any,error){return ParseRuleAST("main",s,o...)}}{
  for _,row:=range []struct{option ParseOption;want error}{
   {WithContext(ctx),context.Canceled},
   {WithContext(&ownedErrorContext{Context:context.Background(),err:owned}),owned},
   {WithLimits(Limits{Budget:1}),ErrLimit},
  }{
   got,err:=r("é",row.option)
   if got!=nil || !errors.Is(err,row.want){t.Fatalf("interruption converted nil: %T %v %v",got,got,err)}
  }
 }
 if got,err:=ParseAST("é");err!=nil || got==nil{t.Fatal("retry",got,err)}
}
`

func TestGeneratedGoConfiguredNilOrdinaryErrors(t *testing.T) {
	t.Parallel()
	gentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds configured nil error consumers")
	}
	for _, tc := range []struct{ name, source, input, option string }{
		{"action", `type A = { N int }
rule main _ = "x" => A{N: 1 / 0}`, "x", "WithMaxDepth(100000)"},
		{"depth", `rule main _ = recurse
rule recurse = "a" recurse?`, "aa", "WithMaxDepth(1)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, err := parseTest(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "ordinarynil", Start: "main", Types: true})
			if err != nil {
				t.Fatal(err)
			}
			consumer := strings.NewReplacer("INPUT", fmt.Sprintf("%q", tc.input), "OPTION", tc.option).Replace(generatedGoOrdinaryNilErrorConsumer)
			dir := t.TempDir()
			for name, data := range map[string][]byte{"go.mod": []byte(gentest.GoMod("ordinarynil")), "parser.go": code, "nil_test.go": []byte(consumer)} {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("go", "test", "-count=1", "./...")
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("ordinary nil errors: %v\n%s", err, out)
			}
		})
	}
}

const generatedGoOrdinaryNilErrorConsumer = `package ordinarynil
import("context";"testing")
func TestOrdinaryNilMapping(t *testing.T){
 for _,unit:=range []PositionUnit{CodePoints,Bytes}{
  for _,extra:=range [][]ParseOption{nil,{WithContext(context.Background())},{WithLimits(Limits{Budget: 1000000})}}{
   opts:=append([]ParseOption{WithUnit(unit),OPTION},extra...)
   got,err:=ParseAST(INPUT,opts...)
   if err==nil || got!=nil{t.Fatalf("ordinary error returned a value: %T %v %v",got,got,err)}
  }
 }
}
`
