package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// streamCases are the grammars with a #stream repetition of the generated-code tests, and the checked-in
// grammars that have one.
func streamCases(t *testing.T) []genCase {
	t.Helper()
	cases := append([]genCase(nil), streamCorpus...)
	for _, c := range genCorpus(t) {
		if strings.Contains(c.src, "#stream") {
			cases = append(cases, c)
		}
	}
	for _, name := range []string{"csv"} {
		src, err := os.ReadFile(filepath.Join("..", "..", "parsers", name, name+".pego"))
		if err != nil {
			t.Fatal(err)
		}
		c := genCase{name: name, src: string(src), inputs: []string{"", "\ufeffa,b\r\n", "a,\"x\"\"y\"\n\n", "a,\"unterminated", "a\rb\r\n"}}
		files, _ := filepath.Glob(filepath.Join("..", "..", "parsers", name, "testdata", "*.txt"))
		for _, f := range files {
			in, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			if len(in) <= 3<<10 {
				c.inputs = append(c.inputs, string(in))
			}
		}
		cases = append(cases, c)
	}
	return cases
}

// streamForms are the builds that the stream tests run: ordinary, with work controls, and observable.
var streamForms = []struct {
	name              string
	controls, observe bool
}{{"ordinary", false, false}, {"controls", true, false}, {"observability", false, true}, {"observability+controls", true, true}}

// TestFlowStreamBodyIsBatchBody generates ParseStream with the stream body of the start rule printed from
// Flow IR and checks the form of the body, in every form: with the statements of the stream removed (the
// start of the stream before the repetition, and the delivery of each element in place of gathering it) and
// the methods it calls expanded, it is the text of the rule's batch body. The batch bodies are the ones
// the Node-table parity tests compare with dgen's, and the stream body of a build with the direct printer
// (directStream) is not comparable by text: generator.expr writes another form, and the tests that run the
// parsers compare the behavior.
func TestFlowStreamBodyIsBatchBody(t *testing.T) { eachMode(t, testFlowStreamBodyIsBatchBody) }

func testFlowStreamBodyIsBatchBody(t *testing.T) {
	flowN := 0
	var fallbacks, flowed []string
	var cur string
	streamRouteHook = func(table string, r *rule) { flowN++; flowed = append(flowed, cur) }
	streamFallbackHook = func(table string, r *rule) { fallbacks = append(fallbacks, cur) }
	t.Cleanup(func() { streamRouteHook, streamFallbackHook = nil, nil })
	for _, form := range streamForms {
		for _, c := range streamCases(t) {
			t.Run(c.name+"/"+form.name, func(t *testing.T) {
				g, err := parseTest(c.src)
				if err != nil {
					t.Fatal(err)
				}
				cur = c.name + "/" + form.name
				before := flowN
				code, err := generateCode(g, GenOptions{Package: "x", Start: "main", Stream: true, SourceInfo: true, WorkControls: form.controls, Observability: form.observe})
				if err != nil {
					if strings.Contains(err.Error(), "needs a #stream repetition") {
						t.Skip("the #stream of the grammar is not at the top of the start rule")
					}
					t.Fatal(err)
				}
				if flowN == before {
					return // a start rule that keeps the direct printer
				}
				src := string(code)
				stream, batch := bodyClosure(t, src, streamBodyRef), bodyClosure(t, src, batchBodyRef)
				if err := streamPlacement(src); err != nil {
					t.Errorf("the stream statements are misplaced: %v", err)
				}
				stripped := streamStatements.ReplaceAllString(stream, "")
				if stripped == stream {
					t.Fatal("the stream body has no stream statement")
				}
				if stripped != batch {
					t.Errorf("without its stream statements, the stream body differs from the batch body:\n%s", firstLineDifference(stripped, batch))
				}
			})
		}
	}
	streamRouteHook, streamFallbackHook = nil, nil
	if flowN == 0 {
		t.Fatal("no stream body is printed from Flow IR: the test exercises nothing")
	}
	t.Logf("%d stream bodies from Flow IR; %d kept by the direct printer: %v", flowN, len(fallbacks), fallbacks)
	// A start rule keeps the direct printer only if its body recovers; a left-recursion leader takes the
	// Flow route, because the runtime owns its growth and the body is the same framed matcher.
	for _, want := range []string{"recovery in the start rule"} {
		if !slices.ContainsFunc(fallbacks, func(f string) bool { return strings.HasPrefix(f, want+"/") }) {
			t.Errorf("the start rule of %q is expected to keep the direct printer, but did not fall back", want)
		}
	}
	for _, want := range []string{"left recursive start", "left recursive start choice", "indirect leader start", "mutual leader start"} {
		if !slices.ContainsFunc(flowed, func(f string) bool { return strings.HasPrefix(f, want+"/") }) {
			t.Errorf("the left-recursive start rule of %q is expected to print from Flow IR, but did not", want)
		}
		if slices.ContainsFunc(fallbacks, func(f string) bool { return strings.HasPrefix(f, want+"/") }) {
			t.Errorf("the left-recursive start rule of %q fell back to the direct printer", want)
		}
	}
}

// firstLineDifference describes the first line at which a and b differ.
func firstLineDifference(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(al) && i < len(bl); i++ {
		if al[i] != bl[i] {
			return fmt.Sprintf("line %d:\n flow   %s\n direct %s", i+1, al[i], bl[i])
		}
	}
	return fmt.Sprintf("lengths %d and %d lines", len(al), len(bl))
}

var (
	streamBodyRef  = regexp.MustCompile(`(?m)^\tstreamRule\.Body = func\(p \*rt\.Parser, _ int\) \(\*rt\.Node, bool\) \{ return (e\d+)\(p\) \}$`)
	batchBodyRef   = regexp.MustCompile(`(?m)^\trules\[\d+\]\.Body = func\(p \*rt\.Parser, _ int\) \(\*rt\.Node, bool\) \{ return (e\d+)\(p\) \}$`)
	streamMethodRe = regexp.MustCompile(`(?ms)^func (e\d+)\(p \*rt\.Parser\) \(\*rt\.Node, bool\) \{\n.*?^\}\n`)
	methodRefRe    = regexp.MustCompile(`\be\d+\b`)
	scopeVarRe     = regexp.MustCompile(`\bscope\d+\b`)
	// streamStatements are the statements that the stream repetition adds to the batch body.
	streamStatements = regexp.MustCompile(`(?m)^\t+if p\.Stream != nil && p\.Stream\.Emit != nil && p\.Depth == 1 \{\n(\t+p\.StreamStart\(\)\n|\t+p\.EmitStream\(.*\)\n\t+if p\.Pos == x\d+\.Pos \{\n\t+break\n\t+\}\n\t+continue\n)\t+\}\n`)
)

// bodyClosure returns the text of the method that the first line of the generated code matching ref names,
// with the methods it calls expanded (recursion is cut at the second call).
func bodyClosure(t *testing.T, src string, ref *regexp.Regexp) string {
	t.Helper()
	m := ref.FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("no body line %v in the generated code", ref)
	}
	methods := map[string]string{}
	for _, m := range streamMethodRe.FindAllStringSubmatch(src, -1) {
		methods[m[1]] = m[0]
	}
	var expand func(name string, stack map[string]bool) string
	expand = func(name string, stack map[string]bool) string {
		text, ok := methods[name]
		if !ok || stack[name] {
			return "<" + name + ">"
		}
		stack[name] = true
		defer delete(stack, name)
		lines := strings.SplitN(text, "\n", 2) // the head names the method
		return "{" + methodRefRe.ReplaceAllStringFunc(lines[1], func(n string) string { return expand(n, stack) }) + "}"
	}
	// The package variables of capture names are numbered in the order the methods are printed.
	return scopeVarRe.ReplaceAllString(expand(m[1], map[string]bool{}), "scope")
}

// TestGeneratedFlowStreamMatchesEngine runs the generated ParseStream, with the stream body printed from Flow
// IR and with the direct printer, against the engine's ParseStream, in every form: the emitted elements, the
// reader calls made before each, the diagnostics and the final error, for reader chunkings, reader errors,
// callback errors, iterators and both position units (testGeneratedStreams). The route must be taken, so
// that the test cannot pass without exercising it.
func TestGeneratedFlowStreamMatchesEngine(t *testing.T) {
	eachMode(t, testGeneratedFlowStreamMatchesEngine)
}

func testGeneratedFlowStreamMatchesEngine(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	cases := streamCases(t)
	taken := 0
	streamRouteHook = func(table string, r *rule) { taken++ }
	t.Cleanup(func() { streamRouteHook = nil })
	for _, form := range streamForms {
		for _, direct := range []bool{false, true} {
			name := form.name + "/flow"
			if direct {
				name = form.name + "/direct"
			}
			t.Run(name, func(t *testing.T) {
				before := taken
				testGeneratedStreams(t, goBin, cases, GenOptions{WorkControls: form.controls, Observability: form.observe, directStream: direct})
				if direct == (taken > before) {
					t.Errorf("stream bodies printed from Flow IR: %d, with directStream %v", taken-before, direct)
				}
			})
		}
	}
}

// streamProbeMain is the consumer of the generated stream packages: for each input and chunk size it prints
// the number and hash of the trace events, the hash of the profile (without times) and of the emitted
// elements with the number of reader calls before each, and the error.
const streamProbeMain = `package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	pegort "github.com/ornew/pego/runtime"
IMPORTS)

var entries = []func(io.Reader, func(*pegort.Node) error, ...pegort.ParseOption) error{
ENTRIES}

func sum(v any) string {
	b, _ := json.Marshal(v)
	return fmt.Sprintf("%x", sha256.Sum256(b))[:16]
}

type reader struct {
	s     string
	chunk int
	reads *int
}

func (r *reader) Read(b []byte) (int, error) {
	*r.reads++
	if r.s == "" {
		return 0, io.EOF
	}
	n := len(r.s)
	if r.chunk > 0 && n > r.chunk {
		n = r.chunk
	}
	if n > len(b) {
		n = len(b)
	}
	copy(b, r.s[:n])
	r.s = r.s[n:]
	return n, nil
}

func main() {
	var inputs [][]string
	data, _ := os.ReadFile(os.Args[1])
	json.Unmarshal(data, &inputs)
	for i := range inputs {
		for j, in := range inputs[i] {
			for _, chunk := range []int{0, 1, 5} {
				var events []pegort.TraceEvent
				var elems []any
				reads := 0
				emit := func(n *pegort.Node) error { elems = append(elems, [2]any{reads, n}); return nil }
				err := entries[i](&reader{s: in, chunk: chunk, reads: &reads}, emit, pegort.WithTrace(func(e pegort.TraceEvent) { events = append(events, e) }))
				var prof pegort.Profile
				reads = 0
				elems2 := 0
				perr := entries[i](&reader{s: in, chunk: chunk, reads: &reads}, func(*pegort.Node) error { elems2++; return nil }, pegort.WithProfile(&prof))
				snap := prof.Snapshot()
				snap.Time = 0
				for k := range snap.Rules {
					snap.Rules[k].Time, snap.Rules[k].SelfTime = 0, 0
				}
				fmt.Printf("%d/%d/%d stream events=%d trace=%s profile=%s elems=%d,%s emitted=%d err=%q perr=%q\n", i, j, chunk, len(events), sum(events), sum(snap), len(elems), sum(elems), elems2, fmt.Sprint(err), fmt.Sprint(perr))
				_ = strings.Contains
			}
		}
	}
}
`

var streamProbeLine = regexp.MustCompile(`^(\S+) stream events=(\d+) trace=(\w+) profile=(\w+) elems=(\d+),(\w+) emitted=(\d+) err=(.*) perr=(.*)$`)

// runStreamProbes builds the cases with a stream and observability as the packages f0..fN (the stream body printed
// from Flow IR, mutated by mutate if not nil) and d0..dN (directStream), and returns the report lines of each; the
// lines of a mutant are nil if its consumer fails.
func runStreamProbes(t *testing.T, cases []genCase, form struct {
	name              string
	controls, observe bool
}, mutate func([]byte) []byte) (flowOut, directOut []string) {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", testGoMod("streamprobe"))
	var inputs [][]string
	var kept []genCase
	for _, c := range cases {
		g, err := parseTest(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		i := len(kept)
		var codes [2][]byte
		for k, direct := range []bool{false, true} {
			o := GenOptions{directStream: direct, Package: fmt.Sprintf("%s%d", "fd"[k:k+1], i), Start: "main", Stream: true, SourceInfo: true, WorkControls: form.controls, Observability: form.observe}
			codes[k], err = generateCode(g, o)
			if err != nil {
				break
			}
		}
		if err != nil {
			if strings.Contains(err.Error(), "needs a #stream repetition") {
				continue
			}
			t.Fatalf("%s: %v", c.name, err)
		}
		if mutate != nil {
			codes[0] = mutate(codes[0])
		}
		write(fmt.Sprintf("f%d/parser.go", i), string(codes[0]))
		write(fmt.Sprintf("d%d/parser.go", i), string(codes[1]))
		kept = append(kept, c)
		inputs = append(inputs, c.inputs)
	}
	run := func(prefix string) []string {
		var imports, entries strings.Builder
		for i := range kept {
			fmt.Fprintf(&imports, "\t%s%d \"streamprobe/%s%d\"\n", prefix, i, prefix, i)
			fmt.Fprintf(&entries, "\t%s%d.ParseStream,\n", prefix, i)
		}
		write("main.go", strings.NewReplacer("IMPORTS", imports.String(), "ENTRIES", entries.String()).Replace(streamProbeMain))
		data, _ := json.Marshal(inputs)
		write("inputs.json", string(data))
		cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", ".", "inputs.json")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -trimpath", "GOTOOLCHAIN=local")
		var stderr strings.Builder
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			if mutate != nil && prefix == "f" {
				return nil // the mutant breaks the parse: a failure the comparison sees
			}
			t.Fatalf("go run (%s): %v\n%s", prefix, err, stderr.String())
		}
		var lines []string
		for _, l := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
			if !streamProbeLine.MatchString(l) {
				t.Fatalf("go run (%s): malformed probe line %q", prefix, l)
			}
			lines = append(lines, l)
		}
		return lines
	}
	return run("f"), run("d")
}

// streamReportDiffs returns the ids of the reports that differ.
func streamReportDiffs(t *testing.T, f, d []string) (differing []string) {
	t.Helper()
	if len(f) != len(d) || len(f) == 0 {
		t.Fatalf("%d reports from the Flow build and %d from the direct build", len(f), len(d))
	}
	events, elems := 0, 0
	for i := range f {
		if f[i] != d[i] {
			differing = append(differing, f[i][:strings.Index(f[i], " ")])
			if len(differing) <= 3 {
				t.Logf("report %d differs:\n flow   %s\n direct %s", i, f[i], d[i])
			}
		}
		m := streamProbeLine.FindStringSubmatch(d[i]) // the unmutated build
		var n, e int
		fmt.Sscanf(m[2], "%d", &n)
		fmt.Sscanf(m[5], "%d", &e)
		events, elems = events+n, elems+e
	}
	if events == 0 || elems == 0 {
		t.Fatalf("the reports have %d events and %d elements", events, elems)
	}
	return differing
}

// TestFlowStreamObservationsMatchDirect checks what a stream parse observes and emits: the trace events, the
// profile, the elements with the reader calls made before each, and the error of the Flow stream body equal
// those of the direct printer's, for each stream grammar, each reader chunking and each form with observability.
func TestFlowStreamObservationsMatchDirect(t *testing.T) {
	eachMode(t, testFlowStreamObservationsMatchDirect)
}

func testFlowStreamObservationsMatchDirect(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	cases := streamCases(t)
	for _, form := range streamForms {
		if !form.observe {
			continue
		}
		t.Run(form.name, func(t *testing.T) {
			f, d := runStreamProbes(t, cases, form, nil)
			if diff := streamReportDiffs(t, f, d); len(diff) > 0 {
				t.Errorf("%d reports differ between the Flow and the direct stream body: %v", len(diff), diff)
			}
		})
	}
}

// TestFlowStreamObservationMutants checks that the comparison of stream parses notices a stream body that
// forgets to start the stream, drops the delivery of an element, delivers at the wrong position or delivers
// after the zero-width check instead of before.
func TestFlowStreamObservationMutants(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	t.Setenv(modeEnv, "1")
	cases := streamCases(t)
	form := streamForms[2]
	f, d := runStreamProbes(t, cases, form, nil)
	if diff := streamReportDiffs(t, f, d); len(diff) > 0 {
		t.Fatalf("the unmutated builds differ: %v", diff)
	}
	startStmt := regexp.MustCompile(`(?m)^\t+p\.StreamStart\(\)\n`)
	emitStmt := regexp.MustCompile(`(?m)^\t+p\.EmitStream\(.*\)\n`)
	reorderEmit := regexp.MustCompile(`(?m)^(\t+)(p\.EmitStream\(.*\)\n)(\t+if p\.Pos == x\d+\.Pos \{\n\t+break\n\t+\}\n)`)
	for _, m := range []struct {
		name   string
		mutate func(string) string
	}{
		{"no stream start", func(s string) string { return startStmt.ReplaceAllString(s, "") }},
		{"no delivery", func(s string) string { return emitStmt.ReplaceAllString(s, "") }},
		{"delivery after the zero-width check", func(s string) string {
			return reorderEmit.ReplaceAllString(s, "$3$2")
		}},
		{"delivery at the wrong position", func(s string) string {
			return emitStmt.ReplaceAllStringFunc(s, func(l string) string { return strings.Replace(l, ", p.Pos)", ", p.Pos+1)", 1) })
		}},
	} {
		t.Run(m.name, func(t *testing.T) {
			mf, _ := runStreamProbes(t, cases, form, func(code []byte) []byte {
				out := m.mutate(string(code))
				return []byte(out)
			})
			if mf == nil {
				return // the consumer of the mutant failed
			}
			if diff := streamReportDiffs(t, mf, d); len(diff) == 0 {
				t.Errorf("the mutant %q changes no report: the stream is not compared", m.name)
			}
		})
	}
}

// streamPlacement checks the placement of the stream statements in generated code that has one stream body (two
// variants of it, ordinary and controlled, in a work-controlled build): each variant has exactly one StreamStart
// and one delivery (EmitStream), the delivery inside a loop and the start outside that loop and before it.
// The comparison with the batch body cannot see where a statement is, as it removes them.
func streamPlacement(src string) error {
	starts, emits := strings.Count(src, "p.StreamStart()"), strings.Count(src, "p.EmitStream(")
	if starts != emits || starts < 1 || starts > 2 {
		return fmt.Errorf("%d stream starts and %d deliveries, want one of each per variant", starts, emits)
	}
	if n := len(streamStatements.FindAllString(src, -1)); n != 2*starts {
		return fmt.Errorf("%d guarded stream statements, want %d", n, 2*starts)
	}
	var method string
	for _, m := range streamMethodRe.FindAllString(src, -1) {
		if strings.Contains(m, "p.EmitStream(") {
			method = m
		}
	}
	if strings.Count(method, "p.StreamStart()") != starts || strings.Count(method, "p.EmitStream(") != emits {
		return fmt.Errorf("the stream statements are in different methods")
	}
	lines := strings.Split(method, "\n")
	var startAt, emitAt []int
	for i, l := range lines {
		if strings.Contains(l, "p.EmitStream(") {
			emitAt = append(emitAt, i)
		}
		if strings.Contains(l, "p.StreamStart()") {
			startAt = append(startAt, i)
		}
	}
	for k, emit := range emitAt {
		// The loop that holds the delivery: scan out from it through the enclosing blocks.
		depth, head := 0, -1
		for i := emit - 1; i >= 0 && head < 0; i-- {
			l := strings.TrimSpace(lines[i])
			closes, opens := strings.HasPrefix(l, "}"), strings.HasSuffix(l, "{")
			switch {
			case closes && opens:
			case closes:
				depth++
			case opens:
				if depth > 0 {
					depth--
				} else if strings.HasPrefix(l, "for ") {
					head = i
				}
			}
		}
		if head < 0 {
			return fmt.Errorf("delivery %d is not inside a loop", k)
		}
		if startAt[k] >= head {
			return fmt.Errorf("stream start %d is not before the loop of its delivery", k)
		}
		if k > 0 && startAt[k] < emitAt[k-1] {
			return fmt.Errorf("stream start %d lies before delivery %d", k, k-1)
		}
	}
	return nil
}

// TestFlowStreamPlacementMutants checks that the placement check notices a stream start inside the loop and a
// duplicated delivery, which the comparison with the batch body and the parses do not tell from the
// original.
func TestFlowStreamPlacementMutants(t *testing.T) {
	eachMode(t, func(t *testing.T) {
		g, err := parseTest(streamCorpus[0].src)
		if err != nil {
			t.Fatal(err)
		}
		code, err := generateCode(g, GenOptions{Package: "x", Start: "main", Stream: true, SourceInfo: true})
		if err != nil {
			t.Fatal(err)
		}
		src := string(code)
		if err := streamPlacement(src); err != nil {
			t.Fatalf("the generated code: %v", err)
		}
		startBlock := regexp.MustCompile(`(?m)^\t+if p\.Stream != nil && p\.Stream\.Emit != nil && p\.Depth == 1 \{\n\t+p\.StreamStart\(\)\n\t+\}\n`)
		emitBlock := regexp.MustCompile(`(?m)^\t+if p\.Stream != nil && p\.Stream\.Emit != nil && p\.Depth == 1 \{\n\t+p\.EmitStream\(.*\)\n\t+if p\.Pos == x\d+\.Pos \{\n\t+break\n\t+\}\n\t+continue\n\t+\}\n`)
		sb, eb := startBlock.FindString(src), emitBlock.FindString(src)
		if sb == "" || eb == "" {
			t.Fatal("no stream blocks in the generated code")
		}
		for _, m := range []struct{ name, code string }{
			{"start inside the loop", strings.Replace(strings.Replace(src, sb, "", 1), eb, sb+eb, 1)},
			{"duplicated delivery", strings.Replace(src, eb, eb+eb, 1)},
			{"start after the loop", strings.Replace(strings.Replace(src, sb, "", 1), eb, eb+sb, 1)},
		} {
			if err := streamPlacement(m.code); err == nil {
				t.Errorf("%s: the placement check passes", m.name)
			}
		}
	})
}
