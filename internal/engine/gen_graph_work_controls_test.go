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
	g, err := syntax.Parse(`type Leaf terminal

type Bag = {
    Items  []Leaf
    Nested [][]Leaf
}

rule main Bag = x:leaf* #cut => Bag{Items: $x, Nested: list($x, $x)}

rule leaf Leaf = "a"

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
			for _, reference := range []bool{false, true} {
				pkg := fmt.Sprintf("graph_%v_%v_%v", convert, general, reference)
				code, err := Generate(g, GenOptions{Package: pkg, Start: "main", Types: true, Recognize: true, convertTypes: convert, disableTypedCuts: general, referenceWorkControls: reference})
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(dir, pkg)
				if err := os.Mkdir(path, 0755); err != nil {
					t.Fatal(err)
				}
				consumer := strings.NewReplacer("PACKAGE", pkg, "REFERENCE", fmt.Sprint(reference), "GENERAL", fmt.Sprint(general), "CONVERT", fmt.Sprint(convert)).Replace(generatedGoGraphConsumer)
				for name, data := range map[string][]byte{"parser.go": code, "graph_test.go": []byte(consumer)} {
					if err := os.WriteFile(filepath.Join(path, name), data, 0644); err != nil {
						t.Fatal(err)
					}
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

const generatedGoGraphConsumer = `package PACKAGE
import("context";"errors";"fmt";"reflect";"strings";"testing")
const reference = REFERENCE
const conversion = CONVERT
const general = GENERAL
func graphParser(limit uint64)*parser{return &parser{control:newWorkControl(nil,WorkLimits{MaxSteps:limit})}}
func graphFatal(f func())(err error){defer func(){if x:=recover();x!=nil{v,ok:=x.(fatal);if !ok{panic(x)};err=v.err}}();f();return nil}
func requireGraphLimit(t *testing.T,p *parser,f func()){
 t.Helper();err:=graphFatal(f)
 if reference {if err!=nil || p.control.stepsUsed!=0{t.Fatal("reference charged",err)};return}
 if !errors.Is(err,ErrWorkLimit){t.Fatal("operation escaped work budget",err)}
 if p.control.inputUsed!=0{t.Fatal("graph work charged input")}
}
func graphKids()([]*Node,[]any){
 nodes:=make([]*Node,300);values:=make([]any,300)
 for i:=range nodes{nodes[i]=&Node{Start:int32(i+3),End:int32(i+4),Text:"a"};values[i]=&tmatch{Span:Span{Start:i+3,End:i+4},Text:"a"}}
 return nodes,values
}
func TestGraphFields(t *testing.T){
 nodes,_:=graphKids();fs:=make(Fields,300);tn:=&tnode{typ:"Seq",fields:make([]tfield,300)}
 for i:=range fs{name:=fmt.Sprintf("field%d",i);fs[i]=NodeField{name,i};tn.fields[i]=tfield{name,i}}
 for _,typed:=range []bool{false,true}{
  p:=graphParser(32);tp:=&tparser{parser:p};node:=&Node{Fields:fs}
  requireGraphLimit(t,p,func(){if typed{(&tctx{p:tp}).member(tn,"absent-field-with-distinct-length")}else{(&actx{p:p}).member(node,"absent-field-with-distinct-length")}})
  p.control=newWorkControl(nil,WorkLimits{MaxSteps:100000})
  var got any;if typed{got=(&tctx{p:tp}).member(tn,"field299")}else{got=(&actx{p:p}).member(node,"field299")}
  if got!=299{t.Fatal("field read changed",got)}
  p.control=newWorkControl(nil,WorkLimits{MaxSteps:32})
  requireGraphLimit(t,p,func(){if typed{tp.setValueField(tn,"field299",nodes[0])}else{p.setValueField(&fs,"field299",nodes[0])}})
  if !reference {if typed && tn.fields[299].val!=299 || !typed && fs[299].Value!=299{t.Fatal("failed setter changed value")}}
  p.control=newWorkControl(nil,WorkLimits{MaxSteps:100000})
  if typed{tp.setValueField(tn,"field299",nodes[0]);if len(tn.fields)!=300 || tn.fields[299].val!=nodes[0]{t.Fatal("typed replacement")};tn.fields[299].val=299}else{p.setValueField(&fs,"field299",nodes[0]);if len(fs)!=300 || fs[299].Value!=nodes[0]{t.Fatal("Node replacement")};fs[299].Value=299}
 }
}
func TestGraphListsAndOwnership(t *testing.T){
 nodes,values:=graphKids()
 for _,typed:=range []bool{false,true}{
  for _,spanOnly:=range []bool{false,true}{
   p:=graphParser(129);tp:=&tparser{parser:p};var result any
   requireGraphLimit(t,p,func(){if typed{c:=&tctx{p:tp};if spanOnly{result=c.listNode(values)}else{result=c.newList(values)}}else{c:=&actx{p:p};if spanOnly{result=c.listNode(nodes)}else{result=c.newList(nodes)}}})
   if !reference && result!=nil{t.Fatal("partial list returned")}
   p.control=newWorkControl(nil,WorkLimits{MaxSteps:100000})
   if typed{n:=(&tctx{p:tp}).newList(values).(*tnode);if n.Start!=3 || n.End!=303 || len(n.kids)!=300{t.Fatal("typed list spans")};n.kids[0]=nil;if values[0]==nil{t.Fatal("typed list aliases source")}}else{n:=(&actx{p:p}).newList(nodes);if n.Start!=3 || n.End!=303 || len(n.Children)!=300{t.Fatal("Node list spans")};n.Children[0]=nil;if nodes[0]==nil{t.Fatal("Node list aliases source")}}
  }
  p:=graphParser(129);tp:=&tparser{parser:p}
  requireGraphLimit(t,p,func(){if typed{tp.kidStack=append([]any(nil),values...);tp.kids(0)}else{p.kidStack=append([]*Node(nil),nodes...);p.kids(0)}})
  p.control=newWorkControl(nil,WorkLimits{MaxSteps:100000})
  if typed{tp.kidStack=append([]any(nil),values...);got:=tp.kids(0);if len(got)!=300{t.Fatal("typed kids")};got[0]=nil;if values[0]==nil{t.Fatal("typed children alias")}}else{p.kidStack=append([]*Node(nil),nodes...);got:=p.kids(0);if len(got)!=300{t.Fatal("Node kids")};got[0]=nil;if nodes[0]==nil{t.Fatal("Node children alias")}}
 }
}
func TestGraphCaptureUndoAndCleanup(t *testing.T){
 for _,typed:=range []bool{false,true}{
  p:=graphParser(32);tp:=&tparser{parser:p}
  n:=&Node{Text:"old"};changed:=&Node{Text:"new"};f:=&frame{vals:make([]*Node,300)};tf:=&tframe{vals:make([]any,300)}
  for i:=0;i<300;i++{f.vals[i]=changed;tf.vals[i]=changed;p.trail=append(p.trail,undo{f:f,slot:i,old:n});tp.trail=append(tp.trail,tundo{f:tf,slot:i,old:n})}
  requireGraphLimit(t,p,func(){if typed{tp.reset(mark{})}else{p.reset(mark{})}})
  if !reference {if typed && len(tp.trail)!=300 || !typed && len(p.trail)!=300{t.Fatal("fatal lost undo records")}}
  p.control=newWorkControl(nil,WorkLimits{MaxSteps:100000})
  if typed{tp.reset(mark{});for _,v:=range tf.vals{if v!=n{t.Fatal("typed undo retry")}};if len(tp.trail)!=0{t.Fatal("typed trail retained")};tp.trail=append(tp.trail,tundo{f:tf,old:n});tp.control=newWorkControl(nil,WorkLimits{MaxSteps:1});tp.recycle();for _,v:=range tp.trail[:cap(tp.trail)]{if v.f!=nil || v.old!=nil{t.Fatal("typed cleanup blocked")}}}else{p.reset(mark{});for _,v:=range f.vals{if v!=n{t.Fatal("Node undo retry")}};if len(p.trail)!=0{t.Fatal("Node trail retained")};p.trail=append(p.trail,undo{f:f,old:n});p.control=newWorkControl(nil,WorkLimits{MaxSteps:1});p.release();for _,v:=range p.trail[:cap(p.trail)]{if v.f!=nil || v.old!=nil{t.Fatal("Node cleanup blocked")}}}
 }
}
func TestTypedFieldMaterialization(t *testing.T){
 bag:=&Bag{Items:make([]*Leaf,300),Nested:make([][]*Leaf,300)}
 for i:=range bag.Items{bag.Items[i]=&Leaf{Span:Span{Start:i+3,End:i+4},Text:"a"};bag.Nested[i]=[]*Leaf{bag.Items[i]}}
 for _,name:=range []string{"Items","Nested"}{
  p:=graphParser(32);tp:=&tparser{parser:p};var result any
  requireGraphLimit(t,p,func(){result=(&tctx{p:tp}).member(bag,name)})
  if !reference && result!=nil{t.Fatal("partial field published")}
  p.control=newWorkControl(nil,WorkLimits{MaxSteps:100000})
  got:=(&tctx{p:tp}).member(bag,name).(*tnode);ordinary,_:=bag.tfield(name)
  want:=ordinary.(*tnode)
  if !reflect.DeepEqual(got,want) || got.Start!=3 || got.End!=303{t.Fatal("field materialization changed",name)}
  if !reference {wantSteps:=uint64(2*len(bag.Items));if name=="Nested"{wantSteps+=uint64(2*len(bag.Nested))};if p.control.stepsUsed!=wantSteps{t.Fatal("list field work omitted",name,p.control.stepsUsed,wantSteps)}}
  if name=="Items"{got.kids[0]=nil;if bag.Items[0]==nil{t.Fatal("field result aliases input")}}
 }
 p:=graphParser(32);tp:=&tparser{parser:p};empty:=&Bag{Items:[]*Leaf{},Nested:nil}
 if got:=(&tctx{p:tp}).member(empty,"Items");got==nil || len(got.(*tnode).kids)!=0{t.Fatal("empty field changed")}
 if got:=(&tctx{p:tp}).member(empty,"Nested");got!=nil{t.Fatal("nil field changed")}
}
func TestGraphFoldOrderAndBudget(t *testing.T){
 nodes,values:=graphKids()
 for _,typed:=range []bool{false,true}{for _,right:=range []bool{false,true}{
  p:=graphParser(32);tp:=&tparser{parser:p};visits:=0
  f:=func(acc,item any)any{visits++;return item}
  requireGraphLimit(t,p,func(){if typed{(&tctx{p:tp}).fold("foldl",right,nil,&tnode{typ:"List",kids:values},f)}else{(&actx{p:p}).fold("foldl",right,nil,&Node{kind:kindList,Children:nodes},f)}})
  if !reference && visits!=32{t.Fatal("fold advanced past budget",visits)}
  p.control=newWorkControl(nil,WorkLimits{MaxSteps:100000});visits=0
  var got any;if typed{got=(&tctx{p:tp}).fold("foldl",right,nil,&tnode{typ:"List",kids:values},f)}else{got=(&actx{p:p}).fold("foldl",right,nil,&Node{kind:kindList,Children:nodes},f)}
  index:=299;if right{index=0};want:=any(nodes[index]);if typed{want=values[index]};if visits!=300 || got!=want{t.Fatal("fold order changed",visits)}
 }}
}
func TestGraphPublicLimitsAndRetainedResults(t *testing.T){
 input:=strings.Repeat("a",150)
 baseline,err:=ParseAST(input);if err!=nil || baseline==nil || len(baseline.Items)!=150 || len(baseline.Nested)!=2{t.Fatal("typed baseline",err)}
 before:=fmt.Sprintf("%#v",baseline.Items[0]);tree,err:=Parse(input);if err!=nil{t.Fatal(err)};treeBefore:=tree.String()
 if reference{return}
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
  p:=graphParser(32);tp:=&tparser{parser:p};var result any
  requireGraphLimit(t,p,func(){if typed{result=tp.attachCaptures(nil,names,values,3,303)}else{result=p.attachCaptures(nil,names,&frame{vals:nodes},3,303)}})
  if !reference && result!=nil{t.Fatal("partial capture node published")}
  p.control=newWorkControl(nil,WorkLimits{MaxSteps:10000000})
  if typed{n:=tp.attachCaptures(nil,names,values,3,303).(*tnode);if len(n.fields)!=299 || n.fields[0].val!=values[299] || n.fields[1].name!=names[1]{t.Fatal("typed capture order/replacement")}}else{n:=p.attachCaptures(nil,names,&frame{vals:nodes},3,303);if len(n.Fields)!=299 || n.Fields[0].Value!=nodes[299] || n.Fields[1].Name!=names[1]{t.Fatal("Node capture order/replacement")}}
 }
}
func TestGraphOperatorsRuleSnapshots(t *testing.T){
 names:=make([]string,300);nodes,values:=graphKids()
 for i:=range names{names[i]=fmt.Sprintf("p%d",i)}
 for _,typed:=range []bool{false,true}{
  for _,limit:=range []uint64{129,430,100000}{
   p:=graphParser(limit);tp:=&tparser{parser:p};var bestNode operatorsRuleAttempt;var bestTyped toperatorsRuleAttempt
   err:=graphFatal(func(){if typed{
    op:=&toperatorsRuleOp{kind:"prefix",line:&toperatorsRuleLine{scope:names,m:func(p *tparser)(any,bool){for i,v:=range values{p.frame.vals[i]=v};p.pos=1;return values[0],true}}}
    bestTyped,_=tp.longest([]*toperatorsRuleOp{op})
   }else{
    op:=&operatorsRuleOp{kind:"prefix",line:&operatorsRuleLine{scope:names,m:func(p *parser)(*Node,bool){for i,v:=range nodes{p.frame.vals[i]=v};p.pos=1;return nodes[0],true}}}
    bestNode,_=p.longest([]*operatorsRuleOp{op})
   }})
   if !reference && limit<100000{if !errors.Is(err,ErrWorkLimit) || bestNode.op!=nil || bestTyped.op!=nil{t.Fatal("partial OperatorsRule attempt",limit,err)};if typed{tp.recycle()}else{p.release()};continue}
   if err!=nil || p.pos!=0 || p.frame!=nil || tp.frame!=nil{t.Fatal("OperatorsRule selection changed state",err)}
   if typed{if bestTyped.op==nil || bestTyped.end!=1 || !reflect.DeepEqual(bestTyped.frame.vals,values){t.Fatal("typed OperatorsRule snapshot changed")}}else{if bestNode.op==nil || bestNode.end!=1 || !reflect.DeepEqual(bestNode.frame.vals,nodes){t.Fatal("Node OperatorsRule snapshot changed")}}
   if !reference && p.control.stepsUsed!=uint64(1+2*len(names)){t.Fatal("OperatorsRule snapshot copy work omitted",p.control.stepsUsed)}
  }
 }
}

func TestDirectCaptureRollback(t *testing.T){
 input:="a"+strings.Repeat("ab",150)+"a"
 if !conversion{for _,name:=range []string{"main","rollback"}{found:=false;for _,r:=range trules{if r.name==name{found=true;if general && r.body==nil{t.Fatal("fixture did not exercise a general typed body",name)};if !general && (r.entry==nil || r.body!=nil){t.Fatal("fixture did not exercise a direct typed body",name)}}};if !found{t.Fatal("missing typed rule",name)}}}
 call:=func(limit uint64)(*Bag,error){var opts []ParseOption;if !reference{opts=append(opts,WithWorkLimits(WorkLimits{MaxSteps:limit}))};v,e:=ParseRuleAST("rollback",input,opts...);if e!=nil{return nil,e};n,ok:=v.(*Bag);if !ok{t.Fatalf("rollback output %T",v)};return n,nil}
 got,err:=call(1000000);if err!=nil || got==nil || len(got.Items)!=2 || len(got.Nested)!=1 || len(got.Nested[0])!=150 || got.Items[0].Start!=0 || got.Items[1].Start!=301{t.Fatal("partial repeat rollback changed output",got,err)}
 if reference{return}
 lo,hi:=uint64(1),uint64(1000000)
 for lo<hi{mid:=lo+(hi-lo)/2;n,e:=call(mid);if errors.Is(e,ErrWorkLimit){if n!=nil{t.Fatal("partial rollback output")};lo=mid+1}else if e==nil{hi=mid}else{t.Fatal(e)}}
 if n,e:=call(lo-1);n!=nil || !errors.Is(e,ErrWorkLimit){t.Fatal("rollback one-short",n,e)}
 retry,e:=call(lo);if e!=nil || !reflect.DeepEqual(retry,got){t.Fatal("rollback retry changed output",e)}
}
`
