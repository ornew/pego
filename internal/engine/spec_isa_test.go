package engine

import (
	"os"
	"regexp"
	"strconv"
	"testing"

	"github.com/ornew/pego/internal/bytecode"
)

// TestSpecInstructionSetVersion checks that spec/bytecode/README.md states the instruction-set version the
// runtime writes and accepts.
func TestSpecInstructionSetVersion(t *testing.T) {
	data, err := os.ReadFile("../../spec/bytecode/README.md")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`\| Instruction-set version \| Unsigned integer for this module: (\d+)\.`).FindSubmatch(data)
	if m == nil {
		t.Fatal("spec/bytecode/README.md does not state the instruction-set version of a module")
	}
	if got, _ := strconv.Atoi(string(m[1])); got != bytecode.ISAVersion {
		t.Fatalf("spec/bytecode/README.md states instruction-set version %d; the runtime uses %d", got, bytecode.ISAVersion)
	}
}
