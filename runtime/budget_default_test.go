package runtime

import (
	"math"
	"strings"
	"testing"
)

func TestDefaultBudget(t *testing.T) {
	for _, tc := range []struct{ n, want uint64 }{
		{0, 1 << 20},
		{1024, 1 << 20},
		{1025, 1025 * 1024},
		{math.MaxUint64, math.MaxUint64 - 1},
	} {
		if got := defaultBudget(tc.n); got != tc.want {
			t.Errorf("defaultBudget(%d) = %d, want %d", tc.n, got, tc.want)
		}
	}
	if got := inputUnits(strings.Repeat("é", 2000), CodePoints); got != 2000 {
		t.Errorf("code points: %d", got)
	}
	if got := inputUnits(strings.Repeat("é", 2000), Bytes); got != 4000 {
		t.Errorf("bytes: %d", got)
	}
}

// A zero budget resolves per unit of input when the parser has controls; Unlimited leaves no control, and
// a stream starts at the floor and follows what the parser demands.
func TestPrepareOptionsDefault(t *testing.T) {
	o := prepareOptions(parseOptions{controls: true}, 5000, false)
	if o.control == nil || o.control.limits.Budget != 5000*1024 {
		t.Fatalf("default: %+v", o.control)
	}
	o = prepareOptions(parseOptions{controls: true, limits: Limits{Budget: Unlimited}}, 5000, false)
	if o.control != nil {
		t.Fatal("Unlimited made a control")
	}
	o = prepareOptions(parseOptions{limits: Limits{Budget: Unlimited}}, 5000, false)
	if o.control != nil {
		t.Fatal("Unlimited without controls made a control")
	}
	o = prepareOptions(parseOptions{}, 5000, false)
	if o.control != nil {
		t.Fatal("a parser without controls got a default")
	}
	o = prepareOptions(parseOptions{controls: true, limits: Limits{Budget: 9}}, 5000, false)
	if o.control == nil || o.control.limits.Budget != 9 {
		t.Fatalf("explicit: %+v", o.control)
	}
	o = prepareOptions(parseOptions{controls: true}, 0, true)
	if o.control == nil || o.control.limits.Budget != 1<<20 || !o.control.adaptive {
		t.Fatalf("stream: %+v", o.control)
	}
	o.control.demand(3 << 20)
	if o.control.limits.Budget != 3<<30 {
		t.Fatalf("grown: %d", o.control.limits.Budget)
	}
}
