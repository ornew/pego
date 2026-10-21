package engine

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

// TestSpecInstructionSetVersion checks that spec/bytecode.md states the instruction-set version the
// runtime writes and accepts.
func TestSpecInstructionSetVersion(t *testing.T) {
	data, err := os.ReadFile("../../spec/bytecode.md")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`\| Instruction-set version \| Unsigned integer for this module: (\d+)\.`).FindSubmatch(data)
	if m == nil {
		t.Fatal("spec/bytecode.md does not state the instruction-set version of a module")
	}
	if got, _ := strconv.Atoi(string(m[1])); got != isaVersion {
		t.Fatalf("spec/bytecode.md states instruction-set version %d; the runtime uses %d", got, isaVersion)
	}
}
