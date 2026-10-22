package lsp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// pollRecorder is a never-cancelled context that records the wall-clock interval between
// consecutive cancellation checks and where each check was made. Its Done channel is non-nil, so
// the controlled paths run. The longest interval bounds how long work continues after a
// cancellation, apart from the work before the first check and after the last.
type pollRecorder struct {
	context.Context
	last     time.Time
	lastSite string
	longest  pollInterval
	polls    int
	// big lists every interval of at least bigThreshold, by from/to site, with its count and sum.
	big map[string]*bigInterval
}

const bigThreshold = 300 * time.Microsecond

type bigInterval struct {
	n        int
	sum, max time.Duration
}

type pollInterval struct {
	d        time.Duration
	from, to string
}

// pollDepth is the number of callers named in a site.
var pollDepth = 2

var pollRecorderDone = make(chan struct{})

func newPollRecorder() *pollRecorder {
	return &pollRecorder{Context: context.Background(), last: time.Now(), lastSite: "start"}
}

func (c *pollRecorder) Err() error            { c.record(); return nil }
func (c *pollRecorder) Done() <-chan struct{} { c.record(); return pollRecorderDone }

func (c *pollRecorder) record() {
	now := time.Now()
	site := pollSite()
	d := now.Sub(c.last)
	if d > c.longest.d {
		c.longest = pollInterval{d, c.lastSite, site}
	}
	if d >= bigThreshold {
		if c.big == nil {
			c.big = map[string]*bigInterval{}
		}
		key := pollFuncs(c.lastSite) + " -> " + pollFuncs(site)
		if c.big[key] == nil {
			c.big[key] = &bigInterval{}
		}
		c.big[key].n++
		c.big[key].sum += d
		c.big[key].max = max(c.big[key].max, d)
	}
	c.last, c.lastSite = now, site
	c.polls++
}

// finish records the end of the work as a final check.
func (c *pollRecorder) finish() pollInterval {
	c.record()
	return c.longest
}

// pollSite names the two innermost callers of the check, skipping the check and poll helpers
// themselves so that a gap is attributed to the code that polls.
func pollSite() string {
	pc := make([]uintptr, 8)
	frames := runtime.CallersFrames(pc[:runtime.Callers(4, pc)])
	var site []string
	for len(site) < pollDepth {
		f, more := frames.Next()
		name := f.Function[strings.LastIndex(f.Function, "/")+1:]
		if !strings.HasSuffix(name, ".check") && !strings.HasSuffix(name, ".poll") && !strings.HasSuffix(name, ".step") {
			site = append(site, name+":"+strconv.Itoa(f.Line))
		}
		if !more {
			break
		}
	}
	return strings.Join(site, " < ")
}

// pollFuncs drops line numbers from a site, so that gaps between the same functions add up.
func pollFuncs(site string) string {
	return regexp.MustCompile(`:\d+`).ReplaceAllString(site, "")
}

// shortestLongest runs work three times and returns the run with the shortest longest interval,
// which discounts intervals stretched by scheduling or garbage collection.
func shortestLongest(work func(context.Context)) (pollInterval, int) {
	var best pollInterval
	var polls int
	for i := range 3 {
		c := newPollRecorder()
		work(c)
		if longest := c.finish(); i == 0 || longest.d < best.d {
			best, polls = longest, c.polls
		}
	}
	return best, polls
}

// TestPollIntervalProbe reports the longest interval between cancellation checks in responsive
// analysis, with lint, and in each analysed request, for the grammars in the repository.
func TestPollIntervalProbe(t *testing.T) {
	if os.Getenv("PEGO_LSP_POLL_PROBE") != "1" {
		t.Skip("manual cancellation poll-interval probe")
	}
	if requestHook != nil || analyzeHook != nil {
		t.Fatal("poll-interval probe requires inactive hooks")
	}
	files, err := filepath.Glob("../../parsers/*/*.pego")
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, "../../pego.pego")
	// Single large tokens: work proportional to one token runs between per-token checks.
	const large = 256 << 10
	sources := map[string]string{
		"f08-benchmark": f08BenchmarkGrammar(),
		"large-literal": `rule main = "` + strings.Repeat("a", large) + `"` + "\n",
		"large-comment": "// " + strings.Repeat("a", large) + "\nrule main = \"a\"\n",
		"large-class":   "rule main = [" + strings.Repeat("a-b", large/3) + "]\n",
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		sources[filepath.Base(f)] = string(b)
	}
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	const uri = "file:///probe.pego"
	snapshot := func(text string) analysisSnapshot {
		return analysisSnapshot{URI: uri, Text: text, Version: 1, Generation: 1}
	}
	for _, name := range names {
		text := sources[name]
		a, err := analyzeContext(context.Background(), text)
		if err != nil {
			t.Fatal(name, err)
		}
		longest, polls := shortestLongest(func(ctx context.Context) {
			a, err := analyzeContext(ctx, text)
			if err == nil {
				err = a.preparePublicationContext(ctx, snapshot(text), true)
			}
			if err != nil {
				t.Fatal(name, err)
			}
		})
		t.Logf("%s bytes=%d analysis polls=%d longest=%v %s -> %s", name, len(text), polls, longest.d, longest.from, longest.to)
		if os.Getenv("PEGO_LSP_POLL_SITES") == "1" {
			// A gap that recurs in every run is structural; scheduling and garbage collection
			// stretch different gaps in different runs. List the gaps between functions whose
			// largest gap in each of the runs stays above the threshold, with the smallest of
			// those largest gaps.
			if d, err := strconv.Atoi(os.Getenv("PEGO_LSP_POLL_DEPTH")); err == nil {
				pollDepth = d
			}
			const runs = 15
			floor := map[string]time.Duration{}
			seen := map[string]int{}
			for range runs {
				c := newPollRecorder()
				a, err := analyzeContext(c, text)
				if err == nil {
					err = a.preparePublicationContext(c, snapshot(text), true)
				}
				if err != nil {
					t.Fatal(name, err)
				}
				c.finish()
				for key, b := range c.big {
					if seen[key] == 0 || b.max < floor[key] {
						floor[key] = b.max
					}
					seen[key]++
				}
			}
			for key, n := range seen {
				if n == runs {
					t.Logf("%s recurring gap >= %v: %v %s", name, bigThreshold, floor[key], key)
				}
			}
		}
		for method, params := range pollProbeRequests(text) {
			raw, err := json.Marshal(params)
			if err != nil {
				t.Fatal(err)
			}
			longest, polls := shortestLongest(func(ctx context.Context) {
				s := &Server{docs: map[string]*document{uri: {uri: uri, text: text, version: 1, generation: 1, an: a}}, state: stateInitialized, requestContext: ctx}
				if _, rerr := s.request(&incoming{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: method, Params: raw}); rerr != nil {
					t.Fatal(name, method, rerr.Message)
				}
			})
			t.Logf("%s %s polls=%d longest=%v %s -> %s", name, method, polls, longest.d, longest.from, longest.to)
		}
	}
}

// pollProbeRequests positions name-based requests on the middle occurrence of the most-referenced rule.
func pollProbeRequests(text string) map[string]any {
	best, count := "", 0
	for _, m := range regexp.MustCompile(`(?m)^\s*rule\s+([A-Za-z_][A-Za-z0-9_]*)`).FindAllStringSubmatch(text, -1) {
		if n := strings.Count(text, m[1]); n > count {
			best, count = m[1], n
		}
	}
	locations := regexp.MustCompile(`\b`+best+`\b`).FindAllStringIndex(text, -1)
	offset := locations[len(locations)/2][0]
	idx := newTextIndex(text)
	position := idx.position(offset)
	document := map[string]any{"uri": "file:///probe.pego"}
	at := map[string]any{"textDocument": document, "position": position}
	end := idx.position(len(text))
	return map[string]any{
		"textDocument/semanticTokens/full": map[string]any{"textDocument": document},
		"textDocument/documentSymbol":      map[string]any{"textDocument": document},
		"textDocument/formatting":          map[string]any{"textDocument": document, "options": map[string]any{"tabSize": 4, "insertSpaces": true}},
		"textDocument/hover":               at,
		"textDocument/definition":          at,
		"textDocument/documentHighlight":   at,
		"textDocument/completion":          at,
		"textDocument/prepareRename":       at,
		"textDocument/references":          map[string]any{"textDocument": document, "position": position, "context": map[string]any{"includeDeclaration": true}},
		"textDocument/rename":              map[string]any{"textDocument": document, "position": position, "newName": best + "Renamed"},
		"textDocument/codeAction":          map[string]any{"textDocument": document, "range": Range{End: end}, "context": map[string]any{"diagnostics": []any{}}},
	}
}
