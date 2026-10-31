package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSelectWasmBuild(t *testing.T) {
	const output = "/tmp/zp.wasm"
	for _, tc := range []struct {
		name string
		want wasmBuildSpec
	}{
		{"", wasmBuildSpec{"go", []string{"build", "-trimpath", "-ldflags=-s -w", "-o", output, "./cmd/playground"}, "GOROOT", "lib/wasm/wasm_exec.js"}},
		{"go", wasmBuildSpec{"go", []string{"build", "-trimpath", "-ldflags=-s -w", "-o", output, "./cmd/playground"}, "GOROOT", "lib/wasm/wasm_exec.js"}},
		{"tinygo", wasmBuildSpec{"tinygo", []string{"build", "-target=wasm", "-no-debug", "-o", output, "./cmd/playground"}, "TINYGOROOT", "targets/wasm_exec.js"}},
	} {
		got, err := selectWasmBuild(tc.name, output)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("toolchain %q: got %+v, %v; want %+v", tc.name, got, err, tc.want)
		}
	}
	if _, err := selectWasmBuild("other", output); err == nil {
		t.Fatal("unknown WASM toolchain accepted")
	}
}

func TestSelectWasmMode(t *testing.T) {
	for _, tc := range []struct{ input, want string }{{"", "go"}, {"go", "go"}, {"dual", "dual"}} {
		got, err := selectWasmMode(tc.input)
		if err != nil || got != tc.want {
			t.Errorf("mode %q: got %q, %v; want %q", tc.input, got, err, tc.want)
		}
	}
	if _, err := selectWasmMode("tinygo"); err == nil {
		t.Fatal("unsupported WASM mode accepted")
	}
}

func TestMixedToolchainAssetPairs(t *testing.T) {
	goPair := testAssets("Go compiler")
	tinygoPair := testAssets("TinyGo compiler")
	tinygoPair.runtime = []byte("// TinyGo runtime")
	tinygoPair.asset = describeAssets(tinygoPair.wasm, tinygoPair.runtime)
	dir := filepath.Join(t.TempDir(), "playground")
	seedAssets(t, dir, goPair, nil, "")
	if err := publishTinyGoAssets(dir, tinygoPair, nil); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "assets.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest assetManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Current != goPair.asset || manifest.TinyGoCurrent == nil || *manifest.TinyGoCurrent != tinygoPair.asset {
		t.Fatalf("mixed toolchain pairs: %+v", manifest)
	}
	for _, pair := range []*assetBytes{goPair, tinygoPair} {
		runtime, err := os.ReadFile(filepath.Join(dir, pair.asset.Runtime))
		if err != nil || !bytes.Equal(runtime, pair.runtime) {
			t.Fatalf("runtime for %s: %v", pair.asset.Wasm, err)
		}
	}
	stable, err := os.ReadFile(filepath.Join(dir, "wasm_exec.js"))
	if err != nil || !bytes.Equal(stable, goPair.runtime) {
		t.Fatalf("legacy runtime: %v", err)
	}
	server := httptest.NewServer(http.FileServer(http.Dir(filepath.Dir(dir))))
	defer server.Close()
	var priorTinyGo *assetBytes
	priorGo, err := readPreviousAssetsWithTinyGo(server.URL, &priorTinyGo)
	if err != nil || priorGo == nil || priorGo.asset != goPair.asset {
		t.Fatalf("published Go pair: %v", err)
	}
	if priorTinyGo == nil || priorTinyGo.asset != tinygoPair.asset {
		t.Fatalf("published TinyGo pair: %v", err)
	}

	goOnly := t.TempDir()
	seedAssets(t, goOnly, testAssets("next Go compiler"), goPair, "")
	if err := publishTinyGoAssets(goOnly, nil, tinygoPair); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(filepath.Join(goOnly, "assets.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest = assetManifest{}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.TinyGoCurrent != nil || manifest.TinyGoPrevious == nil || *manifest.TinyGoPrevious != tinygoPair.asset {
		t.Fatalf("Go-only build did not retain prior TinyGo pair: %+v", manifest)
	}
}
