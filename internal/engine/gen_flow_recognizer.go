package engine

import (
	"fmt"
	"strings"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/engine/flow"
)

// Value-free rules of the Node runtime printed from Flow IR.
//
// The rules of a recognizer (recRules) and the lean rules of the Node table (rules) that
// recFlowEligible accepts are built as Core IR, lowered by flow.Lower and printed by flowPrinter for
// the untyped Node runtime, in place of the direct rules of dgen. The other rules keep their printer,
// in the same output. Both tables share the helpers whose printed text is equal. The ordinary passes
// run on the whole output afterwards, so a printed rule gets its ordinary branch as a generated one does.

// recFlowEligible reports whether the rule r of the Node table or of the recognizer table is printed
// from Flow IR: a rule that the direct printer prints as a lean value-free rule (directLeanOK) and
// whose body has no capture or predicate.
func recFlowEligible(r *rule) bool {
	if !directLeanOK(r) {
		return false
	}
	ok := true
	walkExpr(r.bodyExpr(), func(e grammar.Expr) {
		switch e.(type) {
		case *grammar.Capture, *grammar.Predicate:
			ok = false
		}
	})
	return ok
}

// startRecFlow builds the Core IR of the program rec (the recognizer, or the program of the Node table),
// with the passes of the untyped tables (no terminal programs and no class runs), for printing its
// eligible rules from Flow IR. It runs the passes of typedCore that the Node runtime's rules use:
// rewrites, Analyze, Guards, scans and the repeats, which followRepeats keeps in step with the scans.
// It omits Terminals and ClassRuns, which typedCore runs only for lean typed rules (leanTyped): the Node recognizer writes
// its terminals and classes as the direct printer does, as inline tests.
func (g *generator) startRecFlow(rec *Program) error {
	origin := map[core.Node]grammar.Expr{}
	m, err := buildCoreFrom(rec, origin)
	if err != nil {
		return err
	}
	core.RewriteTerms(m, core.TermOptions{TextDispatch: !g.disableLiteralTextDispatch, FieldCompare: !g.boxedComparisons})
	// Guards read the First sets that Analyze computes, as core_build does: without this pass the
	// alternatives of a choice get no first-character guard, and the Flow form calls the rules of
	// the later alternatives at every position (duckdb: block_comment, 84k calls against none).
	core.Analyze(m)
	core.Guards(m, g.lean && !g.disableGuardedClasses)
	repeats := followRepeats(m, origin)
	core.Scans(m)
	for _, f := range repeats {
		f()
	}
	core.Analyze(m)
	if err := core.Verify(m); err != nil {
		return fmt.Errorf("internal error: %w", err)
	}
	// Classes are inline tests, as the Node runtime's generated code writes them.
	opts := flow.Options{Work: !g.noWork, Recovers: flow.Recovers(m), GuardedClasses: g.lean && !g.disableGuardedClasses,
		InlineLiterals: maxInlineLiteral, Observe: g.observability, Node: true, Document: g.document}
	g.flow = &flowState{m: m, origin: origin, opts: opts, rules: append(append([]*rule(nil), rec.rules...), rec.twins...)}
	return nil
}

// flowRecDirect prints the eligible rule r of the recognizer from Flow IR as a direct rule, or
// returns nil if the printed function would read its start or context, which a value-free body
// does not need.
// refuseFlowRecDirect, if set by a test, makes flowRecDirect refuse the rules it selects, so that the
// fallback of the Flow route to the direct printer is exercised. No manifest grammar reaches the fallback.
var refuseFlowRecDirect func(*rule) bool

func (g *generator) flowRecDirect(r *rule) *direct {
	if refuseFlowRecDirect != nil && refuseFlowRecDirect(r) {
		return nil
	}
	cr := g.flow.m.Rules[r.id]
	if !flow.LocalRule(cr) {
		return nil
	}
	f, err := flow.Lower(g.flow.m, cr, r.name, g.flow.opts)
	if err != nil {
		panic(fmt.Errorf("rule %s of table %s: %w", r.name, g.table, err)) // LocalRule holds
	}
	g.cur, g.proj = r, g.projections(r)
	defer func() { g.proj = nil }()
	p := newFlowPrinter(g, false)
	p.document = f.Document
	p.stmts(f.Body)
	if p.reads[f.Start] || p.reads[f.Ctx] {
		return nil
	}
	out := &direct{fail: p.used[f.Fail], succeed: !p.dead}
	var unread []string
	for _, v := range f.Vars {
		t, ok := untypedGoType(v.Type)
		if !ok {
			return nil
		}
		out.vars = append(out.vars, dvar{v.Name, t})
		if !p.reads[v] {
			unread = append(unread, v.Name)
		}
	}
	out.body = p.b.String()
	if len(unread) > 0 {
		out.body = "\t" + strings.Repeat("_, ", len(unread)-1) + "_ = " + strings.Join(unread, ", ") + "\n" + out.body
	}
	g.flowRules++
	return out
}

// untypedGoType returns the Go type of a variable of a Flow IR function printed for the Node
// runtime, or false if the printer has no such type for it.
func untypedGoType(t flow.Type) (string, bool) {
	switch t {
	case flow.TypeInt, flow.TypeBool, flow.TypeRune, flow.TypeMark, flow.TypeExpMark, flow.TypeExpIDs:
		return goType(t), true
	}
	return "", false
}

// followRepeats returns functions that, once the scan pass has run, give the scan that took the
// place of each repetition with an origin the origin of the repetition.
func followRepeats(m *core.Module, origin map[core.Node]grammar.Expr) []func() {
	var out []func()
	visit := func(at func() core.Node) {
		if rep, ok := at().(*core.Repeat); ok {
			if e, ok := origin[rep]; ok {
				out = append(out, func() {
					if s, ok := at().(*core.Scan); ok {
						origin[s] = e
					}
				})
			}
		}
	}
	for _, r := range m.Rules {
		eachSlot(r, visit)
	}
	return out
}

// eachSlot calls f with an accessor of every place of the rule's body where a node is: the body,
// the bodies of Operators Rule lines and the children of nodes.
func eachSlot(r *core.Rule, f func(at func() core.Node)) {
	var walk func(at func() core.Node)
	walk = func(at func() core.Node) {
		f(at)
		n := at()
		if o, ok := n.(*core.OperatorsRule); ok {
			for _, l := range o.Lines() {
				walk(func() core.Node { return l.Body })
			}
			return
		}
		for i := range core.Children(n) {
			walk(func() core.Node { return core.Children(n)[i] })
		}
	}
	walk(func() core.Node { return r.Body })
}
