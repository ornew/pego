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
