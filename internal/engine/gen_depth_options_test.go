package engine

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

func TestGeneratedMaxDepthValidation(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedMaxDepthValidation)
}

func testGeneratedMaxDepthValidation(t *testing.T) {
	g, err := parseTest(`rule main = "x"`)
	if err != nil {
		t.Fatal(err)
	}
	safe := uint64(9007199254740991)
	if uint64(^uint(0)>>1) >= safe+1 {
		if got, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "main", MaxDepth: int(safe)}); err != nil || len(got) == 0 {
			t.Fatalf("safe JavaScript boundary rejected: %v", err)
		}
		if got, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "main", MaxDepth: int(safe + 1)}); err == nil || len(got) != 0 {
			t.Fatal("unsafe JavaScript boundary accepted")
		}
	}
	for _, ts := range []bool{false, true} {
		generate := func(g *grammar.Grammar, opts gen.GenOptions) ([]byte, error) { return gentest.GenerateCode(t, g, opts) }
		if ts {
			generate = gentest.GenerateTSBundle
		}
		base, err := generate(g, gen.GenOptions{Package: "depth", Start: "main"})
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range []int{0, DefaultMaxDepth} {
			got, err := generate(g, gen.GenOptions{Package: "depth", Start: "main", MaxDepth: n})
			if err != nil || !bytes.Equal(base, got) {
				t.Fatalf("ts=%v depth=%d equivalent default differs: %v", ts, n, err)
			}
		}
		for _, n := range []int{3, int(^uint(0) >> 1)} {
			got, err := generate(g, gen.GenOptions{Package: "depth", Start: "main", MaxDepth: n})
			if ts && uint64(n) > 9007199254740991 {
				if err == nil || len(got) != 0 {
					t.Fatal("unsafe JavaScript default accepted")
				}
				continue
			}
			want := fmt.Sprintf("var pegoGrammar = rt.NewGrammar(%d)", n)
			if ts {
				want = fmt.Sprintf("const G = mkGrammar(%d, ", n)
			}
			if err != nil || !strings.Contains(string(got), want) {
				t.Fatalf("custom default %d: %v", n, err)
			}
		}
		if got, err := generate(g, gen.GenOptions{Package: "depth", Start: "main", MaxDepth: -1}); err == nil || len(got) != 0 {
			t.Fatal("negative default accepted")
		}
	}
}

// Differentially exercise every rule-call owner, including first-character
// dispatch and typed direct/conversion routes, with small predictable ceilings.
func TestGeneratedDepthOptions(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedDepthOptions)
}

func testGeneratedDepthOptions(t *testing.T) {
	if testing.Short() {
		t.Skip("runs generated code")
	}
	fixtures := []gentest.Case{
		{Name: "ordinary", Src: `type Word = { Text string }
rule main Word = t:(n) #atomic => Word{Text: text($t)}
rule n Word = t:("(" n ")" / "é") #atomic => Word{Text: text($t)}`, Inputs: []string{"é", "(é)", "((é))", "((((é))))", "?", "(é"}},
		{Name: "left recursion", Src: `type Word = { Text string }
rule main Word = t:(n) #atomic => Word{Text: text($t)}
rule n Word = t:(n "+" "é" / "(" n ")" / "é") #atomic => Word{Text: text($t)}`, Inputs: []string{"é", "é+é+é", "(é)", "((é+é))", "é+", "?"}},
		{Name: "OperatorsRule", Src: `type Word = { Text string }
rule main Word = t:(n) #atomic => Word{Text: text($t)}
rule n Word = #operators {
    operand w:base => $w
    operand "(" w:n ")" => $w
    level { prefix "-" => $rhs }
    level { infix right "^" => $rhs }
}
rule base Word = t:"é" #atomic => Word{Text: text($t)}`, Inputs: []string{"é", "--é", "é^é^é", "(-é^é)", "((é))", "?"}},
		{Name: "dispatch", Src: `type Word = { Text string }
rule main Word = t:("a" one / "b" two / "c" three) #atomic => Word{Text: text($t)}
rule one Word = t:(two) #atomic => Word{Text: text($t)}
rule two Word = t:(three) #atomic => Word{Text: text($t)}
rule three Word = t:(four) #atomic => Word{Text: text($t)}
rule four Word = t:("é") #atomic => Word{Text: text($t)}`, Inputs: []string{"aé", "bé", "cé", "a?", "b?", "c?"}},
		{Name: "action", Src: `type Word = { N int }
rule main = x:"a"* #atomic => Word{N: 1 / len($x)}`, Inputs: []string{"", "a", "aa", "?"}},
		{Name: "recovery", Src: `type Word = { Text string }
rule main Word = t:(n) #atomic => Word{Text: text($t)}
rule n Word = t:(("(" n ")" / "é") #recover(skip="?")) #atomic => Word{Text: text($t)}`, Inputs: []string{"é", "(é)", "((é))", "?", "(?)"}},
	}
	dir := t.TempDir()
	write := func(name string, b []byte) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", []byte(gentest.GoMod("depthoptions")))
	for i, c := range fixtures {
		g, err := parseTest(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		prog := mustCompile(t, c.Src)
		for _, conv := range []bool{false, true} {
			pkg := fmt.Sprintf("g%d_%v", i, conv)
			code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: pkg, Start: "main", Types: true, Recognize: true, MaxDepth: 4, ConvertTypes: conv})
			if err != nil {
				t.Fatal(err)
			}
			var rows strings.Builder
			for _, name := range []string{"main", prog.RuleList[len(prog.RuleList)-1].Name} {
				for _, in := range c.Inputs {
					for _, unit := range []PositionUnit{CodePoints, Bytes} {
						for _, limit := range []int{1, 2, 3, 4, 5, 6, 8, 20} {
							_, err := prog.ParseWith(name, in, ParseOptions{Unit: unit, MaxDepth: limit})
							_, recErr := prog.ParseWith(name, in, ParseOptions{Unit: unit, MaxDepth: limit, Recognize: true})
							fmt.Fprintf(&rows, "{%q,%q,%s,%d,%q,%q},\n", name, in, map[PositionUnit]string{CodePoints: "CodePoints", Bytes: "Bytes"}[unit], limit, fmt.Sprint(err), fmt.Sprint(recErr))
						}
					}
				}
			}
			harness := `package ` + pkg + `
import("fmt";"strings";"sync";"testing";pegort "github.com/ornew/pego/runtime")
func TestLimitsAndReuse(t *testing.T) {
 rows:=[]struct{name,input string;unit PositionUnit;limit int;want,recWant string}{` + rows.String() + `}
 for repeat:=0;repeat<3;repeat++ {for _,c:=range rows {
  opts:=[]ParseOption{WithUnit(c.unit),WithMaxDepth(c.limit)}
  _,err:=ParseRule(c.name,c.input,opts...)
  if got:=fmt.Sprint(err);got!=c.want {t.Fatalf("rule %s %q unit %d limit %d got %s want %s",c.name,c.input,c.unit,c.limit,got,c.want)}
  if c.name=="main" {
   _,err=Parse(c.input,opts...);if got:=fmt.Sprint(err);got!=c.want {t.Fatalf("Parse limit %d got %s want %s",c.limit,got,c.want)}
   _,err=ParseAST(c.input,opts...);if got:=fmt.Sprint(err);got!=c.want {t.Fatalf("AST %q limit %d got %s want %s",c.input,c.limit,got,c.want)}
   err=Recognize(c.input,opts...);if got:=fmt.Sprint(err);got!=c.recWant {t.Fatalf("Recognize limit %d got %s want %s",c.limit,got,c.want)}
  }
 }}
 for _,c:=range rows {if c.name=="main" && c.limit==20 && c.want=="<nil>" {
  for _,unit:=range []PositionUnit{CodePoints,Bytes,PositionUnit(99)} {
   opts:=[]ParseOption{WithUnit(Bytes),WithUnit(unit),WithMaxDepth(20)}
   n,err:=Parse(c.input,opts...);wantEnd:=len([]rune(c.input));if unit==Bytes {wantEnd=len(c.input)}
   if err!=nil || int(n.RawEnd)!=wantEnd {t.Fatalf("last unit option: %v",err)}
   v,err:=ParseAST(c.input,opts...);if err!=nil || v.End!=wantEnd {t.Fatalf("typed last unit option: %v",err)}
  }
 }}
 for _,c:=range rows {if c.name=="main" && c.limit==4 {
  if c.unit==CodePoints { _,err:=Parse(c.input);if fmt.Sprint(err)!=c.want {t.Fatalf("no options: %v",err)} }
  for _,opts:=range [][]ParseOption{{WithUnit(c.unit)},{WithUnit(c.unit),WithMaxDepth(0)},{WithUnit(c.unit),WithMaxDepth(4)},{WithUnit(c.unit),WithMaxDepth(20),WithMaxDepth(0)},{WithUnit(c.unit),WithMaxDepth(-1),WithMaxDepth(0)}} {
   _,err:=Parse(c.input,opts...);if fmt.Sprint(err)!=c.want {t.Fatalf("generated default: %v",err)}
   _,err=ParseAST(c.input,opts...);if fmt.Sprint(err)!=c.want {t.Fatalf("typed generated default: %v",err)}
   if err=Recognize(c.input,opts...);fmt.Sprint(err)!=c.recWant {t.Fatalf("recognize generated default: %v",err)}
  }
 }}
 var wg sync.WaitGroup
 for worker:=0;worker<8;worker++ {wg.Go(func(){for _,c:=range rows {if c.name=="main" {
  _,err:=ParseAST(c.input,WithUnit(c.unit),WithMaxDepth(c.limit));if fmt.Sprint(err)!=c.want {t.Errorf("concurrent AST limit=%d: %v",c.limit,err)}
  _,err=Parse(c.input,WithUnit(c.unit),WithMaxDepth(c.limit));if fmt.Sprint(err)!=c.want {t.Errorf("concurrent Parse limit=%d: %v",c.limit,err)}
 }}})};wg.Wait()
}
func TestConfiguration(t *testing.T) {
 bad:=WithMaxDepth(-7)
 for _,parse:=range []func(string,...ParseOption)(*Node,error){Parse,func(s string,o ...ParseOption)(*Node,error){return ParseRule("missing",s,o...)}} {
  n,err:=parse("?",bad);if n!=nil || err==nil || !strings.Contains(err.Error(),"max depth must be non-negative: -7") {t.Fatalf("config precedence: %v",err)}
 }
 if _,err:=ParseAST("?",bad);err==nil || !strings.Contains(err.Error(),"max depth") {t.Fatal(err)}
 if err:=Recognize("?",bad);err==nil || !strings.Contains(err.Error(),"max depth") {t.Fatal(err)}
 if _,err:=Parse("?",bad,WithMaxDepth(20));err!=nil && strings.Contains(err.Error(),"max depth") {t.Fatal("validate last effective setting")}
 if _,err:=Parse("?",WithMaxDepth(20),bad);err==nil || !strings.Contains(err.Error(),"max depth") {t.Fatal("last setting not applied")}
 if _,err:=Parse("?",WithMaxDepth(int(^uint(0)>>1)));err!=nil && strings.Contains(err.Error(),"max depth") {t.Fatal("largest native int rejected")}
 called:=0
 option:=func(o *pegort.X_parseOptions){called++;o.X_maxDepth=20;_,err:=Parse("?",WithMaxDepth(1));_ = err}
 Parse("?",option);ParseAST("?",option);Recognize("?",option)
 if called!=3 {t.Fatal("options retained or evaluated more than once")}
 for _,invoke:=range []func(){func(){Parse("?",nil)},func(){ParseRule("missing","?",nil)},func(){ParseAST("?",nil)},func(){Recognize("?",nil)}} {
  func(){defer func(){if recover()==nil {t.Error("nil option did not panic")}}();invoke()}()
 }
 // Typed direct and conversion consumers must use the functional signature.
 var node func(string,...ParseOption)(*Node,error)=Parse
 var rule func(string,string,...ParseOption)(*Node,error)=ParseRule
 var rec func(string,...ParseOption)error=Recognize
 var ast func(string,...ParseOption)(*Word,error)=ParseAST
 _,_,_,_=node,rule,rec,ast
}
`
			write(pkg+"/parser.go", code)
			write(pkg+"/depth_test.go", []byte(harness))
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated depth contracts: %v\n%s", err, out)
	}
}

func TestGeneratedDepthNameCollisions(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs generated code")
	}
	src := `type ParseOption = { Text string }
type ParseOption_ = { Text string }
type WithUnit ~ ParseOption
type WithMaxDepth = { Item WithUnit }
rule main WithUnit = t:("é") #atomic => ParseOption{Text: text($t)}`
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gentest.GoMod("collisions")), 0644)
	for _, conv := range []bool{false, true} {
		name := "direct"
		if conv {
			name = "convert"
		}
		opts := gen.GenOptions{Package: name, Module: "collisions/" + name, Runtime: gen.RuntimeImport, Start: "main", Types: true, ConvertTypes: conv}
		files, err := gentest.Generate(t, g, opts)
		if err != nil {
			t.Fatal(err)
		}
		again, err := gentest.Generate(t, g, opts)
		if err != nil || !slices.EqualFunc(files, again, func(a, b gen.GeneratedFile) bool { return a.Path == b.Path && bytes.Equal(a.Content, b.Content) }) {
			t.Fatal("unstable generation")
		}
		var typesSource string
		for _, f := range files {
			if f.Path == "types/types.go" {
				typesSource = string(f.Content)
			}
		}
		for _, want := range []string{"type ParseOption struct", "type ParseOption_ struct", "type WithUnit = *ParseOption", "type WithMaxDepth struct"} {
			if !strings.Contains(typesSource, want) {
				t.Errorf("missing %s", want)
			}
		}
		consumer := `package ` + name + `_test
import("testing";p "collisions/` + name + `";g "collisions/` + name + `/types")
func TestExports(t *testing.T){
 var fn func(string,...p.ParseOption)(g.WithUnit,error)=p.ParseAST
 opts:=[]p.ParseOption{p.WithUnit(p.Bytes),p.WithMaxDepth(20)}
 value,err:=fn("é",opts...);if err!=nil || value.Text!="é" || value.End!=2{t.Fatal(value,err)}
 var word *g.ParseOption=value
 var other *g.ParseOption_=&g.ParseOption_{}
 var box *g.WithMaxDepth=&g.WithMaxDepth{Item:word}
 _,_=other,box
}
`
		files = append(files, gen.GeneratedFile{Path: "consumer_test.go", Content: []byte(consumer)})
		for _, f := range files {
			path := filepath.Join(dir, name, filepath.FromSlash(f.Path))
			os.MkdirAll(filepath.Dir(path), 0755)
			os.WriteFile(path, f.Content, 0644)
		}
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("collision consumers: %v\n%s", err, out)
	}
}
