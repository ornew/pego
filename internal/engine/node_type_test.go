package engine

import (
	"fmt"
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
			want := gentest.ResultJSON(prog.ParseWith("main", input, ParseOptions{Backend: Closure}))
			for _, backend := range []Backend{Bytecode, BytecodeIterative} {
				got := gentest.ResultJSON(prog.ParseWith("main", input, ParseOptions{Backend: backend}))
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
	got := gentest.ResultJSON(n, err)
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
					if got := gentest.ResultJSON(prog.ParseWith("main", input, ParseOptions{Backend: backend})); got != want {
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
		got := gentest.ResultJSON(prog.ParseWith("main", "(()(()))", ParseOptions{Backend: backend}))
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
	tsCode, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(goCode), `c.Convert("N", `) || !strings.Contains(string(tsCode), `c.convert("N", `) {
		t.Errorf("generated conversions do not name N")
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
