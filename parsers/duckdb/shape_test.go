package duckdb_test

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/parsers/duckdb"
)

// This file checks the syntax tree against the AST of DuckDB. The reference data holds, for each SELECT statement
// of the corpora, what json_serialize_sql returns, reduced to a canonical form (internal/refgen/refgen.py: no
// locations, and no key whose value is null, empty or false). The functions below do what DuckDB's transformer
// does to a syntax tree of this package: they build the same JSON from it, in the same canonical form. The
// constructs that they do not map make the statement skip the comparison; the test reports how many were compared.

// shape is a JSON object of the AST of DuckDB.
type shape = map[string]any

// unsupported is the panic with which the mapping gives up on a construct.
type unsupported string

func unsup(format string, args ...any) { panic(unsupported(fmt.Sprintf(format, args...))) }

// reduce brings a tree to the canonical form of the reference data.
func reduce(v any) any {
	switch v := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, x := range v {
			if k == "query_location" {
				continue
			}
			x = reduce(x)
			if isEmpty(x) {
				continue
			}
			out[k] = x
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = reduce(x)
		}
		return out
	}
	return v
}

func isEmpty(v any) bool {
	switch v := v.(type) {
	case nil:
		return true
	case string:
		return v == ""
	case bool:
		return !v
	case []any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	}
	return false
}

type mapper struct {
	// windows are the WINDOW clauses of the SELECTs being mapped, the innermost last
	windows []map[string]*duckdb.WindowSpec
}

// ---- identifiers and names ----

func names(n *duckdb.Name) []string { return n.Names() }

func lower(s string) string { return strings.ToLower(s) }

// ---- constants ----

func constant(typ shape, value any) shape {
	v := shape{"type": typ}
	if value == nil {
		v["is_null"] = true
	} else {
		v["value"] = value
	}
	return shape{"class": "CONSTANT", "type": "VALUE_CONSTANT", "value": v}
}

func varchar(s string) shape { return constant(shape{"id": "VARCHAR"}, s) }

// hugeint is the value of a 128 bits integer in the AST of DuckDB: the two halves.
func hugeint(b *big.Int) shape {
	lo := new(big.Int).And(b, new(big.Int).SetUint64(^uint64(0)))
	hi := new(big.Int).Rsh(b, 64)
	return shape{"upper": json.Number(hi.String()), "lower": json.Number(lo.String())}
}

func (m *mapper) number(n *duckdb.Number, neg bool) shape {
	d := n.Digits()
	sign := ""
	if neg {
		sign = "-"
	}
	typ := n.Type()
	if n.IsInteger() {
		b, _ := new(big.Int).SetString(sign+d, 10)
		switch {
		case b.IsInt64() && b.Int64() >= -math.MaxInt32 && b.Int64() <= math.MaxInt32:
			typ = "INTEGER"
		case b.IsInt64():
			typ = "BIGINT"
		case b.BitLen() <= 127 || b.Cmp(new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 127))) == 0:
			typ = "HUGEINT"
		case b.Sign() > 0 && b.BitLen() <= 128:
			typ = "UHUGEINT"
		default:
			typ = "DOUBLE"
		}
	}
	switch typ {
	case "INTEGER":
		v, _ := strconv.ParseInt(sign+d, 10, 64)
		return constant(shape{"id": "INTEGER"}, v)
	case "BIGINT":
		v, _ := strconv.ParseInt(sign+d, 10, 64)
		return constant(shape{"id": "BIGINT"}, v)
	case "HUGEINT", "UHUGEINT":
		b, _ := new(big.Int).SetString(sign+d, 10)
		return constant(shape{"id": typ}, hugeint(b))
	case "DECIMAL":
		w, s := n.DecimalWidthScale()
		digits := strings.Replace(d, ".", "", 1)
		digits = strings.TrimLeft(digits, "0")
		if digits == "" {
			digits = "0"
		}
		b, _ := new(big.Int).SetString(sign+digits, 10)
		var v any = json.Number(b.String())
		if w > 18 {
			v = hugeint(b)
		}
		return constant(shape{"id": "DECIMAL", "type_info": shape{"type": "DECIMAL_TYPE_INFO", "width": w, "scale": s}}, v)
	}
	f, err := strconv.ParseFloat(sign+d, 64)
	if err != nil {
		unsup("double out of range")
	}
	return constant(shape{"id": "DOUBLE"}, f)
}

// ---- expressions ----

var comparisons = map[string]string{
	"=": "COMPARE_EQUAL", "==": "COMPARE_EQUAL", "<>": "COMPARE_NOTEQUAL", "!=": "COMPARE_NOTEQUAL", "<": "COMPARE_LESSTHAN", ">": "COMPARE_GREATERTHAN",
	"<=": "COMPARE_LESSTHANOREQUALTO", ">=": "COMPARE_GREATERTHANOREQUALTO",
}

func fn(name string, schema string, distinct bool, children ...any) shape {
	s := shape{"class": "FUNCTION", "type": "FUNCTION", "function_name": name, "schema": schema, "children": children,
		"order_bys": shape{"type": "ORDER_MODIFIER"}, "distinct": distinct}
	return s
}

func operator(name string, children ...any) shape {
	return fn(name, "", false, children...)
}

func opFn(name string, children ...any) shape {
	s := operator(name, children...)
	s["is_operator"] = true
	return s
}

func opNode(typ string, children ...any) shape {
	return shape{"class": "OPERATOR", "type": typ, "children": children}
}

func (m *mapper) exprs(es []duckdb.Expr) []any {
	out := make([]any, len(es))
	for i, e := range es {
		out[i] = m.expr(e)
	}
	return out
}

// negateTypes are the types of the comparisons and the IN operators that NOT turns into the opposite one.
var negateTypes = map[string]string{
	"COMPARE_EQUAL": "COMPARE_NOTEQUAL", "COMPARE_NOTEQUAL": "COMPARE_EQUAL",
	"COMPARE_LESSTHAN": "COMPARE_GREATERTHANOREQUALTO", "COMPARE_GREATERTHANOREQUALTO": "COMPARE_LESSTHAN",
	"COMPARE_GREATERTHAN": "COMPARE_LESSTHANOREQUALTO", "COMPARE_LESSTHANOREQUALTO": "COMPARE_GREATERTHAN",
	"COMPARE_DISTINCT_FROM": "COMPARE_NOT_DISTINCT_FROM", "COMPARE_NOT_DISTINCT_FROM": "COMPARE_DISTINCT_FROM",
	"COMPARE_IN": "COMPARE_NOT_IN", "COMPARE_NOT_IN": "COMPARE_IN",
}

// negate maps NOT x: a comparison is turned into the opposite one, and anything else is wrapped.
func negate(x any) any {
	if s, ok := x.(shape); ok {
		if opposite, ok := negateTypes[s["type"].(string)]; ok && (s["class"] == "COMPARISON" || s["class"] == "OPERATOR") {
			s["type"] = opposite
			return s
		}
	}
	return opNode("OPERATOR_NOT", x)
}

func (m *mapper) conjunction(typ string, l, r any) shape {
	var children []any
	for _, x := range []any{l, r} {
		if s, ok := x.(shape); ok && s["type"] == typ {
			children = append(children, s["children"].([]any)...)
		} else {
			children = append(children, x)
		}
	}
	return shape{"class": "CONJUNCTION", "type": typ, "children": children}
}

func (m *mapper) expr(e duckdb.Expr) any {
	switch e := e.(type) {
	case *duckdb.Number:
		return m.number(e, false)
	case *duckdb.StringLit:
		if e.Kind() == duckdb.UnicodeString {
			unsup("unicode string")
		}
		return varchar(e.Value())
	case *duckdb.BoolLit:
		v := "f"
		if strings.EqualFold(e.Text, "true") {
			v = "t"
		}
		return shape{"class": "CAST", "type": "OPERATOR_CAST", "child": varchar(v), "cast_type": shape{"id": "BOOLEAN"}}
	case *duckdb.NullLit:
		return constant(shape{"id": "NULL"}, nil)
	case *duckdb.Paren:
		return m.expr(e.X)
	case *duckdb.ColumnRef:
		if len(e.Parts) > 4 {
			unsup("long column reference")
		}
		cols := make([]any, len(e.Parts))
		for i := range e.Parts {
			cols[i] = e.Parts[i].Name()
		}
		return shape{"class": "COLUMN_REF", "type": "COLUMN_REF", "column_names": cols}
	case *duckdb.Star:
		s := shape{"class": "STAR", "type": "STAR"}
		if e.Qualifier != nil {
			s["relation_name"] = e.Qualifier.Name()
		}
		if len(e.Replace) > 0 {
			s["replace_list"] = m.replaceList(e.Replace)
		}
		if len(e.Rename) > 0 {
			s["rename_list"] = m.renameList(e.Rename)
		}
		var ex []any
		var qex []any
		for _, n := range e.Exclude {
			switch ps := n.Names(); len(ps) {
			case 1:
				ex = append(ex, ps[0])
			case 2:
				qex = append(qex, shape{"table": ps[0], "column": ps[1]})
			default:
				unsup("qualified exclude")
			}
		}
		s["exclude_list"] = ex
		s["qualified_exclude_list"] = qex
		return s
	case *duckdb.Binary:
		return m.binary(e)
	case *duckdb.Unary:
		if e.Op.Text == "-" {
			if n, ok := e.X.(*duckdb.Number); ok {
				return m.number(n, true)
			}
		}
		return opFn(e.Op.Text, m.expr(e.X))
	case *duckdb.PostfixOp:
		return opFn(e.Op.Text+"__postfix", m.expr(e.X))
	case *duckdb.Not:
		return negate(m.expr(e.X))
	case *duckdb.IsExpr:
		x := m.expr(e.X)
		switch lower(e.Test) {
		case "null", "isnull", "notnull", "unknown":
			if e.Negated {
				return opNode("OPERATOR_IS_NOT_NULL", x)
			}
			return opNode("OPERATOR_IS_NULL", x)
		case "true", "false":
			cast := shape{"class": "CAST", "type": "OPERATOR_CAST", "child": x, "cast_type": shape{"id": "BOOLEAN"}}
			var rhs shape
			if lower(e.Test) == "true" {
				rhs = constant(shape{"id": "BOOLEAN"}, true)
			} else {
				rhs = constant(shape{"id": "BOOLEAN"}, false)
			}
			typ := "COMPARE_NOT_DISTINCT_FROM"
			if e.Negated {
				typ = "COMPARE_DISTINCT_FROM"
			}
			return shape{"class": "COMPARISON", "type": typ, "left": cast, "right": rhs}
		}
		unsup("IS %s", e.Test)
	case *duckdb.DistinctFrom:
		typ := "COMPARE_DISTINCT_FROM"
		if e.Negated {
			typ = "COMPARE_NOT_DISTINCT_FROM"
		}
		return shape{"class": "COMPARISON", "type": typ, "left": m.expr(e.Left), "right": m.expr(e.Right)}
	case *duckdb.Between:
		if e.Symmetric {
			unsup("symmetric")
		}
		b := shape{"class": "BETWEEN", "type": "COMPARE_BETWEEN", "input": m.expr(e.X), "lower": m.expr(e.Lo), "upper": m.expr(e.Hi)}
		if e.Negated {
			return opNode("OPERATOR_NOT", b)
		}
		return b
	case *duckdb.In:
		if e.Query != nil {
			s := m.subquery("ANY", e.Query)
			s["child"] = m.expr(e.X)
			s["comparison_type"] = "COMPARE_EQUAL"
			if e.Negated {
				return opNode("OPERATOR_NOT", s)
			}
			return s
		}
		if e.Source != nil {
			// x IN y: the expression y is a list or a string to search
			c := fn("contains", "", false, m.expr(e.Source), m.expr(e.X))
			if e.Negated {
				return opNode("OPERATOR_NOT", c)
			}
			return c
		}
		typ := "COMPARE_IN"
		if e.Negated {
			typ = "COMPARE_NOT_IN"
		}
		return opNode(typ, append([]any{m.expr(e.X)}, m.exprs(e.Items)...)...)
	case *duckdb.Like:
		return m.like(e)
	case *duckdb.Cast:
		child := m.expr(e.X)
		s := shape{"class": "CAST", "type": "OPERATOR_CAST", "child": child, "cast_type": m.typ(e.Type), "try_cast": e.Try}
		return s
	case *duckdb.TypedLit:
		return shape{"class": "CAST", "type": "OPERATOR_CAST", "child": varchar(e.Value.Value()), "cast_type": m.typ(e.Type)}
	case *duckdb.Case:
		var checks []any
		for _, w := range e.Whens {
			cond := m.expr(w.Cond)
			if e.Arg != nil {
				cond = shape{"class": "COMPARISON", "type": "COMPARE_EQUAL", "left": m.expr(e.Arg), "right": cond}
			}
			checks = append(checks, shape{"when_expr": cond, "then_expr": m.expr(w.Then)})
		}
		var els any = constant(shape{"id": "NULL"}, nil)
		if e.Else != nil {
			els = m.expr(e.Else)
		}
		return shape{"class": "CASE", "type": "CASE_EXPR", "case_checks": checks, "else_expr": els}
	case *duckdb.Row:
		return fn("row", "main", false, m.exprs(e.Items)...)
	case *duckdb.ListLit:
		return fn("list_value", "main", false, m.exprs(e.Items)...)
	case *duckdb.ArrayLit:
		return opNode("ARRAY_CONSTRUCTOR", m.exprs(e.Items)...)
	case *duckdb.StructLit:
		var ch []any
		for _, f := range e.Fields {
			c := m.expr(f.Value).(shape)
			if _, has := c["alias"]; has {
				unsup("aliased struct value")
			}
			c["alias"] = f.Name.Name()
			ch = append(ch, c)
		}
		return fn("struct_pack", "main", false, ch...)
	case *duckdb.MapLit:
		var ks, vs []any
		for _, en := range e.Entries {
			ks = append(ks, m.expr(en.Key))
			vs = append(vs, m.expr(en.Value))
		}
		return fn("map", "main", false, fn("list_value", "main", false, ks...), fn("list_value", "main", false, vs...))
	case *duckdb.Subquery:
		return m.subquery("SCALAR", e.Query)
	case *duckdb.Exists:
		return m.subquery("EXISTS", e.Query)
	case *duckdb.FuncCall:
		return m.funcCall(e)
	case *duckdb.SpecialCall:
		return m.special(e)
	case *duckdb.Param:
		if e.Positional() {
			unsup("positional parameter")
		}
		if i, ok := e.Index(); ok {
			return shape{"class": "PARAMETER", "type": "VALUE_PARAMETER", "identifier": strconv.Itoa(i)}
		}
		if n, ok := e.ParamName(); ok {
			return shape{"class": "PARAMETER", "type": "VALUE_PARAMETER", "identifier": n}
		}
	case *duckdb.PositionalRef:
		i, _ := e.Index.Int64()
		return shape{"class": "POSITIONAL_REFERENCE", "type": "POSITIONAL_REFERENCE", "index": i}
	case *duckdb.Collate:
		return shape{"class": "COLLATE", "type": "COLLATE", "child": m.expr(e.X), "collation": strings.Join(e.Collation.Names(), ".")}
	case *duckdb.Indirection:
		return m.indirection(e)
	case *duckdb.Lambda:
		return m.lambda(e)
	case *duckdb.ListComp:
		return m.listComp(e)
	case *duckdb.ArraySubquery:
		return m.arraySubquery(e)
	case *duckdb.Grouping:
		if lower(e.Name) != "grouping" {
			unsup("GROUPING_ID")
		}
		return opNode("GROUPING_FUNCTION", m.exprs(e.Args)...)
	case *duckdb.IntervalLit:
		return m.interval(e)
	case *duckdb.AnyAll:
		return m.anyAll(e)
	case *duckdb.ColumnsExpr:
		return m.columns(e)
	case *duckdb.UnpackExpr:
		return opNode("OPERATOR_UNPACK", m.expr(e.Arg))
	case *duckdb.AtTimeZone:
		return fn("timezone", "main", false, m.expr(e.Zone), m.expr(e.X))
	}
	unsup("%T", e)
	return nil
}

func (m *mapper) indirection(e *duckdb.Indirection) any {
	x := m.expr(e.X)
	switch s := e.Step.(type) {
	case *duckdb.IndexStep:
		return opNode("ARRAY_EXTRACT", x, m.expr(s.Index))
	case *duckdb.SliceStep:
		return m.slice(x, s)
	case *duckdb.FieldStep:
		if s.Call {
			// x.f(args) is f(x, args)
			args := append([]any{x}, m.exprs(s.Args)...)
			return fn(lower(s.Name.Name()), "", false, args...)
		}
		// a column reference of the grammar goes on with its field names; a parenthesized one does not
		if _, plain := e.X.(*duckdb.ColumnRef); plain {
			if cr, ok := x.(shape); ok && cr["type"] == "COLUMN_REF" {
				cols := append(append([]any(nil), cr["column_names"].([]any)...), s.Name.Name())
				return shape{"class": "COLUMN_REF", "type": "COLUMN_REF", "column_names": cols}
			}
		}
		return opNode("STRUCT_EXTRACT", x, varchar(s.Name.Name()))
	}
	unsup("indirection %T", e.Step)
	return nil
}

func (m *mapper) subquery(kind string, q *duckdb.ParenQuery) shape {
	return shape{"class": "SUBQUERY", "type": "SUBQUERY", "subquery_type": kind, "subquery": shape{"node": m.parenQuery(q)}, "comparison_type": "INVALID"}
}

func (m *mapper) binary(e *duckdb.Binary) any {
	op := e.Op.Text
	switch lower(op) {
	case "and", "or":
		typ := "CONJUNCTION_" + strings.ToUpper(op)
		return m.conjunction(typ, m.expr(e.Left), m.expr(e.Right))
	case "->":
		// DuckDB reads every -> as a lambda; the binder decides later whether it is the JSON operator
		return shape{"class": "LAMBDA", "type": "LAMBDA", "lhs": m.expr(e.Left), "expr": m.expr(e.Right)}
	}
	if typ, ok := comparisons[op]; ok {
		return shape{"class": "COMPARISON", "type": typ, "left": m.expr(e.Left), "right": m.expr(e.Right)}
	}
	switch op {
	case "~":
		return fn("regexp_full_match", "", false, m.expr(e.Left), m.expr(e.Right))
	case "!~":
		return opNode("OPERATOR_NOT", fn("regexp_full_match", "", false, m.expr(e.Left), m.expr(e.Right)))
	}
	l, r := m.expr(e.Left), m.expr(e.Right)
	return opFn(op, l, r)
}

func (m *mapper) like(e *duckdb.Like) any {
	op := lower(strings.Join(strings.Fields(e.Op), " "))
	var name string
	switch op {
	case "like":
		name = "~~"
	case "ilike":
		name = "~~*"
	case "glob":
		name = "~~~"
	case "similar to":
		if e.Escape != nil {
			unsup("similar to escape")
		}
		var c any = fn("regexp_full_match", "", false, m.expr(e.X), m.expr(e.Pattern))
		if e.Negated {
			c = opNode("OPERATOR_NOT", c)
		}
		return c
	}
	x, p := m.expr(e.X), m.expr(e.Pattern)
	if e.Escape != nil {
		esc := m.expr(e.Escape)
		fnName := map[string]string{"like": "like_escape", "ilike": "ilike_escape"}[op]
		if fnName == "" {
			unsup("glob escape")
		}
		if e.Negated {
			fnName = "not_" + fnName
		}
		return fn(fnName, "main", false, x, p, esc)
	}
	if e.Negated {
		if op == "glob" {
			unsup("not glob")
		}
		name = "!" + name
	}
	return opFn(name, x, p)
}

func (m *mapper) funcCall(f *duckdb.FuncCall) any {
	if f.Variadic != nil || f.WithinGroup != nil || f.ExportState {
		unsup("function clause")
	}
	parts := f.Name.Names()
	if len(parts) > 3 {
		unsup("function name")
	}
	name := lower(parts[len(parts)-1])
	written := name
	schema, catalog := "", ""
	if len(parts) >= 2 {
		schema = parts[len(parts)-2]
	}
	if len(parts) == 3 {
		catalog = parts[0]
	}
	var args []any
	for _, a := range f.Args {
		if na, named := a.(*duckdb.NamedArg); named {
			v, ok := m.expr(na.Value).(shape)
			if !ok {
				unsup("named argument")
			}
			if _, has := v["alias"]; has {
				unsup("aliased named argument")
			}
			v["alias"] = na.Name.Name()
			args = append(args, v)
			continue
		}
		args = append(args, m.expr(a))
	}
	if len(f.Args) == 1 {
		// f(*) takes no argument, and count(*) is count_star
		if st, ok := f.Args[0].(*duckdb.Star); ok && st.Qualifier == nil && len(st.Exclude) == 0 && len(st.Replace) == 0 && len(st.Rename) == 0 &&
			!strings.EqualFold(f.Quantifier, "distinct") {
			args = nil
			if name == "count" && f.Quantifier == "" {
				name = "count_star"
			}
		}
	}
	if len(f.Args) == 0 && name == "count" && schema == "" && f.Quantifier == "" {
		name = "count_star"
	}
	if f.Over == nil && schema == "" {
		switch {
		case name == "if" && len(args) == 3:
			return shape{"class": "CASE", "type": "CASE_EXPR", "case_checks": []any{shape{"when_expr": args[0], "then_expr": args[1]}}, "else_expr": args[2]}
		case name == "ifnull" && len(args) == 2:
			return opNode("OPERATOR_COALESCE", args...)
		case name == "try" && len(args) == 1:
			return opNode("OPERATOR_TRY", args[0])
		}
	}
	if f.Over != nil {
		// the window of count(*) is count
		if written == "count" {
			name = "count"
		}
		return m.windowCall(f, name, args)
	}
	if f.Nulls != "" {
		unsup("function nulls clause")
	}
	if name == "date" && schema == "" {
		unsup("function named like a type")
	}
	if f.OrderBy != nil && (name == "list" || name == "array_agg") {
		unsup("ordered list aggregate")
	}
	s := fn(name, schema, strings.EqualFold(f.Quantifier, "distinct"), args...)
	s["catalog"] = catalog
	if f.Filter != nil {
		s["filter"] = m.expr(f.Filter)
	}
	if f.OrderBy != nil {
		s["order_bys"] = m.orderModifier(f.OrderBy)
	}
	return s
}

// extractParts are the plural names of the parts of EXTRACT.
var extractParts = map[string]string{
	"years": "year", "months": "month", "days": "day", "hours": "hour", "minutes": "minute", "seconds": "second",
	"milliseconds": "millisecond", "microseconds": "microsecond", "weeks": "week", "decades": "decade", "centuries": "century",
	"millennia": "millennium",
}

func (m *mapper) special(e *duckdb.SpecialCall) any {
	switch lower(e.Name) {
	case "substring":
		ch := m.exprs(e.Args)
		switch {
		case len(e.From) > 0:
			ch = append(ch, m.exprs(e.From)...)
			if e.For != nil {
				ch = append(ch, m.expr(e.For))
			}
		case e.For != nil:
			ch = append(ch, constant(shape{"id": "INTEGER"}, int64(1)), castTo(m.expr(e.For), "INTEGER"))
		}
		return fn("substring", "main", false, ch...)
	case "trim":
		name := "trim"
		switch lower(e.Spec) {
		case "leading":
			name = "ltrim"
		case "trailing":
			name = "rtrim"
		}
		ch := m.exprs(e.Args)
		if e.Chars != nil {
			ch = append(ch, m.expr(e.Chars))
		}
		return fn(name, "main", false, ch...)
	case "coalesce":
		return opNode("OPERATOR_COALESCE", m.exprs(e.Args)...)
	case "nullif":
		return fn("nullif", "", false, m.exprs(e.Args)...)
	case "extract":
		if len(e.Args) != 1 || e.Spec == "" {
			unsup("extract")
		}
		// a keyword is lower case, a name is as written
		part := extractField(e.Spec)
		if _, isKeyword := duckdb.Keyword(part); isKeyword && !strings.HasPrefix(e.Spec, "'") {
			part = lower(part)
			if singular, ok := extractParts[part]; ok {
				part = singular
			}
		}
		return fn("date_part", "main", false, varchar(part), m.expr(e.Args[0]))
	}
	unsup("special %s", e.Name)
	return nil
}

// ---- queries ----

func (m *mapper) orderModifier(o *duckdb.OrderBy) shape {
	var orders []any
	if o.All {
		star := shape{"class": "STAR", "type": "STAR", "columns": true}
		orders = append(orders, shape{"type": orderType(o.Direction), "null_order": nullOrder(o.Nulls), "expression": star})
		return shape{"type": "ORDER_MODIFIER", "orders": orders}
	}
	for _, it := range o.Items {
		if it.Using != "" {
			unsup("order by using")
		}
		orders = append(orders, shape{"type": orderType(it.Direction), "null_order": nullOrder(it.Nulls), "expression": m.expr(it.Expr)})
	}
	return shape{"type": "ORDER_MODIFIER", "orders": orders}
}

func orderType(direction string) string {
	switch lower(direction) {
	case "asc", "ascending":
		return "ASCENDING"
	case "desc", "descending":
		return "DESCENDING"
	}
	return "ORDER_DEFAULT"
}

func nullOrder(nulls string) string {
	if f := strings.Fields(nulls); len(f) == 2 {
		return "NULLS " + strings.ToUpper(f[1])
	}
	return "ORDER_DEFAULT"
}

func (m *mapper) parenQuery(q *duckdb.ParenQuery) any {
	switch c := q.Query.(type) {
	case *duckdb.Select:
		return m.selectNode(c)
	case *duckdb.ParenQuery:
		return m.parenQuery(c)
	}
	unsup("query in parentheses")
	return nil
}

// selectNode maps a Select: its body, WITH, ORDER BY and LIMIT.
func (m *mapper) selectNode(s *duckdb.Select) shape {
	if len(s.Locking) > 0 {
		unsup("locking")
	}
	if c, ok := s.Body.(*duckdb.SelectCore); ok {
		defer m.pushWindows(c)()
	}
	var node shape
	switch b := s.Body.(type) {
	case *duckdb.ParenQuery:
		inner := m.parenQuery(b).(shape)
		node = inner
	default:
		node = m.body(s.Body)
	}
	mods, _ := node["modifiers"].([]any)
	if node["modifiers"] != nil && s.OrderBy == nil && s.Limit == nil && s.Offset == nil && s.With == nil {
		return node
	}
	if s.OrderBy != nil {
		mods = append(mods, m.orderModifier(s.OrderBy))
	}
	if s.Limit != nil || s.Offset != nil {
		mods = append(mods, m.limit(s.Limit, s.Offset))
	}
	if len(mods) > 0 {
		_, paren := s.Body.(*duckdb.ParenQuery)
		if inner, _ := node["modifiers"].([]any); paren && len(inner) > 0 && (s.OrderBy != nil || s.Limit != nil || s.Offset != nil) {
			unsup("modifiers of a parenthesized query")
		}
		node["modifiers"] = mods
	}
	if s.With != nil {
		node["cte_map"] = m.cteMap(s.With)
	}
	return node
}

func (m *mapper) limit(l *duckdb.LimitClause, o *duckdb.OffsetClause) shape {
	mod := shape{"type": "LIMIT_MODIFIER"}
	if l != nil {
		if l.All || l.Offset != nil || strings.EqualFold(l.Keyword, "fetch") {
			unsup("limit form")
		}
		if l.Value != nil {
			mod["limit"] = m.expr(l.Value)
		}
		if l.Percent != "" {
			mod["type"] = "LIMIT_PERCENT_MODIFIER"
		}
	}
	if o != nil {
		if o.Rows != "" {
			unsup("offset rows")
		}
		mod["offset"] = m.expr(o.Value)
	}
	return mod
}

func (m *mapper) cteMap(w *duckdb.With) shape {
	var entries []any
	for _, c := range w.CTEs {
		q, ok := c.Query.(*duckdb.Select)
		if !ok {
			unsup("cte statement")
		}
		var aliases []any
		for _, a := range c.Columns {
			aliases = append(aliases, a.Name())
		}
		var keys []any
		for _, k := range c.UsingKey {
			keys = append(keys, shape{"class": "COLUMN_REF", "type": "COLUMN_REF", "column_names": []any{k.Name()}})
		}
		node := m.cteQuery(w.Recursive, c, q, aliases)
		// json_serialize_sql reports the default for MATERIALIZED and NOT MATERIALIZED as well
		entries = append(entries, shape{"key": c.Name.Name(), "value": shape{"aliases": aliases, "key_targets": keys,
			"query": shape{"node": node}, "materialized": "CTE_MATERIALIZE_DEFAULT"}})
	}
	return shape{"map": entries}
}

func (m *mapper) body(b duckdb.QueryBody) shape {
	switch b := b.(type) {
	case *duckdb.SelectCore:
		return m.core(b)
	case *duckdb.ValuesClause:
		return shape{"type": "SELECT_NODE", "select_list": []any{shape{"class": "STAR", "type": "STAR"}}, "from_table": m.values(b, "valueslist"), "aggregate_handling": "STANDARD_HANDLING"}
	case *duckdb.TableQuery:
		return shape{"type": "SELECT_NODE", "select_list": []any{shape{"class": "STAR", "type": "STAR"}}, "from_table": m.baseTable(&duckdb.BaseTable{Name: b.Table}), "aggregate_handling": "STANDARD_HANDLING"}
	case *duckdb.SetOp:
		if lower(b.Op) == "union" {
			// DuckDB gathers the operands of a chain of the same UNION, parenthesized or not, and writes them as a
			// tree: the first operand, and the others paired from the left.
			var leaves []duckdb.QueryBody
			m.unionLeaves(b, b, &leaves)
			nodes := make([]any, len(leaves))
			for i, l := range leaves {
				nodes[i] = m.setopSide(l)
			}
			rest := nodes[1:]
			for len(rest) > 1 {
				var next []any
				for i := 0; i < len(rest); i += 2 {
					if i+1 < len(rest) {
						next = append(next, m.unionNode(b, rest[i], rest[i+1]))
					} else {
						next = append(next, rest[i])
					}
				}
				rest = next
			}
			return m.unionNode(b, nodes[0], rest[0])
		}
		typ := strings.ToUpper(b.Op)
		s := shape{"type": "SET_OPERATION_NODE", "setop_type": typ, "left": m.setopSide(b.Left), "right": m.setopSide(b.Right)}
		if b.ByName {
			s["setop_type"] = typ + "_BY_NAME"
		}
		if lower(b.Quantifier) != "distinct" && b.Quantifier != "" {
			s["setop_all"] = true
		}
		return s
	}
	unsup("body %T", b)
	return nil
}

func (m *mapper) unionNode(b *duckdb.SetOp, left, right any) shape {
	s := shape{"type": "SET_OPERATION_NODE", "setop_type": "UNION", "left": left, "right": right}
	if b.ByName {
		s["setop_type"] = "UNION_BY_NAME"
	}
	if lower(b.Quantifier) != "distinct" && b.Quantifier != "" {
		s["setop_all"] = true
	}
	return s
}

// unionLeaves collects the operands of the UNION chain that starts at op, those that are not UNIONs of the kind of like.
func (m *mapper) unionLeaves(op, like *duckdb.SetOp, out *[]duckdb.QueryBody) {
	for _, side := range []duckdb.QueryBody{op.Left, op.Right} {
		inner := side
		for {
			p, ok := inner.(*duckdb.ParenQuery)
			if !ok {
				break
			}
			switch q := p.Query.(type) {
			case *duckdb.Select:
				if q.With != nil || q.OrderBy != nil || q.Limit != nil || q.Offset != nil || len(q.Locking) > 0 {
					inner = nil
				} else {
					inner = q.Body
				}
			case *duckdb.ParenQuery:
				inner = q
			default:
				inner = nil
			}
			if inner == nil {
				break
			}
		}
		if s, ok := inner.(*duckdb.SetOp); ok && lower(s.Op) == "union" && lower(s.Quantifier) == lower(like.Quantifier) && s.ByName == like.ByName {
			m.unionLeaves(s, like, out)
		} else {
			*out = append(*out, side)
		}
	}
}

func (m *mapper) setopSide(b duckdb.QueryBody) any {
	if p, ok := b.(*duckdb.ParenQuery); ok {
		return m.parenQuery(p)
	}
	return m.body(b)
}

func (m *mapper) values(v *duckdb.ValuesClause, alias string) shape {
	var rows []any
	for _, r := range v.Rows {
		rows = append(rows, m.exprs(r.Items))
	}
	return shape{"type": "EXPRESSION_LIST", "alias": alias, "values": rows}
}

// pushWindows makes the WINDOW clause of a SELECT visible to the OVER clauses, and returns what undoes it.
func (m *mapper) pushWindows(c *duckdb.SelectCore) func() {
	w := map[string]*duckdb.WindowSpec{}
	for i := range c.Windows {
		w[c.Windows[i].Name.Fold()] = c.Windows[i].Spec
	}
	m.windows = append(m.windows, w)
	n := len(m.windows)
	return func() { m.windows = m.windows[:n-1] }
}

func (m *mapper) core(c *duckdb.SelectCore) shape {
	if c.Into != nil || c.Sample != nil {
		unsup("select clause")
	}
	defer m.pushWindows(c)()
	node := shape{"type": "SELECT_NODE"}
	var mods []any
	if c.Distinct != nil {
		d := shape{"type": "DISTINCT_MODIFIER"}
		if len(c.Distinct.On) > 0 {
			d["distinct_on_targets"] = m.exprs(c.Distinct.On)
		}
		mods = append(mods, d)
	}
	node["modifiers"] = mods
	var items []any
	for _, it := range c.Items {
		items = append(items, m.item(it))
	}
	if len(items) == 0 {
		if c.FromFirst {
			items = []any{shape{"class": "STAR", "type": "STAR"}}
		} else {
			unsup("empty select list")
		}
	}
	node["select_list"] = items
	node["from_table"] = m.from(c.From)
	if c.Where != nil {
		node["where_clause"] = m.expr(c.Where)
	}
	node["aggregate_handling"] = "STANDARD_HANDLING"
	if c.GroupBy != nil {
		if _, star := firstOf(c.GroupBy.Items).(*duckdb.Star); c.GroupBy.All || star && len(c.GroupBy.Items) == 1 {
			node["aggregate_handling"] = "FORCE_AGGREGATES"
		} else {
			node["group_expressions"], node["group_sets"] = m.groupBy(c.GroupBy.Items)
		}
	}
	if c.Having != nil {
		node["having"] = m.expr(c.Having)
	}
	if c.Qualify != nil {
		node["qualify"] = m.expr(c.Qualify)
	}
	return node
}

func firstOf(es []duckdb.Expr) duckdb.Expr {
	if len(es) == 0 {
		return nil
	}
	return es[0]
}

func (m *mapper) item(it *duckdb.SelectItem) any {
	e := m.expr(it.Expr)
	if it.Alias != nil {
		s, ok := e.(shape)
		if !ok {
			unsup("alias of %T", e)
		}
		s["alias"] = it.Alias.Name()
	}
	return e
}

func (m *mapper) from(refs []duckdb.TableRef) shape {
	if len(refs) == 0 {
		return shape{"type": "EMPTY"}
	}
	t := m.tableRef(refs[0])
	for _, r := range refs[1:] {
		t = shape{"type": "JOIN", "left": t, "right": m.tableRef(r), "join_type": "INNER", "ref_type": "CROSS"}
	}
	return t
}

func (m *mapper) alias(s shape, a *duckdb.TableAlias) {
	if a == nil {
		return
	}
	if a.Name == nil || len(a.ColDefs) > 0 {
		unsup("alias form")
	}
	s["alias"] = a.Name.Name()
	var cols []any
	for _, c := range a.Columns {
		cols = append(cols, c.Name())
	}
	s["column_name_alias"] = cols
}

func (m *mapper) baseTable(t *duckdb.BaseTable) shape {
	if t.Only || t.Star || t.At != nil || t.Sample != nil || t.Prefix != nil {
		unsup("table form")
	}
	p := t.Name.Names()
	if len(p) > 3 {
		unsup("table name")
	}
	s := shape{"type": "BASE_TABLE", "table_name": p[len(p)-1]}
	if len(p) >= 2 {
		s["schema_name"] = p[len(p)-2]
	}
	if len(p) == 3 {
		s["catalog_name"] = p[0]
	}
	m.alias(s, t.Alias)
	return s
}

func (m *mapper) tableRef(r duckdb.TableRef) shape {
	switch r := r.(type) {
	case *duckdb.BaseTable:
		return m.baseTable(r)
	case *duckdb.SubqueryRef:
		if r.Lateral || r.Sample != nil || r.Prefix != nil {
			unsup("subquery form")
		}
		s := shape{"type": "SUBQUERY", "subquery": shape{"node": m.parenQuery(r.Query)}}
		m.alias(s, r.Alias)
		return s
	case *duckdb.FuncTable:
		if r.Lateral || r.Sample != nil || r.Prefix != nil || len(r.RowsFrom) > 0 {
			unsup("function table form")
		}
		f, ok := r.Func.(*duckdb.FuncCall)
		if !ok {
			unsup("function table")
		}
		call := m.funcCall(f).(shape)
		if f.Over != nil {
			unsup("window")
		}
		s := shape{"type": "TABLE_FUNCTION", "function": call, "with_ordinality": "WITHOUT_ORDINALITY"}
		if r.Ordinality {
			s["with_ordinality"] = "WITH_ORDINALITY"
		}
		m.alias(s, r.Alias)
		return s
	case *duckdb.ValuesRef:
		if r.Sample != nil || r.Prefix != nil {
			unsup("values form")
		}
		s := shape{"type": "SUBQUERY", "subquery": shape{"node": shape{"type": "SELECT_NODE", "aggregate_handling": "STANDARD_HANDLING",
			"select_list": []any{shape{"class": "STAR", "type": "STAR"}}, "from_table": m.values(r.Values, "valueslist")}}}
		m.alias(s, r.Alias)
		return s
	case *duckdb.ParenTable:
		if r.Alias != nil || r.Prefix != nil {
			unsup("parenthesized table alias")
		}
		return m.tableRef(r.Table)
	case *duckdb.Join:
		return m.join(r)
	}
	unsup("table %T", r)
	return nil
}

func (m *mapper) join(j *duckdb.Join) shape {
	s := shape{"type": "JOIN", "left": m.tableRef(j.Left), "right": m.tableRef(j.Right), "join_type": "INNER", "ref_type": "REGULAR"}
	words := strings.Fields(lower(j.Kind))
	for _, w := range words {
		switch w {
		case "cross":
			s["ref_type"] = "CROSS"
		case "natural":
			s["ref_type"] = "NATURAL"
		case "asof":
			s["ref_type"] = "ASOF"
		case "positional":
			s["ref_type"] = "POSITIONAL"
		case "left":
			s["join_type"] = "LEFT"
		case "right":
			s["join_type"] = "RIGHT"
		case "full":
			s["join_type"] = "FULL"
		case "semi":
			s["join_type"] = "SEMI"
		case "anti":
			s["join_type"] = "ANTI"
		case "inner", "outer":
		}
	}
	if j.On != nil {
		s["condition"] = m.expr(j.On)
	}
	if len(j.Using) > 0 {
		var us []any
		for _, u := range j.Using {
			us = append(us, u.Name())
		}
		s["using_columns"] = us
	}
	return s
}

func matText(m *duckdb.Match) string {
	if m == nil {
		return ""
	}
	return strings.Join(strings.Fields(m.Text), " ")
}

// mapStatement returns the AST DuckDB gives a SELECT statement, or false with the reason the mapping gives up.
func mapStatement(st duckdb.Statement) (out any, reason string) {
	defer func() {
		if r := recover(); r != nil {
			if u, ok := r.(unsupported); ok {
				out, reason = nil, string(u)
				return
			}
			panic(r)
		}
	}()
	sel, ok := st.(*duckdb.Select)
	if !ok {
		return nil, "not a query"
	}
	var m mapper
	return reduce(m.selectNode(sel)), ""
}

// jsonNormal converts a reduced tree to what json.Unmarshal into an any gives, to compare it with the reference.
func jsonNormal(t testing.TB, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.UseNumber()
	if err := d.Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// sameNumber compares two numbers of the JSON by value (10 and 10.0 are the same).
func sameNumber(a, b json.Number) bool {
	if a == b {
		return true
	}
	x, ok1 := new(big.Rat).SetString(string(a))
	y, ok2 := new(big.Rat).SetString(string(b))
	return ok1 && ok2 && x.Cmp(y) == 0
}

func sortedJSON(l []any) []any {
	out := append([]any(nil), l...)
	key := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	sort.Slice(out, func(i, j int) bool { return key(out[i]) < key(out[j]) })
	return out
}

// firstDifference returns the path of the first difference of two trees.
func firstDifference(a, b any, path string) string {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok {
			return path
		}
		keys := map[string]bool{}
		for k := range x {
			keys[k] = true
		}
		for k := range y {
			keys[k] = true
		}
		var ks []string
		for k := range keys {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		for _, k := range ks {
			if d := firstDifference(x[k], y[k], path+"."+k); d != "" {
				return d
			}
		}
		return ""
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return path + "[len]"
		}
		if strings.HasSuffix(path, "exclude_list") || strings.HasSuffix(path, "replace_list") || strings.HasSuffix(path, "rename_list") {
			// DuckDB keeps these in a hash set: the order is not the one of the query
			x, y = sortedJSON(x), sortedJSON(y)
		}
		for i := range x {
			if d := firstDifference(x[i], y[i], path+"["+strconv.Itoa(i)+"]"); d != "" {
				return d
			}
		}
		return ""
	}
	if x, ok := a.(json.Number); ok {
		if y, ok := b.(json.Number); ok && sameNumber(x, y) {
			return ""
		}
	}
	if !reflect.DeepEqual(a, b) {
		return path
	}
	return ""
}

// TestReferenceShapes compares the ASTs of the SELECT statements with those of DuckDB.
func TestReferenceShapes(t *testing.T) {
	// the differences that are known (see the README): the aliases of a recursive CTE written with a parenthesized WITH, and
	// a NOT before a NOT BETWEEN; the SELECT statements that the mapping covers are most of them
	for _, tc := range []struct {
		name        string
		maxDiff     int
		minCompared int
	}{{"exprs", 0, 6000}, {"tests", 2, 27000}} {
		t.Run(tc.name, func(t *testing.T) {
			cases := sample(loadRef(t, tc.name), 4)
			compared, same := checkShapes(t, cases)
			scale := 1
			if testing.Short() {
				scale = 4
			}
			if compared < tc.minCompared/scale*9/10 {
				t.Errorf("%d statements compared, the mapping covered about %d before", compared, tc.minCompared/scale)
			}
			if compared-same > tc.maxDiff {
				t.Errorf("%d statements differ from DuckDB's AST, %d are known", compared-same, tc.maxDiff)
			}
		})
	}
}

// checkShapes compares the AST that the mapping builds for each SELECT statement of the cases with the one of DuckDB,
// logs what it finds, and returns the number of statements compared and of those that are identical.
func checkShapes(t *testing.T, cases []refCase) (compared, same int) {
	t.Helper()
	var skipped int
	reasons := map[string]int{}
	reasonExample := map[string]string{}
	diffs := map[string]int{}
	example := map[string]string{}
	for _, c := range cases {
		if !c.OK || len(c.Shapes) == 0 {
			continue
		}
		sts, err := duckdb.Split(c.SQL)
		if err != nil || len(sts) != len(c.Shapes) {
			continue
		}
		for i, st := range sts {
			if c.Shapes[i] == nil || string(c.Shapes[i]) == "null" {
				continue
			}
			got, reason := mapStatement(st.Statement)
			if reason != "" {
				skipped++
				reasons[reason]++
				if _, ok := reasonExample[reason]; !ok {
					reasonExample[reason] = st.Text
				}
				continue
			}
			var want any
			d := json.NewDecoder(strings.NewReader(string(c.Shapes[i])))
			d.UseNumber()
			if err := d.Decode(&want); err != nil {
				t.Fatal(err)
			}
			compared++
			if p := firstDifference(jsonNormal(t, got), want, ""); p == "" {
				same++
			} else {
				key := trimIndex(p)
				diffs[key]++
				if _, ok := example[key]; !ok {
					example[key] = st.Text
				}
			}
		}
	}
	t.Logf("%d SELECT statements: %d not compared (a construct the mapping does not cover), %d compared, %d identical", compared+skipped, skipped, compared, same)
	if testing.Verbose() {
		logTop(t, "not mapped", reasons, reasonExample)
		logTop(t, "differences", diffs, example)
	}
	return compared, same
}

func trimIndex(p string) string {
	var b strings.Builder
	depth := 0
	for _, r := range p {
		switch {
		case r == '[':
			depth++
		case r == ']':
			depth--
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func logTop(t *testing.T, title string, m map[string]int, example map[string]string) {
	type kv struct {
		k string
		v int
	}
	var l []kv
	for k, v := range m {
		l = append(l, kv{k, v})
	}
	sort.Slice(l, func(i, j int) bool { return l[i].v > l[j].v || l[i].v == l[j].v && l[i].k < l[j].k })
	for i, e := range l {
		if i == 25 {
			break
		}
		if example != nil {
			ex := example[e.k]
			if len(ex) > 100 {
				ex = ex[:100]
			}
			t.Logf("%s: %5d %s   e.g. %q", title, e.v, e.k, ex)
		} else {
			t.Logf("%s: %5d %s", title, e.v, e.k)
		}
	}
}

func extractField(spec string) string {
	if strings.HasPrefix(spec, "'") {
		return (&duckdb.StringLit{Text: spec}).Value()
	}
	return spec
}

// TestShapeDebug prints, for a statement of the corpora given by the environment variable SHAPE_SQL, the tree that
// the mapping builds and the one of DuckDB. It is a tool for working on the mapping and skips without the variable.
func TestShapeDebug(t *testing.T) {
	sql := os.Getenv("SHAPE_SQL")
	if sql == "" {
		t.Skip("set SHAPE_SQL to a statement of the corpora")
	}
	for _, name := range []string{"exprs", "tests"} {
		for _, c := range loadRef(t, name) {
			if c.SQL != sql || len(c.Shapes) == 0 {
				continue
			}
			sts, err := duckdb.Split(sql)
			if err != nil {
				t.Fatal(err)
			}
			got, reason := mapStatement(sts[0].Statement)
			if reason != "" {
				t.Fatal("not mapped: ", reason)
			}
			a, _ := json.MarshalIndent(jsonNormal(t, got), "", " ")
			var want any
			if err := json.Unmarshal(c.Shapes[0], &want); err != nil {
				t.Fatal(err)
			}
			b, _ := json.MarshalIndent(want, "", " ")
			fmt.Printf("--- mine\n%s\n--- DuckDB\n%s\n", a, b)
			return
		}
	}
	t.Fatal("the statement is not in the corpora")
}
