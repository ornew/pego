package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedDocumentObservation(t *testing.T) {
	needsWorkControls(t)
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
	// The runtime reports the teardown of each invocation and the frontier of each memo entry it
	// keeps or reuses to hooks that the consumers set.
	write("go.mod", testGoModAt("documentobserve", observedRuntime(t)))
	for _, f := range []struct{ name, grammar, input string }{
		{"plain", "type W = { Text string }\nrule main W = t:(part part part) #atomic => W{Text: text($t)}\nrule part W = t:([a-b]) #atomic => W{Text: text($t)}", "aaa"},
		{"list", "type W = { Text string }\nrule main []W = p:part+ => $p.Value\nrule part W = t:([a-b]) #atomic => W{Text: text($t)}", "aaa"},
		{"predicate", "type W = { Text string }\nvar x int\nrule main W = {x:=1} &part p:part {x==1} => $p\nrule part W = t:([a-z]) #atomic => W{Text: text($t)}", "a"},
		{"framed", "type W = { Text string }\ntype N = { Value W }\nrule main N = p:part => N{Value:$p}\nrule part W = t:([a-z]+) #atomic => W{Text: text($t)}", "abc"},
		{"memo", "type W = { Text string }\nrule main W = &part &part &part p:part => $p\nrule part W = t:(letter) #atomic => W{Text: text($t)}\nrule letter = \"a\"", "a"},
		{"leftrec", "type W = { Text string }\nrule main W = t:(expr) #atomic => W{Text: text($t)}\nrule expr W = t:(expr \"a\" / \"a\") #atomic => W{Text: text($t)}", "aaa"},
		{"dispatch", "type W = { Text string }\nrule main W = t:(first / second) #atomic => W{Text: text($t)}\nrule first W = t:(\"a\") #atomic => W{Text: text($t)}\nrule second W = t:(\"b\") #atomic => W{Text: text($t)}", "b"},
		{"recover", "type W = { Text string }\nrule main W = t:((first / second) #recover(skip=\"!\")) #atomic => W{Text: text($t)}\nrule first W = t:(\"a\") #atomic => W{Text: text($t)}\nrule second W = t:(\"b\") #atomic => W{Text: text($t)}", "!"},
		{"operators", "type W = { Text string }\ntype B = { Left Expr; Right Expr }\ntype Expr ~ W | B\nrule main Expr = l:level(expr, product) \"+\" r:expr => B{Left:$l,Right:$r}\nrule expr Expr = operators {operand num;level sum {infix left \"+\" => B{Left:$lhs,Right:$rhs}};level product {infix left \"*\" => B{Left:$lhs,Right:$rhs}}}\nrule num W = t:([0-9]+) #atomic => W{Text: text($t)}", "1+2*3"},
		{"unicode", "type W = { Text string }\nrule main W = t:(part part part) #atomic => W{Text: text($t)}\nrule part W = t:(\"é\") #atomic => W{Text: text($t)}", "ééé"},
		{"integer", "type N = { Value int }\nrule main N = part => N{Value:7}\nrule part = \"a\"", "a"},
		{"boolean", "type N = { Value bool }\nrule main N = part => N{Value:true}\nrule part = \"a\"", "a"},
		{"string", "type N = { Value string }\nrule main N = part => N{Value:\"hello\"}\nrule part = \"a\"", "a"},
		{"nilvalue", "type W = { Text string }\nrule main *W = part => nil\nrule part W = t:(\"a\") #atomic => W{Text: text($t)}", "a"},
		// The start rule examines the whole input before calling part, whose own
		// frontier stays near its match.
		{"lookahead", "type W = { Text string }\nrule main W = &\"abcd\" p:part \"bcd\" => $p\nrule part W = t:(\"a\") #atomic => W{Text: text($t)}", "abcd"},
		// inner's memo result depends on its lookahead, which nothing after part reads again.
		{"innerlookahead", "type W = { Text string }\nrule main W = p:inner \"bcd\" => $p\nrule inner W = &\"abcd\" p:part => $p\nrule part W = t:(\"a\") #atomic => W{Text: text($t)}", "abcd"},
	} {
		g, err := parseTest(f.grammar)
		if err != nil {
			t.Fatal(f.name, err)
		}
		for _, route := range []string{"node", "direct", "convert"} {
			name := f.name + "_" + route
			options := GenOptions{Package: "reference", Start: "main", Document: true, Types: route != "node", Recognize: true, convertTypes: route == "convert"}
			ref, err := generateCode(g, options)
			if err != nil {
				t.Fatal(name, err)
			}
			write(name+"/reference/parser.go", string(ref))
			options.Package = name
			options.Observability = true
			code, err := generateCode(g, options)
			if err != nil {
				t.Fatal(name, err)
			}
			again, err := generateCode(g, options)
			if err != nil || string(code) != string(again) {
				t.Fatal(name, "nondeterministic source", err)
			}
			write(name+"/parser.go", string(code))
			typed := ""
			if route != "node" {
				typed = `routes=append(routes,func(d *Document,opts ...ParseOption)(any,error){return d.ParseAST(opts...) });referenceRoutes=append(referenceRoutes,func(d *reference.Document)(any,error){return d.ParseAST()})`
			}
			consumer := strings.NewReplacer("PACKAGE", name, "INPUT", fmt.Sprintf("%q", f.input), "TYPED", typed, "PLAIN", fmt.Sprint(f.name == "plain"), "LOOKAHEAD", fmt.Sprint(f.name == "lookahead")).Replace(generatedDocumentObservationConsumer)
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

import pegort "github.com/ornew/pego/runtime"

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
var documentParserProbes []*pegort.X_observation
var documentProbeMu sync.Mutex
// frontierHook receives the frontier kept by each new memo entry, or reused by a memo hit, just before the call's exit.
var frontierHook func(rule string,pos,level,examined int,hit bool)
func init(){pegort.ProbeHook=recordDocumentProbe;pegort.FrontierHook=recordFrontier}
func recordFrontier(rule string,pos,level,examined int,hit bool){if frontierHook!=nil{frontierHook(rule,pos,level,examined,hit)}}
type keptFrontier struct{at int;event pegort.TraceEvent}
// checkFrontiers requires each exit that recorded a memo entry to report the entry's frontier.
var checkedFrontiers int
// wantFrontier is the frontier an exit reports for a stored one: a hit still counts one position past its start,
// which only a growing left-recursive seed's stored frontier can lack. A matched call in a provisional context
// may instead report its end.
func wantFrontier(e pegort.TraceEvent,stored int)int{if e.Memo{return max(stored,e.Pos+1)};return stored}
func checkFrontiers(t *testing.T,events []pegort.TraceEvent,kept []keptFrontier){t.Helper();checkedFrontiers+=len(kept);for _,k:=range kept{if k.at>=len(events){t.Fatal("frontier without exit",k)};e:=events[k.at];if e.Kind!=pegort.TraceExit||e.Rule!=k.event.Rule||e.Pos!=k.event.Pos||e.Level!=k.event.Level||e.Memo!=k.event.Memo||!e.ExaminedKnown||e.Examined!=wantFrontier(e,k.event.Examined)&&!(e.Matched&&e.Examined==max(wantFrontier(e,k.event.Examined),e.End)){t.Fatal("reported examined frontier differs from the kept one",e,k)}}}
func recordDocumentProbe(o *pegort.X_observation){documentProbeMu.Lock();defer documentProbeMu.Unlock();documentParserProbes=append(documentParserProbes,o)}
func released(t *testing.T){t.Helper();documentProbeMu.Lock();defer documentProbeMu.Unlock();for _,o:=range documentParserProbes{if o!=nil&&(o.X_trace!=nil||o.X_profile!=nil||o.X_rules!=nil||len(o.X_stack)!=0){t.Fatal("observer references retained",o)}};documentParserProbes=nil}
func documentEqual(t *testing.T,a,b any){t.Helper();x,err:=json.Marshal(a);if err!=nil{t.Fatal(err)};y,err:=json.Marshal(b);if err!=nil{t.Fatal(err)};if string(x)!=string(y){t.Fatalf("value/diagnostic mismatch\n%s\n%s",x,y)}}
func scalarProfile(s pegort.ProfileSnapshot)pegort.ProfileSnapshot{s.Time=0;for i:=range s.Rules{s.Rules[i].Time=0;s.Rules[i].SelfTime=0};return s}
func eventStats(t *testing.T,d *Document,pr *pegort.Profile,events []pegort.TraceEvent){t.Helper();s:=pr.Snapshot();checkProfile(t,s,events);if s.Parses!=1||d.Stats().Evaluated!=s.Evals||d.Stats().Reused!=s.MemoHits{t.Fatal("Document stats differ from actual calls",d.Stats(),s,events)}}
func testInput()string{return INPUT}
func TestDocumentCalls(t *testing.T){
 routes:=[]func(*Document,...ParseOption)(any,error){func(d *Document,opts ...ParseOption)(any,error){return d.Parse(opts...)}}
 referenceRoutes:=[]func(*reference.Document)(any,error){func(d *reference.Document)(any,error){return d.Parse()}}
 TYPED
 for i,parse:=range routes{for _,unit:=range []PositionUnit{CodePoints,Bytes}{
  source:=testInput();d,err:=NewDocument(source,WithUnit(unit));if err!=nil{t.Fatal(err)};ref,err:=reference.NewDocument(source,reference.WithUnit(reference.PositionUnit(unit)));if err!=nil{t.Fatal(err)}
  run:=func(warm bool){t.Helper();var profile pegort.Profile;var events []pegort.TraceEvent;var kept []keptFrontier;frontierHook=func(rule string,pos,level,examined int,hit bool){kept=append(kept,keptFrontier{len(events),pegort.TraceEvent{Rule:rule,Pos:pos,Level:level,Examined:examined,Memo:hit}})};v,gotErr:=parse(d,pegort.WithProfile(&profile),pegort.WithTrace(func(e pegort.TraceEvent){events=append(events,e)}));frontierHook=nil;checkFrontiers(t,events,kept);if LOOKAHEAD{for _,e:=range events{if e.Kind==pegort.TraceExit&&e.Rule=="part"&&!e.Memo&&e.Examined>e.Pos+2{t.Fatal("call frontier includes its caller's lookahead",e)}}};want,wantErr:=referenceRoutes[i](ref);documentEqual(t,v,want);documentEqual(t,gotErr,wantErr);eventStats(t,d,&profile,events);for _,e:=range events{if e.Kind==pegort.TraceExit&&!e.ExaminedKnown{t.Fatal("Document exit without examined frontier",e)}};if warm&&(len(events)!=2||events[1].Kind!=pegort.TraceExit||!events[1].Memo||events[1].Evals!=0||profile.Snapshot().Calls!=1){t.Fatal("warm parse replayed old calls",events,profile.Snapshot())};released(t)}
  run(false);run(true)
  _,size:=utf8.DecodeLastRuneInString(source);start,end:=utf8.RuneCountInString(source)-1,utf8.RuneCountInString(source);if unit==Bytes{start,end=len(source)-size,len(source)}
  if err:=d.Edit(start,end,source[len(source)-size:]);err!=nil{t.Fatal(err)};if err:=ref.Edit(start,end,source[len(source)-size:]);err!=nil{t.Fatal(err)}
  run(false);if PLAIN&&d.Stats().Reused==0{t.Fatal("unchanged prefix not reused")};run(true)
  for _,text:=range []string{"\x00",source}{n:=utf8.RuneCountInString(d.Text());if unit==Bytes{n=len(d.Text())};if err:=d.Edit(0,n,text);err!=nil{t.Fatal(err)};if err:=ref.Edit(0,n,text);err!=nil{t.Fatal(err)};run(false);run(true)}
  var warmProfile pegort.Profile;var warmEvents []pegort.TraceEvent;parse(d,pegort.WithProfile(&warmProfile),pegort.WithTrace(func(e pegort.TraceEvent){warmEvents=append(warmEvents,e)}),WithWorkLimits(WorkLimits{MaxSteps:100000}));eventStats(t,d,&warmProfile,warmEvents)
  for _,marker:=range []any{new(int),pegort.X_fatal{errors.New("private fatal")},pegort.X_evalError{"private eval"},pegort.X_contextPanic{new(int)},pegort.X_tracePanic{new(int)}}{
  for _,controlled:=range []bool{false,true}{for _,kind:=range []pegort.TraceKind{pegort.TraceEnter,pegort.TraceExit}{
   var interrupted pegort.Profile;opts:=[]ParseOption{pegort.WithProfile(&interrupted),pegort.WithTrace(func(e pegort.TraceEvent){if e.Kind==kind{panic(marker)}})};if controlled{opts=append(opts,WithWorkLimits(WorkLimits{MaxSteps:100000}))}
   func(){defer func(){if recover()!=marker{t.Fatal("callback panic changed")}}();parse(d,opts...);t.Fatal("callback did not panic")}();a:=interrupted.Snapshot();if a.Entered!=1||kind==pegort.TraceEnter&&a.Calls!=0||kind==pegort.TraceExit&&a.Calls!=1{t.Fatal("aborted counters",a)};released(t)
   var retry pegort.Profile;var events []pegort.TraceEvent;parse(d,pegort.WithProfile(&retry),pegort.WithTrace(func(e pegort.TraceEvent){events=append(events,e)}));eventStats(t,d,&retry,events);if events[len(events)-1].Memo{t.Fatal("panic retained root memo",events)}
  }}}
  ctx,cancel:=context.WithCancel(context.Background());v,err:=parse(d,WithContext(ctx),pegort.WithTrace(func(e pegort.TraceEvent){if e.Kind==pegort.TraceEnter{cancel()}}));if v!=nil&&!reflect.ValueOf(v).IsZero()||!errors.Is(err,context.Canceled){t.Fatal("cancellation result",v,err)};released(t)
  var retry pegort.Profile;var events []pegort.TraceEvent;parse(d,pegort.WithProfile(&retry),pegort.WithTrace(func(e pegort.TraceEvent){events=append(events,e)}));eventStats(t,d,&retry,events);if events[len(events)-1].Memo{t.Fatal("cancel retained root memo",events)}
  before:=d.Stats();ctx,cancel=context.WithCancel(context.Background());cancel();parse(d,WithContext(ctx),pegort.WithTrace(func(pegort.TraceEvent){t.Fatal("pre-canceled event")}));if d.Stats()!=before{t.Fatal("pre-admission canceled invocation changed stats")}
  released(t)
  length:=utf8.RuneCountInString(d.Text());if unit==Bytes{length=len(d.Text())};if err:=d.Edit(0,length,d.Text());err!=nil{t.Fatal(err)}
  v,err=parse(d,WithWorkLimits(WorkLimits{MaxSteps:1}),pegort.WithTrace(func(pegort.TraceEvent){}));var interrupted *WorkLimitError;if v!=nil&&!reflect.ValueOf(v).IsZero()||!errors.As(err,&interrupted)||interrupted.Kind!=StepWorkLimit{t.Fatal("work budget result",v,err)};released(t)
  retry=pegort.Profile{};events=nil;parse(d,pegort.WithProfile(&retry),pegort.WithTrace(func(e pegort.TraceEvent){events=append(events,e)}));eventStats(t,d,&retry,events);if events[len(events)-1].Memo{t.Fatal("work interruption retained root memo",events)};released(t)
 }}
 if checkedFrontiers==0{t.Fatal("no memo frontier checked")}
}
// Observation must not change which results a Document keeps across edits.
func TestDocumentObservationKeepsReuse(t *testing.T){
 routes:=[]func(*Document,...ParseOption)(any,error){func(d *Document,opts ...ParseOption)(any,error){return d.Parse(opts...)}}
 referenceRoutes:=[]func(*reference.Document)(any,error){func(d *reference.Document)(any,error){return d.Parse()}}
 TYPED
 _=referenceRoutes
 for _,parse:=range routes{for _,unit:=range []PositionUnit{CodePoints,Bytes}{
  source:=testInput();observed,err:=NewDocument(source,WithUnit(unit));if err!=nil{t.Fatal(err)};plain,err:=NewDocument(source,WithUnit(unit));if err!=nil{t.Fatal(err)}
  step:=func(){t.Helper();var profile pegort.Profile;a,errA:=parse(observed,pegort.WithProfile(&profile),pegort.WithTrace(func(pegort.TraceEvent){}));b,errB:=parse(plain);documentEqual(t,a,b);documentEqual(t,errA,errB);if observed.Stats()!=plain.Stats(){t.Fatal("observation changed reuse",observed.Stats(),plain.Stats())}}
  step()
  var positions []int;for i:=range source{positions=append(positions,i)};positions=append(positions,len(source))
  for j,k:=range positions{if unit==CodePoints{k=j};if err:=observed.Edit(k,k,"");err!=nil{t.Fatal(err)};if err:=plain.Edit(k,k,"");err!=nil{t.Fatal(err)};step()}
 }}
 released(t)
}
func TestDocumentDefaultsAndIsolation(t *testing.T){
 var defaults pegort.Profile;var defaultEvents []pegort.TraceEvent;d,err:=NewDocument(testInput(),pegort.WithProfile(&defaults),pegort.WithTrace(func(e pegort.TraceEvent){defaultEvents=append(defaultEvents,e)}));if err!=nil{t.Fatal(err)}
 if defaults.Snapshot().Parses!=0||len(defaultEvents)!=0{t.Fatal("construction emitted calls")};d.Parse();if defaults.Snapshot().Parses!=1{t.Fatal("default profile unused")};n:=len(defaultEvents)
 d.Parse(pegort.WithProfile(nil),pegort.WithTrace(nil));if defaults.Snapshot().Parses!=1||len(defaultEvents)!=n{t.Fatal("invocation override ignored")};d.Parse();if defaults.Snapshot().Parses!=2||len(defaultEvents)<=n{t.Fatal("defaults lost")};released(t)
 makeDoc:=func()*Document{d,err:=NewDocument(testInput());if err!=nil{t.Fatal(err)};return d}
 var unfiltered pegort.Profile;makeDoc().Parse(pegort.WithProfile(&unfiltered));for _,opts:=range [][]ParseOption{{pegort.WithTraceFilter("unknown")},{pegort.WithTraceMaxDepth(1)},{pegort.WithTraceFilter("main")}}{var filtered pegort.Profile;opts=append(opts,pegort.WithProfile(&filtered),pegort.WithTrace(func(pegort.TraceEvent){}));makeDoc().Parse(opts...);if !reflect.DeepEqual(scalarProfile(unfiltered.Snapshot()),scalarProfile(filtered.Snapshot())){t.Fatal("profile depends on trace filter")}};released(t)
 var nested pegort.Profile;entered:=false;makeDoc().Parse(pegort.WithProfile(&nested),pegort.WithTrace(func(pegort.TraceEvent){if !entered{entered=true;other:=makeDoc();other.Parse(pegort.WithProfile(&nested));Parse(testInput(),pegort.WithProfile(&nested))}}));if nested.Snapshot().Parses!=3{t.Fatal("independent reentry lost")};for _,r:=range nested.Snapshot().Rules{if r.ExaminedKnown||r.WastedKnown{t.Fatal("batch calls left examined input known",r)}};released(t)
}
func TestDocumentReentryRejected(t *testing.T){
 routes:=[]func(*Document,...ParseOption)(any,error){func(d *Document,opts ...ParseOption)(any,error){return d.Parse(opts...)}}
 referenceRoutes:=[]func(*reference.Document)(any,error){func(d *reference.Document)(any,error){return d.Parse()}}
 TYPED
 ref,err:=reference.NewDocument(testInput());if err!=nil{t.Fatal(err)}
 for i,parse:=range routes{
  for _,kind:=range []pegort.TraceKind{pegort.TraceEnter,pegort.TraceExit}{
  d,err:=NewDocument(testInput());if err!=nil{t.Fatal(err)}
  fresh,err:=NewDocument(testInput());if err!=nil{t.Fatal(err)};parse(fresh)
  var inner []error;text:=d.Text()
  v,gotErr:=parse(d,pegort.WithTrace(func(e pegort.TraceEvent){if e.Kind!=kind||len(inner)!=0{return};for _,again:=range routes{_,err:=again(d);inner=append(inner,err);_,err=again(d,WithMaxDepth(-1));inner=append(inner,err)};inner=append(inner,d.Edit(0,0,"x"))}))
  if len(inner)!=2*len(routes)+1{t.Fatal("callback did not run")}
  for _,err:=range inner{if !errors.Is(err,ErrDocumentBusy){t.Fatal("same-Document reentry was not rejected",err)}}
  want,wantErr:=referenceRoutes[i](ref);documentEqual(t,v,want);documentEqual(t,gotErr,wantErr)
  if d.Text()!=text{t.Fatal("rejected edit changed the text")}
  if d.Stats()!=fresh.Stats(){t.Fatal("rejected calls changed statistics",d.Stats(),fresh.Stats())}
  for j,again:=range routes{v,err:=again(d);want,wantErr:=referenceRoutes[j](ref);documentEqual(t,v,want);documentEqual(t,err,wantErr)}
  if err:=d.Edit(0,0,"");err!=nil{t.Fatal("edit after parse",err)}
  func(){defer func(){recover()}();parse(d,pegort.WithTrace(func(pegort.TraceEvent){panic("stop")}))}()
  if _,err:=parse(d);errors.Is(err,ErrDocumentBusy){t.Fatal("callback panic left the Document busy")}
  if err:=d.Edit(0,0,"");err!=nil{t.Fatal(err)};parse(d,WithWorkLimits(WorkLimits{MaxSteps:1}))
  if _,err:=parse(d);errors.Is(err,ErrDocumentBusy){t.Fatal("work interruption left the Document busy")}
  }
 }
 released(t)
}
func TestDocumentConcurrentIsolation(t *testing.T){var wg sync.WaitGroup;for range 4{wg.Go(func(){d,err:=NewDocument(testInput());if err!=nil{t.Error(err);return};var profile pegort.Profile;d.Parse(pegort.WithProfile(&profile));d.Parse(pegort.WithProfile(&profile));if profile.Snapshot().Parses!=2{t.Error("independent profile not accumulated")}})};wg.Wait();released(t)}
`

// observedRuntime returns a copy of the runtime that calls ProbeHook when an invocation starts
// observing and FrontierHook with the frontier each memo entry keeps or reuses.
func observedRuntime(t *testing.T) string {
	t.Helper()
	replace := func(src, old, new string, count int) string {
		if strings.Count(src, old) != count {
			t.Fatalf("runtime anchor changed: %q", old)
		}
		return strings.ReplaceAll(src, old, new)
	}
	return runtimeCopyWith(t, func(name, src string) string {
		switch name {
		case "document.go", "typed_document.go":
			return replace(src, "p.Observation = newObservation(&o.observe)\n\tdefer p.releaseObservation()",
				"p.Observation = newObservation(&o.observe)\n\tif ProbeHook != nil {\n\t\tProbeHook(p.Observation)\n\t}\n\tdefer p.releaseObservation()", 1)
		case "runtime.go":
			src = replace(src, "\t\te.examined = p.docHigh\n", "\t\te.examined = p.docHigh\n\t\tFrontierHook(r.Name, start, min, p.docHigh, false)\n", 1)
			return replace(src, "p.documentTouch(e.examined)\n", "p.documentTouch(e.examined)\n\t\t\tFrontierHook(r.Name, start, min, e.examined, true)\n", 1)
		case "typed.go":
			src = replace(src, "\tp.TypedDocument.meta[e] = typedDocumentMeta{examined: p.docHigh,", "\tFrontierHook(r.Name, start, min, p.docHigh, false)\n\tp.TypedDocument.meta[e] = typedDocumentMeta{examined: p.docHigh,", 1)
			return replace(src, "p.documentTouch(m.examined)\n", "p.documentTouch(m.examined)\n\t\tFrontierHook(r.Name, start, min, m.examined, true)\n", 1)
		}
		return src
	}, map[string]string{"zz_hooks.go": `package runtime

var ProbeHook func(*observation)
var FrontierHook = func(rule string, pos, level, examined int, hit bool) {}
`})
}
