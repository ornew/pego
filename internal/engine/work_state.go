package engine

// undoCaptures restores only private capture slots, never Node pointees.
// Interruption aborts the entire invocation; callers truncate the trail only
// after restoration completes. Mandatory scratch cleanup does not call this.
func (p *parser) undoCaptures(from int) {
	if !workControlsEnabled || p.control == nil {
		for i := len(p.trail) - 1; i >= from; i-- {
			u := p.trail[i]
			u.f.vals[u.slot] = u.old
		}
		return
	}
	for i := len(p.trail) - 1; i >= from; i-- {
		p.stepWork()
		u := p.trail[i]
		u.f.vals[u.slot] = u.old
	}
}
