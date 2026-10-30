package gentest

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/ir/exec/core"
)

// SetGoFlags sets the GOFLAGS of the test process, which the go commands it runs inherit, for
// building generated programs: -trimpath, so that their builds in fresh temporary directories share
// the build cache, -mod=mod, so that the temporary modules that replace this module resolve it
// without a go.sum, and the tag of the Exec IR switch if the test binary was built with it
// (core.GoFlags), whether it was given as GOFLAGS or as go test -tags.
func SetGoFlags() {
	os.Setenv("GOFLAGS", core.GoFlags(strings.TrimSpace(os.Getenv("GOFLAGS")+" -trimpath -mod=mod")))
}

// CheckRuntimeTag fails the test unless a go command that inherits the environment of the test
// compiles the runtime with the printed compositions (runtime/call_exec.go) exactly when the Exec IR
// switch is on in the test binary, so that a run with the switch does not test generated parsers
// against the hand-written runtime.
func CheckRuntimeTag(t *testing.T) {
	t.Helper()
	cmd := exec.Command("go", "list", "-f", "{{join .GoFiles \" \"}}", "github.com/ornew/pego/runtime")
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	files := strings.Fields(string(out))
	if got := slices.Contains(files, "call_exec.go"); got != core.Enabled {
		t.Errorf("a go command of the test compiles runtime/call_exec.go: %v, want %v (GOFLAGS=%q)", got, core.Enabled, os.Getenv("GOFLAGS"))
	}
}
