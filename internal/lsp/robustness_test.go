package lsp

import (
	"strings"
	"testing"
)

// Regression tests for input that broke the server.

// TestIncompletePackageClause checks that typing a package clause reports an error and keeps every
// feature working. The parser used to panic on it, so no diagnostics were published and requests
// failed until the text changed.
func TestIncompletePackageClause(t *testing.T) {
	c := newInitialized(t)
	uri := "file:///package.pego"
	c.open(uri, "def a = b\n")
	for i, text := range []string{"package ", "package", "package {", "package\ndef a = \"x\""} {
		d := change(c, uri, i+2, map[string]any{"text": text})
		if len(d) != 1 || d[0].Message[:len("expected identifier")] != "expected identifier" {
			t.Errorf("%q: diagnostics %+v", text, d)
		}
		c.request("textDocument/hover", at(uri, 0, 1))
		c.request("textDocument/semanticTokens/full", docParams(uri))
		c.request("textDocument/documentSymbol", docParams(uri))
		c.request("textDocument/completion", at(uri, 0, len(text)))
	}
	c.exit()
}

// TestHostileInput checks that long runs of invalid characters and deep nesting, which used to
// overflow the stack and kill the server, produce a few diagnostics, and that the number of
// diagnostics is limited.
func TestHostileInput(t *testing.T) {
	c := newInitialized(t)
	junk := "def a = \"x\"\n" + strings.Repeat("`;~😀", 1<<19)
	if d := c.open("file:///junk.pego", junk); len(d) != 1 || !strings.HasPrefix(d[0].Message, "unexpected characters") {
		t.Errorf("junk: %d diagnostics, the first %+v", len(d), d[:min(len(d), 1)])
	}
	if d := c.open("file:///deep.pego", "def a = "+strings.Repeat("(\n", 1<<20)); messages(d) != "nesting too deep" {
		t.Errorf("deep nesting: %.200s", messages(d))
	}
	// Separate invalid characters: the parser records at most 100 errors.
	if d := c.open("file:///many.pego", strings.Repeat("` ", 10000)); len(d) != 101 || d[100].Message != "too many errors" {
		t.Errorf("many syntax errors: %d diagnostics", len(d))
	}
	// Compile errors: the server publishes at most 100.
	refs := strings.Repeat(" x", 300)
	if d := c.open("file:///undefined.pego", "def a ="+refs); len(d) != 101 || d[100].Message != "200 more errors" || d[100].Range != rng(0, 208, 0, 209) {
		t.Errorf("many compile errors: %d diagnostics, the last %+v", len(d), d[len(d)-1])
	}
	c.exit()
}

// TestCompletionAfterTriggerCharacters checks that "$" and "." offer completions only in value
// expressions. In a parsing expression they are the end-of-line anchor and any character, and
// accepting a capture or a rule there (Enter after typing them) broke the expression.
func TestCompletionAfterTriggerCharacters(t *testing.T) {
	c := newInitialized(t)
	uri := "file:///trigger.pego"
	lines := []string{
		`def a = x:"a" $`,
		`def b = x:"a" $x`,
		`def c = "x" .`,
		`def d = "x" .b`,
		`def e = "x" . `,
		`def f = x:"a" -> $`,
		`def g = x:"a" -> $x.`,
		`def h = x:"a" [$`,
		`def i = x:"a" [len($x) > 0] $`,
	}
	c.open(uri, strings.Join(lines, "\n"))
	rules := []string{"a", "b", "c", "d", "def", "e", "f", "g", "h", "i", "type"}
	for i, want := range [][]string{
		nil, nil, nil, nil, rules,
		{"x"}, {"children", "endPos", "startPos"}, {"x"}, nil,
	} {
		got := completionLabels(c, uri, i, len(lines[i]))
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("%q: got %q, want %q", lines[i], got, want)
		}
	}
	c.exit()
}

// TestRenameToUndefinedName checks that a rule or type cannot be renamed to a name that is used
// but not defined: the references to that name would silently start to refer to it.
func TestRenameToUndefinedName(t *testing.T) {
	c := newInitialized(t)
	uri := "file:///undefined.pego"
	c.open(uri, "def a = b c\ndef b = \"x\"\ntype T struct { F U }\ndef d: T = \"d\"")
	for _, tc := range []struct {
		line, char int
		name, want string
	}{
		{1, 4, "c", "rule c is used (but not defined) in the file"},
		{2, 5, "U", "type U is used (but not defined) in the file"},
	} {
		p := at(uri, tc.line, tc.char)
		p["newName"] = tc.name
		if e := c.requestError("textDocument/rename", p); e.Message != tc.want {
			t.Errorf("rename to %s: %q, want %q", tc.name, e.Message, tc.want)
		}
	}
	// A name used only in the other namespace is free.
	p := at(uri, 1, 4)
	p["newName"] = "U"
	c.request("textDocument/rename", p)
	c.exit()
}

// TestEscapeErrorRange checks that the diagnostic of an invalid escape sequence covers the whole
// sequence, from its backslash.
func TestEscapeErrorRange(t *testing.T) {
	c := newInitialized(t)
	d := c.open("file:///escape.pego", "def a = \"😀\\q\" ` b")
	if len(d) != 2 || d[0].Range != rng(0, 11, 0, 13) || d[1].Range != rng(0, 15, 0, 16) {
		t.Errorf("diagnostics %+v", d)
	}
	c.exit()
}

// TestFailedAnalysis checks that a panic while analyzing a document is published as a diagnostic
// and that the last good analysis keeps serving requests.
func TestFailedAnalysis(t *testing.T) {
	analyzeHook = func(text string) {
		if strings.Contains(text, "BOOM") {
			panic("boom")
		}
	}
	t.Cleanup(func() { analyzeHook = nil })
	c := newInitialized(t)
	const want = "internal error while analyzing the document: boom"

	// The first version fails: an empty analysis.
	if d := c.open("file:///new.pego", "def BOOM = x"); messages(d) != want {
		t.Errorf("diagnostics %+v", d)
	}
	var syms []DocumentSymbol
	c.requestInto("textDocument/documentSymbol", docParams("file:///new.pego"), &syms)
	if len(syms) != 0 {
		t.Errorf("symbols %+v", syms)
	}

	// A later version fails: the analysis of the earlier one is kept.
	uri := "file:///boom.pego"
	c.open(uri, "def  a = \"x\"\ndef main = a")
	if d := change(c, uri, 2, edit(1, 0, 1, 0, "// BOOM\n")); messages(d) != want {
		t.Errorf("diagnostics %+v", d)
	}
	c.requestInto("textDocument/documentSymbol", docParams(uri), &syms)
	if len(syms) != 2 || syms[1].Name != "main" {
		t.Errorf("symbols %+v", syms)
	}
	var locs []Location
	c.requestInto("textDocument/definition", at(uri, 1, 11), &locs)
	if len(locs) != 1 || locs[0].Range != rng(0, 5, 0, 6) {
		t.Errorf("definition %+v", locs)
	}
	// Formatting and rename would edit the text as it was, so they refuse.
	if r := c.request("textDocument/formatting", docParams(uri)); string(r) != "[]" {
		t.Errorf("formatting %s", r)
	}
	if e := c.requestError("textDocument/prepareRename", at(uri, 1, 11)); e.Code != codeRequestFailed {
		t.Errorf("prepareRename %+v", e)
	}
	// Once the text analyzes again, everything is back.
	if d := change(c, uri, 3, edit(1, 0, 2, 0, "")); len(d) != 0 {
		t.Errorf("diagnostics after the fix %+v", d)
	}
	c.exit()
}
