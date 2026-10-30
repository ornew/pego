package yaml_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ornew/pego/parsers/yaml"
)

// TestWorkControls checks that the parser, generated with work controls, stops at a work limit
// and at a cancelled context, and that the same input parses without them.
func TestWorkControls(t *testing.T) {
	const input = "a: 1\nb: 2\n"
	if _, err := yaml.Parse(input); err != nil {
		t.Fatal(err)
	}
	_, err := yaml.Parse(input, yaml.WithLimits(yaml.Limits{MaxWork: 1}))
	var limit *yaml.LimitError
	if !errors.Is(err, yaml.ErrLimit) || !errors.As(err, &limit) || limit.Kind != yaml.LimitWork {
		t.Fatalf("work limit: got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := yaml.Parse(input, yaml.WithContext(ctx)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context: got %v", err)
	}
}
