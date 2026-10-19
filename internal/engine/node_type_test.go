package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ornew/pego/internal/syntax"
)

// nodeTypeGrammar uses named node types: rule conversions, constructors, unions of named
// node types, and the node members and functions on them.
const nodeTypeGrammar = `type Num = node[string]
type Ident = node[string]
type Pair = node[(Num, Ident)]
type Nums = node[[]Num]
type Any = node[_]
type Expr ~ Num | Ident
type Out = { Items []Expr; Pair *Pair; Len int; Text string; Start int; Nums *Nums; Any *Any }
rule main Out = items:spaced* p:pair? ns:nums? a:anything? $$ => Out{
  Items: $items.Value, Pair: $p, Len: foldl(0, $items, (n, x) => n + len(x)),
  Text: foldl("", $items, (s, x) => s + text(x)), Start: foldl(0, $items, (n, x) => x.Start), Nums: $ns, Any: $a
}
rule spaced Expr = -" "* x:item => $x
rule item Expr = num / ident
rule num Num = [0-9]+ #atomic
rule ident Ident = t:([a-z]+ #atomic) => Ident{$t}
rule pair Pair = "<" t:(num -"," ident) ">" => Pair{$t}
rule nums Nums = "[" ns:num* "]" => Nums{$ns}
rule anything Any = "!" x:("a" / "bc") => Any{$x}
`

func TestNamedNodeTypesAcrossBackends(t *testing.T) {
	inputs := []string{"", "ab", "ab 12 c3", "<1,x>", "ab <12,y>[1]!bc", "12 ?", "[]!a"}
	for _, sourceInfo := range []bool{false, true} {
		for _, noTypeCheck := range []bool{false, true} {
			prog := compileBare(t, nodeTypeGrammar, Options{SourceInfo: sourceInfo, NoTypeCheck: noTypeCheck})
			for _, input := range inputs {
				want := resultJSON(prog.ParseWith("main", input, ParseOptions{Backend: Closure}))
				for _, backend := range []Backend{Bytecode, BytecodeIterative} {
					got := resultJSON(prog.ParseWith("main", input, ParseOptions{Backend: backend}))
					if got != want {
						t.Errorf("sourceInfo=%t noTypeCheck=%t %s %q:\n got %s\nwant %s", sourceInfo, noTypeCheck, backend, input, got, want)
					}
				}
			}
		}
	}
	prog := compileBare(t, nodeTypeGrammar)
	n, err := prog.Parse("main", "ab 12<1,x>[3]")
	if err != nil {
		t.Fatal(err)
	}
	got := resultJSON(n, err)
	for _, want := range []string{`{"type":"Ident","start":0,"end":2,"value":"ab"}`, `{"type":"Num","start":3,"end":5,"value":"12"}`, `"Pair":{"type":"Pair","start":6`, `"Nums":{"type":"Nums"`, `"Len":4`, `"Text":"ab12"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	items, _ := n.Field("Items").(*Node)
	if items == nil || items.Elem(0).Type() != "Ident" || items.Elem(1).Type() != "Num" {
		t.Fatalf("item types: %s", got)
	}
}

// TestNamedNodeTypeErrors checks the type rules of named node types: a named node type is
// assignable to its underlying node type but not the reverse, distinct named node types are not
// assignable to each other, and a conversion (a rule's declared type or N{x}) needs a node whose
// underlying type fits.
func TestNamedNodeTypeErrors(t *testing.T) {
	const defs = "type N = node[string]\ntype M = node[string]\ntype O = { A N; B node[string] }\ntype U ~ N | M\n"
	for _, tc := range []struct{ src, want string }{
		{`rule main O = x:"a" => O{A: $x}`, "cannot use node[string] as N in field A of O"},
		{`rule main O = x:m => O{A: $x}` + "\nrule m M = \"a\"", "cannot use M as N in field A of O"},
		{`rule main N = x:"a" => N{$x, $x}`, "a node type constructor takes one value: write N{x}"},
		{`rule main N = x:"a" => N{}`, "a node type constructor takes one value: write N{x}"},
		{`rule main O = x:"a" => O{$x}`, "a struct constructor names its fields: write O{Field: value}"},
		{`rule main N = "a" => N{1}`, "cannot convert int to N"},
		{`rule main N = "a" => N{"a"}`, "cannot convert string to N"},
		{`rule main N = "a" "b"`, "rule main produces node[(node[string], node[string])], which cannot be converted to N"},
		{`rule main N = x:"a" => O{A: N{$x}}`, "rule main produces O, which cannot be converted to N"},
		{`rule main U = "a"`, "rule main produces node[string], which is not assignable to N | M"},
		{`rule main N = x:("a" / "b" "c") => N{$x}`, "cannot convert node[string] | node[(node[string], node[string])] to N"},
		{`rule main = x:"a" => P{$x}`, "P is not a struct type or a node type"},
	} {
		if got := compileError(t, defs+tc.src); !strings.Contains(got, tc.want) {
			t.Errorf("%s:\n got %s\nwant %s", tc.src, got, tc.want)
		}
	}
	for _, src := range []string{
		// N is assignable to node[string], and to a union containing it.
		`rule main O = x:n => O{A: $x, B: $x}` + "\nrule n N = \"a\"",
		`rule main U = n` + "\nrule n N = \"a\"",
		// A conversion takes any node whose underlying type fits, named or not.
		`rule main N = x:m => N{$x}` + "\nrule m M = \"a\"",
		`rule main N = m` + "\nrule m M = \"a\"",
		// Casts name the type.
		`rule main N = x:u => $x.(N)` + "\nrule u U = n\nrule n N = \"a\"",
	} {
		compileBare(t, defs+src)
	}
}

// TestNamedNodeTypeConversions runs conversions and casts across the backends: conversions keep
// the range and the value, a cast checks the type name, and a recursive node type reads its own
// elements.
func TestNamedNodeTypeConversions(t *testing.T) {
	for _, tc := range []struct {
		src    string
		inputs map[string]string
	}{
		{`type N = node[string]
type M = node[string]
type U ~ N | M
rule main = x:u $$ => $x.(M)
rule u U = n / m
rule n N = "n"
rule m M = "m"`, map[string]string{
			"m": `{"node":{"type":"M","start":0,"end":1,"value":"m"}}`,
			"n": `{"err":"action in main: cannot cast N to M","node":null}`,
		}},
	} {
		for _, sourceInfo := range []bool{false, true} {
			prog := compileBare(t, tc.src, Options{SourceInfo: sourceInfo})
			for input, want := range tc.inputs {
				for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
					if got := resultJSON(prog.ParseWith("main", input, ParseOptions{Backend: backend})); got != want {
						t.Errorf("%s %q:\n got %s\nwant %s", backend, input, got, want)
					}
				}
			}
		}
	}
	// A recursive node type.
	prog := compileBare(t, `type Tree = node[[]Tree]
type Out = { Size int; First *Tree }
rule main Out = t:tree $$ => Out{Size: len($t.Value), First: $t.Value[0]}
rule tree Tree = "(" ts:tree* ")" => Tree{$ts}`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		got := resultJSON(prog.ParseWith("main", "(()(()))", ParseOptions{Backend: backend}))
		if want := `"First":{"type":"Tree","start":2,"end":2,"value":[]},"Size":2`; !strings.Contains(got, want) {
			t.Errorf("%s: got %s, want %s", backend, got, want)
		}
	}
	// Without the type check, a conversion checks the underlying type at run time.
	for _, tc := range []struct{ src, input, want string }{
		{`type N = node[string]
rule main N = x:("a" / "b" "c") => N{$x}`, "bc", "cannot convert node[(_, _)] to N"},
		{`type N = node[string]
rule main N = "a" / "b" "c"`, "bc", "cannot use node[(_, _)] as node[string] in result of rule main"},
		// A conversion through an alias names the type itself, as Type() does.
		{`type N = node[string]
type A ~ N
rule main A = x:("a" / "b" "c") => A{$x}`, "bc", "cannot convert node[(_, _)] to N"},
	} {
		prog := compileBare(t, tc.src, Options{NoTypeCheck: true})
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			if got := resultJSON(prog.ParseWith("main", tc.input, ParseOptions{Backend: backend})); !strings.Contains(got, tc.want) {
				t.Errorf("%s %q: got %s, want %s", backend, tc.input, got, tc.want)
			}
			if got := resultJSON(prog.ParseWith("main", "a", ParseOptions{Backend: backend})); !strings.Contains(got, `{"type":"N","start":0,"end":1,"value":"a"}`) {
				t.Errorf("%s: got %s", backend, got)
			}
		}
	}
	// The generated parsers convert through an alias with the same name.
	g, err := syntax.Parse(`type N = node[string]
type A ~ N
rule main A = x:"a" => A{$x}`)
	if err != nil {
		t.Fatal(err)
	}
	goCode, err := Generate(g, GenOptions{Package: "p", Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	tsCode, err := GenerateTS(g, GenOptions{Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(goCode), `c.convert("N", `) || !strings.Contains(string(tsCode), `c.convert("N", `) {
		t.Errorf("generated conversions do not name N")
	}
}

// TestGeneratedNamedNodeTypes checks the Go types that pego gen -types gives named node types: a
// defined type embedding TNode of the underlying value, which implements the unions it is a
// member of.
func TestGeneratedNamedNodeTypes(t *testing.T) {
	runGeneratedNamedNodeProgram(t, nodeTypeGrammar, `var (
	_ p.Expr = (*p.Num)(nil)
	_ p.Expr = (*p.Ident)(nil)
)

func main() {
	out, err := p.ParseAST("ab 12<1,x>[3]!bc")
	if err != nil {
		panic(err)
	}
	for _, it := range out.Items {
		switch x := it.(type) {
		case *p.Num:
			fmt.Println("Num", x.Value, x.Start, x.End)
		case *p.Ident:
			fmt.Println("Ident", x.Value, x.Start, x.End)
		}
	}
	var pair p.TNode[p.Tuple2[*p.Num, *p.Ident]] = out.Pair.TNode
	fmt.Println("Pair", pair.Value.E0.Value, pair.Value.E1.Value, pair.Start)
	var nums []*p.Num = out.Nums.Value
	fmt.Println("Nums", len(nums), nums[0].Value)
	fmt.Println("Any", out.Any.Value)
}
`, "Ident ab 0 2\nNum 12 3 5\nPair 1 x 6\nNums 1 3\nAny bc\n")
}

// TestGeneratedNamedNodesAsPlainNodes checks that generated typed Go converts the nodes of named
// node types where a plain node type is expected, also when the same node is converted as both.
func TestGeneratedNamedNodesAsPlainNodes(t *testing.T) {
	const src = `type Num = node[string]
type Nums = node[[]Num]
type P = node[(Num, Num)]
type O = { C *node[string]; D []node[string]; E *Nums; F node[(node[string], node[string])]; G []Num; H *P; I node[_] }
rule main O = ns:nums p:pair $$ => O{C: $ns.Value[0], D: $ns.Value, E: $ns, F: $p, G: $ns.Value, H: $p, I: $ns}
rule nums Nums = "[" ns:num* "]" => Nums{$ns}
rule num Num = [0-9] #atomic
rule pair P = "<" t:(num num) ">" => P{$t}
`
	runGeneratedNamedNodeProgram(t, src, `func main() {
	out, err := p.ParseAST("[12]<34>")
	if err != nil {
		panic(err)
	}
	fmt.Println("C", out.C.Value, out.C.Start)
	fmt.Println("D", len(out.D), out.D[0].Value, out.D[1].Value)
	fmt.Println("E", out.E.Value[0].Value, out.E.Value[0] == out.G[0])
	fmt.Println("F", out.F.Value.E0.Value, out.F.Value.E1.Value)
	fmt.Println("G", out.G[1].Value, out.G[1].End)
	fmt.Println("H", out.H.Value.E0.Value, out.H.Value.E1.Value)
	fmt.Println("I", len(out.I.Value.([]any)))
}
`, "C 1 1\nD 2 1 2\nE 1 true\nF 3 4\nG 2 3\nH 3 4\nI 2\n")
}

// runGeneratedNamedNodeProgram generates typed Go for src as package p, runs the program body
// with it, and compares its output with want.
func runGeneratedNamedNodeProgram(t *testing.T, src, body, want string) {
	t.Helper()
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Generate(g, GenOptions{Package: "p", Start: "main", Types: true})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, content := range map[string]string{
		"go.mod":      "module gentest\n\ngo 1.24\n",
		"p/parser.go": string(code),
		"main.go":     "package main\n\nimport (\n\t\"fmt\"\n\n\t\"gentest/p\"\n)\n\n" + body,
	} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(goBin, "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -trimpath", "GOTOOLCHAIN=local")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, stderr.String())
	}
	if string(out) != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}

// A module is rejected if a node type has fields or is not over a node type, if a rule converts
// its value to a type that is not a named node type or a text struct, or if ENODE names a type
// that is not a named node type.
func TestNamedNodeTypeModuleValidation(t *testing.T) {
	prog := compileBare(t, nodeTypeGrammar)
	base := prog.Module()
	if err := validateModule(base); err != nil {
		t.Fatal(err)
	}
	str := func(m *Module, s string) int {
		for i, x := range m.Strings {
			if x == s {
				return i
			}
		}
		t.Fatalf("no string %q", s)
		return -1
	}
	clone := func() *Module {
		m := *base
		m.Types = append([]TypeInfo(nil), base.Types...)
		m.Rules = append([]RuleInfo(nil), base.Rules...)
		m.Exprs = append([]Instr(nil), base.Exprs...)
		return &m
	}
	for name, tc := range map[string]struct {
		edit func(m *Module)
		want string
	}{
		"fields": {func(m *Module) {
			for i := range m.Types {
				if m.Types[i].Node >= 0 {
					m.Types[i].Fields = []int{str(m, "Text")}
				}
			}
		}, "has fields"},
		"underlying": {func(m *Module) {
			for i := range m.Types {
				if m.Types[i].Node >= 0 {
					m.Types[i].Node = str(m, "Out")
				}
			}
		}, "is not over a node type"},
		"rule": {func(m *Module) {
			for i := range m.Rules {
				if m.Rules[i].NodeType >= 0 {
					m.Rules[i].NodeType = str(m, "main")
				}
			}
		}, "is not a named node type"},
		"enode": {func(m *Module) {
			for i := range m.Exprs {
				if m.Exprs[i].Op == ENode {
					m.Exprs[i].A = int32(str(m, "Out"))
				}
			}
		}, "is not a declared node type"},
	} {
		m := clone()
		tc.edit(m)
		if err := validateModule(m); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want %s", name, err, tc.want)
		}
	}
}

func TestGeneratedTSTypedNamedNodeTypes(t *testing.T) {
	consumer := `import { parseAST } from "./parser.ts";
import type { Num, Ident, Pair, Nums, Any, Expr } from "./parser.ts";
` + tsTypedAssert + `
const r = parseAST("ab 12<1,x>[1]!bc");
check(r.error === null && r.value !== null, "parse success");
const v = r.value;
const items: readonly Expr[] = v.Items ?? [];
const seen: string[] = [];
for (const x of items) {
  if (x === null) continue;
  switch (x.type) {
    case "Num": { const n: Num = x; seen.push("Num " + n.value + " " + n.start + " " + n.end); break; }
    case "Ident": { const i: Ident = x; seen.push("Ident " + i.value + " " + i.start + " " + i.end); break; }
  }
}
check(seen.join(";") === "Ident ab 0 2;Num 12 3 5", "union members: " + seen.join(";"));
const p: Pair | null = v.Pair;
check(p !== null && p.type === "Pair" && p.value?.[0]?.type === "Num" && p.value[1]?.value === "x", "pair " + JSON.stringify(p));
const ns: Nums | null = v.Nums;
check(ns !== null && ns.type === "Nums" && ns.value?.length === 1 && ns.value[0]?.type === "Num", "nums");
const a: Any | null = v.Any;
check(a !== null && a.type === "Any", "any " + stable(a));
`
	runTypedTSFixture(t, nodeTypeGrammar, consumer, GenOptions{}, "")
}

// TestGeneratedTSTypedNamedNodesAsPlainNodes is TestGeneratedNamedNodesAsPlainNodes for typed
// TypeScript.
func TestGeneratedTSTypedNamedNodesAsPlainNodes(t *testing.T) {
	const src = `type Num = node[string]
type Nums = node[[]Num]
type P = node[(Num, Num)]
type O = { C *node[string]; D []node[string]; E *Nums; F node[(node[string], node[string])]; G []Num; H *P }
rule main O = ns:nums p:pair $$ => O{C: $ns.Value[0], D: $ns.Value, E: $ns, F: $p, G: $ns.Value, H: $p}
rule nums Nums = "[" ns:num* "]" => Nums{$ns}
rule num Num = [0-9] #atomic
rule pair P = "<" t:(num num) ">" => P{$t}
`
	consumer := `import { parseAST } from "./parser.ts";
` + tsTypedAssert + `
const v = parseAST("[12]<34>").value;
check(v !== null, "parse");
const got = [v.C?.value, v.D?.map(d => d?.value).join(","), v.E?.value?.[0]?.value, v.E?.value?.[0] === v.G?.[0],
  v.F?.value?.[0]?.value, v.F?.value?.[1]?.value, v.G?.[1]?.end, v.H?.value?.[0]?.value, v.H?.type].join(" ");
check(got === "1 1,2 1 true 3 4 3 3 P", "got " + got + " " + stable(v));
`
	runTypedTSFixture(t, src, consumer, GenOptions{}, "")
}

// TestNamedNodeTypeResolution checks types that refer to each other through named node types: an
// alias may refer to itself through one, a recursive named node type keeps its value inside other
// node types, and many mutually recursive named node types resolve quickly.
func TestNamedNodeTypeResolution(t *testing.T) {
	for _, src := range []string{
		`type Num = node[string]
type Lst = node[[]Expr]
type Expr ~ Num | Lst
rule main Expr = num / lst
rule num Num = [0-9] #atomic
rule lst Lst = "(" xs:main* ")" => Lst{$xs}`,
		`type Tree = node[[]Tree]
type O = { F node[[]Tree] }
rule main O = t:tree $$ => O{F: $t.Value[0]}
rule tree Tree = "(" ts:tree* ")" => Tree{$ts}`,
	} {
		g, err := syntax.Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		prog, err := Compile(g, Options{})
		if err == nil {
			_, err = prog.Parse("main", "((()))")
		}
		if err != nil {
			t.Errorf("%s: %v", src, err)
		}
	}

	const n = 20
	var b strings.Builder
	alts := make([]string, n)
	for i := range alts {
		alts[i] = fmt.Sprintf("T%d", i)
	}
	for i := range n {
		fmt.Fprintf(&b, "type T%d = node[[](%s)]\n", i, strings.Join(alts, " | "))
		fmt.Fprintf(&b, "rule r%d T%d = \"(\" xs:r%d* \")\" => T%d{$xs}\n", i, i, (i+1)%n, i)
	}
	start := time.Now()
	g, err := syntax.Parse(b.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(g, Options{}); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("%d mutually recursive node types compiled in %v", n, d)
	}
}
