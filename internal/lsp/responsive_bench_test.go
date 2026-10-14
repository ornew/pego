package lsp

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// BenchmarkF08Analyze compares the same successful compilation with and without
// the complete payload preparation performed by the responsive worker.
// Inputs and subbenchmark names match the earlier lint-cache/mapping baseline;
// candidate lint timings include preparing and retaining the publication payload.
// It excludes the actor, JSON-RPC, editor, and client command application.
func BenchmarkF08Analyze(b *testing.B) {
	if analyzeHook != nil || requestHook != nil || lintDiagnosticHook != nil {
		b.Fatal("analysis benchmarks require inactive test hooks")
	}
	fixtures := []struct{ name, source string }{{name: "generated", source: f08BenchmarkGrammar()}}
	for _, name := range []string{"golang/golang.pego", "python/python.pego"} {
		data, err := os.ReadFile(filepath.Join("../../parsers", name))
		if err != nil {
			b.Fatal(err)
		}
		fixtures = append(fixtures, struct{ name, source string }{filepath.Dir(name), string(data)})
	}
	for _, fixture := range fixtures {
		b.Run(fixture.name, func(b *testing.B) {
			validated := analyze(fixture.source)
			if validated.prog == nil || len(validated.diags) != 0 {
				b.Fatalf("benchmark grammar is invalid: %+v", validated.diags)
			}
			b.Logf("source bytes=%d sha256=%x", len(fixture.source), sha256.Sum256([]byte(fixture.source)))
			for _, withLint := range []bool{false, true} {
				name := "analyze"
				if withLint {
					name = "lint-diagnostics"
				}
				b.Run(name, func(b *testing.B) {
					b.ReportAllocs()
					var result *analysis
					var diagnostics []Diagnostic
					expectedCount := 0
					if withLint {
						document := document{uri: "file:///benchmark.pego", version: 1, generation: 1, text: fixture.source, an: validated}
						expectedCount = len(document.lintDiagnostics())
					}
					for b.Loop() {
						result = analyze(fixture.source)
						if withLint {
							snapshot := analysisSnapshot{URI: "file:///benchmark.pego", Version: 1, Generation: 1, Text: fixture.source}
							if err := result.preparePublicationContext(context.Background(), snapshot, true); err != nil {
								b.Fatal(err)
							}
							diagnostics = result.publicationDiags
						}
					}
					if withLint {
						if result.prog == nil || len(result.diags) != 0 || !result.lintCached {
							b.Fatalf("worker preparation failed: %+v", result.diags)
						}
						if len(diagnostics) != expectedCount {
							b.Fatalf("publication diagnostic count=%d; baseline mapping count=%d", len(diagnostics), expectedCount)
						}
						b.ReportMetric(float64(len(diagnostics)), "findings/op")
					}
					runtime.KeepAlive(result)
					runtime.KeepAlive(diagnostics)
				})
			}
		})
	}
}

// f08BenchmarkGrammar is deterministic, valid current syntax. Every generated
// rule is reachable and has a nonempty literal, so this measures editor source
// handling without producing a quadratic ordered-choice lint workload.
func f08BenchmarkGrammar() string {
	var source strings.Builder
	source.WriteString("rule main = ")
	for i := 0; i < 1024; i++ {
		fmt.Fprintf(&source, "entry%04d ", i)
	}
	source.WriteByte('\n')
	for i := 0; i < 1024; i++ {
		fmt.Fprintf(&source, "rule entry%04d = \"token%04d\"\n", i, i)
	}
	for source.Len() < 64<<10 {
		source.WriteString("// editor source payload 😀 abcdefghijklmnopqrstuvwxyz\n")
	}
	return source.String()
}

type f08BurstMetrics struct {
	pendingDelta, drainedDelta, closedDelta int64
	heldPending, heldGenerations, completed int
}

// BenchmarkF08Burst measures a queue plus explicitly owned document analyses,
// not the JSON-RPC actor. The first worker is held BEFORE tokenization/analysis
// starts, at a deliberately non-cooperative barrier, while pending work is
// replaced. This is a constructed coalescing state, not active-parser cancellation. Setup, forced GC and heap sampling
// are outside timing; ns/op and allocations cover the burst and final analyses.
// Signed post-GC deltas use the idle live heap as their reference, not RSS, peak
// heap, or an observed maximum. Held counts sample only the constructed barrier
// state; there is no byte-throughput metric for partially discarded work.
func BenchmarkF08Burst(b *testing.B) {
	if analyzeHook != nil || requestHook != nil {
		b.Fatal("burst benchmarks require inactive test hooks")
	}
	source := f08BenchmarkGrammar()
	validated := analyze(source)
	if validated.prog == nil || len(validated.diags) != 0 {
		b.Fatalf("benchmark grammar is invalid: %+v", validated.diags)
	}
	b.Logf("base source bytes=%d sha256=%x", len(source), sha256.Sum256([]byte(source)))
	for _, documents := range []int{1, 4} {
		for _, edits := range []int{32, 256} {
			b.Run(fmt.Sprintf("docs%d/edits%d", documents, edits), func(b *testing.B) {
				b.ReportAllocs()
				var pending, drained, closed int64
				var heldPending, heldGenerations, completed int
				batches := 0
				for b.Loop() {
					metrics := f08BurstBatch(b, source, documents, edits)
					pending += metrics.pendingDelta
					drained += metrics.drainedDelta
					closed += metrics.closedDelta
					heldPending += metrics.heldPending
					heldGenerations += metrics.heldGenerations
					completed += metrics.completed
					batches++
					// B.Loop must be entered with the timer running. The batch
					// leaves it stopped after its untimed heap observations.
					b.StartTimer()
				}
				b.ReportMetric(float64(pending)/float64(batches), "held-postgc-delta-B")
				b.ReportMetric(float64(drained)/float64(batches), "drained-postgc-delta-B")
				b.ReportMetric(float64(closed)/float64(batches), "closed-postgc-delta-B")
				b.ReportMetric(float64(heldPending)/float64(batches), "held-pending")
				b.ReportMetric(float64(heldGenerations)/float64(batches), "held-generations")
				b.ReportMetric(float64(completed)/float64(batches), "completed/op")
			})
		}
	}
	runtime.KeepAlive(validated)
}

func f08LiveHeap() uint64 {
	runtime.GC()
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	return memory.HeapAlloc
}

func f08SnapshotText(source string, document, version int) string {
	// Construct on demand; no fixture array retains obsolete snapshots.
	return source + "// snapshot document=" + strconv.Itoa(document) + " version=" + strconv.Itoa(version) + "\n"
}

func f08BurstBatch(b *testing.B, source string, count, edits int) f08BurstMetrics {
	b.StopTimer()
	documents := make(map[string]*document, count)
	uris := make([]string, count)
	for i := range uris {
		uris[i] = "file:///burst" + strconv.Itoa(i) + ".pego"
		text := f08SnapshotText(source, i, 1)
		a := analyze(text)
		if a.prog == nil || len(a.diags) != 0 {
			b.Fatalf("invalid idle grammar: %+v", a.diags)
		}
		a.lintCache = a.lintFindings()
		a.lintCached = true
		documents[uris[i]] = &document{uri: uris[i], version: 1, text: text, an: a, good: a}
	}
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseWork := func() { releaseOnce.Do(func() { close(release) }) }
	queue := newAnalysisQueue(func(ctx context.Context, snapshot analysisSnapshot) (*analysis, error) {
		if snapshot.Generation == 1 {
			close(started)
			<-release
		}
		a, err := analyzeContext(ctx, snapshot.Text)
		if err != nil {
			return nil, err
		}
		a.lintCache = a.lintFindings()
		a.lintCached = true
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return a, nil
	})
	defer func() { releaseWork(); queue.stop() }()
	documents[uris[0]].generation = 1
	queue.submit(documents[uris[0]].snapshot())
	<-started
	idle := f08LiveHeap()
	generation := uint64(1)
	b.StartTimer()
	for version := 2; version < edits+2; version++ {
		for i, uri := range uris {
			generation++
			d := documents[uri]
			d.text = f08SnapshotText(source, i, version)
			d.version, d.generation, d.an = version, generation, nil
			queue.submit(d.snapshot())
		}
	}
	b.StopTimer()
	queue.mu.Lock()
	metrics := f08BurstMetrics{heldPending: len(queue.pending), heldGenerations: len(queue.pending) + 1}
	valid := queue.runningURI == uris[0] && len(queue.pending) == count && len(queue.order) == count
	for uri, pending := range queue.pending {
		if !matchesSnapshot(documents[uri].snapshot(), pending.snapshot) {
			valid = false
		}
	}
	queue.mu.Unlock()
	if !valid {
		b.Fatal("queue did not coalesce to one current pending generation per document")
	}
	metrics.pendingDelta = int64(f08LiveHeap()) - int64(idle)
	runtime.KeepAlive(documents)
	b.StartTimer()
	releaseWork()
	for range count {
		result := <-queue.results
		if result.Err != nil || result.Analysis == nil {
			b.Fatalf("analysis failed: %v", result.Err)
		}
		d := documents[result.Snapshot.URI]
		if d == nil || d.an != nil || !matchesSnapshot(d.snapshot(), result.Snapshot) {
			b.Fatal("obsolete or duplicate analysis completed")
		}
		d.an, d.good = result.Analysis, result.Analysis
		if len(d.lintDiagnostics()) != 0 {
			b.Fatal("generated grammar unexpectedly produced lint findings")
		}
		metrics.completed++
	}
	b.StopTimer()
	metrics.drainedDelta = int64(f08LiveHeap()) - int64(idle)
	runtime.KeepAlive(documents)
	queue.stop()
	clear(documents)
	metrics.closedDelta = int64(f08LiveHeap()) - int64(idle)
	runtime.KeepAlive(queue)
	return metrics
}

// BenchmarkF08ProtocolCancel measures the real opt-in actor's in-process framed
// JSON-RPC cancel response while requestHook performs synthetic active CPU work.
// A barrier after the first polling chunk orders cancellation without sleeps.
// Synthetic work polls every 256 bytes; stop timestamps mark that hook's exit,
// not the entire request worker's teardown or parsing/compiler/lint termination.
// ns/op includes the cancel round trip and waiting for the synthetic stop record.
// Custom latency metrics are means; they do not describe tails. The chunk count
// includes work before cancellation and only proves active synthetic progress.
// These latencies cannot establish cancellation within uninstrumented phases.
func BenchmarkF08ProtocolCancel(b *testing.B) {
	if analyzeHook != nil || requestHook != nil {
		b.Fatal("cancellation benchmark requires inactive hooks")
	}
	b.StopTimer()
	peer := newF08BenchmarkPeer(b)
	defer func() { peer.abort(); requestHook = nil }()
	peer.send("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": "file:///cancel-benchmark.pego", "version": 1, "text": "rule main = \"a\""}}, false)
	notification := peer.receive()
	if notification.Method != "textDocument/publishDiagnostics" {
		b.Fatalf("unexpected initialization message: %+v", notification)
	}
	var diagnostics publishDiagnosticsParams
	if err := json.Unmarshal(notification.Params, &diagnostics); err != nil || len(diagnostics.Diagnostics) != 0 {
		b.Fatalf("invalid initial diagnostics: %s", notification.Params)
	}
	active := make(chan struct{}, 1)
	stopped := make(chan f08SyntheticStop, 1)
	source := f08BenchmarkGrammar()
	requestHook = func(ctx context.Context, method string) {
		if method != "textDocument/hover" {
			return
		}
		var checksum uint64
		chunks := 0
		offset := 0
		first := true
		for {
			for range 256 {
				checksum = checksum*131 + uint64(source[offset])
				offset++
				if offset == len(source) {
					offset = 0
				}
			}
			chunks++
			if first {
				active <- struct{}{}
				first = false
			}
			if ctx.Err() != nil {
				break
			}
		}
		stopped <- f08SyntheticStop{at: time.Now(), checksum: checksum, chunks: chunks}
	}
	b.ReportAllocs()
	var responseTime, stopTime time.Duration
	iterations := 0
	var totalChunks int
	var checksum uint64
	b.StartTimer() // Setup ended with the timer stopped; B.Loop requires it running.
	for b.Loop() {
		b.StopTimer()
		id := peer.send("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": "file:///cancel-benchmark.pego"}, "position": Position{Line: 0, Character: 5}}, true)
		<-active
		b.StartTimer()
		cancelAt := time.Now()
		peer.send("$/cancelRequest", map[string]any{"id": id}, false)
		response := peer.receive()
		repliedAt := time.Now()
		synthetic := <-stopped
		b.StopTimer()
		var cancellation rpcError
		if string(response.ID) != strconv.Itoa(id) || json.Unmarshal(response.Error, &cancellation) != nil || cancellation.Code != codeRequestCancelled {
			b.Fatalf("unexpected cancellation reply: id=%s error=%s", response.ID, response.Error)
		}
		responseTime += repliedAt.Sub(cancelAt)
		stopTime += synthetic.at.Sub(cancelAt)
		if synthetic.chunks < 1 {
			b.Fatal("synthetic work did not start")
		}
		totalChunks += synthetic.chunks
		checksum ^= synthetic.checksum
		iterations++
		b.StartTimer() // Validation and metric accumulation are outside timed work.
	}
	b.ReportMetric(float64(responseTime.Nanoseconds())/float64(iterations), "cancel-to-reply-ns")
	b.ReportMetric(float64(stopTime.Nanoseconds())/float64(iterations), "cancel-to-synthetic-stop-ns")
	b.ReportMetric(float64(totalChunks)/float64(iterations), "synthetic-total-chunks/op")
	b.Logf("synthetic checksum=%016x; cancellation polls every 256 bytes", checksum)
	peer.shutdown()
}

type f08SyntheticStop struct {
	at       time.Time
	checksum uint64
	chunks   int
}

type f08BenchmarkPeer struct {
	b         *testing.B
	input     *bufio.Reader
	output    *conn
	readPipe  *io.PipeReader
	writePipe *io.PipeWriter
	done      chan error
	nextID    int
	closed    bool
}

func newF08BenchmarkPeer(b *testing.B) *f08BenchmarkPeer {
	reader, serverOutput := io.Pipe()
	serverInput, writer := io.Pipe()
	peer := &f08BenchmarkPeer{b: b, input: bufio.NewReader(reader), output: &conn{w: writer}, readPipe: reader, writePipe: writer, done: make(chan error, 1)}
	go func() {
		err := Serve(serverInput, serverOutput, "benchmark")
		serverOutput.Close()
		serverInput.Close()
		peer.done <- err
	}()
	b.Cleanup(peer.abort)
	id := peer.send("initialize", map[string]any{"capabilities": map[string]any{"experimental": map[string]any{"pego": map[string]any{"responsiveAnalysis": true}}}}, true)
	reply := peer.receive()
	if string(reply.ID) != strconv.Itoa(id) || len(reply.Error) > 0 {
		b.Fatalf("initialize failed: id=%s error=%s", reply.ID, reply.Error)
	}
	return peer
}

func (p *f08BenchmarkPeer) send(method string, params any, request bool) int {
	message := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
	id := 0
	if request {
		p.nextID++
		id = p.nextID
		message["id"] = id
	}
	if err := p.output.write(message); err != nil {
		p.b.Fatal(err)
	}
	return id
}

func (p *f08BenchmarkPeer) receive() incoming {
	body, err := readMessage(p.input)
	if err != nil {
		p.b.Fatal(err)
	}
	var message incoming
	if err := json.Unmarshal(body, &message); err != nil {
		p.b.Fatal(err)
	}
	return message
}

func (p *f08BenchmarkPeer) shutdown() {
	id := p.send("shutdown", nil, true)
	response := p.receive()
	if string(response.ID) != strconv.Itoa(id) || len(response.Error) > 0 {
		p.b.Fatalf("shutdown failed: %+v", response)
	}
	p.send("exit", nil, false)
	err := <-p.done
	p.closed = true
	p.readPipe.Close()
	p.writePipe.Close()
	if err != nil {
		p.b.Fatal(err)
	}
}

func (p *f08BenchmarkPeer) abort() {
	if p.closed {
		return
	}
	p.readPipe.Close()
	p.writePipe.Close()
	<-p.done
	p.closed = true
}
