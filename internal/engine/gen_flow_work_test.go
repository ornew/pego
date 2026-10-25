package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// The Node and recognizer tables of a parser with work controls are printed from Flow IR, which
// charges work where the direct printer does. These tests build each parser twice, with the rules
// printed from Flow IR and with dgen (directNodeRules, directRecognizer), and run both under limits
// and a polling context. The charges are observable only through the limits and the polls, so the
// parsers charge alike when, for every input and limit tried, they fail or succeed alike with the same
// error, need the same total of steps and input work, and poll the context as often.

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
	"hash/fnv"
	"os"
	"os/exec"
	"runtime/debug"
	"sort"
	"strings"
	"time"
IMPORTS)

type probe struct {
	// run returns whether the run failed with a work limit and its error text ("" for success).
	run func(s string, limits func(*uint64, *uint64), ctx context.Context) (limited bool, msg string)
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

func at(p probe, s string, counter int, n uint64) (bool, string) {
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
			if limited, _ := at(p, s, counter, hi); !limited {
				break
			}
			lo, hi = hi+1, hi*2
			if hi > 1<<40 {
				return "unbounded"
			}
		}
		for lo < hi {
			mid := (lo + hi) / 2
			if limited, _ := at(p, s, counter, mid); limited {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		total := lo
		points := map[uint64]bool{}
		for n := uint64(1); n <= 40 && n <= total+1; n++ {
			points[n] = true
		}
		for n := total - 40; n <= total+1; n++ {
			if n >= 1 && n <= total+1 {
				points[n] = true
			}
		}
		for k := uint64(1); k < 64; k++ {
			points[total*k/64+1] = true
		}
		ns := make([]uint64, 0, len(points))
		for n := range points {
			ns = append(ns, n)
		}
		sort.Slice(ns, func(i, j int) bool { return ns[i] < ns[j] })
		h := fnv.New64a()
		for _, n := range ns {
			limited, msg := at(p, s, counter, n)
			fmt.Fprintf(h, "%d:%v:%s\n", n, limited, msg)
			if os.Getenv("WPROBE_DETAIL") != "" {
				fmt.Fprintf(os.Stderr, "counter=%d n=%d limited=%v %s\n", counter, n, limited, msg)
			}
		}
		out = append(out, fmt.Sprintf("total=%d digest=%x", total, h.Sum64()))
	}
	ctx := &countCtx{done: make(chan struct{})}
	_, msg := p.run(s, func(*uint64, *uint64) {}, ctx)
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
			out, err := exec.Command(os.Args[0], os.Args[1], fmt.Sprint(i)).CombinedOutput()
			if err != nil {
				fmt.Printf("case %d: %v\n%s", i, err, out)
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
		fmt.Fprintf(&probes, `	{parse: probe{run: func(s string, limits func(*uint64, *uint64), ctx context.Context) (bool, string) {
		var l %[1]s%[2]d.WorkLimits
		limits(&l.MaxSteps, &l.MaxInputWork)
		opts := []%[1]s%[2]d.ParseOption{%[1]s%[2]d.WithWorkLimits(l)}
		if ctx != nil {
			opts = append(opts, %[1]s%[2]d.WithContext(ctx))
		}
		_, err := %[1]s%[2]d.Parse(s, opts...)
		return errors.Is(err, %[1]s%[2]d.ErrWorkLimit), fmt.Sprint(err)
	}}, recognize: probe{run: func(s string, limits func(*uint64, *uint64), ctx context.Context) (bool, string) {
		var l %[1]s%[2]d.WorkLimits
		limits(&l.MaxSteps, &l.MaxInputWork)
		opts := []%[1]s%[2]d.ParseOption{%[1]s%[2]d.WithWorkLimits(l)}
		if ctx != nil {
			opts = append(opts, %[1]s%[2]d.WithContext(ctx))
		}
		err := %[1]s%[2]d.Recognize(s, opts...)
		return errors.Is(err, %[1]s%[2]d.ErrWorkLimit), fmt.Sprint(err)
	}}},
`, pkg, i)
	}
	return strings.NewReplacer("IMPORTS", imports.String(), "PROBES", probes.String()).Replace(workProbeMain)
}

// runWorkProbes builds the cases twice, as the packages f0..fN with the rules printed from Flow IR (code
// changed by mutate, if set) and d0..dN with dgen, and returns the reports of the consumer for each build.
func runWorkProbes(t *testing.T, cases []genCase, mutate func([]byte) []byte) (flowOut, directOut []string) {
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
			o.Package, o.Start, o.Recognize = fmt.Sprintf("%s%d", form.prefix, i), "main", true
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
		write("main.go", strings.Replace(workProbeSources(prefix, len(cases)), "IMPORTS", "", 1))
		data, _ := json.Marshal(inputs)
		write("inputs.json", string(data))
		cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", ".", "inputs.json")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -trimpath", "GOTOOLCHAIN=local")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("go run (%s): %v\n%s", prefix, err, out)
		}
		return strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	}
	return run("f"), run("d")
}

// stepCharge matches the first step charge of a rule body that the Flow route prints, which the comment
// "(... body inlined)" above its function marks.
var stepCharge = regexp.MustCompile(`(?m)^(// [^\n]*body inlined\)\nfunc e\d+\(p \*rt\.Parser\)[^\n]*\n(?:[^\n]*\n)*?\t+)p\.StepWork\(\)\n`)

// TestFlowNodeWorkChargesMatchDirect checks that the parsers of the generated corpus with work controls
// charge steps and input work and poll the context exactly as the parsers of the direct printers do, and
// that the check fails for a parser that drops a step charge of a rule body printed from Flow IR or charges
// an input twice. Only the rules dgen prints are printed from Flow IR with work controls (see
// flowNodeFrameBody), so the corpus, which also has the Operators Rules and the framed rules that stay with
// generator.expr, shows that the two routes together charge as the direct printers do.
func TestFlowNodeWorkChargesMatchDirect(t *testing.T) {
	needsWorkControls(t)
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	cases := genCorpus(t)
	f, d := runWorkProbes(t, cases, nil)
	if len(f) != len(d) {
		t.Fatalf("%d reports from the Flow build and %d from the direct build", len(f), len(d))
	}
	reports := 0
	for i := range f {
		reports++
		if f[i] != d[i] {
			t.Errorf("report %d differs:\n flow   %s\n direct %s", i, f[i], d[i])
		}
	}
	if reports == 0 {
		t.Fatal("no reports")
	}
	for _, m := range []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{"dropped step charge", func(code []byte) []byte {
			m := stepCharge.FindSubmatchIndex(code)
			if m == nil {
				return code // this grammar has no such rule; the others do
			}
			return append(append([]byte(nil), code[:m[3]]...), code[m[1]:]...)
		}},
		{"doubled input charge", func(code []byte) []byte {
			return []byte(strings.ReplaceAll(string(code), "p.InputWork(1)\n", "p.InputWork(2)\n"))
		}},
	} {
		t.Run("mutant "+m.name, func(t *testing.T) {
			mf, _ := runWorkProbes(t, cases, m.mutate)
			differ := 0
			for i := range mf {
				if mf[i] != d[i] {
					differ++
				}
			}
			if differ == 0 {
				t.Errorf("the mutant %q changes no report: the charges are not observed", m.name)
			}
		})
	}
}
