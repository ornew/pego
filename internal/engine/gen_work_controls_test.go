package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestGeneratedGoWorkControls(t *testing.T) {
	needsWorkControls(t)
	if testing.Short() {
		t.Skip("builds generated work-control consumers")
	}
	g, err := parseTest(`type Word = { Text string }

rule main Word = t:("a"* "é") #atomic => Word{Text: text($t)}

rule recovering Word = t:(("a"* "é") #recover(skip="?")) #atomic => Word{Text: text($t)}

rule predicate Word = t:(!("a"* "é") "a"* "b") #atomic => Word{Text: text($t)}`)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(testGoMod("workfixture")), 0644); err != nil {
		t.Fatal(err)
	}
	for _, convert := range []bool{false, true} {
		for _, general := range []bool{false, true} {
			pkg := fmt.Sprintf("work_%v_%v", convert, general)
			code, err := generateCode(g, GenOptions{Package: pkg, Start: "main", Types: true, Recognize: true, convertTypes: convert, disableOrdinaryWorkSpecialization: general})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, pkg)
			if err := os.Mkdir(path, 0755); err != nil {
				t.Fatal(err)
			}
			fixture := strings.ReplaceAll(generatedGoWorkConsumer, "PACKAGE", pkg)
			for name, data := range map[string][]byte{"parser.go": code, "work_test.go": []byte(fixture)} {
				if err := os.WriteFile(filepath.Join(path, name), data, 0644); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated work controls: %v\n%s", err, out)
	}
}

const generatedGoWorkConsumer = `package PACKAGE

import pegort "github.com/ornew/pego/runtime"
import("context";"errors";"reflect";"strings";"sync";"testing";"time")

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
  calls:=0;option:=func(o *pegort.X_parseOptions){calls++;o.X_unit=unit}
  v,err:=r.call(input,option)
  if err!=nil || calls!=1 {t.Fatalf("baseline callbacks=%d err=%v",calls,err)}
  before:=v
  for _,c:=range []context.Context{ctx,expired}{
   v,err=r.call(input,option,WithContext(c));if !isZero(v) || !errors.Is(err,c.Err()){t.Fatalf("pre-cancel value=%v err=%v",v,err)}
  }
  for _,steps:=range []bool{false,true}{
   limit:=func(n uint64) ParseOption {if steps{return WithWorkLimits(WorkLimits{MaxSteps:n})};return WithWorkLimits(WorkLimits{MaxInputWork:n})}
   lo,hi:=uint64(1),uint64(1<<20)
   if _,err=r.call(input,WithUnit(unit),limit(hi));err!=nil{t.Fatal(err)}
   for lo<hi {mid:=lo+(hi-lo)/2;_,e:=r.call(input,WithUnit(unit),limit(mid));if e==nil{hi=mid}else if errors.Is(e,ErrWorkLimit){lo=mid+1}else{t.Fatal(e)}}
   if lo<=1 {t.Fatal("work not charged")}
   v,err=r.call(input,WithUnit(unit),limit(lo));if err!=nil{t.Fatalf("exact ceiling=%d: %v",lo,err)}
   v,err=r.call(input,WithUnit(unit),limit(lo-1));var exhausted *WorkLimitError
   if !isZero(v) || !errors.As(err,&exhausted) || !errors.Is(err,ErrWorkLimit){t.Fatalf("one short value=%v err=%v",v,err)}
   if exhausted.Limit!=lo-1 || exhausted.Requested==0 || exhausted.Used>exhausted.Limit || exhausted.Requested<=exhausted.Limit-exhausted.Used {t.Fatalf("reservation metadata: %+v",exhausted)}
   if (exhausted.Kind==StepWorkLimit)!=steps {t.Fatalf("wrong counter: %+v",exhausted)}
  }
  if _,err=r.call(input,option,WithWorkLimits(WorkLimits{}),WithContext(nil));err!=nil{t.Fatal("retry",err)}
  _=before
 })}}
}
func TestValidationAndOptions(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 for _,r:=range routes(){
  _,err:=r.call("?",WithContext(ctx),WithMaxDepth(-1));if err==nil || !strings.Contains(err.Error(),"max depth"){t.Fatalf("configuration precedence: %v",err)}
  sentinel:=new(int)
  func(){defer func(){if got:=recover();got!=sentinel{t.Fatalf("option panic %v",got)}}();r.call("?",func(*pegort.X_parseOptions){panic(sentinel)})}()
 }
 for _,call:=range []func()error{func()error{_,e:=ParseRule("missing","?",WithContext(ctx));return e},func()error{_,e:=ParseRuleAST("missing","?",WithContext(ctx));return e}} {
  err:=call();if err==nil || errors.Is(err,context.Canceled) {t.Fatalf("name validation: %v",err)}
 }
 for _,r:=range routes(){
  calls:=0
  option:=func(o *pegort.X_parseOptions){calls++;o.X_limits=WorkLimits{MaxSteps:100000}}
  _,err:=r.call("é",WithContext(ctx),WithContext(context.Background()),WithWorkLimits(WorkLimits{MaxSteps:1}),option)
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
  if ast{v,err=ParseRuleAST(name,input,WithUnit(Bytes),WithWorkLimits(WorkLimits{MaxSteps:40}))}else{v,err=ParseRule(name,input,WithUnit(Bytes),WithWorkLimits(WorkLimits{MaxSteps:40}))}
  if !isZero(v)||!errors.Is(err,ErrWorkLimit){t.Fatalf("%s ast=%v swallowed interruption value=%v err=%v",name,ast,v,err)}
 }}
}
func TestStringAndMalformedTextWork(t *testing.T){
 text:=strings.Repeat("a",300)+"é"
 p:=&pegort.Parser{Control:pegort.X_newWorkControl(context.Background(),WorkLimits{MaxSteps:100000})}
 c:=&pegort.ActCtx{P:p}
 for _,op:=range []string{"==","!=","<","<=",">",">=","+"}{
  got:=c.Binary(op,text,text)
  want:=pegort.X_rtBinary(op,text,text)
  if !reflect.DeepEqual(got,want){t.Fatalf("string %s: %v want %v",op,got,want)}
 }
 if p.Control.X_stepsUsed()==0 || p.Control.X_inputUsed()!=0{t.Fatal("string accounting mixed input and value work")}
 for _,invoke:=range []func(*pegort.Parser){
  func(p *pegort.Parser){(&pegort.ActCtx{P:p}).Binary("==",text,text)},
  func(p *pegort.Parser){(&pegort.ActCtx{P:p}).Binary("+",text,text)},
  func(p *pegort.Parser){p.X_unitLen(text)},
 }{
  p:=&pegort.Parser{Control:pegort.X_newWorkControl(context.Background(),WorkLimits{MaxSteps:127})}
  func(){defer func(){f,ok:=recover().(pegort.X_fatal);if !ok || !errors.Is(f.X_err,ErrWorkLimit){t.Fatal("string work not bounded",f)}}();invoke(p)}()
  if p.Control.X_inputUsed()!=0{t.Fatal("action value charged input")}
 }
 // Malformed-source normalization must account for both sizing and copying.
 runes:=[]rune(strings.Repeat("�",300))
 p=&pegort.Parser{In:runes,Control:pegort.X_newWorkControl(context.Background(),WorkLimits{MaxInputWork:600,MaxSteps:600})}
 if got:=p.Text(0,len(runes));got!=string(runes){t.Fatal("normalized text mismatch")}
 if p.Control.X_inputUsed()!=600 || p.Control.X_stepsUsed()!=600{t.Fatalf("normalized counters: %+v",p.Control)}
 p=&pegort.Parser{In:runes,Control:pegort.X_newWorkControl(context.Background(),WorkLimits{MaxInputWork:599})}
 func(){defer func(){f,ok:=recover().(pegort.X_fatal);if !ok || !errors.Is(f.X_err,ErrWorkLimit){t.Fatal("normalized text unbounded",f)}}();p.Text(0,len(runes))}()
}

func TestFailureDiagnosticsBounded(t *testing.T){
 input:="?"+strings.Repeat("\n",6000)
 for _,r:=range routes(){for _,steps:=range []bool{false,true}{
  limits:=WorkLimits{MaxInputWork:128};if steps{limits=WorkLimits{MaxSteps:128}}
  v,err:=r.call(input,WithUnit(Bytes),WithWorkLimits(limits))
  if !isZero(v) || !errors.Is(err,ErrWorkLimit){t.Errorf("%s steps=%v early-failure diagnosis escaped budget: %v %v",r.name,steps,v,err)}
  if _,err=r.call("é");err!=nil{t.Fatal("retry after diagnostic interruption",err)}
 }}
}
func TestLineDiagnosticReservations(t *testing.T){
 for _,unit:=range []PositionUnit{Bytes,CodePoints}{
  input:=strings.Repeat("é\n",300)
  makeParser:=func(limit uint64)*pegort.Parser{
   p:=&pegort.Parser{X_unit:unit,X_bs:input,In:[]rune(input)};p.X_n=len(p.In);if unit==Bytes{p.X_n=len(input)}
   p.Control=pegort.X_newWorkControl(context.Background(),WorkLimits{MaxInputWork:limit})
   return p
  }
  p:=makeParser(100000);e:=p.MakeError(p.X_n-1,nil)
  if e.Line!=300 || e.Col!=map[PositionUnit]int{Bytes:3,CodePoints:2}[unit]{t.Fatal("line coordinate changed",e)}
  if p.Control.X_inputUsed()!=uint64(p.X_n){t.Errorf("diagnostic input uncharged: %d for %d",p.Control.X_inputUsed(),p.X_n)}
  p=makeParser(uint64(p.X_n))
  if e:=p.MakeError(p.X_n-1,nil);e.Line!=300{t.Fatal("exact diagnostic ceiling",e)}
  p=makeParser(uint64(p.X_n-1))
  func(){defer func(){f,ok:=recover().(pegort.X_fatal);var exhausted *WorkLimitError;if !ok || !errors.As(f.X_err,&exhausted) || exhausted.Kind!=InputWorkLimit{t.Error("one-short diagnostic unbounded",f)}}();p.MakeError(p.X_n-1,nil)}()
 }
}

func TestConcurrentLimits(t *testing.T){
 var wg sync.WaitGroup
 for worker:=0;worker<8;worker++{wg.Go(func(){for repeat:=0;repeat<8;repeat++{for _,r:=range routes(){
  if _,err:=r.call("é",WithWorkLimits(WorkLimits{MaxSteps:10000}));err!=nil{t.Error(err)}
  if _,err:=r.call(strings.Repeat("a",1000)+"é",WithWorkLimits(WorkLimits{MaxSteps:3}));!errors.Is(err,ErrWorkLimit){t.Errorf("independent limit: %v",err)}
 }}})};wg.Wait()
}
`

func TestGeneratedGoConversionWork(t *testing.T) {
	needsWorkControls(t)
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
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(testGoMod("gentest")), 0644); err != nil {
		t.Fatal(err)
	}
	pkg := "conversion"
	generatePackage(t, dir, pkg, g, GenOptions{Start: "main", Types: true, convertTypes: true})
	fixture := strings.ReplaceAll(generatedGoConversionConsumer, "PACKAGE", pkg)
	if err := os.WriteFile(filepath.Join(dir, pkg, "conversion_test.go"), []byte(fixture), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated conversion work: %v\n%s", err, out)
	}
}

const generatedGoConversionConsumer = `package PACKAGE

import pegort "github.com/ornew/pego/runtime"
import("context";"errors";"reflect";"strings";"testing";"gentest/PACKAGE/types")
func TestConversionVisits(t *testing.T){
 n,err:=Parse(strings.Repeat("é",100));if err!=nil{t.Fatal(err)}
 c:=pegort.X_newWorkControl(context.Background(),WorkLimits{MaxSteps:100000})
 a:=astConv{control:c};v:=a.toTypeBranch(n)
 if v==nil || len(v.Leaves)!=100{t.Fatal("conversion failed")}
 // Branch dispatch, field scan, list dispatch, each list edge, and for each leaf its dispatch
 // and the scan of its Text field.
 if c.X_stepsUsed()!=303 || c.X_inputUsed()!=0{t.Fatalf("conversion work: steps=%d input=%d",c.X_stepsUsed(),c.X_inputUsed())}
 for _,ceiling:=range []uint64{1,2,3,4,302}{
  c:=pegort.X_newWorkControl(context.Background(),WorkLimits{MaxSteps:ceiling})
  func(){defer func(){f,ok:=recover().(pegort.X_fatal);var e *WorkLimitError;if !ok || !errors.As(f.X_err,&e) || e.Kind!=StepWorkLimit || e.Used!=ceiling || e.Requested!=1{t.Fatalf("conversion ceiling %d interruption: %+v",ceiling,f)}}();(&astConv{control:c}).toTypeBranch(n)}()
 }
}
func TestParseConversionSharesCounter(t *testing.T){
 input:=strings.Repeat("é",100)
 beforeSteps,beforeInput:=uint64(0),uint64(0)
 conv:=func(n *Node,c *pegort.WorkControl,parseErr error)*types.Branch{beforeSteps,beforeInput=c.X_stepsUsed(),c.X_inputUsed();v:=(&astConv{control:c}).toTypeBranch(n);if c.X_stepsUsed()!=beforeSteps+303 || c.X_inputUsed()!=beforeInput{t.Fatal("conversion counter changed independently")};return v}
 o:=pegort.X_parseOptions{X_maxDepth:pegoGrammar.X_maxDepth,X_limits:WorkLimits{MaxSteps:100000}}
 retained,err:=pegort.RunParseConverted(rules[0],input,o,conv)
 if err!=nil || retained==nil{t.Fatal(err)}
 snapshot:=*retained.Leaves[0]
 total:=beforeSteps+303
 for _,limit:=range []uint64{beforeSteps+1,total-1,total}{
  o.X_limits.MaxSteps=limit
  got,err:=pegort.RunParseConverted(rules[0],input,o,func(n *Node,c *pegort.WorkControl,parseErr error)*types.Branch{return (&astConv{control:c}).toTypeBranch(n)})
  if limit==total {if err!=nil || len(got.Leaves)!=100{t.Fatal("exact conversion ceiling",err)}}else if got!=nil || !errors.Is(err,ErrWorkLimit){t.Fatalf("partial conversion leaked at %d: %v %v",limit,got,err)}
 }
 if !reflect.DeepEqual(*retained.Leaves[0],snapshot){t.Fatal("earlier result was overwritten")}
 // Cancellation raised inside conversion is surfaced at the same outer boundary.
 ctx,cancel:=context.WithCancel(context.Background());o.SetContext(ctx);o.X_limits=WorkLimits{}
 v,err:=pegort.RunParseConverted(rules[0],input,o,func(n *Node,c *pegort.WorkControl,parseErr error)*types.Branch{cancel();c.Check();return (&astConv{control:c}).toTypeBranch(n)})
 if v!=nil || !errors.Is(err,context.Canceled){t.Fatalf("conversion cancellation: %v %v",v,err)}
 if _,err:=ParseAST(input);err!=nil{t.Fatal("retry",err)}
}
`

func TestGeneratedGoWorkControlNameCollisions(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated public-name collision consumers")
	}
	g, err := parseTest(`type WorkLimits = { Text string }

type WorkLimits_ = { Text string }

type WorkLimitError = {
    Value WorkLimits
}

type WorkLimitKind ~ WorkLimits

type WithContext ~ WorkLimits

type WithWorkLimits ~ WorkLimits

type ErrWorkLimit ~ WorkLimits

type InputWorkLimit ~ WorkLimits

type StepWorkLimit ~ WorkLimits

rule main WorkLimits = t:("é") #atomic => WorkLimits{Text: text($t)}`)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(testGoMod("workcollision")), 0644); err != nil {
		t.Fatal(err)
	}
	for _, convert := range []bool{false, true} {
		pkg := fmt.Sprintf("collision_%v", convert)
		opts := GenOptions{Package: pkg, Module: "workcollision/" + pkg, Runtime: RuntimeImport, Start: "main", Types: true, convertTypes: convert, WorkControls: true}
		files, err := Generate(g, opts)
		if err != nil {
			t.Fatal(err)
		}
		again, err := Generate(g, opts)
		if err != nil || !slices.EqualFunc(files, again, func(a, b GeneratedFile) bool { return a.Path == b.Path && string(a.Content) == string(b.Content) }) {
			t.Fatal("unstable generation")
		}
		fixture := strings.ReplaceAll(generatedGoWorkCollisionConsumer, "PACKAGE", pkg)
		files = append(files, GeneratedFile{"consumer_test.go", []byte(fixture)})
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
		t.Fatalf("work-control name collision: %v\n%s", err, out)
	}
}

const generatedGoWorkCollisionConsumer = `package PACKAGE_test
import("context";"errors";"testing";p "workcollision/PACKAGE";g "workcollision/PACKAGE/types")
func TestPublicNames(t *testing.T){
 var parse func(string,...p.ParseOption)(*g.WorkLimits,error)=p.ParseAST
 v,err:=parse("é",p.WithContext(context.Background()),p.WithWorkLimits(p.WorkLimits{MaxSteps:1000}),p.WithUnit(p.Bytes))
 if err!=nil || v.Text!="é" || v.End!=2{t.Fatal(v,err)}
 var second *g.WorkLimits_=&g.WorkLimits_{}
 var box *g.WorkLimitError=&g.WorkLimitError{Value:v}
 var kind g.WorkLimitKind=v
 var ctx g.WithContext=v
 var limits g.WithWorkLimits=v
 var sentinel g.ErrWorkLimit=v
 var input g.InputWorkLimit=v
 var steps g.StepWorkLimit=v
 _,_,_,_,_,_,_,_=second,box,kind,ctx,limits,sentinel,input,steps
 _,err=parse("é",p.WithWorkLimits(p.WorkLimits{MaxSteps:1}))
 var exhausted *p.WorkLimitError
 if !errors.Is(err,p.ErrWorkLimit) || !errors.As(err,&exhausted){t.Fatal(err)}
 var counter p.WorkLimitKind=exhausted.Kind
 if counter!=p.StepWorkLimit{t.Fatal(counter)}
}
`

func TestGeneratedGoNilWorkPublication(t *testing.T) {
	needsWorkControls(t)
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
	code, err := generateCode(g, GenOptions{Package: "nilpublication", Start: "main", Types: true})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range map[string][]byte{"go.mod": []byte(testGoMod("nilpublication")), "parser.go": code, "nil_test.go": []byte(generatedGoNilPublicationConsumer)} {
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

import pegort "github.com/ornew/pego/runtime"
import("context";"errors";"reflect";"testing")
func TestNilPublication(t *testing.T){
 original:=trules[0].Entry;defer func(){trules[0].Entry=original}()
 for _,named:=range []bool{false,true}{for _,ok:=range []bool{false,true}{
  ctx,cancel:=context.WithCancel(context.Background())
  // Cancel exactly at the last execution operation, with no guessed poll count.
  trules[0].Entry=func(p *pegort.TParser)(any,bool){cancel();return nil,ok}
  var v any;var err error
  if named{v,err=ParseRuleAST("main","",WithContext(ctx))}else{v,err=ParseAST("",WithContext(ctx))}
  if (v!=nil && !reflect.ValueOf(v).IsZero()) || !errors.Is(err,context.Canceled){t.Errorf("named=%v successful=%v value=%v err=%v",named,ok,v,err)}
 }}
 for _,named:=range []bool{false,true}{
  ctx,cancel:=context.WithCancel(context.Background())
  established:=&WorkLimitError{Kind:StepWorkLimit,Limit:1,Used:1,Requested:1}
  trules[0].Entry=func(p *pegort.TParser)(any,bool){cancel();panic(pegort.X_fatal{X_err:established})}
  var err error
  if named{_,err=ParseRuleAST("main","",WithContext(ctx))}else{_,err=ParseAST("",WithContext(ctx))}
  if err!=established{t.Fatalf("established fatal was masked: %v",err)}
 }
 trules[0].Entry=original
 if v,err:=ParseAST("");v!=nil || err!=nil{t.Fatalf("nil success retry: %v %v",v,err)}
 if v,err:=ParseAST("é");v==nil || v.Text!="é" || err!=nil{t.Fatalf("value retry: %v %v",v,err)}
}

type clonePhaseContext struct{context.Context;control *pegort.WorkControl;marker any;calls int}
func(c *clonePhaseContext)Done()<-chan struct{}{return make(chan struct{})}
func(c *clonePhaseContext)Err()error{
 c.calls++
 if c.control!=nil && c.control.X_stepsUsed()>=256 {if c.marker!=nil{panic(c.marker)};return context.Canceled}
 return nil
}
func TestRecoveredNilClonePublication(t *testing.T){
 original:=trules[0].Entry;defer func(){trules[0].Entry=original}()
 recovered:=make([]*SyntaxError,300)
 for i:=range recovered{recovered[i]=&SyntaxError{Line:i+1,Col:1}}
 for _,named:=range []bool{false,true}{for _,panicMode:=range []bool{false,true}{
  ctx:=&clonePhaseContext{Context:context.Background()};if panicMode{ctx.marker=new(int)}
  trules[0].Entry=func(p *pegort.TParser)(any,bool){ctx.control=p.Control;p.Recovered=recovered;return nil,true}
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
	needsWorkControls(t)
	if testing.Short() {
		t.Skip("builds configured legacy nil consumers")
	}
	g, err := parseTest(`type Word = { Text string }

rule main _ = x:leaf? => $x

rule leaf Word = t:("é") #atomic => Word{Text: text($t)}`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := generateCode(g, GenOptions{Package: "legacynil", Start: "main", Types: true})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range map[string][]byte{"go.mod": []byte(testGoMod("legacynil")), "parser.go": code, "nil_test.go": []byte(generatedGoLegacyNilConsumer)} {
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
   {WithWorkLimits(WorkLimits{MaxSteps:1}),ErrWorkLimit},
  }{
   got,err:=r("é",row.option)
   if got!=nil || !errors.Is(err,row.want){t.Fatalf("interruption converted nil: %T %v %v",got,got,err)}
  }
 }
 if got,err:=ParseAST("é");err!=nil || got==nil{t.Fatal("retry",got,err)}
}
`

func TestGeneratedGoConfiguredNilOrdinaryErrors(t *testing.T) {
	needsWorkControls(t)
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
			code, err := generateCode(g, GenOptions{Package: "ordinarynil", Start: "main", Types: true})
			if err != nil {
				t.Fatal(err)
			}
			consumer := strings.NewReplacer("INPUT", fmt.Sprintf("%q", tc.input), "OPTION", tc.option).Replace(generatedGoOrdinaryNilErrorConsumer)
			dir := t.TempDir()
			for name, data := range map[string][]byte{"go.mod": []byte(testGoMod("ordinarynil")), "parser.go": code, "nil_test.go": []byte(consumer)} {
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
  for _,extra:=range [][]ParseOption{nil,{WithContext(context.Background())},{WithWorkLimits(WorkLimits{MaxSteps:1000000,MaxInputWork:1000000})}}{
   opts:=append([]ParseOption{WithUnit(unit),OPTION},extra...)
   got,err:=ParseAST(INPUT,opts...)
   if err==nil || got!=nil{t.Fatalf("ordinary error returned a value: %T %v %v",got,got,err)}
  }
 }
}
`
