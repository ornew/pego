package corpora

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tarGz(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "repo-abc/", Typeflag: tar.TypeDir, Mode: 0o755})
	for name, data := range files {
		tw.WriteHeader(&tar.Header{Name: "repo-abc/" + name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(data))})
		tw.Write([]byte(data))
	}
	tw.WriteHeader(&tar.Header{Name: "repo-abc/link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"})
	tw.WriteHeader(&tar.Header{Name: "repo-abc/../evil.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1})
	tw.Write([]byte("x"))
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func serve(t *testing.T, body []byte) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestFetch(t *testing.T) {
	t.Setenv("PEGO_CORPORA", t.TempDir())
	url := serve(t, tarGz(t, map[string]string{"a/one.txt": "1\n", "a/two.go": "2\n", "b/three.txt": "3\n", "top.md": "t\n"}))
	s := Source{Name: "demo", URL: url, Include: []string{"a/", "*.md"}}
	sum, n, err := Pin(s)
	if err != nil || n != 3 {
		t.Fatalf("Pin: %d files, %v", n, err)
	}
	s.SHA256 = sum
	if _, ok := Dir(s); ok {
		t.Fatal("found before fetching")
	}
	dir, err := Fetch(s)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := Dir(s); !ok || got != dir {
		t.Fatalf("Dir = %q, %v; Fetch returned %q", got, ok, dir)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a", "one.txt")); string(b) != "1\n" {
		t.Fatalf("one.txt = %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "b", "three.txt")); err == nil {
		t.Fatal("a file that is not included was kept")
	}
	if err := Verify(s); err != nil {
		t.Fatal(err)
	}
	// A changed file fails verification loudly.
	os.WriteFile(filepath.Join(dir, "a", "one.txt"), []byte("changed"), 0o644)
	if err := Verify(s); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("Verify after a change: %v", err)
	}
}

// A cached entry is trusted only after its files match the pinned digest: fetch replaces a corrupted or extended one.
func TestFetchRepairsCorruptedEntry(t *testing.T) {
	t.Setenv("PEGO_CORPORA", t.TempDir())
	s := Source{Name: "demo", URL: serve(t, tarGz(t, map[string]string{"a/one.txt": "1\n"})), Include: []string{"a/"}}
	sum, _, err := Pin(s)
	if err != nil {
		t.Fatal(err)
	}
	s.SHA256 = sum
	dir, err := Fetch(s)
	if err != nil {
		t.Fatal(err)
	}
	one := filepath.Join(dir, "a", "one.txt")
	for name, corrupt := range map[string]func(){
		"appended byte": func() { os.WriteFile(one, []byte("1\n "), 0o644) },
		"extra file":    func() { os.WriteFile(filepath.Join(dir, "a", "extra.txt"), []byte("x"), 0o644) },
		"removed file":  func() { os.Remove(one) },
	} {
		corrupt()
		if Verify(s) == nil {
			t.Fatalf("%s: Verify accepted a corrupted entry", name)
		}
		if _, err := Fetch(s); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := Verify(s); err != nil {
			t.Fatalf("%s: after Fetch: %v", name, err)
		}
	}
	// If the source no longer delivers the pinned data, a corrupted entry is an error and is not kept.
	s.URL = serve(t, tarGz(t, map[string]string{"a/one.txt": "other\n"}))
	os.WriteFile(one, []byte("bad"), 0o644)
	if _, err := Fetch(s); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("Fetch with a changed source: %v", err)
	}
	if _, ok := Dir(s); ok {
		t.Fatal("a corrupted entry stayed in the cache")
	}
}

func TestFetchRejectsWrongDigest(t *testing.T) {
	t.Setenv("PEGO_CORPORA", t.TempDir())
	s := Source{Name: "demo", URL: serve(t, tarGz(t, map[string]string{"a/one.txt": "1\n"})), Include: []string{"a/"}, SHA256: strings.Repeat("0", 64)}
	if _, err := Fetch(s); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("Fetch: %v", err)
	}
	if _, ok := Dir(s); ok {
		t.Fatal("a failed fetch left data in the cache")
	}
	entries, _ := os.ReadDir(filepath.Join(os.Getenv("PEGO_CORPORA"), "demo"))
	if len(entries) != 0 {
		t.Fatalf("a failed fetch left %d entries", len(entries))
	}
}

func TestFetchZip(t *testing.T) {
	t.Setenv("PEGO_CORPORA", t.TempDir())
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("mod@v1/p/x_test.go")
	w.Write([]byte("package p\n"))
	w, _ = zw.Create("mod@v1/q/y.go")
	w.Write([]byte("package q\n"))
	zw.Close()
	s := Source{Name: "z", URL: serve(t, buf.Bytes()), Format: "zip", Strip: "mod@v1/", Include: []string{"p/"}}
	sum, n, err := Pin(s)
	if err != nil || n != 1 {
		t.Fatalf("Pin: %d, %v", n, err)
	}
	s.SHA256 = sum
	if _, err := Fetch(s); err != nil {
		t.Fatal(err)
	}
}
