package engine

import (
	"encoding/binary"
	"fmt"
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
	"github.com/ornew/pego/internal/gentest"
)

// fieldCheckValueName names a value of fieldCheckValues as messages do, in the terms of the value
// model: lists built by actions are list values, whose elements messages do not examine.
func fieldCheckValueName(value string) string {
	switch {
	case strings.HasPrefix(value, "[]") || strings.HasPrefix(value, "empty "):
		return "[]_"
	case value == "Match":
		return "node[string]"
	case value == "Error":
		return "error"
	}
	return value
}

// fieldCheckGrammarLegacy is fieldCheckGrammar with a terminal type Int, in the retired syntax that
// only syntax.ParseLegacy accepts: grammars that reach the engine as JSON ASTs may still use it.
func fieldCheckGrammarLegacy(typ string) string {
	return `type Int terminal
type Alias ~ Int
type Box = { Value Int }
type S = { F ` + typ + ` }
type Result ~ S | Int
rule main Result = nodeCase / nodePred / no
rule nodeCase = v:val "." => S{F: $v.(` + typ + `)}
rule nodePred = v:val {S{F: $v.(` + typ + `)} != nil} "!" => Int{"yes"}
rule no = [a-z!.?;]+ => Int{"no"}
rule val _ = vm / vt / vl / vk / ve / vn / vq / vr / vw / vp / vz / vi / vs / vb
rule vm = "m"
rule vt = "t" => Int{"t"}
rule vl = "l" => []Int{Int{"a"}}
rule vk = k:"k" => list($k)
rule ve = "e" => list(nil)
rule vn = "n" => list()
rule vq = "q" => Box{Value: Int{"q"}}
rule vr = ("r" ";") #recover(skip="?")
rule vw = "w" => [][]Int{[]Int{Int{"w"}}}
rule vp = "p" => list(Int{"a"}, nil)
rule vz = "z" => nil
rule vi = "i" => 1
rule vs = "s" => "s"
rule vb = "b" => true
`
}

// TestConstructedFieldChecks checks which dynamically typed values a cast to each declared field
// type accepts, on every backend, and from a compiled file.
func TestConstructedFieldChecks(t *testing.T) {
	t.Parallel()
	// The terminal model is the same grammar in the retired terminal type syntax, which
	// syntax.ParseLegacy reads as the text structs it means: it accepts the same values.
	for _, model := range []struct {
		name    string
		grammar func(string) string
		parse   func(string) (*grammar.Grammar, error)
		fields  map[string]string
	}{
		{"text struct", gentest.FieldCheckGrammar, parseTest, gentest.FieldCheckFields},
		{"terminal", fieldCheckGrammarLegacy, parseLegacyTest, gentest.FieldCheckFields},
	} {
		t.Run(model.name, func(t *testing.T) {
			for _, typ := range gentest.FieldCheckTypesOf(model.fields) {
				t.Run(typ, func(t *testing.T) { checkConstructedFieldType(t, typ, model.grammar(typ), model.parse, model.fields) })
			}
		})
	}
}

func checkConstructedFieldType(t *testing.T, typ, src string, parse func(string) (*grammar.Grammar, error), fields map[string]string) {
	yes, no := `"Text":"yes"`, `"Text":"no"`
	{
		{
			g, err := parse(src)
			if err != nil {
				t.Fatal(err)
			}
			checked, err := Compile(g, compile.Options{})
			if err != nil {
				t.Fatal(err)
			}
			loaded := saveLoad(t, checked, "main", CompiledDocument)
			loadedBare := saveLoad(t, checked, "main", CompiledParse)
			_, err = loaded.ParseWith("main", "", servedBy(CompiledDocument, ParseOptions{Backend: Closure}))
			wantClosureRefused(t, err)
			for _, in := range gentest.FieldCheckInputs() {
				value := gentest.FieldCheckValues[in[:1]]
				accepted := strings.Contains(fields[typ], in[:1])
				var want string
				for _, unit := range []PositionUnit{CodePoints, Bytes} {
					for _, run := range []struct {
						name string
						prog *Program
						mode CompiledMode
						b    Backend
					}{
						{"closure", checked, CompiledParse, Closure}, {"bytecode", checked, CompiledParse, Bytecode}, {"iterative", checked, CompiledParse, BytecodeIterative},
						{"document bytecode", loaded, CompiledDocument, Bytecode}, {"document iterative", loaded, CompiledDocument, BytecodeIterative},
						{"parse bytecode", loadedBare, CompiledParse, Bytecode},
					} {
						n, err := run.prog.ParseWith("main", in, servedBy(run.mode, ParseOptions{Backend: run.b, Unit: unit}))
						got := gentest.ResultJSON(n, err)
						if want == "" {
							want = got
						} else if got != want {
							t.Fatalf("%q %s %s: %s\nwant %s", in, run.name, unit, got, want)
						}
					}
				}
				name := fieldCheckValueName(value)
				msg := fmt.Sprintf("cannot cast %s to %s", name, checked.Typed.Fields["S"][0].T)
				switch {
				case in[1] == '!' && accepted:
					if !strings.Contains(want, yes) {
						t.Errorf("predicate %q (%s) rejected: %s", in, value, want)
					}
				case in[1] == '!':
					if !strings.Contains(want, no) {
						t.Errorf("predicate %q (%s) accepted: %s", in, value, want)
					}
				case accepted:
					if !strings.Contains(want, `"type":"S"`) {
						t.Errorf("%q (%s) rejected: %s", in, value, want)
					}
				case !strings.Contains(want, `"err":"action in `) || !strings.Contains(want, msg):
					t.Errorf("%q (%s) accepted or misreported: %s", in, value, want)
				}
			}
		}
	}
}

// TestCompiledFieldChecksValidation checks that loading rejects files of an earlier
// instruction set and cast type texts that do not name a known type.
func TestCompiledFieldChecksValidation(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, gentest.FieldCheckGrammar("[]*(Int | node[string])"))
	data, err := prog.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	if data[isaOffset] != bytecode.ISAVersion {
		t.Fatalf("instruction set %d", data[isaOffset])
	}
	d := append([]byte(nil), data[:len(data)-4]...)
	d[isaOffset] = bytecode.ISAVersion - 1
	d = binary.LittleEndian.AppendUint32(d, crc32.ChecksumIEEE(d))
	// A file of an earlier instruction set is rejected before its records are read.
	if _, _, err := LoadProgram(d); err == nil || !strings.Contains(err.Error(), "compile the grammar again") {
		t.Fatalf("earlier instruction set: %v", err)
	}
	m := *prog.Module()
	if err := compile.ValidateModule(&m); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Other", "[]", "*(Int)", "Int |", "(Int | Match", "Int  | Match", "[]Int)", "", "terminal", "Missing"} {
		m.Strings = append(append([]string(nil), prog.Module().Strings...), text)
		m.Exprs = append([]bytecode.Instr(nil), prog.Module().Exprs...)
		for i := range m.Exprs {
			if m.Exprs[i].Op == bytecode.ECast {
				m.Exprs[i].A = int32(len(m.Strings) - 1)
			}
		}
		if err := compile.ValidateModule(&m); err == nil || !strings.Contains(err.Error(), "invalid field type") {
			t.Errorf("type text %q: %v", text, err)
		}
	}
	for _, text := range []string{"Int", "*Int", "[]*(Int | node[string])", "*(Box | bool)", "[][]Int", "int | string", "error", "node[_]", "node[string] | error"} {
		if _, err := compile.ParseFieldType(text, map[string]bool{"Int": true, "Box": true}); err != nil {
			t.Errorf("type text %q: %v", text, err)
		}
	}
}

func TestGeneratedConstructedFieldChecks(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedConstructedFieldChecks)
}

func testGeneratedConstructedFieldChecks(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	cases := gentest.FieldCheckCases()
	gentest.GeneratedParsersCorpus(t, goBin, cases, gen.GenOptions{})
	gentest.GeneratedTypesCorpus(t, goBin, cases, gen.GenOptions{ConvertTypes: true})
	if node, err := exec.LookPath("node"); err == nil {
		testGeneratedTSParsersCorpus(t, node, cases, gen.GenOptions{})
	} else {
		t.Error("Node is required for field-check parity")
	}
}
