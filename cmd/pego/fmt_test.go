package main

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFmtWriteFileContracts(t *testing.T) {
	t.Run("permissions", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Unix permission bits")
		}
		path := writeFile(t, "source.pego", messy)
		if err := os.Chmod(path, 0o751); err != nil {
			t.Fatal(err)
		}
		if err := formatFile(path, true, false, io.Discard); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o751 {
			t.Fatalf("permission bits changed: %v, %v", info, err)
		}
	})
	t.Run("read-only", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Getuid() == 0 {
			t.Skip("requires unprivileged Unix permissions")
		}
		path := writeFile(t, "source.pego", messy)
		if err := os.Chmod(path, 0o444); err != nil {
			t.Fatal(err)
		}
		if err := formatFile(path, true, false, io.Discard); err == nil {
			t.Fatal("read-only source was replaced")
		}
		if data, err := os.ReadFile(path); err != nil || string(data) != messy {
			t.Fatalf("read-only source changed: %q, %v", data, err)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		path := writeFile(t, "source.pego", messy)
		link := filepath.Join(filepath.Dir(path), "alias.pego")
		if err := os.Symlink(filepath.Base(path), link); err != nil {
			if runtime.GOOS == "windows" {
				t.Skipf("symlinks unavailable: %v", err)
			}
			t.Fatal(err)
		}
		if err := formatFile(link, true, false, io.Discard); err != nil {
			t.Fatal(err)
		}
		if dest, err := os.Readlink(link); err != nil || dest != filepath.Base(path) {
			t.Fatalf("symlink changed: %q, %v", dest, err)
		}
		if data, err := os.ReadFile(path); err != nil || string(data) != tidy {
			t.Fatalf("target not formatted: %q, %v", data, err)
		}
	})
	t.Run("symlink directory", func(t *testing.T) {
		path := writeFile(t, "source.pego", messy)
		dir := filepath.Dir(path)
		link := filepath.Join(t.TempDir(), "alias")
		if err := os.Symlink(dir, link); err != nil {
			if runtime.GOOS == "windows" {
				t.Skipf("symlinks unavailable: %v", err)
			}
			t.Fatal(err)
		}
		if err := formatFile(filepath.Join(link, "source.pego"), true, false, io.Discard); err != nil {
			t.Fatal(err)
		}
		if dest, err := os.Readlink(link); err != nil || dest != dir {
			t.Fatalf("directory symlink changed: %q, %v", dest, err)
		}
		if data, err := os.ReadFile(path); err != nil || string(data) != tidy {
			t.Fatalf("target not formatted: %q, %v", data, err)
		}
	})
	t.Run("symlink before parent component", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Getuid() == 0 {
			t.Skip("requires unprivileged Unix permissions")
		}
		path := writeFile(t, "source.pego", messy)
		child := filepath.Join(filepath.Dir(path), "child")
		if err := os.Mkdir(child, 0o755); err != nil {
			t.Fatal(err)
		}
		work := t.TempDir()
		link := filepath.Join(work, "link")
		if err := os.Symlink(child, link); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(work, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(work, 0o700) })
		// Do not use Join: cleaning ".." before resolving the symlink
		// would select a different directory from the filesystem.
		name := link + "/../source.pego"
		if err := formatFile(name, true, false, io.Discard); err != nil {
			t.Fatal(err)
		}
		if data, err := os.ReadFile(path); err != nil || string(data) != tidy {
			t.Fatalf("target not formatted: %q, %v", data, err)
		}
	})
	t.Run("hard link", func(t *testing.T) {
		path := writeFile(t, "source.pego", messy)
		link := filepath.Join(filepath.Dir(path), "alias.pego")
		if err := os.Link(path, link); err != nil {
			t.Fatal(err)
		}
		if err := formatFile(link, true, false, io.Discard); err != nil {
			t.Fatal(err)
		}
		for name, want := range map[string]string{path: messy, link: tidy} {
			if data, err := os.ReadFile(name); err != nil || string(data) != want {
				t.Fatalf("%s: got %q, %v; want %q", name, data, err, want)
			}
		}
	})
	t.Run("unchanged", func(t *testing.T) {
		path := writeFile(t, "source.pego", tidy)
		before, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := formatFile(path, true, false, io.Discard); err != nil {
			t.Fatal(err)
		}
		after, err := os.Stat(path)
		if err != nil || !os.SameFile(before, after) {
			t.Fatalf("unchanged source was replaced: %v", err)
		}
	})
}

func BenchmarkFmtWrite(b *testing.B) {
	for _, size := range []struct {
		name string
		n    int
	}{{"Short", 1}, {"Large", 128}} {
		b.Run(size.name, func(b *testing.B) {
			data := []byte(strings.Repeat("def  row =  \"a\"  $$\n", size.n))
			path := filepath.Join(b.TempDir(), "source.pego")
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				if err := os.WriteFile(path, data, 0o600); err != nil {
					b.Fatal(err)
				}
				if err := formatFile(path, true, false, io.Discard); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
