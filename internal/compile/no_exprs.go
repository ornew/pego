package compile

import "github.com/ornew/pego/grammar"

// NoExprs is an expression builder that builds nothing: the compiler still walks every rule body
// and decides its static facts. Its methods implement ExprBuilder, which documents them,
// and return the zero form.
type NoExprs struct{}

var _ ExprBuilder[struct{}] = NoExprs{}

func (NoExprs) Literal(e *grammar.Literal, desc ExpID, build bool) struct{} {
	return struct{}{}
}
func (NoExprs) Class(e *grammar.CharClass, desc ExpID, build bool) struct{} {
	return struct{}{}
}
func (NoExprs) Any(build bool) struct{}        { return struct{}{} }
func (NoExprs) Call(r *Rule, min int) struct{} { return struct{}{} }
func (NoExprs) Seq(items []struct{}, flat, valued []bool, n int, build bool) struct{} {
	return struct{}{}
}
func (NoExprs) Choice(e *grammar.Choice, alts []struct{}, firsts []First, build bool) struct{} {
	return struct{}{}
}
func (NoExprs) Optional(e *grammar.Optional, m struct{}, build bool) struct{} { return struct{}{} }
func (NoExprs) And(m struct{}) struct{}                                       { return struct{}{} }
func (NoExprs) Not(m struct{}) struct{}                                       { return struct{}{} }
func (NoExprs) Atomic(m struct{}, build bool) struct{}                        { return struct{}{} }
func (NoExprs) Discard(m struct{}) struct{}                                   { return struct{}{} }
func (NoExprs) Capture(e *grammar.Capture, slot int, m struct{}, projected, build bool) struct{} {
	return struct{}{}
}
func (NoExprs) Cut() struct{}                                     { return struct{}{} }
func (NoExprs) Empty() struct{}                                   { return struct{}{} }
func (NoExprs) Fail() struct{}                                    { return struct{}{} }
func (NoExprs) Anchor(e grammar.Expr, desc ExpID) struct{}        { return struct{}{} }
func (NoExprs) Predicate(e *grammar.Predicate, s *Scope) struct{} { return struct{}{} }
func (NoExprs) ScanRepeat(e *grammar.Repeat, desc ExpID) struct{} { return struct{}{} }
func (NoExprs) Repeat(e *grammar.Repeat, elem struct{}, elemScope *Scope, ownScope, build, stream bool, site int, shiftable bool, in *Rule) struct{} {
	return struct{}{}
}
func (NoExprs) ProjectRepeat(e *grammar.Repeat, elem struct{}, slot, n int) struct{} {
	return struct{}{}
}
func (NoExprs) Missing(m struct{}, site int, expected string, build bool) struct{} { return struct{}{} }
func (NoExprs) ErrorMessage(m struct{}, msg ExpID, missingScope bool) struct{} {
	return struct{}{}
}
func (NoExprs) Diagnostic(m struct{}, name ExpID) struct{}                    { return struct{}{} }
func (NoExprs) Recover(e grammar.Expr, m, skip struct{}, build bool) struct{} { return struct{}{} }
func (NoExprs) RuleBody(r *Rule, body struct{})                               {}
func (NoExprs) OperatorsRule(r *Rule, skip struct{}, hasSkip bool, operands, operators []OpLine[struct{}]) {
}
