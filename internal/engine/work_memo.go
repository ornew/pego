package engine

// rawValueEqual preserves the interface equality of memo keys and bindings.
// In particular, a typed nil pointer differs from a nil interface. Action
// valueEqual has different nil semantics and must not be used for these keys.
func (p *parser) rawValueEqual(l, r any) bool {
	if s, ok := l.(string); ok {
		t, ok := r.(string)
		return ok && len(s) == len(t) && p.compareText(s, t) == 0
	}
	return l == r
}

func (p *parser) memoKeyEqual(l, r []any) bool {
	if len(l) != len(r) {
		return false
	}
	for i, v := range l {
		p.stepWork()
		if !p.rawValueEqual(v, r[i]) {
			return false
		}
	}
	return true
}

func (p *parser) bindControlled(name string, val any) {
	p.stepWork()
	e := p.env
	if p.bound == nil {
		p.bound = make(map[string]bool)
	}
	if e == nil {
		p.bound[name] = true
		p.env = &env{name: name, val: val}
		return
	}
	if p.rawValueEqual(e.name, name) {
		if !p.rawValueEqual(e.val, val) {
			p.env = &env{name: name, val: val, next: e.next}
		}
		return
	}
	// Map hashing and allocation are runtime operations that cannot be split
	// into engine checkpoints. Explicit name comparisons above/below can be.
	if !p.bound[name] {
		p.bound[name] = true
		p.env = &env{name: name, val: val, next: e}
		return
	}
	old := e
	for old != nil {
		p.stepWork()
		if p.rawValueEqual(old.name, name) {
			break
		}
		old = old.next
	}
	if old == nil {
		p.env = &env{name: name, val: val, next: e}
		return
	}
	if p.rawValueEqual(old.val, val) {
		return
	}
	head := &env{name: name, val: val}
	tail := head
	for cur := e; cur != old; cur = cur.next {
		p.stepWork()
		tail.next = &env{name: cur.name, val: cur.val}
		tail = tail.next
	}
	tail.next = old.next
	// Saved environments are immutable; only publish a completed prefix.
	p.env = head
}

func (p *parser) memoGet(k memoKey) (*memoEntry, bool) {
	if !workControlsEnabled || p.control == nil {
		return p.memo.get(k)
	}
	p.stepWork()
	t := p.memo
	i := k.pos - t.base
	if i >= t.gap {
		i += t.gapLen
	}
	if i < 0 || i >= len(t.slots) {
		return nil, false
	}
	for e := t.slots[i]; e != nil; e = e.next {
		p.stepWork()
		if e.rule == int32(k.rule) && e.min == int32(k.min) && e.missingSilent == k.missingSilent && p.memoKeyEqual(e.env, k.env) {
			return e, true
		}
	}
	return nil, false
}

func (p *parser) memoGrowing(k memoKey) (*memoEntry, bool) {
	if !workControlsEnabled || p.control == nil {
		return p.memo.growing(k)
	}
	p.stepWork()
	t := p.memo
	i := k.pos - t.base
	if i >= t.gap {
		i += t.gapLen
	}
	if i < 0 || i >= len(t.slots) {
		return nil, false
	}
	for e := t.slots[i]; e != nil; e = e.next {
		p.stepWork()
		if e.growing && e.rule == int32(k.rule) && e.min == int32(k.min) && e.missingSilent == k.missingSilent {
			return e, true
		}
	}
	return nil, false
}

func (p *parser) memoPut(k memoKey, e *memoEntry) {
	if !workControlsEnabled || p.control == nil {
		p.memo.put(k, e)
		return
	}
	p.stepWork()
	t := p.memo
	i := k.pos - t.base
	if i < 0 {
		return
	}
	if i >= t.gap {
		i += t.gapLen
	}
	slots := t.slots
	if i >= len(slots) {
		p.growthWork(len(slots), i+1)
		if i < cap(slots) {
			slots = slots[:i+1]
		} else {
			grown := make([]*memoEntry, i+1, max(2*cap(slots), i+1, 64))
			copy(grown, slots)
			slots = grown
		}
	}
	link := &slots[i]
	var replaced *memoEntry
	for x := *link; x != nil; link, x = &x.next, x.next {
		p.stepWork()
		if x.rule == int32(k.rule) && x.min == int32(k.min) && x.missingSilent == k.missingSilent && p.memoKeyEqual(x.env, k.env) {
			replaced = x
			break
		}
	}
	p.stepWork()
	// No checkpoint may separate the link swap from retiring its old entry.
	// In particular, do not publish a partially copied slot array or key.
	t.slots = slots
	e.rule, e.min, e.pos, e.env = int32(k.rule), int32(k.min), k.pos, k.env
	e.missingSilent = k.missingSilent
	if replaced != nil {
		e.next = replaced.next
		*link = e
		if replaced != e {
			t.retire(replaced)
		}
		return
	}
	e.next = slots[i]
	slots[i] = e
}

func (p *parser) memoFirstCall(pos, r int) bool {
	if !workControlsEnabled || p.control == nil {
		return p.memo.firstCall(pos, r)
	}
	p.stepWork()
	t := p.memo
	if t.calls == nil || !t.calls[r].eager {
		i := pos*t.stride + r
		w := i >> 6
		if w >= len(t.seen) {
			n := len(t.seen) + max(w+1-len(t.seen), len(t.seen), 64)
			// The new words are the work, whether they come from spare capacity or an allocation.
			p.chargeValues(n - len(t.seen))
			seen := t.seen
			if n <= cap(seen) {
				seen = seen[:n]
				clear(seen[len(t.seen):])
			} else {
				seen = make([]uint64, n)
				copy(seen, t.seen)
			}
			// The original estimator now needs only scalar operations. Keep
			// its discount and eager thresholds, and publish after all copies.
			t.seen = seen
		}
	}
	return t.firstCall(pos, r)
}
