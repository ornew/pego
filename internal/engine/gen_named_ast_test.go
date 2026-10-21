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

const namedASTGrammar = `type T = { Text string }
type S = { T T }
type U ~ S | T
type Alias ~ T
type Mixed ~ S | node[_]
type Number ~ int
type Values = { N Number; Text string; Flag bool }
type Recovery = { T T | error }
type Items ~ []S
type ParseRuleAST = { T T }
type ParseRuleAST_ ~ ParseRuleAST
rule main = t:term => ParseRuleAST{T: $t}
rule term T = t:("é") #atomic => T{Text: text($t)}
rule aliased Alias = t:("é") #atomic => T{Text: text($t)}
rule mixed Mixed = cst
rule structure = t:term => S{T: $t}
rule values = "v" => Values{N: 42, Text: "yes", Flag: true}
rule optional = t:term? => $t
rule items Items = xs:(s:structure)* => map($xs, (x) => x.$s)
rule union U = structure / term
rule cst = "x" "y"
rule open node[_] = "z"
rule arbitrary _ = "a" => S{}
rule abstract node[string] = "t"
rule recursive S = "(" s:recursive ")" / s:structure => $s
rule abort = x:"a"* #atomic => Values{N: 10 / len($x)}
rule recoverStruct S | error = structure #recover(skip="?")
rule recoverTerminal = t:term #recover(skip="?") => Recovery{T: $t}
rule recoverUnion U | error = union #recover(skip="?")
rule nonemptyItems Items = xs:(s:structure)+ => map($xs, (x) => x.$s)
rule recoverList Items | error = nonemptyItems #recover(skip="?")
rule recoveryElements [](S | error) = xs:(s:structure #recover(skip="?"))+ => map($xs, (x) => x.$s)
`

func TestGeneratedNamedAST(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumer fixtures")
	}
	for _, variant := range []struct {
		name, extra, start string
		force              bool
	}{
		{"direct", "", "main", false}, {"conversion", "", "main", true},
		{"global-field", "type Unrelated = { Value node[_] }\n", "main", false},
		{"field-read", "rule fieldRead = s:structure => $s.T\n", "main", false},
		{"mixed-start", "", "cst", false},
	} {
		t.Run(variant.name, func(t *testing.T) {
			g, err := syntax.Parse(namedASTGrammar + variant.extra)
			if err != nil {
				t.Fatal(err)
			}
			code, err := generateCode(g, GenOptions{Package: "named", Start: variant.start, Types: true, convertTypes: variant.force, MaxDepth: 32})
			if err != nil {
				t.Fatal(err)
			}
			named := string(code)[strings.Index(string(code), "func ParseRuleAST("):]
			scalar := named[strings.Index(named, `case "structure":`):]
			scalar = scalar[:strings.Index(scalar, "\n\tcase")]
			direct := strings.Contains(scalar, "TParseResolved")
			// An open field type (global-field) keeps the typed runtime; forced conversion and
			// reading a field back do not.
			if direct != (variant.name != "conversion" && variant.name != "field-read") {
				t.Fatalf("wrong full runtime guard for structure: %s", scalar)
			}
			// The new reserved identifier uses the existing sorted underscore cascade.
			if !strings.Contains(string(code), "type ParseRuleAST_ struct") || !strings.Contains(string(code), "type ParseRuleAST__ = *ParseRuleAST_") {
				t.Fatal("missing deterministic identifier migration")
			}
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "named"), 0755); err != nil {
				t.Fatal(err)
			}
			for name, data := range map[string]string{"go.mod": testGoMod("namedfixture"), "named/parser.go": string(code), "named_test.go": strings.NewReplacer("START_CST", fmt.Sprint(variant.start == "cst"), "FIELD_READ", fmt.Sprint(variant.name == "field-read"), "START_TYPE", map[bool]string{true: "*p.TNode[p.Tuple2[*p.TNode[string], *p.TNode[string]]]", false: "*p.ParseRuleAST_"}[variant.start == "cst"]).Replace(namedASTConsumer)} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0644); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("go", "test", "-count=1", "-v", "./...")
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
		})
	}
}

func TestGeneratedNamedASTUntypedAndTS(t *testing.T) {
	g, err := syntax.Parse(namedASTGrammar)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := generateCode(g, GenOptions{Package: "plain", Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "func ParseRuleAST(") {
		t.Fatal("typed entry point in untyped parser")
	}
	ts, err := GenerateTS(g, GenOptions{Package: "plain", Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(ts), "function ParseRuleAST(") {
		t.Fatal("Go API leaked into TypeScript")
	}
}

// TestGeneratedNamedASTScalarResults checks that a rule producing an int, string or bool gets that
// type as its typed result, and that a node rule declared as an int is still rejected.
func TestGeneratedNamedASTScalarResults(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{`rule main = "a" => 1`, "(int, error)"},
		{`rule main = "a" => "s"`, "(string, error)"},
		{`rule main = "a" => true`, "(bool, error)"},
	} {
		g, err := syntax.Parse(tc.source)
		if err != nil {
			t.Fatal(err)
		}
		out, err := generateCode(g, GenOptions{Package: "named", Start: "main", Types: true})
		if err != nil {
			t.Fatalf("%q: %v", tc.source, err)
		}
		if want := "func ParseAST(input string, opts ...pegort.ParseOption) " + tc.want; !strings.Contains(string(out), want) {
			t.Errorf("%q: no %q", tc.source, want)
		}
	}
	g, err := syntax.Parse(`rule main int = "a"`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = generateCode(g, GenOptions{Package: "named", Start: "main", Types: true}); err == nil || !strings.Contains(err.Error(), "not assignable to int") {
		t.Fatalf("got %v", err)
	}
}

const namedASTConsumer = `package named_test
import("encoding/json";"fmt";"reflect";"strings";"sync";"testing";p "namedfixture/named")
func jsonValue(v any)string {b,err:=json.Marshal(v);if err!=nil {panic(err)};return string(b)}
func TestNamedMappings(t *testing.T) {
 for _,unit:=range []p.PositionUnit{p.CodePoints,p.Bytes} {
  end:=1;if unit==p.Bytes {end=2}
  if FIELD_READ {v,err:=p.ParseRuleAST("fieldRead","é",p.WithUnit(unit));if err!=nil||v.(*p.T).End!=end {t.Fatal(v,err)}}
  for _,c:=range []struct{name,input string;kind reflect.Kind}{
   {"main","é",reflect.Pointer},{"term","é",reflect.Pointer},{"aliased","é",reflect.Pointer},{"mixed","xy",reflect.Pointer},{"structure","é",reflect.Pointer},
   {"values","v",reflect.Pointer},
   {"optional","",reflect.Pointer},{"optional","é",reflect.Pointer},{"items","éé",reflect.Slice},
   {"union","é",reflect.Pointer},{"cst","xy",reflect.Pointer},{"open","z",reflect.Pointer},
   {"arbitrary","a",reflect.Pointer},{"abstract","t",reflect.Pointer},
  } {
   v,err:=p.ParseRuleAST(c.name,c.input,p.WithUnit(unit));if err!=nil {t.Fatalf("%s: %v",c.name,err)}
   if reflect.TypeOf(v)==nil||reflect.TypeOf(v).Kind()!=c.kind {t.Fatalf("%s wrong mapped type %T",c.name,v)}
   switch c.name {
   case "main":x:=v.(*p.ParseRuleAST_);var alias p.ParseRuleAST__=x;if alias.T.Text!="é" {t.Fatal(alias)}
   case "term":x:=v.(*p.T);if x.End!=end||x.Text!="é" {t.Fatal(x)}
   case "aliased":var a p.Alias=v.(*p.T);if a.End!=end {t.Fatal(a)}
   case "mixed":var m p.Mixed=v.(*p.TNode[any]);if m==nil {t.Fatal(m)}
   case "structure":if v.(*p.S).T.End!=end {t.Fatal(v)}
   case "values":x:=v.(*p.Values);var n p.Number=x.N;if n!=42||x.Text!="yes"||!x.Flag {t.Fatal(x)}
   case "items":var xs p.Items=v.([]*p.S);if len(xs)!=2||xs[1].T.Start!=end {t.Fatal(xs)}
   case "union":var u p.U=v.(*p.S);if u==nil {t.Fatal(u)}
   case "cst":x,ok:=v.(*p.TNode[p.Tuple2[*p.TNode[string],*p.TNode[string]]]);if !ok||x.Value.E0.Value!="x"||x.Value.E1.Value!="y"||x.Value.E1.Start!=1 {t.Fatalf("cst: %T %v",v,v)}
   case "open":x,ok:=v.(*p.TNode[any]);if !ok||x.Value!="z" {t.Fatalf("%s: %T",c.name,v)}
   case "abstract":if _,ok:=v.(*p.TNode[string]);!ok {t.Fatalf("%s: %T",c.name,v)}
   }
   if _,err=p.ParseRule(c.name,c.input,p.WithUnit(unit));err!=nil {t.Fatal(err)}
   if _,err=p.ParseRuleAST(c.name,c.input+"!",p.WithUnit(unit));err==nil {t.Fatal("trailing input accepted")}
  }
  for _,c:=range []struct{name string;zero any}{
   {"main",(*p.ParseRuleAST_)(nil)},{"term",(*p.T)(nil)},{"structure",(*p.S)(nil)},
   {"values",(*p.Values)(nil)},{"items",[]*p.S(nil)},
   {"union",nil},{"mixed",nil},{"aliased",(*p.T)(nil)},{"cst",(*p.TNode[p.Tuple2[*p.TNode[string],*p.TNode[string]]])(nil)},{"arbitrary",nil},{"open",(*p.TNode[any])(nil)},{"abstract",(*p.TNode[string])(nil)},
  } {v,err:=p.ParseRuleAST(c.name,"!",p.WithUnit(unit));if err==nil||!reflect.DeepEqual(v,c.zero) {t.Fatalf("%s failure: %T %v %v",c.name,v,v,err)}}
 }
}
func counted(calls *int,opt p.ParseOption)p.ParseOption {
 f:=reflect.ValueOf(opt);return reflect.MakeFunc(f.Type(),func(args []reflect.Value)[]reflect.Value{*calls++;f.Call(args);return nil}).Interface().(p.ParseOption)
}
func TestNamedOptionsAndErrors(t *testing.T) {
 calls:=0
 v,err:=p.ParseRuleAST("missing","",counted(&calls,p.WithMaxDepth(-1)))
 if v!=nil||err==nil||!strings.Contains(err.Error(),"max depth")||calls!=1 {t.Fatalf("precedence or option replay: %v %v %d",v,err,calls)}
 calls=0;v,err=p.ParseRuleAST("structure","é",counted(&calls,p.WithUnit(p.Bytes)));if err!=nil||v.(*p.S).T.End!=2||calls!=1 {t.Fatal(v,err,calls)}
 v,err=p.ParseRuleAST("missing","");if v!=nil||fmt.Sprint(err)!="rule missing is not defined" {t.Fatal(v,err)}
 for _,opts:=range [][]p.ParseOption{nil,{p.WithMaxDepth(0)},{p.WithMaxDepth(32)},{p.WithMaxDepth(64)}} {
  if _,err:=p.ParseRuleAST("recursive",strings.Repeat("(",5)+"é"+strings.Repeat(")",5),opts...);err!=nil {t.Fatal(err)}
 }
 if _,err:=p.ParseRuleAST("recursive","((é))",p.WithMaxDepth(1));err==nil||!strings.Contains(err.Error(),"nesting too deep") {t.Fatal(err)}
 if v,err:=p.ParseRuleAST("abort","");!reflect.DeepEqual(v,(*p.Values)(nil))||err==nil||!strings.Contains(err.Error(),"action in") {t.Fatal(v,err)}
 for _,name:=range []string{"recoverStruct","recoverTerminal","recoverUnion","recoverList","recoveryElements"} {
  v,err:=p.ParseRuleAST(name,"?");if _,ok:=err.(p.SyntaxErrors);!ok {t.Fatalf("%s: %v",name,err)}
  switch name {case "recoverStruct":if _,ok:=v.(*p.Error);!ok {t.Fatalf("%s: %T %v",name,v,v)};case "recoverTerminal":if _,ok:=v.(*p.Recovery).T.(*p.Error);!ok {t.Fatalf("%s: %T %v",name,v,v)};case "recoverUnion":if _,ok:=v.(*p.Error);!ok {t.Fatal(v)};case "recoverList":if _,ok:=v.(*p.Error);!ok {t.Fatal(v)};case "recoveryElements":xs:=v.([]any);if len(xs)!=1 {t.Fatal(v)};if _,ok:=xs[0].(*p.Error);!ok {t.Fatal(v)}}
 }
}
func TestStartUnchanged(t *testing.T) {
 name,input:="main","é";if START_CST {name,input="cst","xy"};var f func(string,...p.ParseOption)(START_TYPE,error)=p.ParseAST;_,_=f(input)
 v,err:=p.ParseRuleAST(name,input);if err!=nil {t.Fatal(err)};a,err:=p.ParseAST(input);if err!=nil||jsonValue(a)!=jsonValue(v) {t.Fatal(a,v,err)}
}
func TestNamedOwnership(t *testing.T) {
 v,err:=p.ParseRuleAST("items","éé");if err!=nil {t.Fatal(err)};before:=jsonValue(v)
 var wg sync.WaitGroup
 for i:=0;i<4;i++ {wg.Add(1);go func(){defer wg.Done();for j:=0;j<20;j++ {if _,err:=p.ParseRuleAST("structure","é");err!=nil {t.Error(err)}}}()};wg.Wait()
 if before!=jsonValue(v) {t.Fatal("retained named result changed")}
}
`

// XML is only a workload: named fragments must agree with existing wrappers,
// with spans relative to the fragment rather than the synthetic document.
func TestGeneratedNamedASTXMLFragments(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated XML workload")
	}
	source, err := os.ReadFile("../../parsers/xml/xml.pego")
	if err != nil {
		t.Fatal(err)
	}
	g, err := syntax.Parse(string(source))
	if err != nil {
		t.Fatal(err)
	}
	for _, convert := range []bool{false, true} {
		t.Run(fmt.Sprint(convert), func(t *testing.T) {
			code, err := generateCode(g, GenOptions{Package: "xml", Start: "main", Types: true, convertTypes: convert})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "xml"), 0755); err != nil {
				t.Fatal(err)
			}
			for name, data := range map[string]string{"go.mod": testGoMod("xmlfixture"), "xml/parser.go": string(code), "xml_test.go": namedXMLConsumer} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0644); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("go", "test", "-count=1", "-v", "./...")
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
		})
	}
}

const namedXMLConsumer = `package xml_test
import("encoding/json";"reflect";"testing";"unicode/utf8";p "xmlfixture/xml")
func normalized(v any,offset int)string {
 data,_:=json.Marshal(v);var x any;if err:=json.Unmarshal(data,&x);err!=nil {panic(err)}
 var shift func(any);shift=func(x any){switch x:=x.(type){case []any:for _,v:=range x {shift(v)};case map[string]any:if start,ok:=x["Start"].(float64);ok {x["Start"]=start-float64(offset);x["End"]=x["End"].(float64)-float64(offset)};for _,v:=range x {shift(v)}}};shift(x);data,_=json.Marshal(x);return string(data)
}
func TestXMLFragments(t *testing.T) {
 for _,unit:=range []p.PositionUnit{p.CodePoints,p.Bytes} {
  for _,c:=range []struct{name,fragment,prefix,suffix string;bad bool}{
   {"content","é<子>日本&amp;語</子><!--注-->","<r>","</r>",false},
   {"content","", "<r>","</r>",false},
   {"int_subset","<!ELEMENT 子 (#PCDATA)><!ENTITY 名 'é'>", "<!DOCTYPE r [","]><r/>",false},
   {"content","é<子>","<r>","</r>",true},
   {"int_subset","<!ELEMENT 子 (", "<!DOCTYPE r [","]><r/>",true},
  } {
   v,err:=p.ParseRuleAST(c.name,c.fragment,p.WithUnit(unit));doc,werr:=p.ParseAST(c.prefix+c.fragment+c.suffix,p.WithUnit(unit))
   if c.bad {
    if err==nil||werr==nil {t.Fatalf("malformed %s accepted",c.name)}
    e,ok:=err.(*p.SyntaxError);if !ok {t.Fatalf("wrong diagnostic class %T",err)}
    limit:=utf8.RuneCountInString(c.fragment);if unit==p.Bytes {limit=len(c.fragment)}
    if e.Pos<0||e.Pos>limit||!reflect.ValueOf(v).IsNil() {t.Fatal("fragment failure coordinates or zero",v,err)}
    continue
   }
   if err!=nil||werr!=nil {t.Fatal(err,werr)}
   var wrapped any
   // content, a rule without an action, has a node of its list as its value; the wrapper's
   // field holds the list.
   if c.name=="content" {wrapped=doc.Root.Content;n,ok:=v.(*p.TNode[[]p.Content]);if !ok {t.Fatalf("content mapping: %T",v)};v=n.Value} else {wrapped=doc.Doctype.Subset;if _,ok:=v.([]p.Decl);!ok {t.Fatalf("declaration mapping: %T",v)}}
   if normalized(v,0)!=normalized(wrapped,len(c.prefix)) {t.Fatalf("fragment differs from shifted wrapper: %s\n%s",normalized(v,0),normalized(wrapped,len(c.prefix)))}
  }
 }
}
`

func TestGeneratedNamedASTSuccessfulNil(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated nil-result consumers")
	}
	g, err := syntax.Parse(`type T = { Text string }
type S = { T T }
type U ~ S | T
type MaybeU ~ *U
rule term T = t:("é") #atomic => T{Text: text($t)}
rule nilAny _ = t:term? => $t
rule seq = "é" "é"?
rule nilNode *node[_] = t:seq? => $t
rule nilUnion MaybeU = t:term? => $t
`)
	if err != nil {
		t.Fatal(err)
	}
	for _, start := range []string{"nilAny", "nilNode", "nilUnion"} {
		for _, conversion := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%v", start, conversion), func(t *testing.T) {
				code, err := generateCode(g, GenOptions{Package: "named", Start: start, Types: true, convertTypes: conversion})
				if err != nil {
					t.Fatal(err)
				}
				dir := t.TempDir()
				if err := os.Mkdir(filepath.Join(dir, "named"), 0755); err != nil {
					t.Fatal(err)
				}
				consumer := strings.ReplaceAll(namedNilConsumer, "START_UNION", fmt.Sprint(start == "nilUnion"))
				for name, data := range map[string]string{"go.mod": testGoMod("namedfixture"), "named/parser.go": string(code), "nil_test.go": consumer} {
					if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0644); err != nil {
						t.Fatal(err)
					}
				}
				cmd := exec.Command("go", "test", "-count=1", "-v", "./...")
				cmd.Dir = dir
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("%v\n%s", err, out)
				}
			})
		}
	}
}

const namedNilConsumer = `package named_test
import("testing";p "namedfixture/named")
func TestSuccessfulNil(t *testing.T) {
 for _,unit:=range []p.PositionUnit{p.CodePoints,p.Bytes} {
  for _,name:=range []string{"nilAny","nilNode","nilUnion"} {
   n,err:=p.ParseRule(name,"",p.WithUnit(unit));if err!=nil||n!=nil {t.Fatalf("%s generic nil success: %v %v",name,n,err)}
   v,err:=p.ParseRuleAST(name,"",p.WithUnit(unit));if name=="nilNode" {if x,ok:=v.(*p.TNode[any]);err!=nil||!ok||x!=nil {t.Fatalf("%s typed nil success: %T %v %v",name,v,v,err)}} else if err!=nil||v!=nil {t.Fatalf("%s typed nil success: %T %v %v",name,v,v,err)}
   v,err=p.ParseRuleAST(name,"é",p.WithUnit(unit));if err!=nil {t.Fatal(err)};if name=="nilNode" {if v==nil {t.Fatalf("%s nonnil result changed: %T %v",name,v,v)}} else {x,ok:=v.(*p.T);if !ok||x.Text!="é" {t.Fatalf("%s nonnil result changed: %T %v",name,v,v)}}
  }
  legacy,err:=p.ParseAST("",p.WithUnit(unit));if err!=nil {t.Fatal(err)}
  boxed:=any(legacy)
  if START_UNION {if boxed!=nil {t.Fatalf("legacy union nil changed: %T %v",boxed,boxed)}} else {
   if legacy!=nil {t.Fatalf("any/node nil mapping changed: %T %v",legacy,legacy)}
  }
 }
}
`
