package gogen

import (
	"fmt"
	"sort"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/compile"
	"github.com/ornew/zeroparse/internal/ir/core"
	irbuild "github.com/ornew/zeroparse/internal/ir/core/build"
)

// A rule of the Node table (rules), the recognizer table (recRules) or the typed table (trules) has
// a Go body and the functions that body calls. The generator prints them only for the rules that
// something can enter: the start rules, which the generated entry points and Starts reach (for the
// typed table, the start rules whose typed entry point calls the rule's Entry), and the rules a
// printed body calls. A rule that the compiler routes through its value-free twin
// (internal/compile/compile_walk.go, the Ref case) has an original that only value contexts call,
// so a rule referenced only from value-free contexts has an original body that nothing calls, and
// so on transitively.
//
// GenerateParser finds the reachable rules with a first pass, which takes the calls of the rules from the
// Core IR (core.Module.Reach) and how a call of each rule is printed (callWrapper), and a second pass
// leaves out the others. The rule tables still list every rule: only the Body, Action,
// Entry and OperatorsRule assignments and the functions of an unreachable rule are left out. What a
// left-out rule would have declared (its expectations, literals, class tables and typed helpers) is
// not declared either, so the IDs of what remains are those of the second pass.
//
// The expectation table that the Node and recognizer tables fix (descs) is the one of the first
// pass up to the end of those tables, because a left-out rule of them keeps its table row. The
// typed table has no such rows, so the expectations of its left-out rules are not in any table.

// ruleKey names what of a rule is entered: its slot in the rule table (the Body that p.Call and
// InvokePlain run), or the wrapper function of a plain rule, which a call of it (ruleCall) uses
// instead and which does not run the Body.
type ruleKey struct {
	Table   string
	ID      int
	Wrapper bool
}

// liveRules is what the first pass of GenerateParser records for the second.
type liveRules struct {
	scopes map[ruleKey][]string // the capture layout of each rule, which its table row lists
	descs  []string             // the expectation table, whose IDs the printed bodies keep
	Roots  []ruleKey            // the start rules of the tables and the entries of the typed table
	Reach  map[ruleKey]bool
	mods   map[string]*core.Module         // the Core IR of each table, whose calls reachFrom follows
	enter  func(table string, id int) bool // whether a call of a rule is printed as a call of its wrapper
}

// reachFrom sets l.Reach to the rules that the roots enter, with the reachability of the Core IR
// (core.Module.Reach) of each table, in the way enter prints a call of a rule.
func (l *liveRules) reachFrom(roots []ruleKey) {
	l.Reach = map[ruleKey]bool{}
	for table, m := range l.mods {
		var rs []core.Root
		for _, k := range roots {
			if k.Table == table {
				e := core.EnterSlot
				if k.Wrapper {
					e = core.EnterWrapper
				}
				rs = append(rs, core.Root{Rule: k.ID, Entry: e})
			}
		}
		reach := m.Reach(rs, func(id int) core.Entry {
			if l.enter(table, id) {
				return core.EnterWrapper
			}
			return core.EnterSlot
		})
		for id, e := range reach {
			if e&core.EnterSlot != 0 {
				l.Reach[ruleKey{table, id, false}] = true
			}
			if e&core.EnterWrapper != 0 {
				l.Reach[ruleKey{table, id, true}] = true
			}
		}
	}
}

// missingBodies returns the rules of the table that are reachable but have no printed Body.
func (l *liveRules) missingBodies(table string, printed map[ruleKey]bool) []ruleKey {
	var miss []ruleKey
	for k := range l.Reach {
		if k.Table == table && !k.Wrapper && !printed[k] {
			miss = append(miss, k)
		}
	}
	sort.Slice(miss, func(i, j int) bool { return miss[i].ID < miss[j].ID })
	return miss
}

// pruned reports whether the second pass leaves out everything of rule r of the current table.
func (g *Generator) pruned(r *compile.Rule) bool {
	if g.live == nil || g.keepTableRules && g.Table != "trules" {
		return false
	}
	return !g.live.Reach[ruleKey{g.Table, r.ID, false}] && !g.live.Reach[ruleKey{g.Table, r.ID, true}]
}

// slotReached reports whether the Body of rule r is printed: something enters the rule through its
// slot, and not only through its wrapper.
func (g *Generator) slotReached(r *compile.Rule) bool {
	return g.live == nil || g.Table == "trules" || g.live.Reach[ruleKey{g.Table, r.ID, false}]
}

// callWrapper reports whether a call of the rule r of the table is printed as a call of its wrapper
// function, which does not run the Body of its table entry, rather than through the entry (ruleCall).
func (g *Generator) callWrapper(table string, r *compile.Rule) bool {
	_, operatorsRule := r.Def.Expr.(*grammar.OperatorsRule)
	switch {
	case g.Document && table == "rules":
		return false
	case g.directTypedDocument && table == "trules" && operatorsRule:
		return false
	case r.Plain && !operatorsRule:
		return true
	case table == "trules" && !operatorsRule && !r.Leader:
		return true
	}
	return false
}

// checkCall checks in the second pass that the callee of a printed call, through its wrapper or its
// slot, was not left out.
func (g *Generator) checkCall(r *compile.Rule, wrapper bool) {
	to := ruleKey{g.Table, r.ID, wrapper}
	if g.live != nil && !g.live.Reach[to] && g.reachErr == nil && (g.Table == "trules" || !g.keepTableRules) {
		g.reachErr = fmt.Errorf("internal error: a generated call reaches the rule %s of %s, whose body is left out", r.Name, g.Table)
	}
}

// printedBody records that the Body of rule r of the current table is printed.
func (g *Generator) printedBody(r *compile.Rule) {
	if g.bodySet == nil {
		g.bodySet = map[ruleKey]bool{}
	}
	g.bodySet[ruleKey{g.Table, r.ID, false}] = true
}

// finishReach ends a pass: the first pass computes the reachable rules from the start rules and
// the calls outside rule bodies; the second checks that no printed call reaches a rule left out and
// that every reachable rule has its Body.
func (g *Generator) finishReach(prog, rec *compile.Program) error {
	if g.collect != nil {
		var roots []ruleKey
		for _, name := range prog.StartRules {
			roots = append(roots, ruleKey{"rules", prog.ByName[name].ID, false})
			if rec != nil {
				roots = append(roots, ruleKey{"recRules", rec.ByName[name].ID, false})
			}
		}
		roots = append(roots, g.typedRoots...)
		g.collect.Roots = roots
		mods := map[string]*core.Module{}
		progs := map[string]*compile.Program{"rules": prog, "trules": prog}
		if rec != nil {
			progs["recRules"] = rec
		}
		byID := map[string][]*compile.Rule{}
		for table, pr := range progs {
			if table == "trules" && len(g.typedRoots) == 0 {
				continue
			}
			m, err := irbuild.CoreFrom(pr, nil)
			if err != nil {
				return err
			}
			mods[table] = m
			byID[table] = append(append([]*compile.Rule(nil), pr.RuleList...), pr.Twins...)
		}
		g.collect.mods = mods
		g.collect.enter = func(table string, id int) bool { return g.callWrapper(table, byID[table][id]) }
		g.collect.reachFrom(roots)
	}
	if g.keepTableRules && g.convertsNodes {
		return fmt.Errorf("internal error: a typed entry point converts Nodes, but the parser prints no Node rule bodies")
	}
	if g.live == nil {
		return nil
	}
	if g.reachErr != nil {
		return g.reachErr
	}
	for _, table := range []string{"rules", "recRules"} {
		if g.keepTableRules {
			break // the Node and recognizer rules are all kept
		}
		if miss := g.live.missingBodies(table, g.bodySet); len(miss) > 0 {
			return fmt.Errorf("internal error: the reachable rule slot %s[%d] has no Body", table, miss[0].ID)
		}
	}
	return nil
}
