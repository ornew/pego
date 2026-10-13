package engine

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// PEGO_TYPED_OUTPUT_BOUNDED_DIR exports bounded consumers without replacing the
// full/reference fixture tree. The coordinator owns timed measurements.
func TestGeneratedBoundedTypedOutputSweep(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated bounded ownership consumers")
	}
	for _, grammar := range []struct{ name, source, consumer, whitebox string }{
		{"closed", typedSweepClosedGrammar, typedSweepClosedConsumer, typedSweepClosedWhitebox + typedSweepGraphWhitebox},
		{"open", typedSweepOpenGrammar, typedSweepOpenConsumer, typedSweepOpenWhitebox},
		{"collision", typedSweepCollisionGrammar, typedSweepCollisionConsumer, "import \"testing\"\n"},
	} {
		g, err := syntax.Parse(grammar.source)
		if err != nil {
			t.Fatal(err)
		}
		for _, conversion := range []bool{false, true} {
			t.Run(grammar.name+"/"+strconv.FormatBool(conversion), func(t *testing.T) {
				opts := GenOptions{Package: "sweepfixture", Start: "main", Types: true, Recognize: true,
					convertTypes: conversion, enableBoundedTypedOutputSweep: true}
				code, err := Generate(g, opts)
				if err != nil {
					t.Fatal(err)
				}
				again, err := Generate(g, opts)
				if err != nil || string(again) != string(code) {
					t.Fatal("bounded generation changed", err)
				}
				both := opts
				both.enableTypedOutputSweep = true
				withBoth, err := Generate(g, both)
				if err != nil || string(withBoth) != string(code) {
					t.Fatal("both selectors must select the bounded implementation", err)
				}
				for _, selector := range []string{"enableTypedOutputSweep", "enableBoundedTypedOutputSweep"} {
					if strings.Contains(string(code), selector) {
						t.Fatal("generation selector leaked into output")
					}
				}
				if !strings.Contains(string(code), "astDefaultOutputBudget") || !strings.Contains(string(code), "m.complete && !m.keepAll") {
					t.Fatal("bounded-only selection omitted publication hooks")
				}
				whitebox := grammar.whitebox
				if grammar.name == "closed" {
					whitebox += boundedSweepOwnerWhitebox(t, string(code), false)
					if !conversion {
						whitebox += typedSweepDirectWhitebox + boundedSweepOwnerWhitebox(t, string(code), true)
					}
				}
				whitebox = strings.Replace(whitebox, "import \"testing\"", "import (\"reflect\"; \"testing\")", 1)
				whitebox = strings.ReplaceAll(whitebox, "astOutputMark{live:make(map[any]struct{})}", "astOutputMark{live:make(map[any]struct{}),budget:astDefaultOutputBudget()}")
				whitebox = strings.Replace(whitebox, `if m.keepAll || len(m.live)<10000 {t.Fatal("iterative DAG walk")}`,
					`if !m.keepAll || m.complete || len(m.live)>256 || cap(m.todo)>64 {t.Fatal("bounded iterative DAG fallback")}`, 1)
				whitebox += boundedSweepBudgetWhitebox
				dir := t.TempDir()
				if export := os.Getenv("PEGO_TYPED_OUTPUT_BOUNDED_DIR"); export != "" {
					dir = filepath.Join(export, grammar.name, strconv.FormatBool(conversion))
					if err := os.MkdirAll(dir, 0o755); err != nil {
						t.Fatal(err)
					}
				}
				metadata, err := json.Marshal(opts)
				if err != nil {
					t.Fatal(err)
				}
				files := map[string][]byte{
					"parser.go": code, "go.mod": []byte("module sweepfixture\n\ngo 1.27.1\n"),
					"consumer_test.go": []byte("package sweepfixture\n" + grammar.consumer + "\nconst forceConversion = " + strconv.FormatBool(conversion) + "\nconst sweepEnabled = true\n"),
					"whitebox_test.go": []byte("package sweepfixture\n" + whitebox),
					"grammar.pego":     []byte(grammar.source), "options.json": metadata,
					"variant.json": []byte(`{"conversion":` + strconv.FormatBool(conversion) + `,"bounded":true}`),
				}
				for name, data := range files {
					if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
						t.Fatal(err)
					}
				}
				cmd := exec.Command("go", "test", "-count=1", "-run", "^(TestSweep|TestBounded)")
				cmd.Dir = dir
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("generated bounded consumer: %v\n%s", err, output)
				}
			})
		}
	}
}

func boundedSweepOwnerWhitebox(t *testing.T, code string, direct bool) string {
	t.Helper()
	holder, fits, publish, name := "astConv", "a.outputFits()", "a.publish(root,err)", "Conversion"
	promote := "a.promoteScalarOwners(m)"
	matchSetup := `deadMatch:=astNew(&a.match);deadMatch.Text="discarded match"
 deadError:=astNew(&a.error);deadError.Text="discarded error"`
	matchDirs, matchAssert := "", `if deadError.Text!="discarded error" {t.Fatal("partial error clearing")}`
	if direct {
		promote = "a.promoteScalarOwners(m,p)"
		holder, fits, publish, name = "tslabs", "a.outputFits(p)", "a.publish(root,err,p)", "Direct"
		matchSetup = `p:=&tparser{};deadMatch:=p.newMatch(0,1,"discarded match",true)`
		matchDirs = `oldMatches:=p.matchBlocks`
		matchAssert = `if p.matchBlocks!=nil {t.Fatal("Match directory retained")};for _,b:=range oldMatches {if b!=nil {t.Fatal("Match directory backing retained")}}`
	}
	start := strings.Index(code, "type "+holder+" struct {")
	if start < 0 {
		t.Fatalf("missing generated %s holder", holder)
	}
	body := code[start:]
	end := strings.Index(body, "\n}")
	if end < 0 {
		t.Fatalf("missing generated %s holder end", holder)
	}
	pairChunk := ""
	for _, line := range strings.Split(body[:end], "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == "astChunk[*Pair]" {
			pairChunk = fields[0]
		}
	}
	if pairChunk == "" {
		t.Fatalf("missing generated %s Pair list owner", holder)
	}
	return strings.NewReplacer("__HOLDER__", holder, "__FITS__", fits, "__PUBLISH__", publish,
		"__PROMOTE__", promote,
		"__NAME__", name, "__MATCH_SETUP__", matchSetup, "__MATCH_DIRS__", matchDirs, "__MATCH_ASSERT__", matchAssert,
		"__PAIR_CHUNK__", pairChunk).Replace(boundedSweepOwnerTemplate)
}

const boundedSweepOwnerTemplate = `
func TestBounded__NAME__InteriorPromotionFallback(t *testing.T) {
 a:=&__HOLDER__{}
 tokens:=make([]Token,100);a.tToken.blocks=[][]Token{tokens}
 for i:=range tokens {tokens[i].Text="preserve"}
 __MATCH_SETUP__
 root:=&tokens[0].Text;var err error
 if !__FITS__ {t.Fatal("interior fixture should pass directory preflight")}
 m:=boundedMarker();m.run(root,nil)
 __PROMOTE__
 if !m.keepAll || m.complete {t.Fatal("interior promotion must exhaust work atomically")}
 __PUBLISH__
 for i:=range tokens {if tokens[i].Text!="preserve" {t.Fatal("partial clearing after promotion fallback",i)}}
 if deadMatch.Text!="discarded match" {t.Fatal("Match cleared after promotion fallback")}
 if a.tToken.blocks!=nil {t.Fatal("fallback retained ownership directory")}
}
func TestBounded__NAME__GeneratedFieldAdmission(t *testing.T) {
 for _,limit:=range []int{-1,0,1,2,7,8} {
  root:=&Box{};m:=boundedMarker();m.budget.pending=limit;m.run(root,nil)
  if limit<8 {
   if !m.keepAll || m.complete {t.Fatal("generated field push exceeded queue budget",limit)}
   if limit>=2 {if _,ok:=m.live[root];!ok {t.Fatal("field fallback did not happen after marking root")}}
  } else if m.keepAll || !m.complete {t.Fatal("exact generated field pending boundary")}
  if limit>=0 && cap(m.todo)>limit {t.Fatal("generated field push grew queue")}
 }
}
func TestBounded__NAME__AtomicFallback(t *testing.T) {
 for _,reason:=range []string{"graph", "owned slots", "owned records", "unknown error"} {
  a:=&__HOLDER__{}
  dead:=astNew(&a.tToken);dead.Text="discarded token"
  live:=astNew(&a.tToken);live.Text="live token"
  deadPair:=astNew(&a.tPair);*deadPair=Pair{Left:dead,Label:"discarded pair"}
  pair:=astNew(&a.tPair);*pair=Pair{Left:live,Right:live,Label:"live pair"}
  cells:=astSlice(&a.__PAIR_CHUNK__,2);cells[0]=deadPair;cells[1]=pair
  __MATCH_SETUP__
  root:=any(&Box{Items:cells[1:2:2]})
  var err error
  switch reason {
  case "graph":
   // The LIFO walk visits the live token before rejecting the first capacity view.
   root=&Box{Items:cells[1:2:2],Nested:[][]*Token{make([]*Token,257),{live}}}
   m:=&astOutputMark{budget:astDefaultOutputBudget()};m.run(root,nil)
   if !m.keepAll || m.complete {t.Fatal("expected graph fallback")}
   if _,ok:=m.live[live];!ok {t.Fatal("fallback did not occur after a live alias was marked")}
   if !__FITS__ {t.Fatal("graph case failed preflight")}
  case "owned slots":
   a.tToken.grow(4097,8,256);a.tToken.free[0].Text="oversized owner"
   root=live
   if __FITS__ {t.Fatal("oversized owned block passed preflight")}
  case "owned records":
   for i:=0;i<65;i++ {a.tToken.blocks=append(a.tToken.blocks,[]Token{})}
   root=live
   if __FITS__ {t.Fatal("too many owner records passed preflight")}
  case "unknown error":
   err=&boundedHiddenError{hidden:dead}
   if !__FITS__ {t.Fatal("error case failed preflight")}
  }
  dirs:=boundedDirectoryBackings(a)
  __MATCH_DIRS__
  __PUBLISH__
  if dead.Text!="discarded token" || live.Text!="live token" || deadPair.Label!="discarded pair" || deadPair.Left!=dead || pair.Left!=live || cells[0]!=deadPair || cells[1]!=pair || deadMatch.Text!="discarded match" {t.Fatal("partial object/list/Match clearing",reason)}
  if reason=="owned slots" && a.tToken.free[0].Text!="oversized owner" {t.Fatal("preflight wrote into declined owner")}
  boundedAssertDirectoriesDropped(t,a,dirs)
  __MATCH_ASSERT__
 }
}
`

const boundedSweepBudgetWhitebox = `
type boundedHiddenError struct { hidden any }
func (*boundedHiddenError) Error()string {return "opaque error"}

func boundedMarker() *astOutputMark {return &astOutputMark{budget:astDefaultOutputBudget()}}
func boundedDirectoryBackings(a any) []reflect.Value {
 v:=reflect.ValueOf(a).Elem();var out []reflect.Value
 for i:=0;i<v.NumField();i++ {f:=v.Field(i);if f.Kind()!=reflect.Struct {continue};b:=f.FieldByName("blocks");if b.IsValid() {out=append(out,b.Slice(0,b.Len()))}}
 return out
}
func boundedAssertDirectoriesDropped(t *testing.T,a any,old []reflect.Value) {
 t.Helper();v:=reflect.ValueOf(a).Elem()
 for i:=0;i<v.NumField();i++ {f:=v.Field(i);if f.Kind()!=reflect.Struct {continue};b:=f.FieldByName("blocks");if b.IsValid() && !b.IsNil() {t.Fatal("owner directory retained")}}
 for _,b:=range old {for i:=0;i<b.Len();i++ {if !b.Index(i).IsNil() {t.Fatal("owner directory backing retained")}}}
}
func TestBoundedWork(t *testing.T) {
 for _,limit:=range []int{-1,0,1,2,3} {
  m:=boundedMarker();m.budget.work=limit;m.run(nil,nil)
  if limit<2 {if !m.keepAll || m.complete {t.Fatal("work fallback",limit)};if limit<=0 && (m.todo!=nil || m.live!=nil) {t.Fatal("zero work allocated state")}} else if m.keepAll || !m.complete || m.budget.work!=limit-2 {t.Fatal("work boundary",limit)}
 }
 m:=boundedMarker();m.budget.work=1;m.run(nil,nil);m.run(nil,nil)
 if m.budget.work!=0 || !m.keepAll || m.complete {t.Fatal("run replenished exhausted budget")}
 m=boundedMarker();if m.charge(-1) || !m.keepAll {t.Fatal("negative charge")}
}
func TestBoundedKeysAndAliases(t *testing.T) {
 x,y:=7,8
 for _,limit:=range []int{-1,0,1} {
  m:=boundedMarker();m.budget.keys=limit;m.run(&x,nil)
  if limit<1 {if !m.keepAll || m.complete || m.live!=nil {t.Fatal("key fallback",limit)}} else if !m.complete || m.keepAll || len(m.live)!=1 || m.budget.keys!=0 {t.Fatal("exact key boundary")}
 }
 m:=boundedMarker();m.budget.keys=3;m.run([]*int{&x,&x},nil)
 if !m.complete || m.keepAll || len(m.live)!=3 || m.budget.keys!=0 || astDefaultOutputBudget().work-m.budget.work!=8 {t.Fatal("duplicate aliases did not consume only work")}
 m=boundedMarker();m.budget.keys=3;m.run([]*int{&x,&y},nil)
 if !m.keepAll || m.complete || len(m.live)!=3 || x!=7 || y!=8 {t.Fatal("one-over key boundary")}
}
func TestBoundedPending(t *testing.T) {
 for _,limit:=range []int{-1,0} {m:=boundedMarker();m.budget.pending=limit;if m.push(nil) || !m.keepAll || m.todo!=nil {t.Fatal("invalid pending allocated queue",limit)}}
 m:=boundedMarker()
 for i:=0;i<64;i++ {if !m.push(nil) {t.Fatal("exact pending boundary",i)}}
 if cap(m.todo)!=64 || len(m.todo)!=64 || m.keepAll {t.Fatal("pending queue capacity")}
 before:=m.budget.work;if m.push(nil) || !m.keepAll || cap(m.todo)!=64 || len(m.todo)!=64 || m.budget.work!=before {t.Fatal("one-over pending grew queue")}
}
func TestBoundedCapacityAdmission(t *testing.T) {
 for _,n:=range []int{0,2,3,257} {
  values:=make([]int,n);m:=boundedMarker();m.budget.work=2;astMarkLeafCells(m,values)
  if n<=2 {if m.keepAll || len(m.live)!=n || m.budget.work!=2-n {t.Fatal("leaf capacity boundary",n)}} else if !m.keepAll || m.live!=nil || m.todo!=nil || m.budget.work!=2 {t.Fatal("oversized capacity examined cells",n)}
 }
 a,b:=1,2
 for _,work:=range []int{3,4} {m:=boundedMarker();m.budget.work=work;astMarkCells(m,[]*int{&a,&b});if work==4 {if m.keepAll || len(m.live)!=2 || len(m.todo)!=2 {t.Fatal("exact cell work")}} else if !m.keepAll || len(m.todo)!=1 || cap(m.todo)>64 {t.Fatal("cell work over boundary")}}
 for _,root:=range []any{Fields(make([]NodeField,257)),&Node{Children:make([]*Node,257)}} {
  m:=boundedMarker();m.run(root,nil);if !m.keepAll || m.complete || cap(m.todo)>64 || len(m.live)>1 {t.Fatal("Fields/Children capacity queued a large view")}
 }
 m:=boundedMarker();m.budget.work=-1;if m.cells(0) || !m.keepAll {t.Fatal("negative remaining cell work")}
 m=boundedMarker();if m.cells(-1) || !m.keepAll {t.Fatal("negative cell request")}
}
func TestBoundedPreflightBudgets(t *testing.T) {
 for _,records:=range []int{-1,0} {p:=astOutputPreflight{records:records,slots:4096};if astCheckBlocks([][]int(nil),&p) {t.Fatal("invalid domain budget")}}
 p:=astOutputPreflight{records:1,slots:-1};if astCheckBlocks([][]int(nil),&p) {t.Fatal("negative slots")}
 p=astOutputPreflight{records:1,slots:0};if !astCheckBlocks([][]int(nil),&p) || p.records!=0 {t.Fatal("empty domain not charged")}
 for _,blocks:=range []int{63,64} {p=astOutputPreflight{records:64,slots:0};ok:=astCheckBlocks(make([][]int,blocks),&p);if ok!=(blocks==63) || p.records!=0 || p.slots!=0 {t.Fatal("record boundary",blocks)}}
 for _,n:=range []int{4096,4097} {p=astOutputPreflight{records:2,slots:4096};ok:=astCheckBlocks([][]int{make([]int,n)},&p);if ok!=(n==4096) || p.slots<0 {t.Fatal("slot boundary",n)}}
 p=astOutputPreflight{records:3,slots:4096};if !astCheckBlocks([][]int{make([]int,2048),make([]int,2048)},&p) || p.slots!=0 {t.Fatal("multiple-block exact slot sum")}
 p=astOutputPreflight{records:3,slots:4096};if astCheckBlocks([][]int{make([]int,2048),make([]int,2049)},&p) || p.slots!=2048 {t.Fatal("multiple-block overflow subtraction")}
 maxInt:=int(^uint(0)>>1);p=astOutputPreflight{records:2,slots:maxInt};if !astCheckBlocks([][]int{{1,2,3}},&p) || p.slots!=maxInt-3 {t.Fatal("native maximum slot subtraction")}
 var empty astChunk[int];p=astOutputPreflight{records:1,slots:0};if !astCheckChunk(&empty,&p) || p.records!=0 {t.Fatal("empty chunk domain")}
}
`
