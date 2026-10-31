package engine

import (
	"testing"

	"github.com/ornew/zeroparse/internal/ir/core"
)

// replaceTerm replaces the first term of the module, in rule order, for which f returns a
// replacement, and reports whether it found one.
func replaceTerm(m *core.Module, f func(core.Term) core.Term) bool {
	done := false
	var visit func(t core.Term) core.Term
	visit = func(t core.Term) core.Term {
		if done || t == nil {
			return t
		}
		if x := f(t); x != nil {
			done = true
			return x
		}
		switch t := t.(type) {
		case *core.Member:
			t.X = visit(t.X)
		case *core.New:
			for i := range t.Fields {
				t.Fields[i].Value = visit(t.Fields[i].Value)
			}
		case *core.Convert:
			t.X = visit(t.X)
		case *core.Tuple:
			for i := range t.Elems {
				t.Elems[i] = visit(t.Elems[i])
			}
		case *core.Builtin:
			for i := range t.Args {
				t.Args[i] = visit(t.Args[i])
			}
		case *core.Lambda:
			t.Body = visit(t.Body)
		case *core.Binary:
			t.L, t.R = visit(t.L), visit(t.R)
		case *core.Unary:
			t.X = visit(t.X)
		case *core.Cast:
			t.X = visit(t.X)
		case *core.TextEq:
			t.L, t.R = visit(t.L), visit(t.R)
		case *core.FieldCompare:
			t.L.X, t.R.X = visit(t.L.X), visit(t.R.X)
		case *core.ProjectedMap:
			t.X = visit(t.X)
		case *core.ConcatFused:
			for i := range t.Parts {
				p := &t.Parts[i]
				for j := range p.Args {
					p.Args[j] = visit(p.Args[j])
				}
				p.X = visit(p.X)
			}
		}
		return t
	}
	for _, r := range m.Rules {
		if r.Finish.Kind == core.FinishAction {
			r.Finish.Action = visit(r.Finish.Action)
		}
		core.Walk(r.Body, func(n core.Node) {
			switch n := n.(type) {
			case *core.Pred:
				n.Term = visit(n.Term)
			case *core.BindNode:
				n.Term = visit(n.Term)
			case *core.OperatorsRule:
				for _, l := range n.Lines() {
					l.Action = visit(l.Action)
				}
			}
		})
	}
	return done
}

// unfieldOperand returns the term an operand of a field comparison stands for.
func unfieldOperand(o core.Operand) core.Term {
	if !o.Field {
		return o.X
	}
	t := &core.Member{X: o.X, Name: o.Name}
	core.SetType(t, o.Type)
	return t
}

// TestCoreTermRewritesFrozen compares the structure and forms of every action, predicate,
// binding and Operators line action after the term rewrites with the frozen ones, over
// the corpus in every mode and over a grammar with text comparisons of two captures, which the
// corpus lacks. It checks that a term pass that leaves out a rewrite is caught.
func TestCoreTermRewritesFrozen(t *testing.T) {
	checkCoreAspect(t, "term", []coreBreak{
		{"text dispatch left out", "terms", []string{"term"}, func(c *coreGenCase, m *core.Module) bool {
			return replaceTerm(m, func(t core.Term) core.Term {
				if x, ok := t.(*core.TextIn); ok {
					return x.Orig
				}
				return nil
			})
		}},
		{"field comparison left out", "terms", []string{"term"}, func(c *coreGenCase, m *core.Module) bool {
			return replaceTerm(m, func(t core.Term) core.Term {
				if x, ok := t.(*core.FieldCompare); ok {
					b := &core.Binary{Op: x.Op, L: unfieldOperand(x.L), R: unfieldOperand(x.R)}
					core.SetType(b, core.TypeOf(x))
					return b
				}
				return nil
			})
		}},
		{"text equality left out", "terms", []string{"term"}, func(c *coreGenCase, m *core.Module) bool {
			return replaceTerm(m, func(t core.Term) core.Term {
				if x, ok := t.(*core.TextEq); ok {
					op := "=="
					if x.Neg {
						op = "!="
					}
					b := &core.Binary{Op: op, L: x.L, R: x.R}
					core.SetType(b, core.TypeOf(x))
					return b
				}
				return nil
			})
		}},
		{"concat fusion left out", "terms", []string{"term"}, func(c *coreGenCase, m *core.Module) bool {
			return replaceTerm(m, func(t core.Term) core.Term {
				x, ok := t.(*core.ConcatFused)
				if !ok {
					return nil
				}
				// The parts as the list and map calls they were; a list has the type of the concat.
				b := &core.Builtin{Func: "concat"}
				for _, p := range x.Parts {
					var arg core.Term
					switch p.Kind {
					case core.PartList:
						arg = &core.Builtin{Func: "list", Args: p.Args}
					case core.PartMap:
						arg = &core.Builtin{Func: "map", Args: []core.Term{p.X, p.Fn}}
					default:
						arg = p.X
					}
					if p.Kind != core.PartItems {
						core.SetType(arg, core.TypeOf(x))
					}
					b.Args = append(b.Args, arg)
				}
				core.SetType(b, core.TypeOf(x))
				return b
			})
		}},
		{"projected map left out", "terms", []string{"term"}, func(c *coreGenCase, m *core.Module) bool {
			return replaceTerm(m, func(t core.Term) core.Term {
				if x, ok := t.(*core.ProjectedMap); ok {
					b := &core.Builtin{Func: "list", Args: []core.Term{x.X}}
					core.SetType(b, core.TypeOf(x))
					return b
				}
				return nil
			})
		}},
	})
	for _, mode := range coreGenModes {
		forms := map[string]int{}
		for _, c := range coreGenCases(t, mode, nil) {
			if c.name != "text equality" && c.name != "field comparison" {
				continue
			}
			n := coreDecisionCounts{forms: forms}
			for _, err := range compareCoreDecisions(c, &n) {
				t.Errorf("%s: %s: %s", mode.name, c.name, err)
			}
		}
		fields := 3
		if mode.opts.BoxedComparisons {
			fields = 0
		}
		if forms["text-eq"] != 2 || forms["field-cmp"] != fields {
			t.Errorf("%s: forms %v", mode.name, forms)
		}
	}
}
