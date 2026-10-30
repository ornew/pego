package compile

import (
	"strings"
	"testing"
)

func TestInlineActionsAreRejectedWhereTheirValueIsUnused(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		`rule main = &(x:"a" => $x) "a"`,
		`rule main = !(x:"a" => $x) "b"`,
		`rule main = -(x:"a" => $x) "b"`,
		`rule main = &("a" (x:"b" => $x)) "a" "b"`,
	} {
		msg := compileError(t, src)
		if !strings.Contains(msg, "an inline action is not allowed in a lookahead or a discard") {
			t.Errorf("%s: %s", src, msg)
		}
	}
}

func TestInlineActionsSeeOnlyTheirOwnCaptures(t *testing.T) {
	t.Parallel()
	msg := compileError(t, `rule main = y:"a" (x:"b" => $y)`)
	if !strings.Contains(msg, "undefined capture $y") {
		t.Fatal(msg)
	}
	// The inline action closes its captures: the enclosing rule cannot read them.
	msg = compileError(t, `rule main = (x:"a" => $x) "b" => $x`)
	if !strings.Contains(msg, "undefined capture $x") {
		t.Fatal(msg)
	}
}

func TestInlineActionsKeepBlocksUnsupported(t *testing.T) {
	t.Parallel()
	msg := compileError(t, `rule main = (x:"a" => { y := $x; $y })`)
	if !strings.Contains(msg, "action statement blocks are not implemented") {
		t.Fatal(msg)
	}
}
