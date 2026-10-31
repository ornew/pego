package csv_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ornew/zeroparse/parsers/csv"
)

// TestBudgetControls checks that the parser, generated with budget controls, stops at a budget limit
// and at a cancelled context, and that the same input parses without them.
func TestBudgetControls(t *testing.T) {
	const input = "a,b\n1,2\n"
	if _, err := csv.Parse(input); err != nil {
		t.Fatal(err)
	}
	_, err := csv.Parse(input, csv.WithLimits(csv.Limits{Budget: 1}))
	var limit *csv.LimitError
	if !errors.Is(err, csv.ErrLimit) || !errors.As(err, &limit) || limit.Kind != csv.LimitBudget {
		t.Fatalf("budget limit: got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := csv.Parse(input, csv.WithContext(ctx)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context: got %v", err)
	}
}
