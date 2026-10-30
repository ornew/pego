package engine

import (
	"encoding/binary"
	"hash/crc32"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/bytecode"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/value"
)

func TestFilterNodeIdentityAndEmptyInput(t *testing.T) {
	t.Parallel()
	program := mustCompile(t, gentest.FilterIdentitySource)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			node, err := program.ParseWith("main", "ab", ParseOptions{Backend: backend, Unit: unit})
			if err != nil {
				t.Fatalf("%s/%s: %v", backend, unit, err)
			}
			first := node.Field("First").(*value.Node)
			kept := node.Field("Kept").(*value.Node)
			empty := node.Field("Empty").(*value.Node)
			if len(value.D(kept).Children) != 2 || value.D(kept).Children[0] != first || value.D(kept).Children[1] != first {
				t.Fatalf("%s/%s: filter changed order or identity", backend, unit)
			}
			if empty == nil || len(value.D(empty).Children) != 0 {
				t.Fatalf("%s/%s: nil input did not produce an empty list", backend, unit)
			}
		}
	}
}

func TestFilterAcrossBackends(t *testing.T) {
	t.Parallel()
	for _, fixture := range gentest.FilterCases() {
		t.Run(fixture.Name, func(t *testing.T) {
			program := mustCompile(t, fixture.Src)
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				for _, recognize := range []bool{false, true} {
					for _, input := range fixture.Inputs {
						baseline := gentest.ResultJSON(program.ParseWith("main", input, ParseOptions{Backend: Closure, Unit: unit, Recognize: recognize}))
						for _, backend := range []Backend{Bytecode, BytecodeIterative} {
							actual := gentest.ResultJSON(program.ParseWith("main", input, ParseOptions{Backend: backend, Unit: unit, Recognize: recognize}))
							if actual != baseline {
								t.Fatalf("%s/%s recognize=%t input=%q: %s / %s", backend, unit, recognize, input, actual, baseline)
							}
						}
					}
				}
			}
		})
	}
}

func TestFilterTypeAndRuntimeValidation(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`rule main = v1:"a" => filter(list($v1), (x) => 1)`,
		`rule main = v1:"a" => filter(list($v1), (x, y) => true)`,
		`rule main = v1:"a" => filter(list($v1), true)`,
		`rule main = "a" => filter(1, (x) => true)`,
		`rule main = v1:"a" => filter(list($v1))`,
		`type Out = { Items []node[string] }
rule main = v1:"a" => Out{Items: filter(list($v1, nil), (x) => x != nil)}`,
	} {
		g, err := parseTest(source)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Compile(g, compile.Options{}); err == nil {
			t.Fatalf("accepted invalid filter: %s", source)
		}
	}
	for _, source := range []string{
		`rule main = v1:"a" => filter(list($v1), (x) => 1)`,
		`rule main = v1:"a" => filter(list($v1), (x) => nil)`,
	} {
		if got := compileMessage(t, source); !strings.Contains(got, "filter: predicate must produce a bool") {
			t.Fatalf("%s: %q", source, got)
		}
	}
}

func TestFilterCompiledInstructionSet(t *testing.T) {
	t.Parallel()
	program := mustCompile(t, gentest.FilterIdentitySource)
	for _, mode := range valueModes {
		data, err := program.Marshal("main", mode)
		if err != nil || data[isaOffset] != bytecode.ISAVersion {
			t.Fatalf("filter ISA: %v", err)
		}
		loaded, _, err := LoadProgram(data)
		if err != nil {
			t.Fatal(err)
		}
		for _, backend := range []Backend{Bytecode, BytecodeIterative} {
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				options := ParseOptions{Backend: backend, Unit: unit}
				if got, want := gentest.ResultJSON(loaded.ParseWith("main", "ab", servedBy(mode, options))), gentest.ResultJSON(program.ParseWith("main", "ab", options)); got != want {
					t.Fatalf("loaded filter: %s / %s", got, want)
				}
			}
		}
		data[isaOffset] = bytecode.ISAVersion - 1
		binary.LittleEndian.PutUint32(data[len(data)-4:], crc32.ChecksumIEEE(data[:len(data)-4]))
		if _, _, err := LoadProgram(data); err == nil || !strings.Contains(err.Error(), "compile the grammar again") {
			t.Fatalf("under-declared filter ISA accepted: %v", err)
		}
	}
	ordinary := mustCompile(t, `rule main = "a"`)
	if ordinary.Module().InstructionSetVersion() != bytecode.ISAVersion {
		t.Fatal("filter support changed the ordinary instruction set")
	}
	module := mustCompile(t, gentest.FilterIdentitySource).Module()
	for i := range module.Exprs {
		if module.Exprs[i].Op == bytecode.ECall && module.Exprs[i].A == bytecode.BuiltinFilter {
			module.Exprs[i].B = 0
			if err := compile.ValidateModule(module); err == nil || !strings.Contains(err.Error(), "filter requires two arguments") {
				t.Fatalf("invalid filter call arity: %v", err)
			}
			return
		}
	}
	t.Fatal("fixture emitted no filter call")
}

// The checker rejects a predicate of type _, so a predicate that produces another value than a
// bool is rejected when the grammar is compiled.
func TestFilterNonBoolPredicateIsRejected(t *testing.T) {
	t.Parallel()
	const src = `rule main = v1:"a" d:one $$ => filter(list($v1), (item) => $d)
rule one _ = _ => 1`
	if got := compileMessage(t, src); !strings.Contains(got, "filter: predicate must produce a bool, got one") {
		t.Errorf("got %q", got)
	}
}
