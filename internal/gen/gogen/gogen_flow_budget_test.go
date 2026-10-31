package gogen_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

// The Node and recognizer tables of a parser with budget controls are printed from Flow IR, which makes
// the charges explicit (Func.Budget, verified by flow.Verify). The spend of a parse depends on the backend,
// the position unit and the generated artifact, so the step totals are not compared with another
// implementation: these tests build each parser, run it under limits and a polling context, check the
// invariants that every build must keep (limits fail with a well-formed error at the counter they limit,
// the least successful limit succeeds, the context is polled at the runtime's cadence) and freeze the
// totals in testdata/budget.golden, so that a change of the charges shows up as a reviewed diff.

// budgetProbe is the consumer of one generated parser. For each input it prints, per entry (Parse and
// Recognize), the least budget limit that does not fail, a digest of the
// outcomes at the limits around it and at a spread of lower ones, and the number of polls of the
// context of an unlimited run.
const budgetProbeMain = `package main

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
	// run returns whether the run failed with a budget limit, its error text ("" for success) and what is
	// wrong with the error as a budget limit error ("" if nothing).
	run func(s string, limit func(*uint64), ctx context.Context) (limited bool, msg, bad string)
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

// shape returns what is wrong with err as the error of a run limited to budget units, "" if nothing. A
// budget limit error has the type LimitError of the generated package, found by reflection because every
// package has a copy of the runtime; its kind must be the budget limit and the reservation that failed
// must have gone past it.
func shape(err error, spend uint64) string {
	if err == nil || !strings.Contains(err.Error(), "limit exceeded") {
		return "" // not limited: the run failed or succeeded on its own
	}
	for e := err; e != nil; e = errors.Unwrap(e) {
		v := reflect.ValueOf(e)
		if v.Kind() != reflect.Ptr || v.IsNil() || v.Elem().Kind() != reflect.Struct || v.Elem().Type().Name() != "LimitError" {
			continue
		}
		v = v.Elem()
		kind, limit, used, req := v.FieldByName("Kind").Uint(), v.FieldByName("Limit").Uint(), v.FieldByName("Used").Uint(), v.FieldByName("Requested").Uint()
		if kind != 0 || limit != spend || used > limit || used+req <= limit || req == 0 {
			return fmt.Sprintf("malformed budget limit error: kind %d limit %d used %d requested %d", kind, limit, used, req)
		}
		return ""
	}
	return "not a budget limit error: " + err.Error()
}

func at(p probe, s string, n uint64) (bool, string, string) {
	return p.run(s, func(spend *uint64) { *spend = n }, nil)
}

func report(p probe, s string) string {
	// The least limit that does not fail with a budget limit: the failure is monotone in the limit.
	lo, hi := uint64(1), uint64(1)
	for {
		if limited, _, _ := at(p, s, hi); !limited {
			break
		}
		lo, hi = hi+1, hi*2
		if hi > 1<<40 {
			return "unbounded"
		}
	}
	for lo < hi {
		mid := (lo + hi) / 2
		if limited, _, _ := at(p, s, mid); limited {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	total := lo
	// A limit below the total fails, in the way a budget limit error is shaped; a limit at the total
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
		limited, msg, shape := at(p, s, n)
		if n < total && !limited || n == total && limited || shape != "" {
			bad++
			fmt.Printf("# n=%d total=%d limited=%v %q %q\n", n, total, limited, msg, shape)
		}
	}
	ctx := &countCtx{done: make(chan struct{})}
	_, msg, _ := p.run(s, func(*uint64) {}, ctx)
	return fmt.Sprintf("total=%d bad=%d polls=%d result=%q", total, bad, ctx.polls, msg)
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

// budgetProbeSources returns the consumer main package for the n generated packages with the prefix pkg.
func budgetProbeSources(pkg string, n int) string {
	var imports, probes strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&imports, "\t%s%d \"wprobe/%s%d\"\n", pkg, i, pkg, i)
		fmt.Fprintf(&probes, `	{parse: probe{run: func(s string, limit func(*uint64), ctx context.Context) (bool, string, string) {
		var l %[1]s%[2]d.Limits
		limit(&l.Budget)
		opts := []%[1]s%[2]d.ParseOption{%[1]s%[2]d.WithLimits(l)}
		if ctx != nil {
			opts = append(opts, %[1]s%[2]d.WithContext(ctx))
		}
		_, err := %[1]s%[2]d.Parse(s, opts...)
		return errors.Is(err, %[1]s%[2]d.ErrLimit), fmt.Sprint(err), shape(err, l.Budget)
	}}, recognize: probe{run: func(s string, limit func(*uint64), ctx context.Context) (bool, string, string) {
		var l %[1]s%[2]d.Limits
		limit(&l.Budget)
		opts := []%[1]s%[2]d.ParseOption{%[1]s%[2]d.WithLimits(l)}
		if ctx != nil {
			opts = append(opts, %[1]s%[2]d.WithContext(ctx))
		}
		err := %[1]s%[2]d.Recognize(s, opts...)
		return errors.Is(err, %[1]s%[2]d.ErrLimit), fmt.Sprint(err), shape(err, l.Budget)
	}}},
`, pkg, i)
	}
	return strings.NewReplacer("IMPORTS", imports.String(), "PROBES", probes.String()).Replace(budgetProbeMain)
}

// runBudgetProbes builds the cases as the packages f0..fN with the rules printed from Flow IR and returns the
// reports of the consumer; mutate, if not nil, changes the code of each parser.
func runBudgetProbes(t *testing.T, cases []gentest.Case, start string, mutate func([]byte) []byte) []string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", gentest.GoMod("wprobe"))
	var inputs [][]string
	for i, c := range cases {
		g, err := parseTest(c.Src)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		o := gen.GenOptions{Package: fmt.Sprintf("f%d", i), Start: start, Recognize: true}
		code, err := gentest.GenerateCode(t, g, o)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		if mutate != nil {
			code = mutate(code)
		}
		write(fmt.Sprintf("f%d/parser.go", i), string(code))
		inputs = append(inputs, c.Inputs)
	}
	write("main.go", strings.Replace(budgetProbeSources("f", len(cases)), "IMPORTS", "", 1))
	data, _ := json.Marshal(inputs)
	write("inputs.json", string(data))
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", ".", "inputs.json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS="+gentest.GoFlags(t, "-mod=mod -trimpath"), "GOTOOLCHAIN=local")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go run: %v\n%s\n%s", err, out, stderr.String())
	}
	// The consumer prints reports (budgetLine) and diagnostics ("# ", logged); anything else is a malformed
	// probe line.
	var reports []string
	for _, l := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		switch {
		case strings.HasPrefix(l, "# "):
			t.Logf("%s", l)
		case budgetLine.MatchString(l):
			reports = append(reports, l)
		default:
			t.Fatalf("go run: malformed probe line %q", l)
		}
	}
	return reports
}

// budgetReport is the report of one input and entry of the consumer.
type budgetReport struct {
	id           string // case/input and entry
	spend, polls uint64
	bad          int
	result       string
}

var budgetLine = regexp.MustCompile(`^(\S+ \S+) total=(\d+) bad=(\d+) polls=(\d+) result=(.*)$`)

func parseBudgetReports(t *testing.T, lines []string) []budgetReport {
	t.Helper()
	var out []budgetReport
	for _, l := range lines {
		m := budgetLine.FindStringSubmatch(l)
		if m == nil {
			t.Fatalf("unexpected report line %q", l)
		}
		n := func(s string) uint64 { v, _ := strconv.ParseUint(s, 10, 64); return v }
		out = append(out, budgetReport{id: m[1], spend: n(m[2]), polls: n(m[4]), bad: int(n(m[3])), result: m[5]})
	}
	return out
}

// budgetGolden holds the totals of every report: "group/case | input entry" to "budget polls result".
const (
	budgetGolden       = "testdata/budget.golden"
	budgetGoldenHeader = "# Budget totals of the generated parsers with budget controls: budget, context polls and result\n" +
		"# of each input and entry, by group, case and input. Not a limit: stage 3 optimizations move the totals.\n" + gentest.GoldenRules
)

var budgetGoldenFile = gentest.GoldenFile{
	Path: budgetGolden, Header: budgetGoldenHeader, CaseOf: gentest.GoldenCaseBefore, TotalNames: [2]string{"budget", "polls"},
	Totals: func(v string) (a, b uint64) {
		f := strings.Fields(v)
		a, _ = strconv.ParseUint(f[0], 10, 64)
		b, _ = strconv.ParseUint(f[1], 10, 64)
		return
	},
}

// TestFlowNodeBudgetInvariants checks, for the parsers of the generated corpus with budget controls built with
// the rules printed from Flow IR, the invariants every build keeps whichever way it spends budget:
//
//   - a limit below the total fails with a budget limit error whose reservation went
//     past the limit, and the least limit that does not fail succeeds (the bad counts of the reports);
//   - the context is polled every 256 budget units, as the runtime does, so the polls of a run number at
//     least spend/256 (less a slack of two);
//   - a run that parses the whole input without error charged at least one budget unit for each character;
//   - the totals equal those of testdata/budget.golden.
//
// The engine's spend is not an oracle for the spend of a generated parser: for the same grammar
// and input the closure engine, the bytecode VM and the generated parser charge different amounts (they agree
// on about a third of the corpus reports and the generated charge is between 0.33 and 1.37 times the
// engine's), and the charges depend on the backend and artifact by contract (spec/runtime/parsing.md).
//
// The bounded spend of the rule bodies is proved on the IR itself: flow.Verify rejects a function lowered
// with budget controls that consumes input or iterates a loop without charging (see TestVerifyBudgetCharges),
// and every function of this corpus is verified when generated.
func TestFlowNodeBudgetInvariants(t *testing.T) {
	t.Parallel()
	gentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	checkBudgetInvariants(t, "corpus", gentest.GenCorpus(t), "main")
}

// checkBudgetInvariants builds the cases with the start rule start, runs the consumer and checks the
// invariants of TestFlowNodeBudgetInvariants and the golden.
func checkBudgetInvariants(t *testing.T, group string, cases []gentest.Case, start string) {
	t.Helper()
	checkBudgetInvariantsIn(t, budgetGoldenFile, group, cases, start)
}

// checkBudgetInvariantsIn is checkBudgetInvariants against the golden file golden.
func checkBudgetInvariantsIn(t *testing.T, golden gentest.GoldenFile, group string, cases []gentest.Case, start string) {
	t.Helper()
	got, problems := budgetTotals(t, group, cases, start, nil)
	for _, p := range problems {
		t.Error(p)
	}
	golden.Check(t, group+"/", got)
}

// budgetOffGolden holds the totals of the parsers generated without expectations, which charge less
// than the recording ones because they do not record expectations.
const budgetOffGolden = "testdata/budget_off.golden"

var budgetOffGoldenFile = gentest.GoldenFile{
	Path: budgetOffGolden, CaseOf: budgetGoldenFile.CaseOf, TotalNames: budgetGoldenFile.TotalNames, Totals: budgetGoldenFile.Totals,
	Header: "# Budget totals of the generated parsers with budget controls and without expectations: budget, context polls\n" +
		"# and result of each input and entry. The charges of parses that do not record expectations are part of the\n" +
		"# contract of the default mode.\n" + gentest.GoldenRules,
}

// TestFlowNodeBudgetInvariantsWithoutExpectations checks the invariants and a golden for the parsers of the
// generated corpus built without expectations, the default of a generated parser, and that the totals
// of two builds and runs are equal.
func TestFlowNodeBudgetInvariantsWithoutExpectations(t *testing.T) {
	t.Parallel()
	gentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	gentest.OmitExpectations(t)
	cases := gentest.GenCorpus(t)
	first, problems := budgetTotals(t, "corpus-off", cases, "main", nil)
	for _, p := range problems {
		t.Error(p)
	}
	second, _ := budgetTotals(t, "corpus-off", cases, "main", nil)
	if len(first) != len(second) {
		t.Fatalf("two runs report %d and %d entries", len(first), len(second))
	}
	for k, v := range first {
		if second[k] != v {
			t.Errorf("%s is not deterministic: %q then %q", k, v, second[k])
		}
	}
	for k, v := range first {
		if strings.Contains(v, "expected ") {
			t.Errorf("%s: a parse without expectations reports one: %s", k, v)
		}
	}
	budgetOffGoldenFile.Check(t, "corpus-off/", first)
}

// budgetTotals builds the cases (the code of each parser changed by mutate if not nil), runs the consumer and
// returns the entries of the golden and the violations of the invariants of the reports.
func budgetTotals(t *testing.T, group string, cases []gentest.Case, start string, mutate func([]byte) []byte) (got map[string]string, problems []string) {
	t.Helper()
	lines := runBudgetProbes(t, cases, start, mutate)
	if len(lines) == 0 {
		t.Fatal("no reports")
	}
	got = map[string]string{}
	for _, r := range parseBudgetReports(t, lines) {
		if r.bad != 0 {
			problems = append(problems, fmt.Sprintf("%s: limit errors are malformed (%d; see the log)", r.id, r.bad))
		}
		// A poll happens whenever the spend since the last poll reaches the interval, so the polls number
		// at least spend/interval, less the polls the runtime's batched reservations may shift past the end
		// of the run.
		min := r.spend / budgetCheckInterval
		if r.polls+budgetPollSlack < min {
			problems = append(problems, fmt.Sprintf("%s: %d polls for %d spend, at least %d expected", r.id, r.polls, r.spend, min-budgetPollSlack))
		}
		// The probe ids number the cases and inputs; the golden names the case, which does not move when
		// cases are added.
		idx, rest, _ := strings.Cut(r.id, "/")
		n, _ := strconv.Atoi(idx)
		j, _, _ := strings.Cut(rest, " ")
		jn, _ := strconv.Atoi(j)
		entry := strings.Fields(rest)[1]
		if r.result == "<nil>" && r.spend < uint64(utf8.RuneCountInString(cases[n].Inputs[jn])) {
			problems = append(problems, fmt.Sprintf("%s: %d spend for a parsed input of %d characters", r.id, r.spend, utf8.RuneCountInString(cases[n].Inputs[jn])))
		}
		key := fmt.Sprintf("%s/%s | %s %s", group, cases[n].Name, j, entry)
		if _, dup := got[key]; dup {
			t.Fatalf("two reports under the key %q: case names repeat", key)
		}
		got[key] = fmt.Sprintf("%d %d %s", r.spend, r.polls, r.result)
	}
	return got, problems
}

// stepStatement is a charge of one step in generated code.
const stepStatement = "p.Spend()\n"

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

// TestFlowNodeBudgetMutants checks that the invariants and the golden of TestFlowNodeBudgetInvariants notice a
// printer that loses the charge of one Step: with the first, and with the last, step charge of each parser
// removed, some report violates an invariant or differs from the golden.
func TestFlowNodeBudgetMutants(t *testing.T) {
	t.Parallel()
	gentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	cases := gentest.GenCorpus(t)
	golden := gentest.ReadGolden(t, budgetGolden)
	for _, m := range []struct {
		name   string
		mutate func([]byte) []byte
	}{{"drop the first step charge", dropFirstStep}, {"drop the last step charge", dropLastStep}} {
		t.Run(m.name, func(t *testing.T) {
			mutated := 0
			got, problems := budgetTotals(t, "corpus", cases, "main", func(code []byte) []byte {
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

// The cadence of the runtime's context polls (runtime/budget.go: budgetCheckInterval).
const (
	budgetCheckInterval = 256
	budgetPollSlack     = 2
)

// TestFlowNodeBudgetInvariantsOnGrammars checks the invariants and the golden of TestFlowNodeBudgetInvariants for
// the checked-in grammars on the inputs of their testdata directories (the .txt files of up to 3 KB, at most
// six each), with the default start rule.
func TestFlowNodeBudgetInvariantsOnGrammars(t *testing.T) {
	t.Parallel()
	gentest.NeedsBudgetControls(t)
	if testing.Short() {
		t.Skip("builds the large parsers twice")
	}
	cases := gentest.GrammarCases(t)
	checkBudgetInvariants(t, "grammars", cases, "")
}
