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
	p := &Parser{Input: Input{Control: NewWorkControl(nil, limits.Limits{MaxWork: 64})}}
	if err := catchFatal(t, func() { RenamedExpected(p, ids, 999) }); !errors.Is(err, limits.ErrLimit) {
		t.Fatalf("rename work limit = %v, want ErrLimit", err)
	}
	if got := p.Control.Used; got != 64 {
		t.Fatalf("rename steps = %d, want 64", got)
	}

	c := newPollingContext(1)
	p.Control = NewWorkControl(c, limits.Limits{})
	if err := catchFatal(t, func() { RenamedExpected(p, ids, 999) }); !errors.Is(err, context.Canceled) {
		t.Fatalf("rename cancellation = %v, want context.Canceled", err)
	}
}
