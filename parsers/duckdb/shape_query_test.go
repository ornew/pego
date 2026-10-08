package duckdb_test

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/ornew/pego/parsers/duckdb"
)

// cteQuery maps the query of a common table expression; that of a recursive one is a RECURSIVE_CTE_NODE.
func (m *mapper) cteQuery(recursive bool, c *duckdb.CTE, q *duckdb.Select, aliases []any) any {
	if recursive {
		if op, ok := q.Body.(*duckdb.SetOp); ok && strings.EqualFold(op.Op, "union") {
			if q.With != nil || q.OrderBy != nil || q.Limit != nil || q.Offset != nil || op.ByName {
				unsup("recursive cte modifiers")
			}
			var keys []any
			for _, k := range c.UsingKey {
				keys = append(keys, shape{"class": "COLUMN_REF", "type": "COLUMN_REF", "column_names": []any{k.Name()}})
			}
			return shape{"type": "RECURSIVE_CTE_NODE", "cte_name": c.Name.Name(), "aliases": aliases, "key_targets": keys,
				"union_all": strings.EqualFold(op.Quantifier, "all"), "left": m.setopSide(op.Left), "right": m.setopSide(op.Right)}
		}
	}
	return m.selectNode(q)
}

// groupBy maps the items of GROUP BY to the expressions of DuckDB, without repetition, and the grouping sets that
// are made of them: the product of the sets of each item.
func (m *mapper) groupBy(items []duckdb.Expr) (exprs []any, sets []any) {
	index := map[string]int{}
	idx := func(e duckdb.Expr) int {
		x := m.expr(e)
		b, _ := json.Marshal(reduce(x))
		if i, ok := index[string(b)]; ok {
			return i
		}
		index[string(b)] = len(exprs)
		exprs = append(exprs, x)
		return len(exprs) - 1
	}
	union := func(a, b []int) []int {
		seen := map[int]bool{}
		var out []int
		for _, x := range append(append([]int(nil), a...), b...) {
			if !seen[x] {
				seen[x] = true
				out = append(out, x)
			}
		}
		sort.Ints(out)
		return out
	}
	// the element of ROLLUP, CUBE and GROUPING SETS: an expression or a parenthesized list of them
	elem := func(e duckdb.Expr) []int {
		if r, ok := e.(*duckdb.Row); ok && !r.Keyword {
			var out []int
			for _, it := range r.Items {
				out = union(out, []int{idx(it)})
			}
			return out
		}
		return []int{idx(e)}
	}
	var setsOf func(item duckdb.Expr) [][]int
	setsOf = func(item duckdb.Expr) [][]int {
		gs, ok := item.(*duckdb.GroupingSet)
		if !ok {
			// GROUP BY (a, b) is two expressions of one set
			if r, ok := item.(*duckdb.Row); ok && !r.Keyword {
				return [][]int{elem(r)}
			}
			return [][]int{{idx(item)}}
		}
		switch kind := strings.Join(strings.Fields(lower(gs.Kind)), " "); kind {
		case "":
			return [][]int{{}}
		case "rollup":
			out := [][]int{{}}
			var cur []int
			for _, it := range gs.Items {
				cur = union(cur, elem(it))
				out = append(out, cur)
			}
			return out
		case "cube":
			var es [][]int
			for _, it := range gs.Items {
				es = append(es, elem(it))
			}
			var out [][]int
			var walk func(cur []int, start int)
			walk = func(cur []int, start int) {
				out = append(out, cur)
				for k := start; k < len(es); k++ {
					walk(union(cur, es[k]), k+1)
				}
			}
			walk([]int{}, 0)
			return out
		case "grouping sets":
			var out [][]int
			for _, it := range gs.Items {
				if _, nested := it.(*duckdb.GroupingSet); nested {
					out = append(out, setsOf(it)...)
				} else {
					out = append(out, elem(it))
				}
			}
			return out
		default:
			unsup("grouping %s", kind)
		}
		return nil
	}
	result := [][]int{{}}
	for _, it := range items {
		var next [][]int
		ss := setsOf(it)
		for _, r := range result {
			for _, s := range ss {
				next = append(next, union(r, s))
			}
		}
		result = next
	}
	for _, r := range result {
		set := []any{}
		for _, x := range r {
			set = append(set, x)
		}
		sets = append(sets, set)
	}
	return exprs, sets
}

// replaceList and renameList map the modifiers of a star.
func (m *mapper) replaceList(items []*duckdb.ReplaceItem) []any {
	var out []any
	for i := range items {
		out = append(out, shape{"key": items[i].Name.Name(), "value": m.expr(items[i].Expr)})
	}
	return out
}

func (m *mapper) renameList(items []*duckdb.RenameItem) []any {
	var out []any
	for i := range items {
		if len(items[i].From.Parts) != 1 {
			unsup("qualified rename")
		}
		out = append(out, shape{"key": shape{"column": items[i].From.Names()[0]}, "value": items[i].To.Name()})
	}
	return out
}
