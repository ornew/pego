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

func TestGeneratedGoMemoEnvironmentWork(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated memo/environment consumers")
	}
	source := `type Word terminal

rule main Word = {v := "a"} &word {v := "b"} word / {v := "b"} word

rule word Word = x:[a-z]+ #atomic {text($x) == v}`
	// Each helper has a callee and two references, making more than 64
	// rules eligible for deferred memoization in the emitted runtime.
	var alternatives []string
	for i := 0; i < 70; i++ {
		name := fmt.Sprintf("spare%d", i)
		source += "\nrule " + name + " Word = word"
		alternatives = append(alternatives, name, name)
	}
	source += "\nrule spare_root Word = " + strings.Join(alternatives, " / ")
	g, err := syntax.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module memofixture\n\ngo 1.27.1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, general := range []bool{false, true} {
		for _, reference := range []bool{false, true} {
			pkg := fmt.Sprintf("memo_%v_%v", general, reference)
			code, err := Generate(g, GenOptions{Package: pkg, Start: "main", Types: true, Recognize: true, disableTypedCuts: general, referenceWorkControls: reference})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(code), "const sparseSeen = true") {
				t.Fatal("memo-work fixture must emit the sparse runtime")
			}
			path := filepath.Join(dir, pkg)
			if err := os.Mkdir(path, 0755); err != nil {
				t.Fatal(err)
			}
			fixture := strings.NewReplacer("PACKAGE", pkg, "REFERENCE", fmt.Sprint(reference)).Replace(generatedGoMemoConsumer)
			for name, data := range map[string][]byte{"parser.go": code, "memo_test.go": []byte(fixture)} {
				if err := os.WriteFile(filepath.Join(path, name), data, 0644); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated memo/env accounting: %v\n%s", err, out)
	}
}

const generatedGoMemoConsumer = `package PACKAGE
import("context";"errors";"fmt";"reflect";"strings";"testing")
const reference = REFERENCE
func catchMemoFatal(f func())(err error){defer func(){if x:=recover();x!=nil{v,ok:=x.(fatal);if !ok{panic(x)};err=v.err}}();f();return nil}
func makeEnvParser(limit uint64)*parser{
 p:=&parser{bound:make(map[string]bool),control:newWorkControl(nil,WorkLimits{MaxSteps:limit})}
 for i:=0;i<300;i++{name:=fmt.Sprintf("v%d",i);p.env=&env{name:name,val:i,next:p.env};p.bound[name]=true}
 return p
}
func TestEnvironmentWalksAndAtomicBind(t *testing.T){
 if reference{p:=makeEnvParser(1);if (&actx{p:p}).lookup("v0")!=0{t.Fatal("reference lookup")};p.bind("v0",777);if p.control.stepsUsed!=0{t.Fatal("reference charged")};return}
 for _,run:=range []struct{name string;f func(*parser)}{
  {"key",func(p *parser){p.envValues([]string{"v0"})}},
  {"lookup",func(p *parser){(&actx{p:p}).lookup("v0")}},
  {"typed lookup",func(p *parser){(&tctx{p:&tparser{parser:p}}).lookup("v0")}},
  {"bind",func(p *parser){p.bind("v0",777)}},
 }{
  p:=makeEnvParser(32);head:=p.env;before:=head.next
  if err:=catchMemoFatal(func(){run.f(p)});!errors.Is(err,ErrWorkLimit){t.Errorf("%s unbounded: %T",run.name,err)}
  if p.env!=head || head.next!=before{t.Fatal("partial environment published")}
  p.control=newWorkControl(nil,WorkLimits{MaxSteps:100000});run.f(p)
  if run.name=="bind" {if (&actx{p:p}).lookup("v0")!=777 || head.val!=299{t.Fatal("binding changed saved environment")}} else {if (&actx{p:p}).lookup("v0")!=0{t.Fatal("lookup retry")}}
  if p.control.inputUsed!=0{t.Fatal("environment work charged input")}
 }
}
func TestNodeMemoLookupWalk(t *testing.T){
 tail:=&memoEntry{rule:7,node:&Node{Text:"kept"},ok:true}
 head:=tail;for i:=0;i<300;i++{head=&memoEntry{rule:8,next:head}}
 p:=&parser{control:newWorkControl(nil,WorkLimits{MaxSteps:32})};p.memo.slots=[]*memoEntry{head}
 r:=&rule{id:7,seen:-1,memo:true}
 var got *Node
 err:=catchMemoFatal(func(){got,_=p.call(r,0)})
 if reference{if err!=nil||got!=tail.node || p.control.stepsUsed!=0{t.Fatal("reference memo")};return}
 if !errors.Is(err,ErrWorkLimit)||got!=nil{t.Errorf("Node memo chain unbounded: value=%T error=%T",got,err)}
 if p.memo.slots[0]!=head || tail.next!=nil{t.Fatal("lookup changed table")}
 p.control=newWorkControl(nil,WorkLimits{MaxSteps:100000})
 got,ok:=p.call(r,0);if !ok||got!=tail.node{t.Fatal("memo retry")}
}
func TestTypedMemoWalksAndAtomicReplacement(t *testing.T){
 tail:=&tmemoEntry{rule:7,val:"kept",ok:true}
 head:=tail;for i:=0;i<300;i++{head=&tmemoEntry{rule:8,next:head}}
 for _,put:=range []bool{false,true}{
  p:=&tparser{parser:&parser{control:newWorkControl(nil,WorkLimits{MaxSteps:32})},memoSlots:[]*tmemoEntry{head}}
  replacement:=&tmemoEntry{rule:-1,val:"replacement",ok:true}
  err:=catchMemoFatal(func(){if put{p.memoPut(7,0,0,nil,replacement)}else{p.memoGet(7,0,0,nil)}})
  if reference{if err!=nil || p.control.stepsUsed!=0{t.Fatal("reference typed memo")};continue}
  if !errors.Is(err,ErrWorkLimit){t.Errorf("typed memo put=%v unbounded: %T",put,err)}
  if p.memoSlots[0]!=head || tail.next!=nil || replacement.rule!=-1{t.Fatal("partial replacement published")}
  p.control=newWorkControl(nil,WorkLimits{MaxSteps:100000})
  if put{p.memoPut(7,0,0,nil,replacement);if p.memoGet(7,0,0,nil)!=replacement{t.Fatal("typed put retry")}}else{if p.memoGet(7,0,0,nil)!=tail{t.Fatal("typed read retry")}}
 }
}
func TestMemoKeysAndStorage(t *testing.T){
 for _,typed:=range []bool{false,true}{
  p:=&parser{control:newWorkControl(nil,WorkLimits{MaxSteps:100000})};tp:=&tparser{parser:p}
  get:=func(key []any)any{if typed{return tp.memoGet(7,0,0,key)};return p.memoGet(7,0,0,key)}
  put:=func(key []any)any{if typed{e:=&tmemoEntry{};tp.memoPut(7,0,0,key,e);return e};e:=&memoEntry{};p.memoPut(7,0,0,key,e);return e}
  var typedNil *Node
  entry:=put([]any{typedNil})
  if !reflect.ValueOf(get([]any{nil})).IsNil() || get([]any{typedNil})!=entry{t.Fatal("typed nil key equality changed")}
  empty:=put([]any{})
  if get(nil)!=empty{t.Fatal("nil/empty key equality changed")}
  key:=make([]any,400);for i:=range key{key[i]=i}
  entry=put(key);p.control=newWorkControl(nil,WorkLimits{MaxSteps:32})
  err:=catchMemoFatal(func(){get(key)})
  if reference{if err!=nil || p.control.stepsUsed!=0{t.Fatal("reference key charged")};continue}
  if !errors.Is(err,ErrWorkLimit){t.Fatal("long key not bounded",err)}
  p.control=newWorkControl(nil,WorkLimits{MaxSteps:100000});if get(key)!=entry{t.Fatal("long key retry")}
  long:=strings.Repeat("prefix",600);entry=put([]any{long+"x"})
  p.control=newWorkControl(nil,WorkLimits{MaxSteps:32})
  if err:=catchMemoFatal(func(){get([]any{long+"y"})});!errors.Is(err,ErrWorkLimit){t.Fatal("string key comparison unbounded",err)}
  if p.control.inputUsed!=0{t.Fatal("memo string value charged input")}
 }
 for _,typed:=range []bool{false,true}{
  p:=&parser{control:newWorkControl(nil,WorkLimits{MaxSteps:129})}
  nodeOriginal:=&memoEntry{rule:3};nodeReplacement:=&memoEntry{rule:-1}
  p.memo.slots=make([]*memoEntry,300);p.memo.slots[0]=nodeOriginal
  typedOriginal:=&tmemoEntry{rule:3};typedReplacement:=&tmemoEntry{rule:-1}
  tp:=&tparser{parser:p,memoSlots:make([]*tmemoEntry,300)};tp.memoSlots[0]=typedOriginal
  put:=func(){if typed{tp.memoPut(7,600,0,nil,typedReplacement)}else{p.memoPut(7,600,0,nil,nodeReplacement)}}
  err:=catchMemoFatal(put)
  if reference{if err!=nil{t.Fatal("reference growth",err)};continue}
  if !errors.Is(err,ErrWorkLimit){t.Fatal("slot copy unbounded",err)}
  if len(p.memo.slots)!=300 || p.memo.slots[0]!=nodeOriginal || nodeReplacement.rule!=-1 || len(tp.memoSlots)!=300 || tp.memoSlots[0]!=typedOriginal || typedReplacement.rule!=-1{t.Fatal("partial slot growth published")}
  p.control=newWorkControl(nil,WorkLimits{MaxSteps:100000});put()
  if typed{tp.memoPut(7,600,0,nil,typedReplacement);if tp.memoGet(7,600,0,nil)!=typedReplacement || typedReplacement.next!=nil{t.Fatal("typed self replacement changed")}}else{p.memoPut(7,600,0,nil,nodeReplacement);if p.memoGet(7,600,0,nil)!=nodeReplacement || nodeReplacement.next!=nil{t.Fatal("Node self replacement changed")}}
 }
}
func TestFirstCallStoragePublication(t *testing.T){
 for _,reuse:=range []bool{false,true}{
  p:=&parser{pos:64000,control:newWorkControl(nil,WorkLimits{MaxSteps:129})}
  p.memo.stride=1;p.memo.calls=[]seenCalls{{calls:17,repeats:1}}
  if reuse{p.memo.seen=make([]uint64,0,2048);for i:=range p.memo.seen[:cap(p.memo.seen)]{p.memo.seen[:cap(p.memo.seen)][i]=^uint64(0)}}else{p.memo.seen=make([]uint64,512)}
  oldLen:=len(p.memo.seen);before:=p.memo.calls[0]
  r:=&rule{seen:0}
  err:=catchMemoFatal(func(){p.firstCall(r)})
  if reference{if err!=nil || p.control.stepsUsed!=0{t.Fatal("reference estimator")};continue}
  if !errors.Is(err,ErrWorkLimit){t.Fatal("seen growth not bounded",err)}
  if len(p.memo.seen)!=oldLen || p.memo.calls[0]!=before{t.Fatal("partial estimator published")}
  p.control=newWorkControl(nil,WorkLimits{MaxSteps:100000})
  if !p.firstCall(r) || p.memo.calls[0].calls!=18{t.Fatal("first call retry changed")}
  if p.memo.seen[0]!=0 || p.memo.seen[1000]!=1{t.Fatal("reused unseen words were not zeroed before stamping")}
  if p.firstCall(r){t.Fatal("repeated call deferred again")}
 }
}
func TestMemoCancellation(t *testing.T){
 if reference{return}
 p:=makeEnvParser(100000);ctx,cancel:=context.WithCancel(context.Background());cancel();p.control=newWorkControl(ctx,WorkLimits{})
 before:=p.env
 if err:=catchMemoFatal(func(){p.bind("v0",999)});!errors.Is(err,context.Canceled){t.Fatal("environment cancellation",err)}
 if p.env!=before{t.Fatal("cancelled bind published")}
 tail:=&memoEntry{rule:7};head:=tail;for i:=0;i<600;i++{head=&memoEntry{rule:8,next:head}}
 p=&parser{control:newWorkControl(ctx,WorkLimits{})};p.memo.slots=[]*memoEntry{head}
 if err:=catchMemoFatal(func(){p.memoGet(7,0,0,nil)});!errors.Is(err,context.Canceled){t.Fatal("memo cancellation",err)}
 if p.memo.slots[0]!=head{t.Fatal("cancelled lookup mutated table")}
}

func TestSparseFirstCallInterruptionAndRetry(t *testing.T){
 if reference{return}
 if !sparseSeen{t.Fatal("sparse interruption fixture unexpectedly emitted a dense runtime")}
 for _,at:=range []int{600,1200}{
  p:=&parser{pos:at*1024,control:newWorkControl(nil,WorkLimits{MaxSteps:129})}
  p.memo.stride=65;p.memo.sparse=true;p.memo.calls=make([]seenCalls,65)
  p.memo.calls[0]=seenCalls{calls:17,repeats:1}
  p.memo.pages=make([]seenRulePages,65)
  pages:=make([]*[16]uint64,300)
  retained:=&[16]uint64{7};pages[0]=retained
  p.memo.pages[0]=seenRulePages{pages:pages,last:retained,at:0}
  before:=p.memo.calls[0];r:=&rule{seen:0}
  if err:=catchMemoFatal(func(){p.firstCall(r)});!errors.Is(err,ErrWorkLimit){t.Fatal("sparse directory copy was not interrupted",err)}
  if p.memo.calls[0]!=before || len(p.memo.pages[0].pages)!=300 || p.memo.pages[0].pages[0]!=retained || retained[0]!=7{t.Fatal("partial sparse directory or estimator published")}
  if len(p.memo.seen)!=0{t.Fatal("sparse invocation allocated a dense table")}
  p.control=newWorkControl(nil,WorkLimits{MaxSteps:100000})
  if !p.firstCall(r) || p.memo.calls[0].calls!=18 || p.firstCall(r){t.Fatal("sparse retry or repeat detection changed")}
  if p.memo.pages[0].pages[0]!=retained || retained[0]!=7 || len(p.memo.seen)!=0{t.Fatal("sparse retry corrupted earlier pages or allocated dense storage")}
 }
}

func TestPublicEnvironmentParity(t *testing.T){
 for _,unit:=range []Unit{Bytes,CodePoints}{for _,input:=range []string{"a","b","aa","bb","?",""}{
  base,baseErr:=Parse(input,WithUnit(unit));before:="";if base!=nil{before=base.String()}
  if reference{continue}
  got,err:=Parse(input,WithUnit(unit),WithWorkLimits(WorkLimits{MaxSteps:100000}))
  if fmt.Sprint(err)!=fmt.Sprint(baseErr) || (got!=nil && got.String()!=before){t.Fatal("public memo/env changed",input,err,baseErr)}
  typed,typedErr:=ParseAST(input,WithUnit(unit));controlled,controlledErr:=ParseAST(input,WithUnit(unit),WithWorkLimits(WorkLimits{MaxSteps:100000}))
  if !reflect.DeepEqual(typed,controlled)||fmt.Sprint(typedErr)!=fmt.Sprint(controlledErr){t.Fatal("typed environment changed")}
 }}
}
`
