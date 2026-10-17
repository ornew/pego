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

const nodeFrameScratchGrammar = `type Tok terminal
type Item = { T Tok }
type Root = { Items []Item }
type Bad = { N int }
rule main = xs:(i:item)* $$ => Root{Items: map($xs, (x) => $x.i)}
rule item = t:tok {text($t) == "é" || text($t) == "�"} => Item{T: $t}
rule tok Tok = "é" / "�"
rule abort = x:"é"* #atomic => Bad{N: 10 / len($x)}
rule recovering = x:("é" "!") #recover(skip="?") $$ => $x
`

// PEGO_NODE_FRAME_DIR preserves paired fixtures. Optional
// PEGO_NODE_FRAME_TYPESCRIPT adds the unchanged TypeScript grammar workload.
// This test validates fixtures without running their benchmarks.
func TestGeneratedNodeFrameScratchWorkloads(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated fixtures")
	}
	dir := os.Getenv("PEGO_NODE_FRAME_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	cases := []struct{ name, source, inputs string }{
		{"capture", nodeFrameScratchGrammar, `func frameInputs() (string,string,string) {return "é",strings.Repeat("é",5000),"é!"}`},
		{"nocapture", `type Tok terminal
rule main Tok = ("é" / "�")* $$`, `func frameInputs() (string,string,string) {return "é",strings.Repeat("é",5000),"é!"}`},
	}
	if os.Getenv("PEGO_NODE_FRAME_TYPESCRIPT") != "" {
		source, err := os.ReadFile("../../parsers/typescript/typescript.pego")
		if err != nil {
			t.Fatal(err)
		}
		cases = append(cases, struct{ name, source, inputs string }{"typescript", string(source),
			`func frameInputs() (string,string,string) {return "const 日本語 = 1 + 2;\n",strings.Repeat("const 日本語 = 1 + 2;\n",1000),"function f( { return 1;"}`})
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g, err := syntax.Parse(c.source)
			if err != nil {
				t.Fatal(err)
			}
			prog := compile(t, c.source)
			var rows strings.Builder
			if c.name != "typescript" {
				for _, input := range []string{"", "é", "éé", "�", "é�", "é!", "\xff", "é\xff", "\xe2\x82"} {
					for _, unit := range []Unit{CodePoints, Bytes} {
						n, err := prog.ParseWith("main", input, ParseOptions{Unit: unit})
						_, rerr := prog.ParseWith("main", input, ParseOptions{Unit: unit, Recognize: true})
						fmt.Fprintf(&rows, "{%q,%s,%q,%q},\n", input, map[Unit]string{CodePoints: "CodePoints", Bytes: "Bytes"}[unit], resultJSON(n, err), fmt.Sprint(rerr))
					}
				}
			}
			var want string
			for _, reference := range []bool{false, true} {
				variant := "scratch"
				if reference {
					variant = "reference"
				}
				code, err := Generate(g, GenOptions{Package: "framefixture", Start: "main", Types: true, Recognize: true, disableNodeFrameScratch: reference})
				if err != nil {
					t.Fatal(err)
				}
				fixture := frameScratchFixture
				workload := strings.ReplaceAll(frameScratchWorkload, "INPUTS", c.inputs)
				fixture = strings.ReplaceAll(fixture, "RAW_ROWS", rows.String())
				fixture = strings.ReplaceAll(fixture, "CAPTURE_CASE", fmt.Sprint(c.name == "capture"))
				depthLimit := "maxDepth"
				if strings.Contains(string(code), "type ParseOption ") {
					workload = strings.ReplaceAll(workload, ", unit)", ", WithUnit(unit))")
					depthLimit = "defaultMaxDepth"
				}
				fixture = strings.ReplaceAll(fixture, "FRAME_DEPTH_LIMIT", depthLimit)
				path := filepath.Join(dir, c.name, variant)
				if err := os.MkdirAll(path, 0o755); err != nil {
					t.Fatal(err)
				}
				for name, data := range map[string][]byte{"parser.go": code, "frames_test.go": []byte(fixture), "bench_test.go": []byte(workload), "go.mod": []byte("module framefixture\n\ngo 1.27.1\n")} {
					if err := os.WriteFile(filepath.Join(path, name), data, 0o644); err != nil {
						t.Fatal(err)
					}
				}
				cmd := exec.Command("go", "test", "-count=1", "-v", "-run", "^TestFrame")
				cmd.Dir = path
				output, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("%s: %v\n%s", variant, err, output)
				}
				var got strings.Builder
				for _, line := range strings.Split(string(output), "\n") {
					if at := strings.Index(line, "FRAME_RESULT "); at >= 0 {
						got.WriteString(line[at:])
						got.WriteByte('\n')
					}
				}
				if reference && (got.Len() == 0 || got.String() != want) {
					t.Fatalf("paired results differ:\n%s\n%s", want, got.String())
				}
				want = got.String()
			}
		})
	}
}

const frameScratchFixture = `package framefixture
import("crypto/sha256";"encoding/json";"fmt";"strings";"sync";"testing")
const captureCase = CAPTURE_CASE
func frameJSON(n *Node,err error)string {out:=map[string]any{"node":n};if err!=nil {out["err"]=err.Error()};b,_:=json.Marshal(out);return string(b)}
func frameCleared(t *testing.T,p *parser) {
 t.Helper()
 if p.captureScratch.frameN!=0 || p.captureScratch.slotCI!=0 || p.captureScratch.slotOff!=0 {t.Fatal("scratch cursor survived release")}
 for _,chunk:=range p.captureScratch.frames {for _,f:=range chunk {if f.vals!=nil {t.Fatal("released frame retains values")}}}
 for _,chunk:=range p.captureScratch.slots {for _,v:=range chunk {if v!=nil {t.Fatal("released slot retains values")}}}
 for _,u:=range p.trail[:cap(p.trail)] {if u.f!=nil || u.old!=nil {t.Fatal("released trail retains frame or value")}}
 for _,v:=range p.saved[:cap(p.saved)] {if v!=nil {t.Fatal("released saved captures retain a value")}}
}
func TestFrameResults(t *testing.T) {
 small,large,invalid:=frameInputs()
 var saved []*Node;var snapshots []string;var savedErrors []error;var errorSnapshots []string
 for _,unit:=range []Unit{CodePoints,Bytes} {
  for i,input:=range []string{small,large,invalid,""} {
   for pass:=0;pass<3;pass++ {
    n,err:=frameNode(input,unit);a,aerr:=frameAST(input,unit);rerr:=frameRecognize(input,unit)
    if i<2 && (err!=nil || aerr!=nil || rerr!=nil) {t.Fatalf("valid input: %v %v %v",err,aerr,rerr)}
    if i==2 && (err==nil || aerr==nil || rerr==nil) {t.Fatal("invalid input accepted")}
    data,jerr:=json.Marshal([]any{n,fmt.Sprint(err),a,fmt.Sprint(aerr),fmt.Sprint(rerr)});if jerr!=nil {t.Fatal(jerr)}
    t.Logf("FRAME_RESULT %d/%d/%d %x",unit,i,pass,sha256.Sum256(data))
    if n!=nil {saved=append(saved,n);snapshots=append(snapshots,frameJSON(n,nil))}
    if err!=nil {savedErrors=append(savedErrors,err);errorSnapshots=append(errorSnapshots,fmt.Sprint(err))}
    if captureCase {
     _,err=frameRule("abort","",unit);if err==nil {t.Fatal("action abort accepted")}
     recovered,recoveryErr:=frameRule("recovering","?",unit);if recovered==nil || recoveryErr==nil {t.Fatal("recovery result missing")}
     saved=append(saved,recovered);snapshots=append(snapshots,frameJSON(recovered,nil));savedErrors=append(savedErrors,recoveryErr);errorSnapshots=append(errorSnapshots,fmt.Sprint(recoveryErr))
    }
    for k,n:=range saved {if frameJSON(n,nil)!=snapshots[k] {t.Fatal("retained result changed")}}
    for k,err:=range savedErrors {if fmt.Sprint(err)!=errorSnapshots[k] {t.Fatal("retained diagnostics changed")}}
   }
  }
 }
 for _,c:=range []struct{input string;unit Unit;node,rec string}{RAW_ROWS} {
  n,err:=frameNode(c.input,c.unit)
  if got:=frameJSON(n,err);got!=c.node {t.Fatalf("raw UTF8 input %q: %s want %s",c.input,got,c.node)}
  if got:=fmt.Sprint(frameRecognize(c.input,c.unit));got!=c.rec {t.Fatalf("raw UTF8 recognize %q: %s want %s",c.input,got,c.rec)}
 }
}
func TestFrameAbortCleanup(t *testing.T) {
 var observed *parser
 r:=&rule{name:"abort-test",seen:-1,scope:[]string{"x"},body:func(p *parser,_ int)(*Node,bool){
  observed=p;p.setCapture(0,p.newNode(Node{Text:"abandoned"}));p.saved=append(p.saved,p.frame.vals[0]);panic(fatal{fmt.Errorf("fixture abort")})
 }}
 if _,err:=parse(r,0,"",nil);err==nil || err.Error()!="fixture abort" {t.Fatal("abort did not propagate")}
 frameCleared(t,observed)
}
func TestFrameDepthAbortCleanup(t *testing.T) {
 var observed *parser
 r:=&rule{name:"depth-abort",seen:-1,scope:[]string{"x"}}
 r.body=func(p *parser,_ int)(*Node,bool){observed=p;p.setCapture(0,p.newNode(Node{Text:"abandoned"}));p.depth=FRAME_DEPTH_LIMIT;return p.call(r,0)}
 if _,err:=parse(r,0,"",nil);err==nil || !strings.Contains(err.Error(),"nesting too deep") {t.Fatal("depth abort did not propagate")}
 frameCleared(t,observed)
}
func TestFrameConcurrentReuse(t *testing.T) {
 small,_,_:=frameInputs()
 var wg sync.WaitGroup
 for i:=0;i<6;i++ {wg.Add(1);go func(i int){defer wg.Done();unit:=CodePoints;if i%2!=0 {unit=Bytes}
  n,err:=frameNode(small,unit);if err!=nil {t.Error(err);return};before:=frameJSON(n,nil)
  for j:=0;j<20;j++ {if _,err:=frameNode(small,unit);err!=nil {t.Error(err)};if err:=frameRecognize(small,unit);err!=nil {t.Error(err)};if _,err:=frameRule("main",small,unit);err!=nil {t.Error(err)}}
  if frameJSON(n,nil)!=before {t.Error("concurrent pool reuse changed a result")}
 }(i)}
 wg.Wait()
}
`

// The standalone benchmark driver uses only public APIs, so it can also be
// built with the exact prior generated parser without private scratch fields.
const frameScratchWorkload = `package framefixture
import("fmt";"strings";"testing")
INPUTS
func frameNode(input string,unit Unit)(*Node,error) {if unit==CodePoints {return Parse(input)};return Parse(input, unit)}
func frameAST(input string,unit Unit)(any,error) {if unit==CodePoints {return ParseAST(input)};return ParseAST(input, unit)}
func frameRecognize(input string,unit Unit)error {if unit==CodePoints {return Recognize(input)};return Recognize(input, unit)}
func frameRule(name,input string,unit Unit)(*Node,error) {if unit==CodePoints {return ParseRule(name,input)};return ParseRule(name,input, unit)}
var frameNodeSink *Node
var frameASTSink any
var frameErrorSink error
func BenchmarkNodeFrameScratch(b *testing.B) {
 small,large,_:=frameInputs()
 for _,input:=range []struct{name,text string}{{"Small",small},{"Large",large}} {for _,unit:=range []Unit{CodePoints,Bytes} {for _,api:=range []string{"Node","Recognize","AST"} {
  b.Run(fmt.Sprintf("%s/%s/%d",input.name,api,unit),func(b *testing.B){run:=func(){switch api {case "Node":frameNodeSink,frameErrorSink=frameNode(input.text,unit);case "AST":frameASTSink,frameErrorSink=frameAST(input.text,unit);default:frameErrorSink=frameRecognize(input.text,unit)};if frameErrorSink!=nil {b.Fatal(frameErrorSink)}}
   run();b.SetBytes(int64(len(input.text)));b.ReportAllocs();for b.Loop(){run()}
  })
 }}}
}
`
