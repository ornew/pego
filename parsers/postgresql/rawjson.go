package postgresql

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// RawJSON returns the raw parse tree of the statements of a script as PostgreSQL 18's parser, through
// libpg_query's pg_query_parse, prints it: a JSON array with the node of each statement, whose members are
// named like the fields of the node structs of the C parser (targetList, fromClause, ...), without the
// locations. Names and constants are decoded (identifiers folded to lower case, strings unescaped), the
// defaults that the parser's actions set are filled in, and what the parser's actions rewrite is rewritten
// the same way (a negative number is one constant, AND and OR chains are flat, ...): where this package's AST
// keeps the syntactic form, RawJSON is the function that makes the raw tree of it. The tests compare it
// with libpg_query's trees.
func RawJSON(s *Script) ([]byte, error) {
	t, err := RawTree(s)
	if err != nil {
		return nil, err
	}
	return json.Marshal(t)
}

// RawTree is RawJSON before encoding: the tree as nested map[string]any, []any, string, int64, bool and nil
// values.
func RawTree(s *Script) ([]any, error) {
	c := &converter{}
	out := []any{}
	for _, st := range s.Stmts {
		out = append(out, c.value(reflect.ValueOf(st.Stmt), staticAny))
	}
	if c.err != nil {
		return nil, c.err
	}
	return out, nil
}

// static says where a value appears, which decides how a name or a number prints: as the value of a field
// of its own type (the relation name of a RangeVar: a plain string) or elsewhere, in a list or in a field of
// a union type (a node of the raw tree: String, Integer, ...).
type static int

const (
	staticAny   static = iota // a generic node, an element of a list or a union
	staticField               // a field whose Go type is the token type itself
)

type converter struct {
	err error
}

func (c *converter) fail(format string, a ...any) any {
	if c.err == nil {
		c.err = fmt.Errorf(format, a...)
	}
	return nil
}

// terminals are the types of tokens: they hold source text.
func isTerminal(name string) bool {
	switch name {
	case "Ident", "OpTok", "Iconst", "Fconst", "Sconst", "Bconst", "Xconst", "Param", "ObjectType", "DropBehavior", "SconstStr":
		return true
	}
	return false
}

// rawHooks build the raw value of the nodes of the types of this package that have no field-for-field
// counterpart in the raw tree; they return the value to print (a map with one key, the raw node name).
var rawHooks map[string]func(c *converter, v reflect.Value) any

// postHooks change the node that node() built for a type of this package that has a raw counterpart, which
// is what the parser's actions do after they have built it (folding a sign, flattening, adding defaults).
// body is the map of the fields; extras holds the converted values of the fields of the AST that the raw
// node does not have (the hook must consume them: the others are an error).
var postHooks map[string]func(c *converter, v reflect.Value, body, extras map[string]any) any

func init() {
	rawHooks = map[string]func(c *converter, v reflect.Value) any{
		"String":  func(c *converter, v reflect.Value) any { return strNode(v.FieldByName("Sval").String()) },
		"Integer": func(c *converter, v reflect.Value) any { return intNode(v.FieldByName("Ival").Int()) },
		"Boolean": func(c *converter, v reflect.Value) any {
			if v.FieldByName("Boolval").Bool() {
				return map[string]any{"Boolean": map[string]any{"boolval": true}}
			}
			return map[string]any{"Boolean": map[string]any{}}
		},
		"A_Star": func(c *converter, v reflect.Value) any { return map[string]any{"A_Star": map[string]any{}} },
		"NodeList": func(c *converter, v reflect.Value) any {
			return map[string]any{"List": map[string]any{"items": c.list(v.FieldByName("Items"))}}
		},
		"A_Const": hookAConst,
		"RangeFuncItem": func(c *converter, v reflect.Value) any {
			items := []any{c.value(v.FieldByName("Func"), staticAny)}
			if cd := v.FieldByName("Coldeflist"); cd.Len() > 0 {
				items = append(items, map[string]any{"List": map[string]any{"items": c.list(cd)}})
			} else {
				items = append(items, map[string]any{})
			}
			return map[string]any{"List": map[string]any{"items": items}}
		},
	}
	postHooks = map[string]func(c *converter, v reflect.Value, body, extras map[string]any) any{
		"SelectStmt": func(c *converter, v reflect.Value, body, extras map[string]any) any {
			if on, _ := extras["DistinctOn"].([]any); len(on) > 0 {
				body["distinctClause"] = on
			} else if d, _ := extras["Distinct"].(bool); d {
				body["distinctClause"] = []any{map[string]any{}}
			}
			if t, _ := extras["LimitWithTies"].(bool); t {
				body["limitOption"] = "LIMIT_OPTION_WITH_TIES"
			} else if body["limitCount"] != nil || body["limitOffset"] != nil {
				body["limitOption"] = "LIMIT_OPTION_COUNT"
			}
			return nil
		},
		"SQLValueFunction": func(c *converter, v reflect.Value, body, extras map[string]any) any {
			if v.FieldByName("Typmod").IsNil() {
				body["typmod"] = int64(-1)
			}
			return nil
		},
		"MergeSupportFunc": func(c *converter, v reflect.Value, body, extras map[string]any) any {
			body["msftype"] = int64(25)
			return nil
		},
		"A_Expr":   hookAExpr,
		"BoolExpr": hookBoolExpr,
	}
}

// extraFields are the fields of the AST that the raw nodes of a type do not have; the post hook consumes them.
var extraFields = map[string][]string{
	"SelectStmt": {"Distinct", "DistinctOn", "LimitWithTies"},
}

func strNode(s string) any {
	return map[string]any{"String": map[string]any{"sval": s}}
}

func intNode(n int64) any {
	if n == 0 {
		return map[string]any{"Integer": map[string]any{}}
	}
	return map[string]any{"Integer": map[string]any{"ival": n}}
}

// list converts the elements of a slice (its values are the elements of a raw List).
func (c *converter) list(v reflect.Value) []any {
	out := make([]any, 0, v.Len())
	for i := 0; i < v.Len(); i++ {
		out = append(out, c.value(v.Index(i), staticAny))
	}
	return out
}

// value converts a value of the AST.
func (c *converter) value(v reflect.Value, st static) any {
	for v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		if v.Kind() == reflect.Interface {
			st = staticAny
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Bool:
		return v.Bool()
	case reflect.Int, reflect.Int64, reflect.Int32:
		return v.Int()
	case reflect.Slice:
		return c.list(v)
	}
	name := v.Type().Name()
	if isTerminal(name) {
		return c.terminal(v, name, st)
	}
	if h, ok := rawHooks[name]; ok {
		return h(c, v)
	}
	return c.node(v, name)
}

// terminal converts a token. As the value of a field of its own type a name is a string and a number a
// number; elsewhere (in a list, in a field of a union type) it is a node of the raw tree: String, Integer,
// Float or BitString.
func (c *converter) terminal(v reflect.Value, name string, st static) any {
	text := v.FieldByName("Text").String()
	switch name {
	case "Ident":
		s, err := (&Ident{Text: text}).Value()
		if err != nil {
			return c.fail("%v", err)
		}
		if st == staticField {
			return s
		}
		return strNode(s)
	case "OpTok":
		return strNode((&OpTok{Text: text}).Value())
	case "Iconst":
		n, err := (&Iconst{Text: text}).Int64()
		if err != nil {
			return c.fail("%v", err)
		}
		if st == staticField {
			return n
		}
		return intNode(n)
	case "Param":
		return int64((&Param{Text: text}).Value())
	case "Fconst":
		return map[string]any{"Float": map[string]any{"fval": text}}
	case "DropBehavior":
		return dropBehavior(text)
	case "ObjectType":
		return objectType(text)
	case "Sconst", "SconstStr":
		s, err := (&Sconst{Text: text}).Value()
		if err != nil {
			return c.fail("%v", err)
		}
		if st == staticField {
			return s
		}
		return strNode(s)
	case "Bconst":
		return map[string]any{"BitString": map[string]any{"bsval": (&Bconst{Text: text}).Value()}}
	case "Xconst":
		return map[string]any{"BitString": map[string]any{"bsval": (&Xconst{Text: text}).Value()}}
	}
	return nil
}

// node converts a struct of this package that has the name and the fields of a node of the raw tree.
func (c *converter) node(v reflect.Value, name string) any {
	fields, ok := rawNodes[name]
	if !ok {
		return c.fail("%s is not a node of the raw parse tree", name)
	}
	body := map[string]any{}
	for _, f := range fields {
		fv := v.FieldByName(f.goName)
		var val any
		if fv.IsValid() {
			val = c.fieldValue(fv, f)
		}
		if isEmpty(val) {
			if f.enum != "" {
				body[f.json] = f.enum
			}
			continue
		}
		body[f.json] = val
	}
	extras := map[string]any{}
	for i := 0; i < v.NumField(); i++ {
		sf := v.Type().Field(i)
		if sf.Name == "Span" || !sf.IsExported() {
			continue
		}
		if !hasField(fields, sf.Name) {
			if !containsString(extraFields[name], sf.Name) {
				c.fail("%s has a field %s that the raw node does not have", name, sf.Name)
				continue
			}
			extras[sf.Name] = c.value(v.Field(i), staticAny)
		}
	}
	if h, ok := postHooks[name]; ok {
		if r := h(c, v, body, extras); r != nil {
			return r
		}
	}
	return map[string]any{name: body}
}

func hasField(fields []rawField, goName string) bool {
	for _, f := range fields {
		if f.goName == goName {
			return true
		}
	}
	return false
}

func containsString(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// fieldValue converts the value of a field; a field of a concrete node type prints without the wrapper.
func (c *converter) fieldValue(fv reflect.Value, f rawField) any {
	t := fv.Type()
	if t.Kind() == reflect.Slice {
		return c.list(fv)
	}
	st := staticAny
	if t.Kind() == reflect.Pointer && isTerminal(t.Elem().Name()) || isTerminal(t.Name()) {
		st = staticField
	}
	val := c.value(fv, st)
	if f.concrete {
		if m, ok := val.(map[string]any); ok && len(m) == 1 {
			for _, inner := range m {
				return inner
			}
		}
	}
	return val
}

func isEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case bool:
		return !x
	case int64:
		return x == 0
	case string:
		return x == ""
	case []any:
		return len(x) == 0
	}
	return false
}

// hookAConst prints the constant as the raw tree does: the value is a member named by its kind.
func hookAConst(c *converter, v reflect.Value) any {
	out := map[string]any{}
	if v.FieldByName("Isnull").Bool() {
		out["isnull"] = true
		return map[string]any{"A_Const": out}
	}
	val := v.FieldByName("Val")
	if val.IsNil() {
		return map[string]any{"A_Const": out}
	}
	x := c.value(val, staticAny) // a node of the raw tree: String, Integer, Float, BitString or Boolean
	m, _ := x.(map[string]any)
	for kind, body := range m {
		switch kind {
		case "Integer":
			out["ival"] = body
		case "Float":
			out["fval"] = body
		case "String":
			out["sval"] = body
		case "BitString":
			out["bsval"] = body
		case "Boolean":
			out["boolval"] = body
		}
	}
	return map[string]any{"A_Const": out}
}

// hookAExpr folds the sign of a negated number: doNegate of gram.y makes - 5 the constant -5 (and - 1.5 the
// float -1.5, - -5 the constant 5); any other operand stays an operator expression.
func hookAExpr(c *converter, v reflect.Value, body, extras map[string]any) any {
	if body["kind"] != "AEXPR_OP" || body["lexpr"] != nil {
		return nil
	}
	names, _ := body["name"].([]any)
	if len(names) != 1 || !reflect.DeepEqual(names[0], strNode("-")) {
		return nil
	}
	r, _ := body["rexpr"].(map[string]any)
	cm, _ := r["A_Const"].(map[string]any)
	if cm == nil {
		return nil
	}
	if iv, ok := cm["ival"].(map[string]any); ok {
		n, _ := iv["ival"].(int64)
		if n == 0 {
			return map[string]any{"A_Const": map[string]any{"ival": map[string]any{}}}
		}
		return map[string]any{"A_Const": map[string]any{"ival": map[string]any{"ival": -n}}}
	}
	if fv, ok := cm["fval"].(map[string]any); ok {
		s, _ := fv["fval"].(string)
		if len(s) > 0 && s[0] == '+' {
			s = s[1:]
		}
		if len(s) > 0 && s[0] == '-' {
			s = s[1:]
		} else {
			s = "-" + s
		}
		return map[string]any{"A_Const": map[string]any{"fval": map[string]any{"fval": s}}}
	}
	return nil
}

// hookBoolExpr flattens a chain: the parser's makeAndExpr and makeOrExpr add the right operand to the
// arguments of a left operand that is an AND (or OR) expression, whether or not it was in parentheses.
func hookBoolExpr(c *converter, v reflect.Value, body, extras map[string]any) any {
	op, _ := body["boolop"].(string)
	if op != "AND_EXPR" && op != "OR_EXPR" {
		return nil
	}
	args, _ := body["args"].([]any)
	if len(args) != 2 {
		return nil
	}
	if l, ok := args[0].(map[string]any); ok {
		if lb, ok := l["BoolExpr"].(map[string]any); ok && lb["boolop"] == op {
			la, _ := lb["args"].([]any)
			body["args"] = append(append([]any{}, la...), args[1])
		}
	}
	return nil
}
