package core

// UndoCaptures restores only private capture slots, never Node pointees.
// Interruption aborts the entire invocation; callers truncate the trail only
// after restoration completes. Mandatory scratch cleanup does not call this.
func (p *Parser) UndoCaptures(from int) {
	if !BudgetControlsEnabled || p.Control == nil {
		for i := len(p.Trail) - 1; i >= from; i-- {
			u := p.Trail[i]
			u.F.Vals[u.Slot] = u.Old
		}
		return
	}
	for i := len(p.Trail) - 1; i >= from; i-- {
		p.Spend()
		u := p.Trail[i]
		u.F.Vals[u.Slot] = u.Old
	}
}
