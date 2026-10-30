package compile

import (
	"strings"
	"testing"
)

// A variable declared with the retired name of the top type is told to use _.
func TestVarOfRetiredTypeName(t *testing.T) {
	t.Parallel()
	if got := compileMessage(t, "var d any\nrule main = \"a\""); !strings.Contains(got, "any is not a type; write _") {
		t.Errorf("got %q", got)
	}
}

// The type of a recursive rule nests without end; a value of it that does not fit is a mismatch,
// not a value of unknown type, so it gets no cast hint. A value of type _ does.
func TestCastHintOnlyForUnknownTypes(t *testing.T) {
	t.Parallel()
	const recursive = "type T = { F [][]int }\nrule n = p / q\nrule p = \"(\" x:n \")\" => list($x)\nrule q = \"a\" => list()\nrule main T = v:n $$ => T{F: $v}"
	got := compileMessage(t, recursive)
	if !strings.Contains(got, "cannot use") || strings.Contains(got, "not known") {
		t.Errorf("recursive: %q", got)
	}
	got = compileMessage(t, "type T = { F int }\nrule main T = d:one $$ => T{F: $d}\nrule one _ = _ => 1")
	if !strings.Contains(got, "the type of the value is not known") {
		t.Errorf("unknown: %q", got)
	}
}
