package engine

import (
	"encoding/binary"
	"hash/crc32"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/bytecode"
	"github.com/ornew/zeroparse/internal/gentest"
	"github.com/ornew/zeroparse/internal/value"
)

func TestIndexAcrossBackends(t *testing.T) {
	t.Parallel()
	for _, fixture := range gentest.IndexCases() {
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

func TestIndexValuesAndErrors(t *testing.T) {
	t.Parallel()
	program := mustCompile(t, `rule main = xs:item+ $$ => $xs[1]
rule item = [a-z]`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		node, err := program.ParseWith("main", "abc", ParseOptions{Backend: backend})
		if err != nil {
			t.Fatalf("%s: %v", backend, err)
		}
		if value.D(node).Text != "b" {
			t.Fatalf("%s: indexed %q", backend, value.D(node).Text)
		}
		_, err = program.ParseWith("main", "a", ParseOptions{Backend: backend})
		if err == nil || !strings.Contains(err.Error(), "index 1 out of range for 1 elements") {
			t.Fatalf("%s: %v", backend, err)
		}
	}
	if got := compileMessage(t, `var i string
rule main = {i := "0"} xs:item+ $$ => $xs[i]
rule item = [a-z]`); !strings.Contains(got, "index must be int, got string") {
		t.Fatalf("string index variable: %q", got)
	}
}

// Elements and captures that hold values other than nodes read as those values, not as the
// carriers that hold them in the tree.
func TestIndexValueElements(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		src    string
		inputs map[string]int
	}{
		{"rule main = x:(n n) $$ => $x[0] + $x[1]\nrule n = [0-9] => 1", map[string]int{"12": 2}},
		{"rule main = xs:n+ $$ => foldl(0, $xs, (a, x) => a + x)\nrule n = [0-9] => 2", map[string]int{"123": 6}},
		{"rule main = x:p $$ => $x.$v + 1\nrule p = v:n \"!\"\nrule n = [0-9] => 1", map[string]int{"1!": 2}},
	} {
		program := mustCompile(t, tc.src)
		for input, want := range tc.inputs {
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				node, err := program.ParseWith("main", input, ParseOptions{Backend: backend})
				if err != nil {
					t.Fatalf("%s %q: %v\n%s", backend, input, err, tc.src)
				}
				if got := value.UnboxNode(node); got != want {
					t.Fatalf("%s %q: got %v, want %d\n%s", backend, input, got, want, tc.src)
				}
			}
		}
	}
}

func TestIndexCompiledInstructionSet(t *testing.T) {
	t.Parallel()
	src := `rule main = xs:item+ $$ => $xs[0]
rule item = [a-z]`
	program := mustCompile(t, src)
	for _, mode := range valueModes {
		data, err := program.Marshal("main", mode)
		if err != nil || data[isaOffset] != bytecode.ISAVersion {
			t.Fatalf("index ISA: %v", err)
		}
		loaded, _, err := LoadProgram(data)
		if err != nil {
			t.Fatal(err)
		}
		for _, backend := range []Backend{Bytecode, BytecodeIterative} {
			options := ParseOptions{Backend: backend}
			if got, want := gentest.ResultJSON(loaded.ParseWith("main", "ab", servedBy(mode, options))), gentest.ResultJSON(program.ParseWith("main", "ab", options)); got != want {
				t.Fatalf("loaded index: %s / %s", got, want)
			}
		}
		data[isaOffset] = bytecode.ISAVersion - 1
		binary.LittleEndian.PutUint32(data[len(data)-4:], crc32.ChecksumIEEE(data[:len(data)-4]))
		if _, _, err := LoadProgram(data); err == nil || !strings.Contains(err.Error(), "compile the grammar again") {
			t.Fatalf("under-declared index ISA accepted: %v", err)
		}
	}
}
