//go:build !pego_work_controls_reference

package core

import (
	"context"
	"errors"
	"testing"

	"github.com/ornew/pego/internal/limits"
)

func TestDiagnosticRenameAccountsForLargeExpectationSets(t *testing.T) {
	ids := make([]ExpID, 512)
	for i := range ids {
		ids[i] = ExpID(i + 1)
	}
	p := &Parser{Input: Input{Control: NewWorkControl(nil, limits.WorkLimits{MaxSteps: 64})}}
	if err := catchFatal(t, func() { RenamedExpected(p, ids, 999) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("rename work limit = %v, want ErrWorkLimit", err)
	}
	if got := p.Control.StepsUsed; got != 64 {
		t.Fatalf("rename steps = %d, want 64", got)
	}

	c := newPollingContext(1)
	p.Control = NewWorkControl(c, limits.WorkLimits{})
	if err := catchFatal(t, func() { RenamedExpected(p, ids, 999) }); !errors.Is(err, context.Canceled) {
		t.Fatalf("rename cancellation = %v, want context.Canceled", err)
	}
}
