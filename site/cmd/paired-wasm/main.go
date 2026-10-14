// Command paired-wasm builds isolated experimental Go and TinyGo playground pairs.
// It never changes the normal site build or its Go default.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type pair struct {
	Wasm          string `json:"wasm"`
	WasmSHA256    string `json:"wasmSHA256"`
	Runtime       string `json:"runtime"`
	RuntimeSHA256 string `json:"runtimeSHA256"`
}

type variant struct {
	ID       string `json:"id"`
	Base     string `json:"base"`
	Compiler string `json:"compiler"`
	Wasm     string `json:"wasm"`
}

func main() {
	repo := flag.String("repo", "..", "PEGO source root")
	out := flag.String("out", "", "new comparison artifact directory (must not exist)")
	tinyVersion := flag.String("tinygo-version", "", "required exact TinyGo version, for example 0.43.0")
	flag.Parse()
	if *out == "" || *tinyVersion == "" {
		fmt.Fprintln(os.Stderr, "-out and -tinygo-version are required; this command is experimental")
		os.Exit(2)
	}
	if err := build(*repo, *out, *tinyVersion); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func command(repo string, env []string, tool string, args ...string) ([]byte, error) {
	cmd := exec.Command(tool, args...)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), env...)
	b, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w\n%s", tool, strings.Join(args, " "), err, b)
	}
	return bytes.TrimSpace(b), nil
}

func build(repo, out, tinyVersion string) error {
	out, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(out); !os.IsNotExist(err) {
		return fmt.Errorf("output must not exist: %s", out)
	}
	goVersion, err := command(repo, nil, "go", "env", "GOVERSION")
	if err != nil {
		return err
	}
	if string(goVersion) != "go1.27.1" {
		return fmt.Errorf("need Go 1.27.1, got %s", goVersion)
	}
	tiny, err := command(repo, nil, "tinygo", "version")
	if err != nil {
		return err
	}
	if !strings.HasPrefix(string(tiny), "tinygo version "+tinyVersion+" ") {
		return fmt.Errorf("need TinyGo %s, got %s", tinyVersion, tiny)
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	staged, err := os.MkdirTemp(filepath.Dir(out), ".pego-paired-wasm-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staged)
	worker, err := os.ReadFile(filepath.Join(repo, "site/static/playground/worker.js"))
	if err != nil {
		return err
	}
	var variants []variant
	for _, id := range []string{"go", "tinygo"} {
		dir := filepath.Join(staged, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		binary := filepath.Join(dir, "build.wasm")
		var root []byte
		var runtimePath, compiler string
		if id == "go" {
			_, err = command(repo, []string{"GOOS=js", "GOARCH=wasm"}, "go", "build", "-trimpath", "-ldflags=-s -w", "-o", binary, "./playground")
			if err == nil {
				root, err = command(repo, nil, "go", "env", "GOROOT")
			}
			runtimePath, compiler = filepath.Join(string(root), "lib/wasm/wasm_exec.js"), string(goVersion)
		} else {
			_, err = command(repo, nil, "tinygo", "build", "-target=wasm", "-no-debug", "-opt=z", "-o", binary, "./playground")
			if err == nil {
				root, err = command(repo, nil, "tinygo", "env", "TINYGOROOT")
			}
			runtimePath, compiler = filepath.Join(string(root), "targets/wasm_exec.js"), string(tiny)
		}
		if err != nil {
			return err
		}
		p, err := writePair(dir, binary, runtimePath, worker)
		if err != nil {
			return err
		}
		variants = append(variants, variant{ID: id, Base: id + "/", Compiler: compiler, Wasm: p.Wasm})
	}
	catalog := struct {
		Version  int       `json:"version"`
		Default  string    `json:"default"`
		Variants []variant `json:"variants"`
	}{1, "go", variants}
	b, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(staged, "variants.json"), b, 0o644); err != nil {
		return err
	}
	// Both builds must succeed before any artifacts appear at the requested path.
	return os.Rename(staged, out)
}

func readLimited(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("artifact exceeds %d bytes: %s", limit, path)
	}
	return b, nil
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func writePair(dir, binary, runtimePath string, worker []byte) (pair, error) {
	wasm, err := readLimited(binary, 64<<20)
	if err != nil {
		return pair{}, err
	}
	runtime, err := readLimited(runtimePath, 1<<20)
	if err != nil {
		return pair{}, err
	}
	if !bytes.HasPrefix(wasm, []byte{0, 'a', 's', 'm', 1, 0, 0, 0}) || len(runtime) == 0 {
		return pair{}, errors.New("invalid WASM/runtime pair")
	}
	w, r := digest(wasm), digest(runtime)
	p := pair{"wasm/pego-" + w[:16] + ".wasm", w, "runtime/wasm_exec-" + r[:16] + ".js", r}
	manifest, err := json.Marshal(struct {
		Version int  `json:"version"`
		Current pair `json:"current"`
	}{1, p})
	if err != nil {
		return pair{}, err
	}
	for name, b := range map[string][]byte{p.Wasm: wasm, p.Runtime: runtime, "wasm_exec.js": runtime, "assets.json": manifest, "worker.js": worker} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return pair{}, err
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			return pair{}, err
		}
	}
	return p, os.Remove(binary)
}
