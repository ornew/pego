package engine

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

type genCase struct {
	name   string
	src    string
	inputs []string
}

// genCorpus returns the grammars and inputs used to compare backends and generated parsers with the engine.
func genCorpus(t *testing.T) []genCase {
	cases := []genCase{
		{"arith", arith, []string{"1+2*3", "-2^2", "3!!", "(1+2)*3", "1==2==3", "1+", ""}},
		{"ast", ast, []string{"a ? b : c ? d : e", "f(a = 1, b), c", "-a(1)[2]", "a->b-c", "f()", "f(,)"}},
		{"incremental", incrementalGrammar, []string{"x = 1+2\nab,cd\n@pos\n\ny = (3)*-4\n", "x = (1\n", "@\n"}},
		{"records", records, []string{"#records\na=1\nbc=22\n", "#records\nx\n"}},
		{"indent", `
def main = [indent = 0] block $$
def block = item+
def item = s:spaces [len($s) == indent] name:@(?a-z)+ "\n" children?
def children = &(s:spaces) [len($s) > indent] [indent = len($s)] block
def spaces = @" "*`, []string{"a\n  b\n  c\n    d\ne\n", "a\n   b\n c\n"}},
		{"recover", `
def main = stmt* $$
def stmt = (@(?a-z)+ ";") #recover(skip=(?^;)+ ";")`, []string{"ab;1x;cd;9;", "1x;ab"}},
		{"error attr", `
def main = "let" " " name #error(message="expected a name") ";"
def name = @(?a-z)+ (?0-9)*`, []string{"let 1;", "let a", "let ab1;"}},
		{"left recursion", `
def main = a
def a = b "x" / "y"
def b = a "z" / c
def c = a "w" / "v"`, []string{"y", "vx", "yzxwx", "yz"}},
		{"fold and lists", `
type Op struct { Left Node, Op Match, Right Node }
type Node = Op | Match
type Args struct { Items []Match, N int, S string, B bool }
def main = e:expr ";" a:args -> new Args{Items: concat($a.Items, $a.Items), N: len($a.Items) * 2 - 1, S: text($e) + "!", B: !($a.N == 0) || false}
def expr: Node = l:num rest:(op:@("+" / "-") r:num)* -> foldl($l, $rest, (acc, i) => new Op{Left: $acc, Op: $i.op, Right: $i.r})
def args: Args = first:num rest:(-"," x:num)* -> new Args{Items: concat(list($first), map($rest, (r) => $r.x)), N: len($rest)}
def num = @(?0-9)+`, []string{"1+2-3;4,5,6", "1;2", "1-;2"}},
		{"projected and indexed repetition", `
type L struct { Items []Match, Rest node, N int }
def main = first:x rest:(-"," r:x)* -> new L{Items: concat(list($first), map($rest, (e) => $e.r)), Rest: $2, N: len($rest)}
def x = @(?a-z)+`, []string{"a,b,c", "a", "a,"}},
		{"runtime error", `
type A struct { N int }
def main = x:@"a"* -> new A{N: 10 / len($x)}`, []string{"aa", ""}},
		{"cut and lookahead", `
def main = ("a" -- "b" / "a" -- "c") rest
def rest = &(x:"!") "!"? !"?" .*`, []string{"ab!x", "ac", "ab?"}},
		{"memo keyed by variables", `
def main = [n = 1] r "x" / [n = 2] r .*
def r = [n == 1] "a" / [n == 2] "b"`, []string{"ax", "b", "ay", "bx"}},
		{"empty prefix and postfix", `
def main = e $$
def e = pratt {
    operand "x"
    level { prefix "-"? }
    level { postfix "!"? }
}`, []string{"x", "-x!", "--x", ""}},
		// An empty operand and an empty infix operator: an application that consumes nothing ends
		// the expression.
		{"empty operand and infix", `
def main = e $$
def e = pratt {
    operand "a"?
    level { infix left _ }
}`, []string{"a", "", "aa"}},
		{"anchors and classes", `
def main = (^ @(?a-z)+ $ "\n"?)* $$ (?^\n)? _`, []string{"ab\ncd\n", "ab\n1", "日本", "é\n\xff"}},
		{"scans", `
def main = a:(?a-z)* -(?0-9){2,3} -.{1,2} -(?^,)+ "," rest:@.*`, []string{"ab12xy,z", "123,", "1", "abc12éq,é"}},
	}
	// Grammars of typed values with their inputs, from reviews of the typed runtime: each was a
	// case where it differed from converting the result of Parse.
	typed, _ := filepath.Glob("testdata/typed/*.pego")
	for _, gp := range typed {
		src, err := os.ReadFile(gp)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(strings.TrimSuffix(gp, ".pego") + ".json")
		if err != nil {
			t.Fatal(err)
		}
		c := genCase{name: gp, src: string(src)}
		if err := json.Unmarshal(data, &c.inputs); err != nil {
			t.Fatalf("%s: %v", gp, err)
		}
		cases = append(cases, c)
	}
	// Grammars and inputs from examples/.
	grammars, _ := filepath.Glob("../../examples/*/*.pego")
	for _, gp := range grammars {
		src, err := os.ReadFile(gp)
		if err != nil {
			t.Fatal(err)
		}
		inputs, _ := filepath.Glob(filepath.Join(filepath.Dir(gp), "testdata", "*.txt"))
		c := genCase{name: gp, src: string(src)}
		for _, in := range inputs {
			data, _ := os.ReadFile(in)
			c.inputs = append(c.inputs, string(data))
		}
		cases = append(cases, c)
	}
	return cases
}

func resultJSON(n *Node, err error) string {
	out := map[string]any{"node": n}
	if err != nil {
		out["err"] = err.Error()
	}
	data, _ := json.Marshal(out)
	return string(data)
}

// TestGeneratedParsersMatchEngine checks that generated parsers return the same results as the engine:
// the same trees, positions included, and the same errors, and that Recognize returns the errors of
// the engine's recognition.
func TestGeneratedParsersMatchEngine(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	cases := genCorpus(t)
	// Exactly at and just beyond the default nesting limit (DefaultMaxDepth rule calls). Only
	// here: the iterative VM, which other tests run on the corpus, has a higher default.
	cases = append(cases, genCase{"nesting limit", `
def main = n
def n = "(" n ")" / "x"`, []string{strings.Repeat("(", DefaultMaxDepth-2) + "x" + strings.Repeat(")", DefaultMaxDepth-2),
		strings.Repeat("(", DefaultMaxDepth-1) + "x" + strings.Repeat(")", DefaultMaxDepth-1)}})
	// Chains of prefix and right-associative operators count against the limit too (a Pratt
	// expression parses them by recursion).
	cases = append(cases, genCase{"pratt nesting limit", `
def main = x
def x = pratt {
    operand "x"
    level { infix right "^" }
    level { prefix "-" }
}`, []string{strings.Repeat("-", 3*DefaultMaxDepth) + "x", strings.Repeat("x^", 3*DefaultMaxDepth) + "x", strings.Repeat("-", DefaultMaxDepth/4) + "x"}})
	dir := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module gentest\n\ngo 1.24\n")
	var imports, parsers, recognizers strings.Builder
	var inputs [][]string
	var want []string
	for i, c := range cases {
		g, err := syntax.Parse(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		code, err := Generate(g, GenOptions{Package: fmt.Sprintf("g%d", i), Start: "main", Recognize: true})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		write(fmt.Sprintf("g%d/parser.go", i), string(code))
		fmt.Fprintf(&imports, "\tg%d \"gentest/g%d\"\n", i, i)
		fmt.Fprintf(&parsers, "\tfunc(s string, b bool) (any, error) { u := g%d.CodePoints; if b { u = g%d.Bytes }; n, err := g%d.Parse(s, u); return n, err },\n", i, i, i)
		fmt.Fprintf(&recognizers, "\tfunc(s string, b bool) error { u := g%d.CodePoints; if b { u = g%d.Bytes }; return g%d.Recognize(s, u) },\n", i, i, i)
		inputs = append(inputs, c.inputs)
		prog := compile(t, c.src, Options{noProjections: true}) // the generated code projects (project.go)
		for _, in := range c.inputs {
			for _, unit := range []Unit{CodePoints, Bytes} {
				n, err := prog.ParseWith("main", in, ParseOptions{Unit: unit})
				out := resultJSON(n, err)
				_, err = prog.ParseWith("main", in, ParseOptions{Unit: unit, Recognize: true})
				want = append(want, out+" recognize: "+strconv.Quote(fmt.Sprint(err)))
			}
		}
	}
	write("main.go", `package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
`+imports.String()+`)

var parsers = []func(string, bool) (any, error){
`+parsers.String()+`}

var recognizers = []func(string, bool) error{
`+recognizers.String()+`}

func main() {
	var inputs [][]string
	data, _ := os.ReadFile(os.Args[1])
	json.Unmarshal(data, &inputs)
	for i, in := range inputs {
		for _, s := range in {
			for _, bytes := range []bool{false, true} {
				n, err := parsers[i](s, bytes)
				out := map[string]any{"node": n}
				if err != nil {
					out["err"] = err.Error()
				}
				b, _ := json.Marshal(out)
				fmt.Println(string(b) + " recognize: " + strconv.Quote(fmt.Sprint(recognizers[i](s, bytes))))
			}
		}
	}
}
`)
	data, _ := json.Marshal(inputs)
	write("inputs.json", string(data))
	cmd := exec.Command(goBin, "run", ".", "inputs.json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	got := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(got) != len(want) {
		t.Fatalf("got %d results, want %d\n%s", len(got), len(want), out)
	}
	k := 0
	for _, c := range cases {
		for _, in := range c.inputs {
			for _, unit := range []Unit{CodePoints, Bytes} {
				if got[k] != want[k] {
					t.Errorf("%s (%v): input %q\n generated %s\n engine    %s", c.name, unit, in, got[k], want[k])
				}
				k++
			}
		}
	}
}

func TestGenerateErrors(t *testing.T) {
	g, _ := syntax.Parse(`def main = "a"`)
	if _, err := Generate(g, GenOptions{Package: "p", Start: "nope"}); err == nil || err.Error() != "start rule nope is not defined" {
		t.Errorf("got %v", err)
	}
	if _, err := Generate(g, GenOptions{Start: "main"}); err == nil {
		t.Error("expected an error for a missing package name")
	}
	for _, pkg := range []string{"foo-bar", "type", "1p", "_"} {
		if _, err := Generate(g, GenOptions{Package: pkg, Start: "main"}); err == nil || err.Error() != fmt.Sprintf("invalid package name %q", pkg) {
			t.Errorf("package %q: got %v", pkg, err)
		}
	}
	bad, _ := syntax.Parse(`def main = x`)
	if _, err := Generate(bad, GenOptions{Package: "p", Start: "main"}); err == nil || !strings.Contains(err.Error(), "undefined rule x") {
		t.Errorf("got %v", err)
	}
}

// mutants returns n random edits of each input (a character inserted, deleted or replaced, or two
// characters swapped, up to three times), with characters from the inputs, reproducibly for a seed.
func mutants(inputs []string, n int, seed uint64) []string {
	rng := rand.New(rand.NewPCG(seed, 1))
	var pool []rune
	for _, in := range inputs {
		pool = append(pool, []rune(in)...)
	}
	if len(pool) == 0 {
		return nil
	}
	var out []string
	for _, in := range inputs {
		for range n {
			rs := []rune(in)
			for range 1 + rng.IntN(3) {
				i := rng.IntN(len(rs) + 1)
				switch c := pool[rng.IntN(len(pool))]; rng.IntN(4) {
				case 0:
					rs = append(rs[:i], append([]rune{c}, rs[i:]...)...)
				case 1:
					if i < len(rs) {
						rs = append(rs[:i], rs[i+1:]...)
					}
				case 2:
					if i < len(rs) {
						rs[i] = c
					}
				default:
					if i+1 < len(rs) {
						rs[i], rs[i+1] = rs[i+1], rs[i]
					}
				}
			}
			out = append(out, string(rs))
		}
	}
	return out
}

// typedGrammar exercises typed values: unions with CST members, lists of lists, optional
// matches (s always matches, maybe empty; o may be nil), int and bool fields, a user type named like a runtime type (Node) and Error nodes
// left by #recover.
const typedGrammar = `
type Name terminal
type Num terminal
type Node = Call | Name | Num | Seq
type Call struct { Fn Name, Args []Node, Span Match, N int, Opt *Match, Rows [][]Num, Ok bool }
def main: []Node = xs:item* $$ -> concat(map($xs, (x) => $x))
def item: Node = (call / name / num / grp) #recover(skip=(?^;)+ ";")
def call: Call = f:name "(" a:args? ")" s:@"!"? o:"?"? ";" -> new Call{Fn: $f, Args: concat($a), Span: $s, N: len($a), Opt: $o, Rows: list(list()), Ok: true}
def args = first:item rest:(-"," x:item)* -> concat(list($first), map($rest, (r) => $r.x))
def grp = "[" n:num "]" ";"
def name: Name = (?a-z)+
def num: Num = (?0-9)+`

// TestGeneratedTypes checks ParseAST of parsers generated with Types: it compiles for every
// grammar of the corpus and returns the errors of Parse, and it converts the trees of
// typedGrammar into the expected values.
func TestGeneratedTypes(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	// Exactly at and just beyond the nesting limit, through direct rules (as in
	// TestGeneratedParsersMatchEngine).
	cases := append(genCorpus(t), genCase{"nesting limit", `
type R struct { T Match }
def main: R = t:@n -> new R{T: $t}
def n = "(" n ")" / "x"`, []string{strings.Repeat("(", DefaultMaxDepth-2) + "x" + strings.Repeat(")", DefaultMaxDepth-2),
		strings.Repeat("(", DefaultMaxDepth-1) + "x" + strings.Repeat(")", DefaultMaxDepth-1)}})
	cases = append(cases, genCase{"typed", typedGrammar, []string{"f(1,x)!?;[3];zz", "f(1,(;g();", "f(a);"}})
	// The grammars written for direct rules also get mutated inputs, which reach other partial
	// matches and failures than the written ones.
	for i := range cases {
		if strings.Contains(cases[i].name, "typed/direct_") {
			cases[i].inputs = append(cases[i].inputs, mutants(cases[i].inputs, 8, uint64(i))...)
		}
	}
	dir := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module gentest\n\ngo 1.24\n")
	var imports, parsers, converters strings.Builder
	var inputs [][]string
	var wantErr []string
	typedRuntime := 0
	for i, c := range cases {
		g, err := syntax.Parse(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		code, err := Generate(g, GenOptions{Package: fmt.Sprintf("g%d", i), Start: "main", Types: true})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if strings.Contains(string(code), "tparse(trules") {
			typedRuntime++
		}
		// The grammars written for direct rules (gen_direct.go) must compile rules that way.
		if strings.Contains(c.name, "typed/direct_") && !strings.Contains(string(code), "(body inlined)") {
			t.Errorf("%s: no direct rules", c.name)
		}
		write(fmt.Sprintf("g%d/parser.go", i), string(code))
		// The same with ParseAST converting the result of Parse, which the typed runtime must equal.
		code, err = Generate(g, GenOptions{Package: fmt.Sprintf("c%d", i), Start: "main", Types: true, convertTypes: true})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		write(fmt.Sprintf("c%d/parser.go", i), string(code))
		fmt.Fprintf(&imports, "\tg%d \"gentest/g%d\"\n\tc%d \"gentest/c%d\"\n", i, i, i, i)
		fmt.Fprintf(&parsers, "\tfunc(s string, b bool) (any, error) { u := g%d.CodePoints; if b { u = g%d.Bytes }; return g%d.ParseAST(s, u) },\n", i, i, i)
		fmt.Fprintf(&converters, "\tfunc(s string, b bool) (any, error) { u := c%d.CodePoints; if b { u = c%d.Bytes }; return c%d.ParseAST(s, u) },\n", i, i, i)
		inputs = append(inputs, c.inputs)
		prog := compile(t, c.src)
		for _, in := range c.inputs {
			_, err := prog.Parse("main", in)
			wantErr = append(wantErr, fmt.Sprint(err))
		}
	}
	write("main.go", `package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
`+imports.String()+`)

var parsers = []func(string, bool) (any, error){
`+parsers.String()+`}

var converters = []func(string, bool) (any, error){
`+converters.String()+`}

// typeName is the dynamic type of v without the package.
func typeName(v any) string {
	s := fmt.Sprintf("%T", v)
	if i := strings.LastIndex(s, "."); i >= 0 {
		return strings.TrimLeft(s[:i], "[]*") + s[i:]
	}
	return s
}

func main() {
	var inputs [][]string
	data, _ := os.ReadFile(os.Args[1])
	json.Unmarshal(data, &inputs)
	for i, in := range inputs {
		for _, s := range in {
			for _, bytes := range []bool{false, true} {
				v, err := parsers[i](s, bytes)
				cv, cerr := converters[i](s, bytes)
				typed, _ := json.Marshal(map[string]any{"value": v, "err": fmt.Sprint(err)})
				conv, _ := json.Marshal(map[string]any{"value": cv, "err": fmt.Sprint(cerr)})
				if string(typed) != string(conv) || typeName(v)[strings.Index(typeName(v), ".")+1:] != typeName(cv)[strings.Index(typeName(cv), ".")+1:] {
					fmt.Fprintf(os.Stderr, "MISMATCH %d %q (bytes: %v)\n typed %s %s\n conv  %s %s\n", i, s, bytes, typeName(v), typed, typeName(cv), conv)
				}
				if !bytes {
					b, _ := json.Marshal(map[string]any{"value": v, "type": fmt.Sprintf("%T", v), "err": fmt.Sprint(err)})
					fmt.Println(string(b))
				}
			}
		}
	}
	// Parsers of the typed runtime are pooled: parsing concurrently gives the same results.
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i, in := range inputs {
				for _, s := range in {
					v, err := parsers[i](s, false)
					cv, cerr := converters[i](s, false)
					a, _ := json.Marshal(map[string]any{"value": v, "err": fmt.Sprint(err)})
					b, _ := json.Marshal(map[string]any{"value": cv, "err": fmt.Sprint(cerr)})
					if string(a) != string(b) {
						fmt.Fprintf(os.Stderr, "CONCURRENT MISMATCH %d %q\n", i, s)
					}
				}
			}
		}()
	}
	wg.Wait()
}
`)
	data, _ := json.Marshal(inputs)
	write("inputs.json", string(data))
	cmd := exec.Command(goBin, "run", ".", "inputs.json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, stderr.String())
	}
	if stderr.Len() > 0 {
		t.Errorf("the typed runtime differs from converting the result of Parse:\n%s", stderr.String())
	}
	if typedRuntime < 5 {
		t.Errorf("only %d of %d grammars use the typed runtime", typedRuntime, len(cases))
	}
	t.Logf("%d of %d grammars use the typed runtime", typedRuntime, len(cases))
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != len(wantErr) {
		t.Fatalf("got %d results, want %d\n%s", len(lines), len(wantErr), out)
	}
	var got []struct {
		Value json.RawMessage
		Type  string
		Err   string
	}
	for _, l := range lines {
		var r struct {
			Value json.RawMessage
			Type  string
			Err   string
		}
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			t.Fatalf("%v: %s", err, l)
		}
		got = append(got, r)
	}
	k := 0
	for _, c := range cases {
		for _, in := range c.inputs {
			if got[k].Err != wantErr[k] {
				t.Errorf("%s: input %q: error %s, want %s", c.name, in, got[k].Err, wantErr[k])
			}
			k++
		}
	}
	typed := got[len(got)-3:]
	for i, want := range []string{
		`{"Value":[` +
			`{"Start":0,"End":9,"Fn":{"Start":0,"End":1,"Text":"f"},"Args":[{"Start":2,"End":3,"Text":"1"},{"Start":4,"End":5,"Text":"x"}],"Span":{"Start":6,"End":7,"Text":"!"},"N":2,"Opt":{"Start":7,"End":8,"Text":"?"},"Rows":[[]],"Ok":true},` +
			`{"type":"Seq","rule":"grp","start":9,"end":13,"children":[{"type":"Match","start":9,"end":10,"text":"["},{"type":"Num","rule":"num","start":10,"end":11,"text":"3"},{"type":"Match","start":11,"end":12,"text":"]"},{"type":"Match","start":12,"end":13,"text":";"}],"fields":{"n":{"type":"Num","rule":"num","start":10,"end":11,"text":"3"}}},` +
			`{"Start":13,"End":15,"Text":"zz"}],"Type":"[]g37.Node_","Err":"<nil>"}`,
		`{"Value":[` +
			`{"Start":0,"End":1,"Text":"f"},` +
			`{"Start":1,"End":6,"Text":"(1,(;","Message":"1:2: syntax error: expected \"[\", (?0-9), (?a-z)"},` +
			`{"Start":6,"End":7,"Text":"g"},` +
			`{"Start":7,"End":10,"Text":"();","Message":"1:8: syntax error: expected \"[\", (?0-9), (?a-z)"}],"Type":"[]g37.Node_","Err":"1:2: syntax error: expected \"[\", (?0-9), (?a-z)\n1:8: syntax error: expected \"[\", (?0-9), (?a-z)"}`,
		`{"Value":[{"Start":0,"End":5,"Fn":{"Start":0,"End":1,"Text":"f"},"Args":[{"Start":2,"End":3,"Text":"a"}],"Span":{"Start":4,"End":4,"Text":""},"N":1,"Opt":null,"Rows":[[]],"Ok":true}],"Type":"[]g37.Node_","Err":"<nil>"}`,
	} {
		var b strings.Builder
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		enc.Encode(typed[i])
		want = strings.ReplaceAll(want, "g37", fmt.Sprintf("g%d", len(cases)-1))
		if got := strings.TrimSuffix(b.String(), "\n"); got != want {
			t.Errorf("typed %q:\n got %s\nwant %s", cases[len(cases)-1].inputs[i], got, want)
		}
	}
}
