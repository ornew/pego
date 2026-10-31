package main

import (
	"os"
	"strings"
	"testing"
)

func TestFix(t *testing.T) {
	const old = "package p\n\n@start\nrule a = \"a\" -- \"b\" / \"c\" // a -- comment\n"
	const fixed = "package p\n\n@start\nrule a = \"a\" #cut \"b\" / \"c\" // a -- comment\n"
	if out, err := runCLI(t, old, "fix"); err != nil || out != fixed {
		t.Errorf("stdin: got %q, %v", out, err)
	}
	if _, err := runCLI(t, old, "fix", "-w"); err == nil {
		t.Error("expected an error for -w with standard input")
	}
	g := writeFile(t, "old.zp", old)
	ok := writeFile(t, "ok.zp", fixed)
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
	if _, err := runCLI(t, "", "lint", "-g", writeFile(t, "bad.zp", old), "-s", "a"); err == nil {
		t.Error("lint accepted the old cut spelling")
	}
	if _, err := runCLI(t, "", "fix", writeFile(t, "x.txt", old)); err == nil {
		t.Error("fix accepted a non-.zp file")
	}
}

func TestFixAnyTypes(t *testing.T) {
	// A value of type _ is narrowed with a cast before it is used as another type, as one of type
	// any is, so every reference to any can become _.
	const old = "type Box = { V any; L []any; O *any }\ntype Out = { S string; A any }\n" +
		"@start\n" +
		"rule main Out = b:box => Out{S: $b.V.(string), A: $b.V}\n" +
		"rule box Box = x:\"x\" => Box{V: $x, L: list($x), O: nil}\n" +
		"rule c any = x:\"y\" => $x.(any)\n"
	const fixed = "type Box = { V _; L []_; O *_ }\ntype Out = { S string; A _ }\n" +
		"@start\n" +
		"rule main Out = b:box => Out{S: $b.V.(string), A: $b.V}\n" +
		"rule box Box = x:\"x\" => Box{V: $x, L: list($x), O: nil}\n" +
		"rule c _ = x:\"y\" => $x.(_)\n"
	if out, err := runCLI(t, old, "fix"); err != nil || out != fixed {
		t.Errorf("got %q, %v\nwant %q", out, err, fixed)
	}
	if out, err := runCLI(t, fixed, "fix"); err != nil || out != fixed {
		t.Errorf("second fix: got %q, %v", out, err)
	}
	// Source that does not compile is left as it is.
	const bad = "type Box = { V any }\n@start\nrule main Box = undefined\n"
	if out, err := runCLI(t, bad, "fix"); err != nil || out != bad {
		t.Errorf("bad: got %q, %v", out, err)
	}
}

func TestFixNodeTypes(t *testing.T) {
	// K holds string nodes and becomes node[string]; V also holds a tuple node and the rule type
	// of pair a list node, so they become node[_]. A node[_] written as such is kept.
	const old = "type P = { K node; V node; W node[_] }\n" +
		"@start\n" +
		"rule main P = k:\"k\" v:(\"v\" / \"w\" \"x\") => P{K: $k, V: $v, W: $k}\n" +
		"rule pair node = \"a\"*\n"
	const fixed = "type P = { K node[string]; V node[_]; W node[_] }\n" +
		"@start\n" +
		"rule main P = k:\"k\" v:(\"v\" / \"w\" \"x\") => P{K: $k, V: $v, W: $k}\n" +
		"rule pair node[_] = \"a\"*\n"
	if out, err := runCLI(t, old, "fix"); err != nil || out != fixed {
		t.Errorf("got %q, %v\nwant %q", out, err, fixed)
	}
	if out, err := runCLI(t, fixed, "fix"); err != nil || out != fixed {
		t.Errorf("second fix: got %q, %v", out, err)
	}
}

func TestFixNodeTypesKeepsCastsAndNames(t *testing.T) {
	// The cast would type-check as node[string], but at run time it also takes the tuple, so it
	// stays node[_]. The rule and capture named node and node in strings and comments are not types.
	// The terminal rewrite before the node sites shifts their offsets.
	const old = "type Name terminal\n" +
		"type Q = { A node; B node }\n" +
		"@start\n" +
		"// a node\n" +
		"rule main Q = a:node b:(\"x\" / \"y\" \"z\") => Q{A: $a, B: $b.(node)}\n" +
		"rule node = \"node\"\n"
	const fixed = "type Name = node[string]\n" +
		"type Q = { A node[string]; B node[_] }\n" +
		"@start\n" +
		"// a node\n" +
		"rule main Q = a:node b:(\"x\" / \"y\" \"z\") => Q{A: $a, B: $b.(node[_])}\n" +
		"rule node = \"node\"\n"
	if out, err := runCLI(t, old, "fix"); err != nil || out != fixed {
		t.Errorf("got %q, %v\nwant %q", out, err, fixed)
	}
	out, err := runCLI(t, "", "parse", "-g", writeFile(t, "q.zp", fixed), "-s", "main", "-f", "sexpr", "-i", "nodeyz")
	if err != nil || !strings.Contains(out, "Q{") {
		t.Errorf("parse of the fixed grammar: %q, %v", out, err)
	}
}
