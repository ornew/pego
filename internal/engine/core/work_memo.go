package core

// rawValueEqual preserves the interface equality of memo keys and bindings.
// In particular, a typed nil pointer differs from a nil interface. Action
// ValueEqual has different nil semantics and must not be used for these keys.
func (p *Parser) rawValueEqual(l, r any) bool {
	if s, ok := l.(string); ok {
		t, ok := r.(string)
		return ok && len(s) == len(t) && p.CompareText(s, t) == 0
	}
	return l == r
}

func (p *Parser) memoKeyEqual(l, r []any) bool {
	if len(l) != len(r) {
		return false
	}
	for i, v := range l {
		p.StepWork()
		if !p.rawValueEqual(v, r[i]) {
			return false
		}
	}
	return true
}

func (p *Parser) bindControlled(name string, val any) {
	p.StepWork()
	e := p.Env
	if p.Bound == nil {
		p.Bound = make(map[string]bool)
	}
	if e == nil {
		p.Bound[name] = true
		p.Env = &Env{Name: name, Val: val}
		return
	}
	if p.rawValueEqual(e.Name, name) {
		if !p.rawValueEqual(e.Val, val) {
			p.Env = &Env{Name: name, Val: val, Next: e.Next}
		}
		return
	}
	// Map hashing and allocation are runtime operations that cannot be split
	// into engine checkpoints. Explicit name comparisons above/below can be.
	if !p.Bound[name] {
		p.Bound[name] = true
		p.Env = &Env{Name: name, Val: val, Next: e}
		return
	}
	old := e
	for old != nil {
		p.StepWork()
		if p.rawValueEqual(old.Name, name) {
			break
		}
		old = old.Next
	}
	if old == nil {
		p.Env = &Env{Name: name, Val: val, Next: e}
		return
	}
	if p.rawValueEqual(old.Val, val) {
		return
	}
	head := &Env{Name: name, Val: val}
	tail := head
	for cur := e; cur != old; cur = cur.Next {
		p.StepWork()
		tail.Next = &Env{Name: cur.Name, Val: cur.Val}
		tail = tail.Next
	}
	tail.Next = old.Next
	// Saved environments are immutable; only publish a completed prefix.
	p.Env = head
}

func (p *Parser) MemoGet(k MemoKey) (*MemoEntry, bool) {
	if !WorkControlsEnabled || p.Control == nil {
		return p.Memo.Get(k)
	}
	p.StepWork()
	t := p.Memo
	i := k.Pos - t.Base
	if i >= t.Gap {
		i += t.GapLen
	}
	if i < 0 || i >= len(t.Slots) {
		return nil, false
	}
	for e := t.Slots[i]; e != nil; e = e.Next {
		p.StepWork()
		if e.Rule == int32(k.Rule) && e.Min == int32(k.Min) && e.missingSilent == k.MissingSilent && p.memoKeyEqual(e.Env, k.Env) {
			return e, true
		}
	}
	return nil, false
}

func (p *Parser) memoGrowing(k MemoKey) (*MemoEntry, bool) {
	if !WorkControlsEnabled || p.Control == nil {
		return p.Memo.growing(k)
	}
	p.StepWork()
	t := p.Memo
	i := k.Pos - t.Base
	if i >= t.Gap {
		i += t.GapLen
	}
	if i < 0 || i >= len(t.Slots) {
		return nil, false
	}
	for e := t.Slots[i]; e != nil; e = e.Next {
		p.StepWork()
		if e.Growing && e.Rule == int32(k.Rule) && e.Min == int32(k.Min) && e.missingSilent == k.MissingSilent {
			return e, true
		}
	}
	return nil, false
}

func (p *Parser) MemoPut(k MemoKey, e *MemoEntry) {
	if !WorkControlsEnabled || p.Control == nil {
		p.Memo.Put(k, e)
		return
	}
	p.StepWork()
	t := p.Memo
	i := k.Pos - t.Base
	if i < 0 {
		return
	}
	if i >= t.Gap {
		i += t.GapLen
	}
	slots := t.Slots
	if i >= len(slots) {
		p.growthWork(len(slots), i+1)
		if i < cap(slots) {
			slots = slots[:i+1]
		} else {
			grown := make([]*MemoEntry, i+1, max(2*cap(slots), i+1, 64))
			copy(grown, slots)
			slots = grown
		}
	}
	link := &slots[i]
	var replaced *MemoEntry
	for x := *link; x != nil; link, x = &x.Next, x.Next {
		p.StepWork()
		if x.Rule == int32(k.Rule) && x.Min == int32(k.Min) && x.missingSilent == k.MissingSilent && p.memoKeyEqual(x.Env, k.Env) {
			replaced = x
			break
		}
	}
	p.StepWork()
	// No checkpoint may separate the link swap from retiring its old entry.
	// In particular, do not publish a partially copied slot array or key.
	t.Slots = slots
	e.Rule, e.Min, e.Pos, e.Env = int32(k.Rule), int32(k.Min), k.Pos, k.Env
	e.missingSilent = k.MissingSilent
	if replaced != nil {
		e.Next = replaced.Next
		*link = e
		if replaced != e {
			t.Retire(replaced)
		}
		return
	}
	e.Next = slots[i]
	slots[i] = e
}

func (p *Parser) memoFirstCall(pos, r int) bool {
	if !WorkControlsEnabled || p.Control == nil {
		return p.Memo.firstCall(pos, r)
	}
	p.StepWork()
	t := p.Memo
	if t.Calls == nil || !t.Calls[r].Eager {
		i := pos*t.Stride + r
		w := i >> 6
		if w >= len(t.Seen) {
			n := len(t.Seen) + max(w+1-len(t.Seen), len(t.Seen), 64)
			// The new words are the work, whether they come from spare capacity or an allocation.
			p.chargeValues(n - len(t.Seen))
			seen := t.Seen
			if n <= cap(seen) {
				seen = seen[:n]
				clear(seen[len(t.Seen):])
			} else {
				seen = make([]uint64, n)
				copy(seen, t.Seen)
			}
			// The original estimator now needs only scalar operations. Keep
			// its discount and eager thresholds, and publish after all copies.
			t.Seen = seen
		}
	}
	return t.firstCall(pos, r)
}
