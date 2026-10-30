package engine

import (
	"encoding/binary"
	"hash/crc32"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/bytecode"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/syntax"
)

func TestCaptureAccessAcrossBackends(t *testing.T) {
	t.Parallel()
	for _, fixture := range gentest.CaptureAccessCases() {
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
