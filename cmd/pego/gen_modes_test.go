package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// listGenerated returns the files below dir, relative to it and with slashes.
func listGenerated(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		out = append(out, filepath.ToSlash(rel))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

func hasRuntimeFiles(files []string) bool {
	for _, f := range files {
		if strings.HasPrefix(f, "internal/runtime/") {
			return true
		}
	}
	return false
}

// TestGenModesReplaceEachOther checks which files a generation in each runtime mode writes and
// removes: embed and import own parser.go, api.go and internal/runtime, while only owns
// internal/runtime alone and never touches the parser.
func TestGenModesReplaceEachOther(t *testing.T) {
	g := writeFile(t, "kv.pego", kv)
	gen := func(t *testing.T, dir string, args ...string) {
		t.Helper()
		if out, err := runCLI(t, "", append([]string{"gen", "-o", dir}, args...)...); err != nil || out != "" {
			t.Fatalf("gen %v: %q, %v", args, out, err)
		}
	}
	embed := []string{"-g", g, "-module", "example.com/kv", "-runtime", "embed"}
	imp := []string{"-g", g, "-module", "example.com/kv", "-runtime", "import"}
	only := []string{"-runtime", "only"}

	t.Run("embed then import", func(t *testing.T) {
		dir := t.TempDir()
		gen(t, dir, embed...)
		if files := listGenerated(t, dir); !hasRuntimeFiles(files) || !slices.Contains(files, "parser.go") || !slices.Contains(files, "api.go") {
			t.Fatalf("embed wrote %v", files)
		}
		gen(t, dir, imp...)
		if files := listGenerated(t, dir); !slices.Equal(files, []string{"api.go", "parser.go"}) {
			t.Errorf("import left %v, want api.go and parser.go only", files)
		}
		if _, err := os.Stat(filepath.Join(dir, "internal")); !os.IsNotExist(err) {
			t.Errorf("the emptied internal directory was kept: %v", err)
		}
	})
	t.Run("import then embed", func(t *testing.T) {
		dir := t.TempDir()
		gen(t, dir, imp...)
		if files := listGenerated(t, dir); hasRuntimeFiles(files) {
			t.Fatalf("import wrote %v", files)
		}
		gen(t, dir, embed...)
		if files := listGenerated(t, dir); !hasRuntimeFiles(files) || !slices.Contains(files, "parser.go") {
			t.Errorf("embed wrote %v", files)
		}
	})
	t.Run("types write and remove types/types.go", func(t *testing.T) {
		dir := t.TempDir()
		gen(t, dir, append([]string{"-types"}, imp...)...)
		if files := listGenerated(t, dir); !slices.Equal(files, []string{"api.go", "parser.go", "types/types.go"}) {
			t.Fatalf("-types wrote %v", files)
		}
		if data, err := os.ReadFile(filepath.Join(dir, "types", "types.go")); err != nil || !strings.HasPrefix(string(data), generatedHeader) || !strings.Contains(string(data), "package types\n") {
			t.Errorf("types/types.go: %v", err)
		}
		gen(t, dir, imp...)
		if files := listGenerated(t, dir); !slices.Equal(files, []string{"api.go", "parser.go"}) {
			t.Errorf("a generation without -types left %v", files)
		}
		if _, err := os.Stat(filepath.Join(dir, "types")); !os.IsNotExist(err) {
			t.Errorf("the emptied types directory was kept: %v", err)
		}
	})
	t.Run("a generation without types keeps a hand-written types package", func(t *testing.T) {
		dir := t.TempDir()
		hand := filepath.Join(dir, "types", "types.go")
		if err := os.MkdirAll(filepath.Dir(hand), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(hand, []byte("package types\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gen(t, dir, imp...)
		if data, err := os.ReadFile(hand); err != nil || string(data) != "package types\n" {
			t.Errorf("types/types.go: %q, %v", data, err)
		}
	})
	t.Run("only keeps types/types.go", func(t *testing.T) {
		dir := t.TempDir()
		gen(t, dir, append([]string{"-types"}, embed...)...)
		before := listGenerated(t, dir)
		gen(t, dir, only...)
		if after := listGenerated(t, dir); !slices.Equal(before, after) || !slices.Contains(after, "types/types.go") {
			t.Errorf("only changed the files: before %v, after %v", before, after)
		}
	})
	t.Run("only after embed keeps the parser", func(t *testing.T) {
		dir := t.TempDir()
		gen(t, dir, embed...)
		before := listGenerated(t, dir)
		parser, err := os.ReadFile(filepath.Join(dir, "parser.go"))
		if err != nil {
			t.Fatal(err)
		}
		// A stale runtime file is removed; the parser stays.
		stale := filepath.Join(dir, "internal", "runtime", "stale.go")
		if err := os.WriteFile(stale, []byte(generatedHeader+" DO NOT EDIT.\n\npackage runtime\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gen(t, dir, only...)
		if after := listGenerated(t, dir); !slices.Equal(before, after) {
			t.Errorf("only changed the files: before %v, after %v", before, after)
		}
		if now, err := os.ReadFile(filepath.Join(dir, "parser.go")); err != nil || string(now) != string(parser) {
			t.Errorf("only changed parser.go: %v", err)
		}
	})
	t.Run("only alone writes the runtime", func(t *testing.T) {
		dir := t.TempDir()
		gen(t, dir, only...)
		files := listGenerated(t, dir)
		if !hasRuntimeFiles(files) || slices.Contains(files, "parser.go") || slices.Contains(files, "api.go") {
			t.Errorf("only wrote %v", files)
		}
	})
	t.Run("only keeps a hand-written parser", func(t *testing.T) {
		dir := t.TempDir()
		for _, name := range []string{"parser.go", "api.go"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("package p\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		gen(t, dir, only...)
		for _, name := range []string{"parser.go", "api.go"} {
			if data, err := os.ReadFile(filepath.Join(dir, name)); err != nil || string(data) != "package p\n" {
				t.Errorf("%s: %q, %v", name, data, err)
			}
		}
	})
}

// TestOwnedFiles checks the files each mode may remove as stale.
func TestOwnedFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "internal", "runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	rt := filepath.Join(dir, "internal", "runtime", "x.go")
	if err := os.WriteFile(rt, []byte("package runtime\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	parser, api, types := filepath.Join(dir, "parser.go"), filepath.Join(dir, "api.go"), filepath.Join(dir, "types", "types.go")
	for mode, want := range map[genRuntime][]string{
		runtimeEmbed:  {parser, api, types, rt},
		runtimeImport: {parser, api, types, rt},
		runtimeOnly:   {rt},
	} {
		if got := ownedFiles(dir, mode, false); !slices.Equal(got, want) {
			t.Errorf("%v owns %v, want %v", mode, got, want)
		}
	}
}

// TestGenKeepsHandWrittenFiles checks that gen refuses to replace a file it did not generate.
func TestGenKeepsHandWrittenFiles(t *testing.T) {
	g := writeFile(t, "kv.pego", kv)
	for _, name := range []string{"parser.go", "api.go", "types/types.go", "internal/runtime/runtime.go"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
				t.Fatal(err)
			}
			const hand = "package kv\n\n// hand-written\n"
			if err := os.WriteFile(file, []byte(hand), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := runCLI(t, "", "gen", "-g", g, "-module", "example.com/kv", "-types", "-o", dir)
			if err == nil || !strings.Contains(err.Error(), "was not generated by pego") || !strings.Contains(err.Error(), name) {
				t.Fatalf("got %v, want a refusal naming %s", err, name)
			}
			if data, _ := os.ReadFile(file); string(data) != hand {
				t.Errorf("%s was overwritten: %q", name, data)
			}
			// Nothing else was written before the refusal.
			if files := listGenerated(t, dir); len(files) != 1 {
				t.Errorf("files after the refusal: %v", files)
			}
		})
	}
}

// TestGenFlagMisuse checks that flags that do nothing in a mode are errors.
func TestGenFlagMisuse(t *testing.T) {
	g := writeFile(t, "kv.pego", kv)
	out := t.TempDir()
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"grammar with only", []string{"-g", g, "-runtime", "only", "-o", out}, "-g cannot be combined with -runtime only"},
		{"types with only", []string{"-runtime", "only", "-types", "-o", out}, "-types cannot be combined with -runtime only"},
		{"recognize with only", []string{"-runtime", "only", "-recognize", "-o", out}, "-recognize cannot be combined"},
		{"pkg with only", []string{"-runtime", "only", "-pkg", "x", "-o", out}, "-pkg cannot be combined"},
		{"module with only", []string{"-runtime", "only", "-module", "example.com/x", "-o", out}, "-module cannot be combined"},
		{"import without runtime module", []string{"-lang", "ts", "-g", g, "-runtime", "import", "-o", out}, "-runtime-module"},
		{"runtime module with embed", []string{"-lang", "ts", "-g", g, "-runtime-module", "../rt.js", "-o", out}, "-runtime-module can only be combined with -runtime import"},
		{"runtime module with go", []string{"-g", g, "-module", "example.com/x", "-runtime-module", "../rt.js", "-o", out}, "-runtime-module is only supported with -lang ts"},
		{"grammar with ts only", []string{"-lang", "ts", "-g", g, "-runtime", "only", "-o", out}, "-g cannot be combined with -runtime only"},
		{"types with ts only", []string{"-lang", "ts", "-runtime", "only", "-types", "-o", out}, "-types cannot be combined with -runtime only"},
		{"module with ts only", []string{"-lang", "ts", "-runtime", "only", "-runtime-module", "../rt.js", "-o", out}, "-runtime-module cannot be combined"},
		{"ts without output", []string{"-lang", "ts", "-g", g}, "-o"},
		{"embed without module", []string{"-g", g, "-pkg", "kv", "-o", out}, "-module"},
		{"import without module or pkg", []string{"-g", g, "-runtime", "import", "-o", out}, "-pkg or -module"},
		{"hyphenated module", []string{"-g", g, "-module", "example.com/my-parser", "-o", out}, "-pkg"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runCLI(t, "", append([]string{"gen"}, tc.args...)...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want an error containing %q", err, tc.want)
			}
		})
	}
	if files := listGenerated(t, out); len(files) != 0 {
		t.Errorf("a rejected command wrote %v", files)
	}
}

// TestGenImportNeedsOnlyPackageName checks that an import-mode parser can be generated with -pkg
// alone, and that the default package name drops a major version suffix.
func TestGenImportNeedsOnlyPackageName(t *testing.T) {
	g := writeFile(t, "kv.pego", kv)
	dir := t.TempDir()
	if _, err := runCLI(t, "", "gen", "-g", g, "-runtime", "import", "-pkg", "kv", "-o", dir); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "parser.go")); err != nil || !strings.Contains(string(data), "package kv\n") {
		t.Errorf("parser.go: %v", err)
	}
	dir = t.TempDir()
	if _, err := runCLI(t, "", "gen", "-g", g, "-module", "example.com/kv/v2", "-o", dir); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "parser.go")); err != nil || !strings.Contains(string(data), "package kv\n") {
		t.Errorf("a /v2 module did not name the package after its parent: %v", err)
	}
}

// TestOwnedFilesTS checks the files each mode may remove as stale for TypeScript.
func TestOwnedFilesTS(t *testing.T) {
	dir := t.TempDir()
	parser, types, rt := filepath.Join(dir, "parser.ts"), filepath.Join(dir, "types.ts"), filepath.Join(dir, "runtime.ts")
	for mode, want := range map[genRuntime][]string{
		runtimeEmbed:  {parser, types, rt},
		runtimeImport: {parser, types},
		runtimeOnly:   {rt},
	} {
		if got := ownedFiles(dir, mode, true); !slices.Equal(got, want) {
			t.Errorf("%v owns %v, want %v", mode, got, want)
		}
	}
}

// TestGenTypeScriptModes checks the files of each TypeScript runtime mode, and that a later
// generation removes the generated files that the new mode no longer writes.
func TestGenTypeScriptModes(t *testing.T) {
	g := writeFile(t, "kv.pego", kv)
	dir := t.TempDir()
	list := func() string {
		var names []string
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return strings.Join(names, " ")
	}
	gen := func(args ...string) {
		t.Helper()
		if _, err := runCLI(t, "", append([]string{"gen", "-lang", "ts", "-o", dir}, args...)...); err != nil {
			t.Fatal(err)
		}
	}
	gen("-g", g, "-types")
	if got := list(); got != "parser.ts runtime.ts types.ts" {
		t.Fatalf("embed -types wrote %q", got)
	}
	gen("-g", g, "-runtime", "import", "-runtime-module", "../shared/runtime.ts")
	if got := list(); got != "parser.ts runtime.ts" {
		t.Fatalf("import wrote %q; the stale types.ts must be gone and runtime.ts, which it does not own, must stay", got)
	}
	if data := readTS(t, dir, "parser.ts"); !strings.Contains(data, `from "../shared/runtime.ts"`) || strings.Contains(data, "./runtime.ts") {
		t.Errorf("parser.ts does not import the runtime module:\n%.400s", data)
	}
	gen("-runtime", "only")
	if got := list(); got != "parser.ts runtime.ts" {
		t.Fatalf("only wrote %q; it must leave parser.ts alone", got)
	}
	gen("-g", g)
	if got := list(); got != "parser.ts runtime.ts" {
		t.Fatalf("embed wrote %q", got)
	}
	hand := filepath.Join(dir, "types.ts")
	if err := os.WriteFile(hand, []byte("export type A = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gen("-g", g) // types.ts is hand-written: not touched, as it is not generated
	if data, _ := os.ReadFile(hand); string(data) != "export type A = 1;\n" {
		t.Errorf("a hand-written types.ts was changed: %q", data)
	}
	if _, err := runCLI(t, "", "gen", "-lang", "ts", "-o", dir, "-g", g, "-types"); err == nil || !strings.Contains(err.Error(), "was not generated by pego") {
		t.Errorf("-types over a hand-written types.ts: %v", err)
	}
}

// TestGenImportKeepsSharedRuntime checks that a parser generated with -runtime import neither
// writes nor removes the runtime.ts it imports, which -runtime only wrote into the same directory.
func TestGenImportKeepsSharedRuntime(t *testing.T) {
	g := writeFile(t, "kv.pego", kv)
	dir := t.TempDir()
	gen := func(args ...string) {
		t.Helper()
		if _, err := runCLI(t, "", append([]string{"gen", "-lang", "ts", "-o", dir}, args...)...); err != nil {
			t.Fatal(err)
		}
	}
	gen("-runtime", "only")
	shared := readTS(t, dir, "runtime.ts")
	gen("-g", g, "-runtime", "import", "-runtime-module", "./runtime.ts")
	if got := readTS(t, dir, "runtime.ts"); got != shared {
		t.Errorf("-runtime import changed the shared runtime.ts")
	}
	if data := readTS(t, dir, "parser.ts"); !strings.Contains(data, `from "./runtime.ts"`) {
		t.Errorf("parser.ts does not import the shared runtime:\n%.400s", data)
	}
	// A hand-written runtime.ts is not touched either, and not refused: the import mode does not own it.
	if err := os.WriteFile(filepath.Join(dir, "runtime.ts"), []byte("export const x = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gen("-g", g, "-runtime", "import", "-runtime-module", "./runtime.ts")
	if got := readTS(t, dir, "runtime.ts"); got != "export const x = 1;\n" {
		t.Errorf("a hand-written runtime.ts was changed: %q", got)
	}
}
