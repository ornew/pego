package sample

// analysisGraph orders callees before callers where possible and records reverse
// dependencies. Cycles remain in the graph; fixedPoint revisits their callers.
// Calls keep walk's generation semantics, excluding negative lookaheads and
// recovery arguments. Repeated calls add only one reverse dependency.
type analysisGraph struct {
	order   []int
	offsets []int
	callers []int // callers of rule i are callers[offsets[i]:offsets[i+1]]
}

func (in *info) dependencies() analysisGraph {
	n := len(in.order)
	g := analysisGraph{order: make([]int, 0, n), offsets: make([]int, n+1)}
	seen := make([]int, n)
	for _, r := range in.order {
		for _, name := range r.calls {
			if c := in.rules[name]; c != nil && seen[c.index] != r.index+1 {
				seen[c.index] = r.index + 1
				g.offsets[c.index+1]++
			}
		}
	}
	for i := range n {
		g.offsets[i+1] += g.offsets[i]
	}
	g.callers = make([]int, g.offsets[n])
	// Reuse the future ordering buffer as insertion cursors. Contiguous edges
	// avoid allocating a separate small caller slice for every alias rule.
	next := g.order[:n]
	copy(next, g.offsets)
	clear(seen)
	for _, r := range in.order {
		for _, name := range r.calls {
			if c := in.rules[name]; c != nil && seen[c.index] != r.index+1 {
				seen[c.index] = r.index + 1
				g.callers[next[c.index]] = r.index
				next[c.index]++
			}
		}
	}
	clear(seen)
	// Explicit DFS frames avoid adding a recursive walk for long alias chains.
	type frame struct{ rule, next int }
	stack := make([]frame, 0, n)
	for _, root := range in.order {
		if seen[root.index] != 0 {
			continue
		}
		seen[root.index] = 1
		stack = append(stack, frame{rule: root.index})
		for len(stack) > 0 {
			f := &stack[len(stack)-1]
			r := in.order[f.rule]
			if f.next == len(r.calls) {
				g.order = append(g.order, f.rule)
				stack = stack[:len(stack)-1]
				continue
			}
			c := in.rules[r.calls[f.next]]
			f.next++
			if c != nil && seen[c.index] == 0 {
				seen[c.index] = 1
				stack = append(stack, frame{rule: c.index})
			}
		}
	}
	return g
}

// fixedPoint evaluates every rule once in dependency order, then only callers
// of changed rules. A bounded ring and membership bits keep each rule queued at
// most once, even in recursive graphs with many incoming edges.
func (g analysisGraph) fixedPoint(in *info, update func(*ruleInfo) bool) {
	n := len(g.order)
	if n == 0 {
		return
	}
	queue := append([]int(nil), g.order...)
	queued := make([]bool, n)
	for i := range queued {
		queued[i] = true
	}
	head, tail, count := 0, 0, n
	for count > 0 {
		i := queue[head]
		head = (head + 1) % n
		count--
		queued[i] = false
		if !update(in.order[i]) {
			continue
		}
		for _, caller := range g.callers[g.offsets[i]:g.offsets[i+1]] {
			if !queued[caller] {
				queue[tail] = caller
				tail = (tail + 1) % n
				count++
				queued[caller] = true
			}
		}
	}
}
