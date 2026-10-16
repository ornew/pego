package lsp

import (
	"context"
	"encoding/json"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

// The calling test is the actor: no event loop concurrently reads its maps.
// A cancellation acknowledgement frees logical admission before a handler's
// indivisible cancellation tail has returned.
func TestResponsiveCancelledTailsKeepNewRequestsPending(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	const uri = "file:///queue.pego"
	const source = `rule main = "a"`
	d := &document{uri: uri, version: 1, text: source, an: analyze(source)}
	s := &Server{out: &conn{w: io.Discard}, docs: map[string]*document{uri: d}, state: stateInitialized}
	a := &responsiveActor{server: s, ctx: ctx, cancel: cancel, requests: map[string]*snapshotRequest{}, results: make(chan snapshotResponse, 64), slots: make(chan struct{}, 4)}
	started := make(chan struct{}, 4)
	tails := make(chan struct{}, 4)
	release := make(chan struct{})
	requestHook = func(ctx context.Context, method string) {
		started <- struct{}{}
		<-ctx.Done()
		tails <- struct{}{}
		<-release
	}
	defer func() { cancel(); close(release); a.workers.Wait(); requestHook = nil }()
	admit := func(id int) {
		t.Helper()
		raw, err := json.Marshal(at(uri, 0, 5))
		if err != nil {
			t.Fatal(err)
		}
		msg := incoming{JSONRPC: "2.0", ID: json.RawMessage(strconv.Itoa(id)), Method: "textDocument/hover", Params: raw}
		if err := a.handle(&msg); err != nil {
			t.Fatal(err)
		}
	}
	for id := 1; id <= 4; id++ {
		admit(id)
	}
	for range 4 {
		awaitBarrier(t, started)
	}
	for _, r := range a.requests {
		if err := a.finish(r, nil, &rpcError{Code: codeRequestCancelled}); err != nil {
			t.Fatal(err)
		}
	}
	for range 4 {
		awaitBarrier(t, tails)
	}
	if len(a.requests) != 0 || len(a.slots) != 4 {
		t.Fatal("cancellation tail fixture did not retain four workers")
	}
	for id := 100; id < 164; id++ {
		admit(id)
	}
	if len(a.requests) != 64 {
		t.Fatalf("logical requests=%d want64", len(a.requests))
	}
	dispatched := 0
	for _, r := range a.requests {
		if r.started {
			dispatched++
		}
	}
	if dispatched != 0 {
		t.Fatalf("dispatched %d new requests while four cancelled workers still occupy execution slots", dispatched)
	}
}

// Exercise real framing as well as the actor-owned saturation reproduction.
// Late completions must release capacity without canceling a reused ID, and a
// canceled pending request must never call a handler.
func TestResponsivePendingRequestsResumeProtocol(t *testing.T) {
	for _, mode := range []string{"cancel", "panic"} {
		t.Run(mode, func(t *testing.T) {
			var hold atomic.Bool
			hold.Store(true)
			started := make(chan struct{}, 4)
			tails := make(chan struct{}, 4)
			release := make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			requestHook = func(ctx context.Context, method string) {
				if !hold.Load() {
					return
				}
				started <- struct{}{}
				<-ctx.Done()
				tails <- struct{}{}
				<-release
				if mode == "panic" {
					panic("controlled canceled worker failure")
				}
			}
			peer := newResourcePeer(t)
			defer func() { unblock(); peer.abort(); requestHook = nil }()
			c := peer.client
			c.request("initialize", map[string]any{"capabilities": map[string]any{"experimental": map[string]any{"pego": map[string]any{"responsiveAnalysis": true}}}})
			uri := "file:///queue.pego"
			c.open(uri, `rule main = "a"`)
			for id := 100; id < 104; id++ {
				asyncRequest(c, id, "textDocument/hover", at(uri, 0, 5))
			}
			for range 4 {
				awaitBarrier(t, started)
			}
			for id := 100; id < 104; id++ {
				c.notify("$/cancelRequest", map[string]any{"id": id})
			}
			for id := 100; id < 104; id++ {
				assertResponseError(t, c.response(strconv.Itoa(id)), codeRequestCancelled)
			}
			for range 4 {
				awaitBarrier(t, tails)
			}
			// All four old operations still hold their slots. The two live requests
			// must resume solely from obsolete completion events, with no new analysis.
			asyncRequest(c, 200, "textDocument/hover", at(uri, 0, 5))
			asyncRequest(c, 201, "textDocument/hover", at(uri, 0, 5))
			asyncRequest(c, 100, "textDocument/hover", at(uri, 0, 5))
			c.notify("$/cancelRequest", map[string]any{"id": 201})
			assertResponseError(t, c.response("201"), codeRequestCancelled)
			asyncRequest(c, 9000, "queue/sequence", nil)
			assertResponseError(t, c.response("9000"), codeMethodNotFound)
			hold.Store(false)
			unblock()
			seen := map[string]bool{}
			for range 2 {
				reply := c.next()
				id := string(reply["id"])
				if id != "100" && id != "200" || seen[id] || len(reply["error"]) > 0 || string(reply["result"]) == "null" {
					t.Fatalf("unexpected queued reply: %s", reply)
				}
				seen[id] = true
			}
			peer.shutdown()
		})
	}
}

func TestResponsivePendingInvalidationProtocol(t *testing.T) {
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	requestHook = func(ctx context.Context, method string) { started <- struct{}{}; <-release }
	peer := newResourcePeer(t)
	defer func() { unblock(); peer.abort(); requestHook = nil }()
	c := peer.client
	c.request("initialize", map[string]any{"capabilities": map[string]any{"experimental": map[string]any{"pego": map[string]any{"responsiveAnalysis": true}}}})
	uri := "file:///invalidate-pending.pego"
	c.open(uri, `rule main = "a"`)
	for id := 100; id < 104; id++ {
		asyncRequest(c, id, "textDocument/hover", at(uri, 0, 5))
	}
	for range 4 {
		awaitBarrier(t, started)
	}
	asyncRequest(c, 200, "textDocument/hover", at(uri, 0, 5))
	c.notify("textDocument/didClose", docParams(uri))
	seen := map[string]bool{}
	for range 5 {
		reply := c.next()
		id := string(reply["id"])
		// Closing also publishes empty diagnostics; it may precede the responses.
		if reply["method"] != nil {
			reply = c.next()
			id = string(reply["id"])
		}
		if id != "100" && id != "101" && id != "102" && id != "103" && id != "200" || seen[id] {
			t.Fatalf("unexpected invalidation reply: %s", reply)
		}
		assertResponseError(t, reply, codeContentModified)
		seen[id] = true
	}
	unblock()
	peer.shutdown()
}

func TestResponsiveCancelledDispatchSkipsHandler(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	requestHook = func(context.Context, string) { called = true }
	defer func() { requestHook = nil }()
	value, err := runSnapshotRequest(nil, &snapshotRequest{ctx: ctx})
	if value != nil || err == nil || err.Code != codeRequestCancelled || called {
		t.Fatalf("canceled dispatch value=%v error=%v called=%t", value, err, called)
	}
}
