package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"testing"
	"time"
)

// testClient talks to a server running in the same process over pipes.
type testClient struct {
	t      *testing.T
	w      *io.PipeWriter
	msgs   chan map[string]json.RawMessage
	done   chan error
	nextID int
	// pending holds the notifications received while waiting for a response.
	pending []map[string]json.RawMessage
}

const timeout = 10 * time.Second

func timeoutC() <-chan time.Time { return time.After(timeout) }

// newServer starts a server and returns a client connected to it, without initializing it.
func newServer(t *testing.T) *testClient {
	t.Helper()
	cr, sw := io.Pipe() // server to client
	sr, cw := io.Pipe() // client to server
	c := &testClient{t: t, w: cw, msgs: make(chan map[string]json.RawMessage, 100), done: make(chan error, 1)}
	go func() {
		err := Serve(sr, sw, "test")
		sw.Close()
		sr.Close()
		c.done <- err
	}()
	go func() {
		r := bufio.NewReader(cr)
		for {
			body, err := readMessage(r)
			if err != nil {
				close(c.msgs)
				return
			}
			var m map[string]json.RawMessage
			if err := json.Unmarshal(body, &m); err != nil {
				t.Errorf("server sent invalid JSON %s: %v", body, err)
			}
			c.msgs <- m
		}
	}()
	t.Cleanup(func() { cw.Close() })
	return c
}

// newInitialized starts and initializes a server.
func newInitialized(t *testing.T) *testClient {
	t.Helper()
	c := newServer(t)
	c.request("initialize", map[string]any{"capabilities": fullCapabilities})
	c.notify("initialized", map[string]any{})
	return c
}

// fullCapabilities are the client capabilities that change the server's responses, all set.
var fullCapabilities = map[string]any{
	"textDocument": map[string]any{
		"completion":     map[string]any{"completionItem": map[string]any{"snippetSupport": true}},
		"documentSymbol": map[string]any{"hierarchicalDocumentSymbolSupport": true},
		"rename":         map[string]any{"prepareSupport": true},
	},
}

// sendRaw sends body as a message.
func (c *testClient) sendRaw(body string) {
	c.t.Helper()
	if _, err := fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
		c.t.Fatalf("write: %v", err)
	}
}

func (c *testClient) send(v any) {
	c.t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		c.t.Fatal(err)
	}
	c.sendRaw(string(data))
}

func (c *testClient) notify(method string, params any) {
	c.t.Helper()
	c.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// next returns the next message from the server.
func (c *testClient) next() map[string]json.RawMessage {
	c.t.Helper()
	select {
	case m, ok := <-c.msgs:
		if !ok {
			c.t.Fatal("the server closed the connection")
		}
		return m
	case <-time.After(timeout):
		c.t.Fatal("timed out waiting for a message")
	}
	return nil
}

// response waits for the response with the given id.
func (c *testClient) response(id string) map[string]json.RawMessage {
	c.t.Helper()
	for {
		m := c.next()
		if _, ok := m["method"]; ok {
			c.pending = append(c.pending, m)
			continue
		}
		if string(m["id"]) != id {
			c.t.Fatalf("unexpected response %v (waiting for id %s)", m, id)
		}
		return m
	}
}

// call sends a request and returns its response.
func (c *testClient) call(method string, params any) map[string]json.RawMessage {
	c.t.Helper()
	c.nextID++
	c.send(map[string]any{"jsonrpc": "2.0", "id": c.nextID, "method": method, "params": params})
	return c.response(fmt.Sprint(c.nextID))
}

// request sends a request and returns its result, failing on an error.
func (c *testClient) request(method string, params any) json.RawMessage {
	c.t.Helper()
	m := c.call(method, params)
	if e, ok := m["error"]; ok {
		c.t.Fatalf("%s: error %s", method, e)
	}
	r, ok := m["result"]
	if !ok {
		c.t.Fatalf("%s: response without result: %v", method, m)
	}
	return r
}

// requestInto sends a request and decodes its result into v.
func (c *testClient) requestInto(method string, params, v any) {
	c.t.Helper()
	if err := json.Unmarshal(c.request(method, params), v); err != nil {
		c.t.Fatal(err)
	}
}

// requestError sends a request and returns its error, failing if it succeeds.
func (c *testClient) requestError(method string, params any) rpcError {
	c.t.Helper()
	m := c.call(method, params)
	var e rpcError
	if err := json.Unmarshal(m["error"], &e); err != nil || e.Code == 0 {
		c.t.Fatalf("%s: expected an error, got %v", method, m)
	}
	return e
}

// diagnostics waits for the next diagnostics of uri.
func (c *testClient) diagnostics(uri string) publishDiagnosticsParams {
	c.t.Helper()
	for {
		var m map[string]json.RawMessage
		if len(c.pending) > 0 {
			m, c.pending = c.pending[0], c.pending[1:]
		} else {
			m = c.next()
		}
		if string(m["method"]) != `"textDocument/publishDiagnostics"` {
			c.t.Fatalf("unexpected message %v", m)
		}
		var p publishDiagnosticsParams
		if err := json.Unmarshal(m["params"], &p); err != nil {
			c.t.Fatal(err)
		}
		if p.URI == uri {
			return p
		}
	}
}

// open opens a document and returns its diagnostics.
func (c *testClient) open(uri, text string) []Diagnostic {
	c.t.Helper()
	c.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{"uri": uri, "languageId": "pego", "version": 1, "text": text},
	})
	return c.diagnostics(uri).Diagnostics
}

// exit shuts the server down and checks that it stops without an error.
func (c *testClient) exit() {
	c.t.Helper()
	c.request("shutdown", nil)
	c.notify("exit", nil)
	c.wait(nil)
}

// wait waits for the server to stop and checks its error.
func (c *testClient) wait(want error) {
	c.t.Helper()
	select {
	case err := <-c.done:
		if err != want {
			c.t.Fatalf("server stopped with %v, want %v", err, want)
		}
	case <-time.After(timeout):
		c.t.Fatal("the server did not stop")
	}
}

func pos(line, char int) map[string]any { return map[string]any{"line": line, "character": char} }

func at(uri string, line, char int) map[string]any {
	return map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos(line, char)}
}

func docParams(uri string) map[string]any {
	return map[string]any{"textDocument": map[string]any{"uri": uri}}
}

// applyEdits applies non-overlapping edits to text.
func applyEdits(text string, edits []TextEdit) string {
	sort.Slice(edits, func(i, j int) bool {
		a, b := edits[i].Range.Start, edits[j].Range.Start
		return a.Line > b.Line || a.Line == b.Line && a.Character > b.Character
	})
	for _, e := range edits {
		r := e.Range
		text = applyChange(text, contentChange{Range: &r, Text: e.NewText})
	}
	return text
}

// rng is a compact way to write a range in tests.
func rng(l1, c1, l2, c2 int) Range { return Range{Position{l1, c1}, Position{l2, c2}} }
