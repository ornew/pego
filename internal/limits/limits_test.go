package limits

import (
	"errors"
	"math"
	"testing"
)

func TestLimitError(t *testing.T) {
	for _, tc := range []struct {
		err  *LimitError
		want string
	}{
		{nil, "parse limit exceeded"},
		{&LimitError{Kind: LimitBudget, Limit: 5, Used: 4, Requested: 2}, "parse budget limit exceeded: limit 5, used 4, requested 2"},
	} {
		if got := tc.err.Error(); got != tc.want {
			t.Errorf("Error() = %q, want %q", got, tc.want)
		}
	}
	var err error = &LimitError{Kind: LimitBudget}
	if !errors.Is(err, ErrLimit) {
		t.Error("errors.Is(LimitError, ErrLimit) = false")
	}
	if errors.Is(err, errors.New("parse limit exceeded")) {
		t.Error("LimitError matched an unrelated error with the same text")
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
