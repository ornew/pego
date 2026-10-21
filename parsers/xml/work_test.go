package xml_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ornew/pego/parsers/xml"
)

// TestWorkControls checks that the parser, generated with work controls, stops at a work limit
// and at a cancelled context, and that the same input parses without them.
func TestWorkControls(t *testing.T) {
	const input = "<a><b/></a>"
	if _, err := xml.Parse(input); err != nil {
		t.Fatal(err)
	}
	_, err := xml.Parse(input, xml.WithWorkLimits(xml.WorkLimits{MaxSteps: 1}))
	var limit *xml.WorkLimitError
	if !errors.Is(err, xml.ErrWorkLimit) || !errors.As(err, &limit) || limit.Kind != xml.StepWorkLimit {
		t.Fatalf("step limit: got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := xml.Parse(input, xml.WithContext(ctx)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context: got %v", err)
	}
}
