package engine

import "github.com/ornew/pego/grammar"

// directTypedOperatorsRuleBody inlines an unfinished operand, operator or skip matcher.
// OperatorsRule's runtime still owns precedence, longest matching, frames and actions.
// Each typed line declares its own frame layout, unlike an LR leader that
// shares the ordinary rule's metadata.
func (g *generator) directTypedOperatorsRuleBody(e grammar.Expr, scope *scope, build bool, action grammar.Term, operator bool) string {
	if !g.typedOperatorsRuleBodyOK(e) {
		return ""
	}
	snap := g.snapshot()
	prevProj := g.proj
	g.proj = nil // OperatorsRule line actions do not use rule-level projections.
	defer func() { g.proj = prevProj }()
	d := &dgen{g: g, nodeBody: true, reads: map[string]bool{}, used: map[string]bool{}}
	s := d.frameBodyScope(e, build)
	d.frameCuts(e)
	v := d.expr(e, s, build, "fail")
	var locals []string
	if operator {
		locals = []string{"lhs", "rhs", "op"}
	}
	if action != nil && !frameRefsKnown(action, d.frameScope(s), locals...) {
		d.bad = true
	}
	if d.bad {
		g.rollback(snap)
		return ""
	}
	// Preserve the expression value until the runtime evaluates its action.
	// No finish operation or rule wrapper belongs in this matcher.
	return g.finishFrameBody(d, s, scope, v, g.cur.name+" (typed OperatorsRule body inlined)")
}

// typedOperatorsRuleBodyOK reports whether the line of an Operators Rule with the expression e is
// inlined as a matcher of the typed rules.
func (g *generator) typedOperatorsRuleBodyOK(e grammar.Expr) bool {
	return g.table == "trules" && !g.disableTypedOperatorsRuleBodies &&
		directFrameExprOK(e, !g.disableTypedFramedCuts, !g.disableTypedRecoveryBodies)
}
