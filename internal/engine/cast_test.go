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

const castRules = "type Num terminal\ntype P = { N Num }\nrule n Num = [0-9]+\nrule w = [a-z]+\nrule pair = n:n \"=\" => P{N: $n}\n"

func castCases() []genCase {
	return []genCase{
		{"terminal", "rule main = x:(n / w) $$ => $x.(Num)\n" + castRules, []string{"12", "ab", ""}},
		{"list", "rule main = x:(n+ / w) $$ => $x.([]Num)\n" + castRules, []string{"1", "ab", ""}},
		{"optional", "rule main = x:(n / w)? $$ => $x.(*Num)\n" + castRules, []string{"1", "", "ab"}},
		{"union", "rule main = x:(n / w / pair) $$ => $x.(Num | P)\n" + castRules, []string{"1", "1=", "ab"}},
		{"field", "rule main = x:(pair / w) $$ => $x.(P).N\n" + castRules, []string{"1=", "ab"}},
		{"predicate", "rule main = x:(n / w) ({$x.(Num) == $x} \"!\" / \"?\") $$\n" + castRules, []string{"1!", "a?", "a!", "1?"}},
		{"constructor", "rule main = x:(n / w) $$ => P{N: $x.(Num)}\n" + castRules, []string{"1", "a"}},
		{"any", "rule main = x:(n / w) $$ => $x.(any)\n" + castRules, []string{"1", "a"}},
		{"int", "rule main = x:w $$ {len(text($x)).(int) > 1} => $x\n" + castRules, []string{"ab", "a"}},
	}
}

func TestCastAcrossBackends(t *testing.T) {
	for _, fixture := range castCases() {
		t.Run(fixture.name, func(t *testing.T) {
			program := compile(t, fixture.src)
			for _, unit := range []Unit{CodePoints, Bytes} {
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

func TestCastValues(t *testing.T) {
	for _, tc := range []struct{ src, input, want string }{
		{"rule main = x:(n / w) $$ => $x.(Num)\n" + castRules, "12", `"type":"Num"`},
		{"rule main = x:(n / w) $$ => $x.(Num)\n" + castRules, "ab", "cannot cast List to Num"},
		{"rule main = x:(n+ / w) $$ => $x.([]Num)\n" + castRules, "ab", "cannot cast List to []Num"},
		{"rule main = x:(n / w)? $$ => $x.(*Num)\n" + castRules, "", "null"},
		{"rule main = x:(pair / w) $$ => $x.(P).N\n" + castRules, "1=", `"type":"Num"`},
		{"rule main = x:(n / w) ({$x.(Num) == $x} \"!\" / \"?\") $$\n" + castRules, "a?", `"text":"?"`},
	} {
		program := compile(t, tc.src)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			if got := resultJSON(program.ParseWith("main", tc.input, ParseOptions{Backend: backend})); !strings.Contains(got, tc.want) {
				t.Errorf("%s %q: %s, want %s", backend, tc.input, got, tc.want)
			}
		}
	}
}

func TestCastErrors(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`rule main = x:"a" => $x.(int)`, "cannot cast Match to int"},
		{`rule main = x:"a" => $x.(Nope)`, "Nope"},
		{"rule main = x:n => $x.(P)\n" + castRules, "cannot cast Num to P"},
		{"rule main = x:n => $x.(Num).N\n" + castRules, "N"},
		{`rule main = "a" => (1).(string)`, "cannot cast int to string"},
	} {
		g, err := syntax.Parse(tc.src)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		if _, err := Compile(g, Options{}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v, want %q", tc.src, err, tc.want)
		}
	}
	// Without type checking the cast still fails at run time.
	program := compile(t, "rule main = x:n => $x.(P)\n"+castRules, Options{NoTypeCheck: true})
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		_, err := program.ParseWith("main", "1", ParseOptions{Backend: backend})
		if err == nil || !strings.Contains(err.Error(), "cannot cast Num to P") {
			t.Fatalf("%s: %v", backend, err)
		}
	}
	// An unknown type is rejected without type checking too.
	g, err := syntax.Parse(`rule main = x:"a" => $x.(Nope)`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(g, Options{NoTypeCheck: true}); err == nil || !strings.Contains(err.Error(), "Nope") {
		t.Fatalf("unknown cast type without type checking: %v", err)
	}
}

func TestCastSyntax(t *testing.T) {
	g, err := syntax.Parse("type Foo = { A Match }\ntype Word terminal\nrule main = x:\"a\" => $x.( []Foo | *Word ).startPos\nrule one = \"a\" => (1).(int)")
	if err != nil {
		t.Fatal(err)
	}
	formatted := grammar.Format(g)
	for _, want := range []string{"$x.([]Foo | *Word).startPos", "(1).(int)"} {
		if !strings.Contains(formatted, want) {
			t.Fatalf("formatted lacks %q:\n%s", want, formatted)
		}
	}
	if _, err := syntax.Parse(formatted); err != nil {
		t.Fatalf("formatted grammar does not parse: %v", err)
	}
	member := g.Rules()[0].Action.(*grammar.Member)
	if cast, ok := member.X.(*grammar.Cast); !ok || member.Name != "startPos" {
		t.Fatalf("cast %#v", member)
	} else if _, ok := cast.Type.(*grammar.UnionType); !ok {
		t.Fatalf("cast type %#v", cast.Type)
	}
	if data, err := grammar.MarshalJSON(g); err != nil || !strings.Contains(string(data), "Cast") {
		t.Fatalf("JSON AST: %v %s", err, data)
	}

	program := compile(t, "rule main = x:(n / w) $$ => $x.(Num)\n"+castRules)
	// The AST survives a compiled file.
	data, err := program.MarshalBinaryWith("main", MarshalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := LoadProgram(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"1", "a"} {
		if got, want := resultJSON(loaded.ParseWith("main", input, ParseOptions{})), resultJSON(program.ParseWith("main", input, ParseOptions{})); got != want {
			t.Fatalf("loaded %q: %s / %s", input, got, want)
		}
	}
	data, err = program.MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
	if err != nil || data[len(compiledMagic)+1] != 12 {
		t.Fatalf("cast ISA: %v", err)
	}
	// A runtime that knows only capture access must not load casts.
	data[len(compiledMagic)+1] = 11
	binary.LittleEndian.PutUint32(data[len(data)-4:], crc32.ChecksumIEEE(data[:len(data)-4]))
	if _, _, err := LoadProgram(data, Options{}); err == nil || !strings.Contains(err.Error(), "instruction set version 12") {
		t.Fatalf("under-declared cast ISA accepted: %v", err)
	}
	// A cast to any checks nothing, so it needs no new instruction.
	data, err = compile(t, "rule main = x:(n / w) $$ => $x.(any)\n"+castRules).MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
	if err != nil || data[len(compiledMagic)+1] != 3 {
		t.Fatalf("cast to any ISA: %v %d", err, data[len(compiledMagic)+1])
	}
}

func TestCastGeneratedGoParity(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	testGeneratedParsersCorpus(t, goBin, castCases(), GenOptions{})
	testGeneratedTypesCorpus(t, goBin, castCases(), GenOptions{convertTypes: true})
}

func TestCastGeneratedTypeScriptParity(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	testGeneratedTSParsersCorpus(t, node, castCases(), GenOptions{})
}
