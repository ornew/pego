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

	"github.com/ornew/pego/internal/bytecode"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/value"
)

func TestNamedNodeTypesAcrossBackends(t *testing.T) {
	t.Parallel()
	inputs := []string{"", "ab", "ab 12 c3", "<1,x>", "ab <12,y>[1]!bc", "12 ?", "[]!a"}
	for _, sourceInfo := range []bool{false, true} {
		prog := compileBare(t, gentest.NodeTypeGrammar, compile.Options{SourceInfo: sourceInfo})
		for _, input := range inputs {
			want := resultJSON(prog.ParseWith("main", input, ParseOptions{Backend: Closure}))
			for _, backend := range []Backend{Bytecode, BytecodeIterative} {
				got := resultJSON(prog.ParseWith("main", input, ParseOptions{Backend: backend}))
				if got != want {
					t.Errorf("sourceInfo=%t %s %q:\n got %s\nwant %s", sourceInfo, backend, input, got, want)
				}
			}
		}
	}
	prog := compileBare(t, gentest.NodeTypeGrammar)
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
	items, _ := n.Field("Items").(*value.Node)
	if items == nil || items.Elem(0).Type() != "Ident" || items.Elem(1).Type() != "Num" {
		t.Fatalf("item types: %s", got)
	}
}

// TestNamedNodeTypeErrors checks the type rules of named node types: a named node type is
// assignable to its underlying node type but not the reverse, distinct named node types are not
// assignable to each other, and a conversion (a rule's declared type or N{x}) needs a node whose
// underlying type fits.
func TestNamedNodeTypeErrors(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	gentest.EachMode(t, testNamedNodeTypeConversions)
}

func testNamedNodeTypeConversions(t *testing.T) {
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
			"n": `{"err":"action in main: cannot cast N to M at input position 0 (line 1, column 1)%s","node":null}`,
		}},
	} {
		for _, sourceInfo := range []bool{false, true} {
			prog := compileBare(t, tc.src, compile.Options{SourceInfo: sourceInfo})
			for input, want := range tc.inputs {
				// With source information the error names the grammar position of the cast.
				suffix := ""
				if sourceInfo {
					suffix = "; cast at grammar position 4:26"
				}
				want = strings.Replace(want, "%s", suffix, 1)
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
	// A conversion whose value does not fit the underlying type is a compile error. Without type
	// checking nothing checks it when the conversion runs.
	for _, tc := range []struct{ src, input, want string }{
		{`type N = node[string]
rule main N = x:("a" / "b" "c") => N{$x}`, "bc", "cannot convert node[string] | node[(node[string], node[string])] to N"},
		{`type N = node[string]
rule main N = "a" / "b" "c"`, "bc", "rule main produces node[string] | node[(node[string], node[string])], which cannot be converted to N"},
		// A conversion through an alias names the type itself, as Type() does.
		{`type N = node[string]
type A ~ N
rule main A = x:("a" / "b" "c") => A{$x}`, "bc", "cannot convert node[string] | node[(node[string], node[string])] to N"},
	} {
		if got := compileMessage(t, tc.src); !strings.Contains(got, tc.want) {
			t.Errorf("checked compilation: got %q, want an error containing %q", got, tc.want)
		}
	}
	// The generated parsers convert through an alias with the same name.
	g, err := parseTest(`type N = node[string]
type A ~ N
rule main A = x:"a" => A{$x}`)
	if err != nil {
		t.Fatal(err)
	}
	goCode, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "p", Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	tsCode, err := generateTSBundle(g, gen.GenOptions{Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(goCode), `c.Convert("N", `) || !strings.Contains(string(tsCode), `c.convert("N", `) {
		t.Errorf("generated conversions do not name N")
	}
}

// TestGeneratedNamedNodeTypes checks the Go types that pego gen -types gives named node types: a
// defined type embedding TNode of the underlying value, which implements the unions it is a
// member of.
func TestGeneratedNamedNodeTypes(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedNamedNodeTypes)
}

func testGeneratedNamedNodeTypes(t *testing.T) {
	runGeneratedNamedNodeProgram(t, gentest.NodeTypeGrammar, `var (
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
	t.Parallel()
	gentest.EachMode(t, testGeneratedNamedNodesAsPlainNodes)
}

func testGeneratedNamedNodesAsPlainNodes(t *testing.T) {
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
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "p", Start: "main", Types: true})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, content := range map[string]string{
		"go.mod":      gentest.GoMod("gentest"),
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
	t.Parallel()
	prog := compileBare(t, gentest.NodeTypeGrammar)
	base := prog.Module()
	if err := compile.ValidateModule(base); err != nil {
		t.Fatal(err)
	}
	str := func(m *bytecode.Module, s string) int {
		for i, x := range m.Strings {
			if x == s {
				return i
			}
		}
		t.Fatalf("no string %q", s)
		return -1
	}
	clone := func() *bytecode.Module {
		m := *base
		m.Types = append([]bytecode.TypeInfo(nil), base.Types...)
		m.Rules = append([]bytecode.RuleInfo(nil), base.Rules...)
		m.Exprs = append([]bytecode.Instr(nil), base.Exprs...)
		return &m
	}
	for name, tc := range map[string]struct {
		edit func(m *bytecode.Module)
		want string
	}{
		"fields": {func(m *bytecode.Module) {
			for i := range m.Types {
				if m.Types[i].Node >= 0 {
					m.Types[i].Fields = []int{str(m, "Text")}
				}
			}
		}, "has fields"},
		"underlying": {func(m *bytecode.Module) {
			for i := range m.Types {
				if m.Types[i].Node >= 0 {
					m.Types[i].Node = str(m, "Out")
				}
			}
		}, "is not over a node type"},
		"rule": {func(m *bytecode.Module) {
			for i := range m.Rules {
				if m.Rules[i].NodeType >= 0 {
					m.Rules[i].NodeType = str(m, "main")
				}
			}
		}, "is not a named node type"},
		"enode": {func(m *bytecode.Module) {
			for i := range m.Exprs {
				if m.Exprs[i].Op == bytecode.ENode {
					m.Exprs[i].A = int32(str(m, "Out"))
				}
			}
		}, "is not a declared node type"},
	} {
		m := clone()
		tc.edit(m)
		if err := compile.ValidateModule(m); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want %s", name, err, tc.want)
		}
	}
}

func TestGeneratedTSTypedNamedNodeTypes(t *testing.T) {
	t.Parallel()
	consumer := `import { parseAST } from "./parser.ts";
import type { Num, Ident, Pair, Nums, Any, Expr } from "./types.ts";
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
	runTypedTSFixture(t, gentest.NodeTypeGrammar, consumer, gen.GenOptions{}, "")
}

// TestGeneratedTSTypedNamedNodesAsPlainNodes is TestGeneratedNamedNodesAsPlainNodes for typed
// TypeScript.
func TestGeneratedTSTypedNamedNodesAsPlainNodes(t *testing.T) {
	t.Parallel()
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
	runTypedTSFixture(t, src, consumer, gen.GenOptions{}, "")
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
		g, err := parseTest(src)
		if err != nil {
			t.Fatal(err)
		}
		prog, err := Compile(g, compile.Options{})
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
	g, err := parseTest(b.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(g, compile.Options{}); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("%d mutually recursive node types compiled in %v", n, d)
	}
}

// TestGeneratedNamedNodeDynamicTypes checks the dynamic types of values of named node types in
// open fields, unions and lists of typed Go values, and that a node converts to one value.
func TestGeneratedNamedNodeDynamicTypes(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedNamedNodeDynamicTypes)
}

func testGeneratedNamedNodeDynamicTypes(t *testing.T) {
	const src = `type N = node[string]
type M = node[string]
type T = node[(N, M)]
type U ~ N | M
type Out = { L []U; A _; B _; C node[_]; D _; Same bool }
rule main Out = xs:u* "," t:pair $$ => Out{L: $xs.Value, A: $xs.Value[0], B: $t, C: $t, D: $xs, Same: $xs.Value[0] == $xs.Value[0]}
rule u U = n / m
rule pair T = n m
rule n N = "n"
rule m M = "m"
`
	runGeneratedNamedNodeProgram(t, src, `func main() {
	out, err := p.ParseAST("mnn,nm")
	if err != nil {
		panic(err)
	}
	for _, x := range out.L {
		fmt.Printf("%T ", x)
	}
	fmt.Printf("\n%T %T %T %T %v\n", out.A, out.B, out.C, out.D, out.A == any(out.L[0]))
	b := out.B.(*p.T)
	fmt.Printf("%T %T %s%s\n", b.Value.E0, b.Value.E1, b.Value.E0.Value, b.Value.E1.Value)
	for _, x := range out.D.(*p.TNode[any]).Value.([]any) {
		fmt.Printf("%T ", x)
	}
	fmt.Println()
}
`, "*p.M *p.N *p.N \n*p.M *p.T *p.TNode[interface {}] *p.TNode[interface {}] true\n*p.N *p.M nm\n*p.M *p.N *p.N \n")
}

// namedNodeTypeCases are grammars with named node types over string, tuple, list and open nodes,
// converted by rules and constructors, in unions, cast, and read by actions.
func namedNodeTypeCases() []gentest.Case {
	return []gentest.Case{
		{Name: "named node types", Src: gentest.NodeTypeGrammar, Inputs: []string{"", "ab", "ab 12 c3", "<1,x>", "ab <12,y>[1]!bc", "12 ?", "[]!a", "ab 12<1,x>[3]!bc"}},
		{Name: "named nodes as plain nodes", Src: `type Num = node[string]
type Nums = node[[]Num]
type P = node[(Num, Num)]
type O = { C *node[string]; D []node[string]; E *Nums; F node[(node[string], node[string])]; G []Num; H *P; I node[_]; J _ }
rule main O = ns:nums p:pair $$ => O{C: $ns.Value[0], D: $ns.Value, E: $ns, F: $p, G: $ns.Value, H: $p, I: $ns, J: $p}
rule nums Nums = "[" ns:num* "]" => Nums{$ns}
rule num Num = [0-9] #atomic
rule pair P = "<" t:(num num) ">" => P{$t}`, Inputs: []string{"[12]<34>", "[1]<34>", "[]<12>", "[1", ""}},
		{Name: "named node casts", Src: `type N = node[string]
type M = node[string]
type U ~ N | M
type Out = { X M; L []U; A _; T string; Y *U }
rule main Out = xs:u* $$ => Out{X: $xs.Value[0].(M), L: $xs.Value, A: $xs.Value[0], T: text($xs), Y: $xs.Value[len($xs.Value) - 1]}
rule u U = n / m
rule n N = "n"
rule m M = "m"`, Inputs: []string{"m", "mn", "mnm", "n", "", "x"}},
		{Name: "named node rule conversions", Src: `type N = node[string]
type M = node[string]
type T = node[(N, M)]
type A = node[_]
type Out = { T T; A A; B A; C N; D *N }
rule main Out = t:pair a:open b:word c:conv d:conv? $$ => Out{T: $t, A: $a, B: $b, C: $c, D: $d}
rule pair T = n m
rule open A = "x" "y"
rule word A = "w"
rule conv N = m
rule n N = "n"
rule m M = "m"`, Inputs: []string{"nmxywm", "nmxywmm", "nmxyw", "nmxy", "mm"}},
		{Name: "named node roots", Src: `type N = node[string]
rule main N = x:[a-z]+ #atomic => N{$x}`, Inputs: []string{"abc", "", "1"}},
		{Name: "named list root", Src: `type Tree = node[[]Tree]
rule main Tree = "(" ts:main* ")" => Tree{$ts}`, Inputs: []string{"()", "(()(()))", "(()", ""}},
		{Name: "named node memo", Src: `type N = node[string]
type Out = { A N; B N; Same bool }
rule main Out = &(x:n) y:n $$ => Out{A: $x, B: $y, Same: $x == $y}
rule n N = [a-z] #atomic`, Inputs: []string{"a", "", "ab"}},
		{Name: "named node predicates", Src: `type N = node[string]
type Out = { L []N; S int }
rule main Out = xs:long* $$ => Out{L: $xs.Value, S: foldl(0, $xs, (s, x) => s + x.End - x.Start)}
rule long N = x:n {len($x.Value) > 1 && text($x) != "zz"} => $x
rule n N = -" "* t:[a-z]+ #atomic => N{$t}`, Inputs: []string{"ab cd", "", "abc d"}},
		{Name: "named node conversion chains", Src: `type N = node[string]
type M = node[string]
type P = node[(M, M)]
type R = node[(M, M)]
type Out = { A N; B N; C N; Same bool; Copy bool; E _; K M; F _; G P }
rule main Out = a:a "," b:mk "," z:conv "," e:(b "!" / b "?" / c) "," f:(rr "!" / rr "?" / pp) $$ => Out{A: $a, B: $b, C: $z, Same: N{$z} == $z, Copy: N{$b} == $b, E: $e, K: $e.(M), F: $f, G: $f.(P)}
rule a N = b
rule b N = c
rule c M = letter
rule letter = "q"
rule mk N = m:c => N{$m}
rule conv N = "z"
rule pp P = c c
rule rr R = pp`, Inputs: []string{"q,q,z,q,qq", "q,q,z,q!,qq", "q,q,z,q,qq?", "q,q,z", "z", ""}},
		{Name: "named recursive tree", Src: `type Tree = node[[]Tree]
type Out = { A _; L []Tree; C node[_]; T Tree; N int }
rule main Out = t:tree ts:tree* $$ => Out{A: $t, L: $ts.Value, C: $t, T: $t, N: len($ts.Value)}
rule tree Tree = "(" ts:tree* ")" => Tree{$ts}`, Inputs: []string{"()", "(())()", "(()(()))(())()", "(", ""}},
		{Name: "named rule bodies", Src: `type N = node[string]
type L = node[[]N]
type P = node[(N, node[string])]
type Out = { L L; P P; A _; B _ }
rule main Out = l:list "," p:pair $$ => Out{L: $l, P: $p, A: $l, B: $p}
rule list L = n*
rule pair P = n "!"
rule n N = [a-z] #atomic`, Inputs: []string{"ab,c!", ",a!", "a,b", "a,!", ""}},
		{Name: "named node backtracking", Src: `type N = node[string]
type P = node[(N, N, node[string])]
type Q = node[(N, node[string])]
type U ~ P | Q | N
type Out = { L []U; A _ }
rule main Out = xs:item* $$ => Out{L: $xs.Value, A: $xs}
rule item U = p / q / n
rule p P = n n "!"
rule q Q = n "?"
rule n N = [a-z] #atomic`, Inputs: []string{"ab!a?c", "ab?", "a", "", "!"}},
		{Name: "named node recovery", Src: `type N = node[string]
type Out = { L []_ }
rule main Out = xs:item* $$ => Out{L: $xs.Value}
rule item _ = x:(n #recover(skip=[^;]+)) ";" => $x
rule n N = [a-z]+ #atomic`, Inputs: []string{"ab;cd;", "ab;12;cd;", "1;", ""}},
		{Name: "named children of open nodes", Src: `type N = node[string]
type M = node[string]
type A = node[_]
type Out = { A A; B _; C A }
rule main Out = a:open "," c:open2 $$ => Out{A: $a, B: $a, C: $c}
rule open A = n m n
rule open2 A = ns:n* => A{$ns}
rule n N = "n"
rule m M = "m"`, Inputs: []string{"nmn,nn", "nmn,", "nm,n", ""}},
		{Name: "named node aliases", Src: `type N = node[string]
type M = node[string]
type K ~ N
type U ~ K | M
type Out = { X K; Y N; L []U; A _ }
rule main Out = k:k xs:u* $$ => Out{X: $k, Y: $xs.Value[0].(K), L: $xs.Value, A: $k}
rule k K = x:"k" => K{$x}
rule u U = n / m
rule n K = "n"
rule m M = "m"`, Inputs: []string{"knm", "kmn", "k", "kn", ""}},
		{Name: "named node Go names", Src: `type Node = node[string]
type Span = node[(Node, Node)]
type TList = node[[]Node]
type Out = { A Node; B Span; C TList; D _ }
rule main Out = a:node b:span c:tlist $$ => Out{A: $a, B: $b, C: $c, D: $b}
rule node Node = [a-z] #atomic
rule span Span = node node
rule tlist TList = "[" xs:node* "]" => TList{$xs}`, Inputs: []string{"abc[de]", "abc[]", "ab[", ""}},
		{Name: "named left recursion", Src: `type N = node[string]
type S = node[_]
type Out = { S S; T S; A _ }
rule main Out = s:sum "," t:prod $$ => Out{S: $s, T: $t, A: $s}
rule sum S = sum -"+" n / n
rule prod S = x:(prod -"*" n / n) => S{$x}
rule n N = [0-9] #atomic`, Inputs: []string{"1+2+3,4*5", "1,2", "1+,2", ""}},
		{Name: "named node in any fields", Src: `type N = node[string]
type H = { V _ }
type Out = { H H; X N; T string }
rule main Out = h:holder $$ => Out{H: $h, X: $h.V.(N), T: text($h.V.(N))}
rule holder H = x:n => H{V: $x}
rule n N = [a-z]+ #atomic`, Inputs: []string{"abc", "", "1"}},
	}
}

// TestGeneratedNamedNodeTypesTypedRuntime checks that generated typed Go builds the values of
// named node types directly in the typed runtime, giving what the conversion of the nodes of
// Parse gives, and that the generated Node parsers give what the engine does.
func TestGeneratedNamedNodeTypesTypedRuntime(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedNamedNodeTypesTypedRuntime)
}

func testGeneratedNamedNodeTypesTypedRuntime(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	// An action that reads a field of a struct makes the typed Go convert the nodes of Parse, with
	// or without named node types.
	converted := map[string]bool{"named node in any fields": true}
	cases := namedNodeTypeCases()
	for _, c := range cases {
		g, err := parseTest(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "p", Start: "main", Types: true})
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		direct := strings.Contains(string(code), "return rt.TParse(trules[") && !strings.Contains(string(code), "rt.RunParseConverted(")
		if direct == converted[c.Name] {
			t.Errorf("%s: generated typed Go uses the typed runtime: %v, want %v", c.Name, direct, !converted[c.Name])
		}
	}
	typed := len(cases) - len(converted)
	testGeneratedParsersCorpus(t, goBin, cases, gen.GenOptions{})
	testGeneratedTypesCorpus(t, goBin, cases, gen.GenOptions{ConvertTypes: true}, typed)
}

// TestGeneratedNamedNodeTypesDocument checks direct typed Documents of grammars with named node
// types against the conversion of the Document's nodes, across edits.
func TestGeneratedNamedNodeTypesDocument(t *testing.T) {
	t.Parallel()
	gentest.NeedsWorkControls(t)
	if testing.Short() {
		t.Skip("builds standalone generated parsers")
	}
	dir := t.TempDir()
	write := func(name, text string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", gentest.GoMod("directtypeddocument"))
	for i, c := range []struct {
		grammar string
		inputs  []string
	}{
		{`type A = node[string]
type B = node[string]
type P = node[(A, B)]
type L = node[[]A]
type U ~ A | B
type Out = { Items []U; Pairs []P; Any _; List *L }
rule main Out = xs:item* ps:pair* l:list? $$ => Out{Items: $xs.Value, Pairs: $ps.Value, Any: $xs, List: $l}
rule item U = a / b
rule a A = [aé] #atomic
rule b B = t:"b" => B{$t}
rule pair P = -"+" a b
rule list L = ";" xs:a* => L{$xs}`, []string{"", "abé", "ab+ab+éb", "a;aé", "b+ab;a", "+"}},
		{`type Tree = node[[]Tree]
type A = node[string]
type P = node[(A, A)]
type Q = node[(A, A)]
type Out = { T Tree; P P; Q Q; Any _; Same bool; Copy bool }
rule main Out = t:tree p:p q:q $$ => Out{T: $t, P: $p, Q: $q, Any: $q, Same: Q{$q} == $q, Copy: P{$q} == $p}
rule tree Tree = "(" ts:tree* ")" => Tree{$ts}
rule p P = l:a r:a
rule q Q = p
rule a A = [a-zé] #atomic`, []string{"()abcd", "(()(()))abcd", "((é))éaéa", "()ab", "("}},
	} {
		pkg := fmt.Sprintf("case%d", i)
		g, err := parseTest(c.grammar)
		if err != nil {
			t.Fatalf("%s: %v", pkg, err)
		}
		code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: pkg, Start: "main", Types: true, Document: true})
		if err != nil {
			t.Fatalf("%s: %v", pkg, err)
		}
		if !strings.Contains(string(code), "return rt.TypedDocumentParse(") {
			t.Fatalf("%s uses conversion instead of direct matching", pkg)
		}
		write(pkg+"/parser.go", string(code))
		reference, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "reference", Start: "main", Types: true, Document: true, ConvertTypes: true})
		if err != nil {
			t.Fatal(err)
		}
		write(pkg+"/reference/parser.go", string(reference))
		test := strings.ReplaceAll(directTypedDocumentTests, "PACKAGE", pkg)
		test = strings.ReplaceAll(test, "INPUTS", fmt.Sprintf("%#v", c.inputs))
		write(pkg+"/parser_test.go", test)
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=-mod=mod -trimpath")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("direct typed Document: %v\n%s", err, out)
	}
}

// declaredNodeAnyCases are grammars whose rules declare node[_] without an action: the declared
// type, and not the inferred value type, is the rule's type.
func declaredNodeAnyCases() []gentest.Case {
	return []gentest.Case{
		{Name: "declared node[_] tuple", Src: `rule main node[_] = a b
rule a = "a"
rule b = "b"`, Inputs: []string{"ab", "a", "", "abb"}},
		{Name: "declared node[_] in a union", Src: `type B = { X node[_] }
type U ~ node[_] | B
type O = { P U; Q U; R []node[_] }
rule main O = p:one q:(wrap / two) r:two* $$ => O{P: $p, Q: $q, R: $r.Value}
rule wrap B = x:one => B{X: $x}
rule one node[_] = "a" "b"
rule two node[_] = "c"+`, Inputs: []string{"abab", "abcab", "abccc", "ab", ""}},
		{Name: "declared node[_] list and reference", Src: `rule main node[_] = xs:item* t:tail
rule item node[_] = "a" "b"
rule tail node[_] = item item?`, Inputs: []string{"abab", "ababab", "abx", ""}},
	}
}

// TestGeneratedDeclaredNodeAny checks that a rule declared node[_] without an action has the
// declared type in generated Go and TypeScript, that the typed runtime still builds its values, and
// that they match the conversion of the nodes of Parse.
func TestGeneratedDeclaredNodeAny(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedDeclaredNodeAny)
}

func testGeneratedDeclaredNodeAny(t *testing.T) {
	src := declaredNodeAnyCases()[0].Src
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "p", Start: "main", Types: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"func ParseAST(input string, opts ...rt.ParseOption) (*TNode[any], error)",
		"//\tmain: *TNode[any]\n",
		"return rt.TParse(trules[",
	} {
		if !strings.Contains(string(code), want) {
			t.Errorf("generated Go lacks %q", want)
		}
	}
	if strings.Contains(string(code), "TNode[Tuple2") {
		t.Errorf("generated Go gives the declared node[_] rule its inferred type")
	}
	if testing.Short() {
		return
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	cases := declaredNodeAnyCases()
	for _, c := range cases {
		g, err := parseTest(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "p", Start: "main", Types: true})
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		if !strings.Contains(string(code), "return rt.TParse(trules[") || strings.Contains(string(code), "rt.RunParseConverted(") {
			t.Errorf("%s: generated typed Go converts nodes instead of using the typed runtime", c.Name)
		}
	}
	testGeneratedParsersCorpus(t, goBin, cases, gen.GenOptions{})
	testGeneratedTypesCorpus(t, goBin, cases, gen.GenOptions{ConvertTypes: true}, len(cases))
}

// TestNamedNodeTypeText checks that the S-expression of a node of a named node type names the type
// before its brackets, whatever it converts (Ident("x"), Pair(a b), Items[a]), on every backend
// and with source information, and that a node of a reserved type stays unnamed. The corpus
// comparisons of generated Go and TypeScript parsers check that they print the same strings.
func TestNamedNodeTypeText(t *testing.T) {
	t.Parallel()
	const want = `Out{Any: Any("bc"), Items: [Ident("ab") Num("12")], Len: 4, Nums: Nums[Num("3")], Pair: Pair(Num("1") Ident("x")), Start: 3, Text: "ab12"}`
	for _, sourceInfo := range []bool{false, true} {
		prog := compileBare(t, gentest.NodeTypeGrammar, compile.Options{SourceInfo: sourceInfo})
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			n, err := prog.ParseWith("main", "ab 12<1,x>[3]!bc", ParseOptions{Backend: backend})
			if err != nil {
				t.Fatal(err)
			}
			if got := n.String(); got != want {
				t.Errorf("sourceInfo=%t %s:\n got %s\nwant %s", sourceInfo, backend, got, want)
			}
		}
	}
	// The plain node types of the same shapes stay unnamed.
	prog := compileBare(t, `type Out = { S node[string]; T node[(node[string], node[string])]; L node[[]node[string]] }
rule main Out = s:"a" #atomic t:("b" "c") l:("d"*) $$ => Out{S: $s, T: $t, L: $l}`)
	n, err := prog.Parse("main", "abcdd")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := n.String(), `Out{L: ["d" "d"], S: "a", T: ("b" "c")}`; got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
}
