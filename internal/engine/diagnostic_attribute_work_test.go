//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"errors"
	"testing"
)

func TestDiagnosticRenameAccountsForLargeExpectationSets(t *testing.T) {
	ids := make([]expID, 512)
	for i := range ids {
		ids[i] = expID(i + 1)
	}
	p := &parser{input: input{control: newWorkControl(nil, WorkLimits{MaxSteps: 64})}}
	if err := catchFatal(t, func() { renamedExpected(p, ids, 999) }); !errors.Is(err, ErrWorkLimit) {
		t.Fatalf("rename work limit = %v, want ErrWorkLimit", err)
	}
	if got := p.control.stepsUsed; got != 64 {
		t.Fatalf("rename steps = %d, want 64", got)
	}

	c := newPollingContext(1)
	p.control = newWorkControl(c, WorkLimits{})
	if err := catchFatal(t, func() { renamedExpected(p, ids, 999) }); !errors.Is(err, context.Canceled) {
		t.Fatalf("rename cancellation = %v, want context.Canceled", err)
	}
}
