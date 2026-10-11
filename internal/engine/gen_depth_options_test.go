package engine

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

func TestGeneratedMaxDepthValidation(t *testing.T) {
	g, err := syntax.Parse(`def main = "x"`)
	if err != nil {
		t.Fatal(err)
	}
	safe := uint64(9007199254740991)
	if uint64(^uint(0)>>1) >= safe+1 {
		if got, err := GenerateTS(g, GenOptions{Start: "main", MaxDepth: int(safe)}); err != nil || len(got) == 0 {
			t.Fatalf("safe JavaScript boundary rejected: %v", err)
		}
		if got, err := GenerateTS(g, GenOptions{Start: "main", MaxDepth: int(safe + 1)}); err == nil || len(got) != 0 {
			t.Fatal("unsafe JavaScript boundary accepted")
		}
	}
	for _, ts := range []bool{false, true} {
		generate := Generate
		if ts {
			generate = GenerateTS
		}
		base, err := generate(g, GenOptions{Package: "depth", Start: "main"})
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range []int{0, DefaultMaxDepth} {
			got, err := generate(g, GenOptions{Package: "depth", Start: "main", MaxDepth: n})
			if err != nil || !bytes.Equal(base, got) {
				t.Fatalf("ts=%v depth=%d equivalent default differs: %v", ts, n, err)
			}
		}
		for _, n := range []int{3, int(^uint(0) >> 1)} {
			got, err := generate(g, GenOptions{Package: "depth", Start: "main", MaxDepth: n})
			if ts && uint64(n) > 9007199254740991 {
				if err == nil || len(got) != 0 {
					t.Fatal("unsafe JavaScript default accepted")
				}
				continue
			}
			if err != nil || !strings.Contains(string(got), fmt.Sprintf("const defaultMaxDepth = %d", n)) {
				t.Fatalf("custom default %d: %v", n, err)
			}
		}
		if got, err := generate(g, GenOptions{Package: "depth", Start: "main", MaxDepth: -1}); err == nil || len(got) != 0 {
			t.Fatal("negative default accepted")
		}
	}
}

// Differentially exercise every rule-call owner, including first-character
// dispatch and typed direct/conversion routes, with small predictable ceilings.
func TestGeneratedDepthOptions(t *testing.T) {
	if testing.Short() {
		t.Skip("runs generated code")
	}
	fixtures := []genCase{
		{"ordinary", `type Word terminal
def main: Word = n
def n: Word = "(" n ")" / "é"`, []string{"é", "(é)", "((é))", "((((é))))", "?", "(é"}},
		{"left recursion", `type Word terminal
def main: Word = n
def n: Word = n "+" "é" / "(" n ")" / "é"`, []string{"é", "é+é+é", "(é)", "((é+é))", "é+", "?"}},
		{"Pratt", `type Word terminal
def main: Word = n
def n: Word = pratt {
 operand w:base -> $w
 operand "(" w:n ")" -> $w
 level { prefix "-" -> $rhs }
 level { infix right "^" -> $rhs }
}
def base: Word = "é"`, []string{"é", "--é", "é^é^é", "(-é^é)", "((é))", "?"}},
		{"dispatch", `type Word terminal
def main: Word = "a" one / "b" two / "c" three
def one: Word = two
def two: Word = three
def three: Word = four
def four: Word = "é"`, []string{"aé", "bé", "cé", "a?", "b?", "c?"}},
		{"action", `type Word struct { N int }
def main = x:@"a"* -> new Word{N: 1 / len($x)}`, []string{"", "a", "aa", "?"}},
		{"recovery", `type Word terminal
def main: Word = n
def n: Word = ("(" n ")" / "é") #recover(skip="?")`, []string{"é", "(é)", "((é))", "?", "(?)"}},
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
	write("go.mod", []byte("module depthoptions\n\ngo 1.27.1\n"))
	for i, c := range fixtures {
		g, err := syntax.Parse(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		prog := compile(t, c.src)
		for _, conv := range []bool{false, true} {
			pkg := fmt.Sprintf("g%d_%v", i, conv)
			code, err := Generate(g, GenOptions{Package: pkg, Start: "main", Types: true, Recognize: true, MaxDepth: 4, convertTypes: conv})
			if err != nil {
				t.Fatal(err)
			}
			var rows strings.Builder
			for _, name := range []string{"main", prog.rules[len(prog.rules)-1].name} {
				for _, in := range c.inputs {
					for _, unit := range []Unit{CodePoints, Bytes} {
						for _, limit := range []int{1, 2, 3, 4, 5, 6, 8, 20} {
							_, err := prog.ParseWith(name, in, ParseOptions{Unit: unit, MaxDepth: limit})
							_, recErr := prog.ParseWith(name, in, ParseOptions{Unit: unit, MaxDepth: limit, Recognize: true})
							fmt.Fprintf(&rows, "{%q,%q,%s,%d,%q,%q},\n", name, in, map[Unit]string{CodePoints: "CodePoints", Bytes: "Bytes"}[unit], limit, fmt.Sprint(err), fmt.Sprint(recErr))
						}
					}
				}
			}
			harness := `package ` + pkg + `
import("fmt";"strings";"sync";"testing")
func TestLimitsAndReuse(t *testing.T) {
 rows:=[]struct{name,input string;unit Unit;limit int;want,recWant string}{` + rows.String() + `}
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
  for _,unit:=range []Unit{CodePoints,Bytes,Unit(99)} {
   opts:=[]ParseOption{WithUnit(Bytes),WithUnit(unit),WithMaxDepth(20)}
   n,err:=Parse(c.input,opts...);wantEnd:=len([]rune(c.input));if unit==Bytes {wantEnd=len(c.input)}
   if err!=nil || int(n.End)!=wantEnd {t.Fatalf("last unit option: %v",err)}
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
 option:=func(o *parseOptions){called++;o.maxDepth=20;_,err:=Parse("?",WithMaxDepth(1));_ = err}
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
	if testing.Short() {
		t.Skip("runs generated code")
	}
	src := `type ParseOption terminal
type ParseOption_ terminal
type WithUnit = ParseOption
type WithMaxDepth struct { Item WithUnit }
def main: WithUnit = "é"`
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module collisions\n\ngo 1.27.1\n"), 0644)
	for _, conv := range []bool{false, true} {
		name := "direct"
		if conv {
			name = "convert"
		}
		opts := GenOptions{Package: name, Start: "main", Types: true, convertTypes: conv}
		code, err := Generate(g, opts)
		if err != nil {
			t.Fatal(err)
		}
		again, err := Generate(g, opts)
		if err != nil || !bytes.Equal(code, again) {
			t.Fatal("unstable collision mapping")
		}
		for _, want := range []string{"type ParseOption_ struct", "type ParseOption__ struct", "type WithUnit_ = *ParseOption_", "type WithMaxDepth_ struct", "opts ...ParseOption) (*ParseOption_, error)"} {
			if !strings.Contains(string(code), want) {
				t.Errorf("missing %s", want)
			}
		}
		os.MkdirAll(filepath.Join(dir, name), 0755)
		os.WriteFile(filepath.Join(dir, name, "parser.go"), code, 0644)
		consumer := `package ` + name + `_test
import("testing";p "collisions/` + name + `")
func TestExports(t *testing.T){
 var fn func(string,...p.ParseOption)(p.WithUnit_,error)=p.ParseAST
 opts:=[]p.ParseOption{p.WithUnit(p.Bytes),p.WithMaxDepth(20)}
 value,err:=fn("é",opts...);if err!=nil || value.Text!="é" || value.End!=2{t.Fatal(value,err)}
 var word *p.ParseOption_=value
 var other *p.ParseOption__=&p.ParseOption__{}
 var box *p.WithMaxDepth_=&p.WithMaxDepth_{Item:word}
 _,_=other,box
}
`
		os.WriteFile(filepath.Join(dir, name, "consumer_test.go"), []byte(consumer), 0644)
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("collision consumers: %v\n%s", err, out)
	}
}
