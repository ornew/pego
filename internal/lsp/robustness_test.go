package lsp

import (
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
