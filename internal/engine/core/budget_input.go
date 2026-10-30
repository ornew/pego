package core

// Spend charges one step; it has a statically removable instrumentation boundary.
func (p *Parser) Spend() {
	if BudgetControlsEnabled && p.Control != nil {
		p.Control.Charge(1)
	}
}

// NewControlledTextInput prepares the input of a parse that has a control. The preparation is the
// ordinary one and charges nothing: its cost is bounded by the input, which MaxInput bounds. It
// is not interruptible, like an allocation.
func NewControlledTextInput(s string, unit PositionUnit, offsets bool, sc *scratch, c *BudgetControl) Input {
	in := newTextInputIn(s, unit, offsets, sc)
	in.Control = c
	return in
}
