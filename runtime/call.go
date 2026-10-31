//go:build !zp_exec

package runtime

// The call protocol of a rule and left-recursion growth, as written by hand. Built with the tag
// zp_exec, the runtime uses the functions that internal/ir/exec/core prints from the shared
// compositions instead (call_exec.go).

// Call calls rule r at level min. An observed or Document parse takes
// callTracked, so that the call path of other parses has no observation or
// Document bookkeeping; callBody has the same logic with that bookkeeping.
func (p *Parser) Call(r *Rule, min int) (*Node, bool) {
	if p.Observation != nil || p.Document != nil {
		return p.callTracked(r, min)
	}
	p.Spend()
	start := p.Pos
	if (!r.Memo && !r.Leader) || p.FirstCall(r) {
		// Unmemoized call: no memo bookkeeping, and expectations need no isolation.
		if len(r.Scope) == 0 {
			return p.invokePlainBody(r, min)
		}
		rec := len(p.Recovered)
		v, ok := p.invoke(r, min)
		if !ok {
			p.Pos = start
			p.Recovered = p.Recovered[:rec]
		}
		return v, ok
	}
	env := p.memoEnv(r)
	// See the engine's callBegin: entries with recovered errors are reused only in the same kind of
	// context (inside or outside a lookahead) they were computed in.
	if e := p.memoGet(r.Id, start, min, env); e != nil && !p.memberStale(r.Member, start) && (e.growing || (!e.silent || p.Silent > 0) && (len(e.errs) == 0 || e.silent == (p.Silent > 0))) {
		p.readResult(e.growing, e.depth)
		if !e.growing {
			p.MergeExpected(e.far, e.expected)
		}
		if !e.ok {
			return nil, false
		}
		p.Pos = e.end
		p.Recovered = p.copyErrors(p.Recovered, e.errs)
		return e.node, true
	}
	rec := len(p.Recovered)
	depth, read := p.growths, p.enterCall()
	// The memoized result includes the expectations recorded in this call.
	m := p.Isolate(start)
	var v *Node
	var ok bool
	var e *memoEntry
	if r.Leader {
		v, ok = p.grow(r, min, env)
		e = p.memoGet(r.Id, start, min, env)
		if e == nil {
			// A stream commit pruned the slot of a growing start rule; its
			// result needs no entry.
			e = &memoEntry{}
		}
	} else {
		v, ok = p.invoke(r, min)
		if !ok {
			p.Recovered = p.Recovered[:rec]
		}
		if p.readDepth <= depth {
			// The result read the seed of a growth that began before the call, or an unstable
			// result, so it can change and is not memoized (readResult).
			e = &memoEntry{}
		} else {
			e = p.memo.alloc()
			*e = memoEntry{node: v, ok: ok, end: p.Pos, silent: p.Silent > 0,
				errs: p.copyErrors(nil, p.Recovered[rec:])}
			p.memoPut(r.Id, start, min, env, e)
		}
	}
	p.leaveCall(read)
	far, inner := p.Unisolate(m)
	e.far, e.expected = far, p.Keep(inner)
	p.MergeExpected(far, inner)
	if !ok {
		p.Pos = start
	}
	return v, ok
}

// grow grows the seed of a left-recursive leader: it memoizes a failure, then evaluates the
// body again while the match gets longer, and keeps the last longer result.
func (p *Parser) grow(r *Rule, min int, env []any) (*Node, bool) {
	start := p.Pos
	rec := len(p.Recovered)
	best := p.memo.alloc()
	*best = memoEntry{end: start, growing: true}
	p.memoPut(r.Id, start, min, env, best)
	prevPos, read := p.seedPos, p.enterCall()
	p.growths++
	p.seedPos = start
	best.depth = int32(p.growths)
	for {
		p.Spend()
		v, ok := p.invoke(r, min)
		if !ok || (best.ok && p.Pos <= best.end) {
			break
		}
		best = p.memo.alloc()
		*best = memoEntry{node: v, ok: true, end: p.Pos, growing: true, depth: int32(p.growths), errs: p.copyErrors(nil, p.Recovered[rec:])}
		p.memoPut(r.Id, start, min, env, best)
		p.Pos = start
		p.Recovered = p.Recovered[:rec]
	}
	best.growing = false
	best.silent = p.Silent > 0
	// The growth has read its seed for the last time: those reads are resolved.
	p.growths--
	p.seedPos = prevPos
	best.depth = finalDepth(p.readDepth, p.growths, best.silent || len(best.errs) > 0)
	p.leaveCall(read)
	// The call that made the growth reads its final result as on a memo hit.
	p.readResult(false, best.depth)
	p.Pos = best.end
	p.Recovered = p.copyErrors(p.Recovered[:rec], best.errs)
	return best.node, best.ok
}

// callBody is call without observation.
func (p *TParser) callBody(r *TRule, min int) (any, bool) {
	if p.TypedDocument != nil {
		return p.DocumentCall(r, min, nil)
	}
	p.Spend()
	start := p.Pos
	if !r.Memo && !r.Leader || p.FirstCall(r.Rule) {
		if len(r.Scope) == 0 {
			return p.invokePlainBody(r, min)
		}
		rec := len(p.Recovered)
		v, ok := p.invoke(r, min)
		if !ok {
			p.Pos = start
			p.Recovered = p.Recovered[:rec]
		}
		return v, ok
	}
	env := p.memoEnv(r.Rule)
	if e := p.memoGet(r.Id, start, min, env); e != nil && !p.memberStale(r.Member, start) && (e.growing || (!e.silent || p.Silent > 0) && (len(e.errs) == 0 || e.silent == (p.Silent > 0))) {
		p.Observation.memoHit()
		p.readResult(e.growing, e.depth)
		if !e.growing {
			p.MergeExpected(e.far, e.expected)
		}
		if !e.ok {
			return nil, false
		}
		p.Pos = e.end
		p.Recovered = p.copyErrors(p.Recovered, e.errs)
		return e.val, true
	}
	rec := len(p.Recovered)
	depth, read := p.growths, p.enterCall()
	m := p.Isolate(start)
	var v any
	var ok bool
	var e *tmemoEntry
	if r.Leader {
		v, ok = p.grow(r, min, env)
		e = p.memoGet(r.Id, start, min, env)
	} else {
		v, ok = p.invoke(r, min)
		if !ok {
			p.Recovered = p.Recovered[:rec]
		}
		if p.readDepth <= depth {
			e = &tmemoEntry{} // a result that can change is not memoized (readResult)
		} else {
			e = p.memoSlab.one()
			*e = tmemoEntry{val: v, ok: ok, end: p.Pos, silent: p.Silent > 0,
				errs: p.copyErrors(nil, p.Recovered[rec:])}
			p.memoPut(r.Id, start, min, env, e)
		}
	}
	p.leaveCall(read)
	far, inner := p.Unisolate(m)
	e.far, e.expected = far, p.Keep(inner)
	p.MergeExpected(far, inner)
	if !ok {
		p.Pos = start
	}
	return v, ok
}

// CallMemo is the memoized part of callBody, for the rule r invoked by inv (the generated i<id>):
// the rule is called at the position again, or its memoization is no longer deferred.
func (p *TParser) CallMemo(r *TRule, inv func(*TParser) (any, bool)) (any, bool) {
	if p.TypedDocument != nil {
		return p.DocumentCall(r, 0, inv)
	}
	start := p.Pos
	env := p.memoEnv(r.Rule)
	if e := p.memoGet(r.Id, start, 0, env); e != nil && !p.memberStale(r.Member, start) && (e.growing || (!e.silent || p.Silent > 0) && (len(e.errs) == 0 || e.silent == (p.Silent > 0))) {
		p.Observation.memoHit()
		p.readResult(e.growing, e.depth)
		if !e.growing {
			p.MergeExpected(e.far, e.expected)
		}
		if !e.ok {
			return nil, false
		}
		p.Pos = e.end
		p.Recovered = p.copyErrors(p.Recovered, e.errs)
		return e.val, true
	}
	rec := len(p.Recovered)
	depth, read := p.growths, p.enterCall()
	m := p.Isolate(start)
	v, ok := inv(p)
	if !ok {
		p.Recovered = p.Recovered[:rec]
	}
	var e *tmemoEntry
	if p.readDepth <= depth {
		e = &tmemoEntry{} // a result that can change is not memoized (readResult)
	} else {
		e = p.memoSlab.one()
		*e = tmemoEntry{val: v, ok: ok, end: p.Pos, silent: p.Silent > 0,
			errs: p.copyErrors(nil, p.Recovered[rec:])}
		p.memoPut(r.Id, start, 0, env, e)
	}
	p.leaveCall(read)
	far, inner := p.Unisolate(m)
	e.far, e.expected = far, p.Keep(inner)
	p.MergeExpected(far, inner)
	if !ok {
		p.Pos = start
	}
	return v, ok
}

func (p *TParser) grow(r *TRule, min int, env []any) (any, bool) {
	start := p.Pos
	rec := len(p.Recovered)
	best := p.memoSlab.one()
	*best = tmemoEntry{end: start, growing: true}
	p.memoPut(r.Id, start, min, env, best)
	prevPos, read := p.seedPos, p.enterCall()
	p.growths++
	p.seedPos = start
	best.depth = int32(p.growths)
	for {
		p.Spend()
		v, ok := p.invoke(r, min)
		if !ok || (best.ok && p.Pos <= best.end) {
			break
		}
		best = p.memoSlab.one()
		*best = tmemoEntry{val: v, ok: true, end: p.Pos, growing: true, depth: int32(p.growths), errs: p.copyErrors(nil, p.Recovered[rec:])}
		p.memoPut(r.Id, start, min, env, best)
		p.Pos = start
		p.Recovered = p.Recovered[:rec]
	}
	best.growing = false
	best.silent = p.Silent > 0
	p.growths--
	p.seedPos = prevPos
	best.depth = finalDepth(p.readDepth, p.growths, best.silent || len(best.errs) > 0)
	p.leaveCall(read)
	p.readResult(false, best.depth) // as in Parser.grow
	p.Pos = best.end
	p.Recovered = p.copyErrors(p.Recovered[:rec], best.errs)
	return best.val, best.ok
}
