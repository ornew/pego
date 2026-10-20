package engine

import (
	"encoding/binary"
	"hash/crc32"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/syntax"
)

const constructorGrammar = `type Int = { Text string }
type Alias ~ Int
type Values = { Items []Int; Nested [][]Int; Maybe []*Int; Default Int; Empty []Int }
rule main Values = v1:"😀" => Values{Items: []Int{Int{Text: "0"}, Alias{Text: text($v1)}}, Nested: [][]Int{[]Int{}, []Int{Int{Text: "1"}}}, Maybe: []*Int{nil, Int{Text: "2"}}, Default: Int{Text: "0"}, Empty: []Int{}}
`

// legacyConstructorGrammar is constructorGrammar in the retired terminal type syntax, read with
// syntax.ParseLegacy as the text structs that pego fix writes for it.
const legacyConstructorGrammar = `type Int terminal
type Alias ~ Int
type Values = { Items []Int; Nested [][]Int; Maybe []*Int; Default Int; Empty []Int }
rule main Values = v1:"😀" => Values{Items: []Int{Int{"0"}, Alias{text($v1)}}, Nested: [][]Int{[]Int{}, []Int{Int{"1"}}}, Maybe: []*Int{nil, Int{"2"}}, Default: Int{"0"}, Empty: []Int{}}
`

func TestTypedNodeConstructors(t *testing.T) {
	const source = legacyConstructorGrammar
	g, err := syntax.ParseLegacy(source)
	if err != nil {
		t.Fatal(err)
	}
	data, err := grammar.MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := grammar.DecodeJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []*grammar.Grammar{g, decoded} {
		p, err := Compile(input, Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			var want string
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				n, err := p.ParseWith("main", "😀", ParseOptions{Backend: backend, Unit: unit})
				if err != nil {
					t.Fatal(err)
				}
				got := resultJSON(n, nil)
				if want == "" {
					want = got
				} else if got != want {
					t.Fatalf("%s/%s differs: %s / %s", backend, unit, got, want)
				}
				items := n.Field("Items").(*Node)
				if len(items.children) != 2 || items.children[0].Kind() != KindStruct || items.children[0].Field("Text") != "0" || items.children[1].Field("Text") != "😀" || items.children[1].Type() != "Int" {
					t.Fatal(got)
				}
				end := int32(1)
				if unit == Bytes {
					end = 4
				}
				if items.start != 0 || items.end != end || items.children[0].start != 0 || items.children[0].end != end {
					t.Fatal(got)
				}
				maybe := n.Field("Maybe").(*Node)
				if len(maybe.children) != 2 || maybe.children[0] != nil {
					t.Fatal(got)
				}
				empty := n.Field("Empty").(*Node)
				if empty == nil || len(empty.children) != 0 || empty.start != 0 || empty.end != 0 {
					t.Fatal(got)
				}
			}
		}
		unchanged, err := grammar.MarshalJSON(input)
		if err != nil || string(unchanged) != string(data) {
			t.Fatalf("constructor compilation changed AST: %v", err)
		}
	}
}

func constructorCases() []genCase {
	return []genCase{
		{"typed node constructors", constructorGrammar, []string{"😀", "", "x"}},
		{"named node types", nodeTypeGrammar, []string{"", "ab", "ab 12 c3", "<1,x>", "ab<12,y>[1]!bc", "12 ?", "[]!a", "ab 12<1,x>[3]"}},
		{"named node type run-time conversions", `type S = node[string]
type L = node[[]S]
type Out = { A S; B L }
rule main Out = x:anything xs:more* $$ => Out{A: S{$x.(node[string])}, B: L{$xs}}
rule anything node[_] = "a" / "b" "c"
rule more S = -"," x:[a-z] => $x`, []string{"a", "a,x,y", "bc", "bc,x", "", "a,"}},
		{"terminal constructor root", `type Int = { Text string }
type Alias ~ Int
rule main Alias = "é" => Alias{Text: "0"}`, []string{"é", "", "e"}},
		{"typed list root", `type T = { Text string }
rule main []T = "x" => []T{T{Text: "0"}}`, []string{"x", "", "xx"}},
		{"typed nullable union list", `type T = { Text string }
type R = { Values []*(T | node[string]) }
rule main R = x:"é" => R{Values: []*(T | node[string]){nil, T{Text: "0"}, $x}}`, []string{"é", "", "x"}},
		{"terminal constructor value error", `type Int = { Text string }
var value int
rule main = {value := 1} "x" => Int{Text: list(value, "a")[0].(string)}`, []string{"x", ""}},
		{"terminal constructor predicate error", `type Int = { Text string }
var value int
rule main = {value := 1} {Int{Text: list(value, "a")[0].(string)} != nil} "x" / "x"`, []string{"x", ""}},
	}
}

func TestGeneratedNodeConstructors(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	cases := constructorCases()
	testGeneratedParsersCorpus(t, goBin, cases, GenOptions{})
	testGeneratedTypesCorpus(t, goBin, cases, GenOptions{convertTypes: true}, 4)
	if node, err := exec.LookPath("node"); err == nil {
		testGeneratedTSParsersCorpus(t, node, cases, GenOptions{})
	} else {
		t.Error("Node is required for constructor parity")
	}
}

func TestGeneratedTypedTSNodeConstructors(t *testing.T) {
	runTypedTSFixture(t, constructorGrammar, `import {parseAST, parse, CodePoints, Bytes} from "./parser.ts";
`+tsTypedAssert+`
for (const unit of [CodePoints, Bytes]) {
  const r = parseAST("😀", unit);
  check(r.error === null && r.value !== null, "parse");
  const v = r.value;
  check(stable(v).length > 0, "serializable output");
  check(v.Items?.length === 2 && v.Items[0]?.Text === "0" && v.Items[1]?.type === "Int" && v.Items[1]?.Text === "😀", "text struct aliases");
  check(v.Nested?.length === 2 && v.Nested[0]?.length === 0 && v.Nested[1]?.[0]?.Text === "1", "nested lists");
  check(v.Maybe?.length === 2 && v.Maybe[0] === null && v.Maybe[1]?.Text === "2" && v.Empty?.length === 0, "nil and empty lists");
  check(v.start === 0 && v.end === (unit === Bytes ? 4 : 1), "span");
  check(parse("😀", unit).error === null, "generic output");
}
`, GenOptions{}, "")
}

func TestTypedNodeConstructorErrors(t *testing.T) {
	for _, tc := range []struct{ action, message string }{
		{`[]Int{nil}`, "cannot use"},
		{`[]Int{Other{}}`, "cannot use"},
		{`[]any{}`, "concrete element type"},
		{`[][]any{}`, "concrete element type"},
		{`[]Int{x}`, "cannot use int as Int in list constructor"},
		{`[][]Int{list(x)}`, "cannot use []int as []Int in list constructor"},
		{`[]Int{$d}`, "statically unknown"},
		{`[][]Int{list($d)}`, "statically unknown"},
		// The retired positional forms are read as the text struct constructors they mean.
		{`Int{1}`, "cannot use int as string in field Text of Int"},
		{`Int{nil}`, "cannot use nil as string in field Text of Int"},
		{`Int{"a", "b"}`, "duplicate field Text"},
		{`Other{"x"}`, "Other has no field Text"},
	} {
		t.Run(tc.action, func(t *testing.T) {
			g, err := syntax.ParseLegacy(`type Int terminal
type Other = {}
var x int
rule main = {x := 1} d:dyn "x" => ` + tc.action + `
rule dyn any = _ => 1`)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Compile(g, Options{})
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("got %v; want %q", err, tc.message)
			}
		})
	}
	// Lists may hold ints, strings and bools.
	for _, action := range []string{`[]int{}`, `[][]string{}`, `[]int{1, 2}`, `[]*bool{nil, true}`} {
		g, err := syntax.ParseLegacy("var x int\nrule main = {x := 1} \"x\" => " + action)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Compile(g, Options{}); err != nil {
			t.Errorf("%s: %v", action, err)
		}
	}
	// A terminal constructor needed exactly one positional argument; a text struct is an ordinary
	// struct, whose constructor may leave Text unset or name it.
	for _, action := range []string{`Int{}`, `Int{Text: "0"}`} {
		g, err := syntax.ParseLegacy("type Int terminal\nrule main = \"x\" => " + action)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Compile(g, Options{}); err != nil {
			t.Errorf("%s: %v", action, err)
		}
	}
}

func TestConstructedTerminalEvaluationErrors(t *testing.T) {
	for _, source := range []string{
		`type Int terminal
var value int
rule main = {value := 1} "x" => Int{value}`,
		`type Int terminal
var value int
rule main = {value := 1} {Int{value} != nil} "x" / "x"`,
	} {
		g, err := syntax.ParseLegacy(source)
		if err != nil {
			t.Fatal(err)
		}
		// The static check rejects these constructors; the run-time check is what remains
		// without it.
		p, err := Compile(g, Options{NoTypeCheck: true})
		if err != nil {
			t.Fatal(err)
		}
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			n, err := p.ParseWith("main", "x", ParseOptions{Backend: backend})
			if strings.Contains(source, "=>") {
				if n != nil || err == nil || !strings.Contains(err.Error(), "cannot use int as string in field Text of Int") {
					t.Fatalf("%s: %v / %v", backend, n, err)
				}
			} else if n == nil || err != nil {
				t.Fatalf("predicate must backtrack: %s: %v", backend, err)
			}
		}
	}
}

// The text struct counterpart: a field of the wrong type is an error in the action and a failed
// predicate backtracks.
func TestConstructedTextStructEvaluationErrors(t *testing.T) {
	for _, source := range []string{
		`type Int = { Text string }
var value int
rule main = {value := 1} "x" => Int{Text: value}`,
		`type Int = { Text string }
var value int
rule main = {value := 1} {Int{Text: value} != nil} "x" / "x"`,
	} {
		// The static check rejects these constructors; the run-time check is what remains
		// without it.
		p := compile(t, source, Options{NoTypeCheck: true})
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			n, err := p.ParseWith("main", "x", ParseOptions{Backend: backend})
			if strings.Contains(source, "=>") {
				if n != nil || err == nil || !strings.Contains(err.Error(), "cannot use int as string in field Text of Int") {
					t.Fatalf("%s: %v / %v", backend, n, err)
				}
			} else if n == nil || err != nil {
				t.Fatalf("predicate must backtrack: %s: %v", backend, err)
			}
		}
	}
}

func TestNodeConstructorCompiledVersions(t *testing.T) {
	for _, omit := range []bool{false, true} {
		p := compileLegacy(t, legacyConstructorGrammar)
		data, err := p.MarshalBinaryWith("main", MarshalOptions{OmitAST: omit})
		if err != nil || data[len(compiledMagic)+1] != isaVersion {
			t.Fatalf("constructor ISA: %v", err)
		}
		loaded, _, err := LoadProgram(data, Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, backend := range []Backend{Bytecode, BytecodeIterative} {
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				opts := ParseOptions{Backend: backend, Unit: unit}
				if got, want := resultJSON(loaded.ParseWith("main", "😀", opts)), resultJSON(p.ParseWith("main", "😀", opts)); got != want {
					t.Fatalf("loaded constructors: %s / %s", got, want)
				}
			}
		}
		data[len(compiledMagic)+1] = isaVersion - 1
		binary.LittleEndian.PutUint32(data[len(data)-4:], crc32.ChecksumIEEE(data[:len(data)-4]))
		if _, _, err := LoadProgram(data, Options{}); err == nil || !strings.Contains(err.Error(), "compile the grammar again") {
			t.Fatalf("wrong ISA accepted: %v", err)
		}
		module := p.Module()
		for i := range module.Exprs {
			if module.Exprs[i].Op == ENew {
				module.Exprs[i].B = -1
				break
			}
		}
		if err := validateModule(module); err == nil {
			t.Fatal("invalid constructor field list accepted")
		}
	}
	for _, source := range []string{`rule main = "x"`, `rule main = v1:"x" => []node[string]{$v1}`} {
		p := compile(t, source)
		data, err := p.MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
		if err != nil || data[len(compiledMagic)+1] != isaVersion {
			t.Fatalf("unchanged/list-only ISA: %v", err)
		}
	}
}

func TestNodeConstructorsWithoutTypeChecking(t *testing.T) {
	const header = "type Int terminal\ntype Other = {}\nrule main = \"x\" => "
	for _, action := range []string{`[]any{}`, `Int{"a", "b"}`, `Other{"a"}`} {
		g, err := syntax.ParseLegacy(header + action)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Compile(g, Options{NoTypeCheck: true}); err == nil {
			t.Fatalf("NoTypeCheck accepted unsupported constructor shape: %s", action)
		}
	}
	for _, action := range []string{`[]int{}`, `[][]string{}`} {
		if g, err := syntax.ParseLegacy(header + action); err != nil {
			t.Fatal(err)
		} else if _, err := Compile(g, Options{NoTypeCheck: true}); err != nil {
			t.Errorf("NoTypeCheck rejected a list of values %s: %v", action, err)
		}
	}
	if g, err := syntax.ParseLegacy(header + `Int{}`); err != nil {
		t.Fatal(err)
	} else if _, err := Compile(g, Options{NoTypeCheck: true}); err != nil {
		t.Fatalf("Int{} is a text struct with no field set: %v", err)
	}
	for _, action := range []string{`[]Int{nil}`, `[]Int{Other{}}`, `[]Int{1}`, `Int{1}`} {
		g, err := syntax.ParseLegacy(header + action)
		if err != nil {
			t.Fatal(err)
		}
		p, err := Compile(g, Options{NoTypeCheck: true})
		if err != nil {
			t.Fatalf("assignability was not bypassed for %s: %v", action, err)
		}
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			n, err := p.ParseWith("main", "x", ParseOptions{Backend: backend})
			// Without type checking a list may hold the int 1 boxed; Int{1} still has no text.
			invalidValue := action == `Int{1}`
			if invalidValue != (err != nil) || invalidValue == (n != nil) {
				t.Fatalf("%s/%s: %v / %v", action, backend, n, err)
			}
			if !invalidValue && (len(n.children) != 1 || (n.children[0] == nil) != (action == `[]Int{nil}`) || (action == `[]Int{1}`) != (n.children[0] != nil && n.children[0].box == 1)) {
				t.Fatal(resultJSON(n, err))
			}
		}
	}
	for _, action := range []string{`[]Int{1}`, `Int{1}`} {
		g, err := syntax.ParseLegacy("type Int terminal\nrule main = {" + action + " != nil} \"x\" / \"x\"")
		if err != nil {
			t.Fatal(err)
		}
		p, err := Compile(g, Options{NoTypeCheck: true})
		if err != nil {
			t.Fatal(err)
		}
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			if n, err := p.ParseWith("main", "x", ParseOptions{Backend: backend}); n == nil || err != nil {
				t.Fatalf("%s/%s predicate must backtrack: %v", action, backend, err)
			}
		}
	}
}
