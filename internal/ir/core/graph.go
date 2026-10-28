package core

import "slices"

// CallGraph sets the call graph of the module: Calls, SCC and Cyclic. A rule calls the rules of
// the Call and Nest nodes of its body, including those of its Operators Rule lines. Passes that
// add or remove calls run it again.
func CallGraph(m *Module) {
	m.Calls = make([][]int, len(m.Rules))
	for i, r := range m.Rules {
		if r != nil {
			m.Calls[i] = calls(r.Body)
		}
	}
	m.SCC, m.Cyclic = components(m.Calls)
}

// calls returns the rules that n calls, in increasing order.
func calls(n Node) []int {
	var out []int
	Walk(n, func(x Node) {
		switch x := x.(type) {
		case *Call:
			out = append(out, x.Rule)
		case *Nest:
			out = append(out, x.Rule)
		}
	})
	slices.Sort(out)
	return slices.Compact(out)
}

// components returns the strongly connected component of each node of the graph (Tarjan's
// algorithm), numbered in the order they are completed, and whether each component is a cycle.
func components(edges [][]int) ([]int, []bool) {
	n := len(edges)
	index, low := make([]int, n), make([]int, n)
	for i := range index {
		index[i] = -1
	}
	comp := make([]int, n)
	onStack := make([]bool, n)
	var stack []int
	var cyclic []bool
	next := 0
	var visit func(v int)
	visit = func(v int) {
		index[v], low[v] = next, next
		next++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range edges[v] {
			if w < 0 || w >= n {
				continue
			}
			if index[w] < 0 {
				visit(w)
				low[v] = min(low[v], low[w])
			} else if onStack[w] {
				low[v] = min(low[v], index[w])
			}
		}
		if low[v] != index[v] {
			return
		}
		c := len(cyclic)
		size := 0
		for {
			w := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[w] = false
			comp[w] = c
			size++
			if w == v {
				break
			}
		}
		cyclic = append(cyclic, size > 1 || slices.Contains(edges[v], v))
	}
	for v := range n {
		if index[v] < 0 {
			visit(v)
		}
	}
	return comp, cyclic
}
