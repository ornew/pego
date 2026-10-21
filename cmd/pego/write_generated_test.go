package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego"
)

func generatedFile(path string) pego.GeneratedFile {
	return pego.GeneratedFile{Path: path, Content: []byte(generatedHeader + " DO NOT EDIT.\n\npackage p\n")}
}

// TestWriteGeneratedSymlink checks that nothing is written through a symbolic link at an owned
// path or directory, and that the refusal names the link and leaves the directory untouched.
func TestWriteGeneratedSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need privileges")
	}
	files := []pego.GeneratedFile{generatedFile("parser.go"), generatedFile("types/types.go")}
	t.Run("file", func(t *testing.T) {
		dir, outside := t.TempDir(), t.TempDir()
		target := filepath.Join(outside, "target.go")
		if err := os.WriteFile(target, []byte(generatedHeader+" old\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(dir, "parser.go")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		err := writeGenerated(dir, files, pego.RuntimeEmbed, false)
		if err == nil || !strings.Contains(err.Error(), link) || !strings.Contains(err.Error(), "symbolic link") {
			t.Fatalf("error %v, want one naming the symbolic link %s", err, link)
		}
		if data, _ := os.ReadFile(target); string(data) != generatedHeader+" old\n" {
			t.Errorf("the target of the link was written: %q", data)
		}
		if _, err := os.Lstat(filepath.Join(dir, "types")); err == nil {
			t.Error("types was created before the refusal")
		}
	})
	t.Run("directory", func(t *testing.T) {
		dir, outside := t.TempDir(), t.TempDir()
		link := filepath.Join(dir, "types")
		if err := os.Symlink(outside, link); err != nil {
			t.Fatal(err)
		}
		err := writeGenerated(dir, files, pego.RuntimeEmbed, false)
		if err == nil || !strings.Contains(err.Error(), link) {
			t.Fatalf("error %v, want one naming the symbolic link %s", err, link)
		}
		if entries, _ := os.ReadDir(outside); len(entries) != 0 {
			t.Errorf("wrote through the directory link: %v", entries)
		}
		if _, err := os.Lstat(filepath.Join(dir, "parser.go")); err == nil {
			t.Error("parser.go was written before the refusal")
		}
	})
}

// TestWriteGeneratedChecksBeforeWriting checks that a file that cannot be read, found while
// scanning for stale files, stops the generation before the first write.
func TestWriteGeneratedChecksBeforeWriting(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("file permissions are not enforced")
	}
	dir := t.TempDir()
	rt := filepath.Join(dir, "internal", "runtime")
	if err := os.MkdirAll(rt, 0o755); err != nil {
		t.Fatal(err)
	}
	unreadable := filepath.Join(rt, "stale.go")
	if err := os.WriteFile(unreadable, []byte(generatedHeader+" old\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	old := []byte(generatedHeader + " old parser\n")
	parser := filepath.Join(dir, "parser.go")
	if err := os.WriteFile(parser, old, 0o644); err != nil {
		t.Fatal(err)
	}
	err := writeGenerated(dir, []pego.GeneratedFile{generatedFile("parser.go"), generatedFile("types/types.go")}, pego.RuntimeEmbed, false)
	if err == nil {
		t.Fatal("no error for an unreadable owned file")
	}
	if data, _ := os.ReadFile(parser); string(data) != string(old) {
		t.Errorf("parser.go was rewritten before the error: %q", data)
	}
	if _, err := os.Lstat(filepath.Join(dir, "types")); err == nil {
		t.Error("types was created before the error")
	}
}
