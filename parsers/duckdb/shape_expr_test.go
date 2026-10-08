package duckdb_test

import (
	"strings"

	"github.com/ornew/pego/parsers/duckdb"
)

// lambda maps LAMBDA x, y: body.
func (m *mapper) lambda(e *duckdb.Lambda) shape {
	var params []any
	for i := range e.Params {
		params = append(params, shape{"class": "COLUMN_REF", "type": "COLUMN_REF", "column_names": []any{e.Params[i].Name()}})
	}
	var lhs any
	if len(params) == 1 {
		lhs = params[0]
	} else {
		lhs = fn("row", "", false, params...)
	}
	return shape{"class": "LAMBDA", "type": "LAMBDA", "lhs": lhs, "expr": m.expr(e.Body)}
}

// intervalFunctions are the functions that the transformer of DuckDB builds INTERVAL ... unit with, and the type
// that the amount is cast to (truncated first, but for the units that are fractions of a second).
var intervalFunctions = map[string]struct {
	fn, cast string
	trunc    bool
}{
	"year": {"to_years", "INTEGER", true}, "quarter": {"to_quarters", "INTEGER", true}, "month": {"to_months", "INTEGER", true},
	"week": {"to_weeks", "INTEGER", true}, "day": {"to_days", "INTEGER", true}, "decade": {"to_decades", "INTEGER", true},
	"century": {"to_centuries", "INTEGER", true}, "centuries": {"to_centuries", "INTEGER", true},
	"millennium": {"to_millennia", "INTEGER", true}, "millennia": {"to_millennia", "INTEGER", true},
	"hour": {"to_hours", "BIGINT", true}, "minute": {"to_minutes", "BIGINT", true},
	"second": {"to_seconds", "DOUBLE", false}, "millisecond": {"to_milliseconds", "DOUBLE", false},
	"microsecond": {"to_microseconds", "BIGINT", true},
}

func castTo(child any, id string) shape {
	return shape{"class": "CAST", "type": "OPERATOR_CAST", "child": child, "cast_type": shape{"id": id}}
}

// interval maps INTERVAL '1' DAY, INTERVAL (x) HOUR and INTERVAL '1 day'.
func (m *mapper) interval(e *duckdb.IntervalLit) any {
	v := m.expr(e.Value)
	if e.Unit == nil {
		return castTo(v, "INTERVAL")
	}
	unit := strings.Join(strings.Fields(lower(e.Unit.Text)), " ")
	if strings.Contains(unit, " ") {
		unsup("interval range")
	}
	if _, ok := intervalFunctions[unit]; !ok {
		unit = strings.TrimSuffix(unit, "s")
	}
	f, ok := intervalFunctions[unit]
	if !ok {
		unsup("interval unit %s", unit)
	}
	arg := castTo(v, "DOUBLE")
	if f.trunc {
		arg = castTo(fn("trunc", "", false, arg), f.cast)
	}
	return fn(f.fn, "", false, arg)
}

// negatedComparison is the comparison that is true when the given one is not.
var negatedComparison = map[string]string{
	"=": "<>", "<>": "=", "!=": "=", "<": ">=", "<=": ">", ">": "<=", ">=": "<",
}

// anyAll maps x = ANY (...) and x < ALL (...).
func (m *mapper) anyAll(e *duckdb.AnyAll) any {
	op := e.Op
	all := strings.EqualFold(e.Quantifier, "all")
	if all {
		neg, ok := negatedComparison[op]
		if !ok {
			unsup("ALL with %s", op)
		}
		op = neg
	}
	typ, ok := comparisons[op]
	if !ok {
		unsup("ANY with %s", op)
	}
	var sub shape
	switch src := e.Source.(type) {
	case *duckdb.Subquery:
		sub = m.subquery("ANY", src.Query)
	default:
		x := e.Source
		if p, ok := x.(*duckdb.Paren); ok {
			x = p.X
		}
		node := shape{"type": "SELECT_NODE", "aggregate_handling": "STANDARD_HANDLING", "from_table": shape{"type": "EMPTY"},
			"select_list": []any{fn("unnest", "", false, m.expr(x))}}
		sub = shape{"class": "SUBQUERY", "type": "SUBQUERY", "subquery_type": "ANY", "subquery": shape{"node": node}}
	}
	sub["child"] = m.expr(e.X)
	sub["comparison_type"] = typ
	if all {
		return opNode("OPERATOR_NOT", sub)
	}
	return sub
}

// columns maps COLUMNS(...) and *COLUMNS(...).
func (m *mapper) columns(e *duckdb.ColumnsExpr) any {
	var s shape
	if st, ok := e.Arg.(*duckdb.Star); ok {
		s = m.expr(st).(shape)
	} else if _, lam := e.Arg.(*duckdb.Lambda); lam || isArrow(e.Arg) {
		// COLUMNS(lambda) keeps the columns for which the lambda is true
		s = shape{"class": "STAR", "type": "STAR", "expr": fn("list_filter", "", false, shape{"class": "STAR", "type": "STAR"}, m.expr(e.Arg))}
	} else {
		s = shape{"class": "STAR", "type": "STAR", "expr": m.expr(e.Arg)}
	}
	s["columns"] = true
	if e.Star {
		return opNode("OPERATOR_UNPACK", s)
	}
	return s
}

func isArrow(e duckdb.Expr) bool {
	b, ok := e.(*duckdb.Binary)
	return ok && b.Op.Text == "->"
}

// emptyList is the constant that DuckDB puts where a bound of a slice is left out.
func emptyList() shape {
	return shape{"class": "CONSTANT", "type": "VALUE_CONSTANT", "value": shape{
		"type": shape{"id": "LIST", "type_info": shape{"type": "LIST_TYPE_INFO", "child_type": shape{"id": "INTEGER"}}}}}
}

func (m *mapper) slice(x any, s *duckdb.SliceStep) any {
	bound := func(e duckdb.Expr) any {
		if e == nil {
			return emptyList()
		}
		return m.expr(e)
	}
	ch := []any{x, bound(s.Lo)}
	if s.Dash {
		ch = append(ch, emptyList())
	} else {
		ch = append(ch, bound(s.Hi))
	}
	if s.Stride != nil {
		ch = append(ch, m.expr(s.Stride))
	}
	return opNode("ARRAY_SLICE", ch...)
}

// ---- windows ----

var windowTypes = map[string]string{
	"row_number": "WINDOW_ROW_NUMBER", "rank": "WINDOW_RANK", "dense_rank": "WINDOW_RANK_DENSE", "percent_rank": "WINDOW_PERCENT_RANK",
	"cume_dist": "WINDOW_CUME_DIST", "ntile": "WINDOW_NTILE", "lead": "WINDOW_LEAD", "lag": "WINDOW_LAG",
	"first_value": "WINDOW_FIRST_VALUE", "first": "WINDOW_FIRST_VALUE", "last_value": "WINDOW_LAST_VALUE", "last": "WINDOW_LAST_VALUE",
	"nth_value": "WINDOW_NTH_VALUE", "fill": "WINDOW_FILL",
}

// resolveWindow returns the specification of a window, with what it takes from the window it is based on.
func (m *mapper) resolveWindow(spec *duckdb.WindowSpec, depth int) duckdb.WindowSpec {
	out := *spec
	if spec.Base == nil {
		return out
	}
	if depth > 8 {
		unsup("window cycle")
	}
	var base *duckdb.WindowSpec
	for i := len(m.windows) - 1; i >= 0 && base == nil; i-- {
		base = m.windows[i][spec.Base.Fold()]
	}
	if base == nil {
		unsup("unknown window")
	}
	b := m.resolveWindow(base, depth+1)
	out.Base = nil
	if len(out.PartitionBy) == 0 {
		out.PartitionBy = b.PartitionBy
	} else if len(b.PartitionBy) > 0 {
		unsup("window partition twice")
	}
	if out.OrderBy == nil {
		out.OrderBy = b.OrderBy
	} else if b.OrderBy != nil {
		unsup("window order twice")
	}
	if out.Frame == nil {
		out.Frame = b.Frame
	} else if b.Frame != nil {
		unsup("window frame twice")
	}
	return out
}

// windowCall maps a function call with an OVER clause.
func (m *mapper) windowCall(f *duckdb.FuncCall, name string, args []any) shape {
	var spec duckdb.WindowSpec
	switch {
	case f.Over.Name != nil:
		spec = m.resolveWindow(&duckdb.WindowSpec{Base: f.Over.Name}, 0)
	case f.Over.Spec != nil:
		spec = m.resolveWindow(f.Over.Spec, 0)
	}
	typ, ok := windowTypes[name]
	if !ok {
		typ = "WINDOW_AGGREGATE"
	}
	s := shape{"class": "WINDOW", "type": typ, "function_name": name, "distinct": strings.EqualFold(f.Quantifier, "distinct"),
		"start": "UNBOUNDED_PRECEDING", "end": "CURRENT_ROW_RANGE", "exclude_clause": "NO_OTHER"}
	if len(f.Name.Parts) > 1 {
		unsup("qualified window function")
	}
	if typ == "WINDOW_LEAD" || typ == "WINDOW_LAG" {
		if len(args) > 3 {
			unsup("lead/lag arguments")
		}
		if len(args) >= 2 {
			s["offset_expr"] = args[1]
		}
		if len(args) == 3 {
			s["default_expr"] = args[2]
		}
		if len(args) > 1 {
			args = args[:1]
		}
	}
	s["children"] = args
	if f.Filter != nil {
		s["filter_expr"] = m.expr(f.Filter)
	}
	if f.OrderBy != nil {
		s["arg_orders"] = m.orderModifier(f.OrderBy)["orders"]
	}
	if strings.EqualFold(strings.Join(strings.Fields(f.Nulls), " "), "ignore nulls") {
		s["ignore_nulls"] = true
	}
	if len(spec.PartitionBy) > 0 {
		s["partitions"] = m.exprs(spec.PartitionBy)
	}
	if spec.OrderBy != nil {
		s["orders"] = m.orderModifier(spec.OrderBy)["orders"]
	}
	if fr := spec.Frame; fr != nil {
		unit := strings.ToUpper(fr.Unit)
		bound := func(b *duckdb.FrameBound, key string) string {
			words := strings.Join(strings.Fields(strings.ToUpper(b.Kind)), "_")
			switch words {
			case "UNBOUNDED_PRECEDING", "UNBOUNDED_FOLLOWING":
				return words
			case "CURRENT_ROW":
				return "CURRENT_ROW_" + unit
			}
			if b.Expr == nil {
				unsup("frame bound")
			}
			s[key] = m.expr(b.Expr)
			return "EXPR_" + words + "_" + unit
		}
		s["start"] = bound(fr.Start, "start_expr")
		if fr.End != nil {
			s["end"] = bound(fr.End, "end_expr")
		} else {
			s["end"] = "CURRENT_ROW_" + unit
		}
		switch ex := strings.Join(strings.Fields(strings.ToUpper(fr.Exclude)), " "); ex {
		case "":
		case "EXCLUDE CURRENT ROW":
			s["exclude_clause"] = "CURRENT_ROW"
		case "EXCLUDE GROUP":
			s["exclude_clause"] = "GROUP"
		case "EXCLUDE TIES":
			s["exclude_clause"] = "TIES"
		case "EXCLUDE NO OTHERS":
		default:
			unsup("frame exclusion %s", ex)
		}
	}
	return s
}

// listComp maps [expr FOR x IN source IF cond].
func (m *mapper) listComp(e *duckdb.ListComp) any {
	var params []any
	for i := range e.Vars {
		params = append(params, shape{"class": "COLUMN_REF", "type": "COLUMN_REF", "column_names": []any{e.Vars[i].Name()}})
	}
	var lhs any = params[0]
	if len(params) > 1 {
		lhs = fn("row", "", false, params...)
	}
	lambda := func(lhs, body any) shape {
		return shape{"class": "LAMBDA", "type": "LAMBDA", "lhs": lhs, "expr": body}
	}
	src, body := m.expr(e.Source), m.expr(e.Expr)
	if e.Cond == nil {
		return fn("list_apply", "main", false, src, lambda(lhs, body))
	}
	alias := func(x any, name string) any {
		s, ok := x.(shape)
		if !ok {
			unsup("alias of %T", x)
		}
		s["alias"] = name
		return s
	}
	pack := fn("struct_pack", "main", false, alias(m.expr(e.Cond), "filter"), alias(body, "result"))
	elem := func() any { return shape{"class": "COLUMN_REF", "type": "COLUMN_REF", "column_names": []any{"elem"}} }
	pick := func(name string) shape {
		return lambda(elem(), fn("struct_extract", "main", false, elem(), varchar(name)))
	}
	filtered := fn("list_filter", "main", false, fn("list_apply", "main", false, src, lambda(lhs, pack)), pick("filter"))
	return fn("list_apply", "main", false, filtered, pick("result"))
}

// arraySubquery maps ARRAY(SELECT ...): the query is aggregated with array_agg.
func (m *mapper) arraySubquery(e *duckdb.ArraySubquery) any {
	if q, ok := e.Query.Query.(*duckdb.Select); !ok || q.OrderBy != nil {
		// DuckDB moves the ORDER BY into the aggregate, with a column of its own
		unsup("array subquery with order by")
	}
	inner := shape{"type": "SUBQUERY", "subquery": shape{"node": m.parenQuery(e.Query)}}
	agg := func() any {
		return fn("array_agg", "", false, shape{"class": "POSITIONAL_REFERENCE", "type": "POSITIONAL_REFERENCE", "index": 1})
	}
	pick := shape{"class": "CASE", "type": "CASE_EXPR", "case_checks": []any{shape{
		"when_expr": opNode("OPERATOR_IS_NULL", agg()), "then_expr": fn("list_value", "", false)}}, "else_expr": agg()}
	node := shape{"type": "SELECT_NODE", "aggregate_handling": "STANDARD_HANDLING", "from_table": inner, "select_list": []any{pick}}
	return shape{"class": "SUBQUERY", "type": "SUBQUERY", "subquery_type": "SCALAR", "comparison_type": "INVALID", "subquery": shape{"node": node}}
}
