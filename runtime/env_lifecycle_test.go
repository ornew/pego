package runtime

import (
	"fmt"
	"slices"
	"testing"
)

// variableCounts cover a single leaf, the full leaf, and the trie of two and three levels.
var variableCounts = []int{1, 2, 3, 4, 5, 16, 17, 256, 257, 280}

func testParser(n int) *Parser {
	vals := make([]any, n)
	for i := range vals {
		vals[i] = i
	}
	p := &Parser{envDepth: envDepthOf(n)}
	p.Env = newEnv(vals, p.envDepth)
	return p
}

func TestBindingSnapshots(t *testing.T) {
	for _, n := range variableCounts {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			p := testParser(n)
			d := p.envDepth
			first := p.Env
			last := n - 1
			for i := 0; i < 100; i++ {
				p.bind(last, last)
				p.bind(0, 0)
			}
			if p.Env != first {
				t.Fatal("equal binding allocates state")
			}
			p.bind(0, "two")
			second := p.Env
			p.bind(0, "two")
			if p.Env != second {
				t.Fatal("equal rebinding replaces state")
			}
			if n > 1 {
				p.bind(last, "three")
				if got := second.get(last, d); got != last {
					t.Fatal("replacement changed saved environment")
				}
				if got := p.Env.get(0, d); got != "two" {
					t.Fatal("replacement lost an earlier binding")
				}
			}
			p.Env = first
			p.bind(0, false)
			if got := p.Env.get(0, d); got != false {
				t.Fatal("binding after rollback was lost")
			}
			if got := second.get(0, d); got != "two" {
				t.Fatal("rollback binding changed another environment")
			}
			for i := 0; i < n; i++ {
				if got := first.get(i, d); got != i {
					t.Fatalf("first environment changed at slot %d: %v", i, got)
				}
			}
		})
	}
}

// Compare persistent environments with independent value slices through changes
// and restores.
func TestBindingRestoresReferenceValues(t *testing.T) {
	for _, n := range variableCounts {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			type snapshot struct {
				env    *Env
				values []any
			}
			p := testParser(n)
			d := p.envDepth
			values := make([]any, n)
			for i := range values {
				values[i] = i
			}
			saved := []snapshot{{p.Env, slices.Clone(values)}}
			verify := func(e *Env, want []any) {
				t.Helper()
				for i, w := range want {
					if got := e.get(i, d); got != w {
						t.Fatalf("wrong binding in slot %d: %v, want %v", i, got, w)
					}
				}
			}
			for i := 0; i < 500; i++ {
				if i%5 == 0 {
					s := saved[(i*13)%len(saved)]
					p.Env = s.env
					values = slices.Clone(s.values)
				}
				slot := (i * 7) % n
				var value any
				switch i % 3 {
				case 0:
					value = i % 11
				case 1:
					value = fmt.Sprint("v", slot)
				case 2:
					value = i%2 == 0
				}
				p.bind(slot, value)
				values[slot] = value
				verify(p.Env, values)
				if i%7 == 0 {
					saved = append(saved, snapshot{p.Env, slices.Clone(values)})
				}
				for _, s := range saved {
					verify(s.env, s.values)
				}
			}
		})
	}
}

// Two branches that continue from one saved environment do not see each other's bindings.
func TestBindingBranchesFromOneSnapshot(t *testing.T) {
	for _, n := range variableCounts {
		p := testParser(n)
		d := p.envDepth
		saved := p.Env
		p.bind(0, "a")
		left := p.Env
		p.Env = saved
		p.bind(n-1, "b")
		right := p.Env
		if n > 1 && (left.get(n-1, d) != n-1 || right.get(0, d) != 0) {
			t.Fatalf("%d variables: a branch saw the binding of another", n)
		}
		if saved.get(0, d) != 0 || saved.get(n-1, d) != n-1 {
			t.Fatalf("%d variables: the saved environment changed", n)
		}
	}
}
