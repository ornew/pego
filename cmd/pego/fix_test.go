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

func TestFixAnyTypes(t *testing.T) {
	// V is read as a string, so it needs a cast with _ and keeps any; so do the rule type and the
	// cast of c, since a rule must produce a node and _ is not one.
	const old = "type Box = { V any; L []any; O *any }\ntype Out = { S string; A any }\n" +
		"rule main Out = b:box => Out{S: $b.V, A: $b.V}\n" +
		"rule box Box = x:\"x\" => Box{V: $x, L: list($x), O: nil}\n" +
		"rule c any = x:\"y\" => $x.(any)\n"
	const fixed = "type Box = { V any; L []_; O *_ }\ntype Out = { S string; A _ }\n" +
		"rule main Out = b:box => Out{S: $b.V, A: $b.V}\n" +
		"rule box Box = x:\"x\" => Box{V: $x, L: list($x), O: nil}\n" +
		"rule c any = x:\"y\" => $x.(any)\n"
	if out, err := runCLI(t, old, "fix"); err != nil || out != fixed {
		t.Errorf("got %q, %v\nwant %q", out, err, fixed)
	}
	if out, err := runCLI(t, fixed, "fix"); err != nil || out != fixed {
		t.Errorf("second fix: got %q, %v", out, err)
	}
	// Source that does not compile is left as it is.
	const bad = "type Box = { V any }\nrule main Box = undefined\n"
	if out, err := runCLI(t, bad, "fix"); err != nil || out != bad {
		t.Errorf("bad: got %q, %v", out, err)
	}
}
