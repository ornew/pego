//go:build !zp_budget_controls_reference

package core

import (
	"testing"

	"github.com/ornew/zeroparse/internal/limits"
)

func TestResolveLimits(t *testing.T) {
	l, adaptive := ResolveLimits(limits.Limits{}, 10, false)
	if l.Budget != limits.DefaultBudgetFloor || adaptive {
		t.Fatalf("short input: %+v adaptive=%t", l, adaptive)
	}
	if l, _ := ResolveLimits(limits.Limits{}, 1<<20, false); l.Budget != 1<<30 {
		t.Fatalf("large input: %+v", l)
	}
	if l, _ := ResolveLimits(limits.Limits{Budget: 7}, 1<<20, false); l.Budget != 7 {
		t.Fatalf("explicit: %+v", l)
	}
	if l, _ := ResolveLimits(limits.Limits{Budget: limits.Unlimited}, 1<<20, false); l.Budget != 0 {
		t.Fatalf("unlimited: %+v", l)
	}
	l, adaptive = ResolveLimits(limits.Limits{}, 0, true)
	if l.Budget != limits.DefaultBudgetFloor || !adaptive {
		t.Fatalf("stream: %+v adaptive=%t", l, adaptive)
	}
}

// A stream's default budget follows the furthest position demanded, and never shrinks.
func TestDemand(t *testing.T) {
	l, adaptive := ResolveLimits(limits.Limits{}, 0, true)
	c := NewBudgetControl(nil, l)
	c.Adaptive = adaptive
	c.Demand(100)
	if c.Limits.Budget != limits.DefaultBudgetFloor {
		t.Fatalf("below the floor: %d", c.Limits.Budget)
	}
	c.Demand(3 << 20)
	if c.Limits.Budget != 3<<30 {
		t.Fatalf("grown: %d", c.Limits.Budget)
	}
	c.Demand(5)
	if c.Limits.Budget != 3<<30 {
		t.Fatalf("shrunk: %d", c.Limits.Budget)
	}
	c.Demand(3<<20 + 1)
	if c.Limits.Budget != (3<<20+1)*1024 {
		t.Fatalf("grown again: %d", c.Limits.Budget)
	}
	var none *BudgetControl
	none.Demand(5)
	fixed := NewBudgetControl(nil, limits.Limits{Budget: 9})
	fixed.Demand(1 << 30)
	if fixed.Limits.Budget != 9 {
		t.Fatalf("explicit budget grew: %d", fixed.Limits.Budget)
	}
}
