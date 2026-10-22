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

// genDecisions holds the recorded decisions of a generator.
type genDecisions struct {
	log []genDecision
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
	x.table, x.rule, x.kind, x.e = g.table, g.cur, kind, e
	d.log = append(d.log, x)
}
