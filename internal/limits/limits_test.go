package limits

import (
	"errors"
	"math"
	"testing"
)

func TestWorkLimitError(t *testing.T) {
	for _, tc := range []struct {
		err  *WorkLimitError
		want string
	}{
		{nil, "parse work limit exceeded"},
		{&WorkLimitError{Kind: InputWorkLimit, Limit: 5, Used: 4, Requested: 2}, "parse input work limit exceeded: limit 5, used 4, requested 2"},
		{&WorkLimitError{Kind: StepWorkLimit, Limit: 3, Used: 3, Requested: 1}, "parse steps limit exceeded: limit 3, used 3, requested 1"},
	} {
		if got := tc.err.Error(); got != tc.want {
			t.Errorf("Error() = %q, want %q", got, tc.want)
		}
	}
	var err error = &WorkLimitError{Kind: StepWorkLimit}
	if !errors.Is(err, ErrWorkLimit) {
		t.Error("errors.Is(WorkLimitError, ErrWorkLimit) = false")
	}
	if errors.Is(err, errors.New("parse work limit exceeded")) {
		t.Error("WorkLimitError matched an unrelated error with the same text")
	}
}

func TestArithmetic(t *testing.T) {
	for _, tc := range []struct {
		used, requested, limit uint64
		want                   bool
	}{
		{0, 5, 0, false},
		{4, 1, 5, false},
		{4, 2, 5, true},
		{6, 0, 5, true},
		{1, math.MaxUint64, 5, true},
	} {
		if got := ExceedsLimit(tc.used, tc.requested, tc.limit); got != tc.want {
			t.Errorf("ExceedsLimit(%d, %d, %d) = %v, want %v", tc.used, tc.requested, tc.limit, got, tc.want)
		}
	}
	if !ReachesInterval(0, 256, 256) || ReachesInterval(0, 255, 256) || !ReachesInterval(300, 0, 256) || !ReachesInterval(1, math.MaxUint64, 256) {
		t.Error("ReachesInterval disagrees with used+requested >= interval")
	}
	if SaturatingAdd(math.MaxUint64-1, 2) != math.MaxUint64 || SaturatingAdd(2, 3) != 5 {
		t.Error("SaturatingAdd does not saturate exactly at overflow")
	}
}
