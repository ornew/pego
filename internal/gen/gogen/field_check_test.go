package gogen_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/bytecode"
	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

// TestConstructedFieldChecksAreSelective checks that a constructed field is never checked when it
// runs: a value statically known to fit needs nothing, a value of an unknown type is rejected
// when the grammar is compiled, and a cast is the only check, one ECAST for each cast.
func TestConstructedFieldChecksAreSelective(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testConstructedFieldChecksAreSelective)
}

func testConstructedFieldChecksAreSelective(t *testing.T) {
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
	prog := mustCompile(t, src)
	m := prog.Module()
	if v := m.InstructionSetVersion(); v != bytecode.ISAVersion {
		t.Fatalf("instruction set %d", v)
	}
	if d := m.Disassemble(); strings.Count(d, "ECAST") != 1 || !strings.Contains(d, `ECAST      "Int"`) {
		t.Fatalf("disassembly:\n%s", d)
	}
	plain := mustCompile(t, `type Int = { Text string }
type S = { Static Int }
rule main = a:item => S{Static: $a}
rule item Int = "i" => Int{Text: "i"}
`)
	if v := plain.Module().InstructionSetVersion(); v != bytecode.ISAVersion {
		t.Fatalf("module instruction set %d", v)
	}
	if d := plain.Module().Disassemble(); strings.Contains(d, "ECAST") {
		t.Fatalf("a statically typed field is checked:\n%s", d)
	}
	if got := resultWith(prog, "iv", engine.ParseOptions{Backend: engine.Bytecode}); !strings.Contains(got, "cannot cast []_ to Int") {
		t.Fatalf("dynamic list: %s", got)
	}
	// The recognizer keeps the same casts as its program.
	pred := mustCompile(t, `type Int = { Text string }
type S = { Static Int; Dynamic Int }
rule main = a:item d:val {S{Static: $a, Dynamic: $d.(Int)} != nil}
rule item Int = "i" => Int{Text: "i"}
rule val _ = "v"
`)
	rec, err := pred.Recognizer()
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
	code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "p", Start: "main", Types: true, Recognize: true})
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(code), "var fieldType"); n != 1 {
		t.Fatalf("generated %d field type variables", n)
	}
}
