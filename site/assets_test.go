package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testAssets(label string) *assetBytes {
	w := append([]byte{0, 'a', 's', 'm', 1, 0, 0, 0}, []byte(label)...)
	r := []byte("// Go runtime " + label)
	return &assetBytes{describeAssets(w, r), w, r}
}

func seedAssets(t *testing.T, dir string, a *assetBytes, previous *assetBytes, source string) {
	t.Helper()
	if err := writeFile(filepath.Join(dir, a.asset.Wasm), a.wasm); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(dir, "wasm_exec.js"), a.runtime); err != nil {
		t.Fatal(err)
	}
	if err := publishAssets(dir, a.asset.Wasm, source, previous); err != nil {
		t.Fatal(err)
	}
}

func TestAssetRetention(t *testing.T) {
	a, b, c := testAssets("A"), testAssets("B"), testAssets("C")
	root := t.TempDir()
	first := filepath.Join(root, "first")
	seedAssets(t, filepath.Join(first, "playground"), a, nil, "")
	server := httptest.NewServer(http.FileServer(http.Dir(first)))
	defer server.Close()
	prior, err := readPreviousAssets(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(root, "second")
	seedAssets(t, filepath.Join(second, "playground"), b, prior, "")
	for _, item := range []*assetBytes{a, b} {
		data, err := os.ReadFile(filepath.Join(second, "playground", item.asset.Wasm))
		if err != nil || !bytes.Equal(data, item.wasm) {
			t.Fatalf("retained WASM %s: %v", item.asset.Wasm, err)
		}
		data, err = os.ReadFile(filepath.Join(second, "playground", item.asset.Runtime))
		if err != nil || !bytes.Equal(data, item.runtime) {
			t.Fatalf("matching runtime: %v", err)
		}
	}
	legacy, _ := os.ReadFile(filepath.Join(second, "playground/wasm_exec.js"))
	if !bytes.Equal(legacy, a.runtime) {
		t.Fatal("legacy workers lost their previous runtime")
	}
	server2 := httptest.NewServer(http.FileServer(http.Dir(second)))
	defer server2.Close()
	prior, err = readPreviousAssets(server2.URL)
	if err != nil || prior.asset != b.asset {
		t.Fatalf("inherited wrong generation: %v", err)
	}
	third := filepath.Join(root, "third")
	seedAssets(t, filepath.Join(third, "playground"), c, prior, "")
	if _, err := os.Stat(filepath.Join(third, "playground", a.asset.Wasm)); !os.IsNotExist(err) {
		t.Fatal("old generations accumulated")
	}
	wasms, _ := filepath.Glob(filepath.Join(third, "playground/wasm/*.wasm"))
	if len(wasms) != 2 {
		t.Fatalf("WASM count %d", len(wasms))
	}
	same := filepath.Join(root, "same")
	seedAssets(t, filepath.Join(same, "playground"), b, prior, "")
	wasms, _ = filepath.Glob(filepath.Join(same, "playground/wasm/*.wasm"))
	var m assetManifest
	data, _ := os.ReadFile(filepath.Join(same, "playground/assets.json"))
	json.Unmarshal(data, &m)
	if len(wasms) != 1 || m.Previous != nil || m.Current != b.asset {
		t.Fatal("identical builds were not deduplicated")
	}
}

func TestPreviousAssetsPinnedSource(t *testing.T) {
	a := testAssets("pinned")
	dir := t.TempDir()
	seedAssets(t, filepath.Join(dir, "playground"), a, nil, "")
	pinned := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer pinned.Close()
	main := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/playground/assets.json" {
			t.Error("asset fetched from changing production alias")
			http.Error(w, "wrong deploy", 500)
			return
		}
		json.NewEncoder(w).Encode(assetManifest{Version: 1, Source: pinned.URL, Current: a.asset})
	}))
	defer main.Close()
	got, err := readPreviousAssets(main.URL)
	if err != nil || got.asset != a.asset {
		t.Fatalf("pinned fetch: %v", err)
	}
}

func TestPreviousAssetsBootstrap(t *testing.T) {
	a := testAssets("legacy")
	dir := t.TempDir()
	writeFile(filepath.Join(dir, "playground/index.html"), []byte(fmt.Sprintf(`<html data-wasm="../playground/%s">`, a.asset.Wasm)))
	writeFile(filepath.Join(dir, "playground", a.asset.Wasm), a.wasm)
	writeFile(filepath.Join(dir, "playground/wasm_exec.js"), a.runtime)
	s := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer s.Close()
	got, err := readPreviousAssets(s.URL)
	if err != nil || got.asset != a.asset {
		t.Fatalf("legacy bootstrap: %v", err)
	}
	empty := httptest.NewServer(http.NotFoundHandler())
	defer empty.Close()
	if got, err := readPreviousAssets(empty.URL); err != nil || got != nil {
		t.Fatalf("first deployment: %v", err)
	}
}

func TestPreviousAssetsFailures(t *testing.T) {
	a := testAssets("verified")
	for _, tc := range []string{"manifest-http", "invalid-json", "version", "trailing", "path", "wasm-hash", "wasm-header", "runtime-hash", "missing-wasm", "missing-runtime", "empty-runtime", "large-runtime", "large-manifest"} {
		t.Run(tc, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/playground/assets.json":
					m := assetManifest{Version: 1, Current: a.asset}
					switch tc {
					case "manifest-http":
						http.Error(w, "unavailable", 503)
						return
					case "invalid-json":
						fmt.Fprint(w, "oops")
						return
					case "large-manifest":
						fmt.Fprint(w, strings.Repeat(" ", 65537))
						return
					case "version":
						m.Version = 2
					case "path":
						m.Current.Wasm = "../private"
					}
					json.NewEncoder(w).Encode(m)
					if tc == "trailing" {
						fmt.Fprint(w, `{}`)
					}
				case "/playground/" + a.asset.Wasm:
					if tc == "missing-wasm" {
						http.NotFound(w, r)
						return
					}
					data := a.wasm
					if tc == "wasm-hash" {
						data = append(bytes.Clone(data), 1)
					}
					if tc == "wasm-header" {
						data = []byte("not wasm")
					}
					w.Write(data)
				case "/playground/" + a.asset.Runtime:
					if tc == "missing-runtime" {
						http.NotFound(w, r)
						return
					}
					if tc == "empty-runtime" {
						return
					}
					if tc == "large-runtime" {
						fmt.Fprint(w, strings.Repeat("x", (1<<20)+1))
						return
					}
					data := a.runtime
					if tc == "runtime-hash" {
						data = []byte("wrong runtime")
					}
					w.Write(data)
				default:
					t.Errorf("unexpected asset request %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer s.Close()
			if _, err := readPreviousAssets(s.URL); err == nil {
				t.Fatal("invalid prior deploy accepted")
			}
		})
	}
}

func TestRetentionFailurePreservesOutput(t *testing.T) {
	dir := t.TempDir()
	writeFile(filepath.Join(dir, marker), nil)
	writeFile(filepath.Join(dir, "sentinel"), []byte("old output"))
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "offline", 503) }))
	defer s.Close()
	if _, err := Build(Config{Out: dir, Wasm: true, PreviousSite: s.URL}); err == nil {
		t.Fatal("retention failure accepted")
	}
	if data, err := os.ReadFile(filepath.Join(dir, "sentinel")); err != nil || string(data) != "old output" {
		t.Fatal("failed retrieval destroyed previous local output")
	}
	if _, err := Build(Config{Out: dir, PreviousSite: s.URL}); err == nil {
		t.Fatal("retention without WASM accepted")
	}
}

func TestDeploymentURLs(t *testing.T) {
	for _, ctx := range []string{"production", "deploy-preview", "branch-deploy", "dev"} {
		env := map[string]string{"NETLIFY": "true", "CONTEXT": ctx, "URL": "https://production.test", "DEPLOY_URL": "https://immutable.test", "DEPLOY_PRIME_URL": "https://preview.test"}
		get := func(k string) string { return env[k] }
		previous, deploy := deploymentURLs(get)
		if ctx == "production" {
			if previous != env["URL"] || deploy != env["DEPLOY_URL"] {
				t.Fatal("wrong production URLs")
			}
		} else if previous != "" || deploy != "" {
			t.Fatal("preview inherited production retention")
		}
		env["NETLIFY_PREVIEW_SERVER"] = "true"
		previous, deploy = deploymentURLs(get)
		if previous != "" || deploy != "" {
			t.Fatal("preview server inherited production retention")
		}
	}
	if a, b := deploymentURLs(func(string) string { return "" }); a != "" || b != "" {
		t.Fatal("local build made network retention automatic")
	}
	for _, raw := range []string{"file:///tmp/site", "https://user:secret@site.test", "https://site.test/?token=secret", "/relative"} {
		if _, err := siteURL(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func BenchmarkPublishAssets(b *testing.B) {
	for _, retain := range []bool{false, true} {
		b.Run(fmt.Sprintf("retain=%v", retain), func(b *testing.B) {
			dir := b.TempDir()
			wasm := make([]byte, 8<<20)
			wasm[0] = 1
			runtime := make([]byte, 17000)
			current := describeAssets(wasm, runtime)
			writeFile(filepath.Join(dir, current.Wasm), wasm)
			var previous *assetBytes
			if retain {
				old := bytes.Clone(wasm)
				old[0] = 2
				previous = &assetBytes{describeAssets(old, runtime), old, runtime}
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(wasm)))
			b.ResetTimer()
			for b.Loop() {
				writeFile(filepath.Join(dir, "wasm_exec.js"), runtime)
				if err := publishAssets(dir, current.Wasm, "", previous); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
