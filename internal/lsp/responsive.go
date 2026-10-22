package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
)

// The actor alone mutates documents and writes protocol output. Analysis and
// request workers receive immutable, versioned copies and return results to it.
type responsiveActor struct {
	server   *Server
	ctx      context.Context
	cancel   context.CancelFunc
	requests map[string]*snapshotRequest
	results  chan snapshotResponse
	workers  sync.WaitGroup
	slots    chan struct{}
	writeErr error
}

type snapshotRequest struct {
	message  incoming
	snapshot analysisSnapshot
	ctx      context.Context
	cancel   context.CancelFunc
	started  bool
}
type snapshotResponse struct {
	request *snapshotRequest
	value   any
	err     *rpcError
}
type readResult struct {
	body []byte
	err  error
}

// The responsive actor admits at most responsiveMaxRequests logical requests, of which at most
// responsiveRequestWorkers execute at once; see design 042, "Editor Workload Probe" for the
// measurements behind these values. Tests and probes may change them before a session starts.
var (
	responsiveRequestWorkers = 4
	responsiveMaxRequests    = 64
)

// requestHook is an optional controlled barrier for protocol tests.
var requestHook func(context.Context, string)

func (s *Server) runResponsive() error {
	ctx, cancel := context.WithCancel(context.Background())
	actor := &responsiveActor{server: s, ctx: ctx, cancel: cancel, requests: map[string]*snapshotRequest{}, results: make(chan snapshotResponse, responsiveMaxRequests), slots: make(chan struct{}, responsiveRequestWorkers)}
	s.actor = actor
	lintEnabled := s.client.Experimental.Pego.LintDiagnostics
	s.queue = newAnalysisQueue(func(ctx context.Context, snapshot analysisSnapshot) (*analysis, error) {
		a, err := analyzeContextSafely(ctx, snapshot.Text)
		if err != nil {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err := a.preparePublicationContext(ctx, snapshot, lintEnabled); err != nil {
			return nil, err
		}
		return a, nil
	})
	defer func() { cancel(); s.queue.stop(); actor.workers.Wait() }()
	incomingMessages := make(chan readResult)
	go func() {
		for {
			body, err := readMessage(s.in)
			select {
			case incomingMessages <- readResult{body, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
			// Once exit is read, do not begin another potentially blocking read.
			var message incoming
			if json.Unmarshal(body, &message) == nil && message.Method == "exit" {
				return
			}
		}
	}()
	analysisResults := s.queue.results
	for {
		select {
		case read := <-incomingMessages:
			if read.err != nil {
				if errors.Is(read.err, io.EOF) {
					return errors.New("the input ended before the exit notification")
				}
				return read.err
			}
			var message incoming
			if err := json.Unmarshal(read.body, &message); err != nil {
				if err := s.out.reply(nil, nil, &rpcError{Code: codeParseError, Message: "invalid JSON: " + err.Error()}); err != nil {
					return err
				}
				continue
			}
			if message.Method == "exit" {
				if s.state == stateShutdown {
					return nil
				}
				return ErrExitWithoutShutdown
			}
			if err := actor.handle(&message); err != nil {
				return err
			}
			if actor.writeErr != nil {
				return actor.writeErr
			}
		case result, ok := <-analysisResults:
			if !ok {
				analysisResults = nil
				continue
			}
			if s.state != stateInitialized {
				continue
			}
			d := s.docs[result.Snapshot.URI]
			if d == nil || !matchesSnapshot(d.snapshot(), result.Snapshot) {
				continue
			}
			if result.Err != nil {
				d.an = failedAnalysis(d.text, d.good, result.Err)
			} else {
				d.an = result.Analysis
				d.good = result.Analysis
			}
			diagnostics := d.an.diags
			if result.Err == nil {
				diagnostics = result.Analysis.publicationDiags
			}
			s.publishDiagnostics(d.uri, d.version, diagnostics)
			for _, request := range actor.requests {
				if request.snapshot.URI == d.uri && !request.started {
					actor.start(request)
				}
			}
			if actor.writeErr != nil {
				return actor.writeErr
			}
		case result := <-actor.results:
			if err := actor.complete(result); err != nil {
				return err
			}
		}
	}
}

func requestKey(id json.RawMessage) string {
	var value string
	if json.Unmarshal(id, &value) == nil {
		return "string:" + value
	}
	return "number:" + strings.TrimSpace(string(id))
}

func (a *responsiveActor) handle(message *incoming) error {
	s := a.server
	if message.JSONRPC != "2.0" || s.state != stateInitialized {
		return s.handle(message)
	}
	if !message.hasID() && message.Method == "$/cancelRequest" {
		var params struct {
			ID json.RawMessage `json:"id"`
		}
		if json.Unmarshal(message.Params, &params) == nil {
			if request := a.requests[requestKey(params.ID)]; request != nil {
				return a.finish(request, nil, &rpcError{Code: codeRequestCancelled, Message: "request cancelled"})
			}
		}
		return nil
	}
	if message.hasID() && message.Method == "shutdown" {
		for _, request := range a.requests {
			if err := a.finish(request, nil, &rpcError{Code: codeRequestCancelled, Message: "server shutting down"}); err != nil {
				return err
			}
		}
		s.queue.cancel()
		return s.handle(message)
	}
	if !message.hasID() || !strings.HasPrefix(message.Method, "textDocument/") && message.Method != "codeAction/resolve" {
		return s.handle(message)
	}
	if _, ok := requestHandlers[message.Method]; !ok {
		return s.handle(message)
	}
	// A code action to resolve names its document in the data the server gave it.
	var params struct {
		TextDocument textDocumentIdentifier `json:"textDocument"`
		Data         *findingData           `json:"data"`
	}
	if err := decode(message.Params, &params); err != nil {
		return s.out.reply(message.ID, nil, err)
	}
	uri := params.TextDocument.URI
	if message.Method == "codeAction/resolve" {
		if params.Data == nil {
			return s.out.reply(message.ID, nil, &rpcError{Code: codeInvalidParams, Message: "code action without data"})
		}
		uri = params.Data.URI
	}
	d, err := s.doc(uri)
	if err != nil {
		return s.out.reply(message.ID, nil, err)
	}
	key := requestKey(message.ID)
	if a.requests[key] != nil {
		return s.out.reply(message.ID, nil, &rpcError{Code: codeInvalidRequest, Message: "request ID already in flight"})
	}
	if len(a.requests) >= responsiveMaxRequests {
		return s.out.reply(message.ID, nil, &rpcError{Code: codeServerCancelled, Message: "server request capacity reached"})
	}
	ctx, cancel := context.WithCancel(a.ctx)
	request := &snapshotRequest{message: *message, snapshot: d.snapshot(), ctx: ctx, cancel: cancel}
	a.requests[key] = request
	if d.an != nil {
		a.start(request)
	} else {
		s.queue.prioritize(d.uri)
	}
	return nil
}

// start reserves execution capacity on the actor before creating a worker.
// Waiting requests stay in requests, without a worker or a document copy.
func (a *responsiveActor) start(request *snapshotRequest) {
	if request.started || len(a.slots) == cap(a.slots) {
		return
	}
	d := a.server.docs[request.snapshot.URI]
	if d == nil || !matchesSnapshot(d.snapshot(), request.snapshot) || d.an == nil {
		return
	}
	if d.an.text != d.text {
		_ = a.finish(request, nil, &rpcError{Code: codeContentModified, Message: "current document analysis unavailable"})
		return
	}
	copyDocument := *d
	worker := &Server{docs: map[string]*document{d.uri: &copyDocument}, state: stateInitialized, client: a.server.client, version: a.server.version, requestContext: request.ctx}
	request.started = true
	a.slots <- struct{}{}
	a.workers.Add(1)
	go func() {
		defer a.workers.Done()
		value, rerr := runSnapshotRequest(worker, request)
		// Cancellation acknowledges the logical request immediately, but only
		// this completion releases its execution slot. Even obsolete results
		// must reach the actor so pending requests can make progress.
		select {
		case a.results <- snapshotResponse{request: request, value: value, err: rerr}:
		case <-a.ctx.Done():
		}
	}()
}

func (a *responsiveActor) complete(result snapshotResponse) error {
	<-a.slots
	key := requestKey(result.request.message.ID)
	if a.requests[key] == result.request {
		d := a.server.docs[result.request.snapshot.URI]
		if d == nil || !matchesSnapshot(d.snapshot(), result.request.snapshot) {
			result.value = nil
			result.err = &rpcError{Code: codeContentModified, Message: "document changed during request"}
		}
		if err := a.finish(result.request, result.value, result.err); err != nil {
			return err
		}
	}
	// Scan the bounded admission map rather than retaining a second queue
	// containing canceled requests. A nil analysis leaves a request waiting
	// for its document's analysis result; started requests are already active.
	for _, request := range a.requests {
		a.start(request)
		if a.writeErr != nil {
			return a.writeErr
		}
		if len(a.slots) == cap(a.slots) {
			break
		}
	}
	return nil
}

func runSnapshotRequest(worker *Server, request *snapshotRequest) (value any, rerr *rpcError) {
	defer func() {
		if recover() != nil {
			value = nil
			rerr = &rpcError{Code: codeInternalError, Message: "request worker failed"}
		}
	}()
	if request.ctx.Err() != nil {
		return nil, &rpcError{Code: codeRequestCancelled, Message: "request cancelled"}
	}
	if requestHook != nil {
		requestHook(request.ctx, request.message.Method)
	}
	if request.ctx.Err() != nil {
		return nil, &rpcError{Code: codeRequestCancelled, Message: "request cancelled"}
	}
	return worker.request(&request.message)
}

func (a *responsiveActor) finish(request *snapshotRequest, value any, rerr *rpcError) error {
	key := requestKey(request.message.ID)
	if a.requests[key] != request {
		return nil
	}
	delete(a.requests, key)
	request.cancel()
	err := a.server.out.reply(request.message.ID, value, rerr)
	if err != nil && a.writeErr == nil {
		a.writeErr = err
	}
	return err
}

func (a *responsiveActor) invalidate(uri string) {
	for _, request := range a.requests {
		if request.snapshot.URI == uri {
			_ = a.finish(request, nil, &rpcError{Code: codeContentModified, Message: "document changed during request"})
		}
	}
}
