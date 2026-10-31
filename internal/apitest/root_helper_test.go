package apitest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// chdirModuleRoot makes the root of module github.com/ornew/zeroparse the working directory of the test,
// so that it reads the repository's files by the paths they have there.
func chdirModuleRoot(t *testing.T) {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil && strings.HasPrefix(string(data), "module github.com/ornew/zeroparse\n") {
			t.Chdir(dir)
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("module github.com/ornew/zeroparse not found above the working directory")
		}
		dir = parent
	}
}
