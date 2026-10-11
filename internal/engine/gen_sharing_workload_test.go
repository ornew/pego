package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// PEGO_GENERATED_SHARING_DIR preserves standalone controls for prebuilt
// parsing, generation-size, compiler-cost and binary-size measurements.
func TestGeneratedSharingWorkloads(t *testing.T) {
	if testing.Short() {
		t.Skip("builds standalone controls")
	}
	dir := os.Getenv("PEGO_GENERATED_SHARING_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	node, _ := exec.LookPath("node")
	for _, lang := range []string{"json", "typescript"} {
		t.Run(lang, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join("../../parsers", lang, lang+".pego"))
			if err != nil {
				t.Fatal(err)
			}
			g, err := syntax.Parse(string(src))
			if err != nil {
				t.Fatal(err)
			}
			var want, wantTS string
			for _, mode := range []string{"default", "combined", "methods", "literals", "both"} {
				opts := GenOptions{Package: "sharingfixture", Start: "main", Types: true, Recognize: true,
					disableMethodSharing:   mode == "methods" || mode == "both",
					disableLiteralSharing:  mode == "literals" || mode == "both",
					enableTSLiteralSharing: mode != "default"}
				code, err := Generate(g, opts)
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(dir, lang, mode)
				if err := os.MkdirAll(path, 0o755); err != nil {
					t.Fatal(err)
				}
				fixture := strings.ReplaceAll(generatedSharingFixture, "LANG", lang)
				for name, data := range map[string][]byte{
					"parser.go": code, "sharing_test.go": []byte(fixture),
					"go.mod": []byte("module sharingfixture\n\ngo 1.24\n"),
				} {
					if err := os.WriteFile(filepath.Join(path, name), data, 0o644); err != nil {
						t.Fatal(err)
					}
				}
				cmd := exec.Command("go", "test", "-run", "^TestSharingResults$", "-count=1", "-v")
				cmd.Dir = path
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("%s: %v\n%s", mode, err, out)
				}
				var got string
				for _, line := range strings.Split(string(out), "\n") {
					if at := strings.Index(line, "SHARING_RESULT "); at >= 0 {
						got += line[at:] + "\n"
					}
				}
				if mode == "default" {
					want = got
				}
				if got == "" || got != want {
					t.Fatalf("%s: Node, typed or recognition output differs", mode)
				}
				opts.Types = false
				ts, err := GenerateTS(g, opts)
				if err != nil {
					t.Fatal(err)
				}
				for name, data := range map[string][]byte{
					"parser.ts": ts, "package.json": []byte(`{"type":"module"}`),
					"measure.mjs": []byte(strings.ReplaceAll(generatedSharingTSMeasure, "LANG", lang)),
				} {
					if err := os.WriteFile(filepath.Join(path, name), data, 0o644); err != nil {
						t.Fatal(err)
					}
				}
				if node != "" {
					cmd := exec.Command(node, "measure.mjs", "--check")
					cmd.Dir = path
					out, err := cmd.CombinedOutput()
					if err != nil {
						t.Fatalf("%s TypeScript: %v\n%s", mode, err, out)
					}
					if mode == "default" {
						wantTS = string(out)
					}
					if len(out) == 0 || string(out) != wantTS {
						t.Fatalf("%s TypeScript output differs", mode)
					}
				}
				t.Logf("%s: Go=%d bytes TS=%d bytes", mode, len(code), len(ts))
			}
		})
	}
}

const generatedSharingFixture = `package sharingfixture
import("crypto/sha256";"encoding/json";"fmt";"strings";"testing")

func sharingInputs() (string,string,string) {
 if "LANG"=="json" {
  item:="{\"日本語\":[1,true,null,\"😀\"]}"
  return item,"["+strings.Repeat(item+",",999)+item+"]","{\"x\":"
 }
 return "const 日本語 = 1 + 2;\n",strings.Repeat("const 日本語 = 1 + 2;\n",1000),"function f( { return 1;"
}
func TestSharingResults(t *testing.T) {
 small,large,invalid:=sharingInputs()
 var saved []*Node;var snapshots []string
 for _,unit:=range []Unit{CodePoints,Bytes} {
  for i,input:=range []string{small,large,invalid,""} {
   for pass:=0;pass<3;pass++ {
    n,err:=Parse(input,WithUnit(unit));ast,aerr:=ParseAST(input,WithUnit(unit));rec:=Recognize(input,WithUnit(unit))
    if i<2 && (err!=nil || aerr!=nil || rec!=nil) {t.Fatalf("valid input failed: %v %v %v",err,aerr,rec)}
    if i==2 && (err==nil || aerr==nil || rec==nil) {t.Fatal("invalid input accepted")}
    data,jerr:=json.Marshal([]any{n,fmt.Sprint(err),ast,fmt.Sprint(aerr),fmt.Sprint(rec)});if jerr!=nil {t.Fatal(jerr)}
    t.Logf("SHARING_RESULT %d/%d/%d %x",unit,i,pass,sha256.Sum256(data))
    if n!=nil {b,_:=json.Marshal(n);saved=append(saved,n);snapshots=append(snapshots,string(b))}
    for k,n:=range saved {b,_:=json.Marshal(n);if string(b)!=snapshots[k] {t.Fatal("retained result changed")}}
   }
  }
 }
}
func BenchmarkSharingParse(b *testing.B) {
 small,large,_:=sharingInputs()
 for _,item:=range []struct{name,input string}{{"small",small},{"large",large}} {
  for _,unit:=range []Unit{CodePoints,Bytes} {
   for _,api:=range []string{"Node","AST","Recognize"} {
    b.Run(fmt.Sprintf("%s/%s/%d",item.name,api,unit),func(b *testing.B) {
     run:=func()error {switch api {case "Node":_,err:=Parse(item.input,WithUnit(unit));return err;case "AST":_,err:=ParseAST(item.input,WithUnit(unit));return err;default:return Recognize(item.input,WithUnit(unit))}}
     if err:=run();err!=nil {b.Fatal(err)}
     b.ReportAllocs();b.SetBytes(int64(len(item.input)));b.ResetTimer()
     for b.Loop() {if err:=run();err!=nil {b.Fatal(err)}}
    })
   }
  }
 }
}
`

const generatedSharingTSMeasure = `import {performance} from "node:perf_hooks";
import {createHash} from "node:crypto";
import {parse,recognize,marshal,CodePoints,Bytes} from "./parser.ts";
const item=JSON.stringify({日本語:[1,true,null,"😀"]});
const inputs="LANG"==="json"
 ? [item,"["+Array(1000).fill(item).join(",")+"]","{\"x\":"]
 : ["const 日本語 = 1 + 2;\n","const 日本語 = 1 + 2;\n".repeat(1000),"function f( { return 1;"];
if(process.argv.includes("--check")) {
 for(const unit of [CodePoints,Bytes]) for(let i=0;i<inputs.length;i++) for(const input of [inputs[i],new TextEncoder().encode(inputs[i])]) {
  const r=parse(input,unit),rec=recognize(input,unit);
  if(i<2 && (r.error!==null || rec!==null)) throw new Error("valid input failed");
  if(i===2 && (r.error===null || rec===null)) throw new Error("invalid input accepted");
  console.log(createHash("sha256").update(JSON.stringify([marshal(r.node),r.error?.message,r.node?.toString(),rec?.message])).digest("hex"));
 }
} else {
 const out=[];
 for(const [index,text] of inputs.slice(0,2).entries()) for(const unit of [CodePoints,Bytes]) for(const rec of [false,true]) {
  const run=()=>{const err=rec?recognize(text,unit):parse(text,unit).error;if(err!==null) throw err;};
  const n=index===0?10000:10;
  for(let i=0;i<(index===0?1000:3);i++) run();
  global.gc();const start=performance.now();for(let i=0;i<n;i++) run();
  out.push({size:index===0?"small":"large",unit,recognize:rec,ms:(performance.now()-start)/n});
 }
 console.log(JSON.stringify(out));
}
`

func TestGeneratedSharingFailedLowering(t *testing.T) {
	c := typedLocalLayoutCases[0]
	p := compile(t, c.src)
	g := &generator{prog: p, table: "rules"}
	g.desc(fixedDescs[0])
	if !directOK(p.byName["body"]) {
		t.Fatal("fixture must enter speculative direct lowering")
	}
	if body := g.directNodeBody(p.byName["body"], newScope()); body != "" {
		t.Fatal("fixture must reject direct lowering after emitting its literal")
	}
	if g.vars.Len() != 0 || len(g.literalNames) != 0 {
		t.Fatal("failed lowering must discard newly emitted literal cache entries")
	}
	if testing.Short() {
		return
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	testGeneratedParsersCorpus(t, goBin, []genCase{c}, GenOptions{})
	testGeneratedTypesCorpus(t, goBin, typedLocalLayoutCases[:5], GenOptions{disableMethodSharing: true, disableLiteralSharing: true})
}
