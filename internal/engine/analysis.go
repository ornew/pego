package engine

import (
	"slices"
	"sort"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/grammaranalysis"
)

// analysis is the result of static analysis across rules.
type analysis struct {
	control    *compileControl
	nullable   map[string]bool
	succeeding map[string]bool
	// leaders are the rules that break left-recursion cycles. These rules are evaluated with
	// grow-the-seed.
	leaders map[string]bool
	// cyclic are the rules that belong to a left-recursion cycle (including leaders).
	cyclic map[string]bool
	// positional are the rules whose actions or predicates read positions (.Start, .End),
	// directly or through callees. Their results may contain position values, so incremental
	// parsing cannot shift them past an edit.
	positional map[string]bool
}

func analyze(rules []*grammar.RuleDef) *analysis {
	return analyzeControlled(rules, nil)
}

func analyzeControlled(rules []*grammar.RuleDef, control *compileControl) *analysis {
	a := &analysis{
		control:    control,
		nullable:   map[string]bool{},
		leaders:    map[string]bool{},
		cyclic:     map[string]bool{},
		positional: map[string]bool{},
	}
	byName := map[string]*grammar.RuleDef{}
	for _, r := range rules {
		control.poll()
		byName[r.Name] = r
	}

	// Fixed-point computation of nullability.
	for changed := true; changed; {
		control.poll()
		changed = false
		for _, r := range rules {
			control.poll()
			if !a.nullable[r.Name] && a.isNullable(r.Expr) {
				a.nullable[r.Name] = true
				changed = true
			}
		}
	}

	// Propagate variable reads.
	calls := map[string][]string{}
	for _, r := range rules {
		control.poll()
		calls[r.Name] = allCallsControlled(r.Expr, nil, control)
	}
	for _, r := range rules {
		control.poll()
		if ruleUsesPositions(r, control) {
			a.positional[r.Name] = true
		}
	}
	propagate(rules, calls, a.positional, control)

	// Find left-recursion cycles from the strongly connected components of the left-call graph.
	order := map[string]int{}
	left := map[string][]string{}
	for i, r := range rules {
		control.poll()
		order[r.Name] = i
		for _, c := range a.leftCalls(r.Expr, nil) {
			control.poll()
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
		control.poll()
		components = tarjan(rules, left, control)
		clear(a.cyclic)
		for _, scc := range components {
			control.poll()
			if len(scc) > 1 || contains(left[scc[0]], scc[0]) {
				for _, n := range scc {
					control.poll()
					a.cyclic[n] = true
				}
			}
		}
		if control == nil {
			a.succeeding = grammaranalysis.SucceedingRules(rules, a.cyclic)
		} else {
			a.succeeding = grammaranalysis.SucceedingRulesWithPoll(rules, a.cyclic, control.poll)
		}
		next := map[string][]string{}
		changed := false
		for _, r := range rules {
			control.poll()
			for _, n := range a.leftCalls(r.Expr, nil) {
				control.poll()
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
		control.poll()
		if len(scc) == 1 && !contains(left[scc[0]], scc[0]) {
			continue
		}
		in := map[string]bool{}
		for _, n := range scc {
			control.poll()
			in[n] = true
			a.cyclic[n] = true
		}
		// Until every cycle passes through some leader, make the first rule of a found cycle a leader.
		for {
			control.poll()
			cycle := findCycle(scc, left, in, a.leaders, control)
			if cycle == nil {
				break
			}
			best := cycle[0]
			for _, n := range cycle {
				control.poll()
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
	a.control.poll()
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
		return e.Min == 0 || e.Min == 1 && a.isNullable(e.Expr)
	case *grammar.Atomic:
		return a.isNullable(e.Expr)
	case *grammar.Discard:
		return a.isNullable(e.Expr)
	case *grammar.Capture:
		return a.isNullable(e.Expr)
	case *grammar.Attributed:
		return a.isNullable(e.Expr)
	case *grammar.OperatorsRule:
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
	a.control.poll()
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
			if a.alwaysSucceeds(alt) {
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
		if !a.alwaysSucceeds(e.Expr) {
			for _, at := range e.Attrs {
				if at.Name == "recover" {
					if skip, ok := at.Arg("skip"); ok {
						acc = a.leftCalls(skip, acc)
					}
				}
			}
		}
	case *grammar.OperatorsRule:
		if e.Skip != nil {
			acc = a.leftCalls(e.Skip, acc)
		}
		for _, o := range e.Operands {
			acc = a.leftCalls(o.Expr, acc)
			if a.alwaysSucceeds(o.Expr) {
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
	return allCallsControlled(e, acc, nil)
}

func allCallsControlled(e grammar.Expr, acc []string, control *compileControl) []string {
	walkExprControlled(e, func(x grammar.Expr) {
		control.poll()
		if r, ok := x.(*grammar.Ref); ok {
			acc = append(acc, r.Name)
		}
	}, control)
	return acc
}

// ruleVariables returns, for each rule, the sorted names of the variables read by
// predicates and actions in the rule or in the rules it calls, directly or
// indirectly. A rule's result depends on the environment
// only through the values of these variables at the call: definitions made inside a rule are
// undone when it returns.
func ruleVariables(rules []*grammar.RuleDef) map[string][]string {
	return ruleVariablesControlled(rules, nil)
}

func ruleVariablesControlled(rules []*grammar.RuleDef, control *compileControl) map[string][]string {
	direct := map[string]map[string]bool{}
	for _, r := range rules {
		control.poll()
		var set map[string]bool
		walkRuleTermsControlled(r, func(t grammar.Term) {
			control.poll()
			if v, ok := t.(*grammar.VarRef); ok {
				if set == nil {
					set = map[string]bool{}
				}
				set[v.Name] = true
			}
		}, control)
		if len(set) > 0 {
			direct[r.Name] = set
		} else {
			delete(direct, r.Name)
		}
	}
	out := map[string][]string{}
	// With no direct variable reads there is nothing to propagate, even
	// through cycles. Avoid building a graph or visiting transitive closures.
	if len(direct) == 0 {
		return out
	}
	calls := make(map[string][]string, len(rules))
	for _, r := range rules {
		control.poll()
		calls[r.Name] = allCallsControlled(r.Expr, nil, control)
	}
	// Tarjan emits callees before their callers. Every rule in a recursive
	// component reaches all its peers, so compute their union only once.
	// A worklist repeatedly copying evolving sets is costly when many peers
	// introduce different variables, including in long acyclic chains.
	for _, component := range tarjan(rules, calls, control) {
		control.poll()
		all := map[string]bool{}
		for _, name := range component {
			control.poll()
			for v := range direct[name] {
				control.poll()
				all[v] = true
			}
			for _, callee := range calls[name] {
				control.poll()
				// Internal edges have no published result yet; direct peer reads
				// above already account for them. Missing rules contribute nothing.
				for _, v := range out[callee] {
					control.poll()
					all[v] = true
				}
			}
		}
		if len(all) == 0 {
			continue
		}
		names := make([]string, 0, len(all))
		for v := range all {
			control.poll()
			names = append(names, v)
		}
		sort.Strings(names)
		// Rule metadata only reads these sorted keys. Share the immutable
		// result within the component, rather than allocating it per peer.
		for _, name := range component {
			control.poll()
			out[name] = names
		}
	}
	return out
}

// walkExpr visits the expression and its subexpressions in preorder.
func walkExprControlled(e grammar.Expr, f func(grammar.Expr), control *compileControl) {
	if control == nil {
		walkExpr(e, f)
		return
	}
	control.poll()
	if e == nil {
		return
	}
	f(e)
	switch e := e.(type) {
	case *grammar.Group:
		walkExprControlled(e.Expr, f, control)
	case *grammar.Seq:
		for _, it := range e.Items {
			walkExprControlled(it, f, control)
		}
	case *grammar.Choice:
		for _, alt := range e.Alts {
			walkExprControlled(alt, f, control)
		}
	case *grammar.Repeat:
		walkExprControlled(e.Expr, f, control)
	case *grammar.InlineAction:
		walkExprControlled(e.Expr, f, control)
	case *grammar.Optional:
		walkExprControlled(e.Expr, f, control)
	case *grammar.And:
		walkExprControlled(e.Expr, f, control)
	case *grammar.Not:
		walkExprControlled(e.Expr, f, control)
	case *grammar.Atomic:
		walkExprControlled(e.Expr, f, control)
	case *grammar.Stream:
		walkExprControlled(e.Expr, f, control)
	case *grammar.Recovery:
		walkExprControlled(e.Expr, f, control)
		walkExprControlled(e.Skip, f, control)
	case *grammar.Discard:
		walkExprControlled(e.Expr, f, control)
	case *grammar.Capture:
		walkExprControlled(e.Expr, f, control)
	case *grammar.Attributed:
		walkExprControlled(e.Expr, f, control)
		for _, at := range e.Attrs {
			control.poll()
			for _, arg := range control.expressionArgs(at) {
				walkExprControlled(arg, f, control)
			}
		}
	case *grammar.OperatorsRule:
		walkExprControlled(e.Skip, f, control)
		for _, o := range e.Operands {
			walkExprControlled(o.Expr, f, control)
		}
		for _, l := range e.Levels {
			control.poll()
			for _, op := range l.Operators {
				walkExprControlled(op.Expr, f, control)
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
	case *grammar.TermGroup:
		walkTerm(t.Term, f)
	case *grammar.Block:
		for _, stmt := range t.Statements {
			walkTerm(stmt, f)
		}
	case *grammar.Member:
		walkTerm(t.X, f)
	case *grammar.New:
		for _, fi := range t.Fields {
			walkTerm(fi.Value, f)
		}
		for _, x := range t.Args {
			walkTerm(x, f)
		}
	case *grammar.TupleLit:
		for _, x := range t.Elems {
			walkTerm(x, f)
		}
	case *grammar.ListConstructor:
		for _, item := range t.Items {
			walkTerm(item, f)
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
	case *grammar.Cast:
		walkTerm(t.X, f)
	case *grammar.Bind:
		walkTerm(t.Value, f)
	}
}

func tarjan(rules []*grammar.RuleDef, edges map[string][]string, control *compileControl) [][]string {
	index := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	var sccs [][]string
	next := 0
	var visit func(v string)
	visit = func(v string) {
		control.poll()
		index[v] = next
		low[v] = next
		next++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range edges[v] {
			control.poll()
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
				control.poll()
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
		control.poll()
		if _, seen := index[r.Name]; !seen {
			visit(r.Name)
		}
	}
	return sccs
}

func findCycle(scc []string, edges map[string][]string, in, leaders map[string]bool, control *compileControl) []string {
	state := map[string]int{} // 0: unvisited, 1: in progress, 2: done
	var path []string
	var found []string
	var visit func(v string) bool
	visit = func(v string) bool {
		control.poll()
		state[v] = 1
		path = append(path, v)
		for _, w := range edges[v] {
			control.poll()
			if !in[w] || leaders[w] {
				continue
			}
			if state[w] == 1 {
				for i, p := range path {
					control.poll()
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
		control.poll()
		if !leaders[v] && state[v] == 0 && visit(v) {
			return found
		}
	}
	return nil
}

func propagate(rules []*grammar.RuleDef, calls map[string][]string, set map[string]bool, control *compileControl) {
	for changed := true; changed; {
		control.poll()
		changed = false
		for _, r := range rules {
			control.poll()
			if set[r.Name] {
				continue
			}
			for _, c := range calls[r.Name] {
				control.poll()
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
// Operators Rule operand and operator actions.
func walkRuleTermsControlled(r *grammar.RuleDef, f func(grammar.Term), control *compileControl) {
	control.poll()
	walkTerm(r.Action, f)
	walkExprControlled(r.Expr, func(e grammar.Expr) {
		switch e := e.(type) {
		case *grammar.Predicate:
			walkTerm(e.Term, f)
		case *grammar.OperatorsRule:
			for _, o := range e.Operands {
				control.poll()
				walkTerm(o.Action, f)
			}
			for _, l := range e.Levels {
				control.poll()
				for _, op := range l.Operators {
					control.poll()
					walkTerm(op.Action, f)
				}
			}
		}
	}, control)
}

func ruleUsesPositions(r *grammar.RuleDef, control *compileControl) bool {
	found := false
	walkRuleTermsControlled(r, func(x grammar.Term) {
		control.poll()
		if m, ok := x.(*grammar.Member); ok && (m.Name == "Start" || m.Name == "End") {
			found = true
		}
	}, control)
	return found
}

// alwaysSucceeds preserves the ordinary proof and shares the invocation control.
func (a *analysis) alwaysSucceeds(e grammar.Expr) bool {
	if a.control == nil {
		return grammaranalysis.AlwaysSucceeds(e, a.succeeding)
	}
	return grammaranalysis.AlwaysSucceedsWithPoll(e, a.succeeding, a.control.poll)
}

func walkExpr(e grammar.Expr, f func(grammar.Expr)) {
	if e == nil {
		return
	}
	f(e)
	switch e := e.(type) {
	case *grammar.Group:
		walkExpr(e.Expr, f)
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
	case *grammar.InlineAction:
		walkExpr(e.Expr, f)
	case *grammar.Optional:
		walkExpr(e.Expr, f)
	case *grammar.And:
		walkExpr(e.Expr, f)
	case *grammar.Not:
		walkExpr(e.Expr, f)
	case *grammar.Atomic:
		walkExpr(e.Expr, f)
	case *grammar.Stream:
		walkExpr(e.Expr, f)
	case *grammar.Recovery:
		walkExpr(e.Expr, f)
		walkExpr(e.Skip, f)
	case *grammar.Discard:
		walkExpr(e.Expr, f)
	case *grammar.Capture:
		walkExpr(e.Expr, f)
	case *grammar.Attributed:
		walkExpr(e.Expr, f)
		for _, at := range e.Attrs {
			for _, arg := range at.ExpressionArgs() {
				walkExpr(arg, f)
			}
		}
	case *grammar.OperatorsRule:
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

func walkRuleTerms(r *grammar.RuleDef, f func(grammar.Term)) { walkRuleTermsControlled(r, f, nil) }
