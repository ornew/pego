package engine

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// fieldCheckValues are the dynamically typed values that fieldCheckGrammar passes to a struct
// field, by the first input character: nodes and nil from the any-typed rule val, and scalars
// from variables.
var fieldCheckValues = map[string]string{
	"m": "Match", "t": "Int", "l": "[]Int", "k": "[]Match", "e": "[]nil", "n": "empty List",
	"q": "Box", "?": "Error", "w": "[][]Int", "p": "[]*Int", "z": "nil", "i": "int", "s": "string", "b": "bool",
}

// fieldCheckFields maps the declared types of fieldCheckGrammar's field F to the values they
// accept.
var fieldCheckFields = map[string]string{
	"Alias":         "t?",
	"*Int":          "t?z",
	"[]Int":         "l?n",
	"[]*Int":        "l?nep",
	"Int | string":  "t?s",
	"int":           "i",
	"string":        "s",
	"bool":          "b",
	"any":           "mtlkenq?wpzisb",
	"node":          "mtlkenq?wp",
	"terminal":      "mt?",
	"[][]Int":       "w?n",
	"*(Box | bool)": "q?zb",
	"Match":         "m?",
	"List":          "lkenwp?",
	"*[]*Int":       "l?nepz",
}

// fieldCheckGrammar gives S a field F of type typ. Constructing S succeeds with an accepted value
// (input "<value>.") and is an action error otherwise; in a predicate (input "<value>!") the
// rejected value makes the predicate fail, so the rule no matches instead.
func fieldCheckGrammar(typ string) string {
	return `type Int terminal
type Alias ~ Int
type Box = { Value Int }
type S = { F ` + typ + ` }
type Result ~ S | Int
rule main Result = nodeCase / nodePred / scalarCase / scalarPred / no
rule nodeCase = v:val "." => S{F: $v}
rule nodePred = v:val {S{F: $v} != nil} "!" => Int{"yes"}
rule scalarCase = {x := 0} ("i" {x := 1} / "s" {x := "s"} / "b" {x := true}) "." => S{F: x}
rule scalarPred = {x := 0} ("i" {x := 1} / "s" {x := "s"} / "b" {x := true}) {S{F: x} != nil} "!" => Int{"yes"}
rule no = [a-z!.?;]+ => Int{"no"}
rule val any = vm / vt / vl / vk / ve / vn / vq / vr / vw / vp / vz
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
`
}

func fieldCheckTypes() []string {
	var types []string
	for typ := range fieldCheckFields {
		types = append(types, typ)
	}
	sort.Strings(types)
	return types
}

func fieldCheckInputs() []string {
	var in []string
	for v := range fieldCheckValues {
		in = append(in, v+".")
	}
	sort.Strings(in)
	return append(in, "l!", "k!", "z!", "i!", "s!", "?!")
}

// TestConstructedFieldChecks checks which dynamically typed values each declared field type
// accepts, on every backend, with and without type checking, and from a compiled file.
func TestConstructedFieldChecks(t *testing.T) {
	for _, typ := range fieldCheckTypes() {
		t.Run(typ, func(t *testing.T) {
			g, err := syntax.Parse(fieldCheckGrammar(typ))
			if err != nil {
				t.Fatal(err)
			}
			checked, err := Compile(g, Options{})
			if err != nil {
				t.Fatal(err)
			}
			unchecked, err := Compile(g, Options{NoTypeCheck: true})
			if err != nil {
				t.Fatal(err)
			}
			data, err := checked.MarshalBinary("main")
			if err != nil {
				t.Fatal(err)
			}
			loaded, _, err := LoadProgram(data, Options{})
			if err != nil {
				t.Fatal(err)
			}
			bare, err := checked.MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
			if err != nil {
				t.Fatal(err)
			}
			loadedBare, _, err := LoadProgram(bare, Options{})
			if err != nil {
				t.Fatal(err)
			}
			for _, in := range fieldCheckInputs() {
				value := fieldCheckValues[in[:1]]
				accepted := strings.Contains(fieldCheckFields[typ], in[:1])
				var want string
				for _, unit := range []Unit{CodePoints, Bytes} {
					for _, run := range []struct {
						name string
						prog *Program
						b    Backend
					}{
						{"closure", checked, Closure}, {"bytecode", checked, Bytecode}, {"iterative", checked, BytecodeIterative},
						{"unchecked closure", unchecked, Closure}, {"unchecked bytecode", unchecked, Bytecode},
						{"loaded closure", loaded, Closure}, {"loaded bytecode", loaded, BytecodeIterative},
						{"bare bytecode", loadedBare, Bytecode},
					} {
						n, err := run.prog.ParseWith("main", in, ParseOptions{Backend: run.b, Unit: unit})
						got := resultJSON(n, err)
						if want == "" {
							want = got
						} else if got != want {
							t.Fatalf("%q %s %s: %s\nwant %s", in, run.name, unit, got, want)
						}
					}
				}
				name := value
				if strings.HasPrefix(value, "[]") || strings.HasPrefix(value, "empty ") {
					name = "List"
				}
				msg := fmt.Sprintf("cannot use %s as %s in field F of S", name, checked.typed.fields["S"][0].t)
				switch {
				case in[1] == '!' && accepted:
					if !strings.Contains(want, `"value":"yes"`) {
						t.Errorf("predicate %q (%s) rejected: %s", in, value, want)
					}
				case in[1] == '!':
					if !strings.Contains(want, `"value":"no"`) {
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
		})
	}
}

// TestConstructedFieldChecksAreSelective checks that only values not statically known to fit get
// a run-time check, which needs instruction set 9.
func TestConstructedFieldChecksAreSelective(t *testing.T) {
	src := `type Int terminal
type S = { Static Int; Var int; Dynamic Int; Elements []Int; Union *Int; Untyped any; Empty []Int; Joined []Int }
rule main = {x := 1} a:item d:val l:item* o:item? => S{Static: $a, Var: x, Dynamic: $d, Elements: $l, Union: $o, Untyped: x, Empty: filter(nil, (e) => true), Joined: concat(nil, $l)}
rule item Int = "i" => Int{"i"}
rule val any = "v"
`
	prog := compile(t, src)
	var checked []string
	for fi, typ := range prog.checks.fields {
		checked = append(checked, fi.Name+" "+typ.String())
	}
	sort.Strings(checked)
	if got := strings.Join(checked, ", "); got != "Dynamic Int, Var int" {
		t.Fatalf("checked fields: %s", got)
	}
	m := prog.Module()
	if v := m.instructionSetVersion(); v != isaVersion {
		t.Fatalf("instruction set %d", v)
	}
	if d := m.Disassemble(); strings.Count(d, "ECHECK") != 2 || !strings.Contains(d, `"Int" field=Dynamic of=S`) {
		t.Fatalf("disassembly:\n%s", d)
	}
	plain := compile(t, `type Int terminal
type S = { Static Int }
rule main = a:item => S{Static: $a}
rule item Int = "i" => Int{"i"}
`)
	if v := plain.Module().instructionSetVersion(); v != isaVersion {
		t.Fatalf("unchecked module instruction set %d", v)
	}
	if got := resultWith(prog, "iv", ParseOptions{Backend: Bytecode}); !strings.Contains(got, "cannot use Match as Int in field Dynamic of S") {
		t.Fatalf("dynamic Match: %s", got)
	}
	// The recognizer, built without type checking, checks the same fields as its program.
	pred := compile(t, `type Int terminal
type S = { Static Int; Dynamic Int }
rule main = a:item d:val {S{Static: $a, Dynamic: $d} != nil}
rule item Int = "i" => Int{"i"}
rule val any = "v"
`)
	rec, err := pred.recognizer()
	if err != nil {
		t.Fatal(err)
	}
	if d := rec.Module().Disassemble(); strings.Count(d, "ECHECK") != 1 {
		t.Fatalf("recognizer disassembly:\n%s", d)
	}
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Generate(g, GenOptions{Package: "p", Start: "main", Types: true, Recognize: true})
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(code), "var fieldType"); n != 2 {
		t.Fatalf("generated %d field type variables", n)
	}
}

// TestCompiledFieldChecksValidation checks that loading rejects files of an earlier
// instruction set and field-check type texts that do not name a known type.
func TestCompiledFieldChecksValidation(t *testing.T) {
	prog := compile(t, fieldCheckGrammar("[]*(Int | Match)"))
	data, err := prog.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	if data[len(compiledMagic)+1] != isaVersion {
		t.Fatalf("instruction set %d", data[len(compiledMagic)+1])
	}
	d := append([]byte(nil), data[:len(data)-4]...)
	d[len(compiledMagic)+1] = isaVersion - 1
	d = binary.LittleEndian.AppendUint32(d, crc32.ChecksumIEEE(d))
	// A file of an earlier instruction set is rejected before its records are read.
	if _, _, err := LoadProgram(d, Options{}); err == nil || !strings.Contains(err.Error(), "compile the grammar again") {
		t.Fatalf("earlier instruction set: %v", err)
	}
	m := *prog.Module()
	if err := validateModule(&m); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Other", "[]", "*(Int)", "Int |", "(Int | Match", "Int  | Match", "[]Int)", ""} {
		m.Strings = append(append([]string(nil), prog.Module().Strings...), text)
		m.Exprs = append([]Instr(nil), prog.Module().Exprs...)
		for i := range m.Exprs {
			if m.Exprs[i].Op == ECheck {
				m.Exprs[i].A = int32(len(m.Strings) - 1)
			}
		}
		if err := validateModule(&m); err == nil || !strings.Contains(err.Error(), "invalid field type") {
			t.Errorf("type text %q: %v", text, err)
		}
	}
	for _, text := range []string{"Int", "*Int", "[]*(Int | Match)", "*(Box | bool)", "[][]Int", "int | string", "Error", "node", "terminal"} {
		if _, err := parseFieldType(text, map[string]bool{"Int": true, "Box": true}); err != nil {
			t.Errorf("type text %q: %v", text, err)
		}
	}
}

func fieldCheckCases() []genCase {
	var cases []genCase
	for _, typ := range fieldCheckTypes() {
		cases = append(cases, genCase{"field check " + typ, fieldCheckGrammar(typ), fieldCheckInputs()})
	}
	return cases
}

func TestGeneratedConstructedFieldChecks(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	cases := fieldCheckCases()
	testGeneratedParsersCorpus(t, goBin, cases, GenOptions{})
	testGeneratedTypesCorpus(t, goBin, cases, GenOptions{convertTypes: true})
	if node, err := exec.LookPath("node"); err == nil {
		testGeneratedTSParsersCorpus(t, node, cases, GenOptions{})
	} else {
		t.Error("Node is required for field-check parity")
	}
}
