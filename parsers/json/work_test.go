package json_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ornew/pego/parsers/json"
)

// TestWorkControls checks that the parser, generated with work controls, stops at a work limit
// and at a cancelled context, and that the same input parses without them.
func TestWorkControls(t *testing.T) {
	const input = "[1, 2, 3]"
	if _, err := json.Parse(input); err != nil {
		t.Fatal(err)
	}
	_, err := json.Parse(input, json.WithLimits(json.Limits{MaxWork: 1}))
	var limit *json.LimitError
	if !errors.Is(err, json.ErrLimit) || !errors.As(err, &limit) || limit.Kind != json.LimitWork {
		t.Fatalf("work limit: got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := json.Parse(input, json.WithContext(ctx)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context: got %v", err)
	}
}
