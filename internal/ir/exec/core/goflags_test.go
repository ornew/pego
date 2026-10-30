package core

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// unswitched lists the files that set GOFLAGS for a go command that builds no runtime.
var unswitched = map[string]bool{
	"internal/ir/exec/execgo/print_test.go": true, // the printer's test program
}

// TestGoFlagsEverywhere checks that every Go file of the module that sets GOFLAGS for a go command
// sets it through GoFlags, so that a run with the Exec IR switch builds generated parsers with the
// switch's runtime.
func TestGoFlagsEverywhere(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..")
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatal(err)
	}
	checked := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			if rel != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata" || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || unswitched[filepath.ToSlash(rel)] {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(src), "\n") {
			if (strings.Contains(line, `"GOFLAGS=`) || strings.Contains(line, `Setenv("GOFLAGS"`)) && !strings.Contains(line, "GoFlags(") {
				t.Errorf("%s:%d sets GOFLAGS without core.GoFlags: %s", rel, i+1, strings.TrimSpace(line))
			}
			if strings.Contains(line, "GOFLAGS") {
				checked++
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 20 {
		t.Errorf("found %d lines that name GOFLAGS", checked)
	}
}
