package postgresql

import (
	"reflect"
	"strconv"
	"strings"
)

// The conversions of RawJSON for the statements other than queries and data changes. Most of them follow what
// the actions of gram.y do with the nodes they build: names that are made up (the name of an option), values
// that are computed (the argument types of a function, from its parameters), enumerators that depend on the
// keywords that were written.

func init() {
	extraFields["FetchStmt"] = []string{"Count"}
	extraFields["ObjectWithArgs"] = []string{"Aggr"}

	rawHooks["SignedNum"] = hookSignedNum
	rawHooks["NoneArg"] = func(c *converter, v reflect.Value) any { return map[string]any{} } // NONE: a NULL in a list
	rawHooks["AggrArgs"] = hookAggrArgs
	rawHooks["DefElemK"] = func(c *converter, v reflect.Value) any { return c.node(v, "DefElem") }
	rawHooks["RoleOpt"] = func(c *converter, v reflect.Value) any {
		return defElem(c.value(v.FieldByName("Name"), staticField).(string), map[string]any{"Boolean": map[string]any{"boolval": v.FieldByName("Value").Bool()}})
	}
	rawHooks["DefaclOpt"] = func(c *converter, v reflect.Value) any {
		if s := v.FieldByName("Schemas"); s.Len() > 0 {
			return defElem("schemas", map[string]any{"List": map[string]any{"items": c.list(s)}})
		}
		return defElem("roles", map[string]any{"List": map[string]any{"items": c.list(v.FieldByName("Roles"))}})
	}
	rawHooks["ParamName"] = func(c *converter, v reflect.Value) any {
		var parts []string
		for _, id := range c.list(v.FieldByName("Names")) {
			parts = append(parts, id.(map[string]any)["String"].(map[string]any)["sval"].(string))
		}
		return strNode(strings.Join(parts, "."))
	}
	rawHooks["TrgArgNum"] = func(c *converter, v reflect.Value) any {
		n := c.value(v.FieldByName("Val"), staticAny).(map[string]any)
		if i, ok := n["Integer"].(map[string]any); ok {
			x, _ := i["ival"].(int64)
			return strNode(strconv.FormatInt(x, 10))
		}
		return strNode(n["Float"].(map[string]any)["fval"].(string))
	}

	postHooks["DefElem"] = hookDefElem
	postHooks["VariableSetStmt"] = hookDottedName
	postHooks["VariableShowStmt"] = hookDottedName
	postHooks["FetchStmt"] = func(c *converter, v reflect.Value, body, extras map[string]any) any {
		if n, ok := extras["Count"].(map[string]any); ok {
			if i, ok := n["Integer"].(map[string]any); ok {
				if x, ok := i["ival"].(int64); ok && x != 0 {
					body["howMany"] = x
				}
			}
		}
		return nil
	}
	postHooks["ObjectWithArgs"] = hookObjectWithArgs
	for _, name := range []string{"JsonObjectAgg", "JsonArrayAgg"} {
		postHooks[name] = func(c *converter, v reflect.Value, body, extras map[string]any) any {
			if _, ok := body["constructor"]; !ok {
				body["constructor"] = map[string]any{}
			}
			return nil
		}
	}
}

func defElem(name string, arg any) any {
	return map[string]any{"DefElem": map[string]any{"defname": name, "arg": arg, "defaction": "DEFELEM_UNSPEC"}}
}

// hookSignedNum folds the sign of NumericOnly and SignedIconst into the number, as the parser does.
func hookSignedNum(c *converter, v reflect.Value) any {
	num, _ := c.value(v.FieldByName("Number"), staticAny).(map[string]any)
	if !v.FieldByName("Negative").Bool() {
		return num
	}
	if i, ok := num["Integer"].(map[string]any); ok {
		x, _ := i["ival"].(int64)
		return intNode(-x)
	}
	s, _ := num["Float"].(map[string]any)["fval"].(string)
	if strings.HasPrefix(s, "-") {
		s = s[1:]
	} else {
		s = "-" + s
	}
	return map[string]any{"Float": map[string]any{"fval": s}}
}

// hookDefElem prints the name that the parser made up (a String) as the plain string it is in the raw node,
// and the false of an option (makeBoolean(false)) with its boolval, which a Node field always prints.
func hookDefElem(c *converter, v reflect.Value, body, extras map[string]any) any {
	if m, ok := body["defname"].(map[string]any); ok {
		if s, ok := m["String"].(map[string]any); ok {
			body["defname"], _ = s["sval"].(string)
		}
	}
	if m, ok := body["arg"].(map[string]any); ok {
		if b, ok := m["Boolean"].(map[string]any); ok && len(b) == 0 {
			b["boolval"] = false
		}
	}
	return nil
}

// hookDottedName joins the parts of the name of a variable (SET a.b TO ...): the raw node has the dotted string.
func hookDottedName(c *converter, v reflect.Value, body, extras map[string]any) any {
	if l, ok := body["name"].([]any); ok {
		var parts []string
		for _, p := range l {
			parts = append(parts, p.(map[string]any)["String"].(map[string]any)["sval"].(string))
		}
		body["name"] = strings.Join(parts, ".")
	}
	return nil
}

// params converts a list of FunctionParameter.
func (c *converter) params(v reflect.Value) []any {
	return c.list(v)
}

func paramMode(p any) string {
	m, _ := p.(map[string]any)["FunctionParameter"].(map[string]any)["mode"].(string)
	return m
}

// aggrParams returns the parameters of aggr_args: the direct arguments and the ordered ones; the last direct
// argument VARIADIC absorbs the single VARIADIC ordered one (makeOrderedSetArgs), and the number of direct
// arguments is the second element of DefineStmt.args.
func (c *converter) aggrParams(a reflect.Value) (list []any, ndirect int64) {
	if a.FieldByName("Star").Bool() {
		return nil, -1
	}
	args := c.params(a.FieldByName("Args"))
	ordered := c.params(a.FieldByName("OrderedArgs"))
	if !a.FieldByName("OrderBy").Bool() {
		return args, -1
	}
	if len(args) == 0 {
		return ordered, 0
	}
	if paramMode(args[len(args)-1]) == "FUNC_PARAM_VARIADIC" {
		return args, int64(len(args))
	}
	return append(args, ordered...), int64(len(args))
}

// hookAggrArgs is the definition of an aggregate's arguments in CREATE AGGREGATE: the list of the parameters
// and the number of direct arguments.
func hookAggrArgs(c *converter, v reflect.Value) any {
	for v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	list, n := c.aggrParams(v)
	var first any = map[string]any{}
	if len(list) > 0 {
		first = map[string]any{"List": map[string]any{"items": list}}
	}
	return []any{first, intNode(n)}
}

// hookObjectWithArgs computes objargs: the types of the parameters that are not OUT or TABLE parameters
// (extractArgTypes), and the objfuncargs of an aggregate.
func hookObjectWithArgs(c *converter, v reflect.Value, body, extras map[string]any) any {
	funcargs, _ := body["objfuncargs"].([]any)
	if a := v.FieldByName("Aggr"); !a.IsNil() {
		funcargs, _ = c.aggrParams(a.Elem())
		if funcargs != nil {
			body["objfuncargs"] = funcargs
		}
	}
	if _, ok := body["objargs"]; !ok {
		var types []any
		for _, p := range funcargs {
			if m := paramMode(p); m == "FUNC_PARAM_OUT" || m == "FUNC_PARAM_TABLE" {
				continue
			}
			types = append(types, map[string]any{"TypeName": p.(map[string]any)["FunctionParameter"].(map[string]any)["argType"]})
		}
		if len(types) > 0 {
			body["objargs"] = types
		}
	}
	return nil
}

// dropBehavior is the enumerator of the keyword CASCADE or RESTRICT.
func dropBehavior(text string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(text)), "cascade") {
		return "DROP_CASCADE"
	}
	return "DROP_RESTRICT"
}

// objectTypes are the enumerators of the object types whose name is not OBJECT_ and the words in upper case.
var objectTypes = map[string]string{
	"":                          "OBJECT_TABLE", // GRANT ... ON name: a table
	"materialized view":         "OBJECT_MATVIEW",
	"foreign table":             "OBJECT_FOREIGN_TABLE",
	"statistics":                "OBJECT_STATISTIC_EXT",
	"text search parser":        "OBJECT_TSPARSER",
	"text search dictionary":    "OBJECT_TSDICTIONARY",
	"text search template":      "OBJECT_TSTEMPLATE",
	"text search configuration": "OBJECT_TSCONFIGURATION",
	"access method":             "OBJECT_ACCESS_METHOD",
	"event trigger":             "OBJECT_EVENT_TRIGGER",
	"foreign data wrapper":      "OBJECT_FDW",
	"procedural language":       "OBJECT_LANGUAGE",
	"server":                    "OBJECT_FOREIGN_SERVER",
	"foreign server":            "OBJECT_FOREIGN_SERVER",
	"operator class":            "OBJECT_OPCLASS",
	"operator family":           "OBJECT_OPFAMILY",
	"large object":              "OBJECT_LARGEOBJECT",
	"group":                     "OBJECT_ROLE",
	"user":                      "OBJECT_ROLE",
	"parameter":                 "OBJECT_PARAMETER_ACL",
	// the kinds of ALTER DEFAULT PRIVILEGES and of GRANT ... ON ALL ... IN SCHEMA
	"tables":         "OBJECT_TABLE",
	"functions":      "OBJECT_FUNCTION",
	"routines":       "OBJECT_FUNCTION",
	"sequences":      "OBJECT_SEQUENCE",
	"types":          "OBJECT_TYPE",
	"schemas":        "OBJECT_SCHEMA",
	"large objects":  "OBJECT_LARGEOBJECT",
	"all tables":     "OBJECT_TABLE",
	"all sequences":  "OBJECT_SEQUENCE",
	"all functions":  "OBJECT_FUNCTION",
	"all procedures": "OBJECT_PROCEDURE",
	"all routines":   "OBJECT_ROUTINE",
}

// keywordWords returns the words of the text of keywords in lower case, separated by one space, without the
// comments between them.
func keywordWords(text string) string {
	var b strings.Builder
	for i := 0; i < len(text); {
		switch {
		case strings.HasPrefix(text[i:], "--"):
			j := strings.IndexByte(text[i:], '\n')
			if j < 0 {
				i = len(text)
			} else {
				i += j
			}
		case strings.HasPrefix(text[i:], "/*"):
			d := 1
			i += 2
			for i < len(text) && d > 0 {
				if strings.HasPrefix(text[i:], "/*") {
					d++
					i += 2
				} else if strings.HasPrefix(text[i:], "*/") {
					d--
					i += 2
				} else {
					i++
				}
			}
			b.WriteByte(' ')
		default:
			b.WriteByte(text[i])
			i++
		}
	}
	return strings.Join(strings.Fields(strings.ToLower(b.String())), " ")
}

// objectType is the enumerator of the object type that the keywords name.
func objectType(text string) string {
	w := keywordWords(text)
	if s, ok := objectTypes[w]; ok {
		return s
	}
	return "OBJECT_" + strings.ToUpper(strings.ReplaceAll(w, " ", "_"))
}
