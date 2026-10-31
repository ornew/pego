package core

import (
	"slices"
	"strings"
	"testing"
)

// Expected entries are ordered names, then literals, then classes, each by their bytes, and the
// message lists at most ten of them.
func TestExpectedOrderAndMessage(t *testing.T) {
	values := []string{"[a-z]", `"b"`, "end of input", `"a"`, "[0-9]", "any character", `"é"`, "beginning of line"}
	sortExpectedEntries(values)
	want := []string{"any character", "beginning of line", "end of input", `"a"`, `"b"`, `"é"`, "[0-9]", "[a-z]"}
	if !slices.Equal(values, want) {
		t.Fatalf("order %q, want %q", values, want)
	}

	entries := func(n int) []string {
		out := []string{"end of input"}
		for i := range n - 1 {
			out = append(out, `"`+string(rune('a'+i))+`"`)
		}
		return out
	}
	for _, c := range []struct {
		n    int
		want string
	}{
		{0, "syntax error"},
		{1, "syntax error: expected end of input"},
		{10, `syntax error: expected end of input, "a", "b", "c", "d", "e", "f", "g", "h", "i"`},
		{11, `syntax error: expected end of input, "a", "b", "c", "d", "e", "f", "g", "h", "i", ... and 1 more`},
		{41, `syntax error: expected end of input, "a", "b", "c", "d", "e", "f", "g", "h", "i", ... and 31 more`},
	} {
		e := &SyntaxError{Line: 1, Col: 2, Expected: entries(c.n)}
		if c.n == 0 {
			e.Expected = nil
		}
		if got := e.Message(); got != c.want {
			t.Errorf("%d entries: %q, want %q", c.n, got, c.want)
		}
		if got := e.Error(); got != "1:2: "+c.want {
			t.Errorf("%d entries: Error %q", c.n, got)
		}
		if c.n > 0 && !slices.Equal(e.Expected, entries(c.n)) {
			t.Errorf("%d entries: Expected changed to %q", c.n, e.Expected)
		}
	}
	// Messages of @error are never cut.
	e := &SyntaxError{Messages: make([]string, 12), Expected: entries(12)}
	if got := e.Message(); got != strings.Repeat("; ", 11) {
		t.Errorf("messages: %q", got)
	}
}
