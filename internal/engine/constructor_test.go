package engine

import (
	"encoding/binary"
	"hash/crc32"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/bytecode"
	"github.com/ornew/zeroparse/internal/compile"
	"github.com/ornew/zeroparse/internal/gentest"
	"github.com/ornew/zeroparse/internal/value"
)

// legacyConstructorGrammar is constructorGrammar in the retired terminal type syntax, read with
// syntax.ParseLegacy as the text structs that zp fix writes for it.
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
				got := gentest.ResultJSON(n, nil)
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
				if got, want := gentest.ResultJSON(loaded.ParseWith("main", "😀", servedBy(mode, opts))), gentest.ResultJSON(p.ParseWith("main", "😀", opts)); got != want {
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
