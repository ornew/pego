package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
)

func TestGeneratedGoProfile(t *testing.T) {
	needsWorkControls(t)
	if testing.Short() {
		t.Skip("builds standalone generated profiling consumers")
	}
	dir := t.TempDir()
	write := func(name string, data []byte) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", []byte(testGoMod("profilefixture")))
	for _, f := range []struct{ name, grammar, input, checks string }{
		{"plain", "type N = { Text string }\nrule main N = t:(part part) #atomic => N{Text: text($t)}\nrule part = \"a\"", "aa", `if s.Entered!=3||s.Calls!=3||s.Evals!=3||s.Skipped!=0{t.Fatal(s)};for _,r:=range s.Rules{if r.Rule=="part"&&(r.Calls!=2||r.Consumed!=2){t.Fatal(r)}}`},
		{"dispatch", "rule main = first / second\nrule first = \"a\"\nrule second = \"b\"", "b", `if s.Skipped!=1||s.Entered!=2{t.Fatal(s)};for _,r:=range s.Rules{if r.Rule=="first"&&(r.Calls!=0||r.Skipped!=1||len(r.SkipReasons)!=1||r.SkipReasons[0].Reason!="first-character"){t.Fatal(r)}}`},
		{"memo", "type N = { Text string }\nrule main = &part &part &part part\nrule part N = t:(letter) #atomic => N{Text: text($t)}\nrule letter = \"a\"", "a", `if s.MemoHits==0{t.Fatal("missing memo",s)}`},
		{"leftrec", "rule main = expr\nrule expr = expr \"a\" / \"a\"", "aaa", `if s.Evals<=s.Calls-s.MemoHits{t.Fatal("missing growth",s)}`},
		{"operators", "rule main = level(expr, product) \"+\" expr\nrule num = [0-9]+\nrule expr = #operators {operand num;level sum {infix left \"+\"};level product {infix left \"*\"}}", "1+2*3", ``},
		{"recover", "rule main = (first / second) #recover(skip=\"!\")\nrule first = \"a\"\nrule second = \"b\"", "!", `if s.Skipped!=2{t.Fatal(s)}`},
		{"unicode", "type N = { Text string }\nrule main N = t:(part part) #atomic => N{Text: text($t)}\nrule part = \"é\"", "éé", `for _,r:=range s.Rules{if r.Rule=="main"{n:=2;if unit==Bytes{n=4};if r.Consumed!=n{t.Fatal(r)}}}`},
		{"collision", "type Profile = { Text string }\ntype ProfileSnapshot = { Text string }\ntype RuleProfile = { Text string }\ntype ProfileLocation = { Text string }\ntype SkipReasonCount = { Text string }\ntype WithProfile = { Text string }\nrule main Profile = t:(part) #atomic => Profile{Text: text($t)}\nrule part WithProfile = t:(\"a\") #atomic => WithProfile{Text: text($t)}", "a", ``},
	} {
		g, err := parseTest(f.grammar)
		if err != nil {
			t.Fatal(f.name, err)
		}
		for _, route := range []string{"node", "direct", "convert"} {
			pkg := f.name + "_" + route
			opts := gen.GenOptions{Package: pkg, Start: "main", Recognize: true, Observability: true, Types: route != "node", ConvertTypes: route == "convert"}
			generatePackageIn(t, dir, "profilefixture", pkg, g, opts)
			typed := ""
			if opts.Types {
				typed = `operations=append(operations,func(opts ...ParseOption)(any,error){return ParseAST(INPUT,opts...)},func(opts ...ParseOption)(any,error){return ParseRuleAST("main",INPUT,opts...)})`
			}
			typed = strings.ReplaceAll(typed, "INPUT", fmt.Sprintf("%q", f.input))
			consumer := strings.NewReplacer("PACKAGE", pkg, "INPUT", fmt.Sprintf("%q", f.input), "CHECKS", f.checks, "TYPED", typed, "PLAIN", fmt.Sprint(f.name == "plain")).Replace(generatedProfileConsumer)
			write(pkg+"/profile_test.go", []byte(consumer))
		}
	}
	cmd := exec.Command("go", "test", "-p", "1", "-race", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated profiles: %v\n%s", err, out)
	}
}

const generatedProfileConsumer = `package PACKAGE

import pegort "github.com/ornew/pego/runtime"
import("context";"encoding/json";"fmt";"reflect";"sync";"testing";"time")
func TestProfile(t *testing.T){
 operations:=[]func(...ParseOption)(any,error){func(opts ...ParseOption)(any,error){return Parse(INPUT,opts...)},func(opts ...ParseOption)(any,error){return ParseRule("main",INPUT,opts...)},func(opts ...ParseOption)(any,error){return nil,Recognize(INPUT,opts...)}}
 TYPED
 for _,unit:=range []PositionUnit{CodePoints,Bytes}{for op,parse:=range operations{
  var pr pegort.Profile;var events []pegort.TraceEvent
  got,err:=parse(WithUnit(unit),pegort.WithProfile(&pr),pegort.WithTrace(func(e pegort.TraceEvent){events=append(events,e)}));expected,base:=parse(WithUnit(unit));if !reflect.DeepEqual(got,expected){t.Fatal("profile changed value")}
  if fmt.Sprint(err)!=fmt.Sprint(base){t.Fatal("error changed",err,base)}
  s:=pr.Snapshot();checkProfile(t,s,events)
  if op<2{CHECKS}
  if s.Parses!=1||s.Time<=0{t.Fatal(s)}
  bytes1,_:=json.Marshal(s);bytes2,_:=json.Marshal(pr.Snapshot());if string(bytes1)!=string(bytes2){t.Fatal("unstable snapshot")}
  if len(s.Rules)>0{s.Rules[0].Rule="mutated";if len(s.Rules[0].SkipReasons)>0{s.Rules[0].SkipReasons[0].Count=999}}
  if b,_:=json.Marshal(pr.Snapshot());string(b)!=string(bytes1){t.Fatal("snapshot aliases sink")}
  before:=pr.Snapshot();parse(WithUnit(unit),pegort.WithProfile(&pr),pegort.WithTrace(func(pegort.TraceEvent){t.Fatal("filtered event")}),pegort.WithTraceFilter("absent"),pegort.WithTraceMaxDepth(1));after:=pr.Snapshot()
  if after.Parses!=2||after.Calls!=2*before.Calls||after.Evals!=2*before.Evals||after.Skipped!=2*before.Skipped{t.Fatal("filter affected profile",before,after)}
  for i,r:=range after.Rules{if r.Repeats!=2*before.Rules[i].Repeats||r.MaxEvals!=before.Rules[i].MaxEvals{t.Fatal("history crossed invocation",r,before.Rules[i])}}
  saved:=pr.Snapshot();parse(pegort.WithProfile(nil));if !reflect.DeepEqual(saved,pr.Snapshot()){t.Fatal("sink retained")}
  var reentrant pegort.Profile;once:=false;parse(pegort.WithProfile(&reentrant),pegort.WithTrace(func(pegort.TraceEvent){if !once{once=true;parse(pegort.WithProfile(&reentrant))}}));re:=reentrant.Snapshot();if re.Parses!=2||re.Calls!=2*before.Calls{t.Fatal("reentrant state",re,before)}
  var aborted pegort.Profile;marker:=new(int);func(){defer func(){if recover()!=marker{t.Fatal("panic identity")}}();parse(pegort.WithProfile(&aborted),pegort.WithTrace(func(pegort.TraceEvent){panic(marker)}));t.Fatal("missing panic")}();a:=aborted.Snapshot();if a.Parses!=1||a.Entered!=1||a.Calls!=0||a.Evals!=0||a.Time!=0{t.Fatal("fabricated completion",a)}
  parse(pegort.WithProfile(&aborted));a=aborted.Snapshot();if a.Parses!=2||a.Calls!=before.Calls||a.Entered!=before.Entered+1{t.Fatal("abort poisoned next parse",a)}
  var exitPanic pegort.Profile;func(){defer func(){if recover()!=marker{t.Fatal("exit panic identity")}}();parse(pegort.WithProfile(&exitPanic),pegort.WithTrace(func(e pegort.TraceEvent){if e.Kind==pegort.TraceExit{panic(marker)}}));t.Fatal("missing exit panic")}();if x:=exitPanic.Snapshot();x.Calls!=1{t.Fatal("exit panic lost completed call",x)}
  if PLAIN&&op==0{var timed pegort.Profile;var delay time.Duration;parse(pegort.WithProfile(&timed),pegort.WithTrace(func(e pegort.TraceEvent){if e.Rule=="part"&&e.Kind==pegort.TraceExit{start:=time.Now();time.Sleep(time.Millisecond);delay+=time.Since(start)}}));for _,r:=range timed.Snapshot().Rules{if r.Rule=="part"&&r.Time<delay{t.Fatal("child exit callback attributed outside child",r.Time,delay)}}}
  ctx,cancel:=context.WithCancel(context.Background());cancel();var canceled pegort.Profile;parse(pegort.WithProfile(&canceled),WithContext(ctx));if canceled.Snapshot().Entered!=0{t.Fatal("canceled call entered")}
  var wg sync.WaitGroup;for range 4{wg.Go(func(){var p pegort.Profile;parse(pegort.WithProfile(&p));if p.Snapshot().Parses!=1{t.Error("concurrent isolation")}})};wg.Wait()
 }}
 var rejected pegort.Profile;_,rejectedError:=Parse("\x00",pegort.WithProfile(&rejected));if rejectedError==nil{t.Fatal("invalid input accepted")};failedRoot:=false;for _,r:=range rejected.Snapshot().Rules{if r.Rule=="main"&&r.Failed==1{failedRoot=true}};if !failedRoot{t.Fatal("failed root unrecorded",rejected.Snapshot())}
 var nilProfile *pegort.Profile;if len(nilProfile.Snapshot().Rules)!=0{t.Fatal("nil snapshot")}
}
` + generatedProfileCheck

const generatedProfileCheck = `
func checkProfile(t *testing.T,s ProfileSnapshot,events []TraceEvent){
 t.Helper();entered,calls,evals,memo,skips:=0,0,0,0,0
 rows:=map[string][]TraceEvent{};for _,e:=range events{rows[e.Rule]=append(rows[e.Rule],e);switch e.Kind{case TraceEnter:entered++;case TraceExit:calls++;evals+=e.Evals;if e.Memo{memo++};case TraceSkipped:skips++}}
 if s.Entered!=entered||s.Calls!=calls||s.Evals!=evals||s.MemoHits!=memo||s.Skipped!=skips{t.Fatal("event totals",s,events)}
 if len(s.Rules)!=len(rows){t.Fatal("row count",s)}
 for i,r:=range s.Rules{if i>0&&s.Rules[i-1].Rule>=r.Rule{t.Fatal("ordering",s)};n,m,f,c,ev,mm,sk:=0,0,0,0,0,0,0;for _,e:=range rows[r.Rule]{switch e.Kind{case TraceEnter:n++;case TraceSkipped:sk++;case TraceExit:c++;ev+=e.Evals;if e.Memo{mm++};if e.Matched{m++;f+=e.End-e.Pos}}};if r.Entered!=n||r.Calls!=c||r.Matched!=m||r.Failed!=c-m||r.Consumed!=f||r.Evals!=ev||r.MemoHits!=mm||r.Skipped!=sk{t.Fatal("row counters",r,rows[r.Rule])};x,w,known:=0,0,c>0;for _,e:=range rows[r.Rule]{if e.Kind!=TraceExit{continue};if !e.ExaminedKnown{known=false;if e.Examined!=0{t.Fatal("invented examined range",e)};continue};if e.Examined<e.Pos||e.Matched&&e.Examined<e.End{t.Fatal("examined frontier before the call or its match",e)};x+=e.Examined-e.Pos;if !e.Matched&&!e.Memo{w+=e.Examined-e.Pos}};if !known{x,w=0,0};if r.ExaminedKnown!=known||r.WastedKnown!=known||r.Examined!=x||r.Wasted!=w{t.Fatal("examined range",r,known,x,w)};if r.Time<0||r.SelfTime<0||r.Time<r.SelfTime{t.Fatal("time accounting",r)}}
}
`
