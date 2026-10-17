package main

import (
	"os"
	"testing"
)

func TestFix(t *testing.T) {
	const old = "package p\n\nrule a = \"a\" -- \"b\" / \"c\" // a -- comment\n"
	const fixed = "package p\n\nrule a = \"a\" #cut \"b\" / \"c\" // a -- comment\n"
	if out, err := runCLI(t, old, "fix"); err != nil || out != fixed {
		t.Errorf("stdin: got %q, %v", out, err)
	}
	if _, err := runCLI(t, old, "fix", "-w"); err == nil {
		t.Error("expected an error for -w with standard input")
	}
	g := writeFile(t, "old.pego", old)
	ok := writeFile(t, "ok.pego", fixed)
	if out, err := runCLI(t, "", "fix", "-l", ok, g); err != nil || out != g+"\n" {
		t.Errorf("-l: got %q, %v", out, err)
	}
	if out, err := runCLI(t, "", "fix", "-w", g, ok); err != nil || out != "" {
		t.Errorf("-w: got %q, %v", out, err)
	}
	if b, err := os.ReadFile(g); err != nil || string(b) != fixed {
		t.Errorf("-w wrote %q, %v", b, err)
	}
	// The rewritten source compiles; the old spelling is rejected with a pointer to fix.
	if _, err := runCLI(t, "", "lint", "-g", g, "-s", "a"); err != nil {
		t.Errorf("lint after fix: %v", err)
	}
	if _, err := runCLI(t, "", "lint", "-g", writeFile(t, "bad.pego", old), "-s", "a"); err == nil {
		t.Error("lint accepted the old cut spelling")
	}
	if _, err := runCLI(t, "", "fix", writeFile(t, "x.txt", old)); err == nil {
		t.Error("fix accepted a non-.pego file")
	}
}
