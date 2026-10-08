package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestWasmSmoke builds the playground for WebAssembly and the pego command, and runs
// testdata/smoke.mjs in Node, which compares the WebAssembly API with the command on every example of
// examples.txt. It is skipped when node is not installed and in -short mode.
func TestWasmSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("builds WebAssembly; skipped in -short mode")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	goTool := filepath.Join(goEnv(t, "GOROOT"), "bin", "go")
	dir := t.TempDir()
	wasm := filepath.Join(dir, "pego.wasm")
	cli := filepath.Join(dir, "pego")

	build := exec.Command(goTool, "build", "-o", wasm, ".")
	build.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building pego.wasm: %v\n%s", err, out)
	}
	if out, err := exec.Command(goTool, "build", "-o", cli, "../cmd/pego").CombinedOutput(); err != nil {
		t.Fatalf("building pego: %v\n%s", err, out)
	}
	wasmExec := filepath.Join(goEnv(t, "GOROOT"), "lib", "wasm", "wasm_exec.js")
	if _, err := os.Stat(wasmExec); err != nil {
		t.Fatalf("wasm_exec.js: %v", err)
	}
	repo, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "testdata/smoke.mjs", "--wasm", wasm, "--wasm-exec", wasmExec, "--cli", cli, "--repo", repo)
	out, err := cmd.CombinedOutput()
	t.Logf("%s", out)
	if err != nil {
		t.Fatalf("smoke test failed: %v", err)
	}
}

func goEnv(t *testing.T, name string) string {
	t.Helper()
	out, err := exec.Command("go", "env", name).Output()
	if err != nil {
		t.Fatalf("go env %s: %v", name, err)
	}
	return strings.TrimSpace(string(out))
}
