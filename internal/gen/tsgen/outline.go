package tsgen

import (
	"github.com/ornew/zeroparse/internal/ir/core"
	flowlower "github.com/ornew/zeroparse/internal/ir/flow/lower"
)

// Outlining of the generated TypeScript.
//
// A framed function that is printed in one piece grows with the grammar: a rule of many alternatives or a
// body that nests the same few matchers many times prints as thousands of lines that no other rule can
// share, and a function that large is neither compact nor fast for the JavaScript engine to compile. Some
// of its nodes therefore run as functions of their own (flow.CallOutline), which share a body wherever the
// text printed from a node is equal once the constants it names (literal tables, capture names) are
// replaced by their declarations (tsSharedKey, Generator.MethodName); the generator does not share the constants
// themselves, so equal nodes read tables of equal value under different names.
//
// The nodes are chosen by the structure of the Core IR: a node whose structure occurs in more than one
// place of the module (outlineDuplicate), and a node that is large (outlineLarge) so that no function
// grows beyond a bound.

const (
	// outlineLarge is the number of Core nodes from which a node runs as a function of its own.
	outlineLarge = 120
	// outlineChild is the number of Core nodes from which a child of a large node runs as a function of its
	// own.
	outlineChild = 16
	// outlineDuplicate is the number of Core nodes from which a structure that occurs more than once runs
	// as a function of its own in the Node table.
	outlineDuplicate = 1
)

// outlineNodes returns the nodes of the module m that run as functions of their own: a node that is
// Outlinable is chosen if it has at least outlineLarge Core nodes, or if build is set and
// its structure occurs at least twice where its function would not already be a shared body.
func outlineNodes(m *core.Module, build bool) map[core.Node]bool {
	type info struct {
		n    core.Node
		size int
		key  string
		kids []*info
	}
	var all []*info
	byNode := map[core.Node]*info{}
	var visit func(n core.Node) *info
	visit = func(n core.Node) *info {
		in := &info{n: n, size: 1}
		byNode[n] = in
		all = append(all, in)
		var kids []core.Node
		if o, ok := n.(*core.OperatorsRule); ok {
			for _, l := range o.Lines() {
				kids = append(kids, l.Body)
			}
		} else {
			kids = core.Children(n)
		}
		for _, c := range kids {
			if c != nil {
				k := visit(c)
				in.kids = append(in.kids, k)
				in.size += k.size
			}
		}
		return in
	}
	for _, r := range m.Rules {
		if r != nil {
			visit(r.Body)
		}
	}
	chosen := map[core.Node]bool{}
	candidate := func(in *info) bool { return flowlower.Outlinable(in.n) }
	var dup map[string]bool
	if build {
		count := map[string]int{}
		for _, in := range all {
			if in.size >= outlineDuplicate && in.size < outlineLarge && candidate(in) {
				in.key = core.PrintNode(m, in.n, true)
				count[in.key]++
			}
		}
		// Count the structures again as the program that runs them as functions of their own: the
		// copies inside a structure that is shared are one copy.
		dup = map[string]bool{}
		for k, c := range count {
			if c >= 2 {
				dup[k] = true
			}
		}
		for {
			eff := map[string]int{}
			seen := map[string]bool{}
			var walk func(n core.Node)
			walk = func(n core.Node) {
				in := byNode[n]
				if in.key != "" {
					eff[in.key]++
				}
				if in.key != "" && dup[in.key] {
					if seen[in.key] {
						return // a copy of a shared structure
					}
					seen[in.key] = true
				}
				var kids []core.Node
				if o, ok := n.(*core.OperatorsRule); ok {
					for _, l := range o.Lines() {
						kids = append(kids, l.Body)
					}
				} else {
					kids = core.Children(n)
				}
				for _, c := range kids {
					if c != nil {
						walk(c)
					}
				}
			}
			for _, r := range m.Rules {
				if r != nil {
					walk(r.Body)
				}
			}
			changed := false
			for k := range dup {
				if eff[k] < 2 {
					delete(dup, k)
					changed = true
				}
			}
			if !changed {
				break
			}
		}
	}
	for _, in := range all {
		if candidate(in) && (in.size >= outlineLarge || in.key != "" && dup[in.key]) {
			chosen[in.n] = true
		}
		if in.size >= outlineLarge {
			// The function of a large node keeps calls in place of its large parts.
			for _, k := range in.kids {
				if k.size >= outlineChild && candidate(k) {
					chosen[k.n] = true
				}
			}
		}
	}
	return chosen
}
