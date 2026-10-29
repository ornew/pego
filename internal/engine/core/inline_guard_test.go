package core

import (
	"os/exec"
	"strings"
	"testing"
)

// TestStrictHotHelpersInline keeps the helpers that every parse runs inlinable. They sit at or near
// the compiler's inlining budget, so a branch added to one of them silently costs strict parses time.
func TestStrictHotHelpersInline(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the compiler")
	}
	out, err := exec.Command("go", "build", "-gcflags=-m=2", "-o", "/dev/null", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	for _, name := range []string{"(*Parser).Apply", "(*Parser).Mark"} {
		if !strings.Contains(string(out), "can inline "+name+" with cost") {
			t.Errorf("%s is no longer inlinable", name)
		}
	}
}
