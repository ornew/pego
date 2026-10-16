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

func TestGeneratedDocumentObservation(t *testing.T) {
	if testing.Short() {
		t.Skip("builds standalone generated Document observation consumers")
	}
	dir := t.TempDir()
	write := func(name, data string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module documentobserve\n\ngo 1.27.1\n")
	for _, f := range []struct{ name, grammar, input string }{
		{"plain", "type W terminal\nrule main W = part part part\nrule part W = [a-b]", "aaa"},
		{"list", "type W terminal\nrule main []W = part+\nrule part W = [a-b]", "aaa"},
		{"predicate", "type W terminal\nrule main W = {x:=1} &part p:part {x==1} => $p\nrule part W = [a-z]", "a"},
		{"framed", "type W terminal\ntype N struct { Value W }\nrule main N = p:part => N{Value:$p}\nrule part W = [a-z]+", "abc"},
		{"memo", "type W terminal\nrule main W = &part &part &part p:part => $p\nrule part W = letter\nrule letter = \"a\"", "a"},
		{"leftrec", "type W terminal\nrule main W = expr\nrule expr W = expr \"a\" / \"a\"", "aaa"},
		{"dispatch", "type W terminal\nrule main W = first / second\nrule first W = \"a\"\nrule second W = \"b\"", "b"},
		{"recover", "type W terminal\nrule main W = (first / second) #recover(skip=\"!\")\nrule first W = \"a\"\nrule second W = \"b\"", "!"},
		{"operators", "type W terminal\ntype B struct { Left Expr; Right Expr }\ntype Expr = W | B\nrule main Expr = l:level(expr, product) \"+\" r:expr => B{Left:$l,Right:$r}\nrule expr Expr = operators {operand num;level sum {infix left \"+\" => B{Left:$lhs,Right:$rhs}};level product {infix left \"*\" => B{Left:$lhs,Right:$rhs}}}\nrule num W = [0-9]+", "1+2*3"},
		{"unicode", "type W terminal\nrule main W = part part part\nrule part W = \"é\"", "ééé"},
		{"integer", "type N struct { Value int }\nrule main N = part => N{Value:7}\nrule part = \"a\"", "a"},
		{"boolean", "type N struct { Value bool }\nrule main N = part => N{Value:true}\nrule part = \"a\"", "a"},
		{"string", "type N struct { Value string }\nrule main N = part => N{Value:\"hello\"}\nrule part = \"a\"", "a"},
		{"nilvalue", "type W terminal\nrule main *W = part => nil\nrule part W = \"a\"", "a"},
	} {
		g, err := syntax.Parse(f.grammar)
		if err != nil {
			t.Fatal(f.name, err)
		}
		for _, route := range []string{"node", "direct", "convert"} {
			name := f.name + "_" + route
			options := GenOptions{Package: "reference", Start: "main", Document: true, Types: route != "node", Recognize: true, convertTypes: route == "convert"}
			ref, err := Generate(g, options)
			if err != nil {
				t.Fatal(name, err)
			}
			write(name+"/reference/parser.go", string(ref))
			options.Package = name
			options.Observability = true
			code, err := Generate(g, options)
			if err != nil {
				t.Fatal(name, err)
			}
			again, err := Generate(g, options)
			if err != nil || string(code) != string(again) {
				t.Fatal(name, "nondeterministic source", err)
			}
			// Keep invocation teardown observable without exporting parser internals.
			instrumented := strings.ReplaceAll(string(code), "p.observation = newDocumentObservation(o)", "p.observation = newDocumentObservation(o)\n recordDocumentProbe(p.observation)")
			write(name+"/parser.go", instrumented)
			typed := ""
			if route != "node" {
				typed = `routes=append(routes,func(d *Document,opts ...ParseOption)(any,error){return d.ParseAST(opts...) });referenceRoutes=append(referenceRoutes,func(d *reference.Document)(any,error){return d.ParseAST()})`
			}
			consumer := strings.NewReplacer("PACKAGE", name, "INPUT", fmt.Sprintf("%q", f.input), "TYPED", typed, "PLAIN", fmt.Sprint(f.name == "plain")).Replace(generatedDocumentObservationConsumer)
			write(name+"/observation_test.go", consumer+generatedProfileCheck)
		}
	}
	cmd := exec.Command("go", "test", "-p", "1", "-race", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated Document observation: %v\n%s", err, out)
	}
}

const generatedDocumentObservationConsumer = `package PACKAGE

import(
 "context"
 "encoding/json"
 "errors"
 "reflect"
 "sync"
 "testing"
 "unicode/utf8"
 reference "documentobserve/PACKAGE/reference"
)
var documentParserProbes []*observation
var documentProbeMu sync.Mutex
func recordDocumentProbe(o *observation){documentProbeMu.Lock();defer documentProbeMu.Unlock();documentParserProbes=append(documentParserProbes,o)}
func released(t *testing.T){t.Helper();documentProbeMu.Lock();defer documentProbeMu.Unlock();for _,o:=range documentParserProbes{if o!=nil&&(o.trace!=nil||o.profile!=nil||o.rules!=nil||len(o.stack)!=0){t.Fatal("observer references retained",o)}};documentParserProbes=nil}
func documentEqual(t *testing.T,a,b any){t.Helper();x,err:=json.Marshal(a);if err!=nil{t.Fatal(err)};y,err:=json.Marshal(b);if err!=nil{t.Fatal(err)};if string(x)!=string(y){t.Fatalf("value/diagnostic mismatch\n%s\n%s",x,y)}}
func scalarProfile(s ProfileSnapshot)ProfileSnapshot{s.Time=0;for i:=range s.Rules{s.Rules[i].Time=0;s.Rules[i].SelfTime=0};return s}
func eventStats(t *testing.T,d *Document,pr *Profile,events []TraceEvent){t.Helper();s:=pr.Snapshot();checkProfile(t,s,events);if s.Parses!=1||d.Stats().Evaluated!=s.Evals||d.Stats().Reused!=s.MemoHits{t.Fatal("Document stats differ from actual calls",d.Stats(),s,events)}}
func testInput()string{return INPUT}
func TestDocumentCalls(t *testing.T){
 routes:=[]func(*Document,...ParseOption)(any,error){func(d *Document,opts ...ParseOption)(any,error){return d.Parse(opts...)}}
 referenceRoutes:=[]func(*reference.Document)(any,error){func(d *reference.Document)(any,error){return d.Parse()}}
 TYPED
 for i,parse:=range routes{for _,unit:=range []Unit{CodePoints,Bytes}{
  source:=testInput();d,err:=NewDocument(source,WithUnit(unit));if err!=nil{t.Fatal(err)};ref,err:=reference.NewDocument(source,reference.WithUnit(reference.Unit(unit)));if err!=nil{t.Fatal(err)}
  run:=func(warm bool){t.Helper();var profile Profile;var events []TraceEvent;v,gotErr:=parse(d,WithProfile(&profile),WithTrace(func(e TraceEvent){events=append(events,e)}));want,wantErr:=referenceRoutes[i](ref);documentEqual(t,v,want);documentEqual(t,gotErr,wantErr);eventStats(t,d,&profile,events);if warm&&(len(events)!=2||events[1].Kind!=TraceExit||!events[1].Memo||events[1].Evals!=0||profile.Snapshot().Calls!=1){t.Fatal("warm parse replayed old calls",events,profile.Snapshot())};released(t)}
  run(false);run(true)
  _,size:=utf8.DecodeLastRuneInString(source);start,end:=utf8.RuneCountInString(source)-1,utf8.RuneCountInString(source);if unit==Bytes{start,end=len(source)-size,len(source)}
  if err:=d.Edit(start,end,source[len(source)-size:]);err!=nil{t.Fatal(err)};if err:=ref.Edit(start,end,source[len(source)-size:]);err!=nil{t.Fatal(err)}
  run(false);if PLAIN&&d.Stats().Reused==0{t.Fatal("unchanged prefix not reused")};run(true)
  for _,text:=range []string{"\x00",source}{n:=utf8.RuneCountInString(d.Text());if unit==Bytes{n=len(d.Text())};if err:=d.Edit(0,n,text);err!=nil{t.Fatal(err)};if err:=ref.Edit(0,n,text);err!=nil{t.Fatal(err)};run(false);run(true)}
  var warmProfile Profile;var warmEvents []TraceEvent;parse(d,WithProfile(&warmProfile),WithTrace(func(e TraceEvent){warmEvents=append(warmEvents,e)}),WithWorkLimits(WorkLimits{MaxSteps:100000}));eventStats(t,d,&warmProfile,warmEvents)
  for _,marker:=range []any{new(int),fatal{errors.New("private fatal")},evalError{"private eval"},contextPanic{new(int)},tracePanic{new(int)}}{
  for _,controlled:=range []bool{false,true}{for _,kind:=range []TraceKind{TraceEnter,TraceExit}{
   var interrupted Profile;opts:=[]ParseOption{WithProfile(&interrupted),WithTrace(func(e TraceEvent){if e.Kind==kind{panic(marker)}})};if controlled{opts=append(opts,WithWorkLimits(WorkLimits{MaxSteps:100000}))}
   func(){defer func(){if recover()!=marker{t.Fatal("callback panic changed")}}();parse(d,opts...);t.Fatal("callback did not panic")}();a:=interrupted.Snapshot();if a.Entered!=1||kind==TraceEnter&&a.Calls!=0||kind==TraceExit&&a.Calls!=1{t.Fatal("aborted counters",a)};released(t)
   var retry Profile;var events []TraceEvent;parse(d,WithProfile(&retry),WithTrace(func(e TraceEvent){events=append(events,e)}));eventStats(t,d,&retry,events);if events[len(events)-1].Memo{t.Fatal("panic retained root memo",events)}
  }}}
  ctx,cancel:=context.WithCancel(context.Background());v,err:=parse(d,WithContext(ctx),WithTrace(func(e TraceEvent){if e.Kind==TraceEnter{cancel()}}));if v!=nil&&!reflect.ValueOf(v).IsZero()||!errors.Is(err,context.Canceled){t.Fatal("cancellation result",v,err)};released(t)
  var retry Profile;var events []TraceEvent;parse(d,WithProfile(&retry),WithTrace(func(e TraceEvent){events=append(events,e)}));eventStats(t,d,&retry,events);if events[len(events)-1].Memo{t.Fatal("cancel retained root memo",events)}
  before:=d.Stats();ctx,cancel=context.WithCancel(context.Background());cancel();parse(d,WithContext(ctx),WithTrace(func(TraceEvent){t.Fatal("pre-canceled event")}));if d.Stats()!=before{t.Fatal("pre-admission canceled invocation changed stats")}
  released(t)
  length:=utf8.RuneCountInString(d.Text());if unit==Bytes{length=len(d.Text())};if err:=d.Edit(0,length,d.Text());err!=nil{t.Fatal(err)}
  v,err=parse(d,WithWorkLimits(WorkLimits{MaxSteps:1}),WithTrace(func(TraceEvent){}));var interrupted *WorkLimitError;if v!=nil&&!reflect.ValueOf(v).IsZero()||!errors.As(err,&interrupted)||interrupted.Kind!=StepWorkLimit{t.Fatal("work budget result",v,err)};released(t)
  retry=Profile{};events=nil;parse(d,WithProfile(&retry),WithTrace(func(e TraceEvent){events=append(events,e)}));eventStats(t,d,&retry,events);if events[len(events)-1].Memo{t.Fatal("work interruption retained root memo",events)};released(t)
 }}
}
func TestDocumentDefaultsAndIsolation(t *testing.T){
 var defaults Profile;var defaultEvents []TraceEvent;d,err:=NewDocument(testInput(),WithProfile(&defaults),WithTrace(func(e TraceEvent){defaultEvents=append(defaultEvents,e)}));if err!=nil{t.Fatal(err)}
 if defaults.Snapshot().Parses!=0||len(defaultEvents)!=0{t.Fatal("construction emitted calls")};d.Parse();if defaults.Snapshot().Parses!=1{t.Fatal("default profile unused")};n:=len(defaultEvents)
 d.Parse(WithProfile(nil),WithTrace(nil));if defaults.Snapshot().Parses!=1||len(defaultEvents)!=n{t.Fatal("invocation override ignored")};d.Parse();if defaults.Snapshot().Parses!=2||len(defaultEvents)<=n{t.Fatal("defaults lost")};released(t)
 makeDoc:=func()*Document{d,err:=NewDocument(testInput());if err!=nil{t.Fatal(err)};return d}
 var unfiltered Profile;makeDoc().Parse(WithProfile(&unfiltered));for _,opts:=range [][]ParseOption{{WithTraceFilter("unknown")},{WithTraceMaxDepth(1)},{WithTraceFilter("main")}}{var filtered Profile;opts=append(opts,WithProfile(&filtered),WithTrace(func(TraceEvent){}));makeDoc().Parse(opts...);if !reflect.DeepEqual(scalarProfile(unfiltered.Snapshot()),scalarProfile(filtered.Snapshot())){t.Fatal("profile depends on trace filter")}};released(t)
 var nested Profile;entered:=false;makeDoc().Parse(WithProfile(&nested),WithTrace(func(TraceEvent){if !entered{entered=true;other:=makeDoc();other.Parse(WithProfile(&nested));Parse(testInput(),WithProfile(&nested))}}));if nested.Snapshot().Parses!=3{t.Fatal("independent reentry lost")};released(t)
}
func TestDocumentReentryRejected(t *testing.T){
 routes:=[]func(*Document,...ParseOption)(any,error){func(d *Document,opts ...ParseOption)(any,error){return d.Parse(opts...)}}
 referenceRoutes:=[]func(*reference.Document)(any,error){func(d *reference.Document)(any,error){return d.Parse()}}
 TYPED
 ref,err:=reference.NewDocument(testInput());if err!=nil{t.Fatal(err)}
 for i,parse:=range routes{
  for _,kind:=range []TraceKind{TraceEnter,TraceExit}{
  d,err:=NewDocument(testInput());if err!=nil{t.Fatal(err)}
  fresh,err:=NewDocument(testInput());if err!=nil{t.Fatal(err)};parse(fresh)
  var inner []error;text:=d.Text()
  v,gotErr:=parse(d,WithTrace(func(e TraceEvent){if e.Kind!=kind||len(inner)!=0{return};for _,again:=range routes{_,err:=again(d);inner=append(inner,err);_,err=again(d,WithMaxDepth(-1));inner=append(inner,err)};inner=append(inner,d.Edit(0,0,"x"))}))
  if len(inner)!=2*len(routes)+1{t.Fatal("callback did not run")}
  for _,err:=range inner{if !errors.Is(err,ErrDocumentBusy){t.Fatal("same-Document reentry was not rejected",err)}}
  want,wantErr:=referenceRoutes[i](ref);documentEqual(t,v,want);documentEqual(t,gotErr,wantErr)
  if d.Text()!=text{t.Fatal("rejected edit changed the text")}
  if d.Stats()!=fresh.Stats(){t.Fatal("rejected calls changed statistics",d.Stats(),fresh.Stats())}
  for j,again:=range routes{v,err:=again(d);want,wantErr:=referenceRoutes[j](ref);documentEqual(t,v,want);documentEqual(t,err,wantErr)}
  if err:=d.Edit(0,0,"");err!=nil{t.Fatal("edit after parse",err)}
  func(){defer func(){recover()}();parse(d,WithTrace(func(TraceEvent){panic("stop")}))}()
  if _,err:=parse(d);errors.Is(err,ErrDocumentBusy){t.Fatal("callback panic left the Document busy")}
  if err:=d.Edit(0,0,"");err!=nil{t.Fatal(err)};parse(d,WithWorkLimits(WorkLimits{MaxSteps:1}))
  if _,err:=parse(d);errors.Is(err,ErrDocumentBusy){t.Fatal("work interruption left the Document busy")}
  }
 }
 released(t)
}
func TestDocumentConcurrentIsolation(t *testing.T){var wg sync.WaitGroup;for range 4{wg.Go(func(){d,err:=NewDocument(testInput());if err!=nil{t.Error(err);return};var profile Profile;d.Parse(WithProfile(&profile));d.Parse(WithProfile(&profile));if profile.Snapshot().Parses!=2{t.Error("independent profile not accumulated")}})};wg.Wait();released(t)}
`
