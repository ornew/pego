package gogen_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

func TestGeneratedGoMemoEnvironmentWork(t *testing.T) {
	t.Parallel()
	gentest.NeedsWorkControls(t)
	if testing.Short() {
		t.Skip("builds generated memo/environment consumers")
	}
	source := `type Word = { Text string }

var v string
rule main Word = t:({v := "a"} &word {v := "b"} word / {v := "b"} word) #atomic => Word{Text: text($t)}

rule word Word = t:(x:[a-z]+ #atomic {text($x) == v}) => Word{Text: text($t)}`
	// Each helper has a callee and two references, making more than 64
	// rules eligible for deferred memoization in the emitted runtime.
	var alternatives []string
	for i := 0; i < 70; i++ {
		name := fmt.Sprintf("spare%d", i)
		source += "\nrule " + name + " Word = word"
		alternatives = append(alternatives, name, name)
	}
	source += "\nrule spare_root Word = " + strings.Join(alternatives, " / ")
	g, err := parseTest(source)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gentest.GoMod("memofixture")), 0644); err != nil {
		t.Fatal(err)
	}
	pkg := "memo"
	code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: pkg, Start: "main", Types: true, Recognize: true})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, pkg)
	if err := os.Mkdir(path, 0755); err != nil {
		t.Fatal(err)
	}
	fixture := strings.NewReplacer("PACKAGE", pkg).Replace(generatedGoMemoConsumer)
	for name, data := range map[string][]byte{"parser.go": code, "memo_test.go": []byte(fixture)} {
		if err := os.WriteFile(filepath.Join(path, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated memo/env accounting: %v\n%s", err, out)
	}
}

const generatedGoMemoConsumer = `package PACKAGE

import pegort "github.com/ornew/pego/runtime"
import("context";"errors";"fmt";"reflect";"strings";"testing")
func catchMemoFatal(f func())(err error){defer func(){if x:=recover();x!=nil{v,ok:=x.(pegort.X_fatal);if !ok{panic(x)};err=v.X_err}}();f();return nil}
func makeEnvParser(limit uint64)*pegort.Parser{
 p:=&pegort.Parser{X_bound:make(map[string]bool),Control:pegort.X_newWorkControl(nil,WorkLimits{MaxSteps:limit})}
 for i:=0;i<300;i++{name:=fmt.Sprintf("v%d",i);p.Env=&pegort.Env{X_name:name,X_val:i,X_next:p.Env};p.X_bound[name]=true}
 return p
}
func TestEnvironmentWalksAndAtomicBind(t *testing.T){
 for _,run:=range []struct{name string;f func(*pegort.Parser)}{
  {"key",func(p *pegort.Parser){p.X_envValues([]string{"v0"})}},
  {"lookup",func(p *pegort.Parser){(&pegort.ActCtx{P:p}).Lookup("v0")}},
  {"typed lookup",func(p *pegort.Parser){(&pegort.TActCtx{P:&pegort.TParser{Parser:p}}).Lookup("v0")}},
  {"bind",func(p *pegort.Parser){p.X_bind("v0",777)}},
 }{
  p:=makeEnvParser(32);head:=p.Env;before:=head.X_next
  if err:=catchMemoFatal(func(){run.f(p)});!errors.Is(err,ErrWorkLimit){t.Errorf("%s unbounded: %T",run.name,err)}
  if p.Env!=head || head.X_next!=before{t.Fatal("partial environment published")}
  p.Control=pegort.X_newWorkControl(nil,WorkLimits{MaxSteps:100000});run.f(p)
  if run.name=="bind" {if (&pegort.ActCtx{P:p}).Lookup("v0")!=777 || head.X_val!=299{t.Fatal("binding changed saved environment")}} else {if (&pegort.ActCtx{P:p}).Lookup("v0")!=0{t.Fatal("lookup retry")}}
  if p.Control.X_inputUsed()!=0{t.Fatal("environment work charged input")}
 }
}
func TestNodeMemoLookupWalk(t *testing.T){
 tail:=&pegort.X_memoEntry{X_rule:7,X_node:&Node{RawText:"kept"},X_ok:true}
 head:=tail;for i:=0;i<300;i++{head=&pegort.X_memoEntry{X_rule:8,X_next:head}}
 p:=&pegort.Parser{Control:pegort.X_newWorkControl(nil,WorkLimits{MaxSteps:32})};p.X_memo.X_slots=[]*pegort.X_memoEntry{head}
 r:=&pegort.Rule{Id:7,Seen:-1,Memo:true}
 var got *Node
 err:=catchMemoFatal(func(){got,_=p.Call(r,0)})
 if !errors.Is(err,ErrWorkLimit)||got!=nil{t.Errorf("Node memo chain unbounded: value=%T error=%T",got,err)}
 if p.X_memo.X_slots[0]!=head || tail.X_next!=nil{t.Fatal("lookup changed table")}
 p.Control=pegort.X_newWorkControl(nil,WorkLimits{MaxSteps:100000})
 got,ok:=p.Call(r,0);if !ok||got!=tail.X_node{t.Fatal("memo retry")}
}
func TestTypedMemoWalksAndAtomicReplacement(t *testing.T){
 tail:=&pegort.X_tmemoEntry{X_rule:7,X_val:"kept",X_ok:true}
 head:=tail;for i:=0;i<300;i++{head=&pegort.X_tmemoEntry{X_rule:8,X_next:head}}
 for _,put:=range []bool{false,true}{
  p:=&pegort.TParser{Parser:&pegort.Parser{Control:pegort.X_newWorkControl(nil,WorkLimits{MaxSteps:32})},X_memoSlots:[]*pegort.X_tmemoEntry{head}}
  replacement:=&pegort.X_tmemoEntry{X_rule:-1,X_val:"replacement",X_ok:true}
  err:=catchMemoFatal(func(){if put{p.X_memoPut(7,0,0,nil,replacement)}else{p.X_memoGet(7,0,0,nil)}})
  if !errors.Is(err,ErrWorkLimit){t.Errorf("typed memo put=%v unbounded: %T",put,err)}
  if p.X_memoSlots[0]!=head || tail.X_next!=nil || replacement.X_rule!=-1{t.Fatal("partial replacement published")}
  p.Control=pegort.X_newWorkControl(nil,WorkLimits{MaxSteps:100000})
  if put{p.X_memoPut(7,0,0,nil,replacement);if p.X_memoGet(7,0,0,nil)!=replacement{t.Fatal("typed put retry")}}else{if p.X_memoGet(7,0,0,nil)!=tail{t.Fatal("typed read retry")}}
 }
}
func TestMemoKeysAndStorage(t *testing.T){
 for _,typed:=range []bool{false,true}{
  p:=&pegort.Parser{Control:pegort.X_newWorkControl(nil,WorkLimits{MaxSteps:100000})};tp:=&pegort.TParser{Parser:p}
  get:=func(key []any)any{if typed{return tp.X_memoGet(7,0,0,key)};return p.X_memoGet(7,0,0,key)}
  put:=func(key []any)any{if typed{e:=&pegort.X_tmemoEntry{};tp.X_memoPut(7,0,0,key,e);return e};e:=&pegort.X_memoEntry{};p.X_memoPut(7,0,0,key,e);return e}
  var typedNil *Node
  entry:=put([]any{typedNil})
  if !reflect.ValueOf(get([]any{nil})).IsNil() || get([]any{typedNil})!=entry{t.Fatal("typed nil key equality changed")}
  empty:=put([]any{})
  if get(nil)!=empty{t.Fatal("nil/empty key equality changed")}
  key:=make([]any,400);for i:=range key{key[i]=i}
  entry=put(key);p.Control=pegort.X_newWorkControl(nil,WorkLimits{MaxSteps:32})
  err:=catchMemoFatal(func(){get(key)})
  if !errors.Is(err,ErrWorkLimit){t.Fatal("long key not bounded",err)}
  p.Control=pegort.X_newWorkControl(nil,WorkLimits{MaxSteps:100000});if get(key)!=entry{t.Fatal("long key retry")}
  long:=strings.Repeat("prefix",600);entry=put([]any{long+"x"})
  p.Control=pegort.X_newWorkControl(nil,WorkLimits{MaxSteps:32})
  if err:=catchMemoFatal(func(){get([]any{long+"y"})});!errors.Is(err,ErrWorkLimit){t.Fatal("string key comparison unbounded",err)}
  if p.Control.X_inputUsed()!=0{t.Fatal("memo string value charged input")}
 }
 for _,typed:=range []bool{false,true}{
  p:=&pegort.Parser{Control:pegort.X_newWorkControl(nil,WorkLimits{MaxSteps:129})}
  nodeOriginal:=&pegort.X_memoEntry{X_rule:3};nodeReplacement:=&pegort.X_memoEntry{X_rule:-1}
  p.X_memo.X_slots=make([]*pegort.X_memoEntry,300);p.X_memo.X_slots[0]=nodeOriginal
  typedOriginal:=&pegort.X_tmemoEntry{X_rule:3};typedReplacement:=&pegort.X_tmemoEntry{X_rule:-1}
  tp:=&pegort.TParser{Parser:p,X_memoSlots:make([]*pegort.X_tmemoEntry,300)};tp.X_memoSlots[0]=typedOriginal
  put:=func(){if typed{tp.X_memoPut(7,600,0,nil,typedReplacement)}else{p.X_memoPut(7,600,0,nil,nodeReplacement)}}
  err:=catchMemoFatal(put)
  if !errors.Is(err,ErrWorkLimit){t.Fatal("slot copy unbounded",err)}
  if len(p.X_memo.X_slots)!=300 || p.X_memo.X_slots[0]!=nodeOriginal || nodeReplacement.X_rule!=-1 || len(tp.X_memoSlots)!=300 || tp.X_memoSlots[0]!=typedOriginal || typedReplacement.X_rule!=-1{t.Fatal("partial slot growth published")}
  p.Control=pegort.X_newWorkControl(nil,WorkLimits{MaxSteps:100000});put()
  if typed{tp.X_memoPut(7,600,0,nil,typedReplacement);if tp.X_memoGet(7,600,0,nil)!=typedReplacement || typedReplacement.X_next!=nil{t.Fatal("typed self replacement changed")}}else{p.X_memoPut(7,600,0,nil,nodeReplacement);if p.X_memoGet(7,600,0,nil)!=nodeReplacement || nodeReplacement.X_next!=nil{t.Fatal("Node self replacement changed")}}
 }
}
func TestFirstCallStoragePublication(t *testing.T){
 for _,reuse:=range []bool{false,true}{
  p:=&pegort.Parser{Pos:64000,Control:pegort.X_newWorkControl(nil,WorkLimits{MaxSteps:129})}
  p.X_memo.X_stride=1;p.X_memo.X_calls=[]pegort.X_seenCalls{{X_calls:17,X_repeats:1}}
  if reuse{p.X_memo.X_seen=make([]uint64,0,2048);for i:=range p.X_memo.X_seen[:cap(p.X_memo.X_seen)]{p.X_memo.X_seen[:cap(p.X_memo.X_seen)][i]=^uint64(0)}}else{p.X_memo.X_seen=make([]uint64,512)}
  oldLen:=len(p.X_memo.X_seen);before:=p.X_memo.X_calls[0]
  r:=&pegort.Rule{Seen:0}
  err:=catchMemoFatal(func(){p.FirstCall(r)})
  if !errors.Is(err,ErrWorkLimit){t.Fatal("seen growth not bounded",err)}
  if len(p.X_memo.X_seen)!=oldLen || p.X_memo.X_calls[0]!=before{t.Fatal("partial estimator published")}
  p.Control=pegort.X_newWorkControl(nil,WorkLimits{MaxSteps:100000})
  if !p.FirstCall(r) || p.X_memo.X_calls[0].X_calls!=18{t.Fatal("first call retry changed")}
  if p.X_memo.X_seen[0]!=0 || p.X_memo.X_seen[1000]!=1{t.Fatal("reused unseen words were not zeroed before stamping")}
  if p.FirstCall(r){t.Fatal("repeated call deferred again")}
 }
}
func TestMemoCancellation(t *testing.T){
 p:=makeEnvParser(100000);ctx,cancel:=context.WithCancel(context.Background());cancel();p.Control=pegort.X_newWorkControl(ctx,WorkLimits{})
 before:=p.Env
 if err:=catchMemoFatal(func(){p.X_bind("v0",999)});!errors.Is(err,context.Canceled){t.Fatal("environment cancellation",err)}
 if p.Env!=before{t.Fatal("cancelled bind published")}
 tail:=&pegort.X_memoEntry{X_rule:7};head:=tail;for i:=0;i<600;i++{head=&pegort.X_memoEntry{X_rule:8,X_next:head}}
 p=&pegort.Parser{Control:pegort.X_newWorkControl(ctx,WorkLimits{})};p.X_memo.X_slots=[]*pegort.X_memoEntry{head}
 if err:=catchMemoFatal(func(){p.X_memoGet(7,0,0,nil)});!errors.Is(err,context.Canceled){t.Fatal("memo cancellation",err)}
 if p.X_memo.X_slots[0]!=head{t.Fatal("cancelled lookup mutated table")}
}

func TestSparseFirstCallInterruptionAndRetry(t *testing.T){
 if pegoGrammar.X_nseen<=64{t.Fatal("sparse interruption fixture has too few seen rules for sparse tracking")}
 for _,at:=range []int{600,1200}{
  p:=&pegort.Parser{Pos:at*1024,Control:pegort.X_newWorkControl(nil,WorkLimits{MaxSteps:129})}
  p.X_memo.X_stride=65;p.X_memo.X_sparse=true;p.X_memo.X_calls=make([]pegort.X_seenCalls,65)
  p.X_memo.X_calls[0]=pegort.X_seenCalls{X_calls:17,X_repeats:1}
  p.X_memo.X_pages=make([]pegort.X_seenRulePages,65)
  pages:=make([]*[16]uint64,300)
  retained:=&[16]uint64{7};pages[0]=retained
  p.X_memo.X_pages[0]=pegort.X_seenRulePages{X_pages:pages,X_last:retained,X_at:0}
  before:=p.X_memo.X_calls[0];r:=&pegort.Rule{Seen:0}
  if err:=catchMemoFatal(func(){p.FirstCall(r)});!errors.Is(err,ErrWorkLimit){t.Fatal("sparse directory copy was not interrupted",err)}
  if p.X_memo.X_calls[0]!=before || len(p.X_memo.X_pages[0].X_pages)!=300 || p.X_memo.X_pages[0].X_pages[0]!=retained || retained[0]!=7{t.Fatal("partial sparse directory or estimator published")}
  if len(p.X_memo.X_seen)!=0{t.Fatal("sparse invocation allocated a dense table")}
  p.Control=pegort.X_newWorkControl(nil,WorkLimits{MaxSteps:100000})
  if !p.FirstCall(r) || p.X_memo.X_calls[0].X_calls!=18 || p.FirstCall(r){t.Fatal("sparse retry or repeat detection changed")}
  if p.X_memo.X_pages[0].X_pages[0]!=retained || retained[0]!=7 || len(p.X_memo.X_seen)!=0{t.Fatal("sparse retry corrupted earlier pages or allocated dense storage")}
 }
}

func TestPublicEnvironmentParity(t *testing.T){
 for _,unit:=range []PositionUnit{Bytes,CodePoints}{for _,input:=range []string{"a","b","aa","bb","?",""}{
  base,baseErr:=Parse(input,WithUnit(unit));before:="";if base!=nil{before=base.String()}
  got,err:=Parse(input,WithUnit(unit),WithWorkLimits(WorkLimits{MaxSteps:100000}))
  if fmt.Sprint(err)!=fmt.Sprint(baseErr) || (got!=nil && got.String()!=before){t.Fatal("public memo/env changed",input,err,baseErr)}
  typed,typedErr:=ParseAST(input,WithUnit(unit));controlled,controlledErr:=ParseAST(input,WithUnit(unit),WithWorkLimits(WorkLimits{MaxSteps:100000}))
  if !reflect.DeepEqual(typed,controlled)||fmt.Sprint(typedErr)!=fmt.Sprint(controlledErr){t.Fatal("typed environment changed")}
 }}
}
`
