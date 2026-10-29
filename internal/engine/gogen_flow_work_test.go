package engine

import (
	"bytes"
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
	"unicode/utf8"

	"github.com/ornew/pego/internal/gen"
)

// The Node and recognizer tables of a parser with work controls are printed from Flow IR, which makes
// the charges explicit (Func.Work, verified by flow.Verify). The work of a parse depends on the backend,
// the position unit and the generated artifact, so the step totals are not compared with another
// implementation: these tests build each parser, run it under limits and a polling context, check the
// invariants that every build must keep (limits fail with a well-formed error at the counter they limit,
// the least successful limit succeeds, the context is polled at the runtime's cadence) and freeze the
// totals in testdata/work.golden, so that a change of the charges shows up as a reviewed diff.

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
		// A limit below the total fails, in the way a work-limit error is shaped; a limit at the total
		// does not, whatever the pooled parsers hold.
		points := map[uint64]bool{total: true}
		for n := uint64(1); n <= 8 && n < total; n++ {
			points[n] = true
		}
		for k := uint64(1); k < 16; k++ {
			points[total*k/16+1] = true
		}
		points[total-1] = true
		bad := 0
		for n := range points {
			if n < 1 || n > total {
				continue
			}
			limited, msg, shape := at(p, s, counter, n)
			if n < total && !limited || n == total && limited || shape != "" {
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
	// Collections empty the pool of the runtime at times the process does not control; the charges
	// of a run must not depend on them.
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

// runWorkProbes builds the cases as the packages f0..fN with the rules printed from Flow IR and returns the
// reports of the consumer; mutate, if not nil, changes the code of each parser.
func runWorkProbes(t *testing.T, cases []genCase, start string, mutate func([]byte) []byte) []string {
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
		o := gen.GenOptions{Package: fmt.Sprintf("f%d", i), Start: start, Recognize: true}
		code, err := generateCode(g, o)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if mutate != nil {
			code = mutate(code)
		}
		write(fmt.Sprintf("f%d/parser.go", i), string(code))
		inputs = append(inputs, c.inputs)
	}
	write("main.go", strings.Replace(workProbeSources("f", len(cases)), "IMPORTS", "", 1))
	data, _ := json.Marshal(inputs)
	write("inputs.json", string(data))
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", ".", "inputs.json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -trimpath", "GOTOOLCHAIN=local")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go run: %v\n%s\n%s", err, out, stderr.String())
	}
	// The consumer prints reports (workLine) and diagnostics ("# ", logged); anything else is a malformed
	// probe line.
	var reports []string
	for _, l := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		switch {
		case strings.HasPrefix(l, "# "):
			t.Logf("%s", l)
		case workLine.MatchString(l):
			reports = append(reports, l)
		default:
			t.Fatalf("go run: malformed probe line %q", l)
		}
	}
	return reports
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

// workGolden holds the totals of every report: "group/case | input entry" to "steps input polls result".
const (
	workGolden       = "testdata/work.golden"
	workGoldenHeader = "# Work totals of the generated parsers with work controls: steps, input work, context polls and result\n" +
		"# of each input and entry, by group, case and input. Not a limit: stage 3 optimizations move the totals.\n" + goldenRules
)

var workGoldenFile = goldenFile{
	path: workGolden, header: workGoldenHeader, caseOf: goldenCaseBefore, totalNames: [2]string{"steps", "input work"},
	totals: func(v string) (a, b uint64) {
		f := strings.Fields(v)
		a, _ = strconv.ParseUint(f[0], 10, 64)
		b, _ = strconv.ParseUint(f[1], 10, 64)
		return
	},
}

// TestFlowNodeWorkInvariants checks, for the parsers of the generated corpus with work controls built with
// the rules printed from Flow IR, the invariants every build keeps whichever way it charges steps:
//
//   - a limit below the total fails with a work-limit error of the limited counter whose reservation went
//     past the limit, and the least limit that does not fail succeeds (the bad counts of the reports);
//   - the context is polled every 256 steps and every 4096 units of input work, as the runtime does, so the
//     polls of a run number at least the larger of steps/256 and input/4096 (less a slack of two);
//   - a run that charges input work charges steps, and a run that parses the whole input without error
//     charged at least one unit of input work for each character;
//   - the totals equal those of testdata/work.golden.
//
// The engine's input work is not an oracle for the input work of a generated parser: for the same grammar
// and input the closure engine, the bytecode VM and the generated parser charge different amounts (they agree
// on about a third of the corpus reports and the generated charge is between 0.33 and 1.37 times the
// engine's), and the charges depend on the backend and artifact by contract (spec/runtime/parsing.md).
//
// The bounded work of the rule bodies is proved on the IR itself: flow.Verify rejects a function lowered
// with work controls that consumes input or iterates a loop without charging (see TestVerifyWorkCharges),
// and every function of this corpus is verified when generated.
func TestFlowNodeWorkInvariants(t *testing.T) {
	needsWorkControls(t)
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	checkWorkInvariants(t, "corpus", genCorpus(t), "main")
}

// checkWorkInvariants builds the cases with the start rule start, runs the consumer and checks the
// invariants of TestFlowNodeWorkInvariants and the golden.
func checkWorkInvariants(t *testing.T, group string, cases []genCase, start string) {
	t.Helper()
	got, problems := workTotals(t, group, cases, start, nil)
	for _, p := range problems {
		t.Error(p)
	}
	workGoldenFile.check(t, group+"/", got)
}

// workTotals builds the cases (the code of each parser changed by mutate if not nil), runs the consumer and
// returns the entries of the golden and the violations of the invariants of the reports.
func workTotals(t *testing.T, group string, cases []genCase, start string, mutate func([]byte) []byte) (got map[string]string, problems []string) {
	t.Helper()
	lines := runWorkProbes(t, cases, start, mutate)
	if len(lines) == 0 {
		t.Fatal("no reports")
	}
	got = map[string]string{}
	for _, r := range parseWorkReports(t, lines) {
		if r.bad != 0 {
			problems = append(problems, fmt.Sprintf("%s: limit errors are malformed (%d; see the log)", r.id, r.bad))
		}
		// A poll happens when either counter reaches its interval since the last poll, so the polls number
		// at least the larger of the two quotients, less the polls the runtime's batched reservations may
		// shift past the end of the run.
		min := max(r.steps/workCheckSteps, r.input/workCheckInput)
		if r.polls+workPollSlack < min {
			problems = append(problems, fmt.Sprintf("%s: %d polls for %d steps and %d input work, at least %d expected", r.id, r.polls, r.steps, r.input, min-workPollSlack))
		}
		if r.steps == 0 && r.input != 0 {
			problems = append(problems, fmt.Sprintf("%s: no step is charged for %d input work", r.id, r.input))
		}
		// The probe ids number the cases and inputs; the golden names the case, which does not move when
		// cases are added.
		idx, rest, _ := strings.Cut(r.id, "/")
		n, _ := strconv.Atoi(idx)
		j, _, _ := strings.Cut(rest, " ")
		jn, _ := strconv.Atoi(j)
		entry := strings.Fields(rest)[1]
		if r.result == "<nil>" && r.input < uint64(utf8.RuneCountInString(cases[n].inputs[jn])) {
			problems = append(problems, fmt.Sprintf("%s: %d input work for a parsed input of %d characters", r.id, r.input, utf8.RuneCountInString(cases[n].inputs[jn])))
		}
		key := fmt.Sprintf("%s/%s | %s %s", group, cases[n].name, j, entry)
		if _, dup := got[key]; dup {
			t.Fatalf("two reports under the key %q: case names repeat", key)
		}
		got[key] = fmt.Sprintf("%d %d %d %s", r.steps, r.input, r.polls, r.result)
	}
	return got, problems
}

// stepStatement is a charge of one step in generated code.
const stepStatement = "p.StepWork()\n"

// dropFirstStep removes the first step charge of the code, as a printer would if it lost the charge of a
// Step of the IR; dropLastStep removes the last.
func dropFirstStep(code []byte) []byte {
	i := bytes.Index(code, []byte(stepStatement))
	if i < 0 {
		return code
	}
	return append(append([]byte(nil), code[:i]...), code[i+len(stepStatement):]...)
}

func dropLastStep(code []byte) []byte {
	i := bytes.LastIndex(code, []byte(stepStatement))
	if i < 0 {
		return code
	}
	return append(append([]byte(nil), code[:i]...), code[i+len(stepStatement):]...)
}

// TestFlowNodeWorkMutants checks that the invariants and the golden of TestFlowNodeWorkInvariants notice a
// printer that loses the charge of one Step: with the first, and with the last, step charge of each parser
// removed, some report violates an invariant or differs from the golden.
func TestFlowNodeWorkMutants(t *testing.T) {
	needsWorkControls(t)
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	cases := genCorpus(t)
	golden := readGolden(t, workGolden)
	for _, m := range []struct {
		name   string
		mutate func([]byte) []byte
	}{{"drop the first step charge", dropFirstStep}, {"drop the last step charge", dropLastStep}} {
		t.Run(m.name, func(t *testing.T) {
			mutated := 0
			got, problems := workTotals(t, "corpus", cases, "main", func(code []byte) []byte {
				out := m.mutate(code)
				if !bytes.Equal(out, code) {
					mutated++
				}
				return out
			})
			if mutated == 0 {
				t.Fatal("the mutant changes no generated parser")
			}
			differ := 0
			for k, v := range got {
				if w, ok := golden[k]; !ok || w != v {
					differ++
				}
			}
			if len(problems)+differ == 0 {
				t.Errorf("the mutant %q changes no report and breaks no invariant", m.name)
			}
			t.Logf("%d parsers mutated: %d reports differ from the golden, %d invariants violated", mutated, differ, len(problems))
		})
	}
}

// The cadence of the runtime's context polls (runtime/work.go: workCheckStepInterval and
// workCheckInputInterval).
const (
	workCheckSteps = 256
	workCheckInput = 4096
	workPollSlack  = 2
)

// TestFlowNodeWorkInvariantsOnGrammars checks the invariants and the golden of TestFlowNodeWorkInvariants for
// the checked-in grammars on the inputs of their testdata directories (the .txt files of up to 3 KB, at most
// six each), with the default start rule.
func TestFlowNodeWorkInvariantsOnGrammars(t *testing.T) {
	needsWorkControls(t)
	if testing.Short() {
		t.Skip("builds the large parsers twice")
	}
	cases := grammarCases(t)
	checkWorkInvariants(t, "grammars", cases, "")
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
