package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// The Node tables of an observability build are printed from Flow IR. What the build observes (the events of
// WithTrace and the counters of WithProfile) must be what the direct printers' build observes: the sequence
// of events with their rules, levels, depths, positions, memo flags and evaluations, and the profile of
// every rule. These tests build each parser twice, with the rules printed from Flow IR and with dgen
// (directNodeRules, directRecognizer), and compare the observations of Parse and Recognize on every input.

// observeProbeMain is the consumer of the generated packages with the prefix PREFIX: for each input and entry
// it prints the number of trace events, the hash of the events and of the profile (without times), and the
// error. With OBSERVE_DETAIL set it also prints the events.
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

func report(id, name string, run func(string, ...pegort.ParseOption) error, in string) {
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
	if os.Getenv("OBSERVE_DETAIL") != "" {
		for _, e := range events {
			fmt.Printf("# %s %s %+v\n", id, name, e)
		}
		b, _ := json.Marshal(snap)
		fmt.Printf("# %s %s profile %s\n", id, name, strings.ReplaceAll(string(b), "\n", " "))
	}
}

func main() {
	var inputs [][]string
	data, _ := os.ReadFile(os.Args[1])
	json.Unmarshal(data, &inputs)
	for i := range inputs {
		for j, in := range inputs[i] {
			id := fmt.Sprintf("%d/%d", i, j)
			report(id, "parse", entries[i].parse, in)
			report(id, "recognize", entries[i].recognize, in)
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

// runObserveProbes builds the cases as the packages f0..fN (Flow IR) and d0..dN (dgen) with observability and
// returns the report lines of the consumer for each build, and the events of both when detail is set.
func runObserveProbes(t *testing.T, cases []genCase, start string, detail bool, mutate func([]byte) []byte) (flowOut, directOut []string) {
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
		for _, form := range []struct {
			prefix string
			opts   GenOptions
		}{{"f", GenOptions{}}, {"d", GenOptions{directNodeRules: true, directRecognizer: true}}} {
			o := form.opts
			o.Package, o.Start, o.Recognize, o.Observability = fmt.Sprintf("%s%d", form.prefix, i), start, true, true
			code, err := generateCode(g, o)
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			if form.prefix == "f" && mutate != nil {
				code = mutate(code)
			}
			write(fmt.Sprintf("%s%d/parser.go", form.prefix, i), string(code))
		}
		inputs = append(inputs, c.inputs)
	}
	run := func(prefix string) []string {
		write("main.go", observeSources(prefix, len(cases)))
		data, _ := json.Marshal(inputs)
		write("inputs.json", string(data))
		cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", ".", "inputs.json")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -trimpath", "GOTOOLCHAIN=local")
		if detail {
			cmd.Env = append(cmd.Env, "OBSERVE_DETAIL=1")
		}
		var stderr strings.Builder
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go run (%s): %v\n%s", prefix, err, stderr.String())
		}
		var lines []string
		for _, l := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
			if !observeLine.MatchString(l) && !strings.HasPrefix(l, "# ") {
				t.Fatalf("go run (%s): malformed probe line %q", prefix, l)
			}
			lines = append(lines, l)
		}
		return lines
	}
	return run("f"), run("d")
}

// checkObservations builds the cases with the start rule start and checks that both builds report the same
// events and profiles.
func checkObservations(t *testing.T, cases []genCase, start string) {
	t.Helper()
	fl, di := runObserveProbes(t, cases, start, false, nil)
	if differing := observationDiffs(t, fl, di); len(differing) > 0 {
		t.Errorf("%d reports differ between the Flow and the direct build", len(differing))
	}
}

// observationDiffs returns the ids of the reports that differ between the builds, logging each.
func observationDiffs(t *testing.T, fl, di []string) (differing []string) {
	t.Helper()
	reports := func(lines []string) (out []string) {
		for _, l := range lines {
			if observeLine.MatchString(l) {
				out = append(out, l)
			}
		}
		return out
	}
	f, d := reports(fl), reports(di)
	if len(f) != len(d) || len(f) == 0 {
		t.Fatalf("%d reports from the Flow build and %d from the direct build", len(f), len(d))
	}
	events := 0
	for i := range f {
		if f[i] != d[i] {
			differing = append(differing, f[i][:strings.Index(f[i], " events=")])
			t.Logf("report %d differs:\n flow   %s\n direct %s", i, f[i], d[i])
		}
		var n int
		fmt.Sscanf(observeLine.FindStringSubmatch(f[i])[2], "%d", &n)
		events += n
	}
	if events == 0 {
		t.Fatal("no trace event in any report")
	}
	sort.Strings(differing)
	return differing
}

// TestFlowNodeObservationsMatchDirect checks the observations of the generated corpus with observability.
func TestFlowNodeObservationsMatchDirect(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	eachMode(t, func(t *testing.T) { checkObservations(t, append(genCorpus(t), observeShapes...), "main") })
}

// TestFlowNodeObservationsOnGrammars checks the observations of the checked-in grammars on the inputs of their
// testdata directories (the .txt files of up to 3 KB, at most six each), with the default start rule.
func TestFlowNodeObservationsOnGrammars(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the large parsers twice")
	}
	eachMode(t, func(t *testing.T) {
		cases := grammarCases(t)
		for i := range cases {
			cases[i].inputs = withDamagedInputs(cases[i].inputs)
		}
		checkObservations(t, cases, "")
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

// TestFlowNodeObservationMutants checks that the observation comparison notices a Flow build that drops,
// moves or reorders an event: it changes the code of the Flow build in each case in one of those ways and
// requires a difference in the reports.
func TestFlowNodeObservationMutants(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	t.Setenv(modeEnv, "1")
	cases := append(genCorpus(t), observeShapes...)
	fl, di := runObserveProbes(t, cases, "main", false, nil)
	if d := observationDiffs(t, fl, di); len(d) > 0 {
		t.Fatalf("the unmutated builds differ: %v", d)
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
			mf, _ := runObserveProbes(t, cases, "main", false, m.mutate)
			if d := observationDiffs(t, mf, di); len(d) == 0 {
				t.Errorf("the mutant %q changes no report: the events are not compared", m.name)
			}
		})
	}
}
