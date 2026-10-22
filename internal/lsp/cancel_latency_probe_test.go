package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// cancelTrial runs work with a context that is cancelled after delay and reports how long work
// kept running after the cancellation. cancelled is false when work finished first.
func cancelTrial(delay time.Duration, work func(context.Context) error) (latency time.Duration, cancelled bool) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var at atomic.Int64
	timer := time.AfterFunc(delay, func() {
		at.Store(time.Now().UnixNano())
		cancel()
	})
	err := work(ctx)
	end := time.Now().UnixNano()
	timer.Stop()
	if !errors.Is(err, context.Canceled) || at.Load() == 0 {
		return 0, false
	}
	return time.Duration(end - at.Load()), true
}

type latencyStats []time.Duration

func (l latencyStats) quantile(q float64) time.Duration {
	if len(l) == 0 {
		return 0
	}
	s := append(latencyStats(nil), l...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[min(len(s)-1, int(q*float64(len(s))))]
}

// TestCancelLatencyProbe (PEGO_LSP_CANCEL_PROBE=1, optional PEGO_LSP_CANCEL_TRIALS) cancels
// responsive analysis and each analysed request at uniformly random moments of a run and reports
// the time from the cancellation to the return of the work. Unlike the poll-interval probe, which
// records the gaps between checks, this measures the delay a cancellation actually sees, including
// scheduling and garbage collection. Cancellations land uniformly in time, so a phase is hit in
// proportion to its duration.
func TestCancelLatencyProbe(t *testing.T) {
	if os.Getenv("PEGO_LSP_CANCEL_PROBE") != "1" {
		t.Skip("manual cancellation latency probe")
	}
	if requestHook != nil || analyzeHook != nil {
		t.Fatal("cancellation latency probe requires inactive hooks")
	}
	trials := 300
	if v, err := strconv.Atoi(os.Getenv("PEGO_LSP_CANCEL_TRIALS")); err == nil && v > 0 {
		trials = v
	}
	names := []string{"duckdb/duckdb.pego", "typescript/typescript.pego", "python/python.pego", "golang/golang.pego"}
	rng := rand.New(rand.NewSource(1))
	const uri = "file:///probe.pego"
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join("../../parsers", name))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		snapshot := analysisSnapshot{URI: uri, Text: text, Version: 1, Generation: 1}
		analysisWork := func(ctx context.Context) error {
			a, err := analyzeContextSafely(ctx, text)
			if err == nil {
				err = a.preparePublicationContext(ctx, snapshot, true)
			}
			return err
		}
		report := func(what string, full time.Duration, work func(context.Context) error) {
			var lat latencyStats
			missed := 0
			for range trials {
				delay := time.Duration(rng.Int63n(int64(full)))
				if l, ok := cancelTrial(delay, work); ok {
					lat = append(lat, l)
				} else {
					missed++
				}
			}
			t.Logf("%s %s full=%v cancelled=%d/%d p50=%v p90=%v p99=%v max=%v", filepath.Base(name), what, full.Round(time.Microsecond), len(lat), trials,
				lat.quantile(.5).Round(time.Microsecond), lat.quantile(.9).Round(time.Microsecond), lat.quantile(.99).Round(time.Microsecond), lat.quantile(1).Round(time.Microsecond))
		}
		// context.Background has a nil Done channel, which selects the ordinary paths.
		// Time the controlled path with a context that is never cancelled.
		live, stop := context.WithCancel(context.Background())
		full := func() time.Duration {
			best := time.Duration(1 << 62)
			for range 3 {
				start := time.Now()
				if err := analysisWork(live); err != nil {
					t.Fatal(err)
				}
				best = min(best, time.Since(start))
			}
			return best
		}()
		stop()
		report("analysis", full, analysisWork)
		if os.Getenv("PEGO_LSP_CANCEL_REQUESTS") == "0" {
			continue
		}
		a, err := analyzeContext(context.Background(), text)
		if err != nil {
			t.Fatal(err)
		}
		for method, params := range pollProbeRequests(text) {
			raw, err := json.Marshal(params)
			if err != nil {
				t.Fatal(err)
			}
			work := func(ctx context.Context) error {
				s := &Server{docs: map[string]*document{uri: {uri: uri, text: text, version: 1, generation: 1, an: a}}, state: stateInitialized, requestContext: ctx}
				_, rerr := s.request(&incoming{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: method, Params: raw})
				if rerr != nil && rerr.Code == codeRequestCancelled {
					return context.Canceled
				}
				if rerr != nil {
					t.Fatal(method, rerr.Message)
				}
				return nil
			}
			live, stop := context.WithCancel(context.Background())
			start := time.Now()
			best := time.Duration(1 << 62)
			for range 5 {
				start = time.Now()
				if err := work(live); err != nil {
					t.Fatal(err)
				}
				best = min(best, time.Since(start))
			}
			stop()
			if best < 20*time.Microsecond {
				continue // too short for a timer-based cancellation to land inside it
			}
			report(method, best, work)
		}
	}
}
