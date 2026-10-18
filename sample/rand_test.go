package sample

import (
	"slices"
	"testing"
)

// TestPCGStream pins the random numbers of a seed: they must not change with
// the Go release, and a change to them changes every generated input.
func TestPCGStream(t *testing.T) {
	p := newPCG(1, 2)
	if got := p.Uint64(); got != 14192431797130687760 {
		t.Fatalf("Uint64 = %d", got)
	}
	var got []int
	for _, n := range []int{2, 3, 10, 1000, 1 << 40, 1<<63 - 1} {
		got = append(got, p.IntN(n))
	}
	if want := []int{0, 2, 7, 234, 547831175868, 4610872105503713596}; !slices.Equal(got, want) {
		t.Fatalf("IntN = %v, want %v", got, want)
	}
	s := []int{0, 1, 2, 3, 4, 5, 6, 7}
	p.Shuffle(len(s), func(i, j int) { s[i], s[j] = s[j], s[i] })
	if want := []int{7, 5, 4, 2, 3, 1, 0, 6}; !slices.Equal(s, want) {
		t.Fatalf("Shuffle = %v, want %v", s, want)
	}
}
