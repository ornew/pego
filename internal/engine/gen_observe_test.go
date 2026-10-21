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

func TestGeneratedTraceOptions(t *testing.T) {
	g, err := syntax.Parse(`rule main = "a"`)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := generateCode(g, GenOptions{Package: "fixture", Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"type TraceEvent", "func newProfileObservation", "func WithTrace"} {
		if strings.Contains(string(plain), marker) {
			t.Fatalf("ordinary output contains %s", marker)
		}
	}
	if _, err := generateCode(g, GenOptions{Package: "fixture", Start: "main", Types: true, Observability: true}); err != nil {
		t.Fatal("typed observability:", err)
	}
	for _, opts := range []GenOptions{{Document: true}, {Document: true, Types: true}} {
		opts.Package, opts.Start, opts.Observability = "fixture", "main", true
		if _, err := generateCode(g, opts); err != nil {
			t.Fatal("Document observability:", err)
		}
	}
	if code, err := generateTSBundle(g, GenOptions{Start: "main", Observability: true}); err != nil || !strings.Contains(string(code), "export interface TraceEvent") {
		t.Fatal("TypeScript observability:", err)
	}
	if _, err := generateTSBundle(g, GenOptions{Start: "main", Observability: true, Document: true}); err == nil {
		t.Fatal("TypeScript accepted unsupported Document")
	}
}

func TestGeneratedNodeTrace(t *testing.T) {
	needsWorkControls(t)
	if testing.Short() {
		t.Skip("builds generated standalone tracing consumers")
	}
	fixtures := []struct{ name, grammar, input, checks string }{
		{"plain", `rule main = part part
rule part = "a"`, "aa", `if count("part",TraceEnter)!=2 {t.Fatal(events)}`},
		{"lean", `rule main = -part part
rule part = "a"`, "aa", `if count("part",TraceEnter)!=2 {t.Fatal(events)}`},
		{"memo", `type Word = { Text string }
rule main = &part &part &part part
rule part Word = t:(letter) #atomic => Word{Text: text($t)}
rule letter = "a"`, "a", `hits:=0;for _,e:=range events {if e.Memo {hits++;if e.Evals!=0 {t.Fatal(e)}}};if hits==0 {t.Fatal("missing memo hit",events)}`},
		{"leftrec", `rule main = expr
rule expr = expr "a" / "a"`, "aaa", `growth:=false;for _,e:=range events {if e.Rule=="expr"&&e.Kind==TraceExit&&e.Evals>1 {growth=true}};if !growth {t.Fatal("missing growth",events)}`},
		{"dispatch", `rule main = first / second
rule first = "a"
rule second = "b"`, "b", `if count("first",TraceEnter)!=0 || count("first",TraceSkipped)!=1 || count("second",TraceEnter)!=1 {t.Fatal(events)}`},
		{"recovery", `rule main = (first / second) #recover(skip="!")
rule first = "a"
rule second = "b"`, "!", `if count("first",TraceSkipped)!=1 || count("second",TraceSkipped)!=1 {t.Fatal(events)}`},
		{"operators", `rule main = level(expr, product) "+" expr
rule num = [0-9]+
rule expr = operators {operand num;level sum {infix left "+"};level product {infix left "*"}}`, "1+2*3", `nested:=false;for _,e:=range events {if e.Rule=="expr"&&e.Kind==TraceEnter&&e.Level>0 {nested=true}};if !nested {t.Fatal("missing binding levels",events)}`},
		{"actions", `type N = { Text string }
rule main N = a:token {len($a) > 0} => N{Text: text($a)}
rule token = [a-z]+`, "abc", `if count("token",TraceEnter)!=1 {t.Fatal(events)}`},
		{"unicode", `rule main = part part
rule part = "é"`, "éé", `last:=events[len(events)-1];expectedEnd:=2;if unit==Bytes {expectedEnd=4};if last.End!=expectedEnd {t.Fatal(last)}`},
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(testGoMod("tracefixture")), 0644); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		g, err := syntax.Parse(fixture.grammar)
		if err != nil {
			t.Fatal(fixture.name, err)
		}
		referenceCode, err := generateCode(g, GenOptions{Package: "reference_" + fixture.name, Start: "main", Recognize: true})
		if err != nil {
			t.Fatal(fixture.name, err)
		}
		referenceDir := filepath.Join(dir, "reference_"+fixture.name)
		if err := os.Mkdir(referenceDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(referenceDir, "parser.go"), referenceCode, 0644); err != nil {
			t.Fatal(err)
		}
		code, err := generateCode(g, GenOptions{Package: fixture.name, Start: "main", Recognize: true, Observability: true})
		if err != nil {
			t.Fatal(fixture.name, err)
		}
		pkg := filepath.Join(dir, fixture.name)
		if err := os.Mkdir(pkg, 0755); err != nil {
			t.Fatal(err)
		}
		consumer := strings.NewReplacer("PACKAGE", fixture.name, "INPUT", fmt.Sprintf("%q", fixture.input), "CHECKS", fixture.checks).Replace(generatedNodeTraceConsumer)
		for name, data := range map[string][]byte{"parser.go": code, "trace_test.go": []byte(consumer)} {
			if err := os.WriteFile(filepath.Join(pkg, name), data, 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	cmd := exec.Command("go", "test", "-p", "1", "-race", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated tracing: %v\n%s", err, out)
	}
}

const generatedNodeTraceConsumer = `package PACKAGE

import pegort "github.com/ornew/pego/runtime"
import("context";"encoding/json";"errors";"fmt";"reflect";"sync";"testing";reference "tracefixture/reference_PACKAGE")
func TestTrace(t *testing.T){
 for _,unit:=range []PositionUnit{CodePoints,Bytes}{
  for _,controlled:=range []bool{false,true}{
   opts:=[]ParseOption{WithUnit(unit)};if controlled {opts=append(opts,WithWorkLimits(WorkLimits{MaxSteps:100000}))}
   refUnit:=reference.CodePoints;if unit==Bytes{refUnit=reference.Bytes}
   referenceOpts:=[]reference.ParseOption{reference.WithUnit(refUnit)};if controlled{referenceOpts=append(referenceOpts,reference.WithWorkLimits(reference.WorkLimits{MaxSteps:100000}))}
   want,werr:=reference.Parse(INPUT,referenceOpts...)
   var events []pegort.TraceEvent
   traced:=append(append([]ParseOption{},opts...),pegort.WithTrace(func(e pegort.TraceEvent){events=append(events,e)}))
   got,gerr:=Parse(INPUT,traced...)
   a,_:=json.Marshal(want);b,_:=json.Marshal(got)
   if string(a)!=string(b)||fmt.Sprint(werr)!=fmt.Sprint(gerr){t.Fatal("trace changed result",string(a),string(b),werr,gerr)}
   if len(events)<2 || events[0].Rule!="main" || events[0].Depth!=1 {t.Fatal(events)}
   var stack []pegort.TraceEvent
   for _,e:=range events{
    if e.ExaminedKnown {t.Fatal("unavailable range claimed",e)}
    switch e.Kind{
    case pegort.TraceEnter:if e.Depth!=len(stack)+1 {t.Fatal("entry depth",e,stack)};stack=append(stack,e)
    case pegort.TraceExit:if len(stack)==0 {t.Fatal("unpaired exit",e)};in:=stack[len(stack)-1];stack=stack[:len(stack)-1];if in.Rule!=e.Rule||in.Pos!=e.Pos||in.Depth!=e.Depth||in.Level!=e.Level {t.Fatal("exit mismatch",in,e)};if e.Memo&&e.Evals!=0 {t.Fatal("memo evaluated",e)};if !e.Memo&&e.Evals<1 {t.Fatal("missing body evaluation",e)}
    case pegort.TraceSkipped:if e.SkipReason!="first-character"||e.Evals!=0||e.Memo||e.Depth!=len(stack)+1 {t.Fatal("invalid skip",e)}
    default:t.Fatal(e)
    }
   }
   if len(stack)!=0 {t.Fatal("open calls",stack)}
   count:=func(rule string,kind pegort.TraceKind)int{n:=0;for _,e:=range events{if e.Rule==rule&&e.Kind==kind{n++}};return n}
   _=count
   CHECKS
   events=nil
   if err:=Recognize(INPUT,traced...);fmt.Sprint(err)!=fmt.Sprint(werr){t.Fatal("recognition parity",err,werr)}
   if len(events)==0||events[0].Rule!="main"{t.Fatal("recognizer unobserved")}
   events=nil
   if _,err:=ParseRule("main",INPUT,traced...);fmt.Sprint(err)!=fmt.Sprint(werr){t.Fatal("named parity",err,werr)}
   if len(events)==0 {t.Fatal("named rule unobserved")}
  }
 }
}
func TestRejectionAndDepthParity(t *testing.T){
 for _,input:=range []string{"", INPUT+"?"}{
  for _,unit:=range []PositionUnit{CodePoints,Bytes}{
   for _,depth:=range []int{1,100}{
    opts:=[]ParseOption{WithUnit(unit),WithMaxDepth(depth)}
    refUnit:=reference.CodePoints;if unit==Bytes{refUnit=reference.Bytes}
    want,werr:=reference.Parse(input,reference.WithUnit(refUnit),reference.WithMaxDepth(depth))
    var events []pegort.TraceEvent
    got,gerr:=Parse(input,append(opts,pegort.WithTrace(func(e pegort.TraceEvent){events=append(events,e)}))...)
    a,_:=json.Marshal(want);b,_:=json.Marshal(got)
    if string(a)!=string(b)||fmt.Sprint(werr)!=fmt.Sprint(gerr){t.Fatal("rejection/depth parity",werr,gerr,events)}
   }
  }
 }
}
func TestFilteringAndIsolation(t *testing.T){
 var all,filtered []pegort.TraceEvent
 Parse(INPUT,pegort.WithTrace(func(e pegort.TraceEvent){all=append(all,e)}))
 Parse(INPUT,pegort.WithTrace(func(e pegort.TraceEvent){filtered=append(filtered,e)}),pegort.WithTraceMaxDepth(1))
 for _,e:=range filtered{if e.Depth!=1 {t.Fatal("depth filter",e)}}
 if len(filtered)!=2 {t.Fatal(filtered)}
 delivered:=0;Parse(INPUT,pegort.WithTrace(func(pegort.TraceEvent){delivered++}),pegort.WithTraceFilter("not-defined"));if delivered!=0 {t.Fatal("unknown filter matched")}
 names:=[]string{"main"};filter:=pegort.WithTraceFilter(names...);names[0]="not-defined"
 var selected []pegort.TraceEvent;Parse(INPUT,pegort.WithTrace(func(e pegort.TraceEvent){selected=append(selected,e)}),filter)
 if !reflect.DeepEqual(all,selected){t.Fatal("filter ownership or descendants")}
 entered:=false
 Parse(INPUT,pegort.WithTrace(func(e pegort.TraceEvent){if !entered{entered=true;Parse(INPUT,pegort.WithTrace(func(pegort.TraceEvent){}))}}))
 if !entered {t.Fatal("missing callback")}
 var wg sync.WaitGroup
 for i:=0;i<4;i++{wg.Go(func(){for j:=0;j<4;j++{Parse(INPUT,pegort.WithTrace(func(pegort.TraceEvent){}))}})};wg.Wait()
 // A later invocation must have no prior callback, even after pooling.
 calls:=0;Parse(INPUT,pegort.WithTrace(func(pegort.TraceEvent){calls++}));before:=calls
 Parse(INPUT);if calls!=before{t.Fatal("observer leaked across parses")}
 private:=&pegort.Parser{Observation:&pegort.X_observation{X_trace:func(pegort.TraceEvent){}}}
 private.X_release();if private.Observation!=nil {t.Fatal("pool retained observer")}
}
func TestCallbackPanicAndInterruption(t *testing.T){
 marker:=&struct{n int}{7}
 for _,value:=range []any{marker,pegort.X_fatal{errors.New("private marker")},pegort.X_contextPanic{marker},pegort.X_tracePanic{marker}}{
  func(){defer func(){got:=recover();if got!=value{t.Fatalf("panic identity %T %v want %T %v",got,got,value,value)}}();Parse(INPUT,pegort.WithTrace(func(pegort.TraceEvent){panic(value)}));t.Fatal("panic swallowed")}()
 }
 // A failure after nested callbacks still cleans pooled observation state.
 n:=0;func(){defer func(){if recover()!=marker{t.Fatal("nested panic")}}();Parse(INPUT,pegort.WithTrace(func(pegort.TraceEvent){n++;if n==3{panic(marker)}}));t.Fatal("nested panic absent")}()
 Parse(INPUT)
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=Parse(INPUT,WithContext(ctx),pegort.WithTrace(func(pegort.TraceEvent){t.Fatal("callback after pre-cancel")}));!errors.Is(err,context.Canceled){t.Fatal(err)}
 var events []pegort.TraceEvent
 _,err:=Parse(INPUT,WithWorkLimits(WorkLimits{MaxSteps:1}),pegort.WithTrace(func(e pegort.TraceEvent){events=append(events,e)}))
 if !errors.Is(err,ErrWorkLimit){t.Fatal("budget",err)}
 for _,e:=range events{if e.Kind==pegort.TraceExit&&e.Rule=="main"{t.Fatal("fabricated root exit",events)}}
 Parse(INPUT)
}
`
