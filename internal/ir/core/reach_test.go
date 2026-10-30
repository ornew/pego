package core

import (
	"slices"
	"testing"
)

func TestReach(t *testing.T) {
	// 0 -> 1, 2; 1 -> 3; 2 -> 3; 3 -> 3 (cycle); 4 -> 1; 5 unreferenced.
	m := &Module{Rules: make([]*Rule, 6), Calls: [][]int{{1, 2}, {3}, {3}, {3}, {1}, nil}}
	enter := func(rule int) Entry {
		if rule == 2 {
			return EnterWrapper
		}
		return EnterSlot
	}
	got := m.Reach([]Root{{0, EnterSlot}}, enter)
	want := []Entry{EnterSlot, EnterSlot, EnterWrapper, EnterSlot, 0, 0}
	if !slices.Equal(got, want) {
		t.Errorf("one root: got %v, want %v", got, want)
	}
	// A rule entered both ways has both entries, and a root's own entry is kept.
	got = m.Reach([]Root{{0, EnterSlot}, {2, EnterSlot}, {4, EnterWrapper}}, enter)
	want = []Entry{EnterSlot, EnterSlot, EnterSlot | EnterWrapper, EnterSlot, EnterWrapper, 0}
	if !slices.Equal(got, want) {
		t.Errorf("several roots: got %v, want %v", got, want)
	}
	if got := m.Reach(nil, enter); !slices.Equal(got, make([]Entry, 6)) {
		t.Errorf("no roots: got %v", got)
	}
}
