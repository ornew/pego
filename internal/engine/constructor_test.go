package engine

import (
	"encoding/binary"
	"github.com/ornew/pego/internal/gentest"
	"hash/crc32"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/bytecode"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/value"
)

// legacyConstructorGrammar is constructorGrammar in the retired terminal type syntax, read with
// syntax.ParseLegacy as the text structs that pego fix writes for it.
const legacyConstructorGrammar = `type Int terminal
type Alias ~ Int
type Values = { Items []Int; Nested [][]Int; Maybe []*Int; Default Int; Empty []Int }
rule main Values = v1:"😀" => Values{Items: []Int{Int{"0"}, Alias{text($v1)}}, Nested: [][]Int{[]Int{}, []Int{Int{"1"}}}, Maybe: []*Int{nil, Int{"2"}}, Default: Int{"0"}, Empty: []Int{}}
`

func TestTypedNodeConstructors(t *testing.T) {
	t.Parallel()
	const source = legacyConstructorGrammar
	g, err := parseLegacyTest(source)
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
		p, err := Compile(input, compile.Options{})
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
				items := n.Field("Items").(*value.Node)
				if len(value.D(items).Children) != 2 || value.D(items).Children[0].Kind() != value.KindStruct || value.D(items).Children[0].Field("Text") != "0" || value.D(items).Children[1].Field("Text") != "😀" || value.D(items).Children[1].Type() != "Int" {
					t.Fatal(got)
				}
				end := int32(1)
				if unit == Bytes {
					end = 4
				}
				if value.D(items).Start != 0 || value.D(items).End != end || value.D(value.D(items).Children[0]).Start != 0 || value.D(value.D(items).Children[0]).End != end {
					t.Fatal(got)
				}
				maybe := n.Field("Maybe").(*value.Node)
				if len(value.D(maybe).Children) != 2 || value.D(maybe).Children[0] != nil {
					t.Fatal(got)
				}
				empty := n.Field("Empty").(*value.Node)
				if empty == nil || len(value.D(empty).Children) != 0 || value.D(empty).Start != 0 || value.D(empty).End != 0 {
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

func TestGeneratedNodeConstructors(t *testing.T) {
	t.Parallel()
	eachMode(t, testGeneratedNodeConstructors)
}

func testGeneratedNodeConstructors(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	cases := gentest.ConstructorCases()
	testGeneratedParsersCorpus(t, goBin, cases, gen.GenOptions{})
	testGeneratedTypesCorpus(t, goBin, cases, gen.GenOptions{ConvertTypes: true}, 4)
	if node, err := exec.LookPath("node"); err == nil {
		testGeneratedTSParsersCorpus(t, node, cases, gen.GenOptions{})
	} else {
		t.Error("Node is required for constructor parity")
	}
}

func TestGeneratedTypedTSNodeConstructors(t *testing.T) {
	t.Parallel()
	runTypedTSFixture(t, gentest.ConstructorGrammar, `import {parseAST, parse, CodePoints, Bytes} from "./parser.ts";
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
`, gen.GenOptions{}, "")
}

func TestTypedNodeConstructorErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ action, message string }{
		{`[]Int{nil}`, "cannot use"},
		{`[]Int{Other{}}`, "cannot use"},
		{`[]Int{x}`, "cannot use int as Int in list constructor"},
		{`[][]Int{list(x)}`, "cannot use []int as []Int in list constructor"},
		{`[]Int{$d}`, "cannot use _ as Int in list constructor: the type of the value is not known; convert it with a cast, as in value.(Int)"},
		{`[][]Int{list($d)}`, "in list constructor: the type of the value is not known; convert it with a cast, as in value.([]Int)"},
		// The retired positional forms are read as the text struct constructors they mean.
		{`Int{1}`, "cannot use int as string in field Text of Int"},
		{`Int{nil}`, "cannot use nil as string in field Text of Int"},
		{`Int{"a", "b"}`, "duplicate field Text"},
		{`Other{"x"}`, "Other has no field Text"},
	} {
		t.Run(tc.action, func(t *testing.T) {
			g, err := parseLegacyTest(`type Int terminal
type Other = {}
var x int
rule main = {x := 1} d:dyn "x" => ` + tc.action + `
rule dyn _ = _ => 1`)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Compile(g, compile.Options{})
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("got %v; want %q", err, tc.message)
			}
		})
	}
	// Lists may hold ints, strings and bools.
	for _, action := range []string{`[]_{}`, `[][]_{}`, `[]int{}`, `[][]string{}`, `[]int{1, 2}`, `[]*bool{nil, true}`} {
		g, err := parseLegacyTest("var x int\nrule main = {x := 1} \"x\" => " + action)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Compile(g, compile.Options{}); err != nil {
			t.Errorf("%s: %v", action, err)
		}
	}
	// A terminal constructor needed exactly one positional argument; a text struct is an ordinary
	// struct, whose constructor may leave Text unset or name it.
	for _, action := range []string{`Int{}`, `Int{Text: "0"}`} {
		g, err := parseLegacyTest("type Int terminal\nrule main = \"x\" => " + action)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Compile(g, compile.Options{}); err != nil {
			t.Errorf("%s: %v", action, err)
		}
	}
}

func TestConstructedTerminalEvaluationErrors(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`type Int terminal
var value int
rule main = {value := 1} "x" => Int{value}`,
		`type Int terminal
var value int
rule main = {value := 1} {Int{value} != nil} "x" / "x"`,
	} {
		g, err := parseLegacyTest(source)
		if err != nil {
			t.Fatal(err)
		}
		// The checker rejects these constructors when the grammar is compiled.
		if _, err := Compile(g, compile.Options{}); err == nil || !strings.Contains(err.Error(), "cannot use int as string in field Text of Int") {
			t.Fatalf("compilation: %v", err)
		}
	}
}

// The text struct counterpart: a field of the wrong type is a compile error.
func TestConstructedTextStructEvaluationErrors(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`type Int = { Text string }
var value int
rule main = {value := 1} "x" => Int{Text: value}`,
		`type Int = { Text string }
var value int
rule main = {value := 1} {Int{Text: value} != nil} "x" / "x"`,
	} {
		// The checker rejects these constructors when the grammar is compiled.
		if got := compileMessage(t, source); !strings.Contains(got, "cannot use int as string in field Text of Int") {
			t.Fatalf("compilation: %q", got)
		}
	}
}

func TestNodeConstructorCompiledVersions(t *testing.T) {
	t.Parallel()
	for _, mode := range valueModes {
		p := compileLegacy(t, legacyConstructorGrammar)
		data, err := p.Marshal("main", mode)
		if err != nil || data[isaOffset] != bytecode.ISAVersion {
			t.Fatalf("constructor ISA: %v", err)
		}
		loaded, _, err := LoadProgram(data)
		if err != nil {
			t.Fatal(err)
		}
		for _, backend := range []Backend{Bytecode, BytecodeIterative} {
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				opts := ParseOptions{Backend: backend, Unit: unit}
				if got, want := resultJSON(loaded.ParseWith("main", "😀", servedBy(mode, opts))), resultJSON(p.ParseWith("main", "😀", opts)); got != want {
					t.Fatalf("loaded constructors: %s / %s", got, want)
				}
			}
		}
		data[isaOffset] = bytecode.ISAVersion - 1
		binary.LittleEndian.PutUint32(data[len(data)-4:], crc32.ChecksumIEEE(data[:len(data)-4]))
		if _, _, err := LoadProgram(data); err == nil || !strings.Contains(err.Error(), "compile the grammar again") {
			t.Fatalf("wrong ISA accepted: %v", err)
		}
		module := p.Module()
		for i := range module.Exprs {
			if module.Exprs[i].Op == bytecode.ENew {
				module.Exprs[i].B = -1
				break
			}
		}
		if err := compile.ValidateModule(module); err == nil {
			t.Fatal("invalid constructor field list accepted")
		}
	}
	for _, source := range []string{`rule main = "x"`, `rule main = v1:"x" => []node[string]{$v1}`} {
		p := mustCompile(t, source)
		data, err := p.MarshalBinary("main")
		if err != nil || data[isaOffset] != bytecode.ISAVersion {
			t.Fatalf("unchanged/list-only ISA: %v", err)
		}
	}
}

func TestNodeConstructorShapes(t *testing.T) {
	t.Parallel()
	const header = "type Int terminal\ntype Other = {}\nrule main = \"x\" => "
	for _, action := range []string{`[]any{}`, `Int{"a", "b"}`, `Other{"a"}`} {
		g, err := parseLegacyTest(header + action)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Compile(g, compile.Options{}); err == nil {
			t.Fatalf("accepted unsupported constructor shape: %s", action)
		}
	}
	for _, action := range []string{`[]int{}`, `[][]string{}`} {
		if g, err := parseLegacyTest(header + action); err != nil {
			t.Fatal(err)
		} else if _, err := Compile(g, compile.Options{}); err != nil {
			t.Errorf("rejected a list of values %s: %v", action, err)
		}
	}
	if g, err := parseLegacyTest(header + `Int{}`); err != nil {
		t.Fatal(err)
	} else if _, err := Compile(g, compile.Options{}); err != nil {
		t.Fatalf("Int{} is a text struct with no field set: %v", err)
	}
	// Values of the wrong type are rejected when the grammar is compiled, in actions and predicates.
	for _, action := range []string{`[]Int{nil}`, `[]Int{Other{}}`, `[]Int{1}`, `Int{1}`} {
		g, err := parseLegacyTest(header + action)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Compile(g, compile.Options{}); err == nil {
			t.Errorf("%s was accepted", action)
		}
	}
	for _, action := range []string{`[]Int{1}`, `Int{1}`} {
		g, err := parseLegacyTest("type Int terminal\nrule main = {" + action + " != nil} \"x\" / \"x\"")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Compile(g, compile.Options{}); err == nil {
			t.Errorf("predicate %s was accepted", action)
		}
	}
}
