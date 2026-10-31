package gogen_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
	"github.com/ornew/zeroparse/internal/ir/exec/core"
)

// TestGeneratedNamedNodeTypes checks the Go types that zp gen -types gives named node types: a
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
	cmd.Env = append(os.Environ(), "GOFLAGS="+core.GoFlags("-mod=mod -trimpath"), "GOTOOLCHAIN=local")
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
	gentest.GeneratedParsersCorpus(t, goBin, cases, gen.GenOptions{})
	gentest.GeneratedTypesCorpus(t, goBin, cases, gen.GenOptions{ConvertTypes: true}, typed)
}

// TestGeneratedNamedNodeTypesDocument checks direct typed Documents of grammars with named node
// types against the conversion of the Document's nodes, across edits.
func TestGeneratedNamedNodeTypesDocument(t *testing.T) {
	t.Parallel()
	gentest.NeedsBudgetControls(t)
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
		test := strings.ReplaceAll(gentest.DirectTypedDocumentTests, "PACKAGE", pkg)
		test = strings.ReplaceAll(test, "INPUTS", fmt.Sprintf("%#v", c.inputs))
		write(pkg+"/parser_test.go", test)
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS="+core.GoFlags("-mod=mod -trimpath"))
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
	gentest.GeneratedParsersCorpus(t, goBin, cases, gen.GenOptions{})
	gentest.GeneratedTypesCorpus(t, goBin, cases, gen.GenOptions{ConvertTypes: true}, len(cases))
}
