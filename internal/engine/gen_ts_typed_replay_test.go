package engine

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/syntax"
)

// Compare public typed values with the existing generated Go Node-conversion
// route. Normalize spans, integer/text transport and CST Nodes explicitly;
// neither backend's JSON shape is being used as the other's value contract.
func TestGeneratedTSTypedReplay(t *testing.T) {
	if testing.Short() {
		t.Skip("runs generated Go/TypeScript conversion references")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	tsc, err := exec.LookPath("tsc")
	if err != nil {
		t.Skip("tsc not found")
	}
	cases := append([]genCase(nil), aliasCases...)
	cases = append(cases, genCase{"typed", typedGrammar, []string{"f(1,x)!?;[3];zz", "f(1,(;g();", "f(a);", ""}})
	for _, c := range genCorpus(t) {
		switch c.name {
		case "Operators Rule explicit default action", "fold and lists", "runtime error", "cut and lookahead", "typed/OperatorsRule_recovery", "typed/OperatorsRule_framed_cuts", "typed/LR_indirect", "typed/LR_recovery", "typed/OperatorsRule_positional_actions", "diagnostic attr":
			cases = append(cases, c)
		}
	}
	cases = append(cases, genCase{"mixed union recovery", `type Word terminal
type Mixed = Word | int
type U = Word | Match
type R struct { A Mixed; B U; C Word; D any; E []Word; O *int; I int }
rule main R = x:word #recover(skip=[^;]+ ";") xs:word #recover(skip=[^;]+ ";")* => R{A: $x, B: $x, C: $x, D: $x, E: $xs, I: 7}
rule word Word = [a-z😀]+ ";"`, []string{"😀;abc;", "1;abc;2;", "", "x;"}})
	cases = append(cases, genCase{"shared fold", tsTypedDAGTypes + `rule main Tree = first:leaf rest:leaf* => foldl($first, $rest, (acc, i) => Branch{Left: $acc, Right: $acc})`, []string{"x", "xxxxxxxx"}}, genCase{"shared LR", tsTypedDAGTypes + `rule main Tree = branch / leaf
rule branch Branch = prev:main "x" => Branch{Left: $prev, Right: $prev}`, []string{"x", "xxxxxxxx"}}, genCase{"raw Unicode text", `type R struct { Text string; Token Match }
rule main R = x:.* #atomic => R{Text: text($x), Token: $x}`, []string{"😀é", string([]byte{0x61, 0xff, 0xe2, 0x82})}})
	cases = append(cases, genCase{"inferred nil recovery", `rule leaf = "a" => nil
rule main = leaf #recover(skip=.+)`, []string{"a", "!", ""}}, genCase{"inferred never recovery", `rule leaf = leaf
rule main = leaf #recover(skip=.+)`, []string{"!", ""}})
	cases = append(cases, genCase{"nil-valued expression recovery", `rule main = (-"a") #recover(skip=.+)`, []string{"a", "!", ""}}, genCase{"inferred never start", `rule main = main`, []string{"!", ""}})
	dir := t.TempDir()
	write := func(path, text string) {
		t.Helper()
		p := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module replay\n\ngo 1.24\n")
	write("package.json", `{"type":"module"}`)
	var imports, parsers, recognizers, terminalNames, valueNames strings.Builder
	var inputs [][]string
	var files []string
	for i, c := range cases {
		g, err := syntax.Parse(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		goCode, err := Generate(g, GenOptions{Start: "main", Package: fmt.Sprintf("g%d", i), Types: true, convertTypes: true, Recognize: true})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		prog, err := Compile(g, Options{})
		if err != nil {
			t.Fatal(err)
		}
		meta := &typedGen{g: &generator{prog: prog}, info: prog.typed, names: map[string]string{}, unions: map[string]string{}}
		meta.name()
		for name, exported := range meta.names {
			fmt.Fprintf(&valueNames, "%q:%q,\n", fmt.Sprintf("replay/g%d.%s", i, exported), name)
			if _, ok := prog.types[name].(*grammar.TerminalSpec); ok {
				fmt.Fprintf(&terminalNames, "%q:true,\n", fmt.Sprintf("replay/g%d.%s", i, exported))
			}
		}
		fmt.Fprintf(&terminalNames, "%q:true,\n%q:true,\n", fmt.Sprintf("replay/g%d.Match", i), fmt.Sprintf("replay/g%d.Error", i))
		write(fmt.Sprintf("g%d/parser.go", i), string(goCode))
		tsCode, err := GenerateTS(g, GenOptions{Start: "main", Types: true, Recognize: true})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		file := fmt.Sprintf("g%d.ts", i)
		files = append(files, file)
		write(file, string(tsCode))
		fmt.Fprintf(&imports, "g%d \"replay/g%d\"\n", i, i)
		fmt.Fprintf(&parsers, "func(s string, bytes bool)(any,error){u:=g%d.CodePoints; if bytes {u=g%d.Bytes};return g%d.ParseAST(s,g%d.WithUnit(u))},\n", i, i, i, i)
		fmt.Fprintf(&recognizers, "func(s string, bytes bool)error{u:=g%d.CodePoints; if bytes {u=g%d.Bytes};return g%d.Recognize(s,g%d.WithUnit(u))},\n", i, i, i, i)
		var in []string
		for _, s := range c.inputs {
			in = append(in, base64.StdEncoding.EncodeToString([]byte(s)))
		}
		inputs = append(inputs, in)
	}
	b, _ := json.Marshal(inputs)
	write("inputs.json", string(b))
	write("main.go", "package main\nimport (\"encoding/base64\";\"encoding/json\";\"fmt\";\"os\";\"reflect\";\"strconv\";"+imports.String()+")\nvar parsers=[]func(string,bool)(any,error){"+parsers.String()+"}\nvar recognizers=[]func(string,bool)error{"+recognizers.String()+"}\nvar terminalTypes=map[string]bool{"+terminalNames.String()+"}\nvar valueNames=map[string]string{"+valueNames.String()+"}\n"+tsTypedGoOracle)
	write("replay.mjs", tsTypedReplayHarness)
	cmd := exec.Command(tsc, append([]string{"--noEmit", "--target", "es2020", "--lib", "es2020", "--strict", "--noUnusedLocals", "--noUnusedParameters", "--noUncheckedIndexedAccess", "--noPropertyAccessFromIndexSignature", "--erasableSyntaxOnly"}, files...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("replay tsc: %v\n%s", err, out)
	}
	cmd = exec.Command("go", "run", ".", "inputs.json")
	cmd.Dir = dir
	want, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Go oracle: %v\n%s", err, want)
	}
	cmd = exec.Command(node, "replay.mjs")
	cmd.Dir = dir
	got, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("TS replay: %v\n%s", err, got)
	}
	// Node 22 can report a strip-types experimental warning on stderr.
	var rows []string
	for _, row := range strings.Split(string(got), "\n") {
		if strings.HasPrefix(row, "{") {
			rows = append(rows, row)
		}
	}
	ws := bufio.NewScanner(strings.NewReader(string(want)))
	for i, row := range rows {
		if !ws.Scan() {
			t.Fatalf("extra TS row %d", i)
		}
		var a, b any
		if err := json.Unmarshal(ws.Bytes(), &a); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(row), &b); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(a, b) {
			t.Errorf("row %d\nGo %s\nTS %s", i, ws.Text(), row)
		}
	}
	if len(rows) == 0 || ws.Scan() || ws.Err() != nil {
		t.Fatal("missing/malformed TS replay rows", ws.Err())
	}
	t.Logf("compared %d typed results from %d grammars with generated Go conversion", len(rows), len(cases))
}

const tsTypedGoOracle = `
func text(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
func normal(v reflect.Value) any {
  if !v.IsValid() {return nil}
  if v.Kind()==reflect.Interface {if v.IsNil(){return nil};return normal(v.Elem())}
  if v.Kind()==reflect.Pointer {
    if v.IsNil() {return nil}
    if n,ok:=v.Interface().(interface{Type()string;Rule()string});ok {
      x:=v.Elem(); fields:=map[string]any{};fs:=x.FieldByName("Fields")
      for i:=0;i<fs.Len();i++ {f:=fs.Index(i);fields[f.FieldByName("Name").String()]=normal(f.FieldByName("Value"))}
      kids:=normal(x.FieldByName("Children"));if x.FieldByName("Children").Len()==0{kids=nil}
      return map[string]any{"type":n.Type(),"rule":n.Rule(),"start":x.FieldByName("Start").Int(),"end":x.FieldByName("End").Int(),"text":text(x.FieldByName("Text").String()),"children":kids,"fields":fields}
    }
    return normal(v.Elem())
  }
  switch v.Kind() {
  case reflect.Int: return "int:"+strconv.FormatInt(v.Int(),10)
  case reflect.String:return text(v.String())
  case reflect.Bool:return v.Bool()
  case reflect.Slice:
    if v.IsNil(){return nil};out:=make([]any,v.Len());for i:=range out {out[i]=normal(v.Index(i))};return out
  case reflect.Struct:
    typeKey:=v.Type().PkgPath()+"."+v.Type().Name();name:=v.Type().Name();if n,ok:=valueNames[typeKey];ok{name=n};out:=map[string]any{"type":name}
    for i:=0;i<v.NumField();i++ {if v.Type().Field(i).Anonymous {sp:=v.Field(i);out["start"]=sp.FieldByName("Start").Int();out["end"]=sp.FieldByName("End").Int()}}
    for i:=0;i<v.NumField();i++ {name:=v.Type().Field(i).Name;if v.Type().Field(i).Anonymous{continue};if name=="Text"&&terminalTypes[typeKey]{name="text"};if name=="Message"&&v.Type().Name()=="Error"{name="message"};out[name]=normal(v.Field(i))};return out
  }
  panic(v.Kind())
}
func main(){data,_:=os.ReadFile(os.Args[1]);var inputs [][]string;json.Unmarshal(data,&inputs);for i,in:=range inputs {for _,b64:=range in {b,_:=base64.StdEncoding.DecodeString(b64);for _,bytes:=range []bool{false,true}{v,e:=parsers[i](string(b),bytes);var err,rec any;if e!=nil {err=e.Error()};if e:=recognizers[i](string(b),bytes);e!=nil{rec=e.Error()};row,_:=json.Marshal(map[string]any{"grammar":i,"bytes":bytes,"input":b64,"value":normal(reflect.ValueOf(v)),"error":err,"recognize":rec});fmt.Println(string(row))}}}}
`

const tsTypedReplayHarness = `import {readFileSync} from "node:fs";
const inputs=JSON.parse(readFileSync("inputs.json","utf8"));
function text(s){const out=[];for(const ch of s){const c=ch.codePointAt(0);if(c>=0xdc80&&c<=0xdcff)out.push(c&255);else out.push(...Buffer.from(ch,"utf8"));}return Buffer.from(out).toString("base64");}
function normal(v,m){if(v===null)return null;if(typeof v==="number"||typeof v==="bigint")return "int:"+v;if(typeof v==="string")return text(v);if(typeof v==="boolean")return v;if(Array.isArray(v))return v.map(x=>normal(x,m));if(v instanceof m.Node){const fs={};for(const f of v.fields)fs[f.name]=normal(f.value,m);return {type:v.type,rule:v.rule,start:v.start,end:v.end,text:text(v.text),children:v.children.length===0?null:v.children.map(x=>normal(x,m)),fields:fs};}const out={};for(const [k,x] of Object.entries(v)){out[k]=k==="type"||k==="start"||k==="end"?x:normal(x,m);}return out;}
for(let i=0;i<inputs.length;i++){const m=await import("./g"+i+".ts");for(const b64 of inputs[i]){const input=new Uint8Array(Buffer.from(b64,"base64"));for(const unit of [m.CodePoints,m.Bytes]){const r=m.parseAST(input,unit),n=m.parse(input,unit),rec=m.recognize(input,unit);if(r.error?.message!==n.error?.message)throw new Error("typed error changed");console.log(JSON.stringify({grammar:i,bytes:unit===m.Bytes,input:b64,value:normal(r.value,m),error:r.error?.message??null,recognize:rec?.message??null}));}}}
`
