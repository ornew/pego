package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/bytecode"
	"github.com/ornew/pego/internal/gentest"
)

// TestModuleCompilesCorpus checks that every test grammar compiles to bytecode and that all
// branch targets are within the instruction range.
func TestModuleCompilesCorpus(t *testing.T) {
	t.Parallel()
	for _, c := range gentest.GenCorpus(t) {
		m := mustCompile(t, c.Src).Module()
		for i, in := range m.Code {
			switch in.Op {
			case bytecode.OpJump, bytecode.OpChoice, bytecode.OpCommit, bytecode.OpIter, bytecode.OpNext, bytecode.OpLook, bytecode.OpRecover, bytecode.OpEndRecover:
				if in.A < 0 || int(in.A) > len(m.Code) {
					t.Errorf("%s: code %d: jump to %d", c.Name, i, in.A)
				}
			case bytecode.OpGuard:
				if in.C <= int32(i) || int(in.C) > len(m.Code) {
					t.Errorf("%s: code %d: guard jumps to %d", c.Name, i, in.C)
				}
			}
		}
		if !strings.Contains(m.Disassemble(), "RETURN") {
			t.Errorf("%s: no RETURN", c.Name)
		}
	}
}
