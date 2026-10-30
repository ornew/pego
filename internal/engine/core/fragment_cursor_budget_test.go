//go:build !pego_budget_controls_reference

package core

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/limits"
)

func TestFragmentCursorFallbackInterruption(t *testing.T) {
	source := strings.Repeat("😀\xff", 1000) + "tail"
	in := NewTextInput(source, CodePoints, false)
	// Endpoint conversion itself must cooperate even after matching succeeded.
	in.Control = NewBudgetControl(nil, limits.Limits{Budget: 8})
	if err := catchFatal(t, func() { in.PrefixByteOffset(source, 2000) }); !errors.Is(err, limits.ErrLimit) {
		t.Fatalf("fallback budget limit: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	in.Control = NewBudgetControl(ctx, limits.Limits{})
	cancel()
	if err := catchFatal(t, func() { in.PrefixByteOffset(source, 2000) }); !errors.Is(err, context.Canceled) {
		t.Fatalf("fallback cancellation: %v", err)
	}
	if in.Src != source || in.Offs != nil {
		t.Fatal("interruption changed source or retained an index")
	}
	in.Control = nil
	if got := in.PrefixByteOffset(source, 2000); got != 5000 {
		t.Fatalf("retry endpoint=%d, want 5000", got)
	}
}
