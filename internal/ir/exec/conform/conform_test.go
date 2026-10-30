package conform

import (
	"errors"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/ir/exec"
	"github.com/ornew/pego/internal/ir/exec/ref"
)

var allOps = []string{"memo.lookup", "memo.lookupGrowing", "memo.store", "memo.seed", "memo.reseed", "memo.finishSeed",
	"memo.ok", "memo.end", "memo.growing", "seen.first", "budget.charge", "budget.used",
	"depth.enter", "depth.exit", "depth.current"}

// modelImpl is the reference model run as an implementation: every case must pass on it.
type modelImpl struct{ *ref.Model }

func (m modelImpl) Values(id int64) any { return m.Model.Values(id) }

// mutant breaks one operation of the model.
type mutant struct {
	modelImpl
	op   string
	edit func(m *ref.Model, args []any) []any
}

func (m mutant) Call(o *exec.Op, args []any) []any {
	if o.Name == m.op {
		return m.edit(m.Model, args)
	}
	return m.Model.Call(o, args)
}

func modelSuite() Suite {
	return Suite{Name: "model", Ops: allOps, New: func(c ref.Config) Impl { return modelImpl{ref.New(c)} }}
}

func TestCasesVerify(t *testing.T) {
	cs := Cases()
	if err := exec.Verify(cs[0].Module); err != nil {
		t.Fatal(err)
	}
	r := Random("random", rand.New(rand.NewPCG(1, 2)), allOps, 200, ref.Config{Rules: 3, Limit: 300, MaxDepth: 4})
	if err := exec.Verify(r.Module); err != nil {
		t.Fatal(err)
	}
}

// TestModelPassesItsCases checks the cases against the contract as the model states it.
func TestModelPassesItsCases(t *testing.T) {
	s := modelSuite()
	for _, c := range Cases() {
		if _, err := Run(s, c); err != nil {
			t.Error(err)
		}
	}
	for seed := uint64(0); seed < 200; seed++ {
		c := Random("random", rand.New(rand.NewPCG(seed, 7)), allOps, 100, ref.Config{Rules: 3, Limit: 400, MaxDepth: 5})
		if _, err := Run(s, c); err != nil {
			t.Fatal(err)
		}
	}
}

// TestMutantsFail is the negative control of the harness: each broken implementation must fail.
func TestMutantsFail(t *testing.T) {
	mutants := map[string]mutant{
		"lookup ignores the level": {op: "memo.lookup", edit: func(m *ref.Model, a []any) []any {
			return m.Call(Module().Op("memo.lookup"), []any{a[0], a[1], int64(0), a[3]})
		}},
		"store keeps the old entry": {op: "memo.store", edit: func(m *ref.Model, a []any) []any {
			if r := m.Call(Module().Op("memo.lookup"), a[:4]); r[1].(bool) {
				return []any{r[0]}
			}
			return m.Call(Module().Op("memo.store"), a)
		}},
		"seen defers every call at position 100": {op: "seen.first", edit: func(m *ref.Model, a []any) []any {
			fresh := m.Call(Module().Op("seen.first"), a)
			return []any{fresh[0].(bool) || a[1].(int64) == 100}
		}},
		"budget charges one unit less": {op: "budget.charge", edit: func(m *ref.Model, a []any) []any {
			n := a[0].(uint64)
			if n > 0 {
				n--
			}
			return m.Call(Module().Op("budget.charge"), []any{n})
		}},
	}
	for name, mu := range mutants {
		t.Run(name, func(t *testing.T) {
			s := Suite{Name: name, Ops: allOps, New: func(c ref.Config) Impl {
				mu.modelImpl = modelImpl{ref.New(c)}
				return mu
			}}
			if !fails(s) {
				t.Fatal("no case detected the mutant")
			}
		})
	}
}

// fails reports whether some handwritten or randomized case fails on s.
func fails(s Suite) bool {
	for _, c := range Cases() {
		if _, err := Run(s, c); err != nil {
			return true
		}
	}
	for seed := uint64(0); seed < 200; seed++ {
		c := Random("random", rand.New(rand.NewPCG(seed, 7)), s.Ops, 100, ref.Config{Rules: 3, Limit: 400, MaxDepth: 5})
		if _, err := Run(s, c); err != nil {
			return true
		}
	}
	return false
}

func TestFailureReport(t *testing.T) {
	s := Suite{Name: "broken", Ops: allOps, New: func(c ref.Config) Impl {
		return mutant{modelImpl: modelImpl{ref.New(c)}, op: "memo.end", edit: func(*ref.Model, []any) []any { return []any{int64(-1)} }}
	}}
	_, err := Run(s, Cases()[0])
	var f *Failure
	if !errors.As(err, &f) || !strings.Contains(err.Error(), "memo.end(#") || !strings.Contains(err.Error(), "func memo_store_lookup") {
		t.Fatalf("report = %v", err)
	}
}

func TestSkipsMissingOps(t *testing.T) {
	s := modelSuite()
	s.Ops = []string{"budget.charge", "budget.used"}
	skipped, err := Run(s, Cases()[0])
	if !skipped || err != nil {
		t.Fatalf("Run = %v, %v; want the memo case skipped", skipped, err)
	}
}
