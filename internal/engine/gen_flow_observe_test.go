package engine

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
)

// The Node tables of an observability build are printed from Flow IR. What the build observes (the events of
// WithTrace and the counters of WithProfile) is checked three ways:
//   - against the engine: the rule calls of a parse, with their rules, levels, depths, positions, results,
//     memo flags and evaluations, are those of the engine's trace, except that a call the generated code does
//     not make because the next character cannot start the rule is reported as a skipped event instead of as
//     a call (the engine's backends skip such calls too, but not always the same ones);
//   - against itself: the counters of every rule in the profile are what the events of the same parse add up
//     to;
//   - against a golden, testdata/observation.golden, which holds a digest of the reports of every case. It
//     freezes what the engine cannot say: the skipped events, and the observations of the checked-in
//     grammars on damaged inputs. Rewrite it with -update after reviewing a change of what is observed.

// observeProbeMain is the consumer of the generated packages with the prefix PREFIX: for each input and entry
// it prints the number of trace events, the hash of the events and of the profile (without times), and the
// error. For the first detail[i] inputs of case i it also prints the events and the profile.
const observeProbeMain = `package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	pegort "github.com/ornew/pego/runtime"
IMPORTS)

var entries = []struct{ parse, recognize func(string, ...pegort.ParseOption) error }{
ENTRIES}

func sum(v any) string {
	b, _ := json.Marshal(v)
	return fmt.Sprintf("%x", sha256.Sum256(b))[:16]
}

func report(id, name string, run func(string, ...pegort.ParseOption) error, in string, detail bool) {
	var events []pegort.TraceEvent
	err := run(in, pegort.WithTrace(func(e pegort.TraceEvent) { events = append(events, e) }))
	var prof pegort.Profile
	perr := run(in, pegort.WithProfile(&prof))
	snap := prof.Snapshot()
	snap.Time = 0
	for i := range snap.Rules {
		snap.Rules[i].Time, snap.Rules[i].SelfTime = 0, 0
	}
	fmt.Printf("%s %s events=%d trace=%s profile=%s entered=%d err=%q perr=%q\n", id, name, len(events), sum(events), sum(snap), snap.Entered, fmt.Sprint(err), fmt.Sprint(perr))
	if detail {
		for _, e := range events {
			fmt.Printf("# %s %s %+v\n", id, name, e)
		}
		b, _ := json.Marshal(snap)
		fmt.Printf("# %s %s profile %s\n", id, name, strings.ReplaceAll(string(b), "\n", " "))
	}
}

func main() {
	var in struct {
		Inputs [][]string
		Detail []int
	}
	data, _ := os.ReadFile(os.Args[1])
	json.Unmarshal(data, &in)
	for i := range in.Inputs {
		for j, input := range in.Inputs[i] {
			id := fmt.Sprintf("%d/%d", i, j)
			report(id, "parse", entries[i].parse, input, j < in.Detail[i])
			report(id, "recognize", entries[i].recognize, input, j < in.Detail[i])
		}
	}
}
`

var observeLine = regexp.MustCompile(`^(\S+ \S+) events=(\d+) trace=(\w+) profile=(\w+) entered=(\d+) err=(.*) perr=(.*)$`)

func observeSources(pkg string, n int) string {
	var imports, entries strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&imports, "\t%s%d \"obsprobe/%s%d\"\n", pkg, i, pkg, i)
		fmt.Fprintf(&entries, `	{parse: func(in string, opts ...pegort.ParseOption) error {
		_, err := %[1]s%[2]d.Parse(in, opts...)
		return err
	}, recognize: func(in string, opts ...pegort.ParseOption) error { return %[1]s%[2]d.Recognize(in, opts...) }},
`, pkg, i)
	}
	return strings.NewReplacer("IMPORTS", imports.String(), "ENTRIES", entries.String()).Replace(observeProbeMain)
}

// runObserveProbes builds the cases as the packages f0..fN with observability, runs the consumer and
// returns its lines. The first detail[i] inputs of case i are reported with their events and profile; a nil
// detail reports none. mutate, if not nil, changes the code of every package.
func runObserveProbes(t *testing.T, cases []genCase, start string, detail []int, mutate func([]byte) []byte) []string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", testGoMod("obsprobe"))
	var inputs [][]string
	for i, c := range cases {
		g, err := parseTest(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		code, err := generateCode(g, gen.GenOptions{Package: fmt.Sprintf("f%d", i), Start: start, Recognize: true, Observability: true})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if mutate != nil {
			code = mutate(code)
		}
		write(fmt.Sprintf("f%d/parser.go", i), string(code))
		inputs = append(inputs, c.inputs)
	}
	write("main.go", observeSources("f", len(cases)))
	if detail == nil {
		detail = make([]int, len(cases))
	}
	data, _ := json.Marshal(struct {
		Inputs [][]string
		Detail []int
	}{inputs, detail})
	write("inputs.json", string(data))
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", ".", "inputs.json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -trimpath", "GOTOOLCHAIN=local")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, stderr.String())
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		if !observeLine.MatchString(l) && !strings.HasPrefix(l, "# ") {
			t.Fatalf("go run: malformed probe line %q", l)
		}
		lines = append(lines, l)
	}
	return lines
}

// observationGolden holds the digest of the reports of every case: the key names the mode, the group
// of cases and the case, and the value the number of events and the hash of its report lines.
const observationGolden = "testdata/observation.golden"

// observationDigests returns, for each case, the number of events of its reports and the hash of its report
// lines, keyed by group/mode/name. The reports are those of the lines in the order the consumer printed them.
func observationDigests(t *testing.T, group string, cases []genCase, lines []string) map[string]string {
	t.Helper()
	hashes := make([]hash.Hash, len(cases))
	events := make([]int, len(cases))
	for i := range hashes {
		hashes[i] = sha256.New()
	}
	for _, l := range lines {
		m := observeLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		var i int
		fmt.Sscanf(m[1], "%d/", &i)
		var n int
		fmt.Sscanf(m[2], "%d", &n)
		events[i] += n
		_, rest, _ := strings.Cut(l, "/") // the case number is not part of the digest
		io.WriteString(hashes[i], rest+"\n")
	}
	out := map[string]string{}
	for i, c := range cases {
		out[fmt.Sprintf("%s/%s/%s", group, os.Getenv(modeEnv), c.name)] = fmt.Sprintf("events=%d %x", events[i], hashes[i].Sum(nil)[:12])
	}
	return out
}

// observedEvent is an event of a generated parser, read from its probe line.
type observedEvent struct {
	kind                   int
	rule                   string
	level, depth, pos, end int
	evals                  int
	matched, memo          bool
}

var observedEventLine = regexp.MustCompile(`^# (\d+)/(\d+) (parse|recognize) \{Kind:(\d) Rule:(\S*) Level:(\d+) Depth:(\d+) Pos:(\d+) Matched:(\w+) End:(\d+) Memo:(\w+) Evals:(\d+) `)

// observedProfile is the profile line of a probe report.
type observedProfile struct {
	Parses, Entered, Calls, Evals, MemoHits, Skipped int
	Rules                                            []struct {
		Rule                                                                string
		Entered, Calls, Evals, MemoHits, Matched, Failed, Consumed, Skipped int
	}
}

// observedReport is the events and the profile of one entry of one input.
type observedReport struct {
	events  []observedEvent
	profile *observedProfile
}

// detailReports returns the detailed reports of the lines, keyed by "case/input entry".
func detailReports(t *testing.T, lines []string) map[string]*observedReport {
	t.Helper()
	out := map[string]*observedReport{}
	for _, l := range lines {
		if m := observedEventLine.FindStringSubmatch(l); m != nil {
			r := out[m[1]+"/"+m[2]+" "+m[3]]
			if r == nil {
				r = &observedReport{}
				out[m[1]+"/"+m[2]+" "+m[3]] = r
			}
			var e observedEvent
			e.kind, e.rule = atoiOr0(m[4]), m[5]
			e.level, e.depth, e.pos, e.end, e.evals = atoiOr0(m[6]), atoiOr0(m[7]), atoiOr0(m[8]), atoiOr0(m[10]), atoiOr0(m[12])
			e.matched, e.memo = m[9] == "true", m[11] == "true"
			r.events = append(r.events, e)
			continue
		}
		if rest, ok := strings.CutPrefix(l, "# "); ok {
			id, rest, _ := strings.Cut(rest, " ")
			entry, rest, _ := strings.Cut(rest, " ")
			if js, ok := strings.CutPrefix(rest, "profile "); ok {
				var p observedProfile
				if err := json.Unmarshal([]byte(js), &p); err != nil {
					t.Fatalf("profile line of %s: %v", id, err)
				}
				r := out[id+" "+entry]
				if r == nil {
					r = &observedReport{}
					out[id+" "+entry] = r
				}
				r.profile = &p
			}
		}
	}
	return out
}

func atoiOr0(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// profileMismatch checks that the counters of the profile are what the events add up to, and returns the
// first difference. Skipped events (kind 2) count as skipped calls, which are not calls.
func profileMismatch(r *observedReport) string {
	type tally struct{ entered, calls, evals, memo, matched, failed, consumed, skipped int }
	byRule := map[string]*tally{}
	get := func(rule string) *tally {
		if byRule[rule] == nil {
			byRule[rule] = &tally{}
		}
		return byRule[rule]
	}
	var total tally
	for _, e := range r.events {
		x := get(e.rule)
		switch e.kind {
		case 0:
			x.entered++
			total.entered++
		case 1:
			x.calls++
			total.calls++
			x.evals += e.evals
			total.evals += e.evals
			if e.memo {
				x.memo++
				total.memo++
			}
			if e.matched {
				x.matched++
				x.consumed += e.end - e.pos
			} else {
				x.failed++
			}
		case 2:
			x.skipped++
			total.skipped++
		}
	}
	p := r.profile
	if p == nil {
		return "no profile"
	}
	if p.Parses != 1 || p.Entered != total.entered || p.Calls != total.calls || p.Evals != total.evals || p.MemoHits != total.memo || p.Skipped != total.skipped {
		return fmt.Sprintf("totals %+v, events add up to %+v", *p, total)
	}
	seen := map[string]bool{}
	for _, row := range p.Rules {
		seen[row.Rule] = true
		x := get(row.Rule)
		if row.Entered != x.entered || row.Calls != x.calls || row.Evals != x.evals || row.MemoHits != x.memo || row.Matched != x.matched || row.Failed != x.failed || row.Consumed != x.consumed || row.Skipped != x.skipped {
			return fmt.Sprintf("rule %s: profile %+v, events add up to %+v", row.Rule, row, *x)
		}
	}
	for rule, x := range byRule {
		if !seen[rule] && (x.entered != 0 || x.skipped != 0) {
			return fmt.Sprintf("rule %s has events %+v but no profile row", rule, *x)
		}
	}
	return ""
}

// engineTraceMismatch compares the calls of a generated parse (the events of got, skipped events set aside)
// with the engine's trace eng of the same input and entry, and returns the first difference. An engine call
// that fails having looked at no more than its first character may be missing from the generated trace.
func engineTraceMismatch(eng []TraceEvent, got []observedEvent, in string) string {
	var g []observedEvent
	for _, e := range got {
		if e.kind != 2 {
			g = append(g, e)
		}
	}
	subtreeEnd := func(ei int) int {
		depth := 0
		for k := ei; k < len(eng); k++ {
			if eng[k].Kind == TraceEnter {
				depth++
			} else if depth--; depth == 0 {
				return k + 1
			}
		}
		return len(eng)
	}
	gi := 0
	for ei := 0; ei < len(eng); {
		e := eng[ei]
		if e.Kind == TraceExit {
			if gi >= len(g) {
				return "the generated trace ends before the exit of " + e.Rule
			}
			x := g[gi]
			if x.kind != 1 || x.rule != e.Rule || x.pos != e.Pos || x.matched != e.Matched || x.end != e.End || x.memo != e.Memo || x.evals != e.Evals {
				return fmt.Sprintf("exit of %s@%d: engine {end %d matched %v memo %v evals %d}, generated %+v", e.Rule, e.Pos, e.End, e.Matched, e.Memo, e.Evals, x)
			}
			gi++
			ei++
			continue
		}
		if gi < len(g) && g[gi].kind == 0 && g[gi].rule == e.Rule && g[gi].pos == e.Pos && g[gi].level == e.Level && g[gi].depth == e.Depth {
			gi++
			ei++
			continue
		}
		end := subtreeEnd(ei)
		if exit := eng[end-1]; !exit.Matched && exit.Examined <= firstCharEnd(in, e.Pos, CodePoints) {
			ei = end
			continue
		}
		var next any = "the end"
		if gi < len(g) {
			next = g[gi]
		}
		return fmt.Sprintf("the engine calls %s@%d (level %d, depth %d); the generated trace has %v", e.Rule, e.Pos, e.Level, e.Depth, next)
	}
	if gi != len(g) {
		return fmt.Sprintf("the generated trace has %d more events", len(g)-gi)
	}
	return ""
}

// checkObservations builds the cases with the start rule start, checks the first detail[i] inputs of case i
// (all of them if detail is nil) against the engine and the profile against the events, and compares the
// digests of all reports with the golden.
func checkObservations(t *testing.T, group string, cases []genCase, start string, detail []int) {
	t.Helper()
	if detail == nil {
		detail = make([]int, len(cases))
		for i, c := range cases {
			detail[i] = len(c.inputs)
		}
	}
	lines := runObserveProbes(t, cases, start, detail, nil)
	reports := detailReports(t, lines)
	events, checked := 0, 0
	for i, c := range cases {
		prog := compile(t, c.src)
		for j := 0; j < detail[i] && j < len(c.inputs); j++ {
			for _, entry := range []string{"parse", "recognize"} {
				r := reports[fmt.Sprintf("%d/%d %s", i, j, entry)]
				if r == nil {
					t.Errorf("%s %q %s: no report", c.name, c.inputs[j], entry)
					continue
				}
				checked++
				events += len(r.events)
				var eng []TraceEvent
				o := ParseOptions{Backend: Closure, Unit: CodePoints, Recognize: entry == "recognize"}
				o.Trace = func(e TraceEvent) { eng = append(eng, e) }
				prog.ParseWith(start, c.inputs[j], o)
				if msg := engineTraceMismatch(eng, r.events, c.inputs[j]); msg != "" {
					t.Errorf("%s %q %s: %s", c.name, c.inputs[j], entry, msg)
				}
				if msg := profileMismatch(r); msg != "" {
					t.Errorf("%s %q %s: profile: %s", c.name, c.inputs[j], entry, msg)
				}
			}
		}
	}
	if checked == 0 || events == 0 {
		t.Fatalf("%d reports checked with %d events", checked, events)
	}
	observationGoldenFile.check(t, group+"/"+os.Getenv(modeEnv)+"/", observationDigests(t, group, cases, lines))
}

const observationGoldenHeader = "# Digests of the observations (trace and profile) of the generated Node parsers, by group, mode and case\n" +
	"# (see gen_flow_observe_test.go). Rewrite with: go test ./internal/engine -run Observation -update\n" + goldenRules

var observationGoldenFile = goldenFile{path: observationGolden, header: observationGoldenHeader}

// TestFlowNodeObservationsMatchEngine checks the observations of the generated corpus with observability.
func TestFlowNodeObservationsMatchEngine(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	eachMode(t, func(t *testing.T) {
		checkObservations(t, "corpus", append(genCorpus(t), observeShapes...), "main", nil)
	})
}

// TestFlowNodeObservationsOnGrammars checks the observations of the checked-in grammars on the inputs of their
// testdata directories (the .txt files of up to 3 KB, at most six each), with the default start rule: the
// inputs themselves against the engine, the damaged ones against the golden.
func TestFlowNodeObservationsOnGrammars(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the large parsers")
	}
	eachMode(t, func(t *testing.T) {
		cases := grammarCases(t)
		detail := make([]int, len(cases))
		for i := range cases {
			detail[i] = len(cases[i].inputs)
			cases[i].inputs = withDamagedInputs(cases[i].inputs)
		}
		checkObservations(t, "grammars", cases, "", detail)
	})
}

// observeShapes are grammars whose rules call other rules in shapes where a first-character guard skips the call
// of an alternative or not: choices of rule references in framed bodies and in the lines of Operators Rules,
// with captures, repetitions and nested groups.
var observeShapes = []genCase{
	{"line choice of rules", `rule main = e $$
rule e = #operators {
    operand num
    level { infix left (plus / minus) }
    level { prefix (minus / plus) }
    level { postfix (bang / "?") }
}
rule num = [0-9]+
rule plus = "+"
rule minus = "-"
rule bang = "!"
`, []string{"1", "1+2", "1-2", "-1", "+1", "1!", "1?", "1+-2", "--1+2!", "1*2", ""}},
	{"line choice with captures and skip", `rule main = e $$
rule e = #operators {
    skip " "*
    operand x:num => $x
    level { infix left o:(plus / minus) => ($lhs, $o, $rhs) }
    level { infix right (star / slash / "^") }
}
rule num = [0-9]+
rule plus = "+"
rule minus = "-"
rule star = "*"
rule slash = "/"
`, []string{"1", "1 + 2", "1 - 2 * 3", "1 ^ 2", "1 / 2 / 3", "1 +", " 1 *  2", "1 ? 2"}},
	{"framed choice of rules", `rule main = item+ $$
rule item = (alpha / digit / space) #recover(skip=" ")
rule alpha = [a-z]+
rule digit = [0-9]+
rule space = " "
`, []string{"ab 12", "a1b2", "?", "", "ab ?", "12ab"}},
	{"guarded repetition of rules", `rule main = (word (comma / semi)?)* $$
rule word = [a-z]+
rule comma = ","
rule semi = ";"
`, []string{"a", "a,b", "a;b,c", "a,,", ",", ""}},
}

// withDamagedInputs returns the inputs with, for each, prefixes at eight evenly spaced lengths and the input
// without one character in eight places, so that the parses fail at many points and try the alternatives
// the valid inputs never reach.
func withDamagedInputs(inputs []string) []string {
	out := append([]string(nil), inputs...)
	for _, in := range inputs {
		r := []rune(in)
		for k := 1; k <= 8; k++ {
			out = append(out, string(r[:len(r)*k/9]))
			i := len(r) * k / 9
			if i < len(r) {
				out = append(out, string(r[:i])+string(r[i+1:]))
			}
		}
	}
	return out
}

var skippedStmt = regexp.MustCompile(`(?m)^\t*p\.Observation\.Skipped\("([^"]*)", p\.Pos, 0\)\n`)

// TestFlowNodeObservationMutants checks that the golden notices a Flow build that drops, moves or reorders
// an event: it changes the code of the build in each way and requires a digest that differs from the
// golden's for some case.
func TestFlowNodeObservationMutants(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	t.Setenv(modeEnv, "1")
	cases := append(genCorpus(t), observeShapes...)
	golden := readGolden(t, observationGolden)
	changed := func(lines []string) int {
		n := 0
		for k, v := range observationDigests(t, "corpus", cases, lines) {
			if w, ok := golden[k]; !ok || w != v {
				n++
			}
		}
		return n
	}
	if n := changed(runObserveProbes(t, cases, "main", nil, nil)); n != 0 {
		t.Fatalf("%d digests of the unmutated build differ from %s", n, observationGolden)
	}
	for _, m := range []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{"dropped skipped event", func(code []byte) []byte {
			loc := skippedStmt.FindIndex(code)
			if loc == nil {
				return code
			}
			return append(append([]byte(nil), code[:loc[0]]...), code[loc[1]:]...)
		}},
		{"reordered skipped events", func(code []byte) []byte {
			// Exchange the rule names of the first two consecutive reports of a function.
			all := skippedStmt.FindAllSubmatchIndex(code, -1)
			for i := 0; i+1 < len(all); i++ {
				a, b := all[i], all[i+1]
				if string(code[a[2]:a[3]]) != string(code[b[2]:b[3]]) && !strings.Contains(string(code[a[1]:b[0]]), "\nfunc ") {
					na, nb := string(code[a[2]:a[3]]), string(code[b[2]:b[3]])
					out := string(code[:a[2]]) + nb + string(code[a[3]:b[2]]) + na + string(code[b[3]:])
					return []byte(out)
				}
			}
			return code
		}},
		{"shifted position of an event", func(code []byte) []byte {
			loc := skippedStmt.FindIndex(code)
			if loc == nil {
				return code
			}
			stmt := strings.Replace(string(code[loc[0]:loc[1]]), "p.Pos, 0)", "p.Pos+1, 0)", 1)
			return []byte(string(code[:loc[0]]) + stmt + string(code[loc[1]:]))
		}},
	} {
		t.Run(m.name, func(t *testing.T) {
			if changed(runObserveProbes(t, cases, "main", nil, m.mutate)) == 0 {
				t.Errorf("the mutant %q changes no digest: the events are not compared", m.name)
			}
		})
	}
}
