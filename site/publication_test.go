package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakePublicationHost models an atomic publication authority, not a production
// alias check followed by a separately mutable promotion request.
type fakePublicationHost struct {
	t         *testing.T
	server    *httptest.Server
	deploys   map[string]string
	active    string
	calls     int
	beforeCAS func()
	fail      error
}

func newPublicationHost(t *testing.T, active string, deploys map[string]string) *fakePublicationHost {
	h := &fakePublicationHost{t: t, active: active, deploys: deploys}
	h.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)
		id := parts[0]
		if id == "production" {
			id = h.active
		}
		dir, exists := h.deploys[id]
		if !exists || len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		r.URL.Path = "/" + parts[1]
		http.FileServer(http.Dir(dir)).ServeHTTP(w, r)
	}))
	t.Cleanup(h.server.Close)
	return h
}

func (h *fakePublicationHost) Current(context.Context) (publishedDeployment, error) {
	if h.active == "" {
		return publishedDeployment{}, nil
	}
	return publishedDeployment{ID: h.active, URL: h.server.URL + "/" + h.active}, nil
}

func (h *fakePublicationHost) CompareAndSwap(_ context.Context, expected, staged string) error {
	h.calls++
	if h.beforeCAS != nil {
		hook := h.beforeCAS
		h.beforeCAS = nil
		hook()
	}
	if h.active != expected {
		return errPublicationConflict
	}
	if h.fail != nil {
		return h.fail
	}
	dir := h.t.TempDir()
	err := filepath.WalkDir(staged, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(staged, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return writeFile(filepath.Join(dir, rel), data)
	})
	if err != nil {
		return err
	}
	id := fmt.Sprintf("promoted-%d", h.calls)
	manifestPath := filepath.Join(dir, "playground/assets.json")
	b, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var m assetManifest
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	m.Source = h.server.URL + "/" + id
	b, err = json.Marshal(m)
	if err != nil {
		return err
	}
	if err := os.WriteFile(manifestPath, b, 0o644); err != nil {
		return err
	}
	h.deploys[id] = dir
	h.active = id
	return nil
}

func publicationSite(t *testing.T, current, previous *assetBytes) string {
	t.Helper()
	dir := t.TempDir()
	seedAssets(t, filepath.Join(dir, "playground"), current, previous, "")
	if err := writeFile(filepath.Join(dir, "index.html"), []byte("candidate page")); err != nil {
		t.Fatal(err)
	}
	return dir
}

func requirePublicationPair(t *testing.T, h *fakePublicationHost, current, previous *assetBytes) {
	t.Helper()
	dir := h.deploys[h.active]
	data, err := os.ReadFile(filepath.Join(dir, "playground/assets.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m assetManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m.Source != h.server.URL+"/"+h.active {
		t.Fatal("host failed to record its immutable staged source")
	}
	if m.Current != current.asset || (previous == nil && m.Previous != nil) || (previous != nil && (m.Previous == nil || *m.Previous != previous.asset)) {
		t.Fatalf("wrong publication pair: %+v", m)
	}
	stable, err := os.ReadFile(filepath.Join(dir, "playground/wasm_exec.js"))
	if err != nil {
		t.Fatal(err)
	}
	wantStable := current.runtime
	if previous != nil {
		wantStable = previous.runtime
	}
	if !bytes.Equal(stable, wantStable) {
		t.Fatal("stable compatibility runtime did not follow actual predecessor")
	}
	count := 1
	if previous != nil {
		count++
	}
	for _, pattern := range []string{"wasm/*.wasm", "runtime/*.js"} {
		files, err := filepath.Glob(filepath.Join(dir, "playground", pattern))
		if err != nil || len(files) != count {
			t.Fatalf("unbounded files %s: %v %v", pattern, files, err)
		}
	}
	for _, a := range []*assetBytes{current, previous} {
		if a == nil {
			continue
		}
		for path, want := range map[string][]byte{a.asset.Wasm: a.wasm, a.asset.Runtime: a.runtime} {
			// A page already open before promotion requests the production alias.
			resp, err := http.Get(h.server.URL + "/production/playground/" + path)
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || resp.StatusCode != 200 || !bytes.Equal(got, want) {
				t.Fatalf("old page asset %s changed: HTTP %d, %v", path, resp.StatusCode, err)
			}
		}
	}
}

func TestPublicationReselectsActualPredecessor(t *testing.T) {
	for _, mode := range []string{"queued", "manual-preview", "rollback"} {
		t.Run(mode, func(t *testing.T) {
			a, b, c := testAssets("A"), testAssets("B"), testAssets("C")
			candidate := publicationSite(t, c, a) // built before B was promoted
			h := newPublicationHost(t, "B", map[string]string{"A": publicationSite(t, a, nil), "B": publicationSite(t, b, a)})
			if mode == "queued" {
				h.active = "A"
				h.beforeCAS = func() { h.active = "B" }
			}
			if mode == "rollback" {
				candidate = h.deploys["A"]
				c = a
			}
			before, err := os.ReadFile(filepath.Join(candidate, "playground/assets.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err := promotePublication(context.Background(), h, candidate, 3); err != nil {
				t.Fatal(err)
			}
			requirePublicationPair(t, h, c, b)
			after, _ := os.ReadFile(filepath.Join(candidate, "playground/assets.json"))
			if !bytes.Equal(before, after) {
				t.Fatal("publication mutated candidate deployment")
			}
			if mode == "queued" && h.calls != 2 {
				t.Fatalf("expected conflicting promotion to be rebuilt: %d attempts", h.calls)
			}
		})
	}
}

func TestPublicationFailureDoesNotPromote(t *testing.T) {
	a, b := testAssets("A"), testAssets("B")
	for _, mode := range []string{"candidate", "predecessor", "upload", "cancelled", "conflict"} {
		t.Run(mode, func(t *testing.T) {
			candidate := publicationSite(t, b, nil)
			h := newPublicationHost(t, "A", map[string]string{"A": publicationSite(t, a, nil)})
			ctx := context.Background()
			switch mode {
			case "candidate":
				os.Remove(filepath.Join(candidate, "playground", b.asset.Runtime))
			case "predecessor":
				os.Remove(filepath.Join(h.deploys["A"], "playground", a.asset.Wasm))
			case "upload":
				h.fail = errors.New("upload failed")
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "conflict":
				h.fail = errPublicationConflict
			}
			if err := promotePublication(ctx, h, candidate, 2); err == nil {
				t.Fatal("failed publication succeeded")
			}
			if h.active != "A" {
				t.Fatal("failed publication changed production")
			}
			if mode == "conflict" && h.calls != 2 {
				t.Fatalf("retry bound ignored: %d", h.calls)
			}
		})
	}
}

func TestPublicationFirstAndSamePair(t *testing.T) {
	a := testAssets("A")
	for _, active := range []string{"", "A"} {
		t.Run("active="+active, func(t *testing.T) {
			candidate := publicationSite(t, a, nil)
			h := newPublicationHost(t, active, map[string]string{"A": publicationSite(t, a, nil)})
			if err := promotePublication(context.Background(), h, candidate, 1); err != nil {
				t.Fatal(err)
			}
			requirePublicationPair(t, h, a, nil)
		})
	}
}

func TestPublicationRejectsSymlinkInput(t *testing.T) {
	a := testAssets("A")
	candidate := publicationSite(t, a, nil)
	if err := os.Symlink("index.html", filepath.Join(candidate, "alias.html")); err != nil {
		t.Fatal(err)
	}
	if staged, err := preparePublication(candidate, nil); err == nil {
		os.RemoveAll(staged)
		t.Fatal("publication followed a symbolic link")
	}
}
