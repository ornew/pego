package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedTypedTrace(t *testing.T) {
	needsWorkControls(t)
	if testing.Short() {
		t.Skip("builds standalone typed trace consumers")
	}
	fixtures := []struct {
		name, grammar, input, checks string
		fallback, general            bool
	}{
		{name: "plain", grammar: `type W = { Text string }
rule main W = t:(part) #atomic => W{Text: text($t)}
rule part W = t:("a") #atomic => W{Text: text($t)}`, input: "a", checks: `expectedEvents:=[]TraceEvent{{Kind:TraceEnter,Rule:"main",Depth:1},{Kind:TraceEnter,Rule:"part",Depth:2},{Kind:TraceExit,Rule:"part",Depth:2,Matched:true,End:1,Evals:1},{Kind:TraceExit,Rule:"main",Depth:1,Matched:true,End:1,Evals:1}};if !reflect.DeepEqual(events,expectedEvents){t.Fatal("exact plain calls",events)}`},
		{name: "lean", grammar: `type W = { Text string }
rule main W = t:(-space part) #atomic => W{Text: text($t)}
rule space = " "
rule part W = t:("a") #atomic => W{Text: text($t)}`, input: " a", checks: `if count("space",TraceEnter)!=1 || count("part",TraceEnter)!=1 {t.Fatal(events)}`},
		{name: "memo", grammar: `type W = { Text string }
rule main W = &part &part &part p:part => $p
rule part W = t:(letter) #atomic => W{Text: text($t)}
rule letter = "a"`, input: "a", checks: `hits:=0;for _,e:=range events {if e.Memo {hits++}};if hits==0 {t.Fatal("missing memo",events)}`},
		{name: "framed", grammar: `type W = { Text string }
type N = { Value W }
rule main N = p:part => N{Value:$p}
rule part W = t:("a") #atomic => W{Text: text($t)}`, input: "a", checks: `if count("part",TraceEnter)!=1 {t.Fatal(events)}`},
		{name: "general", grammar: `type W = { Text string }
var x int
rule main W = {x:=1} #cut p:part {x==1} => $p
rule part W = t:("a") #atomic => W{Text: text($t)}`, input: "a", general: true, checks: `if count("part",TraceEnter)!=1 {t.Fatal(events)}`},
		{name: "leftrec", grammar: `type W = { Text string }
rule main W = t:(expr) #atomic => W{Text: text($t)}
rule expr W = t:(expr "a" / "a") #atomic => W{Text: text($t)}`, input: "aaa", checks: `growth:=false;for _,e:=range events {if e.Rule=="expr"&&e.Kind==TraceExit&&e.Evals>1 {growth=true}};if !growth {t.Fatal("missing growth",events)}`},
		{name: "dispatch", grammar: `type W = { Text string }
rule main W = t:(first / second) #atomic => W{Text: text($t)}
rule first W = t:("a") #atomic => W{Text: text($t)}
rule second W = t:("b") #atomic => W{Text: text($t)}`, input: "b", checks: `if count("first",TraceEnter)!=0 || count("first",TraceSkipped)!=1 {t.Fatal(events)}`},
		{name: "recovery", grammar: `type W = { Text string }
rule main W = t:(part #recover(skip="!")) #atomic => W{Text: text($t)}
rule part W = t:("a") #atomic => W{Text: text($t)}`, input: "!", checks: `if count("part",TraceEnter)!=1 {t.Fatal(events)}`},
		{name: "operators", grammar: `type W = { Text string }
type B = { Left Expr; Right Expr }
type Expr ~ W | B
rule main Expr = l:level(expr, product) "+" r:expr => B{Left:$l,Right:$r}
rule expr Expr = operators {operand num;level sum {infix left "+" => B{Left:$lhs,Right:$rhs}};level product {infix left "*" => B{Left:$lhs,Right:$rhs}}}
rule num W = t:([0-9]+) #atomic => W{Text: text($t)}`, input: "1+2*3", checks: `nested:=false;for _,e:=range events {if e.Rule=="expr"&&e.Kind==TraceEnter&&e.Level>0 {nested=true}};if !nested {t.Fatal("missing level",events)}`},
		{name: "mixed", grammar: `type W = { Text string }
rule main W = t:(part) #atomic => W{Text: text($t)}
rule part W = t:("a") #atomic => W{Text: text($t)}
rule cst = part ";"`, input: "a", checks: `if count("part",TraceEnter)!=1 {t.Fatal(events)}`},
		{name: "field", grammar: `type W = { Text string }
type N = { Value W }
rule main W = n:box => $n.Value
rule box N = p:part => N{Value:$p}
rule part W = t:("a") #atomic => W{Text: text($t)}`, input: "a", fallback: true, checks: `if count("box",TraceEnter)!=1 {t.Fatal(events)}`},
		{name: "unicode", grammar: `type W = { Text string }
rule main W = t:(part part) #atomic => W{Text: text($t)}
rule part W = t:("é") #atomic => W{Text: text($t)}`, input: "éé", checks: `last:=events[len(events)-1];end:=2;if unit==Bytes {end=4};if last.End!=end {t.Fatal(last)}`},
		{name: "nilvalue", grammar: `type W = { Text string }
rule main *W = part => nil
rule part W = t:("a") #atomic => W{Text: text($t)}`, input: "a", checks: `if count("part",TraceEnter)!=1 {t.Fatal(events)}`},
		{name: "collision", grammar: `type TraceKind = { Text string }
type TraceEvent = { Value WithTrace }
type TraceEnter = { Text string }
type TraceExit = { Text string }
type TraceSkipped = { Text string }
type WithTrace = { Text string }
type WithTraceFilter = { Text string }
type WithTraceMaxDepth = { Text string }
type TraceEvent_ = { Text string }
rule main TraceEvent = p:part => TraceEvent{Value:$p}
rule part WithTrace = t:("a") #atomic => WithTrace{Text: text($t)}`, input: "a", checks: `if count("part",TraceEnter)!=1 {t.Fatal(events)}`},
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(testGoMod("typedtracefixture")), 0644); err != nil {
		t.Fatal(err)
	}
	write := func(pkg, name string, data []byte) {
		t.Helper()
		path := filepath.Join(dir, pkg)
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range fixtures {
		g, err := parseTest(f.grammar)
		if err != nil {
			t.Fatal(f.name, err)
		}
		for _, convert := range []bool{false, true} {
			pkg := f.name
			if convert {
				pkg += "_converted"
			}
			opts := GenOptions{Package: "reference_" + pkg, Start: "main", Types: true, Recognize: true, convertTypes: convert}
			generatePackageIn(t, dir, "typedtracefixture", opts.Package, g, opts)
			opts.Package, opts.Observability = pkg, true
			code := generatePackageIn(t, dir, "typedtracefixture", pkg, g, opts)
			if !convert && !f.fallback && !strings.Contains(code, "return rt.TParse(trules[") {
				t.Fatal(pkg, "direct start unexpectedly unavailable")
			}
			if (convert || f.fallback) && !strings.Contains(code, "return rt.RunParseConverted(rules[") {
				t.Fatal(pkg, "missing Node conversion")
			}
			nodeChecks := strings.NewReplacer("PACKAGE", pkg, "INPUT", fmt.Sprintf("%q", f.input), "CHECKS", f.checks).Replace(strings.ReplaceAll(generatedNodeTraceConsumer, "tracefixture/", "typedtracefixture/"))
			typedChecks := strings.NewReplacer("INPUT", fmt.Sprintf("%q", f.input), "CHECKS", f.checks).Replace(generatedTypedTraceConsumer)
			write(pkg, "trace_test.go", []byte(nodeChecks+typedChecks))
		}
	}
	cmd := exec.Command("go", "test", "-p", "1", "-race", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated typed tracing: %v\n%s", err, out)
	}
}

const generatedTypedTraceConsumer = `
func checkTypedTrace(t *testing.T, events []TraceEvent){
 t.Helper();if len(events)<2 || events[0].Depth!=1 {t.Fatal(events)}
 var stack []TraceEvent
 for _,e:=range events {
  if e.ExaminedKnown {t.Fatal("unknown examined",e)}
  switch e.Kind {
  case TraceEnter:if e.Depth!=len(stack)+1{t.Fatal("depth",e,stack)};stack=append(stack,e)
  case TraceExit:if len(stack)==0{t.Fatal("unpaired exit",e)};in:=stack[len(stack)-1];stack=stack[:len(stack)-1];if in.Rule!=e.Rule||in.Pos!=e.Pos||in.Level!=e.Level||in.Depth!=e.Depth {t.Fatal("exit mismatch",in,e)};if e.Memo&&e.Evals!=0||!e.Memo&&e.Evals<1{t.Fatal("body count",e)}
  case TraceSkipped:if e.Depth!=len(stack)+1||e.SkipReason!="first-character"{t.Fatal("skip",e)}
  default:t.Fatal(e)
  }
 }
 if len(stack)!=0 {t.Fatal("open stack",stack)}
}
func TestTypedTraceParity(t *testing.T){
 for _,unit:=range []PositionUnit{CodePoints,Bytes}{for _,controlled:=range []bool{false,true}{
  opts:=[]ParseOption{WithUnit(unit)};refUnit:=reference.CodePoints;if unit==Bytes{refUnit=reference.Bytes};refOpts:=[]reference.ParseOption{reference.WithUnit(refUnit)}
  if controlled{opts=append(opts,WithWorkLimits(WorkLimits{MaxSteps:100000}));refOpts=append(refOpts,reference.WithWorkLimits(reference.WorkLimits{MaxSteps:100000}))}
  for _,input:=range []string{INPUT,"",INPUT+"?"}{
   want,werr:=reference.ParseAST(input,refOpts...);var events []TraceEvent
   got,gerr:=ParseAST(input,append(append([]ParseOption{},opts...),WithTrace(func(e TraceEvent){events=append(events,e)}))...)
   a,ae:=json.Marshal(want);b,be:=json.Marshal(got);if ae!=nil||be!=nil||string(a)!=string(b)||fmt.Sprint(werr)!=fmt.Sprint(gerr){t.Fatal("typed parity",string(a),string(b),werr,gerr,ae,be)}
   checkTypedTrace(t,events)
   if input==INPUT{count:=func(rule string,kind TraceKind)int{n:=0;for _,e:=range events{if e.Rule==rule&&e.Kind==kind{n++}};return n};_=count;CHECKS}
   seenRules:=map[string]bool{}
   for _,r:=range rules {
    if seenRules[r.Name]{continue};seenRules[r.Name]=true
    want,werr:=reference.ParseRuleAST(r.Name,input,refOpts...);events=nil
    got,gerr:=ParseRuleAST(r.Name,input,append(append([]ParseOption{},opts...),WithTrace(func(e TraceEvent){events=append(events,e)}))...)
    a,_:=json.Marshal(want);b,_:=json.Marshal(got);if string(a)!=string(b)||fmt.Sprint(werr)!=fmt.Sprint(gerr){t.Fatal("named typed parity",r.Name,string(a),string(b),werr,gerr)}
    checkTypedTrace(t,events)
   }
  }
 }}
}
func TestTypedTraceLifecycle(t *testing.T){
 routes:=[]func(...ParseOption)(any,error){func(opts ...ParseOption)(any,error){return ParseAST(INPUT,opts...)},func(opts ...ParseOption)(any,error){return ParseRuleAST("main",INPUT,opts...)}}
 for _,parse:=range routes{
  marker:=&struct{n int}{11}
  for _,value:=range []any{marker,pegort.X_fatal{errors.New("private marker")},pegort.X_evalError{X_msg:"private marker"},pegort.X_contextPanic{marker},pegort.X_tracePanic{marker}}{func(){defer func(){if got:=recover();got!=value{t.Fatalf("typed panic %T %v want %T %v",got,got,value,value)}}();parse(WithTrace(func(TraceEvent){panic(value)}));t.Fatal("panic absent")}()}
  late:=0;func(){defer func(){if recover()!=marker{t.Fatal("late typed panic")}}();parse(WithTrace(func(TraceEvent){late++;if late==2{panic(marker)}}));t.Fatal("late panic absent")}()
  var events []TraceEvent;parse(WithTrace(func(e TraceEvent){events=append(events,e)}));checkTypedTrace(t,events)
  seen:=0;parse(WithTrace(func(TraceEvent){seen++}));before:=seen;parse();if seen!=before{t.Fatal("typed callback retained")}
  n:=0;parse(WithTrace(func(TraceEvent){if n==0{n++;parse(WithTrace(func(TraceEvent){}))}}));if n!=1{t.Fatal(n)}
  var filtered []TraceEvent;parse(WithTrace(func(e TraceEvent){filtered=append(filtered,e)}),WithTraceMaxDepth(1));if len(filtered)!=2{t.Fatal(filtered)};for _,e:=range filtered{if e.Depth!=1{t.Fatal(e)}}
  parse(WithTrace(func(TraceEvent){t.Fatal("unknown typed filter")}),WithTraceFilter("not-defined"))
  parse(WithTrace(func(e TraceEvent){if e.Kind==TraceEnter&&e.Depth>1{panic(marker)}}),WithTraceMaxDepth(1))
  ctx,cancel:=context.WithCancel(context.Background());cancel()
  _,err:=parse(WithContext(ctx),WithTrace(func(TraceEvent){t.Fatal("pre-cancel callback")}));if !errors.Is(err,context.Canceled){t.Fatal(err)}
  events=nil;_,err=parse(WithWorkLimits(WorkLimits{MaxSteps:1}),WithTrace(func(e TraceEvent){events=append(events,e)}));if !errors.Is(err,ErrWorkLimit){t.Fatal("typed budget",err)};for _,e:=range events{if e.Kind==TraceExit&&e.Rule=="main"{t.Fatal("fabricated typed exit",events)}}
 }
 var wg sync.WaitGroup;for i:=0;i<4;i++{wg.Go(func(){for j:=0;j<4;j++{ParseAST(INPUT,WithTrace(func(TraceEvent){}))}})};wg.Wait()
 p:=&pegort.TParser{Parser:&pegort.Parser{Observation:&pegort.X_observation{X_trace:func(pegort.TraceEvent){}}}};p.X_recycle();if p.Observation!=nil{t.Fatal("typed pool retained observer")}
}
`

func TestGeneratedTracePrivateTypeNamesRemainInvalid(t *testing.T) {
	for _, name := range []string{"observation", "observationCall", "tracePanic"} {
		g, err := parseTest("type " + name + " = { Text string }\nrule main = \"a\"")
		if err != nil {
			t.Fatal(err)
		}
		for _, observed := range []bool{false, true} {
			if _, err := generateCode(g, GenOptions{Package: "fixture", Start: "main", Types: true, Observability: observed}); err == nil || !strings.Contains(err.Error(), "must start with an uppercase letter") {
				t.Fatalf("%s observed=%v: %v", name, observed, err)
			}
		}
	}
}
