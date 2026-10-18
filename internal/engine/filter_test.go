package engine

import (
	"encoding/binary"
	"hash/crc32"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

const filterIdentitySource = `type Out = { First Match; Kept []Match; Empty []Match }
rule main = first:"a" second:"b" $$ => Out{First: $first, Kept: filter(list($first, $second, $first), (x) => text(x) == "a"), Empty: filter(nil, (x) => true)}`

func filterCases() []genCase {
	return []genCase{
		{"identity and nil input", filterIdentitySource, []string{"ab", "a", "ac", ""}},
		{"nil elements", `type Out = { Items []*Match }
rule main = x:"é" $$ => Out{Items: filter(list(nil, $x, nil, $x), (item) => item == nil || text(item) == "é")}`, []string{"é", "a", ""}},
		{"remove all", `rule main = v1:"é" $$ => filter(list($v1), (item) => false)`, []string{"é", "", "a"}},
		{"keep order", `rule main = x:[a-z]* #atomic $$ => filter(list($x, nil, $x), (item) => item != nil)`, []string{"ab", "", "é"}},
		{"nested lambda", `rule main = v1:"a" $$ => filter(list($v1), (item) => len(filter(list(item, nil), (inner) => inner != nil)) == 1)`, []string{"a", "", "ab"}},
		{"predicate error rollback", `rule main = a:"a" ({len(filter(list($a), (item) => 1 / 0 == 0)) == 1} "!" / "?") $$`, []string{"a?", "a!", "a", ""}},
		{"value error", `rule main = v1:"a" $$ => filter(list($v1), (item) => 1 / 0 == 0)`, []string{"a", "", "ab"}},
		{"dynamic nonbool", `rule main = {value := 1} v1:"a" $$ => filter(list($v1), (item) => value)`, []string{"a", "", "ab"}},
		{"dynamic nonbool predicate", `rule main = {value := 1} a:"a" ({len(filter(list($a), (item) => value)) == 1} "!" / "?") $$`, []string{"a?", "a!", "a", ""}},
	}
}

func TestFilterNodeIdentityAndEmptyInput(t *testing.T) {
	program := compile(t, filterIdentitySource)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			node, err := program.ParseWith("main", "ab", ParseOptions{Backend: backend, Unit: unit})
			if err != nil {
				t.Fatalf("%s/%s: %v", backend, unit, err)
			}
			first := node.Field("First").(*Node)
			kept := node.Field("Kept").(*Node)
			empty := node.Field("Empty").(*Node)
			if len(kept.children) != 2 || kept.children[0] != first || kept.children[1] != first {
				t.Fatalf("%s/%s: filter changed order or identity", backend, unit)
			}
			if empty == nil || len(empty.children) != 0 {
				t.Fatalf("%s/%s: nil input did not produce an empty list", backend, unit)
			}
		}
	}
}

func TestFilterAcrossBackends(t *testing.T) {
	for _, fixture := range filterCases() {
		t.Run(fixture.name, func(t *testing.T) {
			program := compile(t, fixture.src)
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				for _, recognize := range []bool{false, true} {
					for _, input := range fixture.inputs {
						baseline := resultJSON(program.ParseWith("main", input, ParseOptions{Backend: Closure, Unit: unit, Recognize: recognize}))
						for _, backend := range []Backend{Bytecode, BytecodeIterative} {
							actual := resultJSON(program.ParseWith("main", input, ParseOptions{Backend: backend, Unit: unit, Recognize: recognize}))
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
	for _, source := range []string{
		`rule main = v1:"a" => filter(list($v1), (x) => 1)`,
		`rule main = v1:"a" => filter(list($v1), (x, y) => true)`,
		`rule main = v1:"a" => filter(list($v1), true)`,
		`rule main = "a" => filter(1, (x) => true)`,
		`rule main = v1:"a" => filter(list($v1))`,
		`type Out = { Items []Match }
rule main = v1:"a" => Out{Items: filter(list($v1, nil), (x) => x != nil)}`,
	} {
		g, err := syntax.Parse(source)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Compile(g, Options{}); err == nil {
			t.Fatalf("accepted invalid filter: %s", source)
		}
	}
	for _, source := range []string{
		`rule main = v1:"a" => filter(list($v1), (x) => 1)`,
		`rule main = v1:"a" => filter(list($v1), (x) => nil)`,
	} {
		program := compile(t, source, Options{NoTypeCheck: true})
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				_, err := program.ParseWith("main", "a", ParseOptions{Backend: backend, Unit: unit})
				if err == nil || !strings.Contains(err.Error(), "filter: predicate must produce a bool") {
					t.Fatalf("%s/%s: %v", backend, unit, err)
				}
			}
		}
	}
}

func TestFilterCompiledInstructionSet(t *testing.T) {
	program := compile(t, filterIdentitySource)
	for _, omit := range []bool{false, true} {
		data, err := program.MarshalBinaryWith("main", MarshalOptions{OmitAST: omit})
		if err != nil || data[len(compiledMagic)+1] != isaVersion {
			t.Fatalf("filter ISA: %v", err)
		}
		loaded, _, err := LoadProgram(data, Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, backend := range []Backend{Bytecode, BytecodeIterative} {
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				options := ParseOptions{Backend: backend, Unit: unit}
				if got, want := resultJSON(loaded.ParseWith("main", "ab", options)), resultJSON(program.ParseWith("main", "ab", options)); got != want {
					t.Fatalf("loaded filter: %s / %s", got, want)
				}
			}
		}
		data[len(compiledMagic)+1] = isaVersion - 1
		binary.LittleEndian.PutUint32(data[len(data)-4:], crc32.ChecksumIEEE(data[:len(data)-4]))
		if _, _, err := LoadProgram(data, Options{}); err == nil || !strings.Contains(err.Error(), "compile the grammar again") {
			t.Fatalf("under-declared filter ISA accepted: %v", err)
		}
	}
	ordinary := compile(t, `rule main = "a"`)
	if ordinary.Module().instructionSetVersion() != isaVersion {
		t.Fatal("filter support changed the ordinary instruction set")
	}
	module := compile(t, filterIdentitySource).Module()
	for i := range module.Exprs {
		if module.Exprs[i].Op == ECall && module.Exprs[i].A == bFilter {
			module.Exprs[i].B = 0
			if err := validateModule(module); err == nil || !strings.Contains(err.Error(), "filter requires two arguments") {
				t.Fatalf("invalid filter call arity: %v", err)
			}
			return
		}
	}
	t.Fatal("fixture emitted no filter call")
}

func TestFilterGeneratedGoParity(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	testGeneratedParsersCorpus(t, goBin, filterCases(), GenOptions{})
	testGeneratedTypesCorpus(t, goBin, filterCases(), GenOptions{})
}

func TestFilterGeneratedTypeScriptParity(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	testGeneratedTSParsersCorpus(t, node, filterCases(), GenOptions{})
}
