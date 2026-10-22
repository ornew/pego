package lsp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"
)

// editorSession drives a responsive server the way an editor does while a user types: one
// didChange per keystroke, followed by the requests an editor issues for the new version
// (semantic tokens, whose previous request it cancels, document highlights and completion), and
// finally a burst of requests once typing stops. It reads replies as they arrive and timestamps
// them, so the result describes the latencies the editor sees rather than server-side phases.
type editorSession struct {
	t          *testing.T
	peer       *resourcePeer
	uri        string
	text       string
	nextID     int
	sent       map[int]editorRequest
	replies    map[int]editorReply
	versions   map[int]time.Time // publication time by diagnostics version
	pubs       int
	lastPub    int // highest version published so far
	duplicates int // replies to an id that already had one, or to an id never sent
	reordered  int // publications of a version below an earlier one
	peakOut    int
	out        chan []byte
	written    chan struct{}
}

type editorRequest struct {
	method string
	at     time.Time
}

type editorReply struct {
	at   time.Time
	code int // 0 for a result
}

type editorResult struct {
	Interval, Keystrokes, Workers, MaxRequests int
	Source                                     string
	// FinalPublishMs is the time from the last keystroke to the diagnostics of the last version.
	FinalPublishMs float64
	Publications   int
	Reordered      int
	Duplicates     int // replies to an id that already had one, or to one never sent
	// Replies to the typing-phase requests by outcome.
	Results, ContentModified, Cancelled, Rejected int
	PeakOutstanding                               int
	// BatchMs is the time from sending the post-typing burst to the last reply; BatchP50Ms and
	// BatchMaxMs are per-request latencies of the burst.
	BatchRequests                   int
	BatchMs, BatchP50Ms, BatchMaxMs float64
	BatchFailures                   int
}

func newEditorSession(t *testing.T, text string) *editorSession {
	t.Helper()
	s := &editorSession{t: t, uri: "file:///probe.pego", text: "// \n" + text, sent: map[int]editorRequest{}, replies: map[int]editorReply{}, versions: map[int]time.Time{}, nextID: 100}
	s.peer = newResourcePeer(t)
	c := s.peer.client
	// Writes go through a queue, as in an editor, so that a server blocked writing replies
	// to a client that is busy sending cannot deadlock the session.
	s.out, s.written = make(chan []byte, 1<<16), make(chan struct{})
	go func() {
		defer close(s.written)
		for data := range s.out {
			if _, err := fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n%s", len(data), data); err != nil {
				return
			}
		}
	}()
	c.request("initialize", map[string]any{"capabilities": map[string]any{"experimental": map[string]any{"pego": map[string]any{"responsiveAnalysis": true, "lintDiagnostics": true}}}})
	c.notify("initialized", map[string]any{})
	if d := c.open(s.uri, s.text); d == nil {
		t.Fatal("no diagnostics for the opened grammar")
	}
	return s
}

func (s *editorSession) write(message any) {
	data, err := json.Marshal(message)
	if err != nil {
		s.t.Error(err)
		return
	}
	s.out <- data
}

func (s *editorSession) request(method string, params any) int {
	s.nextID++
	id := s.nextID
	s.sent[id] = editorRequest{method, time.Now()}
	s.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	return id
}

// record accounts for one message from the server: a publication or the reply to a request.
func (s *editorSession) record(m map[string]json.RawMessage) {
	now := time.Now()
	if _, ok := m["method"]; ok {
		var p publishDiagnosticsParams
		if json.Unmarshal(m["params"], &p) == nil && p.Version != nil {
			s.versions[*p.Version] = now
			s.pubs++
			if *p.Version < s.lastPub {
				s.reordered++
			}
			s.lastPub = max(s.lastPub, *p.Version)
		}
		return
	}
	id, err := strconv.Atoi(string(m["id"]))
	if err != nil {
		return
	}
	if _, sent := s.sent[id]; !sent {
		s.duplicates++
		return
	}
	if _, again := s.replies[id]; again {
		s.duplicates++
		return
	}
	reply := editorReply{at: now}
	if m["error"] != nil {
		var e rpcError
		_ = json.Unmarshal(m["error"], &e)
		reply.code = e.Code
	}
	s.replies[id] = reply
	if out := len(s.sent) - len(s.replies); out > s.peakOut {
		s.peakOut = out
	}
}

// waitFor reads messages until done reports true.
func (s *editorSession) waitFor(done func() bool) {
	for !done() {
		select {
		case m, ok := <-s.peer.client.msgs:
			if !ok {
				s.t.Fatal("the server closed the connection")
			}
			s.record(m)
		case <-time.After(30 * time.Second):
			s.t.Fatal("timed out waiting for the server")
		}
	}
}

// typeText sends keystrokes at the given interval with the requests of an editor after each, and
// reads the server's messages between keystrokes, so that no reader goroutine is needed.
func (s *editorSession) typeText(keystrokes int, interval time.Duration) (last time.Time, finalVersion int) {
	tokens := -1
	for i := range keystrokes {
		version := i + 2
		s.write(map[string]any{"jsonrpc": "2.0", "method": "textDocument/didChange", "params": map[string]any{
			"textDocument":   map[string]any{"uri": s.uri, "version": version},
			"contentChanges": []map[string]any{{"range": Range{Start: Position{Line: 0, Character: 3 + i}, End: Position{Line: 0, Character: 3 + i}}, "text": "x"}},
		}})
		last, finalVersion = time.Now(), version
		if tokens >= 0 {
			s.write(map[string]any{"jsonrpc": "2.0", "method": "$/cancelRequest", "params": map[string]any{"id": tokens}})
		}
		tokens = s.request("textDocument/semanticTokens/full", docParams(s.uri))
		s.request("textDocument/documentHighlight", at(s.uri, 5, 8))
		s.request("textDocument/completion", at(s.uri, 5, 8))
		deadline := time.Now().Add(interval)
		for drained := false; !drained; {
			select {
			case m, ok := <-s.peer.client.msgs:
				if !ok {
					s.t.Fatal("the server closed the connection")
				}
				s.record(m)
			default:
				drained = true
			}
		}
		for {
			wait := time.Until(deadline)
			if wait <= 0 {
				break
			}
			select {
			case m, ok := <-s.peer.client.msgs:
				if !ok {
					s.t.Fatal("the server closed the connection")
				}
				s.record(m)
			case <-time.After(wait):
			}
		}
	}
	return last, finalVersion
}

// burst sends one request per entry of methods, repeated, and waits for all replies.
func (s *editorSession) burst(text string, repeat int) (ids []int, start time.Time) {
	params := pollProbeRequests(s.text)
	methods := make([]string, 0, len(params))
	for method := range params {
		methods = append(methods, method)
	}
	sort.Strings(methods)
	start = time.Now()
	for range repeat {
		for _, method := range methods {
			ids = append(ids, s.request(method, params[method]))
		}
	}
	return ids, start
}

func milliseconds(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// runEditorSession types, waits for the last version's diagnostics, then bursts requests.
func runEditorSession(t *testing.T, text string, keystrokes int, interval time.Duration, workers, maxRequests int) editorResult {
	t.Helper()
	oldWorkers, oldMax := responsiveRequestWorkers, responsiveMaxRequests
	responsiveRequestWorkers, responsiveMaxRequests = workers, maxRequests
	defer func() { responsiveRequestWorkers, responsiveMaxRequests = oldWorkers, oldMax }()
	s := newEditorSession(t, text)
	defer s.peer.abort()
	result := editorResult{Interval: int(interval.Milliseconds()), Keystrokes: keystrokes, Workers: workers, MaxRequests: maxRequests, Source: strconv.Itoa(len(text))}
	last, final := s.typeText(keystrokes, interval)
	s.waitFor(func() bool {
		_, ok := s.versions[final]
		return ok && len(s.replies) == len(s.sent)
	})
	result.FinalPublishMs = milliseconds(s.versions[final].Sub(last))
	result.Publications, result.Reordered = s.pubs, s.reordered
	for _, reply := range s.replies {
		switch reply.code {
		case 0:
			result.Results++
		case codeContentModified:
			result.ContentModified++
		case codeRequestCancelled:
			result.Cancelled++
		case codeServerCancelled:
			result.Rejected++
		default:
			t.Errorf("unexpected error code %d", reply.code)
		}
	}
	result.PeakOutstanding = s.peakOut
	ids, start := s.burst(text, 4)
	s.waitFor(func() bool { return len(s.replies) == len(s.sent) })
	var lat []float64
	var end time.Time
	for _, id := range ids {
		r := s.replies[id]
		if r.code != 0 {
			result.BatchFailures++
		}
		lat = append(lat, milliseconds(r.at.Sub(s.sent[id].at)))
		if r.at.After(end) {
			end = r.at
		}
	}
	sort.Float64s(lat)
	result.BatchRequests = len(ids)
	result.BatchMs, result.BatchP50Ms, result.BatchMaxMs = milliseconds(end.Sub(start)), lat[len(lat)/2], lat[len(lat)-1]
	result.Duplicates = s.duplicates
	s.peer.shutdown()
	close(s.out)
	<-s.written
	return result
}

// TestEditorWorkloadProbe (PEGO_LSP_EDITOR_PROBE=1) runs editor sessions on the repository's
// large grammars. PEGO_LSP_EDITOR_GRAMMAR selects parsers/<name>/<name>.pego, PEGO_LSP_EDITOR_INTERVAL_MS
// the keystroke interval, PEGO_LSP_EDITOR_WORKERS and PEGO_LSP_EDITOR_MAX_REQUESTS the capacity.
func TestEditorWorkloadProbe(t *testing.T) {
	if os.Getenv("PEGO_LSP_EDITOR_PROBE") != "1" {
		t.Skip("manual editor workload probe")
	}
	env := func(name string, def int) int {
		if v, err := strconv.Atoi(os.Getenv(name)); err == nil {
			return v
		}
		return def
	}
	name := os.Getenv("PEGO_LSP_EDITOR_GRAMMAR")
	if name == "" {
		name = "duckdb"
	}
	data, err := os.ReadFile(filepath.Join("../../parsers", name, name+".pego"))
	if err != nil {
		t.Fatal(err)
	}
	result := runEditorSession(t, string(data), env("PEGO_LSP_EDITOR_KEYSTROKES", 60), time.Duration(env("PEGO_LSP_EDITOR_INTERVAL_MS", 20))*time.Millisecond, env("PEGO_LSP_EDITOR_WORKERS", 4), env("PEGO_LSP_EDITOR_MAX_REQUESTS", 64))
	raw, _ := json.Marshal(result)
	t.Logf("editor %s %s", name, raw)
}

// TestEditorSession types faster than the grammar can be analysed while issuing and cancelling
// requests for every version, at several capacities. Whatever the timing, the final version must
// be published exactly as the latest one, no version may be published after a later one, every
// request must receive exactly one reply with an allowed outcome, and a burst after typing must be
// answered completely.
func TestEditorSession(t *testing.T) {
	text := f08BenchmarkGrammar()
	for _, capacity := range []struct{ workers, maxRequests int }{{1, 64}, {4, 64}, {8, 48}} {
		t.Run(fmt.Sprintf("workers=%d/max=%d", capacity.workers, capacity.maxRequests), func(t *testing.T) {
			result := runEditorSession(t, text, 40, 0, capacity.workers, capacity.maxRequests)
			if result.Duplicates != 0 {
				t.Fatalf("a request received more than one reply: %+v", result)
			}
			if result.Reordered != 0 {
				t.Fatalf("a version was published after a later one: %+v", result)
			}
			if result.Publications < 1 {
				t.Fatalf("no diagnostics for the final version: %+v", result)
			}
			total := result.Results + result.ContentModified + result.Cancelled + result.Rejected
			if total != 3*40 {
				t.Fatalf("replies=%d want %d: %+v", total, 3*40, result)
			}
			if result.BatchFailures != 0 {
				t.Fatalf("burst requests failed: %+v", result)
			}
			if result.Rejected != 0 {
				t.Fatalf("requests rejected below the limit: %+v", result)
			}
		})
	}
}
