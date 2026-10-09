package engine

import (
	"slices"
	"sort"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/grammaranalysis"
)

// analysis is the result of static analysis across rules.
type analysis struct {
	nullable   map[string]bool
	succeeding map[string]bool
	// leaders are the rules that break left-recursion cycles. These rules are evaluated with
	// grow-the-seed.
	leaders map[string]bool
	// cyclic are the rules that belong to a left-recursion cycle (including leaders).
	cyclic map[string]bool
	// positional are the rules whose actions or predicates read positions (startPos, endPos),
	// directly or through callees. Their results may contain position values, so incremental
	// parsing cannot shift them past an edit.
	positional map[string]bool
}

func analyze(rules []*grammar.RuleDef) *analysis {
	a := &analysis{
		nullable:   map[string]bool{},
		leaders:    map[string]bool{},
		cyclic:     map[string]bool{},
		positional: map[string]bool{},
	}
	byName := map[string]*grammar.RuleDef{}
	for _, r := range rules {
		byName[r.Name] = r
	}

	// Fixed-point computation of nullability.
	for changed := true; changed; {
		changed = false
		for _, r := range rules {
			if !a.nullable[r.Name] && a.isNullable(r.Expr) {
				a.nullable[r.Name] = true
				changed = true
			}
		}
	}

	// Propagate variable reads.
	calls := map[string][]string{}
	for _, r := range rules {
		calls[r.Name] = allCalls(r.Expr, nil)
	}
	for _, r := range rules {
		if ruleUsesPositions(r) {
			a.positional[r.Name] = true
		}
	}
	propagate(rules, calls, a.positional)

	// Find left-recursion cycles from the strongly connected components of the left-call graph.
	order := map[string]int{}
	left := map[string][]string{}
	for i, r := range rules {
		order[r.Name] = i
		for _, c := range a.leftCalls(r.Expr, nil) {
			if _, ok := byName[c]; ok {
				left[r.Name] = append(left[r.Name], c)
			}
		}
	}
	// Removing unreachable calls can break a cycle and make another rule's
	// success provable. Repeat until the graph stabilizes; each pass only
	// removes edges, so there are finitely many refinements.
	var components [][]string
	for {
		components = tarjan(rules, left)
		clear(a.cyclic)
		for _, scc := range components {
			if len(scc) > 1 || contains(left[scc[0]], scc[0]) {
				for _, n := range scc {
					a.cyclic[n] = true
				}
			}
		}
		a.succeeding = grammaranalysis.SucceedingRules(rules, a.cyclic)
		next := map[string][]string{}
		changed := false
		for _, r := range rules {
			for _, n := range a.leftCalls(r.Expr, nil) {
				if byName[n] != nil {
					next[r.Name] = append(next[r.Name], n)
				}
			}
			changed = changed || !slices.Equal(left[r.Name], next[r.Name])
		}
		if !changed {
			break
		}
		left = next
	}
	for _, scc := range components {
		if len(scc) == 1 && !contains(left[scc[0]], scc[0]) {
			continue
		}
		in := map[string]bool{}
		for _, n := range scc {
			in[n] = true
			a.cyclic[n] = true
		}
		// Until every cycle passes through some leader, make the first rule of a found cycle a leader.
		for {
			cycle := findCycle(scc, left, in, a.leaders)
			if cycle == nil {
				break
			}
			best := cycle[0]
			for _, n := range cycle {
				if order[n] < order[best] {
					best = n
				}
			}
			a.leaders[best] = true
		}
	}
	return a
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func (a *analysis) isNullable(e grammar.Expr) bool {
	switch e := e.(type) {
	case *grammar.Ref:
		return a.nullable[e.Name]
	case *grammar.Literal:
		return e.Value == ""
	case *grammar.CharClass, *grammar.Any, *grammar.Bottom:
		return false
	case *grammar.Seq:
		for _, it := range e.Items {
			if !a.isNullable(it) {
				return false
			}
		}
		return true
	case *grammar.Choice:
		for _, alt := range e.Alts {
			if a.isNullable(alt) {
				return true
			}
		}
		return false
	case *grammar.Repeat:
		return e.Min == 0 || a.isNullable(e.Expr)
	case *grammar.Atomic:
		return a.isNullable(e.Expr)
	case *grammar.Discard:
		return a.isNullable(e.Expr)
	case *grammar.Capture:
		return a.isNullable(e.Expr)
	case *grammar.Attributed:
		return a.isNullable(e.Expr)
	case *grammar.Pratt:
		for _, o := range e.Operands {
			if a.isNullable(o.Expr) {
				return true
			}
		}
		return false
	default:
		// Optional, lookahead, predicate, anchor, cut, top
		return true
	}
}

// leftCalls returns the rules that may be called before any input is consumed.
func (a *analysis) leftCalls(e grammar.Expr, acc []string) []string {
	switch e := e.(type) {
	case *grammar.Ref:
		return append(acc, e.Name)
	case *grammar.Seq:
		for _, it := range e.Items {
			acc = a.leftCalls(it, acc)
			if !a.isNullable(it) {
				break
			}
		}
	case *grammar.Choice:
		for _, alt := range e.Alts {
			acc = a.leftCalls(alt, acc)
			if grammaranalysis.AlwaysSucceeds(alt, a.succeeding) {
				break
			}
		}
	case *grammar.Repeat:
		if e.Max != 0 {
			acc = a.leftCalls(e.Expr, acc)
		}
	case *grammar.Optional:
		acc = a.leftCalls(e.Expr, acc)
	case *grammar.And:
		acc = a.leftCalls(e.Expr, acc)
	case *grammar.Not:
		acc = a.leftCalls(e.Expr, acc)
	case *grammar.Atomic:
		acc = a.leftCalls(e.Expr, acc)
	case *grammar.Discard:
		acc = a.leftCalls(e.Expr, acc)
	case *grammar.Capture:
		acc = a.leftCalls(e.Expr, acc)
	case *grammar.Attributed:
		acc = a.leftCalls(e.Expr, acc)
		if !grammaranalysis.AlwaysSucceeds(e.Expr, a.succeeding) {
			for _, at := range e.Attrs {
				if at.Name == "recover" {
					if skip, ok := at.Arg("skip"); ok {
						acc = a.leftCalls(skip, acc)
					}
				}
			}
		}
	case *grammar.Pratt:
		if e.Skip != nil {
			acc = a.leftCalls(e.Skip, acc)
		}
		for _, o := range e.Operands {
			acc = a.leftCalls(o.Expr, acc)
			if grammaranalysis.AlwaysSucceeds(o.Expr, a.succeeding) {
				break
			}
		}
		for _, l := range e.Levels {
			for _, op := range l.Operators {
				if op.Kind == grammar.Prefix {
					acc = a.leftCalls(op.Expr, acc)
				}
			}
		}
	}
	return acc
}

// allCalls returns every rule called in the expression.
func allCalls(e grammar.Expr, acc []string) []string {
	walkExpr(e, func(x grammar.Expr) {
		if r, ok := x.(*grammar.Ref); ok {
			acc = append(acc, r.Name)
		}
	})
	return acc
}

// ruleVariables returns, for each rule, the sorted names of the variables read by
// predicates and actions in the rule or in the rules it calls, directly or
// indirectly. A rule's result depends on the environment
// only through the values of these variables at the call: definitions made inside a rule are
// undone when it returns.
func ruleVariables(rules []*grammar.RuleDef) map[string][]string {
	direct := map[string]map[string]bool{}
	calls := map[string][]string{}
	for _, r := range rules {
		set := map[string]bool{}
		walkRuleTerms(r, func(t grammar.Term) {
			if v, ok := t.(*grammar.VarRef); ok {
				set[v.Name] = true
			}
		})
		direct[r.Name] = set
		calls[r.Name] = allCalls(r.Expr, nil)
	}
	out := map[string][]string{}
	for _, r := range rules {
		seen := map[string]bool{r.Name: true}
		all := map[string]bool{}
		stack := []string{r.Name}
		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for v := range direct[n] {
				all[v] = true
			}
			for _, c := range calls[n] {
				if !seen[c] {
					seen[c] = true
					stack = append(stack, c)
				}
			}
		}
		if len(all) > 0 {
			names := make([]string, 0, len(all))
			for v := range all {
				names = append(names, v)
			}
			sort.Strings(names)
			out[r.Name] = names
		}
	}
	return out
}

func termReadsVars(t grammar.Term) bool {
	found := false
	walkTerm(t, func(x grammar.Term) {
		if _, ok := x.(*grammar.VarRef); ok {
			found = true
		}
	})
	return found
}

// walkExpr visits the expression and its subexpressions in preorder.
func walkExpr(e grammar.Expr, f func(grammar.Expr)) {
	if e == nil {
		return
	}
	f(e)
	switch e := e.(type) {
	case *grammar.Seq:
		for _, it := range e.Items {
			walkExpr(it, f)
		}
	case *grammar.Choice:
		for _, alt := range e.Alts {
			walkExpr(alt, f)
		}
	case *grammar.Repeat:
		walkExpr(e.Expr, f)
	case *grammar.Optional:
		walkExpr(e.Expr, f)
	case *grammar.And:
		walkExpr(e.Expr, f)
	case *grammar.Not:
		walkExpr(e.Expr, f)
	case *grammar.Atomic:
		walkExpr(e.Expr, f)
	case *grammar.Discard:
		walkExpr(e.Expr, f)
	case *grammar.Capture:
		walkExpr(e.Expr, f)
	case *grammar.Attributed:
		walkExpr(e.Expr, f)
		for _, at := range e.Attrs {
			for _, arg := range at.Args {
				walkExpr(arg.Value, f)
			}
		}
	case *grammar.Pratt:
		walkExpr(e.Skip, f)
		for _, o := range e.Operands {
			walkExpr(o.Expr, f)
		}
		for _, l := range e.Levels {
			for _, op := range l.Operators {
				walkExpr(op.Expr, f)
			}
		}
	}
}

// walkTerm visits the value expression and its subexpressions in preorder.
func walkTerm(t grammar.Term, f func(grammar.Term)) {
	if t == nil {
		return
	}
	f(t)
	switch t := t.(type) {
	case *grammar.Member:
		walkTerm(t.X, f)
	case *grammar.New:
		for _, fi := range t.Fields {
			walkTerm(fi.Value, f)
		}
	case *grammar.Call:
		for _, a := range t.Args {
			walkTerm(a, f)
		}
	case *grammar.Lambda:
		walkTerm(t.Body, f)
	case *grammar.Binary:
		walkTerm(t.L, f)
		walkTerm(t.R, f)
	case *grammar.Unary:
		walkTerm(t.X, f)
	case *grammar.Assign:
		walkTerm(t.Value, f)
	}
}

func tarjan(rules []*grammar.RuleDef, edges map[string][]string) [][]string {
	index := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	var sccs [][]string
	next := 0
	var visit func(v string)
	visit = func(v string) {
		index[v] = next
		low[v] = next
		next++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range edges[v] {
			if _, seen := index[w]; !seen {
				visit(w)
				low[v] = min(low[v], low[w])
			} else if onStack[w] {
				low[v] = min(low[v], index[w])
			}
		}
		if low[v] == index[v] {
			var scc []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}
			sccs = append(sccs, scc)
		}
	}
	for _, r := range rules {
		if _, seen := index[r.Name]; !seen {
			visit(r.Name)
		}
	}
	return sccs
}

// findCycle finds one cycle within the SCC that does not pass through a leader.
func findCycle(scc []string, edges map[string][]string, in, leaders map[string]bool) []string {
	state := map[string]int{} // 0: unvisited, 1: in progress, 2: done
	var path []string
	var found []string
	var visit func(v string) bool
	visit = func(v string) bool {
		state[v] = 1
		path = append(path, v)
		for _, w := range edges[v] {
			if !in[w] || leaders[w] {
				continue
			}
			if state[w] == 1 {
				for i, p := range path {
					if p == w {
						found = append([]string(nil), path[i:]...)
						return true
					}
				}
			}
			if state[w] == 0 && visit(w) {
				return true
			}
		}
		path = path[:len(path)-1]
		state[v] = 2
		return false
	}
	for _, v := range scc {
		if !leaders[v] && state[v] == 0 && visit(v) {
			return found
		}
	}
	return nil
}

// propagate adds to set the rules that call rules in set (transitively).
func propagate(rules []*grammar.RuleDef, calls map[string][]string, set map[string]bool) {
	for changed := true; changed; {
		changed = false
		for _, r := range rules {
			if set[r.Name] {
				continue
			}
			for _, c := range calls[r.Name] {
				if set[c] {
					set[r.Name] = true
					changed = true
					break
				}
			}
		}
	}
}

// walkRuleTerms visits every term in a rule's predicates and actions, including
// Pratt operand and operator actions.
func walkRuleTerms(r *grammar.RuleDef, f func(grammar.Term)) {
	walkTerm(r.Action, f)
	walkExpr(r.Expr, func(e grammar.Expr) {
		switch e := e.(type) {
		case *grammar.Predicate:
			walkTerm(e.Term, f)
		case *grammar.Pratt:
			for _, o := range e.Operands {
				walkTerm(o.Action, f)
			}
			for _, l := range e.Levels {
				for _, op := range l.Operators {
					walkTerm(op.Action, f)
				}
			}
		}
	})
}

// ruleUsesPositions reports whether the rule's actions or predicates refer to startPos or
// endPos.
func ruleUsesPositions(r *grammar.RuleDef) bool {
	found := false
	walkRuleTerms(r, func(x grammar.Term) {
		if m, ok := x.(*grammar.Member); ok && (m.Name == "startPos" || m.Name == "endPos") {
			found = true
		}
	})
	return found
}
