package main

import (
	"go/build"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestReferenceListsPublicPackages checks that the API reference documents every public package of
// the root module: every importable non-main package outside internal directories that exports a
// name. Directories with a go.mod of their own (such as the ready-made parsers) are other modules.
func TestReferenceListsPublicPackages(t *testing.T) {
	const repo = ".."
	listed := map[string]bool{}
	for _, rp := range refPackages {
		listed[rp.dir] = true
	}
	var public []string
	err := filepath.WalkDir(repo, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(repo, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel != "." {
			name := d.Name()
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata" || name == "internal" || name == "node_modules" {
				return fs.SkipDir
			}
			if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
				return fs.SkipDir
			}
		}
		if exportsNames(t, repo, rel) {
			public = append(public, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(public, ".") {
		t.Fatal("the walk found no public package in the module root")
	}
	for _, dir := range public {
		if !listed[dir] {
			t.Errorf("public package %s is missing from refPackages", path.Join(modulePath, dir))
		}
	}
	for dir := range listed {
		if !slices.Contains(public, dir) {
			t.Errorf("refPackages lists %s, which is not a public package", dir)
		}
	}
}

// exportsNames reports whether dir holds a non-main package with an exported top-level name.
func exportsNames(t *testing.T, repo, dir string) bool {
	t.Helper()
	bp, err := build.ImportDir(filepath.Join(repo, filepath.FromSlash(dir)), build.ImportComment)
	if err != nil {
		if _, ok := err.(*build.NoGoError); ok {
			return false
		}
		t.Fatalf("%s: %v", dir, err)
	}
	if bp.Name == "main" {
		return false
	}
	_, _, pkg, err := loadPackage(repo, dir, path.Join(modulePath, dir))
	if err != nil {
		t.Fatalf("%s: %v", dir, err)
	}
	return len(pkg.Consts)+len(pkg.Vars)+len(pkg.Funcs)+len(pkg.Types) > 0
}
