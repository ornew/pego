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

// prepareText rejects a text longer than MaxInput before any work, counting code points only when
// the byte length passes the limit; zero and Unlimited set no bound and need no control.
func TestPrepareTextMaxInput(t *testing.T) {
	text := strings.Repeat("é", 10) // 10 code points, 20 bytes
	over := func(o parseOptions) (le *LimitError) {
		defer func() {
			if x := recover(); x != nil {
				f, ok := x.(fatal)
				if !ok {
					panic(x)
				}
				le, _ = f.err.(*LimitError)
			}
		}()
		prepareText(o, text)
		return nil
	}
	for _, tc := range []struct {
		unit PositionUnit
		max  uint64
		want uint64 // the length reported, or zero when the text is accepted
	}{
		{CodePoints, 0, 0}, {CodePoints, Unlimited, 0}, {CodePoints, 10, 0}, {CodePoints, 9, 10}, {CodePoints, 20, 0},
		{Bytes, 20, 0}, {Bytes, 19, 20}, {Bytes, 10, 20},
	} {
		le := over(parseOptions{unit: tc.unit, limits: Limits{MaxInput: tc.max}, controls: true})
		if tc.want == 0 {
			if le != nil {
				t.Errorf("unit %d, max %d: %v", tc.unit, tc.max, le)
			}
			continue
		}
		if le == nil || le.Kind != LimitInput || le.Limit != tc.max || le.Requested != tc.want || le.Used != 0 {
			t.Errorf("unit %d, max %d: %+v, want length %d", tc.unit, tc.max, le, tc.want)
		}
	}
	// MaxInput makes no control of its own: with the budget unlimited the batch check is the whole of it.
	o := prepareText(parseOptions{controls: true, limits: Limits{Budget: Unlimited, MaxInput: 100}}, text)
	if o.control != nil {
		t.Fatal("a finite MaxInput made a control")
	}
	// A stream keeps the resolved bound for the parser.
	o = prepareOptions(parseOptions{controls: true, limits: Limits{MaxInput: 100}}, 0, true)
	if o.maxInput != 100 {
		t.Errorf("stream bound %d", o.maxInput)
	}
	o = prepareOptions(parseOptions{controls: true, limits: Limits{MaxInput: Unlimited}}, 0, true)
	if o.maxInput != 0 {
		t.Errorf("Unlimited stream bound %d", o.maxInput)
	}
	if !(Limits{}).disabled() || !(Limits{Budget: Unlimited, MaxInput: Unlimited}).disabled() || (Limits{MaxInput: 1}).disabled() {
		t.Error("disabled")
	}
}
