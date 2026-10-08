package main

import (
	"fmt"
	"strings"
	"testing"
)

// lspMessages frames JSON-RPC messages for the language server.
func lspMessages(bodies ...string) string {
	var b strings.Builder
	for _, body := range bodies {
		fmt.Fprintf(&b, "Content-Length: %d\r\n\r\n%s", len(body), body)
	}
	return b.String()
}

func TestLSP(t *testing.T) {
	in := lspMessages(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{}}}`,
		`{"jsonrpc":"2.0","method":"initialized","params":{}}`,
		`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///a.pego","languageId":"pego","version":1,"text":"def main = x"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
	)
	// Clients pass flags of their own, such as --clientProcessId; they are ignored.
	out, err := runCLI(t, in, "lsp", "--stdio", "--clientProcessId=123", "-v", "extra")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"id":1,"result":{"capabilities":`, `"message":"undefined rule x"`, `"id":2,"result":null`} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %s:\n%s", want, out)
		}
	}
	// Without shutdown, exit is an error (the process exits with status 1).
	if _, err := runCLI(t, lspMessages(`{"jsonrpc":"2.0","method":"exit"}`), "lsp"); err == nil {
		t.Error("exit without shutdown succeeded")
	}
}
