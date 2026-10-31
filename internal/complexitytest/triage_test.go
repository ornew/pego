package complexitytest

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/gen"
	"github.com/ornew/zeroparse/internal/engine"
	"github.com/ornew/zeroparse/internal/ir/exec/core"
)

var updateTriage = flag.Bool("update-triage", false, "rewrite the cause lists in testdata/triage")

// classifyFile classifies the grammar at path from its default start rule.
func classifyFile(t *testing.T, path string) *engine.Complexity {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	g, err := zp.ParseGrammar(string(src))
	if err != nil {
		t.Fatal(err)
	}
	c, err := engine.ClassifyComplexity(context.Background(), g, "")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestTriageCoverage checks that every witness and every reason of every triage grammar belongs to
// exactly one triage group, that every group holds a cause, and that the list of causes of each
// group, in testdata/triage, is current: go test -run TestTriageCoverage -update-triage rewrites
// it. The list is the audit trail of the grouping.
func TestTriageCoverage(t *testing.T) {
	t.Parallel()
	groups := TriageGroups()
	for _, tg := range TriageGrammars {
		t.Run(tg.Name, func(t *testing.T) {
			t.Parallel()
			c := classifyFile(t, tg.Path)
			if c.Class == engine.ClassLinear {
				t.Fatalf("%s is classified Linear; remove it from TriageGrammars", tg.Name)
			}
			var mine []*TriageGroup
			for i := range groups {
				if groups[i].Grammar == tg.Name {
					mine = append(mine, &groups[i])
				}
			}
			members := map[string][]string{}
			for _, list := range []struct {
				witness bool
				causes  []engine.ComplexityCause
			}{{true, c.Causes}, {false, c.Reasons}} {
				for _, x := range list.causes {
					var in []string
					for _, g := range mine {
						if g.Matches(x, list.witness) {
							in = append(in, g.ID)
						}
					}
					line := fmt.Sprintf("%d:%d %s: %s", x.Pos.Line, x.Pos.Col, x.Rule, x.Message)
					switch len(in) {
					case 0:
						t.Errorf("no group: witness=%v %s", list.witness, line)
					case 1:
						members[in[0]] = append(members[in[0]], line)
					default:
						t.Errorf("groups %v: witness=%v %s", in, list.witness, line)
					}
				}
			}
			var b strings.Builder
			fmt.Fprintf(&b, "# %s: %v (degree %d), %d witnesses, %d reasons\n", tg.Name, c.Class, c.Degree, len(c.Causes), len(c.Reasons))
			for _, g := range mine {
				if len(members[g.ID]) == 0 {
					t.Errorf("group %s holds no cause", g.ID)
				}
				fmt.Fprintf(&b, "\n## %s (%d) -> %s\n", g.ID, len(members[g.ID]), g.Assign)
				for _, m := range members[g.ID] {
					fmt.Fprintf(&b, "%s\n", m)
				}
			}
			path := filepath.Join("testdata", "triage", tg.Name+".txt")
			if *updateTriage {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run with -update-triage)", err)
			}
			if string(want) != b.String() {
				t.Errorf("%s is out of date; run go test -run TestTriageCoverage -update-triage", path)
			}
		})
	}
	for _, tg := range LinearGrammars {
		t.Run(tg.Name, func(t *testing.T) {
			t.Parallel()
			if c := classifyFile(t, tg.Path); c.Class != engine.ClassLinear {
				t.Errorf("%s is classified %v; add it to TriageGrammars", tg.Name, c.Class)
			}
		})
	}
}

// TestTriageTable checks the triage table itself: identifiers are unique per grammar, every
// pattern compiles, every group is assigned, and families have doubling sizes.
func TestTriageTable(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, g := range TriageGroups() {
		key := g.Grammar + "/" + g.ID
		if seen[key] {
			t.Errorf("duplicate group %s", key)
		}
		seen[key] = true
		if _, err := regexp.Compile(g.Pattern); err != nil {
			t.Errorf("%s: %v", key, err)
		}
		if g.Assign == "" || g.Shape == "" {
			t.Errorf("%s: no assignment or shape", key)
		}
		if g.Assign == Inherent && g.Budget == "" {
			t.Errorf("%s: an inherent group needs budget guidance", key)
		}
		for _, f := range g.Families {
			if len(f.Sizes) < 3 {
				t.Errorf("%s/%s: fewer than three sizes", key, f.Name)
			}
			for i := 1; i < len(f.Sizes); i++ {
				if f.Sizes[i] != 2*f.Sizes[i-1] {
					t.Errorf("%s/%s: sizes %v do not double", key, f.Name, f.Sizes)
				}
				// The order is read against the input length, which must grow.
				if r := float64(len(f.Build(f.Sizes[i]))) / float64(len(f.Build(f.Sizes[i-1]))); r < 1.5 {
					t.Errorf("%s/%s: the input grows by %.2f from size %d to %d", key, f.Name, r, f.Sizes[i-1], f.Sizes[i])
				}
			}
		}
	}
}

// triageCap bounds one triage measurement.
const triageCap = 4_000_000_000

// TestTriageMeasure measures every family of the triage table on every backend and, with
// ZP_TRIAGE_GO set, on a Go parser generated with budget controls. It prints one Markdown row
// per family and backend: the input lengths, the costs, the growth exponents of the cost against
// the length and the order read from them. It runs only when ZP_TRIAGE is set to a regular
// expression that selects grammar/group/family names ("." for all), because it takes minutes:
//
//	ZP_TRIAGE=. ZP_TRIAGE_GO=1 go test -run TestTriageMeasure -v ./internal/complexitytest
//
// A family whose measured order differs from the recorded one fails.
func TestTriageMeasure(t *testing.T) {
	top := t
	sel := os.Getenv("ZP_TRIAGE")
	if sel == "" {
		t.Skip("set ZP_TRIAGE to measure the triage families")
	}
	re := regexp.MustCompile(sel)
	withGo := os.Getenv("ZP_TRIAGE_GO") != ""
	paths := map[string]string{}
	for _, tg := range TriageGrammars {
		paths[tg.Name] = tg.Path
	}
	parsers := map[string]*zp.Parser{}
	for _, g := range TriageGroups() {
		for _, f := range g.Families {
			name := g.Grammar + "/" + g.ID + "/" + f.Name
			if !re.MatchString(name) {
				continue
			}
			t.Run(name, func(t *testing.T) {
				p := parsers[g.Grammar]
				if p == nil {
					src, err := os.ReadFile(paths[g.Grammar])
					if err != nil {
						t.Fatal(err)
					}
					if p, err = zp.CompileSource(string(src), ""); err != nil {
						t.Fatal(err)
					}
					parsers[g.Grammar] = p
				}
				measure := Steps
				if f.Lenient {
					measure = StepsLenient
				}
				var lengths []int
				for _, n := range f.Sizes {
					lengths = append(lengths, len(f.Build(n)))
				}
				report := func(backend string, s Series) {
					got := ClassifyLengths(s, lengths)
					var costs []string
					for _, c := range s.Costs {
						costs = append(costs, fmt.Sprint(c.Steps))
					}
					t.Logf("| %s | %s | %s | %s | %d…%d bytes: %s | %.2f | %v |", g.Grammar, g.ID, f.Name, backend,
						lengths[0], lengths[len(lengths)-1], strings.Join(costs, " → "), s.LengthSlopes(lengths), got)
					if got != f.Order {
						t.Errorf("%s: measured %v, recorded %v", backend, got, f.Order)
					}
				}
				for _, b := range Backends {
					s := Series{Sizes: f.Sizes}
					for _, n := range f.Sizes {
						c, err := measure(p, f.Build(n), b, triageCap)
						if err != nil {
							t.Fatalf("%s size %d: %v", BackendName(b), n, err)
						}
						s.Costs = append(s.Costs, c)
					}
					report(BackendName(b), s)
				}
				if withGo {
					var inputs []string
					for _, n := range f.Sizes {
						inputs = append(inputs, f.Build(n))
					}
					report("generated Go", Series{Sizes: f.Sizes, Costs: generatedSteps(t, top, g.Grammar, paths[g.Grammar], inputs, f.Lenient)})
				}
			})
		}
	}
}

// generatedDrivers holds the directory of the generated driver program of each grammar.
var generatedDrivers sync.Map

// generatedDriverMain is the driver of a generated parser: it reads the inputs as a JSON list of
// strings from the file named by its first argument and prints the spend of each, found by the
// same bisection as Steps, as a JSON list of Cost.
const generatedDriverMain = `package main

import (
	"encoding/json"
	"errors"
	"os"

	rt "github.com/ornew/zeroparse/runtime"
	p "triage/p"
)

type cost struct {
	Steps  uint64
	Capped bool
	Err    string
}

func parses(s string, budget uint64) (bool, error) {
	_, err := p.Parse(s, p.WithLimits(p.Limits{Budget: budget}))
	var le *rt.LimitError
	if errors.As(err, &le) && le.Kind == rt.LimitBudget {
		return false, nil
	}
	return true, err
}

func syntax(err error) bool {
	var one *rt.SyntaxError
	var many rt.SyntaxErrors
	return errors.As(err, &one) || errors.As(err, &many)
}

func main() {
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	var req struct {
		Cap     uint64
		Lenient bool
		Inputs  []string
	}
	if err := json.Unmarshal(data, &req); err != nil {
		panic(err)
	}
	var out []cost
	for _, in := range req.Inputs {
		ok, perr := parses(in, req.Cap)
		if !ok {
			out = append(out, cost{Steps: req.Cap, Capped: true})
			continue
		}
		if perr != nil && !(req.Lenient && syntax(perr)) {
			out = append(out, cost{Err: perr.Error()})
			continue
		}
		lo, hi := uint64(0), req.Cap
		for step := uint64(1024); step < hi; step *= 4 {
			if ok, _ := parses(in, step); ok {
				hi = step
				break
			}
			lo = step
		}
		for hi-lo > 1 && hi-lo > hi/256 {
			mid := lo + (hi-lo)/2
			if ok, _ := parses(in, mid); ok {
				hi = mid
			} else {
				lo = mid
			}
		}
		out = append(out, cost{Steps: hi})
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		panic(err)
	}
}
`

// generatedSteps measures inputs on a Go parser generated with budget controls for the grammar
// at path, building the driver program once per grammar; top owns the program.
func generatedSteps(t, top *testing.T, name, path string, inputs []string, lenient bool) []Cost {
	t.Helper()
	bin, _ := generatedDrivers.Load(name)
	if bin == nil {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		g, err := zp.ParseGrammar(string(src))
		if err != nil {
			t.Fatal(err)
		}
		files, err := gen.Go{Module: "triage/p", Runtime: gen.RuntimeImport, BudgetControls: true}.Generate(g, "")
		if err != nil {
			t.Fatal(err)
		}
		root, err := filepath.Abs("../..")
		if err != nil {
			t.Fatal(err)
		}
		dir, err := os.MkdirTemp("", "zp-triage-")
		if err != nil {
			t.Fatal(err)
		}
		top.Cleanup(func() { os.RemoveAll(dir) })
		write := func(rel string, data []byte) {
			full := filepath.Join(dir, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		for _, f := range files {
			write("p/"+f.Path, f.Content)
		}
		write("go.mod", []byte("module triage\n\ngo 1.27.1\n\nrequire github.com/ornew/zeroparse v0.0.0\n\nreplace github.com/ornew/zeroparse => "+root+"\n"))
		write("main.go", []byte(generatedDriverMain))
		out := filepath.Join(dir, "driver")
		cmd := exec.Command("go", "build", "-o", out, ".")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOFLAGS="+core.GoFlags(os.Getenv("GOFLAGS")))
		if msg, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go build: %v\n%s", err, msg)
		}
		bin = out
		generatedDrivers.Store(name, out)
	}
	req, err := json.Marshal(struct {
		Cap     uint64
		Lenient bool
		Inputs  []string
	}{triageCap, lenient, inputs})
	if err != nil {
		t.Fatal(err)
	}
	reqPath := filepath.Join(t.TempDir(), "req.json")
	if err := os.WriteFile(reqPath, req, 0o644); err != nil {
		t.Fatal(err)
	}
	msg, err := exec.Command(bin.(string), reqPath).Output()
	if err != nil {
		t.Fatalf("driver: %v", err)
	}
	var got []struct {
		Steps  uint64
		Capped bool
		Err    string
	}
	if err := json.Unmarshal(msg, &got); err != nil {
		t.Fatal(err)
	}
	var costs []Cost
	for i, c := range got {
		if c.Err != "" {
			t.Fatalf("generated Go, input %d: %s", i, c.Err)
		}
		costs = append(costs, Cost{Steps: c.Steps, Capped: c.Capped})
	}
	return costs
}
