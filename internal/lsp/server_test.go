package lsp

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestLifecycle(t *testing.T) {
	c := newServer(t)
	var init struct {
		Capabilities struct {
			PositionEncoding string `json:"positionEncoding"`
			TextDocumentSync struct {
				OpenClose bool `json:"openClose"`
				Change    int  `json:"change"`
			} `json:"textDocumentSync"`
			DocumentFormattingProvider bool `json:"documentFormattingProvider"`
			DefinitionProvider         bool `json:"definitionProvider"`
			RenameProvider             struct {
				PrepareProvider bool `json:"prepareProvider"`
			} `json:"renameProvider"`
		} `json:"capabilities"`
		ServerInfo struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	c.requestInto("initialize", map[string]any{"processId": nil, "capabilities": map[string]any{}}, &init)
	caps := init.Capabilities
	if caps.PositionEncoding != "utf-16" || !caps.TextDocumentSync.OpenClose || caps.TextDocumentSync.Change != 2 ||
		!caps.DocumentFormattingProvider || !caps.DefinitionProvider || !caps.RenameProvider.PrepareProvider ||
		init.ServerInfo.Name != "pego" {
		t.Errorf("initialize result %+v", init)
	}
	c.notify("initialized", map[string]any{})
	if e := c.requestError("initialize", map[string]any{}); e.Code != codeInvalidRequest {
		t.Errorf("second initialize: %+v", e)
	}
	if r := c.request("shutdown", nil); string(r) != "null" {
		t.Errorf("shutdown result %s", r)
	}
	if e := c.requestError("textDocument/hover", at("file:///a.pego", 0, 0)); e.Code != codeInvalidRequest {
		t.Errorf("request after shutdown: %+v", e)
	}
	c.notify("exit", nil)
	c.wait(nil)
}

func TestRequestBeforeInitialize(t *testing.T) {
	c := newServer(t)
	if e := c.requestError("textDocument/hover", at("file:///a.pego", 0, 0)); e.Code != codeServerNotInitialized {
		t.Errorf("error %+v", e)
	}
	// Notifications before initialize are dropped: no diagnostics are published for this document.
	c.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{"uri": "file:///early.pego", "languageId": "pego", "version": 1, "text": "def"},
	})
	c.request("initialize", map[string]any{"capabilities": map[string]any{}})
	if diags := c.open("file:///late.pego", `def main = "a"`); len(diags) != 0 {
		t.Errorf("diagnostics %v", diags)
	}
	if e := c.requestError("textDocument/hover", at("file:///early.pego", 0, 0)); e.Code != codeRequestFailed {
		t.Errorf("hover on a document opened before initialize: %+v", e)
	}
	c.exit()
}

func TestExitWithoutShutdown(t *testing.T) {
	c := newInitialized(t)
	c.notify("exit", nil)
	c.wait(ErrExitWithoutShutdown)
}

func TestMalformedMessages(t *testing.T) {
	c := newInitialized(t)

	// Invalid JSON: a parse error with a null id, and the server goes on.
	c.sendRaw(`{"jsonrpc": "2.0", "id": 1, "method": `)
	m := c.next()
	var e rpcError
	if json.Unmarshal(m["error"], &e); e.Code != codeParseError || string(m["id"]) != "null" {
		t.Errorf("invalid JSON: %v", m)
	}
	// A batch is not supported.
	c.sendRaw(`[{"jsonrpc": "2.0", "id": 1, "method": "shutdown"}]`)
	if m := c.next(); m["error"] == nil {
		t.Errorf("batch: %v", m)
	}
	// Not JSON-RPC 2.0.
	c.sendRaw(`{"jsonrpc": "1.0", "id": 7, "method": "shutdown"}`)
	m = c.next()
	if json.Unmarshal(m["error"], &e); e.Code != codeInvalidRequest || string(m["id"]) != "7" {
		t.Errorf("wrong version: %v", m)
	}
	// A request without a method.
	c.sendRaw(`{"jsonrpc": "2.0", "id": "x"}`)
	m = c.next()
	if json.Unmarshal(m["error"], &e); e.Code != codeInvalidRequest || string(m["id"]) != `"x"` {
		t.Errorf("no method: %v", m)
	}
	// A response from the client and unknown notifications are ignored.
	c.sendRaw(`{"jsonrpc": "2.0", "id": 99, "result": null}`)
	c.notify("$/cancelRequest", map[string]any{"id": 1})
	c.notify("workspace/didChangeConfiguration", map[string]any{"settings": nil})
	c.notify("textDocument/didOpen", "not an object")
	if e := c.requestError("no/such/method", nil); e.Code != codeMethodNotFound {
		t.Errorf("unknown method: %+v", e)
	}
	if e := c.requestError("textDocument/hover", nil); e.Code != codeInvalidParams {
		t.Errorf("missing params: %+v", e)
	}
	if e := c.requestError("textDocument/hover", map[string]any{"textDocument": 1}); e.Code != codeInvalidParams {
		t.Errorf("invalid params: %+v", e)
	}
	if e := c.requestError("textDocument/hover", at("file:///closed.pego", 0, 0)); e.Code != codeRequestFailed {
		t.Errorf("document not open: %+v", e)
	}
	// A string id is echoed back.
	c.sendRaw(`{"jsonrpc": "2.0", "id": "abc", "method": "textDocument/documentSymbol", "params": {"textDocument": {"uri": "file:///closed.pego"}}}`)
	if m := c.next(); string(m["id"]) != `"abc"` {
		t.Errorf("string id: %v", m)
	}
	// Header names are case-insensitive and other headers are ignored.
	body := `{"jsonrpc":"2.0","id":100,"method":"shutdown"}`
	if _, err := c.w.Write([]byte("content-type: application/vscode-jsonrpc; charset=utf-8\r\ncontent-length: " +
		strconv.Itoa(len(body)) + "\r\n\r\n" + body)); err != nil {
		t.Fatal(err)
	}
	if m := c.next(); string(m["id"]) != "100" || string(m["result"]) != "null" {
		t.Errorf("shutdown: %v", m)
	}
	c.notify("exit", nil)
	c.wait(nil)
}

func TestHeaderWithoutContentLength(t *testing.T) {
	c := newInitialized(t)
	if _, err := c.w.Write([]byte("Content-Type: x\r\n\r\n{}")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-c.done:
		if err == nil || !strings.Contains(err.Error(), "Content-Length") {
			t.Errorf("error %v", err)
		}
	case <-timeoutC():
		t.Fatal("the server did not stop")
	}
}

func TestInputEndsBeforeExit(t *testing.T) {
	c := newInitialized(t)
	c.w.Close()
	select {
	case err := <-c.done:
		if err == nil {
			t.Error("no error")
		}
	case <-timeoutC():
		t.Fatal("the server did not stop")
	}
}

func TestClientCapabilities(t *testing.T) {
	src := "type P struct { A Match }\ndef a = x:\"a\" -> len($x)"
	completion := func(c *testClient) CompletionItem {
		t.Helper()
		var list completionList
		c.requestInto("textDocument/completion", at("file:///caps.pego", 1, 19), &list)
		for _, it := range list.Items {
			if it.Label == "len" {
				return it
			}
		}
		t.Fatalf("no len in %+v", list)
		return CompletionItem{}
	}

	// A client with snippets and hierarchical symbols.
	c := newInitialized(t)
	c.open("file:///caps.pego", src)
	if it := completion(c); it.InsertText != "len($1)" || it.InsertTextFormat != insertSnippet {
		t.Errorf("with snippets: %+v", it)
	}
	var syms []DocumentSymbol
	c.requestInto("textDocument/documentSymbol", docParams("file:///caps.pego"), &syms)
	if len(syms) != 2 || len(syms[0].Children) != 1 {
		t.Errorf("hierarchical symbols %+v", syms)
	}
	c.exit()

	// A client without them.
	c = newServer(t)
	if e := c.requestError("initialize", map[string]any{"capabilities": "none"}); e.Code != codeInvalidParams {
		t.Errorf("initialize with invalid params: %+v", e)
	}
	c.request("initialize", map[string]any{"capabilities": map[string]any{}})
	c.open("file:///caps.pego", src)
	if it := completion(c); it.InsertText != "" || it.InsertTextFormat != 0 {
		t.Errorf("without snippets: %+v", it)
	}
	var flat []symbolInformation
	c.requestInto("textDocument/documentSymbol", docParams("file:///caps.pego"), &flat)
	uri := "file:///caps.pego"
	want := []symbolInformation{
		{Name: "P", Kind: symbolKindStruct, Location: Location{URI: uri, Range: rng(0, 0, 0, 25)}},
		{Name: "A", Kind: symbolKindField, Location: Location{URI: uri, Range: rng(0, 16, 0, 23)}, ContainerName: "P"},
		{Name: "a", Kind: symbolKindFunction, Location: Location{URI: uri, Range: rng(1, 0, 1, 24)}},
	}
	if !reflect.DeepEqual(flat, want) {
		t.Errorf("flat symbols\ngot  %+v\nwant %+v", flat, want)
	}
	c.exit()
}
