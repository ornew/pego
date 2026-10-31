package tsgen

import (
	"fmt"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/compile"
	"github.com/ornew/zeroparse/internal/ir/core"
	irbuild "github.com/ornew/zeroparse/internal/ir/core/build"
)

// A generated parser prints only the rules that a parse can enter: the start rules, which parseRule
// names and parse and recognize run, and the rules that the printed bodies call (core.Module.Reach).
// A rule left out has no constant, body, action, plain function or table row, and what only it would
// have declared (helpers, literals, expectations) is not declared either. A rule that returns a tree
// has a value-free twin that every call from a value-free context uses, so the original of a rule
// that only such contexts reference is left out, and so are the originals that only it calls.
//
// Rule IDs and the numbering of memoized rules (Seen) are those of the compiled program, so the
// remaining rules keep their values. The printer checks that no printed call reaches a rule left out.

// tsLive holds, for each table ("rules" or "recRules"), the ways in which the rules are entered.
type tsLive struct {
	reach map[string][]core.Entry
}

// reachableRules returns the rules of prog and of the recognizer rec (nil if there is none) that a
// parse can enter: every start rule of the Node table, which parseRule can name, and the rule start
// that parse and recognize run.
func reachableRules(prog, rec *compile.Program, start *compile.Rule) (*tsLive, error) {
	l := &tsLive{reach: map[string][]core.Entry{}}
	for table, p := range map[string]*compile.Program{"rules": prog, "recRules": rec} {
		if p == nil {
			continue
		}
		m, err := irbuild.CoreFrom(p, nil)
		if err != nil {
			return nil, err
		}
		var roots []core.Root
		if table == "rules" {
			for _, name := range p.StartRules {
				roots = append(roots, core.Root{Rule: p.ByName[name].ID, Entry: core.EnterSlot})
			}
		}
		roots = append(roots, core.Root{Rule: p.ByName[start.Name].ID, Entry: core.EnterSlot})
		rules := append(append([]*compile.Rule(nil), p.RuleList...), p.Twins...)
		l.reach[table] = m.Reach(roots, func(id int) core.Entry {
			if callsPlain(rules[id]) {
				return core.EnterWrapper
			}
			return core.EnterSlot
		})
	}
	return l, nil
}

// callsPlain reports whether a call of the rule r is printed as a call of its plain function
// (tsFlowPrinter.call) rather than through its constant.
func callsPlain(r *compile.Rule) bool {
	_, operatorsRule := r.Def.Expr.(*grammar.OperatorsRule)
	return r.Plain && !operatorsRule
}

// pruned reports whether the rule r of the current table is left out.
func (g *Generator) pruned(r *compile.Rule) bool {
	return g.live != nil && g.live.reach[g.Table][r.ID] == 0
}

// slotReached reports whether something enters the rule r through its constant (and so its body
// assignment), and not only through its plain function.
func (g *Generator) slotReached(r *compile.Rule) bool {
	return g.live == nil || g.live.reach[g.Table][r.ID]&core.EnterSlot != 0
}

// tableRow reports whether the constant of the rule r is declared: something reads it, which is
// the entry through its slot, or the plain function of a rule with a value (it finishes the value
// with the rule). A rule without a value that only its plain function enters needs no constant.
func (g *Generator) tableRow(r *compile.Rule) bool {
	return g.slotReached(r) || !r.Novalue
}

// checkCall checks that the callee of a printed call was not left out.
func (g *Generator) checkCall(r *compile.Rule) {
	if g.pruned(r) && g.reachErr == nil {
		g.reachErr = fmt.Errorf("internal error: a generated call reaches the rule %s of %s, whose body is left out", r.Name, g.Table)
	}
}
