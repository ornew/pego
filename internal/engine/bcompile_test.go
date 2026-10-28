package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/bytecode"
)

func TestModuleDisassembly(t *testing.T) {
	prog := compile(t, `type P = { K node[string]; N int }
rule main = k:key "=" v:[0-9]+ #atomic (";" / _) => P{K: $k, N: len($v) + 1}
rule key = [a-z]+ @error(message="expected a key") #atomic`)
	got := prog.Module().Disassemble()
	want := `rules:
  0 main entry=0 scope=[k v] action=0 memo,transient
  1 key entry=13 scope=[] memo,transient
code:
main:
     0  CALL       key min=0 keep=1
     1  CAPTURE    slot=0 pop
     2  STR        "=" build=0
     3  PUSHPOS    
     4  SCAN       "[0-9]" min=1 max=-1
     5  ATOMIC     build=1
     6  CAPTURE    slot=1 pop
     7  GUARD      ";" depth=0 -> 11
     8  CHOICE     -> 11
     9  STR        ";" build=0
    10  COMMIT     -> 12
    11  TOP        build=0
    12  RETURN     
key:
    13  PUSHPOS    
    14  LABEL      "expected a key"
    15  SCAN       "[a-z]" min=1 max=-1
    16  ENDLABEL   
    17  ATOMIC     build=1
    18  RETURN     
exprs:
main.action:
     0  ECAP       0
     1  ECAP       1
     2  ECALL      len argc=1
     3  EINT       1
     4  EBIN       "+"
     5  ENEW       P fields=[K N]
     6  ERET       
`
	if got != want {
		t.Errorf("got\n%s", got)
	}
}

func TestDiagnosticLabelDisassembly(t *testing.T) {
	module := compile(t, `rule main = "a" @error(message="explicit") / "b" @diagnostic(name="letter")`).Module()
	text := module.Disassemble()
	if !strings.Contains(text, `LABEL      "explicit"`+"\n") {
		t.Errorf("explicit error label changed:\n%s", text)
	}
	if !strings.Contains(text, `LABEL      "letter" mode=diagnostic`+"\n") {
		t.Errorf("diagnostic label mode missing:\n%s", text)
	}
}

// TestModuleCompilesCorpus checks that every test grammar compiles to bytecode and that all
// branch targets are within the instruction range.
func TestModuleCompilesCorpus(t *testing.T) {
	for _, c := range genCorpus(t) {
		m := compile(t, c.src).Module()
		for i, in := range m.Code {
			switch in.Op {
			case bytecode.OpJump, bytecode.OpChoice, bytecode.OpCommit, bytecode.OpIter, bytecode.OpNext, bytecode.OpLook, bytecode.OpRecover, bytecode.OpEndRecover:
				if in.A < 0 || int(in.A) > len(m.Code) {
					t.Errorf("%s: code %d: jump to %d", c.name, i, in.A)
				}
			case bytecode.OpGuard:
				if in.C <= int32(i) || int(in.C) > len(m.Code) {
					t.Errorf("%s: code %d: guard jumps to %d", c.name, i, in.C)
				}
			}
		}
		if !strings.Contains(m.Disassemble(), "RETURN") {
			t.Errorf("%s: no RETURN", c.name)
		}
	}
}
