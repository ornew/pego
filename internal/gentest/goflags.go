package gentest

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ornew/zeroparse/internal/ir/exec/core"
)

// SetGoFlags sets the GOFLAGS of the test process, which the go commands it runs inherit, for
// building generated programs: -trimpath, so that their builds in fresh temporary directories share
// the build cache, -mod=mod, so that the temporary modules that replace this module resolve it
// without a go.sum, and the tag of the Exec IR switch if the test binary was built with it
// (core.GoFlags), whether it was given as GOFLAGS or as go test -tags.
func SetGoFlags() {
	os.Setenv("GOFLAGS", core.GoFlags(strings.TrimSpace(os.Getenv("GOFLAGS")+" -trimpath -mod=mod")))
}

// SetGoCache points the build cache of the go commands that the test process runs at a fresh
// temporary directory and returns a function that removes it. Every test builds a different
// generated program, so the shared build cache would only accumulate entries that are never reused;
// a cache private to the test process still shares the builds of the standard library and of this
// module among the tests of a package.
func SetGoCache() (remove func()) {
	dir, err := os.MkdirTemp("", "zp-gocache-")
	if err != nil {
		panic(err)
	}
	os.Setenv("GOCACHE", dir)
	return func() { os.RemoveAll(dir) }
}

// CheckRuntimeTag fails the test unless a go command that inherits the environment of the test
// compiles the runtime with the printed compositions (runtime/call_exec.go) exactly when the Exec IR
// switch is on in the test binary, so that a run with the switch does not test generated parsers
// against the hand-written runtime.
func CheckRuntimeTag(t *testing.T) {
	t.Helper()
	cmd := exec.Command("go", "list", "-f", "{{join .GoFiles \" \"}}", "github.com/ornew/zeroparse/runtime")
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

// execRuntimes holds the names of the tests that set UseExecRuntime.
var execRuntimes sync.Map

// UseExecRuntime makes the generated Go programs that t and its subtests build through GoFlags (as
// GeneratedParsersCorpus and GeneratedTypesCorpus do) use the runtime with the printed
// compositions (core.TagFlags), so that a run without the Exec IR switch compares them with the
// engine too.
func UseExecRuntime(t testing.TB) {
	name := t.Name()
	execRuntimes.Store(name, true)
	t.Cleanup(func() { execRuntimes.Delete(name) })
}

// GoFlags returns the GOFLAGS flags of a go command that builds a generated program of t: with
// the tag of the printed runtime if the switch is on or t uses it (UseExecRuntime).
func GoFlags(t testing.TB, flags string) string {
	if _, ok := byTest(&execRuntimes, t); ok {
		return core.TagFlags(flags)
	}
	return core.GoFlags(flags)
}
