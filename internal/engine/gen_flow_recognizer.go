package engine

import (
	"fmt"
	"strings"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/ir/flow"
)

// Value-free rules of the Node runtime printed from Flow IR.
//
// The rules of a recognizer (recRules) and the lean rules of the Node table (rules) that
// recFlowEligible accepts are built as Core IR, lowered by flow.Lower and printed by flowPrinter for
// the untyped Node runtime. The other rules print their framed body from Flow IR as well
// (flowNodeFrameBody), in the same output. Both tables share the helpers whose printed text is equal. The text passes
// run on the whole output afterwards.

// refuseFlowRecRule, if set by a test, makes flowRecRule refuse the rules it selects, so that the
// framed body that takes their place is exercised. No manifest grammar reaches that fallback.
var refuseFlowRecRule func(*compile.Rule) bool

// flowRecRule prints the eligible rule r of the recognizer from Flow IR as a flat rule. It returns
// nil in three cases, and the caller (generator.rules) then prints the rule's framed body from Flow IR
// (flowNodeFrameBody) and calls it through the rule table (plainCall); the generation fails if that
// does not print it either:
//   - flow.LocalRule rejects the rule, which needs the runtime-owned frame of a framed function;
//   - the printed function would read its start or its context, which a value-free flat rule has
//     no parameters for;
//   - a variable of the function has a type that untypedGoType cannot name.
func (g *generator) flowRecRule(r *compile.Rule) *flatRule {
	if refuseFlowRecRule != nil && refuseFlowRecRule(r) {
		return nil
	}
	cr := g.flow.m.Rules[r.ID]
	if !flow.LocalRule(cr) {
		return nil
	}
	f, err := flow.Lower(g.flow.m, cr, r.Name, g.flow.opts)
	if err != nil {
		panic(fmt.Errorf("rule %s of table %s: %w", r.Name, g.table, err)) // LocalRule holds
	}
	g.cur, g.proj = r, g.projections(r)
	defer func() { g.proj = nil }()
	p := newFlowPrinter(g, false)
	p.document = f.Document
	p.stmts(f.Body)
	if p.reads[f.Start] || p.reads[f.Ctx] {
		return nil
	}
	out := &flatRule{fail: p.used[f.Fail], succeed: !p.dead}
	res := p.vars.resolve(f.Vars, p.b.String())
	g.flowVarsErr(res)
	var unread []string
	for _, v := range f.Vars {
		t, ok := untypedGoType(v.Type)
		if !ok {
			return nil
		}
		out.vars = append(out.vars, dvar{v.Name, t})
		switch {
		case res.dead(v):
			out.markVar(v.Name, true)
		case !p.reads[v]:
			unread = append(unread, v.Name)
		case !res.read(v):
			out.markVar(v.Name, false)
		}
	}
	out.body = res.body
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
