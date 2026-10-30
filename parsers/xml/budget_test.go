package xml_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ornew/pego/parsers/xml"
)

// TestBudgetControls checks that the parser, generated with budget controls, stops at a budget limit
// and at a cancelled context, and that the same input parses without them.
func TestBudgetControls(t *testing.T) {
	const input = "<a><b/></a>"
	if _, err := xml.Parse(input); err != nil {
		t.Fatal(err)
	}
	_, err := xml.Parse(input, xml.WithLimits(xml.Limits{Budget: 1}))
	var limit *xml.LimitError
	if !errors.Is(err, xml.ErrLimit) || !errors.As(err, &limit) || limit.Kind != xml.LimitBudget {
		t.Fatalf("budget limit: got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := xml.Parse(input, xml.WithContext(ctx)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context: got %v", err)
	}
}
