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

	"github.com/ornew/pego/grammar"
)

// fieldCheckValues are the dynamically typed values that fieldCheckGrammar passes to a struct
// field, by the first input character: nodes, nil and scalars from the rule val of type _.
var fieldCheckValues = map[string]string{
	"m": "Match", "t": "Int", "l": "[]Int", "k": "[]Match", "e": "[]nil", "n": "empty List",
	"q": "Box", "?": "Error", "w": "[][]Int", "p": "[]*Int", "z": "nil", "i": "int", "s": "string", "b": "bool",
}

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

// fieldCheckFields maps the declared types of fieldCheckGrammar's field F to the values they
// accept. A match is a string node and a value of the text struct Int is a node of its own type,
// lists built by actions are list values, which are not nodes, and an error value fits only a type
// that admits error.
var fieldCheckFields = map[string]string{
	"Alias":                    "t",
	"*Int":                     "tz",
	"[]Int":                    "ln",
	"[]*Int":                   "lnep",
	"Int | string":             "ts",
	"int":                      "i",
	"string":                   "s",
	"bool":                     "b",
	"_":                        "mtlkenq?wpzisb",
	"node[_]":                  "m",
	"[][]Int":                  "wn",
	"*(Box | bool)":            "qzb",
	"node[string]":             "m",
	"*[]*Int":                  "lnepz",
	"Int | error":              "t?",
	"[](node[string] | error)": "kn",
}

// fieldCheckGrammar gives S a field F of type typ, set from the dynamically typed value of val, which
// the grammar must cast to typ. Constructing S succeeds with an accepted value (input
// "<value>.") and is an action error of the cast otherwise; in a predicate (input "<value>!")
// the rejected value makes the predicate fail, so the rule no matches instead.
func fieldCheckGrammar(typ string) string {
	return `type Int = { Text string }
type Alias ~ Int
type Box = { Value Int }
type S = { F ` + typ + ` }
type Result ~ S | Int
rule main Result = nodeCase / nodePred / no
rule nodeCase = v:val "." => S{F: $v.(` + typ + `)}
rule nodePred = v:val {S{F: $v.(` + typ + `)} != nil} "!" => Int{Text: "yes"}
rule no = [a-z!.?;]+ => Int{Text: "no"}
rule val _ = vm / vt / vl / vk / ve / vn / vq / vr / vw / vp / vz / vi / vs / vb
rule vm = "m"
rule vt = "t" => Int{Text: "t"}
rule vl = "l" => []Int{Int{Text: "a"}}
rule vk = k:"k" => list($k)
rule ve = "e" => list(nil)
rule vn = "n" => list()
rule vq = "q" => Box{Value: Int{Text: "q"}}
rule vr = ("r" ";") #recover(skip="?")
rule vw = "w" => [][]Int{[]Int{Int{Text: "w"}}}
rule vp = "p" => list(Int{Text: "a"}, nil)
rule vz = "z" => nil
rule vi = "i" => 1
rule vs = "s" => "s"
rule vb = "b" => true
`
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

func fieldCheckTypes() []string { return fieldCheckTypesOf(fieldCheckFields) }

func fieldCheckTypesOf(fields map[string]string) []string {
	var types []string
	for typ := range fields {
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

// TestConstructedFieldChecks checks which dynamically typed values a cast to each declared field
// type accepts, on every backend, and from a compiled file.
func TestConstructedFieldChecks(t *testing.T) {
	// The terminal model is the same grammar in the retired terminal type syntax, which
	// syntax.ParseLegacy reads as the text structs it means: it accepts the same values.
	for _, model := range []struct {
		name    string
		grammar func(string) string
		parse   func(string) (*grammar.Grammar, error)
		fields  map[string]string
	}{
		{"text struct", fieldCheckGrammar, parseTest, fieldCheckFields},
		{"terminal", fieldCheckGrammarLegacy, parseLegacyTest, fieldCheckFields},
	} {
		t.Run(model.name, func(t *testing.T) {
			for _, typ := range fieldCheckTypesOf(model.fields) {
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
			checked, err := Compile(g, Options{})
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
				accepted := strings.Contains(fields[typ], in[:1])
				var want string
				for _, unit := range []PositionUnit{CodePoints, Bytes} {
					for _, run := range []struct {
						name string
						prog *Program
						b    Backend
					}{
						{"closure", checked, Closure}, {"bytecode", checked, Bytecode}, {"iterative", checked, BytecodeIterative},
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
				name := fieldCheckValueName(value)
				msg := fmt.Sprintf("cannot cast %s to %s", name, checked.typed.fields["S"][0].t)
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

// TestConstructedFieldChecksAreSelective checks that a constructed field is never checked when it
// runs: a value statically known to fit needs nothing, a value of an unknown type is rejected
// when the grammar is compiled, and a cast is the only check, one ECAST for each cast.
func TestConstructedFieldChecksAreSelective(t *testing.T) {
	const decls = `type Int = { Text string }
type S = { Static Int; Var int; Dynamic Int; Elements []Int; Union *Int; Untyped _; Empty []Int; Joined []Int }
var x int
rule item Int = "i" => Int{Text: "i"}
rule val _ = "v" => list()
`
	const body = "rule main = {x := 1} a:item d:val l:item* o:item? => S{Static: $a, Var: x, Dynamic: %s, Elements: $l.Value, Union: $o, Untyped: x, Empty: filter(nil, (e) => true), Joined: concat(nil, $l.Value)}\n"
	if got := compileMessage(t, decls+fmt.Sprintf(body, "$d")); !strings.Contains(got, "cannot use _ as Int in field Dynamic of S: the type of the value is not known; convert it with a cast, as in value.(Int)") {
		t.Fatalf("unknown value: %q", got)
	}
	src := decls + fmt.Sprintf(body, "$d.(Int)")
	prog := compile(t, src)
	m := prog.Module()
	if v := m.instructionSetVersion(); v != isaVersion {
		t.Fatalf("instruction set %d", v)
	}
	if d := m.Disassemble(); strings.Count(d, "ECAST") != 1 || !strings.Contains(d, `ECAST      "Int"`) {
		t.Fatalf("disassembly:\n%s", d)
	}
	plain := compile(t, `type Int = { Text string }
type S = { Static Int }
rule main = a:item => S{Static: $a}
rule item Int = "i" => Int{Text: "i"}
`)
	if v := plain.Module().instructionSetVersion(); v != isaVersion {
		t.Fatalf("module instruction set %d", v)
	}
	if d := plain.Module().Disassemble(); strings.Contains(d, "ECAST") {
		t.Fatalf("a statically typed field is checked:\n%s", d)
	}
	if got := resultWith(prog, "iv", ParseOptions{Backend: Bytecode}); !strings.Contains(got, "cannot cast []_ to Int") {
		t.Fatalf("dynamic list: %s", got)
	}
	// The recognizer keeps the same casts as its program.
	pred := compile(t, `type Int = { Text string }
type S = { Static Int; Dynamic Int }
rule main = a:item d:val {S{Static: $a, Dynamic: $d.(Int)} != nil}
rule item Int = "i" => Int{Text: "i"}
rule val _ = "v"
`)
	rec, err := pred.recognizer()
	if err != nil {
		t.Fatal(err)
	}
	if d := rec.Module().Disassemble(); strings.Count(d, "ECAST") != 1 {
		t.Fatalf("recognizer disassembly:\n%s", d)
	}
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	code, err := generateCode(g, GenOptions{Package: "p", Start: "main", Types: true, Recognize: true})
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(code), "var fieldType"); n != 1 {
		t.Fatalf("generated %d field type variables", n)
	}
}

// TestCompiledFieldChecksValidation checks that loading rejects files of an earlier
// instruction set and cast type texts that do not name a known type.
func TestCompiledFieldChecksValidation(t *testing.T) {
	prog := compile(t, fieldCheckGrammar("[]*(Int | node[string])"))
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
	for _, text := range []string{"Other", "[]", "*(Int)", "Int |", "(Int | Match", "Int  | Match", "[]Int)", "", "terminal", "Missing"} {
		m.Strings = append(append([]string(nil), prog.Module().Strings...), text)
		m.Exprs = append([]Instr(nil), prog.Module().Exprs...)
		for i := range m.Exprs {
			if m.Exprs[i].Op == ECast {
				m.Exprs[i].A = int32(len(m.Strings) - 1)
			}
		}
		if err := validateModule(&m); err == nil || !strings.Contains(err.Error(), "invalid field type") {
			t.Errorf("type text %q: %v", text, err)
		}
	}
	for _, text := range []string{"Int", "*Int", "[]*(Int | node[string])", "*(Box | bool)", "[][]Int", "int | string", "error", "node[_]", "node[string] | error"} {
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
