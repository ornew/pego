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

const constructorGrammar = `type Int terminal
type Alias = Int
type Values struct { Items []Int; Nested [][]Int; Maybe []*Int; Default Int; Empty []Int }
rule main Values = "😀" => Values{Items: []Int{Int{"0"}, Alias{text($1)}}, Nested: [][]Int{[]Int{}, []Int{Int{"1"}}}, Maybe: []*Int{nil, Int{"2"}}, Default: Int{"0"}, Empty: []Int{}}
`

func TestTypedNodeConstructors(t *testing.T) {
	const source = constructorGrammar
	g, err := syntax.Parse(source)
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
		for _, unit := range []Unit{CodePoints, Bytes} {
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
				if len(items.Children) != 2 || !items.Children[0].IsTerminal() || items.Children[0].Text != "0" || items.Children[1].Text != "😀" || items.Children[1].Type() != "Int" {
					t.Fatal(got)
				}
				end := int32(1)
				if unit == Bytes {
					end = 4
				}
				if items.Start != 0 || items.End != end || items.Children[0].Start != 0 || items.Children[0].End != end {
					t.Fatal(got)
				}
				maybe := n.Field("Maybe").(*Node)
				if len(maybe.Children) != 2 || maybe.Children[0] != nil {
					t.Fatal(got)
				}
				empty := n.Field("Empty").(*Node)
				if empty == nil || len(empty.Children) != 0 || empty.Start != 0 || empty.End != 0 {
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
		{"terminal constructor root", `type Int terminal
type Alias = Int
rule main Alias = "é" => Alias{"0"}`, []string{"é", "", "e"}},
		{"typed list root", `type T terminal
rule main []T = "x" => []T{T{"0"}}`, []string{"x", "", "xx"}},
		{"typed nullable union list", `type T terminal
type R struct { Values []*(T | Match) }
rule main R = x:"é" => R{Values: []*(T | Match){nil, T{"0"}, $x}}`, []string{"é", "", "x"}},
		{"terminal constructor value error", `type Int terminal
rule main = {value := 1} "x" => Int{value}`, []string{"x", ""}},
		{"terminal constructor predicate error", `type Int terminal
rule main = {value := 1} {Int{value} != nil} "x" / "x"`, []string{"x", ""}},
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
  check(v.Items?.length === 2 && v.Items[0]?.text === "0" && v.Items[1]?.type === "Int" && v.Items[1]?.text === "😀", "terminal aliases");
  check(v.Nested?.length === 2 && v.Nested[0]?.length === 0 && v.Nested[1]?.[0]?.text === "1", "nested lists");
  check(v.Maybe?.length === 2 && v.Maybe[0] === null && v.Maybe[1]?.text === "2" && v.Empty?.length === 0, "nil and empty lists");
  check(v.start === 0 && v.end === (unit === Bytes ? 4 : 1), "span");
  check(parse("😀", unit).error === null, "generic output");
}
`, GenOptions{}, "")
}

func TestTypedNodeConstructorErrors(t *testing.T) {
	for _, tc := range []struct{ action, message string }{
		{`[]Int{nil}`, "cannot use"},
		{`[]Int{Other{}}`, "cannot use"},
		{`[]int{}`, "node element type"},
		{`[][]string{}`, "node element type"},
		{`[]any{}`, "node element type"},
		{`[]Int{x}`, "statically unknown"},
		{`[][]Int{list(x)}`, "statically unknown"},
		{`Int{1}`, "requires a string"},
		{`Int{nil}`, "requires a string"},
		{`Int{}`, "one positional"},
		{`Int{"a", "b"}`, "one argument"},
		{`Int{Text: "0"}`, "one positional"},
		{`Other{"x"}`, "declared terminal type"},
	} {
		t.Run(tc.action, func(t *testing.T) {
			g, err := syntax.Parse(`type Int terminal
type Other struct {}
rule main = {x := 1} "x" => ` + tc.action)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Compile(g, Options{})
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("got %v; want %q", err, tc.message)
			}
		})
	}
}

func TestConstructedTerminalEvaluationErrors(t *testing.T) {
	for _, source := range []string{
		`type Int terminal
rule main = {value := 1} "x" => Int{value}`,
		`type Int terminal
rule main = {value := 1} {Int{value} != nil} "x" / "x"`,
	} {
		g, err := syntax.Parse(source)
		if err != nil {
			t.Fatal(err)
		}
		p, err := Compile(g, Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			n, err := p.ParseWith("main", "x", ParseOptions{Backend: backend})
			if strings.Contains(source, "=>") {
				if n != nil || err == nil || !strings.Contains(err.Error(), "requires a string") {
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
		p := compile(t, constructorGrammar)
		data, err := p.MarshalBinaryWith("main", MarshalOptions{OmitAST: omit})
		if err != nil || data[len(compiledMagic)+1] != 5 {
			t.Fatalf("terminal ISA: %v", err)
		}
		loaded, _, err := LoadProgram(data, Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, backend := range []Backend{Bytecode, BytecodeIterative} {
			for _, unit := range []Unit{CodePoints, Bytes} {
				opts := ParseOptions{Backend: backend, Unit: unit}
				if got, want := resultJSON(loaded.ParseWith("main", "😀", opts)), resultJSON(p.ParseWith("main", "😀", opts)); got != want {
					t.Fatalf("loaded constructors: %s / %s", got, want)
				}
			}
		}
		data[len(compiledMagic)+1] = 4
		binary.LittleEndian.PutUint32(data[len(data)-4:], crc32.ChecksumIEEE(data[:len(data)-4]))
		if _, _, err := LoadProgram(data, Options{}); err == nil || !strings.Contains(err.Error(), "instruction set version 5") {
			t.Fatalf("wrong ISA accepted: %v", err)
		}
		module := p.Module()
		for i := range module.Exprs {
			if module.Exprs[i].Op == ETerminal {
				module.Exprs[i].A = -1
				break
			}
		}
		if err := validateModule(module); err == nil {
			t.Fatal("invalid terminal type accepted")
		}
	}
	for _, source := range []string{`rule main = "x"`, `rule main = "x" => []Match{$1}`} {
		p := compile(t, source)
		data, err := p.MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
		if err != nil || data[len(compiledMagic)+1] != 3 {
			t.Fatalf("unchanged/list-only ISA: %v", err)
		}
	}
}

func TestNodeConstructorsWithoutTypeChecking(t *testing.T) {
	const header = "type Int terminal\ntype Other struct {}\nrule main = \"x\" => "
	for _, action := range []string{`[]int{}`, `[][]string{}`, `Int{}`, `Int{"a", "b"}`, `Other{"a"}`} {
		g, err := syntax.Parse(header + action)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Compile(g, Options{NoTypeCheck: true}); err == nil {
			t.Fatalf("NoTypeCheck accepted unsupported constructor shape: %s", action)
		}
	}
	for _, action := range []string{`[]Int{nil}`, `[]Int{Other{}}`, `[]Int{1}`, `Int{1}`} {
		g, err := syntax.Parse(header + action)
		if err != nil {
			t.Fatal(err)
		}
		p, err := Compile(g, Options{NoTypeCheck: true})
		if err != nil {
			t.Fatalf("assignability was not bypassed for %s: %v", action, err)
		}
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			n, err := p.ParseWith("main", "x", ParseOptions{Backend: backend})
			invalidValue := action == `[]Int{1}` || action == `Int{1}`
			if invalidValue != (err != nil) || invalidValue == (n != nil) {
				t.Fatalf("%s/%s: %v / %v", action, backend, n, err)
			}
			if !invalidValue && (len(n.Children) != 1 || (n.Children[0] == nil) != (action == `[]Int{nil}`)) {
				t.Fatal(resultJSON(n, err))
			}
		}
	}
	for _, action := range []string{`[]Int{1}`, `Int{1}`} {
		g, err := syntax.Parse("type Int terminal\nrule main = {" + action + " != nil} \"x\" / \"x\"")
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
