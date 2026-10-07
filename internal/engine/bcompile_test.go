package engine

import (
	"strings"
	"testing"
)

func TestModuleDisassembly(t *testing.T) {
	prog := compile(t, `
type P struct { K Match, N int }
def main = k:key "=" v:@(?0-9)+ (";" / _) -> new P{K: $k, N: len($v) + 1}
def key = @(?a-z)+ #error(message="expected a key")`)
	got := prog.Module().Disassemble()
	want := `rules:
  0 main entry=0 scope=[k v] action=0 memo,seq,transient
  1 key entry=12 scope=[] memo,transient
code:
main:
     0  CALL       key min=0 keep=1
     1  CAPTURE    slot=0 pop
     2  STR        "=" build=0
     3  PUSHPOS    
     4  SCAN       "(?0-9)" min=1 max=-1
     5  ATOMIC     build=1
     6  CAPTURE    slot=1 pop
     7  CHOICE     -> 10
     8  STR        ";" build=0
     9  COMMIT     -> 11
    10  TOP        build=0
    11  RETURN     
key:
    12  PUSHPOS    
    13  LABEL      "expected a key"
    14  SCAN       "(?a-z)" min=1 max=-1
    15  ENDLABEL   
    16  ATOMIC     build=1
    17  RETURN     
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

// TestModuleCompilesCorpus checks that every test grammar compiles to bytecode and that all
// branch targets are within the instruction range.
func TestModuleCompilesCorpus(t *testing.T) {
	for _, c := range genCorpus(t) {
		m := compile(t, c.src).Module()
		for i, in := range m.Code {
			switch in.Op {
			case OpJump, OpChoice, OpCommit, OpIter, OpNext, OpLook, OpRecover, OpEndRecover:
				if in.A < 0 || int(in.A) > len(m.Code) {
					t.Errorf("%s: code %d: jump to %d", c.name, i, in.A)
				}
			}
		}
		if !strings.Contains(m.Disassemble(), "RETURN") {
			t.Errorf("%s: no RETURN", c.name)
		}
	}
}
