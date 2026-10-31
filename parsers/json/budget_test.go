package json_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ornew/zeroparse/parsers/json"
)

// TestBudgetControls checks that the parser, generated with budget controls, stops at a budget limit
// and at a cancelled context, and that the same input parses without them.
func TestBudgetControls(t *testing.T) {
	const input = "[1, 2, 3]"
	if _, err := json.Parse(input); err != nil {
		t.Fatal(err)
	}
	_, err := json.Parse(input, json.WithLimits(json.Limits{Budget: 1}))
	var limit *json.LimitError
	if !errors.Is(err, json.ErrLimit) || !errors.As(err, &limit) || limit.Kind != json.LimitBudget {
		t.Fatalf("budget limit: got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := json.Parse(input, json.WithContext(ctx)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context: got %v", err)
	}
}
