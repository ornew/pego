package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteCompilerPairs(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"go", "tinygo"} {
		dir := filepath.Join(root, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		wasm := append([]byte{0, 'a', 's', 'm', 1, 0, 0, 0}, id...)
		runtime := []byte("runtime for " + id)
		binary, runtimePath := filepath.Join(dir, "build.wasm"), filepath.Join(root, id+"_wasm_exec.js")
		if err := os.WriteFile(binary, wasm, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(runtimePath, runtime, 0o644); err != nil {
			t.Fatal(err)
		}
		p, err := writePair(dir, binary, runtimePath, []byte("worker source"))
		if err != nil {
			t.Fatal(err)
		}
		for name, want := range map[string][]byte{p.Wasm: wasm, p.Runtime: runtime, "wasm_exec.js": runtime} {
			got, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("%s compiler pair changed %s: %v", id, name, err)
			}
		}
		b, err := os.ReadFile(filepath.Join(dir, "assets.json"))
		if err != nil {
			t.Fatal(err)
		}
		var m struct {
			Version int  `json:"version"`
			Current pair `json:"current"`
		}
		if err := json.Unmarshal(b, &m); err != nil || m.Version != 1 || m.Current != p {
			t.Fatalf("invalid compiler manifest: %v", err)
		}
		if _, err := os.Stat(binary); !os.IsNotExist(err) {
			t.Fatal("unhashed temporary binary remained")
		}
	}
}

func TestWritePairRejectsInvalidArtifacts(t *testing.T) {
	root := t.TempDir()
	binary, runtime := filepath.Join(root, "build.wasm"), filepath.Join(root, "runtime.js")
	for _, tc := range []struct{ wasm, runtime []byte }{
		{[]byte("not WASM"), []byte("runtime")},
		{[]byte{0, 'a', 's', 'm', 1, 0, 0, 0}, nil},
	} {
		if err := os.WriteFile(binary, tc.wasm, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(runtime, tc.runtime, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := writePair(root, binary, runtime, nil); err == nil {
			t.Fatal("invalid compiler pair accepted")
		}
	}
}
