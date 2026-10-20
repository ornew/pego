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

func TestGeneratedGoGraphWork(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated graph-control consumers")
	}
	g, err := syntax.Parse(`type Leaf = { Text string }

type Bag = {
    Items  []Leaf
    Nested [][]Leaf
}

rule main Bag = x:leaf* #cut => Bag{Items: $x.Value, Nested: list($x.Value, $x.Value)}

rule leaf Leaf = t:("a") #atomic => Leaf{Text: text($t)}

rule rollback Bag = (x:leaf "z" / x:leaf) rest:(part:leaf "b")* last:leaf #cut => Bag{Items: list($x, $last), Nested: list(map($rest, (r) => r.$part))}`)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module graphfixture\n\ngo 1.27.1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, convert := range []bool{false, true} {
		for _, general := range []bool{false, true} {
			pkg := fmt.Sprintf("graph_%v_%v", convert, general)
			code, err := Generate(g, GenOptions{Package: pkg, Start: "main", Types: true, Recognize: true, convertTypes: convert, disableTypedCuts: general})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, pkg)
			if err := os.Mkdir(path, 0755); err != nil {
				t.Fatal(err)
			}
			check := ""
			if !convert {
				check = generatedGoGraphTypedRuleCheck
			}
			consumer := strings.NewReplacer("TYPED_RULE_CHECK", check, "PACKAGE", pkg, "GENERAL", fmt.Sprint(general), "CONVERT", fmt.Sprint(convert)).Replace(generatedGoGraphConsumer)
			for name, data := range map[string][]byte{"parser.go": code, "graph_test.go": []byte(consumer)} {
				if err := os.WriteFile(filepath.Join(path, name), data, 0644); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated graph controls: %v\n%s", err, out)
	}
}

// generatedGoGraphTypedRuleCheck needs the typed rule table, which conversion output omits.
const generatedGoGraphTypedRuleCheck = `for _,name:=range []string{"main","rollback"}{found:=false;for _,r:=range trules{if r.Name==name{found=true;if general && r.Body==nil{t.Fatal("fixture did not exercise a general typed body",name)};if !general && (r.Entry==nil || r.Body!=nil){t.Fatal("fixture did not exercise a direct typed body",name)}}};if !found{t.Fatal("missing typed rule",name)}}
`

const generatedGoGraphConsumer = `package PACKAGE
import("context";"errors";"fmt";"reflect";"strings";"testing")
const conversion = CONVERT
const general = GENERAL
func graphParser(limit uint64)*Parser{p:=&Parser{Control:newWorkControl(nil,WorkLimits{MaxSteps:limit})};p.setGrammar(pegoGrammar);return p}
func graphFatal(f func())(err error){defer func(){if x:=recover();x!=nil{v,ok:=x.(fatal);if !ok{panic(x)};err=v.err}}();f();return nil}
func requireGraphLimit(t *testing.T,p *Parser,f func()){
 t.Helper();err:=graphFatal(f)
 if !errors.Is(err,ErrWorkLimit){t.Fatal("operation escaped work budget",err)}
 if p.Control.inputUsed()!=0{t.Fatal("graph work charged input")}
}
func graphKids()([]*Node,[]any){
 nodes:=make([]*Node,300);values:=make([]any,300)
 for i:=range nodes{nodes[i]=&Node{RawStart:int32(i+3),RawEnd:int32(i+4),RawText:"a"};values[i]=&TMatch{TNode:TNode[string]{Span:Span{Start:i+3,End:i+4},Value:"a"}}}
 return nodes,values
}
func TestGraphFields(t *testing.T){
 nodes,_:=graphKids();fs:=make(Fields,300);tn:=&TSeq{Typ:"tuple",fields:make([]tfield,300)}
 for i:=range fs{name:=fmt.Sprintf("field%d",i);fs[i]=NodeField{name,i};tn.fields[i]=tfield{name,i}}
 for _,typed:=range []bool{false,true}{
  p:=graphParser(32);tp:=&TParser{Parser:p};node:=&Node{RawFields:fs}
  requireGraphLimit(t,p,func(){if typed{(&TActCtx{P:tp}).Member(tn,"absent-field-with-distinct-length")}else{(&ActCtx{P:p}).Member(node,"absent-field-with-distinct-length")}})
  p.Control=newWorkControl(nil,WorkLimits{MaxSteps:100000})
  var got any;if typed{got=(&TActCtx{P:tp}).Member(tn,"field299")}else{got=(&ActCtx{P:p}).Member(node,"field299")}
  if got!=299{t.Fatal("field read changed",got)}
  p.Control=newWorkControl(nil,WorkLimits{MaxSteps:32})
  requireGraphLimit(t,p,func(){if typed{tp.setValueField(tn,"field299",nodes[0])}else{p.setValueField(&fs,"field299",nodes[0])}})
  if typed && tn.fields[299].val!=299 || !typed && fs[299].Value!=299{t.Fatal("failed setter changed value")}
  p.Control=newWorkControl(nil,WorkLimits{MaxSteps:100000})
  if typed{tp.setValueField(tn,"field299",nodes[0]);if len(tn.fields)!=300 || tn.fields[299].val!=nodes[0]{t.Fatal("typed replacement")};tn.fields[299].val=299}else{p.setValueField(&fs,"field299",nodes[0]);if len(fs)!=300 || fs[299].Value!=nodes[0]{t.Fatal("Node replacement")};fs[299].Value=299}
 }
}
func TestGraphListsAndOwnership(t *testing.T){
 nodes,values:=graphKids()
 for _,typed:=range []bool{false,true}{
  for _,spanOnly:=range []bool{false,true}{
   p:=graphParser(129);tp:=&TParser{Parser:p};var result any
   requireGraphLimit(t,p,func(){if typed{c:=&TActCtx{P:tp};if spanOnly{result=c.listNode(values)}else{result=c.NewList(values)}}else{c:=&ActCtx{P:p};if spanOnly{result=c.listNode(nodes)}else{result=c.NewList(nodes)}}})
   if result!=nil{t.Fatal("partial list returned")}
   p.Control=newWorkControl(nil,WorkLimits{MaxSteps:100000})
   if typed{n:=(&TActCtx{P:tp}).NewList(values).(*TSeq);if n.Start!=3 || n.End!=303 || len(n.Kids)!=300{t.Fatal("typed list spans")};n.Kids[0]=nil;if values[0]==nil{t.Fatal("typed list aliases source")}}else{n:=(&ActCtx{P:p}).NewList(nodes);if n.RawStart!=3 || n.RawEnd!=303 || len(n.RawChildren)!=300{t.Fatal("Node list spans")};n.RawChildren[0]=nil;if nodes[0]==nil{t.Fatal("Node list aliases source")}}
  }
  p:=graphParser(129);tp:=&TParser{Parser:p}
  requireGraphLimit(t,p,func(){if typed{tp.KidStack=append([]any(nil),values...);tp.Kids(0)}else{p.KidStack=append([]*Node(nil),nodes...);p.Kids(0)}})
  p.Control=newWorkControl(nil,WorkLimits{MaxSteps:100000})
  if typed{tp.KidStack=append([]any(nil),values...);got:=tp.Kids(0);if len(got)!=300{t.Fatal("typed kids")};got[0]=nil;if values[0]==nil{t.Fatal("typed children alias")}}else{p.KidStack=append([]*Node(nil),nodes...);got:=p.Kids(0);if len(got)!=300{t.Fatal("Node kids")};got[0]=nil;if nodes[0]==nil{t.Fatal("Node children alias")}}
 }
}
func TestGraphCaptureUndoAndCleanup(t *testing.T){
 for _,typed:=range []bool{false,true}{
  p:=graphParser(32);tp:=&TParser{Parser:p}
  n:=&Node{RawText:"old"};changed:=&Node{RawText:"new"};f:=&CaptureFrame{Vals:make([]*Node,300)};tf:=&TCaptureFrame{Vals:make([]any,300)}
  for i:=0;i<300;i++{f.Vals[i]=changed;tf.Vals[i]=changed;p.Trail=append(p.Trail,undo{f:f,slot:i,old:n});tp.Trail=append(tp.Trail,tundo{f:tf,slot:i,old:n})}
  requireGraphLimit(t,p,func(){if typed{tp.Reset(Mark{})}else{p.Reset(Mark{})}})
  if typed && len(tp.Trail)!=300 || !typed && len(p.Trail)!=300{t.Fatal("fatal lost undo records")}
  p.Control=newWorkControl(nil,WorkLimits{MaxSteps:100000})
  if typed{tp.Reset(Mark{});for _,v:=range tf.Vals{if v!=n{t.Fatal("typed undo retry")}};if len(tp.Trail)!=0{t.Fatal("typed trail retained")};tp.Trail=append(tp.Trail,tundo{f:tf,old:n});tp.Control=newWorkControl(nil,WorkLimits{MaxSteps:1});tp.recycle();for _,v:=range tp.Trail[:cap(tp.Trail)]{if v.f!=nil || v.old!=nil{t.Fatal("typed cleanup blocked")}}}else{p.Reset(Mark{});for _,v:=range f.Vals{if v!=n{t.Fatal("Node undo retry")}};if len(p.Trail)!=0{t.Fatal("Node trail retained")};p.Trail=append(p.Trail,undo{f:f,old:n});p.Control=newWorkControl(nil,WorkLimits{MaxSteps:1});p.release();for _,v:=range p.Trail[:cap(p.Trail)]{if v.f!=nil || v.old!=nil{t.Fatal("Node cleanup blocked")}}}
 }
}
func TestTypedFieldMaterialization(t *testing.T){
 bag:=&Bag{Items:make([]*Leaf,300),Nested:make([][]*Leaf,300)}
 for i:=range bag.Items{bag.Items[i]=&Leaf{Span:Span{Start:i+3,End:i+4},Text:"a"};bag.Nested[i]=[]*Leaf{bag.Items[i]}}
 for _,name:=range []string{"Items","Nested"}{
  p:=graphParser(32);tp:=&TParser{Parser:p};var result any
  requireGraphLimit(t,p,func(){result=(&TActCtx{P:tp}).Member(bag,name)})
  if result!=nil{t.Fatal("partial field published")}
  p.Control=newWorkControl(nil,WorkLimits{MaxSteps:100000})
  got:=(&TActCtx{P:tp}).Member(bag,name).(*TSeq);ordinary,_:=bag.TField(name)
  want:=ordinary.(*TSeq)
  if !reflect.DeepEqual(got,want) || got.Start!=3 || got.End!=303{t.Fatal("field materialization changed",name)}
  wantSteps:=uint64(2*len(bag.Items));if name=="Nested"{wantSteps+=uint64(2*len(bag.Nested))};if p.Control.stepsUsed()!=wantSteps{t.Fatal("list field work omitted",name,p.Control.stepsUsed(),wantSteps)}
  if name=="Items"{got.Kids[0]=nil;if bag.Items[0]==nil{t.Fatal("field result aliases input")}}
 }
 p:=graphParser(32);tp:=&TParser{Parser:p};empty:=&Bag{Items:[]*Leaf{},Nested:nil}
 if got:=(&TActCtx{P:tp}).Member(empty,"Items");got==nil || len(got.(*TSeq).Kids)!=0{t.Fatal("empty field changed")}
 if got:=(&TActCtx{P:tp}).Member(empty,"Nested");got!=nil{t.Fatal("nil field changed")}
}
func TestGraphFoldOrderAndBudget(t *testing.T){
 nodes,values:=graphKids()
 for _,typed:=range []bool{false,true}{for _,right:=range []bool{false,true}{
  p:=graphParser(32);tp:=&TParser{Parser:p};visits:=0
  f:=func(acc,item any)any{visits++;return item}
  requireGraphLimit(t,p,func(){if typed{(&TActCtx{P:tp}).Fold("foldl",right,nil,&TSeq{Typ:"list",Kids:values},f)}else{(&ActCtx{P:p}).Fold("foldl",right,nil,&Node{RawKind:RawKindList,RawChildren:nodes},f)}})
  if visits!=32{t.Fatal("fold advanced past budget",visits)}
  p.Control=newWorkControl(nil,WorkLimits{MaxSteps:100000});visits=0
  var got any;if typed{got=(&TActCtx{P:tp}).Fold("foldl",right,nil,&TSeq{Typ:"list",Kids:values},f)}else{got=(&ActCtx{P:p}).Fold("foldl",right,nil,&Node{RawKind:RawKindList,RawChildren:nodes},f)}
  index:=299;if right{index=0};want:=any(nodes[index]);if typed{want=values[index]};if visits!=300 || got!=want{t.Fatal("fold order changed",visits)}
 }}
}
func TestGraphPublicLimitsAndRetainedResults(t *testing.T){
 input:=strings.Repeat("a",150)
 baseline,err:=ParseAST(input);if err!=nil || baseline==nil || len(baseline.Items)!=150 || len(baseline.Nested)!=2{t.Fatal("typed baseline",err)}
 before:=fmt.Sprintf("%#v",baseline.Items[0]);tree,err:=Parse(input);if err!=nil{t.Fatal(err)};treeBefore:=tree.String()
 for _,unit:=range []PositionUnit{Bytes,CodePoints}{
  for _,ast:=range []bool{false,true}{
   call:=func(n uint64)(any,error){opts:=[]ParseOption{WithUnit(unit),WithWorkLimits(WorkLimits{MaxSteps:n})};if ast{return ParseAST(input,opts...)};return Parse(input,opts...)}
   lo,hi:=uint64(1),uint64(1000000)
   for lo<hi{mid:=lo+(hi-lo)/2;v,e:=call(mid);if errors.Is(e,ErrWorkLimit){if !reflect.ValueOf(v).IsZero(){t.Fatal("partial public result")};lo=mid+1}else if e==nil{hi=mid}else{t.Fatal("unexpected public error",e)}}
   if _,e:=call(lo);e!=nil{t.Fatal("exact limit",e)}
   if v,e:=call(lo-1);!errors.Is(e,ErrWorkLimit) || !reflect.ValueOf(v).IsZero(){t.Fatal("one-short result",v,e)}
   ctx,cancel:=context.WithCancel(context.Background());cancel()
   if n,e:=ParseAST(input,WithContext(ctx));n!=nil || !errors.Is(e,context.Canceled){t.Fatal("cancelled typed result",n,e)}
   if n,e:=ParseAST(input);n==nil || e!=nil{t.Fatal("typed retry",e)}
  }
 }
 if tree.String()!=treeBefore || fmt.Sprintf("%#v",baseline.Items[0])!=before{t.Fatal("retained result changed")}
}
func TestGraphCaptureAttachment(t *testing.T){
 names:=make([]string,300);nodes,values:=graphKids()
 for i:=range names{names[i]=fmt.Sprintf("capture%d",i)}
 names[299]=names[0]
 for _,typed:=range []bool{false,true}{
  p:=graphParser(32);tp:=&TParser{Parser:p};var result any
  requireGraphLimit(t,p,func(){if typed{result=tp.AttachCaptures(nil,names,values,3,303)}else{result=p.AttachCaptures(nil,names,&CaptureFrame{Vals:nodes},3,303)}})
  if result!=nil{t.Fatal("partial capture node published")}
  p.Control=newWorkControl(nil,WorkLimits{MaxSteps:10000000})
  if typed{n:=tp.AttachCaptures(nil,names,values,3,303).(*TSeq);if len(n.fields)!=299 || n.fields[0].val!=values[299] || n.fields[1].name!=names[1]{t.Fatal("typed capture order/replacement")}}else{n:=p.AttachCaptures(nil,names,&CaptureFrame{Vals:nodes},3,303);if len(n.RawFields)!=299 || n.RawFields[0].Value!=nodes[299] || n.RawFields[1].Name!=names[1]{t.Fatal("Node capture order/replacement")}}
 }
}
func TestGraphOperatorsRuleSnapshots(t *testing.T){
 names:=make([]string,300);nodes,values:=graphKids()
 for i:=range names{names[i]=fmt.Sprintf("p%d",i)}
 for _,typed:=range []bool{false,true}{
  for _,limit:=range []uint64{129,430,100000}{
   p:=graphParser(limit);tp:=&TParser{Parser:p};var bestNode operatorsRuleAttempt;var bestTyped toperatorsRuleAttempt
   err:=graphFatal(func(){if typed{
    op:=&TOperatorsRuleOp{Kind:"prefix",Line:&TOperatorsRuleLine{Scope:names,M:func(p *TParser)(any,bool){for i,v:=range values{p.Frame.Vals[i]=v};p.Pos=1;return values[0],true}}}
    bestTyped,_=tp.longest([]*TOperatorsRuleOp{op})
   }else{
    op:=&OperatorsRuleOp{Kind:"prefix",Line:&OperatorsRuleLine{Scope:names,M:func(p *Parser)(*Node,bool){for i,v:=range nodes{p.Frame.Vals[i]=v};p.Pos=1;return nodes[0],true}}}
    bestNode,_=p.longest([]*OperatorsRuleOp{op})
   }})
   if limit<100000{if !errors.Is(err,ErrWorkLimit) || bestNode.op!=nil || bestTyped.op!=nil{t.Fatal("partial OperatorsRule attempt",limit,err)};if typed{tp.recycle()}else{p.release()};continue}
   if err!=nil || p.Pos!=0 || p.Frame!=nil || tp.Frame!=nil{t.Fatal("OperatorsRule selection changed state",err)}
   if typed{if bestTyped.op==nil || bestTyped.end!=1 || !reflect.DeepEqual(bestTyped.frame.Vals,values){t.Fatal("typed OperatorsRule snapshot changed")}}else{if bestNode.op==nil || bestNode.end!=1 || !reflect.DeepEqual(bestNode.frame.Vals,nodes){t.Fatal("Node OperatorsRule snapshot changed")}}
   if p.Control.stepsUsed()!=uint64(1+2*len(names)){t.Fatal("OperatorsRule snapshot copy work omitted",p.Control.stepsUsed())}
  }
 }
}

func TestDirectCaptureRollback(t *testing.T){
 input:="a"+strings.Repeat("ab",150)+"a"
TYPED_RULE_CHECK call:=func(limit uint64)(*Bag,error){var opts []ParseOption;opts=append(opts,WithWorkLimits(WorkLimits{MaxSteps:limit}));v,e:=ParseRuleAST("rollback",input,opts...);if e!=nil{return nil,e};n,ok:=v.(*Bag);if !ok{t.Fatalf("rollback output %T",v)};return n,nil}
 got,err:=call(1000000);if err!=nil || got==nil || len(got.Items)!=2 || len(got.Nested)!=1 || len(got.Nested[0])!=150 || got.Items[0].Start!=0 || got.Items[1].Start!=301{t.Fatal("partial repeat rollback changed output",got,err)}
 lo,hi:=uint64(1),uint64(1000000)
 for lo<hi{mid:=lo+(hi-lo)/2;n,e:=call(mid);if errors.Is(e,ErrWorkLimit){if n!=nil{t.Fatal("partial rollback output")};lo=mid+1}else if e==nil{hi=mid}else{t.Fatal(e)}}
 if n,e:=call(lo-1);n!=nil || !errors.Is(e,ErrWorkLimit){t.Fatal("rollback one-short",n,e)}
 retry,e:=call(lo);if e!=nil || !reflect.DeepEqual(retry,got){t.Fatal("rollback retry changed output",e)}
}
`
