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
	"github.com/ornew/pego/internal/syntax"
)

func captureAccessCases() []gentest.Case {
	return []gentest.Case{
		{Name: "map elements", Src: `rule main = xs:(y:[a-z] [0-9])+ $$ => map($xs, (i) => i.$y)`, Inputs: []string{"a1b2", "a1", "", "a"}},
		{Name: "unmatched alternative", Src: `rule main = xs:(y:"a" / z:"b")+ $$ => filter($xs, (i) => i.$y != nil)`, Inputs: []string{"abab", "bb", ""}},
		{Name: "both captures", Src: `rule main = xs:(y:"a" z:"b")+ $$ => list($xs[0].$z, $xs[0].$y)`, Inputs: []string{"ab", "abab", "a", ""}},
		{Name: "predicate", Src: `rule main = xs:(y:"a"? "b")+ ({$xs[0].$y != nil} "!" / "?") $$`, Inputs: []string{"ab!", "b?", "b!", "ab?"}},
		{Name: "chained", Src: `rule main = xs:(ys:(z:"a" "b")+ "c")+ $$ => $xs[0].$ys[0].$z`, Inputs: []string{"abc", "ababc", "c", ""}},
		{Name: "indexed", Src: `rule main = xs:(y:[a-z] [0-9])+ $$ => $xs[len($xs) - 1].$y`, Inputs: []string{"a1b2", ""}},
		{Name: "struct field", Src: "type Out = { Y *node[string]; Z *node[string] }\nrule main = xs:(y:\"a\"? z:\"b\")+ $$ => Out{Y: $xs[0].$y, Z: $xs[0].$z}", Inputs: []string{"ab", "b", ""}},
		{Name: "sequence capture", Src: `rule main = x:(y:"a" "b") $$ => $x.$y`, Inputs: []string{"ab", "a", ""}},
		{Name: "struct list", Src: "type Out = { Ys []node[string] }\nrule main = xs:(-\",\" y:[a-z])* $$ => Out{Ys: map($xs, (i) => i.$y)}", Inputs: []string{",a,b", "", ",a,"}},
	}
}

func TestCaptureAccessAcrossBackends(t *testing.T) {
	t.Parallel()
	for _, fixture := range captureAccessCases() {
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

// A value of a terminal type was a string node, with the members of a node. pego fix rewrites the
// terminal type to a named string node, which keeps those members.
func TestNodeMembersOfTerminalTypes(t *testing.T) {
	t.Parallel()
	fixed, err := syntax.Fix(`type Num terminal
type Out = { S int; E int; V string }
rule main = "-" n:num $$ => Out{S: $n.Start, E: $n.End, V: $n.Value}
rule num Num = [0-9]+ #atomic`)
	if err != nil {
		t.Fatal(err)
	}
	program := mustCompile(t, fixed)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		n, err := program.ParseWith("main", "-12", ParseOptions{Backend: backend})
		if err != nil || n.String() != `Out{E: 3, S: 1, V: "12"}` {
			t.Fatalf("%s: %v %v\n%s", backend, n, err, fixed)
		}
	}
}

// A text struct value reads its text through its declared field.
func TestTextStructFieldRead(t *testing.T) {
	t.Parallel()
	program := mustCompile(t, `type Num = { Text string }
type Out = { V string; N int }
rule main = "-" n:num $$ => Out{V: $n.Text, N: len($n.Text)}
rule num Num = t:([0-9]+ #atomic) => Num{Text: text($t)}`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		n, err := program.ParseWith("main", "-12", ParseOptions{Backend: backend})
		if err != nil || n.String() != `Out{N: 2, V: "12"}` {
			t.Fatalf("%s: %v %v", backend, n, err)
		}
	}
}

// x.$y reads capture y of node x and projects a repetition. The earlier x.y, which still reaches
// the engine from JSON grammars, reads a field: on a node it is rejected with a pointer to .$y.
func TestCaptureAccessMatchesField(t *testing.T) {
	t.Parallel()
	access := mustCompile(t, `rule main = xs:(-"," y:[a-z])* $$ => map($xs, (i) => i.$y)`)
	if a := compile.RuleProjections(access.Static(), access.RuleList[0]); a["xs"] != "y" {
		t.Fatalf("projection: %v", a)
	}
	g, err := parseLegacyTest(`rule main = xs:(-"," y:[a-z])* $$ => map($xs, (i) => i.y)`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(g, compile.Options{}); err == nil || !strings.Contains(err.Error(), "read the capture with .$y") {
		t.Fatalf("field access on a node: %v", err)
	}
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for input, want := range map[string]string{",a,b": `["a" "b"]`, "": `[]`, ",a,": "error"} {
			n, err := access.ParseWith("main", input, ParseOptions{Backend: backend})
			got := "error"
			if err == nil {
				got = n.String()
			}
			if got != want {
				t.Fatalf("%s %q: %s, want %s", backend, input, got, want)
			}
		}
	}
}

func TestCaptureAccessErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, want string }{
		{`rule main = x:(y:"a" "b") => $x.$z`, "has no capture z"},
		{"type T = { A node[string] }\nrule main = x:t => $x.$A\nrule t = a:\"a\" => T{A: $a}", "T has no capture A"},
		{`rule main = x:(y:"a" "b")? => $x.$y`, "the value may be nil"},
		{`rule main = "a" => (1).$y`, "int has no capture y"},
		{`rule main = x:((y:"a" "b") / (z:"c" "d")) => $x.$y`, "has no capture y"},
	} {
		g, err := parseTest(tc.src)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		if _, err := Compile(g, compile.Options{}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v, want %q", tc.src, err, tc.want)
		}
	}
	// A struct value has no captures.
	if got := compileMessage(t, "type T = { A node[string] }\nrule main = x:t => $x.$A\nrule t = a:\"a\" => T{A: $a}"); !strings.Contains(got, "T has no capture A") {
		t.Fatalf("got %q", got)
	}
}

func TestCaptureAccessSyntax(t *testing.T) {
	t.Parallel()
	g, err := parseTest(`rule main = xs:(y:"a")+ => map($xs, (i) => i.$y.Start)`)
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
	program := mustCompile(t, `rule main = xs:(y:"a" "b")* $$ => $xs[0].$y`)
	data, err := program.MarshalBinary("main")
	if err != nil || data[isaOffset] != bytecode.ISAVersion {
		t.Fatalf("capture access ISA: %v", err)
	}
	// A runtime that knows only an earlier instruction set must not load it.
	data[isaOffset] = bytecode.ISAVersion - 1
	binary.LittleEndian.PutUint32(data[len(data)-4:], crc32.ChecksumIEEE(data[:len(data)-4]))
	if _, _, err := LoadProgram(data); err == nil || !strings.Contains(err.Error(), "compile the grammar again") {
		t.Fatalf("under-declared capture access ISA accepted: %v", err)
	}
}

func TestCaptureAccessGeneratedGoParity(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testCaptureAccessGeneratedGoParity)
}

func testCaptureAccessGeneratedGoParity(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	gentest.GeneratedParsersCorpus(t, goBin, captureAccessCases(), gen.GenOptions{})
	gentest.GeneratedTypesCorpus(t, goBin, captureAccessCases(), gen.GenOptions{})
}

func TestCaptureAccessGeneratedTypeScriptParity(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	testGeneratedTSParsersCorpus(t, node, captureAccessCases(), gen.GenOptions{})
}
