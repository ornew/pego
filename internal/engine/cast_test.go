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
	"github.com/ornew/pego/internal/bytecode"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

const castRules = "type Num = { Text string }\ntype P = { N Num }\nrule n Num = t:([0-9]+) #atomic => Num{Text: text($t)}\nrule w = [a-z]+\nrule pair = n:n \"=\" => P{N: $n}\n"

func castCases() []gentest.Case {
	return []gentest.Case{
		{Name: "terminal", Src: "rule main = x:(n / w) $$ => $x.(Num)\n" + castRules, Inputs: []string{"12", "ab", ""}},
		{Name: "list", Src: "rule main = x:(n+ / w) $$ => $x.([]Num)\n" + castRules, Inputs: []string{"1", "ab", ""}},
		{Name: "optional", Src: "rule main = x:(n / w)? $$ => $x.(*Num)\n" + castRules, Inputs: []string{"1", "", "ab"}},
		{Name: "union", Src: "rule main = x:(n / w / pair) $$ => $x.(Num | P)\n" + castRules, Inputs: []string{"1", "1=", "ab"}},
		{Name: "field", Src: "rule main = x:(pair / w) $$ => $x.(P).N\n" + castRules, Inputs: []string{"1=", "ab"}},
		{Name: "predicate", Src: "rule main = x:(n / w) ({$x.(Num) == $x} \"!\" / \"?\") $$\n" + castRules, Inputs: []string{"1!", "a?", "a!", "1?"}},
		{Name: "constructor", Src: "rule main = x:(n / w) $$ => P{N: $x.(Num)}\n" + castRules, Inputs: []string{"1", "a"}},
		{Name: "top", Src: "rule main = x:(n / w) $$ => $x.(_)\n" + castRules, Inputs: []string{"1", "a"}},
		// The error names where the action started, after a newline, in code points and bytes.
		{Name: "position", Src: "rule main = \"\\u00e9\\n\" r\nrule r = x:(n / w) $$ => $x.(Num)\n" + castRules, Inputs: []string{"\u00e9\n1", "\u00e9\nab"}},
		{Name: "int", Src: "rule main = x:w $$ {len(text($x)).(int) > 1} => $x\n" + castRules, Inputs: []string{"ab", "a"}},
	}
}

func TestCastAcrossBackends(t *testing.T) {
	t.Parallel()
	for _, fixture := range castCases() {
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

func TestCastValues(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, input, want string }{
		{"rule main = x:(n / w) $$ => $x.(Num)\n" + castRules, "12", `"type":"Num","start":0,"end":2,"fields":{"Text":"12"}`},
		{"rule main = x:(n / w) $$ => $x.(Num)\n" + castRules, "ab", "cannot cast node[[]_] to Num"},
		{"rule main = x:(n+ / w) $$ => $x.([]Num)\n" + castRules, "ab", "cannot cast node[[]_] to []Num"},
		{"rule main = x:(n / w)? $$ => $x.(*Num)\n" + castRules, "", "null"},
		{"rule main = x:(pair / w) $$ => $x.(P).N\n" + castRules, "1=", `"type":"Num","start":0,"end":1,"fields":{"Text":"1"}`},
		{"rule main = \"\\n\" r\nrule r = x:(n / w) $$ => $x.(Num)\n" + castRules, "\nab", "cannot cast node[[]_] to Num at input position 1 (line 2, column 1); cast at grammar position 2:29"},
		{"rule main = x:(n / w) ({$x.(Num) == $x} \"!\" / \"?\") $$\n" + castRules, "a?", `"value":"?"`},
	} {
		program := mustCompile(t, tc.src)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			if got := gentest.ResultJSON(program.ParseWith("main", tc.input, ParseOptions{Backend: backend})); !strings.Contains(got, tc.want) {
				t.Errorf("%s %q: %s, want %s", backend, tc.input, got, tc.want)
			}
		}
	}
}

func TestCastErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, want string }{
		{`rule main = x:"a" => $x.(int)`, "cannot cast node[string] to int"},
		{`rule main = x:"a" => $x.(Nope)`, "Nope"},
		{"rule main = x:n => $x.(P)\n" + castRules, "cannot cast Num to P"},
		{"rule main = x:n => $x.(Num).N\n" + castRules, "N"},
		{`rule main = "a" => (1).(string)`, "cannot cast int to string"},
	} {
		g, err := parseTest(tc.src)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		if _, err := Compile(g, compile.Options{}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v, want %q", tc.src, err, tc.want)
		}
	}
	// An unknown type is rejected.
	g, err := parseTest(`rule main = x:"a" => $x.(Nope)`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(g, compile.Options{}); err == nil || !strings.Contains(err.Error(), "Nope") {
		t.Fatalf("unknown cast type: %v", err)
	}
}

func TestCastSyntax(t *testing.T) {
	t.Parallel()
	g, err := parseTest("type Foo = { A node[string] }\ntype Word = { Text string }\nrule main = x:\"a\" => $x.( []Foo | *Word ).Start\nrule one = \"a\" => (1).(int)")
	if err != nil {
		t.Fatal(err)
	}
	formatted := grammar.Format(g)
	for _, want := range []string{"$x.([]Foo | *Word).Start", "(1).(int)"} {
		if !strings.Contains(formatted, want) {
			t.Fatalf("formatted lacks %q:\n%s", want, formatted)
		}
	}
	if _, err := parseTest(formatted); err != nil {
		t.Fatalf("formatted grammar does not parse: %v", err)
	}
	member := g.Rules()[0].Action.(*grammar.Member)
	if cast, ok := member.X.(*grammar.Cast); !ok || member.Name != "Start" {
		t.Fatalf("cast %#v", member)
	} else if _, ok := cast.Type.(*grammar.JoinType); !ok {
		t.Fatalf("cast type %#v", cast.Type)
	}
	if data, err := grammar.MarshalJSON(g); err != nil || !strings.Contains(string(data), "Cast") {
		t.Fatalf("JSON AST: %v %s", err, data)
	}

	program := mustCompile(t, "rule main = x:(n / w) $$ => $x.(Num)\n"+castRules)
	// A cast survives a compiled file.
	data, err := program.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := LoadProgram(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"1", "a"} {
		if got, want := gentest.ResultJSON(loaded.ParseWith("main", input, ParseOptions{})), gentest.ResultJSON(program.ParseWith("main", input, ParseOptions{})); got != want {
			t.Fatalf("loaded %q: %s / %s", input, got, want)
		}
	}
	data, err = program.MarshalBinary("main")
	if err != nil || data[isaOffset] != bytecode.ISAVersion {
		t.Fatalf("cast ISA: %v", err)
	}
	// A runtime that knows only an earlier instruction set must not load it.
	data[isaOffset] = bytecode.ISAVersion - 1
	binary.LittleEndian.PutUint32(data[len(data)-4:], crc32.ChecksumIEEE(data[:len(data)-4]))
	if _, _, err := LoadProgram(data); err == nil || !strings.Contains(err.Error(), "compile the grammar again") {
		t.Fatalf("under-declared cast ISA accepted: %v", err)
	}
	// A cast to any checks nothing; it uses the same instruction set.
	data, err = mustCompile(t, "rule main = x:(n / w) $$ => $x.(_)\n"+castRules).MarshalBinary("main")
	if err != nil || data[isaOffset] != bytecode.ISAVersion {
		t.Fatalf("cast to any ISA: %v %d", err, data[isaOffset])
	}
}

func TestCastGeneratedGoParity(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testCastGeneratedGoParity)
}

func testCastGeneratedGoParity(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	gentest.GeneratedParsersCorpus(t, goBin, castCases(), gen.GenOptions{})
	gentest.GeneratedParsersCorpus(t, goBin, castCases(), gen.GenOptions{SourceInfo: true})
	gentest.GeneratedTypesCorpus(t, goBin, castCases(), gen.GenOptions{ConvertTypes: true})
}

func TestCastGeneratedTypeScriptParity(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	testGeneratedTSParsersCorpus(t, node, castCases(), gen.GenOptions{})
	testGeneratedTSParsersCorpus(t, node, castCases(), gen.GenOptions{SourceInfo: true})
}
