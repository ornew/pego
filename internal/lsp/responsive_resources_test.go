package lsp

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// This peer uses the production framing/actor and retains only a stable Server
// pointer for joining workers after admission and cancellation are complete.
// No document or request maps are read outside the actor.
type resourcePeer struct {
	client  *testClient
	server  *Server
	reader  *io.PipeReader
	stopped bool
}

func newResourcePeer(t *testing.T) *resourcePeer {
	t.Helper()
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()
	c := &testClient{t: t, w: cw, msgs: make(chan map[string]json.RawMessage, 100), done: make(chan error, 1)}
	s := &Server{in: bufio.NewReader(sr), out: &conn{w: sw}, docs: map[string]*document{}, version: "resource-test"}
	p := &resourcePeer{client: c, server: s, reader: cr}
	go func() { err := s.run(); sw.Close(); sr.Close(); c.done <- err }()
	go func() {
		r := bufio.NewReader(cr)
		defer close(c.msgs)
		for {
			body, err := readMessage(r)
			if err != nil {
				return
			}
			var m map[string]json.RawMessage
			if err := json.Unmarshal(body, &m); err != nil {
				t.Errorf("invalid server response: %v", err)
				return
			}
			c.msgs <- m
		}
	}()
	return p
}
func (p *resourcePeer) abort() {
	if p.stopped {
		return
	}
	p.client.w.Close()
	p.reader.Close()
	select {
	case <-p.client.done:
		p.stopped = true
	case <-timeoutC():
		p.client.t.Error("resource server did not stop")
		// Do not reset the global hook or continue with live server workers.
		// The outer test timeout remains the final guard for broken teardown.
		<-p.client.done
		p.stopped = true
	}
}
func (p *resourcePeer) shutdown() {
	c := p.client
	c.request("shutdown", nil)
	c.notify("exit", nil)
	select {
	case err := <-c.done:
		p.stopped = true
		if err != nil {
			c.t.Fatal(err)
		}
	case <-timeoutC():
		c.t.Fatal("resource server did not stop")
	}
	c.w.Close()
	p.reader.Close()
}

type requestResourceState struct {
	HeapBytes, HeapObjects, TotalAllocated uint64
	Goroutines                             int
}

func requestResourceSample() requestResourceState {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return requestResourceState{m.HeapAlloc, m.HeapObjects, m.TotalAlloc, runtime.NumGoroutine()}
}

type requestResourceResult struct {
	Count, Filters, BodyBytes, ParamsBytes, SourceBytes, PeakHeldWorkers, CancelledResponses int
	Method, SourceHash, ParamsHash                                                           string
	OverflowRejected                                                                         bool
	Idle, Held, Joined                                                                       requestResourceState
}

// The source and one client-side payload remain alive across all GC samples.
// Only request admission, retained state, cancellation and teardown are probed;
// workers pause before dispatch, so this is not a handler latency benchmark.
func runRequestResourceCase(t *testing.T, count, filters int, measure bool) requestResourceResult {
	t.Helper()
	if requestHook != nil || analyzeHook != nil {
		t.Fatal("resource probe requires inactive hooks")
	}
	uri := "file:///resource.zp"
	source := f08BenchmarkGrammar()
	method := "textDocument/hover"
	var params any = at(uri, 1, 5)
	if filters > 0 {
		method = "textDocument/codeAction"
		only := make([]string, filters)
		for i := range only {
			only[i] = "quickfix.zp"
		}
		params = map[string]any{"textDocument": textDocumentIdentifier{URI: uri}, "range": Range{}, "context": map[string]any{"only": only, "diagnostics": []Diagnostic{}}}
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	params = nil
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 100, "method": method, "params": json.RawMessage(raw)})
	if err != nil {
		t.Fatal(err)
	}
	if len(body) > maxMessage {
		t.Fatal("resource payload exceeds protocol limit")
	}
	result := requestResourceResult{Count: count, Filters: filters, Method: method, BodyBytes: len(body), ParamsBytes: len(raw), SourceBytes: len(source), SourceHash: fmt.Sprintf("%x", sha256.Sum256([]byte(source))), ParamsHash: fmt.Sprintf("%x", sha256.Sum256(raw))}
	body = nil
	var hold atomic.Bool
	hold.Store(true)
	var active, peak atomic.Int64
	started := make(chan struct{}, 64)
	requestHook = func(ctx context.Context, got string) {
		if got != method || !hold.Load() {
			return
		}
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		started <- struct{}{}
		<-ctx.Done()
	}
	peer := newResourcePeer(t)
	c := peer.client
	defer func() { peer.abort(); requestHook = nil }()
	c.request("initialize", map[string]any{"capabilities": map[string]any{"experimental": map[string]any{"zp": map[string]any{"responsiveAnalysis": true}}}})
	if d := c.open(uri, source); len(d) != 0 {
		t.Fatalf("resource fixture diagnostics: %+v", d)
	}
	if measure {
		result.Idle = requestResourceSample()
	}
	for i := range count {
		asyncRequest(c, 100+i, method, json.RawMessage(raw))
	}
	for range min(count, 4) {
		awaitBarrier(t, started)
	}
	// A non-worker reply orders admission of every preceding request without
	// observing the actor's mutable maps or changing request capacity.
	asyncRequest(c, 9000, "resource/sequence", nil)
	assertResponseError(t, c.response("9000"), codeMethodNotFound)
	if cap(peer.server.actor.slots) != 4 {
		t.Fatalf("execution slots=%d want4", cap(peer.server.actor.slots))
	}
	result.PeakHeldWorkers = int(peak.Load())
	if result.PeakHeldWorkers != min(count, 4) {
		t.Fatalf("held workers=%d want%d", result.PeakHeldWorkers, min(count, 4))
	}
	if count == 64 {
		asyncRequest(c, 200, method, json.RawMessage(raw))
		assertResponseError(t, c.response("200"), codeServerCancelled)
		result.OverflowRejected = true
	}
	if measure {
		result.Held = requestResourceSample()
	}
	// The buffered protocol reader drains replies while notifications are sent.
	// No other client writer runs concurrently.
	for i := range count {
		c.notify("$/cancelRequest", map[string]any{"id": 100 + i})
	}
	for i := range count {
		assertResponseError(t, c.response(strconv.Itoa(100+i)), codeRequestCancelled)
		result.CancelledResponses++
	}
	// Every admitted request has now been canceled and removed. No further
	// document update or text request is sent until Wait returns, so pending
	// dispatch cannot Add while Wait observes an empty job count.
	joined := make(chan struct{})
	go func() { peer.server.actor.workers.Wait(); close(joined) }()
	awaitBarrier(t, joined)
	if active.Load() != 0 {
		t.Fatal("worker join left executing requests")
	}
	// Done workers can have buffered late responses. Wait until all are consumed,
	// then flush the actor's final case with another non-worker sequence reply.
	deadline := time.Now().Add(timeout)
	for len(peer.server.actor.results) > 0 {
		if time.Now().After(deadline) {
			t.Fatal("late worker results did not drain")
		}
		runtime.Gosched()
	}
	asyncRequest(c, 9001, "resource/sequence", nil)
	assertResponseError(t, c.response("9001"), codeMethodNotFound)
	if measure {
		result.Joined = requestResourceSample()
	}
	hold.Store(false)
	var hover any
	c.requestInto("textDocument/hover", at(uri, 1, 5), &hover)
	if hover == nil {
		t.Fatal("normal request did not recover after cancellation")
	}
	peer.shutdown()
	runtime.KeepAlive(raw)
	runtime.KeepAlive(source)
	runtime.KeepAlive(peer)
	return result
}
func TestResponsiveRequestResources(t *testing.T) {
	for _, count := range []int{1, 4, 16, 64} {
		for _, filters := range []int{0, 256} {
			t.Run(fmt.Sprintf("%d/%d", count, filters), func(t *testing.T) {
				result := runRequestResourceCase(t, count, filters, false)
				if result.CancelledResponses != count || result.OverflowRejected != (count == 64) {
					t.Fatalf("admission/cancellation: %+v", result)
				}
			})
		}
	}
}

// Run explicitly in fresh processes to avoid attributing process-wide test/GC
// history to one case. Heap deltas are post-GC live heap, not peak heap or RSS.
func TestResponsiveRequestResourceProbe(t *testing.T) {
	if os.Getenv("ZP_LSP_RESOURCE_PROBE") != "1" {
		t.Skip("manual fresh-process resource probe")
	}
	count, err := strconv.Atoi(os.Getenv("ZP_LSP_RESOURCE_COUNT"))
	if err != nil || count != 1 && count != 4 && count != 16 && count != 64 {
		t.Fatal("count must be 1,4,16,64")
	}
	filters, err := strconv.Atoi(os.Getenv("ZP_LSP_RESOURCE_FILTERS"))
	if err != nil || filters != 0 && filters != 65536 {
		t.Fatal("filters must be 0 or65536")
	}
	result := runRequestResourceCase(t, count, filters, true)
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("resource %s", raw)
}
