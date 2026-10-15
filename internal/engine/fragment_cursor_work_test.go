//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestFragmentCursorFallbackInterruption(t *testing.T) {
	source := strings.Repeat("😀\xff", 1000) + "tail"
	in := newTextInput(source, CodePoints, false)
	// Endpoint conversion itself must cooperate even after matching succeeded.
	in.control = newWorkControl(nil, WorkLimits{MaxInputWork: 8})
	if err := catchFatal(t, func() { in.prefixByteOffset(source, 2000) }); !errors.Is(err, ErrWorkLimit) {
		t.Fatalf("fallback work limit: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	in.control = newWorkControl(ctx, WorkLimits{})
	cancel()
	if err := catchFatal(t, func() { in.prefixByteOffset(source, 2000) }); !errors.Is(err, context.Canceled) {
		t.Fatalf("fallback cancellation: %v", err)
	}
	if in.src != source || in.offs != nil {
		t.Fatal("interruption changed source or retained an index")
	}
	in.control = nil
	if got := in.prefixByteOffset(source, 2000); got != 5000 {
		t.Fatalf("retry endpoint=%d, want 5000", got)
	}
}
