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

func TestGeneratedTypedTrace(t *testing.T) {
	if testing.Short() {
		t.Skip("builds standalone typed trace consumers")
	}
	fixtures := []struct {
		name, grammar, input, checks string
		fallback, general            bool
	}{
		{name: "plain", grammar: `type W terminal
rule main W = part
rule part W = "a"`, input: "a", checks: `expectedEvents:=[]TraceEvent{{Kind:TraceEnter,Rule:"main",Depth:1},{Kind:TraceEnter,Rule:"part",Depth:2},{Kind:TraceExit,Rule:"part",Depth:2,Matched:true,End:1,Evals:1},{Kind:TraceExit,Rule:"main",Depth:1,Matched:true,End:1,Evals:1}};if !reflect.DeepEqual(events,expectedEvents){t.Fatal("exact plain calls",events)}`},
		{name: "lean", grammar: `type W terminal
rule main W = -space part
rule space = " "
rule part W = "a"`, input: " a", checks: `if count("space",TraceEnter)!=1 || count("part",TraceEnter)!=1 {t.Fatal(events)}`},
		{name: "memo", grammar: `type W terminal
rule main W = &part &part &part p:part => $p
rule part W = letter
rule letter = "a"`, input: "a", checks: `hits:=0;for _,e:=range events {if e.Memo {hits++}};if hits==0 {t.Fatal("missing memo",events)}`},
		{name: "framed", grammar: `type W terminal
type N = { Value W }
rule main N = p:part => N{Value:$p}
rule part W = "a"`, input: "a", checks: `if count("part",TraceEnter)!=1 {t.Fatal(events)}`},
		{name: "general", grammar: `type W terminal
rule main W = {x:=1} #cut p:part {x==1} => $p
rule part W = "a"`, input: "a", general: true, checks: `if count("part",TraceEnter)!=1 {t.Fatal(events)}`},
		{name: "leftrec", grammar: `type W terminal
rule main W = expr
rule expr W = expr "a" / "a"`, input: "aaa", checks: `growth:=false;for _,e:=range events {if e.Rule=="expr"&&e.Kind==TraceExit&&e.Evals>1 {growth=true}};if !growth {t.Fatal("missing growth",events)}`},
		{name: "dispatch", grammar: `type W terminal
rule main W = first / second
rule first W = "a"
rule second W = "b"`, input: "b", checks: `if count("first",TraceEnter)!=0 || count("first",TraceSkipped)!=1 {t.Fatal(events)}`},
		{name: "recovery", grammar: `type W terminal
rule main W = part #recover(skip="!")
rule part W = "a"`, input: "!", checks: `if count("part",TraceEnter)!=1 {t.Fatal(events)}`},
		{name: "operators", grammar: `type W terminal
type B = { Left Expr; Right Expr }
type Expr ~ W | B
rule main Expr = l:level(expr, product) "+" r:expr => B{Left:$l,Right:$r}
rule expr Expr = operators {operand num;level sum {infix left "+" => B{Left:$lhs,Right:$rhs}};level product {infix left "*" => B{Left:$lhs,Right:$rhs}}}
rule num W = [0-9]+`, input: "1+2*3", checks: `nested:=false;for _,e:=range events {if e.Rule=="expr"&&e.Kind==TraceEnter&&e.Level>0 {nested=true}};if !nested {t.Fatal("missing level",events)}`},
		{name: "mixed", grammar: `type W terminal
rule main W = part
rule part W = "a"
rule cst = part ";"`, input: "a", checks: `if count("part",TraceEnter)!=1 {t.Fatal(events)}`},
		{name: "field", grammar: `type W terminal
type N = { Value W }
rule main W = n:box => $n.Value
rule box N = p:part => N{Value:$p}
rule part W = "a"`, input: "a", fallback: true, checks: `if count("box",TraceEnter)!=1 {t.Fatal(events)}`},
		{name: "unicode", grammar: `type W terminal
rule main W = part part
rule part W = "é"`, input: "éé", checks: `last:=events[len(events)-1];end:=2;if unit==Bytes {end=4};if last.End!=end {t.Fatal(last)}`},
		{name: "nilvalue", grammar: `type W terminal
rule main *W = part => nil
rule part W = "a"`, input: "a", checks: `if count("part",TraceEnter)!=1 {t.Fatal(events)}`},
		{name: "collision", grammar: `type TraceKind terminal
type TraceEvent = { Value WithTrace }
type TraceEnter terminal
type TraceExit terminal
type TraceSkipped terminal
type WithTrace terminal
type WithTraceFilter terminal
type WithTraceMaxDepth terminal
type TraceEvent_ terminal
rule main TraceEvent = p:part => TraceEvent{Value:$p}
rule part WithTrace = "a"`, input: "a", checks: `if count("part",TraceEnter)!=1 {t.Fatal(events)}`},
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module typedtracefixture\n\ngo 1.27.1\n"), 0644); err != nil {
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
		g, err := syntax.Parse(f.grammar)
		if err != nil {
			t.Fatal(f.name, err)
		}
		for _, convert := range []bool{false, true} {
			pkg := f.name
			if convert {
				pkg += "_converted"
			}
			opts := GenOptions{Package: "reference_" + pkg, Start: "main", Types: true, Recognize: true, convertTypes: convert, disableTypedCuts: f.general, disableTypedFrameLayouts: f.general}
			reference, err := Generate(g, opts)
			if err != nil {
				t.Fatal(pkg, err)
			}
			write(opts.Package, "parser.go", reference)
			opts.Package, opts.Observability = pkg, true
			code, err := Generate(g, opts)
			if err != nil {
				t.Fatal(pkg, err)
			}
			if !convert && !f.fallback && !strings.Contains(string(code), "return tparse(trules[") {
				t.Fatal(pkg, "direct start unexpectedly unavailable")
			}
			if (convert || f.fallback) && !strings.Contains(string(code), "return parseConverted(rules[") {
				t.Fatal(pkg, "missing Node conversion")
			}
			write(pkg, "parser.go", code)
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
    if seenRules[r.name]{continue};seenRules[r.name]=true
    want,werr:=reference.ParseRuleAST(r.name,input,refOpts...);events=nil
    got,gerr:=ParseRuleAST(r.name,input,append(append([]ParseOption{},opts...),WithTrace(func(e TraceEvent){events=append(events,e)}))...)
    a,_:=json.Marshal(want);b,_:=json.Marshal(got);if string(a)!=string(b)||fmt.Sprint(werr)!=fmt.Sprint(gerr){t.Fatal("named typed parity",r.name,string(a),string(b),werr,gerr)}
    checkTypedTrace(t,events)
   }
  }
 }}
}
func TestTypedTraceLifecycle(t *testing.T){
 routes:=[]func(...ParseOption)(any,error){func(opts ...ParseOption)(any,error){return ParseAST(INPUT,opts...)},func(opts ...ParseOption)(any,error){return ParseRuleAST("main",INPUT,opts...)}}
 for _,parse:=range routes{
  marker:=&struct{n int}{11}
  for _,value:=range []any{marker,fatal{errors.New("private marker")},evalError{msg:"private marker"},contextPanic{marker},tracePanic{marker}}{func(){defer func(){if got:=recover();got!=value{t.Fatalf("typed panic %T %v want %T %v",got,got,value,value)}}();parse(WithTrace(func(TraceEvent){panic(value)}));t.Fatal("panic absent")}()}
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
 p:=&tparser{parser:&parser{observation:&observation{trace:func(TraceEvent){}}}};p.recycle();if p.observation!=nil{t.Fatal("typed pool retained observer")}
}
`

func TestGeneratedTracePrivateTypeNamesRemainInvalid(t *testing.T) {
	for _, name := range []string{"observation", "observationCall", "tracePanic"} {
		g, err := syntax.Parse("type " + name + " terminal\nrule main = \"a\"")
		if err != nil {
			t.Fatal(err)
		}
		for _, observed := range []bool{false, true} {
			if _, err := Generate(g, GenOptions{Package: "fixture", Start: "main", Types: true, Observability: observed}); err == nil || !strings.Contains(err.Error(), "must start with an uppercase letter") {
				t.Fatalf("%s observed=%v: %v", name, observed, err)
			}
		}
	}
}
