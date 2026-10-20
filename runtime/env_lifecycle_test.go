package runtime

import (
	"fmt"
	"testing"
)

func TestBindingSnapshots(t *testing.T) {
	p := &Parser{}
	p.bind("x", 1)
	first := p.Env
	for i := 0; i < 100; i++ {
		p.bind("x", 1)
	}
	if p.Env != first {
		t.Fatal("equal head assignment allocates state")
	}
	p.bind("y", "two")
	second := p.Env
	p.bind("x", 1)
	if p.Env != second {
		t.Fatal("equal non-head assignment replaces state")
	}
	p.bind("x", 3)
	if got, _ := lookupBinding(second, "x"); got != 1 {
		t.Fatal("replacement changed saved environment")
	}
	if got, _ := lookupBinding(p.Env, "y"); got != "two" {
		t.Fatal("replacement lost prefix")
	}
	p.Env = first
	p.bind("y", false)
	if got, _ := lookupBinding(p.Env, "y"); got != false {
		t.Fatal("seen name absent after rollback was not bound")
	}
	p.Env = nil
	p.bind("z", true)
	p.bind("w", "four")
	p.bind("z", false)
	names := make(map[string]bool)
	for e := p.Env; e != nil; e = e.next {
		if names[e.name] {
			t.Fatalf("duplicate binding %s", e.name)
		}
		names[e.name] = true
	}
	if len(names) != 2 {
		t.Fatalf("have %d bindings", len(names))
	}
	if got, _ := lookupBinding(first, "x"); got != 1 {
		t.Fatal("first environment changed")
	}
}

func lookupBinding(e *Env, name string) (any, bool) {
	for ; e != nil; e = e.next {
		if e.name == name {
			return e.val, true
		}
	}
	return nil, false
}

// Compare persistent environments with independent value maps through changes
// and restores, including names first seen only in abandoned branches.
func TestBindingRestoresReferenceValues(t *testing.T) {
	type snapshot struct {
		env    *Env
		values map[string]any
	}
	p := &Parser{}
	values := map[string]any{}
	saved := []snapshot{{}}
	clone := func(m map[string]any) map[string]any {
		n := make(map[string]any, len(m))
		for k, v := range m {
			n[k] = v
		}
		return n
	}
	verify := func(e *Env, want map[string]any) {
		t.Helper()
		seen := make(map[string]bool)
		for ; e != nil; e = e.next {
			if seen[e.name] || want[e.name] != e.val {
				t.Fatalf("wrong binding %s=%v", e.name, e.val)
			}
			seen[e.name] = true
		}
		if len(seen) != len(want) {
			t.Fatalf("%d bindings, want %d", len(seen), len(want))
		}
	}
	for i := 0; i < 500; i++ {
		if i%5 == 0 {
			s := saved[(i*13)%len(saved)]
			p.Env = s.env
			values = clone(s.values)
		}
		name := fmt.Sprintf("v%d", (i*7)%16)
		var value any
		switch i % 3 {
		case 0:
			value = i % 11
		case 1:
			value = name
		case 2:
			value = i%2 == 0
		}
		p.bind(name, value)
		values[name] = value
		verify(p.Env, values)
		if i%7 == 0 {
			saved = append(saved, snapshot{p.Env, clone(values)})
		}
		for _, s := range saved {
			verify(s.env, s.values)
		}
	}
}

// A hint created after switching between single-name snapshots must still know
// names from the earlier snapshot when it is restored behind another binding.
func TestBindingRestoresBeforeSecondName(t *testing.T) {
	p := &Parser{}
	p.bind("x", 1)
	saved := p.Env
	p.Env = nil
	p.bind("y", 2)
	p.bind("z", 3)
	p.Env = saved
	p.bind("w", 4)
	p.bind("x", 5)
	count := 0
	for e := p.Env; e != nil; e = e.next {
		count++
	}
	if count != 2 {
		t.Fatalf("%d bindings for two names", count)
	}
	if saved.val != 1 {
		t.Fatal("saved binding changed")
	}
}
