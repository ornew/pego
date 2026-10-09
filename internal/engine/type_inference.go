package engine

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ornew/pego/grammar"
)

// maxInferredGrowth bounds successive depth increases of a recursive rule's
// type after its initial seed. It is a resource budget, not an infinitude proof.
const maxInferredGrowth = 50

// inferTypes processes dependencies before their callers. Only recursive
// components need iteration and a conservative bound on evolving type size.
func (k *checker) inferTypes(rules []*grammar.RuleDef) {
	var inferred []*grammar.RuleDef
	defs := map[string]*grammar.RuleDef{}
	for _, rd := range rules {
		if !k.declared[rd.Name] {
			inferred = append(inferred, rd)
			defs[rd.Name] = rd
		}
	}
	depends := map[string][]string{}
	for _, rd := range inferred {
		for _, name := range allCalls(rd.Expr, nil) {
			if defs[name] != nil {
				depends[rd.Name] = append(depends[rd.Name], name)
			}
		}
		if pr, ok := rd.Expr.(*grammar.Pratt); ok {
			for _, level := range pr.Levels {
				for _, op := range level.Operators {
					if op.Action != nil {
						// Operator actions can read the rule's own inferred type
						// through lhs/rhs without a syntactic rule reference.
						depends[rd.Name] = append(depends[rd.Name], rd.Name)
					}
				}
			}
		}
	}
	// Tarjan emits a component after all components it depends on.
	for _, component := range tarjan(inferred, depends) {
		if len(component) == 1 && !contains(depends[component[0]], component[0]) {
			rd := defs[component[0]]
			k.ruleTypes[rd.Name] = k.ruleType(rd)
			continue
		}
		k.inferRecursive(component, defs)
	}
}

func (k *checker) inferRecursive(names []string, defs map[string]*grammar.RuleDef) {
	frozen := map[string]bool{}
	texts := make([]string, len(names))
	depths := make([]int, len(names))
	sizes := make([]int, len(names))
	never, unknown := inferredShape(tyNever), inferredShape(tyAny)
	growth := make([]int, len(names))
	for i, name := range names {
		shape := inferredShape(k.ruleTypes[name])
		texts[i], depths[i], sizes[i] = shape.key, shape.depth, shape.size
	}
	// Projections and builtins need not be monotone on interim types. Detect
	// a repeating non-stable vector instead of imposing a global round cap.
	key := func(texts []string) string { return typeKey("v", texts...) }
	states := [][]string{append([]string(nil), texts...)}
	seen := map[string]int{key(texts): 0}
	for {
		changed, widened := false, false
		for i, name := range names {
			if frozen[name] {
				continue
			}
			t := k.ruleType(defs[name])
			shape := inferredShape(t)
			text := shape.key
			if text == texts[i] {
				continue
			}
			// An oversized first result can still be finite (r = r / huge).
			// Bound further growth only, and never reevaluate a widened rule.
			if texts[i] != never.key && shape.depth > depths[i] {
				growth[i]++
			}
			oversized := texts[i] != never.key && shape.size > maxInferredType && shape.size > sizes[i]
			if growth[i] >= maxInferredGrowth || oversized {
				t, shape = tyAny, unknown
				text = shape.key
				frozen[name], widened = true, true
			}
			k.ruleTypes[name], texts[i], depths[i], sizes[i] = t, text, shape.depth, shape.size
			changed = true
		}
		if !changed {
			return
		}
		state := key(texts)
		if start, ok := seen[state]; ok && !widened {
			// Widen only rules that changed in this cycle; stable peers keep
			// their types, including rules with constant actions.
			for i, name := range names {
				for _, prior := range states[start:] {
					if prior[i] != texts[i] {
						k.ruleTypes[name], texts[i], depths[i], sizes[i] = tyAny, unknown.key, unknown.depth, unknown.size
						frozen[name], widened = true, true
						break
					}
				}
			}
		}
		if widened {
			states = nil
			clear(seen)
			state = key(texts)
		}
		seen[state] = len(states)
		states = append(states, append([]string(nil), texts...))
	}
}

// inferredShape compares types without depending on union-member order. Keep
// keys tagged and framed even for malformed AST names; public String is unchanged.
// size counts the written form, while depth tracks structural expansion.
type inferredTypeShape struct {
	key         string
	size, depth int
}

func typeKey(tag string, parts ...string) string {
	var b strings.Builder
	b.WriteString(tag)
	for _, part := range parts {
		fmt.Fprintf(&b, "%d:%s", len(part), part)
	}
	return b.String()
}

func inferredShape(t ty) inferredTypeShape {
	switch t := t.(type) {
	case basicTy:
		return inferredTypeShape{typeKey("b", string(t)), len(t), 1}
	case namedTy:
		return inferredTypeShape{typeKey("n"+string(t.kind), t.name), len(t.name), 1}
	case listTy:
		s := inferredShape(t.elem)
		if _, ok := t.elem.(unionTy); ok {
			s.size += 2
		}
		return inferredTypeShape{typeKey("l", s.key), s.size + 2, s.depth + 1}
	case optTy:
		s := inferredShape(t.elem)
		if _, ok := t.elem.(unionTy); ok {
			s.size += 2
		}
		return inferredTypeShape{typeKey("o", s.key), s.size + 1, s.depth + 1}
	case unionTy:
		var keys []string
		size, depth := 0, 0
		for _, alt := range t.alts {
			s := inferredShape(alt)
			keys = append(keys, s.key)
			size += s.size
			depth = max(depth, s.depth)
		}
		size += 3 * (len(t.alts) - 1)
		slices.Sort(keys)
		keys = slices.Compact(keys)
		if len(keys) == 1 {
			return inferredTypeShape{keys[0], size, depth}
		}
		return inferredTypeShape{typeKey("u", keys...), size, depth + 1}
	case recordTy:
		var names []string
		for name := range t.fields {
			names = append(names, name)
		}
		slices.Sort(names)
		var parts []string
		size, depth := len("Seq{}"), 0
		for i, name := range names {
			s := inferredShape(t.fields[name])
			parts = append(parts, name, s.key)
			size += len(name) + 2 + s.size
			if i > 0 {
				size += 2
			}
			depth = max(depth, s.depth)
		}
		return inferredTypeShape{typeKey("r", parts...), size, depth + 1}
	}
	panic("unknown inferred type")
}
