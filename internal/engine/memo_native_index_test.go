package engine

import (
	"fmt"
	"strconv"
	"testing"
)

func TestDeferredMemoBitAddress(t *testing.T) {
	for _, tc := range []struct {
		pos, rule, stride, word int
		bit                     uint64
	}{
		{0, 0, 1, 0, 1},
		{1, 63, 64, 1, 1 << 63},
		{63, 1, 65, 64, 1},
		{1024, 174, 175, 2802, 1 << 46},
		{1 << 25, 63, 64, 1 << 25, 1 << 63},
		{1 << 26, 0, 64, 1 << 26, 1},
		{(1 << 27) - 1, 63, 64, (1 << 27) - 1, 1 << 63},
		{1 << 27, 0, 128, 1 << 28, 1},
		{(1 << 28) - 1, 63, 128, (1 << 29) - 2, 1 << 63},
	} {
		t.Run(fmt.Sprintf("%d/%d/%d", tc.pos, tc.rule, tc.stride), func(t *testing.T) {
			word, bit := seenBit(tc.pos, tc.rule, tc.stride)
			if word != tc.word || bit != tc.bit {
				t.Fatalf("address=(%d,%#x), want (%d,%#x)", word, bit, tc.word, tc.bit)
			}
		})
	}
}

func TestDeferredMemoBackingByteOverflow(t *testing.T) {
	if strconv.IntSize != 32 {
		t.Skip("32-bit backing-array limit")
	}
	defer func() {
		x := recover()
		f, ok := x.(fatal)
		if !ok || f.err.Error() != "deferred memoization bitset size overflows uintptr" {
			t.Fatalf("panic=%v; want backing-byte overflow error", x)
		}
	}()
	seenBit((1<<28)-1, 64, 128)
}

func TestDeferredMemoGrowthCapacity(t *testing.T) {
	for _, tc := range []struct{ used, word, want int }{
		{0, 0, 64}, {64, 64, 128}, {64, 1000, 1001},
		{200000000, 200000000, min(400000000, int(^uint(0)/8))},
		{400000000, 400000000, min(800000000, int(^uint(0)/8))},
	} {
		if got := seenCapacity(tc.used, tc.word); got != tc.want {
			t.Fatalf("capacity(%d,%d)=%d, want %d", tc.used, tc.word, got, tc.want)
		}
	}
	seen := memoTable{stride: 65}
	// Cross words and growth boundaries, revisit calls in descending order,
	// then change the rule at a previously visited position.
	for _, pos := range []int{0, 1, 63, 64, 65, 1023, 1024, 4096} {
		if !seen.markSeen(pos, 64) {
			t.Fatal("first call treated as repeated")
		}
	}
	for _, pos := range []int{4096, 1024, 1023, 65, 64, 63, 1, 0} {
		if seen.markSeen(pos, 64) {
			t.Fatal("repeated call treated as first")
		}
		if !seen.markSeen(pos, 63) {
			t.Fatal("different rule treated as repeated")
		}
	}
	if strconv.IntSize == 32 && cap(seen.seen) != len(seen.seen) {
		t.Fatal("32-bit bitset overallocated")
	}
	if cap(seen.seen) > int(^uint(0)/8) {
		t.Fatal("bitset capacity exceeds native backing-array limit")
	}
}
