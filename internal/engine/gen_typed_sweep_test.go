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

const typedSweepClosedGrammar = `
type Token = { Text string }
type ScalarOwner = { Active bool; Count int; Text string; Child *Token; Next *string }
type Pair = { Left Token; Right Token; M node[string]; Label string }
type Box = { Items []Pair; Nested [][]Token; S []string; N []int; B []bool; Opt *string; PI *int; PB *bool }
type Tree ~ Branch | Token | error
type Branch = { Left Tree; Right Tree }
type Expr ~ Bin | Token
type Bin = { Left Expr; Right Expr; Op node[string] }
rule main Box = discarded x:token => Box{Items: []Pair{Pair{Left: $x, Right: $x, Label: "b"}}, Nested: [][]Token{[]Token{$x}}, Opt: "b", PI: 7, PB: true}
rule discarded Token = t:("é"+) #atomic => Token{Text: text($t)}
rule token Token = t:("b") #atomic => Token{Text: text($t)}
rule constructed Token = discarded "b" => Token{Text: "b"}
rule match node[string] = "b" #atomic
rule discardedMatch node[string] = "é"+ #atomic
rule matches node[string] = discardedMatch y:"b" #atomic => $y
rule recovery []Tree = xs:(recoverItem #recover(skip=([^;]+ ";")))* => $xs.Value
rule recoverItem Token = t:("b;") #atomic => Token{Text: text($t)}
rule fold Tree = first:token rest:token* => foldl($first, $rest, (acc, i) => Branch{Left: acc, Right: acc})
rule lr Tree = branch / token
rule branch Branch = prev:lr "b" => Branch{Left: $prev, Right: $prev}
rule expr Expr = operators {
  operand token;
  level {
    infix left "+" => Bin{Left: $lhs, Right: $rhs, Op: $op}
  }
}
rule actionError Box = "b" => Box{PI: 1 / 0}
rule cst = "?" "?"
rule nilValue = "n" => nil
`

const typedSweepOpenGrammar = `
type Token = { Text string }
type Choice ~ Token | node[string] | error
type Mixed ~ Token | node[string] | int | error
type Record = { A any; Raw node[_]; Items []Choice; M Mixed; O *string }
rule main Record = x:"b" #atomic raw:("?" "?") => Record{A: $x, Raw: $raw, Items: []Choice{$x}, M: $x, O: "b"}
rule token Token = t:("b") #atomic => Token{Text: text($t)}
rule recovery Record = x:(token #recover(skip=([^;]+ ";"))) => Record{A: $x, M: $x, Items: []Choice{$x}}
`

const typedSweepCollisionGrammar = `
type ParseAST = { Text string }
type ParseAST_ = { Text string }
type ParseRuleAST = { Text string }
type WithMaxDepth = { Text string }
type ParseOption = { Text string }
type Span = { Text string }
type Tokens ~ []ParseAST
type Matrix ~ []Tokens
type AliasBox = { Items Tokens; Nested Matrix }
rule main AliasBox = x:token => AliasBox{Items: []ParseAST{$x}, Nested: [][]ParseAST{[]ParseAST{$x}}}
rule token ParseAST = t:("b") #atomic => ParseAST{Text: text($t)}
rule cascade ParseAST_ = t:("c") #atomic => ParseAST_{Text: text($t)}
`

// PEGO_TYPED_OUTPUT_SWEEP_DIR exports same-generator reference/prototype modules
// for coordinator-controlled validation and measurement. This test never times them.
func TestGeneratedTypedOutputSweep(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated ownership consumers")
	}
	for _, grammar := range []struct{ name, source, consumer, whitebox string }{
		{"closed", typedSweepClosedGrammar, typedSweepClosedConsumer, typedSweepClosedWhitebox + typedSweepGraphWhitebox},
		{"open", typedSweepOpenGrammar, typedSweepOpenConsumer, typedSweepOpenWhitebox},
		{"collision", typedSweepCollisionGrammar, typedSweepCollisionConsumer, ""},
	} {
		g, err := syntax.Parse(grammar.source)
		if err != nil {
			t.Fatal(err)
		}
		for _, conversion := range []bool{false, true} {
			for _, sweep := range []bool{false, true} {
				name := grammar.name + "/" + strconv.FormatBool(conversion) + "/" + strconv.FormatBool(sweep)
				t.Run(name, func(t *testing.T) {
					opts := GenOptions{Package: "sweepfixture", Start: "main", Types: true, Recognize: true,
						convertTypes: conversion, enableTypedOutputSweep: sweep}
					code, err := Generate(g, opts)
					if err != nil {
						t.Fatal(err)
					}
					again, err := Generate(g, opts)
					if err != nil || string(again) != string(code) {
						t.Fatal("generation changed", err)
					}
					if strings.Contains(string(code), "enableTypedOutputSweep") {
						t.Fatal("generation selector leaked into output")
					}
					if !sweep && (strings.Contains(string(code), "matchBlocks") || strings.Contains(string(code), "type astOutputMark struct {\n\tlive")) {
						t.Fatal("reference output acquired tracking or sweep state")
					}
					dir := t.TempDir()
					if export := os.Getenv("PEGO_TYPED_OUTPUT_SWEEP_DIR"); export != "" {
						dir = filepath.Join(export, grammar.name, strconv.FormatBool(conversion), strconv.FormatBool(sweep))
						if err := os.MkdirAll(dir, 0o755); err != nil {
							t.Fatal(err)
						}
					}
					metadata, _ := json.Marshal(opts)
					consumer := "package sweepfixture\n" + grammar.consumer + "\nconst forceConversion = " + strconv.FormatBool(conversion) + "\nconst sweepEnabled = " + strconv.FormatBool(sweep) + "\n"
					files := map[string][]byte{"parser.go": code, "consumer_test.go": []byte(consumer),
						"go.mod": []byte("module sweepfixture\n\ngo 1.27.1\n"), "grammar.pego": []byte(grammar.source),
						"options.json": metadata}
					// Keep an explicit private-control label because JSON omits private options.
					files["variant.json"] = []byte(`{"conversion":` + strconv.FormatBool(conversion) + `,"sweep":` + strconv.FormatBool(sweep) + `}`)
					// The open whitebox inspects conversion owners, which the typed runtime does not use.
					if sweep && (grammar.name != "open" || conversion) {
						whitebox := grammar.whitebox
						if grammar.name == "closed" && !conversion {
							// Every rule of the closed grammar runs on the typed runtime, so it has no
							// conversion owners; the conversion variant inspects those.
							if strings.Contains(string(code), "type astConv struct {") {
								t.Fatal("closed grammar fell back to Node conversion")
							}
							whitebox = "import \"testing\"\n" + typedSweepDirectWhitebox
						}
						files["whitebox_test.go"] = []byte("package sweepfixture\n" + whitebox)
					}
					for name, data := range files {
						if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
							t.Fatal(err)
						}
					}
					cmd := exec.Command("go", "test", "-count=1", "-run", "^TestSweep")
					cmd.Dir = dir
					if output, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("generated sweep consumer: %v\n%s", err, output)
					}
				})
			}
		}
	}
}

const typedSweepClosedConsumer = `
import (
 "reflect"
 "strings"
 "sync"
 "testing"
)
func TestSweepPublic(t *testing.T) {
 for _, unit := range []PositionUnit{CodePoints, Bytes} {
  input := strings.Repeat("é", 300)+"b"
  v,e := ParseAST(input, WithUnit(unit)); if e!=nil || v==nil {t.Fatal(e)}
  if len(v.Items)!=1 || len(v.Nested)!=1 || len(v.Nested[0])!=1 || v.Items[0].Left.Text!="b" || v.Items[0].Right.Text!="b" {t.Fatal("nested fields")}
  if !forceConversion && v.Items[0].Left != v.Items[0].Right {t.Fatal("action alias changed")}
  if cap(v.Items)!=len(v.Items) || cap(v.Nested)!=len(v.Nested) || cap(v.Nested[0])!=len(v.Nested[0]) {t.Fatal("public slice capacities")}
  if v.S!=nil || v.N!=nil || v.B!=nil || v.Opt==nil || *v.Opt!="b" || v.PI==nil || *v.PI!=7 || v.PB==nil || !*v.PB {t.Fatal("scalar/optional fields")}
  width:=1; if unit==Bytes {width=2}; if v.End!=300*width+1 || v.Items[0].Left.Start!=300*width {t.Fatal("UTF8 spans")}
  n,e:=ParseRuleAST("main",input,WithUnit(unit)); if e!=nil || !reflect.DeepEqual(v,n) {t.Fatal("named root")}
  made,e:=ParseRuleAST("constructed",input,WithUnit(unit)); if e!=nil {t.Fatal(e)}
  terminal:=made.(*Token); if terminal.Text!="b" || terminal.Start!=0 || terminal.End!=300*width+1 {t.Fatal("constructed terminal")}
  before:=*v.Items[0].Left
  _=append(v.Items,&Pair{})
  if _,e:=ParseAST("?",WithUnit(unit)); e==nil {t.Fatal("ordinary failure")}
  if _,e:=ParseAST(input,WithUnit(unit),WithMaxDepth(1)); e==nil {t.Fatal("depth failure")}
  if _,e:=ParseRuleAST("actionError","b",WithUnit(unit)); e==nil || !strings.Contains(e.Error(),"action") {t.Fatal("action failure",e)}
  m,e:=ParseRuleAST("matches",input,WithUnit(unit)); if e!=nil {t.Fatal(e)}
  match:=m.(*TNode[string]); if match.Value!="b" || match.Start!=300*width || match.End!=match.Start+1 {t.Fatal("Match owner")}
  r,e:=ParseRuleAST("recovery","b;1;b;",WithUnit(unit)); if _,ok:=e.(SyntaxErrors); !ok {t.Fatal("recovery",e)}
  tree:=r.([]Tree); if len(tree)!=3 {t.Fatal("recovery list")}; if _,ok:=tree[1].(*Error); !ok {t.Fatal("recovered union")}
  ex,e:=ParseRuleAST("expr","b+b+b",WithUnit(unit)); if e!=nil || ex.(*Bin).Op.Value!="+" || ex.(*Bin).Left==nil {t.Fatal("Operators Rule",e)}
  for _,rule:=range []string{"fold","lr"} {
   count:=6000; if forceConversion {count=6}
   result,e:=ParseRuleAST(rule,strings.Repeat("b",count),WithUnit(unit)); if e!=nil {t.Fatal(rule,e)}
   levels:=1; for x,ok:=result.(*Branch); ok; x,ok=result.(*Branch) {
    if x.Left==nil || x.Right==nil || (!forceConversion && x.Left!=x.Right) {t.Fatal("DAG identity")}; result=x.Left; levels++
   }
   if levels!=count || result.(*Token).Text!="b" {t.Fatal("DAG traversal",levels)}
  }
  if _,e:=ParseAST("éb",WithUnit(unit)); e!=nil {t.Fatal(e)}
  if *v.Items[0].Left!=before || terminal.Text!="b" || terminal.Start!=0 || terminal.End!=300*width+1 || match.Value!="b" || tree[0].(*Token).Text!="b;" || ex.(*Bin).Op.Value!="+" {t.Fatal("later parses mutated result")}
  calls:=0
  _,e=ParseRuleAST("main","éb",func(o *parseOptions){calls++; _,inner:=ParseAST("éb"); if inner!=nil {panic(inner)}; o.unit=unit})
  if e!=nil || calls!=1 {t.Fatal("options evaluated more than once")}
  zero,e:=ParseRuleAST("nilValue","n",WithUnit(unit)); if e!=nil || zero!=nil {t.Fatal("nil result",zero,e)}
 }
 var wg sync.WaitGroup
 for i:=0;i<6;i++ { wg.Add(1); go func(){defer wg.Done(); for j:=0;j<10;j++ {v,e:=ParseAST("éb"); if e!=nil || v.Items[0].Left.Text!="b" {t.Error("concurrency",e)}}}() }; wg.Wait()
}
`

const typedSweepClosedWhitebox = `
import "testing"
func TestSweepConversionInteriorPointers(t *testing.T) {
 for _,field:=range []string{"text","start","end","bool","count","ownerText","match","error"} {
  a:=&astConv{}
  tokens:=make([]Token,3);a.tToken.blocks=[][]Token{tokens}
  tokens[0]=Token{Span:Span{4,9},Text:"live"};tokens[1].Text="next";tokens[2].Text="dead"
  owners:=make([]ScalarOwner,1);a.tScalarOwner.blocks=[][]ScalarOwner{owners}
  owners[0]=ScalarOwner{Active:true,Count:42,Text:"owner",Child:&tokens[0],Next:&tokens[1].Text}
  matches:=make([]TNode[string],1);a.match.blocks=[][]TNode[string]{matches};matches[0].Value="match"
  errors:=make([]Error,1);a.error.blocks=[][]Error{errors};errors[0].Message="error"
  var root any
  switch field {case "text":root=&tokens[0].Text;case "start":root=&tokens[0].Start;case "end":root=&tokens[0].End;case "bool":root=&owners[0].Active;case "count":root=&owners[0].Count;case "ownerText":root=&owners[0].Text;case "match":root=&matches[0].Value;case "error":root=&errors[0].Message}
  a.publish(root,nil)
  switch field {case "text","start","end":if tokens[0].Text!="live" || tokens[0].Start!=4 || tokens[0].End!=9 {t.Fatal("terminal interior pointer",field)}
   case "bool","count","ownerText":if !owners[0].Active || owners[0].Count!=42 || owners[0].Text!="owner" || tokens[0].Text!="live" || tokens[1].Text!="next" {t.Fatal("parent and transitive promotion",field)}
   case "match":if matches[0].Value!="match" {t.Fatal("match interior")};case "error":if errors[0].Message!="error" {t.Fatal("error interior")}}
  if tokens[2].Text!="" {t.Fatal("eligible discarded sibling not cleared",field)}
 }
}
func TestSweepConversionOwners(t *testing.T) {
 a:=&astConv{}
 dead:=astNew(&a.tToken); dead.Text="dead"
 live:=astNew(&a.tToken); live.Text="live"
 deadMatch:=astNew(&a.match); deadMatch.Value="dead"
 keep:=astNew(&a.match); keep.Value="keep"
 pair:=astNew(&a.tPair); *pair=Pair{Left:live,Right:live,M:keep}
 a.publish(&Box{Items:[]*Pair{pair}},nil)
 if dead.Text!="" || deadMatch.Value!="" || live.Text!="live" || keep.Value!="keep" || pair.Left!=live || pair.M!=keep {t.Fatal("conversion owners")}
}
`

const typedSweepDirectWhitebox = `
func TestSweepDirectInteriorPointers(t *testing.T) {
 for _,field:=range []string{"text","start","end","bool","count","ownerText","matchText","matchStart","matchEnd"} {
  a:=&tslabs{};p:=&tparser{parser:&parser{}}
  tokens:=make([]Token,3);a.tToken.blocks=[][]Token{tokens}
  tokens[0]=Token{Span:Span{4,9},Text:"live"};tokens[1].Text="next";tokens[2].Text="dead"
  owners:=make([]ScalarOwner,1);a.tScalarOwner.blocks=[][]ScalarOwner{owners}
  owners[0]=ScalarOwner{Active:true,Count:42,Text:"owner",Child:&tokens[0],Next:&tokens[1].Text}
  matches:=make([]tmatch,2);p.matchBlocks=[][]tmatch{matches};matches[0].TNode=TNode[string]{Span:Span{2,7},Value:"match"};matches[1].Value="dead"
  var root any
  switch field {case "text":root=&tokens[0].Text;case "start":root=&tokens[0].Start;case "end":root=&tokens[0].End;case "bool":root=&owners[0].Active;case "count":root=&owners[0].Count;case "ownerText":root=&owners[0].Text;case "matchText":root=&matches[0].Value;case "matchStart":root=&matches[0].Start;case "matchEnd":root=&matches[0].End}
  a.publish(root,nil,p)
  switch field {case "text","start","end":if tokens[0].Text!="live" || tokens[0].Start!=4 || tokens[0].End!=9 {t.Fatal("terminal interior pointer",field)}
   case "bool","count","ownerText":if !owners[0].Active || owners[0].Count!=42 || owners[0].Text!="owner" || tokens[0].Text!="live" || tokens[1].Text!="next" {t.Fatal("parent and transitive promotion",field)}
   default:if matches[0].Value!="match" || matches[0].Start!=2 || matches[0].End!=7 {t.Fatal("direct match interior",field)}}
  if tokens[2].Text!="" || matches[1].Value!="" {t.Fatal("eligible discarded sibling not cleared",field)}
 }
}
func TestSweepOwnedGraph(t *testing.T) {
 a:=&tslabs{}
 dead:=astNew(&a.tToken); *dead=Token{Text:"discarded large text"}
 live:=astNew(&a.tToken); *live=Token{Text:"live"}
 pair:=astNew(&a.tPair); *pair=Pair{Left:live,Right:live,Label:"pair"}
 p:=&tparser{parser:&parser{}}
 deadMatch:=p.newMatch(0,100,"discarded match",true)
 keep:=p.newMatch(100,101,"m",true); pair.M=&keep.TNode
 root:=&Box{Items:[]*Pair{pair},Nested:[][]*Token{{live}}}
 a.publish(root,nil,p)
 if dead.Text!="" || deadMatch.Value!="" {t.Fatal("unreachable object/Match not cleared")}
 if live.Text!="live" || pair.Left!=live || pair.Right!=live || pair.M!=&keep.TNode || keep.Value!="m" {t.Fatal("reachable alias cleared")}
 if len(a.tToken.blocks)!=0 || len(p.matchBlocks)!=0 {t.Fatal("ownership directories retained")}
}
`

const typedSweepGraphWhitebox = `
func TestSweepCellVsPointee(t *testing.T) {
 var tokens astChunk[Token]
 dead:=astNew(&tokens); dead.Text="dead"
 live:=astNew(&tokens); live.Text="live"
 var cells astChunk[*Token]
 discarded:=astSlice(&cells,1); discarded[0]=dead
 visible:=astSlice(&cells,3); visible[0]=live; visible[1]=live; visible[2]=live
 root:=&Box{Nested:[][]*Token{visible[:1:2],visible[1:2:3]}}
 m:=&astOutputMark{live:make(map[any]struct{})}; m.run(root,nil)
 astSweepChunk(&cells,m.live); astSweepChunk(&tokens,m.live)
 if discarded[0]!=nil || dead.Text!="" {t.Fatal("dead cell/pointee survives")}
 if visible[0]!=live || visible[1]!=live || visible[2]!=live || live.Text!="live" {t.Fatal("capacity/overlapping alias cleared")}
 // Marking a pointee cannot accidentally protect an unreachable neighboring cell.
 var separate astChunk[*Token]; onlyCell:=astSlice(&separate,1); onlyCell[0]=live
 astSweepChunk(&separate,m.live); if onlyCell[0]!=nil || live.Text!="live" {t.Fatal("cell/object keys conflated")}
 var ints astChunk[int]; values:=astSlice(&ints,4); copy(values,[]int{90,11,12,13})
 root.N=values[1:2:3]; root.PI=&values[3]
 m=&astOutputMark{live:make(map[any]struct{})}; m.run(root,nil); astSweepChunk(&ints,m.live)
 if values[0]!=0 || values[1]!=11 || values[2]!=12 || values[3]!=13 {t.Fatal("scalar cells/optional alias",values)}
}
func TestSweepCyclicKnownGraph(t *testing.T) {
 a:=&astConv{}
 live:=astNew(&a.tToken); live.Text="live"
 dead:=astNew(&a.tToken); dead.Text="dead"
 n:=&Node{}; n.children=[]*Node{n}; n.fields=Fields{{Name:"value",Value:live},{Name:"cycle",Value:n}}
 a.publish(n,nil)
 if live.Text!="live" || dead.Text!="" || n.children[0]!=n || n.fields[0].Value!=live {t.Fatal("cyclic CST graph")}
 branch:=&Branch{Left:live,Right:live}
 for i:=0;i<10000;i++ {branch=&Branch{Left:branch,Right:branch}}
 m:=&astOutputMark{live:make(map[any]struct{})}; m.run(branch,nil)
 if m.keepAll || len(m.live)<10000 {t.Fatal("iterative DAG walk")}
}
func TestSweepOpaqueFallback(t *testing.T) {
 for _,opaque:=range []func(*Token)any{func(v *Token)any{return map[string]any{"hidden":v}},func(v *Token)any{c:=make(chan *Token,1);c<-v;return c},func(v *Token)any{return func()*Token{return v}},func(v *Token)any{return struct{Hidden *Token}{v}}} {
  a:=&astConv{}; token:=astNew(&a.tToken); token.Text="preserve"
  known:=astNew(&a.tToken); known.Text="known"
  n:=&Node{fields:Fields{{Name:"known",Value:known},{Name:"opaque",Value:opaque(token)}}}
  a.publish(n,nil); if token.Text!="preserve" || known.Text!="known" {t.Fatal("partial clearing on opaque graph")}
 }
 // A typed nil never reaches the generic opaque fallback.
 m:=&astOutputMark{live:make(map[any]struct{})}; m.run((*Token)(nil),nil); if m.keepAll {t.Fatal("typed nil")}
}
`

const typedSweepOpenConsumer = `
import (
 "reflect"
 "testing"
)
func TestSweepOpenValues(t *testing.T) {
 for _,unit:=range []PositionUnit{CodePoints,Bytes} {
  v,e:=ParseAST("b??",WithUnit(unit)); if e!=nil || v==nil || v.Raw==nil || v.O==nil || *v.O!="b" {t.Fatal("open root",e)}
  if v.A.(*TNode[string]).Value!="b" || v.Items[0].(*TNode[string]).Value!="b" || v.M.(*TNode[string]).Value!="b" {t.Fatal("open conversion")}
  named,e:=ParseRuleAST("main","b??",WithUnit(unit)); if e!=nil || !reflect.DeepEqual(v,named) {t.Fatal("named conversion")}
  recovered,e:=ParseRuleAST("recovery","1;",WithUnit(unit)); if _,ok:=e.(SyntaxErrors); !ok {t.Fatal("open recovery",e)}
  r:=recovered.(*Record); if r.A.(*Error).Text!="1;" || r.M.(*Error).Text!="1;" || r.Items[0].(*Error).Text!="1;" {t.Fatal("open recovered values")}
  if _,e:=ParseAST("b",WithUnit(unit)); e==nil {t.Fatal("failed input")}
  if v.A.(*TNode[string]).Value!="b" || v.Raw.(*TNode[any]).Value.([]any)[0].(*TNode[string]).Value!="?" || r.A.(*Error).Text!="1;" {t.Fatal("conversion ownership")}
 }
}
`

const typedSweepOpenWhitebox = `
import "testing"
func TestSweepOpenCells(t *testing.T) {
 a:=&astConv{}; live:=astNew(&a.tToken); live.Text="live"; dead:=astNew(&a.tToken); dead.Text="dead"
 raw:=&TNode[any]{}; raw.Value=[]any{live,raw}
 root:=&Record{A:live,Raw:raw,M:live,Items:[]Choice{live}}
 a.publish(root,nil)
 if dead.Text!="" || root.A!=live || root.M!=live || root.Items[0]!=live || raw.Value.([]any)[0]!=live || live.Text!="live" {t.Fatal("any/CST/union aliases")}
 for _,unknown:=range []func(*Token)any{func(v *Token)any{return map[string]any{"hidden":v}},func(v *Token)any{return struct{Hidden *Token}{v}}} {
  a:=&astConv{}; value:=astNew(&a.tToken); value.Text="must stay"
  known:=astNew(&a.tToken);known.Text="known"
  a.publish(&Record{A:unknown(value),M:known},nil); if value.Text!="must stay" || known.Text!="known" {t.Fatal("unknown any partial clearing")}
 }
}
`

const typedSweepCollisionConsumer = `
import (
 "reflect"
 "testing"
)
func TestSweepNamesAndAliases(t *testing.T) {
 for _,unit:=range []PositionUnit{CodePoints,Bytes} {
  v,e:=ParseAST("b",WithUnit(unit)); if e!=nil || v==nil || len(v.Items)!=1 || len(v.Nested)!=1 || len(v.Nested[0])!=1 {t.Fatal("alias lists",e)}
  if v.Items[0].Text!="b" || v.Nested[0][0].Text!="b" {t.Fatal("alias contents")}
  if !forceConversion && v.Items[0]!=v.Nested[0][0] {t.Fatal("alias identity")}
  if reflect.TypeOf(v.Items[0]).Elem().Name()!="ParseAST_" {t.Fatal("public runtime name collision")}
  x,e:=ParseRuleAST("cascade","c",WithUnit(unit)); if e!=nil {t.Fatal(e)}
  if reflect.TypeOf(x).Elem().Name()!="ParseAST__" {t.Fatal("name collision cascade")}
  _=append(v.Items,nil); if _,e:=ParseAST("?",WithUnit(unit));e==nil {t.Fatal("expected error")}
  if v.Items[0].Text!="b" || v.Nested[0][0].Text!="b" {t.Fatal("alias ownership")}
 }
}
`

// Template drift must fail generation instead of silently losing ownership.
func TestTypedOutputSweepAnchors(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		rewrite      func(string) (string, error)
		anchors      []string
	}{
		{"slabs", astHelpers, typedOutputSweepHelpers, []string{"\tnext int\n", "\ts.free = make([]T, size)\n"}},
		{"runtime", typedSource, typedOutputSweepRuntime, []string{"\tres = conv(v)\n\to.control.check()", "\tmatches  []tmatch // returned in values, so not kept for the next parse", "\t\tp.matches = make([]tmatch, nodeChunk)\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.rewrite(tc.source); err != nil {
				t.Fatal(err)
			}
			for _, anchor := range tc.anchors {
				for _, drift := range []string{strings.Replace(tc.source, anchor, "", 1), tc.source + anchor} {
					if _, err := tc.rewrite(drift); err == nil {
						t.Fatalf("accepted template drift for %q", anchor)
					}
				}
			}
		})
	}
}
