package gogen

import (
	"fmt"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/ir/core"
	irbuild "github.com/ornew/zeroparse/internal/ir/core/build"
)

// TypedCore builds the Core IR of the typed Go rules and runs the passes of the typed Go backend:
// term rewrites (with literal text dispatch and unboxed field comparisons unless disabled),
// terminal regions (in rules lowered to Go-local code of lean parsers without observability),
// choice guards (with guarded classes in lean parsers), scans and, in lean parsers, class runs.
// With a decisions hook (tests), it calls the hook with the module after it is built and after
// each pass.
func (g *Generator) TypedCore(origin map[core.Node]grammar.Expr) (*core.Module, error) {
	m, err := irbuild.CoreFrom(g.Prog, origin)
	if err != nil {
		return nil, err
	}
	hook := func(string, *core.Module) {}
	if g.CoreHook != nil {
		hook = g.CoreHook
	}
	hook("build", m)
	leanTyped := g.Ordinary && g.GoBudgetControls
	if leanTyped && !g.Observability && !g.disableTerminalPrograms {
		core.Terminals(m, CoreLocalRule)
		hook("terminals", m)
	}
	core.RewriteTerms(m, core.TermOptions{TextDispatch: !g.DisableLiteralTextDispatch, FieldCompare: !g.BoxedComparisons})
	hook("terms", m)
	core.Analyze(m)
	core.Guards(m, g.Ordinary && !g.DisableGuardedClasses)
	hook("guards", m)
	core.Scans(m)
	hook("scans", m)
	if leanTyped {
		core.ClassRuns(m)
		hook("class runs", m)
	}
	core.Analyze(m)
	if err := core.Verify(m); err != nil {
		return nil, fmt.Errorf("internal error: %w", err)
	}
	return m, nil
}

// CoreLocalRule reports whether the typed Go backend lowers the rule r to Go-local code: its
// captures are variables of the rule's function rather than a runtime frame. Left-recursion
// leaders, whose growth the runtime owns, Operators Rules and rules with recovery keep runtime
// frames.
func CoreLocalRule(r *core.Rule) bool {
	if r.Policy.Leader {
		return false
	}
	if _, ok := r.Body.(*core.OperatorsRule); ok {
		return false
	}
	recovers := false
	core.Walk(r.Body, func(n core.Node) {
		if _, ok := n.(*core.Recover); ok {
			recovers = true
		}
	})
	return !recovers
}
