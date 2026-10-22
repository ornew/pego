// Package starttest lets tests whose subject is not start rules enter any rule
// of a grammar.
package starttest

import (
	"strings"

	"github.com/ornew/pego/grammar"
)

// MarkAll marks every rule of g that is not hidden @start, and the first of
// them @start(default) when there are several, unless a rule of g already
// has declaration attributes, and returns g. A hidden rule, whose name begins
// with an underscore, cannot be a start rule. Only the syntax tree changes:
// source positions stay where they were, so the positions tests expect in
// diagnostics do not move.
func MarkAll(g *grammar.Grammar) *grammar.Grammar {
	if g == nil {
		return nil
	}
	var marked []*grammar.RuleDef
	for _, rd := range g.Rules() {
		if len(rd.Attrs) > 0 {
			return g
		}
		if !strings.HasPrefix(rd.Name, "_") {
			marked = append(marked, rd)
		}
	}
	for i, rd := range marked {
		a := &grammar.Attribute{Name: "start"}
		if i == 0 && len(marked) > 1 {
			a.Args = []any{&grammar.Ref{Name: "default"}}
		}
		rd.Attrs = []*grammar.Attribute{a}
	}
	return g
}
