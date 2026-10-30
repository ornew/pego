package core

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// unswitched lists the files that set GOFLAGS for a go command that builds no runtime.
var unswitched = map[string]bool{
	"internal/ir/exec/execgo/print_test.go": true, // the printer's test program
}

// untagged lists the files of the module whose go commands build packages that have no file
// under the tag of the switch, and so pass GOFLAGS through unchanged.
var untagged = map[string]bool{
	"internal/engine/core/inline_guard_test.go": true, // the inlining of the engine's helpers
}

// goCommand matches a go command with a literal subcommand (group 1) or with arguments that are
// not literal (group 2).
var goCommand = regexp.MustCompile(`(?:"go"|goBin), "(\w+)"|Command\((?:"go"|goBin), ([^"\s])`)

// building lists the go subcommands that compile packages.
var building = []string{"build", "install", "run", "test", "vet"}

// TestGoFlagsEverywhere checks that every Go file of the module that sets GOFLAGS for a go command
// sets it through GoFlags, and that every go command that builds sets it so unless it inherits it
// (inherits), so that a run with the Exec IR switch builds generated parsers with the switch's
// runtime.
func TestGoFlagsEverywhere(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..")
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatal(err)
	}
	checked, commands := 0, 0
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
		lines := strings.Split(string(src), "\n")
		dir := filepath.ToSlash(filepath.Dir(rel))
		for i, line := range lines {
			if !untagged[filepath.ToSlash(rel)] && !inherits(t, root, dir) && runsBuild(line) {
				if !slices.ContainsFunc(statement(lines, i), func(l string) bool { return strings.Contains(l, "GoFlags(") }) {
					t.Errorf("%s:%d runs a go command that builds without the GOFLAGS of core.GoFlags: %s", rel, i+1, strings.TrimSpace(line))
				}
				commands++
			}
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
	if commands < 6 {
		t.Errorf("found %d go commands that build outside the packages that call SetGoFlags", commands)
	}
}

// runsBuild reports whether a line starts a go command that compiles packages: one with a building
// subcommand, or one whose arguments are not literal.
func runsBuild(line string) bool {
	for _, m := range goCommand.FindAllStringSubmatch(line, -1) {
		if m[2] != "" || slices.Contains(building, m[1]) {
			return true
		}
	}
	return false
}

// statement returns the lines from line i to the one that runs the command.
func statement(lines []string, i int) []string {
	for j := i; j < len(lines) && j < i+8; j++ {
		if strings.Contains(lines[j], "Run()") || strings.Contains(lines[j], "Output()") || strings.Contains(lines[j], "Start()") {
			return lines[i : j+1]
		}
	}
	return lines[i:min(i+8, len(lines))]
}

// inheritsFrom caches inherits by directory.
var inheritsFrom = map[string]bool{}

// inherits reports whether the go commands of the files in dir, relative to the module root, need
// no GoFlags of their own: in a package whose tests call gentest.SetGoFlags, they inherit its
// GOFLAGS, as do the helpers of package gentest, which run in those tests; a nested module (with
// its own go.mod) never runs with the switch.
func inherits(t *testing.T, root, dir string) bool {
	if v, ok := inheritsFrom[dir]; ok {
		return v
	}
	v := dir == "internal/gentest"
	for d := dir; d != "."; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(root, d, "go.mod")); err == nil {
			v = true
		}
	}
	files, err := filepath.Glob(filepath.Join(root, dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(src), "SetGoFlags()") {
			v = true
		}
	}
	inheritsFrom[dir] = v
	return v
}

// TestTagFlags checks that TagFlags adds the tag of the printed runtime to the tags that the go
// command uses, keeping the others.
func TestTagFlags(t *testing.T) {
	for _, c := range []struct{ flags, want string }{
		{"", "-tags=" + RuntimeTag},
		{"-trimpath", "-trimpath -tags=" + RuntimeTag},
		{"-tags=foo", "-tags=foo," + RuntimeTag},
		{"-trimpath -tags=foo -mod=mod", "-trimpath -tags=foo," + RuntimeTag + " -mod=mod"},
		{"--tags=foo,bar", "-tags=foo,bar," + RuntimeTag},
		{"-tags=", "-tags=" + RuntimeTag},
		{"-tags=foo -tags=bar", "-tags=foo -tags=bar," + RuntimeTag},
		{"-tags=" + RuntimeTag + ",foo", "-tags=" + RuntimeTag + ",foo"},
		{"-tags=foo," + RuntimeTag, "-tags=foo," + RuntimeTag},
		{"-tags=" + RuntimeTag, "-tags=" + RuntimeTag},
		{"-tagsx=foo", "-tagsx=foo -tags=" + RuntimeTag},
	} {
		if got := TagFlags(c.flags); got != c.want {
			t.Errorf("TagFlags(%q) = %q, want %q", c.flags, got, c.want)
		}
	}
}
