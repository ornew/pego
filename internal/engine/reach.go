package engine

import (
	"fmt"
	"sort"
)

// A rule of the Node table (rules), the recognizer table (recRules) or the typed table (trules) has
// a Go body and the functions that body calls. The generator prints them only for the rules that
// something can enter: the start rules, which the generated entry points and Starts reach (for the
// typed table, the start rules whose typed entry point calls the rule's Entry), and the rules a
// printed body calls. A rule that the compiler routes through its value-free twin (compile.go, the Ref case) has
// an original that only value contexts call, so a rule referenced only from value-free contexts has
// an original body that nothing calls, and so on transitively.
//
// generate finds the reachable rules with a first pass whose calls (ruleCall) are the edges, and a
// second pass leaves out the others. The rule tables still list every rule: only the Body, Action,
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
	table   string
	id      int
	wrapper bool
}

// rule returns the key of the rule that k enters part of.
func (k ruleKey) rule() ruleKey { k.wrapper = false; return k }

// rootKey is the origin of the calls that are not generated for the body of a rule: the stream
// body and the entry points.
var rootKey = ruleKey{"", -1, false}

// liveRules is what the first pass of generate records for the second.
type liveRules struct {
	log    []ruleEdge                   // the calls generated so far, which a rolled back attempt truncates
	edges  map[ruleKey]map[ruleKey]bool // the rules a rule's printed body calls, from log
	scopes map[ruleKey][]string         // the capture layout of each rule, which its table row lists
	descs  []string                     // the expectation table, whose IDs the printed bodies keep
	roots  []ruleKey                    // the start rules of the tables and the entries of the typed table
	reach  map[ruleKey]bool
}

// reachHook is called by tests with the result of the first pass of generate, before the second
// pass; a test removes a root from it to show that the output then fails.
var reachHook func(*liveRules)

// ruleEdge is a call of rule to from the body of rule from.
type ruleEdge struct{ from, to ruleKey }

// buildEdges sets l.edges to the calls of l.log.
func (l *liveRules) buildEdges() {
	l.edges = map[ruleKey]map[ruleKey]bool{}
	for _, e := range l.log {
		if l.edges[e.from] == nil {
			l.edges[e.from] = map[ruleKey]bool{}
		}
		l.edges[e.from][e.to] = true
	}
}

// reachFrom sets l.reach to the rules that the roots and the calls outside rule bodies reach
// through the recorded edges.
func (l *liveRules) reachFrom(roots []ruleKey) {
	l.reach = map[ruleKey]bool{}
	work := append([]ruleKey(nil), roots...)
	for to := range l.edges[rootKey] {
		work = append(work, to)
	}
	for len(work) > 0 {
		k := work[len(work)-1]
		work = work[:len(work)-1]
		if l.reach[k] {
			continue
		}
		l.reach[k] = true
		for to := range l.edges[k.rule()] {
			work = append(work, to)
		}
	}
}

// missingBodies returns the rules of the table that are reachable but have no printed Body.
func (l *liveRules) missingBodies(table string, printed map[ruleKey]bool) []ruleKey {
	var miss []ruleKey
	for k := range l.reach {
		if k.table == table && !k.wrapper && !printed[k] {
			miss = append(miss, k)
		}
	}
	sort.Slice(miss, func(i, j int) bool { return miss[i].id < miss[j].id })
	return miss
}

// pruned reports whether the second pass leaves out everything of rule r of the current table.
func (g *generator) pruned(r *rule) bool {
	if g.live == nil || g.keepTableRules && g.table != "trules" {
		return false
	}
	return !g.live.reach[ruleKey{g.table, r.id, false}] && !g.live.reach[ruleKey{g.table, r.id, true}]
}

// slotReached reports whether the Body of rule r is printed: something enters the rule through its
// slot, and not only through its wrapper.
func (g *generator) slotReached(r *rule) bool {
	return g.live == nil || g.table == "trules" || g.live.reach[ruleKey{g.table, r.id, false}]
}

// beginRule and endRule attribute the calls generated between them to the body of r.
func (g *generator) beginRule(r *rule) {
	g.emitting, g.emittingOK = ruleKey{g.table, r.id, false}, true
}

func (g *generator) endRule() { g.emittingOK = false }

// noteCall records a call of r from the body being generated, through its wrapper or its slot, and
// checks in the second pass that the callee was not left out.
func (g *generator) noteCall(r *rule, wrapper bool) {
	to := ruleKey{g.table, r.id, wrapper}
	if g.collect != nil {
		from := rootKey
		if g.emittingOK {
			from = g.emitting
		}
		g.collect.log = append(g.collect.log, ruleEdge{from, to})
	}
	if g.live != nil && !g.live.reach[to] && g.reachErr == nil && (g.table == "trules" || !g.keepTableRules) {
		g.reachErr = fmt.Errorf("internal error: a generated call reaches the rule %s of %s, whose body is left out", r.name, g.table)
	}
}

// printedBody records that the Body of rule r of the current table is printed.
func (g *generator) printedBody(r *rule) {
	if g.bodySet == nil {
		g.bodySet = map[ruleKey]bool{}
	}
	g.bodySet[ruleKey{g.table, r.id, false}] = true
}

// finishReach ends a pass: the first pass computes the reachable rules from the start rules and
// the calls outside rule bodies; the second checks that no printed call reaches a rule left out and
// that every reachable rule has its Body.
func (g *generator) finishReach(prog, rec *Program) error {
	if g.collect != nil {
		var roots []ruleKey
		for _, name := range prog.starts {
			roots = append(roots, ruleKey{"rules", prog.byName[name].id, false})
			if rec != nil {
				roots = append(roots, ruleKey{"recRules", rec.byName[name].id, false})
			}
		}
		roots = append(roots, g.typedRoots...)
		g.collect.roots = roots
		g.collect.buildEdges()
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
			return fmt.Errorf("internal error: the reachable rule slot %s[%d] has no Body", table, miss[0].id)
		}
	}
	return nil
}
