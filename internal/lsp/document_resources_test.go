package lsp

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"
)

type documentResourceResult struct {
	Documents, SourceBytes, BurstPublications int
	Responsive                                bool
	// Nanoseconds: opening every document one at a time until its diagnostics arrive; from the
	// first change of the burst to the diagnostics of the last document, and to the reply to a
	// hover on the last document sent after the changes.
	OpenNanos, BurstNanos, HoverNanos int64
	Idle, Opened, Closed              requestResourceState
}

// runDocumentResourceCase opens n copies of the benchmark grammar under distinct URIs, changes
// every document at once, hovers the last one, and closes them all. Heap samples are post-GC
// process heap, including the client; they are neither peak heap nor RSS.
func runDocumentResourceCase(t *testing.T, n int, responsive, measure bool) documentResourceResult {
	t.Helper()
	if requestHook != nil || analyzeHook != nil {
		t.Fatal("resource probe requires inactive hooks")
	}
	source := f08BenchmarkGrammar()
	result := documentResourceResult{Documents: n, SourceBytes: len(source), Responsive: responsive}
	uri := func(i int) string { return fmt.Sprintf("file:///resource%04d.zp", i) }
	peer := newResourcePeer(t)
	defer peer.abort()
	c := peer.client
	c.request("initialize", map[string]any{"capabilities": map[string]any{"experimental": map[string]any{"zp": map[string]any{"responsiveAnalysis": responsive}}}})
	if measure {
		result.Idle = requestResourceSample()
	}
	start := time.Now()
	for i := range n {
		if d := c.open(uri(i), source); len(d) != 0 {
			t.Fatalf("resource fixture diagnostics: %+v", d)
		}
	}
	result.OpenNanos = time.Since(start).Nanoseconds()
	if measure {
		result.Opened = requestResourceSample()
	}

	// Change every document, then hover the last one. Like a real client, keep reading while
	// writing: the server blocks on writes the client does not read.
	var burst []any
	for i := range n {
		burst = append(burst, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didChange", "params": map[string]any{
			"textDocument":   map[string]any{"uri": uri(i), "version": 2},
			"contentChanges": []map[string]any{{"range": Range{}, "text": "// changed\n"}},
		}})
	}
	burst = append(burst, map[string]any{"jsonrpc": "2.0", "id": 7000, "method": "textDocument/hover", "params": at(uri(n-1), 2, 5)})
	start = time.Now()
	written := sendAll(c, burst)
	published := map[string]bool{}
	for len(published) < n || result.HoverNanos == 0 {
		m := c.next()
		if _, ok := m["method"]; !ok {
			if string(m["id"]) != "7000" || string(m["result"]) == "null" || m["error"] != nil {
				t.Fatalf("burst hover: %v", m)
			}
			result.HoverNanos = time.Since(start).Nanoseconds()
			continue
		}
		var p publishDiagnosticsParams
		if string(m["method"]) != `"textDocument/publishDiagnostics"` || json.Unmarshal(m["params"], &p) != nil {
			t.Fatalf("unexpected message %v", m)
		}
		if len(p.Diagnostics) != 0 {
			t.Fatalf("burst diagnostics: %+v", p)
		}
		result.BurstPublications++
		if p.Version != nil && *p.Version == 2 {
			published[p.URI] = true
		}
		if len(published) == n && result.BurstNanos == 0 {
			result.BurstNanos = time.Since(start).Nanoseconds()
		}
	}

	if err := <-written; err != nil {
		t.Fatal(err)
	}
	var closes []any
	for i := range n {
		closes = append(closes, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didClose", "params": map[string]any{"textDocument": map[string]any{"uri": uri(i)}}})
	}
	// A reply after the closes orders them; notifications before it are discarded.
	closes = append(closes, map[string]any{"jsonrpc": "2.0", "id": 7001, "method": "resource/sequence"})
	written = sendAll(c, closes)
	assertResponseError(t, c.response("7001"), codeMethodNotFound)
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	c.pending = nil
	if measure {
		result.Closed = requestResourceSample()
	}
	peer.shutdown()
	runtime.KeepAlive(source)
	return result
}

// sendAll writes messages in order from another goroutine and reports the first error.
func sendAll(c *testClient, messages []any) <-chan error {
	done := make(chan error, 1)
	go func() {
		for _, m := range messages {
			data, err := json.Marshal(m)
			if err == nil {
				_, err = fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n%s", len(data), data)
			}
			if err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	return done
}

func TestDocumentResources(t *testing.T) {
	for _, responsive := range []bool{false, true} {
		t.Run(fmt.Sprintf("responsive=%v", responsive), func(t *testing.T) {
			result := runDocumentResourceCase(t, 8, responsive, false)
			// The synchronous server publishes every version; the responsive one may skip
			// superseded versions but publishes each document's latest.
			if result.BurstPublications < 8 || !responsive && result.BurstPublications != 8 {
				t.Fatalf("burst publications: %+v", result)
			}
		})
	}
}

// TestDocumentResourceProbe is run in fresh processes, one per configuration.
func TestDocumentResourceProbe(t *testing.T) {
	if os.Getenv("ZP_LSP_DOCUMENT_PROBE") != "1" {
		t.Skip("manual fresh-process resource probe")
	}
	n, err := strconv.Atoi(os.Getenv("ZP_LSP_DOCUMENT_COUNT"))
	if err != nil || n < 1 || n > 1024 {
		t.Fatal("ZP_LSP_DOCUMENT_COUNT must be 1 to 1024")
	}
	result := runDocumentResourceCase(t, n, os.Getenv("ZP_LSP_RESPONSIVE") == "1", true)
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("resource %s", raw)
}
