package engine

import (
	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/engine/core"
)

// Lowering decisions of the generator, recorded for the tests that compare the Core IR passes
// with the code the generator writes (core_gen_test.go). A generator records them only when
// its decisions field is set; the records of an attempt that is rolled back (generator.rollback)
// are dropped with it.

// genDecisionKind is the kind of a recorded decision.
type genDecisionKind uint8

const (
	// decTerminal: the expression is run as a terminal program (generator.terminalTable).
	decTerminal genDecisionKind = iota
	// decGuard: the alternative of a choice has the guard (generator.first) or none.
	decGuard
	// decClassRuns: the value-free sequence matches the runs of classes together (classRun).
	decClassRuns
	// decScan: the repetition is a scan (dgen.scan, generator.scanRepeat).
	decScan
	// decScope: the scope with the body expression (a repetition element with captures of its
	// own, an Operators Rule line) has the slots names.
	decScope
	// decExpr: the expression is written (dgen.expr, generator.expr).
	decExpr
)

// genDecision is a decision the generator made for an expression of a rule.
type genDecision struct {
	table string
	rule  *rule
	kind  genDecisionKind
	e     grammar.Expr
	guard genGuard
	// runs holds the class runs of a sequence: the index of the first item and the length.
	runs  [][2]int
	names []string
}

// genGuard is the guard of an alternative: the condition on the next character, the expectation
// and the depth, and whether the alternative's first class consumes the character the guard read.
type genGuard struct {
	ok    bool
	cond  string
	desc  expID
	depth int
	class bool
}

// genTerm is a term as the generator writes it: its form and the terms written inside it, in the
// order they are written.
type genTerm struct {
	form string
	kids []*genTerm
}

// genTermRecord is a term the generator wrote at the top: an action, a predicate or the value of
// a binding.
type genTermRecord struct {
	table string
	rule  *rule
	t     grammar.Term
	tree  *genTerm
}

// genDecisions holds the recorded decisions of a generator.
type genDecisions struct {
	log   []genDecision
	terms []genTermRecord
	// stack holds the terms being written; mute counts the writes not to record.
	stack []*genTerm
	mute  int
	// rule, if not nil, is the rule the decisions are recorded for, where the generator writes
	// a rule's parts outside the rule (generator.decidingFor).
	rule *rule
	// gen is the generator, for the Core IR of the same options (generator.typedCore).
	gen *generator
	// coreHook, if set, is called by typedCore with the module after it is built and after each
	// pass.
	coreHook func(pass string, m *core.Module)
}

// decide records a decision of the rule being generated.
func (g *generator) decide(kind genDecisionKind, e grammar.Expr, x genDecision) {
	d := g.decisions
	if d == nil {
		return
	}
	x.table, x.rule, x.kind, x.e = g.table, d.cur(g), kind, e
	d.log = append(d.log, x)
}

// enterTerm records the writing of the term t of form form ("" for a term that is written as
// its only inner term) and returns the function that ends it.
func (g *generator) enterTerm(t grammar.Term, form string) func() {
	d := g.decisions
	if d == nil || d.mute > 0 {
		return func() {}
	}
	n := &genTerm{form: form}
	d.stack = append(d.stack, n)
	return func() {
		d.stack = d.stack[:len(d.stack)-1]
		kids := n.kids
		if n.form != "" {
			kids = []*genTerm{n}
		}
		if len(d.stack) > 0 {
			top := d.stack[len(d.stack)-1]
			top.kids = append(top.kids, kids...)
			return
		}
		if len(kids) == 1 {
			d.terms = append(d.terms, genTermRecord{table: g.table, rule: d.cur(g), t: t, tree: kids[0]})
		}
	}
}

// cur returns the rule whose decisions are being recorded.
func (d *genDecisions) cur(g *generator) *rule {
	if d.rule != nil {
		return d.rule
	}
	return g.cur
}

// decidingFor records the decisions for the rule r until the returned function is called.
func (g *generator) decidingFor(r *rule) func() {
	d := g.decisions
	if d == nil {
		return func() {}
	}
	d.rule = r
	return func() { d.rule = nil }
}

// termForm sets the form of the term being written.
func (g *generator) termForm(form string) {
	if d := g.decisions; d != nil && d.mute == 0 && len(d.stack) > 0 {
		d.stack[len(d.stack)-1].form = form
	}
}

// discardTermKids drops the terms recorded inside the term being written: terms written but left
// out of the code.
func (g *generator) discardTermKids() {
	if d := g.decisions; d != nil && d.mute == 0 && len(d.stack) > 0 {
		d.stack[len(d.stack)-1].kids = nil
	}
}

// muteTerms stops recording terms until the returned function is called.
func (g *generator) muteTerms() func() {
	d := g.decisions
	if d == nil {
		return func() {}
	}
	d.mute++
	return func() { d.mute-- }
}

// termKind returns the form of a term written as itself.
func termKind(t grammar.Term) string {
	switch t := t.(type) {
	case *grammar.IntLit, *grammar.StringLit, *grammar.BoolLit, *grammar.NilLit:
		return "const"
	case *grammar.CaptureRef:
		return "capture"
	case *grammar.VarRef:
		return "var"
	case *grammar.Member:
		return "member"
	case *grammar.New:
		if len(t.Args) != 0 {
			return "convert"
		}
		return "new"
	case *grammar.ListConstructor:
		return "" // written as a list call
	case *grammar.TupleLit:
		return "tuple"
	case *grammar.Call:
		return "call:" + t.Func
	case *grammar.Lambda:
		return "lambda"
	case *grammar.Binary:
		return "binary:" + t.Op
	case *grammar.Unary:
		return "unary:" + t.Op
	case *grammar.Cast:
		return "cast"
	}
	return "?"
}
