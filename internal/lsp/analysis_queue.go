package lsp

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// analysisSnapshot and analysisQueue implement the worker boundary described in
// design 042. The protocol actor remains the sole owner of documents and output;
// this queue never reads documents or writes JSON-RPC messages.
type analysisSnapshot struct {
	URI        string
	Text       string
	Version    int
	Generation uint64
}

type analysisResult struct {
	Snapshot analysisSnapshot
	Analysis *analysis
	Err      error
}

type pendingAnalysis struct {
	snapshot analysisSnapshot
	cancel   context.CancelFunc
	ctx      context.Context
}

// analysisQueue bounds work globally to one running snapshot and one replaceable
// pending snapshot per URI. A worker must cooperate with its supplied context.
type analysisQueue struct {
	mu            sync.Mutex
	pending       map[string]pendingAnalysis
	order         []string
	runningURI    string
	runningCancel context.CancelFunc
	wake          chan struct{}
	results       chan analysisResult
	ctx           context.Context
	cancel        context.CancelFunc
	done          chan struct{}
	work          func(context.Context, analysisSnapshot) (*analysis, error)
}

func newAnalysisQueue(work func(context.Context, analysisSnapshot) (*analysis, error)) *analysisQueue {
	ctx, cancel := context.WithCancel(context.Background())
	q := &analysisQueue{pending: map[string]pendingAnalysis{}, wake: make(chan struct{}, 1), results: make(chan analysisResult, 1), ctx: ctx, cancel: cancel, done: make(chan struct{}), work: work}
	go q.run()
	return q
}

func (q *analysisQueue) submit(snapshot analysisSnapshot) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.ctx.Err() != nil {
		return
	}
	if old, ok := q.pending[snapshot.URI]; ok {
		old.cancel()
	} else {
		q.order = append(q.order, snapshot.URI)
	}
	if q.runningURI == snapshot.URI && q.runningCancel != nil {
		q.runningCancel()
	}
	ctx, cancel := context.WithCancel(q.ctx)
	q.pending[snapshot.URI] = pendingAnalysis{snapshot: snapshot, cancel: cancel, ctx: ctx}
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// prioritize moves a pending analysis of uri to the front of the queue, so that a request waiting
// for it is not delayed by analyses of documents that changed earlier.
func (q *analysisQueue) prioritize(uri string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, ok := q.pending[uri]; !ok {
		return
	}
	for i := range q.order {
		if q.order[i] == uri {
			copy(q.order[1:i+1], q.order[:i])
			q.order[0] = uri
			return
		}
	}
}

func (q *analysisQueue) closeDocument(uri string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if pending, ok := q.pending[uri]; ok {
		pending.cancel()
		delete(q.pending, uri)
		for i := range q.order {
			if q.order[i] == uri {
				copy(q.order[i:], q.order[i+1:])
				q.order[len(q.order)-1] = ""
				q.order = q.order[:len(q.order)-1]
				break
			}
		}
	}
	if q.runningURI == uri && q.runningCancel != nil {
		q.runningCancel()
	}
}

func (q *analysisQueue) stop() { q.cancel(); <-q.done }

func (q *analysisQueue) run() {
	defer close(q.done)
	defer close(q.results)
	for {
		if q.ctx.Err() != nil {
			return
		}
		q.mu.Lock()
		var next pendingAnalysis
		found := false
		for len(q.order) > 0 {
			uri := q.order[0]
			q.order[0] = ""
			q.order = q.order[1:]
			if next, found = q.pending[uri]; found {
				delete(q.pending, uri)
				q.runningURI, q.runningCancel = uri, next.cancel
				break
			}
		}
		q.mu.Unlock()
		if !found {
			select {
			case <-q.ctx.Done():
				return
			case <-q.wake:
				continue
			}
		}
		a, err := q.workSafely(next.ctx, next.snapshot)
		cancelled := next.ctx.Err() != nil
		next.cancel()
		q.mu.Lock()
		q.runningURI, q.runningCancel = "", nil
		q.mu.Unlock()
		// Cancellation is independent of stale-result validation by the actor.
		if cancelled {
			if q.ctx.Err() != nil {
				return
			}
			continue
		}
		if err == nil && a != nil {
			select {
			case q.results <- analysisResult{Snapshot: next.snapshot, Analysis: a}:
			case <-q.ctx.Done():
				return
			}
		} else if !errors.Is(err, context.Canceled) {
			select {
			case q.results <- analysisResult{Snapshot: next.snapshot, Err: err}:
			case <-q.ctx.Done():
				return
			}
		}
		if q.ctx.Err() != nil {
			return
		}
	}
}

// matchesSnapshot guards publication even when a phase could not stop promptly.
func matchesSnapshot(current, completed analysisSnapshot) bool {
	return current.URI == completed.URI && current.Version == completed.Version && current.Generation == completed.Generation
}

func (q *analysisQueue) workSafely(ctx context.Context, snapshot analysisSnapshot) (a *analysis, err error) {
	defer func() {
		if failure := recover(); failure != nil {
			a, err = nil, fmt.Errorf("analysis worker failed: %v", failure)
		}
	}()
	return q.work(ctx, snapshot)
}
