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

func captureAccessCases() []genCase {
	return []genCase{
		{"map elements", `rule main = xs:(y:[a-z] [0-9])+ $$ => map($xs, (i) => i.$y)`, []string{"a1b2", "a1", "", "a"}},
		{"unmatched alternative", `rule main = xs:(y:"a" / z:"b")+ $$ => filter($xs, (i) => i.$y != nil)`, []string{"abab", "bb", ""}},
		{"both captures", `rule main = xs:(y:"a" z:"b")+ $$ => list($xs[0].$z, $xs[0].$y)`, []string{"ab", "abab", "a", ""}},
		{"predicate", `rule main = xs:(y:"a"? "b")+ ({$xs[0].$y != nil} "!" / "?") $$`, []string{"ab!", "b?", "b!", "ab?"}},
		{"chained", `rule main = xs:(ys:(z:"a" "b")+ "c")+ $$ => $xs[0].$ys[0].$z`, []string{"abc", "ababc", "c", ""}},
		{"indexed", `rule main = xs:(y:[a-z] [0-9])+ $$ => $xs[len($xs) - 1].$y`, []string{"a1b2", ""}},
		{"struct field", "type Out = { Y *Match; Z *Match }\nrule main = xs:(y:\"a\"? z:\"b\")+ $$ => Out{Y: $xs[0].$y, Z: $xs[0].$z}", []string{"ab", "b", ""}},
		{"struct list", "type Out = { Ys []Match }\nrule main = xs:(-\",\" y:[a-z])* $$ => Out{Ys: map($xs, (i) => i.$y)}", []string{",a,b", "", ",a,"}},
	}
}

func TestCaptureAccessAcrossBackends(t *testing.T) {
	for _, fixture := range captureAccessCases() {
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

// On capture nodes x.$y and x.y read the same capture, and both project a repetition.
func TestCaptureAccessMatchesField(t *testing.T) {
	access := compile(t, `rule main = xs:(-"," y:[a-z])* $$ => map($xs, (i) => i.$y)`)
	// x.y is earlier syntax, which still reaches the engine from JSON grammars.
	g, err := syntax.ParseLegacy(`rule main = xs:(-"," y:[a-z])* $$ => map($xs, (i) => i.y)`)
	if err != nil {
		t.Fatal(err)
	}
	field, err := Compile(g, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if a, f := ruleProjections(access, access.rules[0]), ruleProjections(field, field.rules[0]); a["xs"] != "y" || f["xs"] != "y" {
		t.Fatalf("projection: %v / %v", a, f)
	}
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, input := range []string{",a,b", "", ",a,"} {
			options := ParseOptions{Backend: backend}
			if got, want := resultJSON(access.ParseWith("main", input, options)), resultJSON(field.ParseWith("main", input, options)); got != want {
				t.Fatalf("%s %q: %s / %s", backend, input, got, want)
			}
		}
	}
}

func TestCaptureAccessErrors(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`rule main = x:(y:"a" "b") => $x.$z`, "has no capture z"},
		{"type T = { A Match }\nrule main = x:t => $x.$A\nrule t = a:\"a\" => T{A: $a}", "T has no capture A"},
		{`rule main = x:(y:"a" "b")? => $x.$y`, "the value may be nil"},
		{`rule main = "a" => (1).$y`, "int has no capture y"},
		{`rule main = x:(y:"a" "b") => $x.$y`, "Seq has no capture y"},
		{`rule main = x:((y:"a" "b") / (z:"c" "d")) => $x.$y`, "has no capture y"},
	} {
		g, err := syntax.Parse(tc.src)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		if _, err := Compile(g, Options{}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v, want %q", tc.src, err, tc.want)
		}
	}
	// A struct value has no captures at run time either.
	program := compile(t, "type T = { A Match }\nrule main = x:t => $x.$A\nrule t = a:\"a\" => T{A: $a}", Options{NoTypeCheck: true})
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		_, err := program.ParseWith("main", "a", ParseOptions{Backend: backend})
		if err == nil || !strings.Contains(err.Error(), "T has no capture A") {
			t.Fatalf("%s: %v", backend, err)
		}
	}
}

func TestCaptureAccessSyntax(t *testing.T) {
	g, err := syntax.Parse(`rule main = xs:(y:"a")+ => map($xs, (i) => i.$y.Start)`)
	if err != nil {
		t.Fatal(err)
	}
	if got := grammar.Format(g); !strings.Contains(got, "i.$y.Start") {
		t.Fatalf("formatted:\n%s", got)
	}
	body := g.Rules()[0].Action.(*grammar.Call).Args[1].(*grammar.Lambda).Body.(*grammar.Member)
	if inner, ok := body.X.(*grammar.Member); !ok || inner.Name != "$y" || body.Name != "Start" {
		t.Fatalf("access %#v", body)
	}
	program := compile(t, `rule main = xs:(y:"a" "b")* $$ => $xs[0].$y`)
	data, err := program.MarshalBinaryWith("main", MarshalOptions{OmitAST: true})
	if err != nil || data[len(compiledMagic)+1] != isaVersion {
		t.Fatalf("capture access ISA: %v", err)
	}
	// A runtime that knows only an earlier instruction set must not load it.
	data[len(compiledMagic)+1] = isaVersion - 1
	binary.LittleEndian.PutUint32(data[len(data)-4:], crc32.ChecksumIEEE(data[:len(data)-4]))
	if _, _, err := LoadProgram(data, Options{}); err == nil || !strings.Contains(err.Error(), "compile the grammar again") {
		t.Fatalf("under-declared capture access ISA accepted: %v", err)
	}
}

func TestCaptureAccessGeneratedGoParity(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	testGeneratedParsersCorpus(t, goBin, captureAccessCases(), GenOptions{})
	testGeneratedTypesCorpus(t, goBin, captureAccessCases(), GenOptions{})
}

func TestCaptureAccessGeneratedTypeScriptParity(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	testGeneratedTSParsersCorpus(t, node, captureAccessCases(), GenOptions{})
}
