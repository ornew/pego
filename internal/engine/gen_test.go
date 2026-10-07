package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
		{"anchors and classes", `
def main = (^ @(?a-z)+ $ "\n"?)* $$ (?^\n)? _`, []string{"ab\ncd\n", "ab\n1", "日本", "é\n\xff"}},
		{"scans", `
def main = a:(?a-z)* -(?0-9){2,3} -.{1,2} -(?^,)+ "," rest:@.*`, []string{"ab12xy,z", "123,", "1", "abc12éq,é"}},
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
// the same trees, positions included, and the same errors.
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
	dir := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module gentest\n\ngo 1.24\n")
	var imports, parsers strings.Builder
	var inputs [][]string
	var want []string
	for i, c := range cases {
		g, err := syntax.Parse(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		code, err := Generate(g, GenOptions{Package: fmt.Sprintf("g%d", i), Start: "main"})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		write(fmt.Sprintf("g%d/parser.go", i), string(code))
		fmt.Fprintf(&imports, "\tg%d \"gentest/g%d\"\n", i, i)
		fmt.Fprintf(&parsers, "\tfunc(s string, b bool) (any, error) { u := g%d.CodePoints; if b { u = g%d.Bytes }; n, err := g%d.Parse(s, u); return n, err },\n", i, i, i)
		inputs = append(inputs, c.inputs)
		prog := compile(t, c.src)
		for _, in := range c.inputs {
			for _, unit := range []Unit{CodePoints, Bytes} {
				n, err := prog.ParseWith("main", in, ParseOptions{Unit: unit})
				want = append(want, resultJSON(n, err))
			}
		}
	}
	write("main.go", `package main

import (
	"encoding/json"
	"fmt"
	"os"
`+imports.String()+`)

var parsers = []func(string, bool) (any, error){
`+parsers.String()+`}

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
				fmt.Println(string(b))
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
	for _, pkg := range []string{"foo-bar", "type", "1p"} {
		if _, err := Generate(g, GenOptions{Package: pkg, Start: "main"}); err == nil || err.Error() != fmt.Sprintf("invalid package name %q", pkg) {
			t.Errorf("package %q: got %v", pkg, err)
		}
	}
	bad, _ := syntax.Parse(`def main = x`)
	if _, err := Generate(bad, GenOptions{Package: "p", Start: "main"}); err == nil || !strings.Contains(err.Error(), "undefined rule x") {
		t.Errorf("got %v", err)
	}
}
