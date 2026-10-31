//go:build !zp_budget_controls_reference

package apitest_test

import (
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/zeroparse"
)

// leastSteps returns the least Budget limit under which the parse succeeds, and the error text of
// the parse one step below it.
func leastSteps(t *testing.T, p *zeroparse.Parser, in string) (uint64, string) {
	t.Helper()
	fails := func(n uint64) (bool, string) {
		_, err := p.Parse(in, zeroparse.WithLimits(zeroparse.Limits{Budget: n}))
		if err == nil {
			return false, ""
		}
		return true, err.Error()
	}
	lo, hi := uint64(1), uint64(1)
	for {
		if limited, _ := fails(hi); !limited {
			break
		}
		lo, hi = hi+1, hi*2
	}
	for lo < hi {
		mid := (lo + hi) / 2
		if limited, _ := fails(mid); limited {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	_, msg := fails(lo - 1)
	return lo, msg
}

// TestBudgetChargeIndependentOfPoolState checks that the spend a parse is charged does not depend on
// the memo storage that an earlier parse left in a pooled parser, nor on when a collection empties
// the pool.
func TestBudgetChargeIndependentOfPoolState(t *testing.T) {
	p, err := zeroparse.CompileSource(`@start
rule main = (pair / item ","?)* $$
rule pair = item ";"
rule item = word / num
rule word = [a-z]+
rule num = [0-9]+`, "main")
	if err != nil {
		t.Fatal(err)
	}
	input := strings.Repeat("abc,123,", 100)
	big := strings.Repeat("abc,123,", 4000)
	states := []struct {
		name string
		prep func()
	}{
		{"as is", func() {}},
		{"collected", func() { runtime.GC(); runtime.GC() }},
		{"warmed by a larger parse", func() {
			for range 4 {
				if _, err := p.Parse(big); err != nil {
					t.Fatal(err)
				}
			}
		}},
		{"warmed by the same parse", func() {
			if _, err := p.Parse(input); err != nil {
				t.Fatal(err)
			}
		}},
		{"collected again", func() { runtime.GC(); runtime.GC() }},
	}
	// The reference is measured with a warm pool; every state must reproduce it at once, because a
	// single parse is all a caller has.
	want, wantMsg := leastSteps(t, p, input)
	for _, s := range states {
		s.prep()
		if _, err := p.Parse(input, zeroparse.WithLimits(zeroparse.Limits{Budget: want})); err != nil {
			t.Errorf("%s: limit %d fails: %v", s.name, want, err)
		}
		s.prep()
		_, err := p.Parse(input, zeroparse.WithLimits(zeroparse.Limits{Budget: want - 1}))
		if err == nil || err.Error() != wantMsg {
			t.Errorf("%s: limit %d: error %v, want %q", s.name, want-1, err, wantMsg)
		}
	}
}
