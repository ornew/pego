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
	"strconv"
	"strings"
	"testing"
)

// The Node and recognizer tables of a parser with work controls are printed from Flow IR, which makes
// the charges explicit (Func.Work, verified by flow.Verify). Its charges are not those of the direct
// printers for every rule (an Operators Rule line, a rule with dead branches, a choice whose guards skip an
// alternative), because the backends charge as the spec allows: the work of a parse depends on the
// backend, the position unit and the generated artifact. These tests build each parser twice, with the
// rules printed from Flow IR and with dgen (directNodeRules, directRecognizer), run both under limits and
// a polling context, and check the invariants that every build must keep: limits fail with a well-formed
// error at the counter they limit, the input work is the same, the context is polled at the runtime's
// cadence, and the number of steps stays within a factor of the direct build's.

// workProbe is the consumer of one generated parser. For each input it prints, per entry (Parse and
// Recognize) and per counter (steps, input work), the least limit that does not fail, a digest of the
// outcomes at the limits around it and at a spread of lower ones, and the number of polls of the
// context of an unlimited run.
const workProbeMain = `package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"runtime/debug"
	"strings"
	"time"
IMPORTS)

type probe struct {
	// run returns whether the run failed with a work limit, its error text ("" for success) and what is
	// wrong with the error as a work-limit error ("" if nothing).
	run func(s string, limits func(*uint64, *uint64), ctx context.Context) (limited bool, msg, bad string)
}

type countCtx struct {
	done  chan struct{}
	polls int
}

func (c *countCtx) Deadline() (time.Time, bool)       { return time.Time{}, false }
func (c *countCtx) Done() <-chan struct{}             { return c.done }
func (c *countCtx) Err() error                        { c.polls++; return nil }
func (c *countCtx) Value(key any) any                 { return nil }

var probes = []struct{ parse, recognize probe }{
PROBES}

// shape returns what is wrong with err as the error of a run limited to steps and input work, "" if
// nothing. A work-limit error has the type WorkLimitError of the generated package, found by reflection
// because every package has a copy of the runtime; its kind says which limit was reached and the
// reservation that failed must have gone past it.
func shape(err error, steps, input uint64) string {
	if err == nil || !strings.Contains(err.Error(), "limit exceeded") {
		return "" // not limited: the run failed or succeeded on its own
	}
	for e := err; e != nil; e = errors.Unwrap(e) {
		v := reflect.ValueOf(e)
		if v.Kind() != reflect.Ptr || v.IsNil() || v.Elem().Kind() != reflect.Struct || v.Elem().Type().Name() != "WorkLimitError" {
			continue
		}
		v = v.Elem()
		kind, limit, used, req := v.FieldByName("Kind").Uint(), v.FieldByName("Limit").Uint(), v.FieldByName("Used").Uint(), v.FieldByName("Requested").Uint()
		want, wantKind, wantCode := input, "input", uint64(0)
		if steps != 0 && input == 0 {
			want, wantKind, wantCode = steps, "steps", 1
		}
		if kind != wantCode || limit != want || used > limit || used+req <= limit || req == 0 {
			return fmt.Sprintf("malformed %s limit error: kind %d limit %d used %d requested %d", wantKind, kind, limit, used, req)
		}
		return ""
	}
	return "not a work-limit error: " + err.Error()
}

func at(p probe, s string, counter int, n uint64) (bool, string, string) {
	return p.run(s, func(steps, input *uint64) {
		if counter == 0 {
			*steps = n
		} else {
			*input = n
		}
	}, nil)
}

func report(p probe, s string) string {
	var out []string
	for counter := 0; counter < 2; counter++ {
		// The least limit that does not fail with a work limit: the failure is monotone in the limit.
		lo, hi := uint64(1), uint64(1)
		for {
			if limited, _, _ := at(p, s, counter, hi); !limited {
				break
			}
			lo, hi = hi+1, hi*2
			if hi > 1<<40 {
				return "unbounded"
			}
		}
		for lo < hi {
			mid := (lo + hi) / 2
			if limited, _, _ := at(p, s, counter, mid); limited {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		total := lo
		// A limit below the total fails, in the way a work-limit error is shaped. A limit at the total may
		// fail: growing a memo table is charged by the pooled parser's state, which the total of an earlier
		// run in the process changed.
		points := map[uint64]bool{}
		for n := uint64(1); n <= 8 && n < total; n++ {
			points[n] = true
		}
		for k := uint64(1); k < 16; k++ {
			points[total*k/16+1] = true
		}
		points[total-1] = true
		bad := 0
		for n := range points {
			if n < 1 || n >= total {
				continue
			}
			limited, msg, shape := at(p, s, counter, n)
			if n < total && !limited || shape != "" {
				bad++
				fmt.Printf("# counter=%d n=%d total=%d limited=%v %q %q\n", counter, n, total, limited, msg, shape)
			}
		}
		out = append(out, fmt.Sprintf("total=%d bad=%d", total, bad))
	}
	ctx := &countCtx{done: make(chan struct{})}
	_, msg, _ := p.run(s, func(*uint64, *uint64) {}, ctx)
	out = append(out, fmt.Sprintf("polls=%d result=%q", ctx.polls, msg))
	return strings.Join(out, " ")
}

func main() {
	var inputs [][]string
	data, _ := os.ReadFile(os.Args[1])
	json.Unmarshal(data, &inputs)
	if len(os.Args) == 2 {
		// Each case runs in a process of its own: a parse that stops at a limit may leave state in the
		// runtime that a later parse of another case would observe.
		for i := range inputs {
			cmd := exec.Command(os.Args[0], os.Args[1], fmt.Sprint(i))
			cmd.Stderr = os.Stderr
			out, err := cmd.Output()
			if err != nil {
				fmt.Fprintf(os.Stderr, "case %d: %v\n", i, err)
				os.Exit(1)
			}
			os.Stdout.Write(out)
		}
		return
	}
	// A parser from the pool of the runtime keeps the memo tables that earlier parses grew, and growing
	// a table is charged; a collection empties the pool at a time the process does not control. Without
	// collections, the charges of a run depend on the runs before it alone, which are the same in both builds.
	debug.SetGCPercent(-1)
	var i int
	fmt.Sscan(os.Args[2], &i)
	for j, in := range inputs[i] {
		fmt.Printf("%d/%d parse %s\n", i, j, report(probes[i].parse, in))
		fmt.Printf("%d/%d recognize %s\n", i, j, report(probes[i].recognize, in))
	}
	_ = errors.Is
}
`

// workProbeSources returns the consumer main package for the n generated packages with the prefix pkg.
func workProbeSources(pkg string, n int) string {
	var imports, probes strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&imports, "\t%s%d \"wprobe/%s%d\"\n", pkg, i, pkg, i)
		fmt.Fprintf(&probes, `	{parse: probe{run: func(s string, limits func(*uint64, *uint64), ctx context.Context) (bool, string, string) {
		var l %[1]s%[2]d.WorkLimits
		limits(&l.MaxSteps, &l.MaxInputWork)
		opts := []%[1]s%[2]d.ParseOption{%[1]s%[2]d.WithWorkLimits(l)}
		if ctx != nil {
			opts = append(opts, %[1]s%[2]d.WithContext(ctx))
		}
		_, err := %[1]s%[2]d.Parse(s, opts...)
		return errors.Is(err, %[1]s%[2]d.ErrWorkLimit), fmt.Sprint(err), shape(err, l.MaxSteps, l.MaxInputWork)
	}}, recognize: probe{run: func(s string, limits func(*uint64, *uint64), ctx context.Context) (bool, string, string) {
		var l %[1]s%[2]d.WorkLimits
		limits(&l.MaxSteps, &l.MaxInputWork)
		opts := []%[1]s%[2]d.ParseOption{%[1]s%[2]d.WithWorkLimits(l)}
		if ctx != nil {
			opts = append(opts, %[1]s%[2]d.WithContext(ctx))
		}
		err := %[1]s%[2]d.Recognize(s, opts...)
		return errors.Is(err, %[1]s%[2]d.ErrWorkLimit), fmt.Sprint(err), shape(err, l.MaxSteps, l.MaxInputWork)
	}}},
`, pkg, i)
	}
	return strings.NewReplacer("IMPORTS", imports.String(), "PROBES", probes.String()).Replace(workProbeMain)
}

// runWorkProbes builds the cases twice, as the packages f0..fN with the rules printed from Flow IR and d0..dN with dgen, and returns the reports of the consumer for each build.
func runWorkProbes(t *testing.T, cases []genCase, start string) (flowOut, directOut []string) {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", testGoMod("wprobe"))
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
			o.Package, o.Start, o.Recognize = fmt.Sprintf("%s%d", form.prefix, i), start, true
			code, err := generateCode(g, o)
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			write(fmt.Sprintf("%s%d/parser.go", form.prefix, i), string(code))
		}
		inputs = append(inputs, c.inputs)
	}
	run := func(prefix string) []string {
		write("main.go", strings.Replace(workProbeSources(prefix, len(cases)), "IMPORTS", "", 1))
		data, _ := json.Marshal(inputs)
		write("inputs.json", string(data))
		cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", ".", "inputs.json")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -trimpath", "GOTOOLCHAIN=local")
		var stderr strings.Builder
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go run (%s): %v\n%s\n%s", prefix, err, out, stderr.String())
		}
		// The consumer prints reports (workLine) and diagnostics ("# ", logged); diagnostics may differ between
		// the builds, so only the reports are returned and anything else is a malformed probe line.
		var reports []string
		for _, l := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
			switch {
			case strings.HasPrefix(l, "# "):
				t.Logf("%s: %s", prefix, l)
			case workLine.MatchString(l):
				reports = append(reports, l)
			default:
				t.Fatalf("go run (%s): malformed probe line %q", prefix, l)
			}
		}
		return reports
	}
	return run("f"), run("d")
}

// workReport is the report of one input and entry of the consumer.
type workReport struct {
	id                  string // case/input and entry
	steps, input, polls uint64
	bad                 int
	result              string
}

var workLine = regexp.MustCompile(`^(\S+ \S+) total=(\d+) bad=(\d+) total=(\d+) bad=(\d+) polls=(\d+) result=(.*)$`)

func parseWorkReports(t *testing.T, lines []string) []workReport {
	t.Helper()
	var out []workReport
	for _, l := range lines {
		m := workLine.FindStringSubmatch(l)
		if m == nil {
			t.Fatalf("unexpected report line %q", l)
		}
		n := func(s string) uint64 { v, _ := strconv.ParseUint(s, 10, 64); return v }
		out = append(out, workReport{id: m[1], steps: n(m[2]), input: n(m[4]), polls: n(m[6]), bad: int(n(m[3]) + n(m[5])), result: m[7]})
	}
	return out
}

// Steps of the Flow build beyond flagStepRatio times the direct build's on an input are logged; beyond
// maxStepRatio they fail the test.
const (
	flagStepRatio = 1.25
	maxStepRatio  = 1.5
)

// TestFlowNodeWorkInvariants checks, for the parsers of the generated corpus with work controls built with
// the rules printed from Flow IR, the invariants every build keeps whichever way it charges steps:
//
//   - a limit below the total fails with a work-limit error of the limited counter whose reservation went
//     past the limit (the bad counts of the reports);
//   - the input work of a parse is the same as the direct build's: both charge one unit for each character
//     that a matcher or a source scan reads, so a difference would be a character read twice or never;
//   - the context is polled every 256 steps and every 4096 units of input work, as the runtime does, so the
//     polls of a run number at least the larger of steps/256 and input/4096 (less a slack of two), in both
//     builds;
//   - the result of a run under a context is the direct build's;
//   - the steps of the Flow build stay within a factor of the direct build's (the distribution is logged
//     and a case beyond flagStepRatio is flagged and one beyond maxStepRatio fails the test).
//
// The bounded work of the rule bodies is proved on the IR itself: flow.Verify rejects a function lowered
// with work controls that consumes input or iterates a loop without charging (see TestVerifyWorkCharges),
// and every function of this corpus is verified when generated.
func TestFlowNodeWorkInvariants(t *testing.T) {
	needsWorkControls(t)
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	checkWorkInvariants(t, genCorpus(t), "main")
}

// checkWorkInvariants builds the cases with the start rule start, runs the consumer on both builds and checks
// the invariants of TestFlowNodeWorkInvariants.
func checkWorkInvariants(t *testing.T, cases []genCase, start string) {
	t.Helper()
	fl, di := runWorkProbes(t, cases, start)
	if len(fl) != len(di) || len(fl) == 0 {
		t.Fatalf("%d reports from the Flow build and %d from the direct build", len(fl), len(di))
	}
	f, d := parseWorkReports(t, fl), parseWorkReports(t, di)
	var ratios []float64
	var differ, equal int
	for i := range f {
		if f[i].id != d[i].id {
			t.Fatalf("report %d is for %s and %s", i, f[i].id, d[i].id)
		}
		if f[i].bad != 0 || d[i].bad != 0 {
			t.Errorf("%s: limit errors are malformed (%d in the Flow build, %d in the direct build; see the log)", f[i].id, f[i].bad, d[i].bad)
		}
		if f[i].input != d[i].input {
			t.Errorf("%s: input work %d in the Flow build, %d in the direct build", f[i].id, f[i].input, d[i].input)
		}
		if f[i].result != d[i].result {
			t.Errorf("%s: result %s in the Flow build, %s in the direct build", f[i].id, f[i].result, d[i].result)
		}
		for _, r := range []workReport{f[i], d[i]} {
			// A poll happens when either counter reaches its interval since the last poll, so the polls number
			// at least the larger of the two quotients, less the polls the runtime's batched reservations may
			// shift past the end of the run.
			min := max(r.steps/workCheckSteps, r.input/workCheckInput)
			if r.polls+workPollSlack < min {
				t.Errorf("%s: %d polls for %d steps and %d input work, at least %d expected", r.id, r.polls, r.steps, r.input, min-workPollSlack)
			}
		}
		if f[i].steps == 0 && d[i].steps != 0 {
			t.Errorf("%s: the Flow build charges no step where the direct build charges %d", f[i].id, d[i].steps)
		}
		if d[i].steps == 0 {
			continue
		}
		r := float64(f[i].steps) / float64(d[i].steps)
		ratios = append(ratios, r)
		if f[i].steps == d[i].steps {
			equal++
		} else {
			differ++
		}
		if r > flagStepRatio {
			t.Logf("flagged: %s charges %d steps in the Flow build, %.2fx the direct build's %d", f[i].id, f[i].steps, r, d[i].steps)
		}
		if r > maxStepRatio {
			t.Errorf("%s: the Flow build charges %d steps, %.2fx the direct build's %d", f[i].id, f[i].steps, r, d[i].steps)
		}
	}
	sort.Float64s(ratios)
	q := func(p float64) float64 { return ratios[int(p*float64(len(ratios)-1))] }
	t.Logf("step ratio Flow/direct over %d reports (%d equal, %d differ): min %.3f p50 %.3f p90 %.3f p99 %.3f max %.3f",
		len(ratios), equal, differ, ratios[0], q(0.5), q(0.9), q(0.99), ratios[len(ratios)-1])
}

// The cadence of the runtime's context polls (runtime/work.go: workCheckStepInterval and
// workCheckInputInterval).
const (
	workCheckSteps = 256
	workCheckInput = 4096
	workPollSlack  = 2
)

// TestFlowNodeWorkInvariantsOnGrammars checks the invariants of TestFlowNodeWorkInvariants for the checked-in
// grammars on the inputs of their testdata directories (the .txt files of up to 3 KB, at most six each), with
// the default start rule.
func TestFlowNodeWorkInvariantsOnGrammars(t *testing.T) {
	needsWorkControls(t)
	if testing.Short() {
		t.Skip("builds the large parsers twice")
	}
	cases := grammarCases(t)
	checkWorkInvariants(t, cases, "")
}

// grammarCases returns the ten checked-in grammars with the inputs of their testdata directories (the .txt
// files of up to 3 KB, at most six each).
func grammarCases(t *testing.T) []genCase {
	t.Helper()
	var cases []genCase
	for _, name := range []string{"json", "cel", "cue", "duckdb", "golang", "python", "xml", "yaml", "csv", "typescript"} {
		src, err := os.ReadFile(filepath.Join("..", "..", "parsers", name, name+".pego"))
		if err != nil {
			t.Fatal(err)
		}
		files, _ := filepath.Glob(filepath.Join("..", "..", "parsers", name, "testdata", "*.txt"))
		sort.Strings(files)
		c := genCase{name: name, src: string(src)}
		for _, f := range files {
			if b, err := os.ReadFile(f); err == nil && len(b) <= 3<<10 && len(c.inputs) < 6 {
				c.inputs = append(c.inputs, string(b))
			}
		}
		c.inputs = append(c.inputs, grammarOperatorInputs[name]...)
		if len(c.inputs) == 0 {
			t.Fatalf("%s: no inputs in testdata", name)
		}
		cases = append(cases, c)
	}
	return cases
}

// grammarOperatorInputs are short inputs that use many operators of the grammars with an Operators Rule, so
// that the guards of the choices of operator tokens skip some alternatives and the lines match others.
var grammarOperatorInputs = map[string][]string{
	"duckdb":     {"SELECT a BETWEEN 1 + 2 AND 3 - 4, b BETWEEN 7 / 8 % 9 AND 5 * 6, c BETWEEN 1 AND 2 // 3, d BETWEEN 4 ^ 5 AND 6 ** 7;\n", "SELECT 1 / 2, 3 % 4, 5 * 6, 7 - 8, 9 + 10, 2 ^ 3, 6 // 4, 1 < 2, 1 >= 2, a AND b OR NOT c;\n"},
	"cel":        {"1 / 2 % 3 * 4 - 5 + 6 < 7 && 8 >= 9 || !x", "a ? b : c", "-x + y * z"},
	"cue":        {"x: 1 / 2 * 3 - 4 + 5 & !true | 6 < 7 && 8 || 9\n"},
	"golang":     {"package p\nfunc f() {\n\tx = a / b * c % d - e + f << 1 >> 2 & g | h ^ i &^ j && !k || l < m\n}\n"},
	"python":     {"x = a / b * c % d - e + f ** 2 // 3 << 1 >> 2 & g | h ^ i and not j or k < l\n"},
	"typescript": {"x = a / b * c % d - e + f ** 2 << 1 >> 2 >>> 3 & g | h ^ i && !j || k < l ?? m;\n"},
}
