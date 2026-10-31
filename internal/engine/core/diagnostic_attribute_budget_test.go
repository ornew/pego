//go:build !zp_budget_controls_reference

package core

import (
	"context"
	"errors"
	"testing"

	"github.com/ornew/zeroparse/internal/limits"
)

func TestDiagnosticRenameAccountsForLargeExpectationSets(t *testing.T) {
	ids := make([]ExpID, 512)
	for i := range ids {
		ids[i] = ExpID(i + 1)
	}
	p := &Parser{Input: Input{Control: NewBudgetControl(nil, limits.Limits{Budget: 64})}}
	if err := catchFatal(t, func() { RenamedExpected(p, ids, 999) }); !errors.Is(err, limits.ErrLimit) {
		t.Fatalf("rename budget limit = %v, want ErrLimit", err)
	}
	if got := p.Control.Used; got != 64 {
		t.Fatalf("rename steps = %d, want 64", got)
	}

	c := newPollingContext(1)
	p.Control = NewBudgetControl(c, limits.Limits{})
	if err := catchFatal(t, func() { RenamedExpected(p, ids, 999) }); !errors.Is(err, context.Canceled) {
		t.Fatalf("rename cancellation = %v, want context.Canceled", err)
	}
}
