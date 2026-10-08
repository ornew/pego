package python

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
)

// Dump returns the tree as CPython's ast.dump(tree) prints it (with its default arguments: field
// names, no attributes, on one line), for a value returned by ParseAST or any node in it. It
// computes what the AST of this package keeps as source text: the values of constants, the
// concatenation of adjacent string literals and f-string parts, the operator classes, the
// contexts and the conversions of replacement fields.
//
// A value that cannot be computed (an unknown name in \N{...}, which ParseAST accepts) is printed
// as <error: ...>.
func Dump(node any) string {
	var b strings.Builder
	writeVal(&b, convert(node))
	return b.String()
}

// The tree that Dump prints, in the shape of Python's AST objects.
type (
	pnode struct {
		cls    string
		fields []pfield
	}
	pfield struct {
		name string
		v    any  // *pnode, plist, prepr or nil (None)
		opt  bool // the field is optional: omitted when None
	}
	plist []any
	prepr string // the repr of a value
)

func writeVal(b *strings.Builder, v any) {
	switch v := v.(type) {
	case nil:
		b.WriteString("None")
	case prepr:
		b.WriteString(string(v))
	case plist:
		b.WriteByte('[')
		for i, x := range v {
			if i > 0 {
				b.WriteString(", ")
			}
			writeVal(b, x)
		}
		b.WriteByte(']')
	case *pnode:
		b.WriteString(v.cls)
		b.WriteByte('(')
		n := 0
		for _, f := range v.fields {
			if f.v == nil && f.opt {
				continue
			}
			if l, ok := f.v.(plist); ok && len(l) == 0 {
				continue
			}
			if n > 0 {
				b.WriteString(", ")
			}
			n++
			b.WriteString(f.name)
			b.WriteByte('=')
			writeVal(b, f.v)
		}
		b.WriteByte(')')
	default:
		fmt.Fprintf(b, "<unknown %T>", v)
	}
}

func node(cls string, fields ...pfield) *pnode { return &pnode{cls: cls, fields: fields} }
func f(name string, v any) pfield              { return pfield{name: name, v: v} }
func opt(name string, v any) pfield            { return pfield{name: name, v: v, opt: true} }

// optional converts an optional node: nil stays None.
func optional[T any](x T) any {
	if isNil(x) {
		return nil
	}
	return convert(x)
}

func isNil(x any) bool {
	if x == nil {
		return true
	}
	v := reflect.ValueOf(x)
	return v.Kind() == reflect.Pointer && v.IsNil()
}

func list[T any](xs []T) plist {
	out := make(plist, len(xs))
	for i, x := range xs {
		out[i] = optional(x)
	}
	return out
}

// str is an identifier, normalized as Python normalizes it.
func str(s string) prepr { return prepr(reprStr([]rune(NormalizeName(s)))) }

// optStr is an identifier that may be absent ("").
func optStr(s string) any {
	if s == "" {
		return nil
	}
	return str(s)
}

func ctx(c string) *pnode { return node(c) }

func boolInt(b bool) prepr {
	if b {
		return "1"
	}
	return "0"
}

func convert(x any) any {
	if isNil(x) {
		return nil
	}
	switch n := x.(type) {
	case *Module:
		return node("Module", f("body", list(n.Body)), f("type_ignores", plist{}))

	// --- Statements ---
	case *FunctionDef:
		return node("FunctionDef", f("name", str(n.Name)), f("args", convert(n.Args)), f("body", list(n.Body)),
			f("decorator_list", list(n.DecoratorList)), opt("returns", optional(n.Returns)), opt("type_comment", nil),
			f("type_params", list(n.TypeParams)))
	case *AsyncFunctionDef:
		return node("AsyncFunctionDef", f("name", str(n.Name)), f("args", convert(n.Args)), f("body", list(n.Body)),
			f("decorator_list", list(n.DecoratorList)), opt("returns", optional(n.Returns)), opt("type_comment", nil),
			f("type_params", list(n.TypeParams)))
	case *ClassDef:
		return node("ClassDef", f("name", str(n.Name)), f("bases", list(n.Bases)), f("keywords", list(n.Keywords)),
			f("body", list(n.Body)), f("decorator_list", list(n.DecoratorList)), f("type_params", list(n.TypeParams)))
	case *Return:
		return node("Return", opt("value", optional(n.Value)))
	case *Delete:
		return node("Delete", f("targets", list(n.Targets)))
	case *Assign:
		return node("Assign", f("targets", list(n.Targets)), f("value", convert(n.Value)), opt("type_comment", nil))
	case *TypeAlias:
		return node("TypeAlias", f("name", convert(n.Name)), f("type_params", list(n.TypeParams)), f("value", convert(n.Value)))
	case *AugAssign:
		return node("AugAssign", f("target", convert(n.Target)), f("op", opNode(n.Op, binOps)), f("value", convert(n.Value)))
	case *AnnAssign:
		return node("AnnAssign", f("target", convert(n.Target)), f("annotation", convert(n.Annotation)),
			opt("value", optional(n.Value)), f("simple", boolInt(n.Simple)))
	case *For:
		return node("For", f("target", convert(n.Target)), f("iter", convert(n.Iter)), f("body", list(n.Body)),
			f("orelse", list(n.OrElse)), opt("type_comment", nil))
	case *AsyncFor:
		return node("AsyncFor", f("target", convert(n.Target)), f("iter", convert(n.Iter)), f("body", list(n.Body)),
			f("orelse", list(n.OrElse)), opt("type_comment", nil))
	case *While:
		return node("While", f("test", convert(n.Test)), f("body", list(n.Body)), f("orelse", list(n.OrElse)))
	case *If:
		return node("If", f("test", convert(n.Test)), f("body", list(n.Body)), f("orelse", list(n.OrElse)))
	case *With:
		return node("With", f("items", list(n.Items)), f("body", list(n.Body)), opt("type_comment", nil))
	case *AsyncWith:
		return node("AsyncWith", f("items", list(n.Items)), f("body", list(n.Body)), opt("type_comment", nil))
	case *MatchStmt:
		return node("Match", f("subject", convert(n.Subject)), f("cases", list(n.Cases)))
	case *Raise:
		return node("Raise", opt("exc", optional(n.Exc)), opt("cause", optional(n.Cause)))
	case *Try:
		return node("Try", f("body", list(n.Body)), f("handlers", list(n.Handlers)), f("orelse", list(n.OrElse)),
			f("finalbody", list(n.FinalBody)))
	case *TryStar:
		return node("TryStar", f("body", list(n.Body)), f("handlers", list(n.Handlers)), f("orelse", list(n.OrElse)),
			f("finalbody", list(n.FinalBody)))
	case *Assert:
		return node("Assert", f("test", convert(n.Test)), opt("msg", optional(n.Msg)))
	case *Import:
		return node("Import", f("names", list(n.Names)))
	case *ImportFrom:
		return node("ImportFrom", opt("module", optStr(n.Module)), f("names", list(n.Names)),
			opt("level", prepr(strconv.Itoa(n.Level))))
	case *Global:
		return node("Global", f("names", identifiers(n.Names)))
	case *Nonlocal:
		return node("Nonlocal", f("names", identifiers(n.Names)))
	case *ExprStmt:
		return node("Expr", f("value", convert(n.Value)))
	case *Pass:
		return node("Pass")
	case *Break:
		return node("Break")
	case *Continue:
		return node("Continue")

	// --- Expressions ---
	case *BoolOp:
		return node("BoolOp", f("op", opNode(n.Op, boolOps)), f("values", list(n.Values)))
	case *NamedExpr:
		return node("NamedExpr", f("target", convert(n.Target)), f("value", convert(n.Value)))
	case *BinOp:
		return node("BinOp", f("left", convert(n.Left)), f("op", opNode(n.Op, binOps)), f("right", convert(n.Right)))
	case *UnaryOp:
		return node("UnaryOp", f("op", opNode(n.Op, unaryOps)), f("operand", convert(n.Operand)))
	case *Lambda:
		return node("Lambda", f("args", convert(n.Args)), f("body", convert(n.Body)))
	case *IfExp:
		return node("IfExp", f("test", convert(n.Test)), f("body", convert(n.Body)), f("orelse", convert(n.OrElse)))
	case *Dict:
		return node("Dict", f("keys", list(n.Keys)), f("values", list(n.Values)))
	case *Set:
		return node("Set", f("elts", list(n.Elts)))
	case *ListComp:
		return node("ListComp", f("elt", convert(n.Elt)), f("generators", list(n.Generators)))
	case *SetComp:
		return node("SetComp", f("elt", convert(n.Elt)), f("generators", list(n.Generators)))
	case *DictComp:
		return node("DictComp", f("key", convert(n.Key)), f("value", convert(n.Value)), f("generators", list(n.Generators)))
	case *GeneratorExp:
		return node("GeneratorExp", f("elt", convert(n.Elt)), f("generators", list(n.Generators)))
	case *Await:
		return node("Await", f("value", convert(n.Value)))
	case *Yield:
		return node("Yield", opt("value", optional(n.Value)))
	case *YieldFrom:
		return node("YieldFrom", f("value", convert(n.Value)))
	case *Compare:
		ops := make(plist, len(n.Ops))
		for i, o := range n.Ops {
			ops[i] = opNode(o, cmpOps)
		}
		return node("Compare", f("left", convert(n.Left)), f("ops", ops), f("comparators", list(n.Comparators)))
	case *Call:
		return node("Call", f("func", convert(n.Func)), f("args", list(n.Args)), f("keywords", list(n.Keywords)))
	case *FormattedValue:
		// Only inside JoinedStr, where joinedValues handles it; a FormattedValue on its own has no
		// debug text.
		return formattedValue(n)
	case *Interpolation:
		return interpolation(n)
	case *JoinedStr:
		return node("JoinedStr", f("values", joinedValues(n.Values)))
	case *TemplateStr:
		return node("TemplateStr", f("values", joinedValues(n.Values)))
	case *Constant:
		return constant(n)
	case *Attribute:
		return node("Attribute", f("value", convert(n.Value)), f("attr", str(n.Attr)), f("ctx", ctx(n.Ctx)))
	case *Subscript:
		return node("Subscript", f("value", convert(n.Value)), f("slice", convert(n.Slice)), f("ctx", ctx(n.Ctx)))
	case *Starred:
		return node("Starred", f("value", convert(n.Value)), f("ctx", ctx(n.Ctx)))
	case *Name:
		return node("Name", f("id", str(n.Id)), f("ctx", ctx(n.Ctx)))
	case *ListExpr:
		return node("List", f("elts", list(n.Elts)), f("ctx", ctx(n.Ctx)))
	case *Tuple:
		return node("Tuple", f("elts", list(n.Elts)), f("ctx", ctx(n.Ctx)))
	case *Slice:
		return node("Slice", opt("lower", optional(n.Lower)), opt("upper", optional(n.Upper)), opt("step", optional(n.Step)))

	// --- Patterns ---
	case *MatchCase:
		return node("match_case", f("pattern", convert(n.Pattern)), opt("guard", optional(n.Guard)), f("body", list(n.Body)))
	case *MatchValue:
		return node("MatchValue", f("value", convert(n.Value)))
	case *MatchSingleton:
		v := constant(n.Value)
		if c, ok := v.(*pnode); ok && c.cls == "Constant" {
			return node("MatchSingleton", f("value", c.fields[0].v))
		}
		return node("MatchSingleton", f("value", v))
	case *MatchSequence:
		return node("MatchSequence", f("patterns", list(n.Patterns)))
	case *MatchMapping:
		return node("MatchMapping", f("keys", list(n.Keys)), f("patterns", list(n.Patterns)), opt("rest", optStr(n.Rest)))
	case *MatchClass:
		return node("MatchClass", f("cls", convert(n.Cls)), f("patterns", list(n.Patterns)),
			f("kwd_attrs", identifiers(n.KwdAttrs)), f("kwd_patterns", list(n.KwdPatterns)))
	case *MatchStar:
		return node("MatchStar", opt("name", optStr(n.Name)))
	case *MatchAs:
		return node("MatchAs", opt("pattern", optional(n.Pattern)), opt("name", optStr(n.Name)))
	case *MatchOr:
		return node("MatchOr", f("patterns", list(n.Patterns)))

	// --- Other nodes ---
	case *Arguments:
		return node("arguments", f("posonlyargs", list(n.PosOnlyArgs)), f("args", list(n.Args)),
			opt("vararg", optional(n.VarArg)), f("kwonlyargs", list(n.KwOnlyArgs)), f("kw_defaults", list(n.KwDefaults)),
			opt("kwarg", optional(n.KwArg)), f("defaults", list(n.Defaults)))
	case *Arg:
		return node("arg", f("arg", str(n.Arg)), opt("annotation", optional(n.Annotation)), opt("type_comment", nil))
	case *Keyword:
		return node("keyword", opt("arg", optStr(n.Arg)), f("value", convert(n.Value)))
	case *Alias:
		return node("alias", f("name", str(n.Name)), opt("asname", optStr(n.AsName)))
	case *WithItem:
		return node("withitem", f("context_expr", convert(n.ContextExpr)), opt("optional_vars", optional(n.OptionalVars)))
	case *Comprehension:
		return node("comprehension", f("target", convert(n.Target)), f("iter", convert(n.Iter)), f("ifs", list(n.Ifs)),
			f("is_async", boolInt(n.IsAsync)))
	case *ExceptHandler:
		return node("ExceptHandler", opt("type", optional(n.Type)), opt("name", optStr(n.Name)), f("body", list(n.Body)))
	case *TypeVar:
		return node("TypeVar", f("name", str(n.Name)), opt("bound", optional(n.Bound)), opt("default_value", optional(n.DefaultValue)))
	case *ParamSpec:
		return node("ParamSpec", f("name", str(n.Name)), opt("default_value", optional(n.DefaultValue)))
	case *TypeVarTuple:
		return node("TypeVarTuple", f("name", str(n.Name)), opt("default_value", optional(n.DefaultValue)))
	case *Error:
		return prepr("<error: " + n.Message + ">")
	}
	return prepr(fmt.Sprintf("<unknown %T>", x))
}

func identifiers(ids []*Identifier) plist {
	out := make(plist, len(ids))
	for i, id := range ids {
		out[i] = str(id.Text)
	}
	return out
}

var (
	binOps = map[string]string{"+": "Add", "-": "Sub", "*": "Mult", "@": "MatMult", "/": "Div", "%": "Mod", "**": "Pow",
		"<<": "LShift", ">>": "RShift", "|": "BitOr", "^": "BitXor", "&": "BitAnd", "//": "FloorDiv"}
	unaryOps = map[string]string{"~": "Invert", "not": "Not", "+": "UAdd", "-": "USub"}
	boolOps  = map[string]string{"and": "And", "or": "Or"}
	cmpOps   = map[string]string{"==": "Eq", "!=": "NotEq", "<>": "NotEq", "<": "Lt", "<=": "LtE", ">": "Gt", ">=": "GtE",
		"is": "Is", "is not": "IsNot", "in": "In", "not in": "NotIn"}
)

// OpName returns the name of the class of Python's ast module for the operator: "Add" for "+" and
// "+=", "NotIn" for "not in", and so on (op is the text of an Op, in any context).
func OpName(op string) string {
	return opName(op)
}

func opName(op string) string {
	switch {
	case strings.HasPrefix(op, "not") && len(op) > 3:
		return "NotIn"
	case strings.HasPrefix(op, "is") && len(op) > 2:
		return "IsNot"
	}
	for _, m := range []map[string]string{cmpOps, binOps, unaryOps, boolOps} {
		if n, ok := m[op]; ok {
			return n
		}
	}
	if n, ok := binOps[strings.TrimSuffix(op, "=")]; ok && strings.HasSuffix(op, "=") {
		return n
	}
	return ""
}

func opNode(o *Op, m map[string]string) *pnode {
	if o == nil {
		return node("<nil op>")
	}
	op := o.Text
	if n, ok := m[op]; ok {
		return node(n)
	}
	return node(opName(op))
}

func constant(c *Constant) any {
	if c == nil {
		return nil
	}
	v, err := constValue(c.Text)
	if err != nil {
		return prepr("<error: " + err.Error() + ">")
	}
	var kind any
	if v.kind == kStr && v.u {
		kind = str("u")
	}
	return node("Constant", f("value", reprValue(v)), opt("kind", kind))
}

func reprValue(v cval) prepr {
	switch v.kind {
	case kInt:
		return prepr(v.i.String())
	case kFloat:
		return prepr(reprFloat(v.f, true))
	case kComplex:
		return prepr(reprFloat(v.f, false) + "j")
	case kStr:
		return prepr(reprStr(v.str))
	case kBytes:
		return prepr(reprBytes(v.bytes))
	case kTrue:
		return "True"
	case kFalse:
		return "False"
	case kNone:
		return "None"
	}
	return "Ellipsis"
}

// A part of a JoinedStr or TemplateStr before adjacent constants are concatenated.
type strItem struct {
	text []rune // a constant (node == nil)
	u    bool   // its kind is "u"
	node any    // a converted replacement field
}

// joinedValues computes the values of a JoinedStr or TemplateStr as CPython does
// (_PyPegen_concatenate_strings): the debug text of fields with '=' comes before them, adjacent
// constants are concatenated (the kind is that of the first), and empty constants dropped.
func joinedValues(parts []StrPart) plist {
	var items []strItem
	for _, p := range parts {
		switch p := p.(type) {
		case *Constant:
			rs, u, err := p.StringValue()
			if err != nil {
				items = append(items, strItem{node: prepr("<error: " + err.Error() + ">")})
				continue
			}
			items = append(items, strItem{text: rs, u: u})
		case *FStringMiddle:
			rs, err := decodeFStringText(p.Text, false)
			if err != nil {
				items = append(items, strItem{node: prepr("<error: " + err.Error() + ">")})
				continue
			}
			items = append(items, strItem{text: rs})
		case *FStringRawMiddle:
			rs, _ := decodeFStringText(p.Text, true)
			items = append(items, strItem{text: rs})
		case *FormattedValue:
			if p.Debug != "" {
				items = append(items, strItem{text: debugText(p.Str, p.Debug)})
			}
			items = append(items, strItem{node: formattedValue(p)})
		case *Interpolation:
			if p.Debug != "" {
				items = append(items, strItem{text: debugText(p.Str, p.Debug)})
			}
			items = append(items, strItem{node: interpolation(p)})
		default:
			items = append(items, strItem{node: convert(p)})
		}
	}
	var out plist
	for i := 0; i < len(items); {
		if items[i].node != nil {
			out = append(out, items[i].node)
			i++
			continue
		}
		u := items[i].u
		var text []rune
		for ; i < len(items) && items[i].node == nil; i++ {
			text = append(text, items[i].text...)
		}
		if len(text) == 0 {
			continue
		}
		var kind any
		if u {
			kind = str("u")
		}
		out = append(out, node("Constant", f("value", prepr(reprStr(text))), opt("kind", kind)))
	}
	return out
}

// debugText is the text of a replacement field with '=' that comes before its value: the
// expression and the '=' as written, without comments, with the newlines read as \n.
func debugText(expr, debug string) []rune {
	s := expr + debug
	var out []rune
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\'', '"':
			// A string literal: copied up to its closing quote.
			if _, n, err := literalBody(s[i:]); err == nil {
				out = append(out, []rune(strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(s[i:i+n]))...)
				i += n - 1
				continue
			}
			out = append(out, rune(c))
		case '#':
			for i+1 < len(s) && s[i+1] != '\n' && s[i+1] != '\r' {
				i++
			}
		case '\r':
			out = append(out, '\n')
			if i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
		default:
			r := []rune(s[i:])[0]
			out = append(out, r)
			i += len(string(r)) - 1
		}
	}
	return out
}

func formattedValue(n *FormattedValue) any {
	var spec any
	if n.FormatSpec != nil {
		spec = node("JoinedStr", f("values", joinedValues(n.FormatSpec.Values)))
	}
	return node("FormattedValue", f("value", convert(n.Value)), f("conversion", prepr(strconv.Itoa(n.ConversionCode()))),
		opt("format_spec", spec))
}

func interpolation(n *Interpolation) any {
	var spec any
	if n.FormatSpec != nil {
		spec = node("JoinedStr", f("values", joinedValues(n.FormatSpec.Values)))
	}
	return node("Interpolation", f("value", convert(n.Value)), f("str", prepr(reprStr(debugText(n.Str, "")))),
		f("conversion", prepr(strconv.Itoa(n.ConversionCode()))), opt("format_spec", spec))
}

// reprStr returns the repr of a str, as Python writes it.
func reprStr(rs []rune) string {
	quote := '\''
	if containsRune(rs, '\'') && !containsRune(rs, '"') {
		quote = '"'
	}
	var b strings.Builder
	b.WriteRune(quote)
	for _, r := range rs {
		switch {
		case r == quote || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r < 0x7f:
			b.WriteRune(r)
		case isPrintable(r):
			b.WriteRune(r)
		case r <= 0xff:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r <= 0xffff:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			fmt.Fprintf(&b, `\U%08x`, r)
		}
	}
	b.WriteRune(quote)
	return b.String()
}

func containsRune(rs []rune, r rune) bool {
	for _, x := range rs {
		if x == r {
			return true
		}
	}
	return false
}

// reprBytes returns the repr of a bytes object.
func reprBytes(bs []byte) string {
	quote := byte('\'')
	if strings.IndexByte(string(bs), '\'') >= 0 && strings.IndexByte(string(bs), '"') < 0 {
		quote = '"'
	}
	var b strings.Builder
	b.WriteByte('b')
	b.WriteByte(quote)
	for _, c := range bs {
		switch {
		case c == quote || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c == '\t':
			b.WriteString(`\t`)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\r':
			b.WriteString(`\r`)
		case c < 0x20 || c >= 0x7f:
			fmt.Fprintf(&b, `\x%02x`, c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte(quote)
	return b.String()
}

// reprFloat returns the repr of a float as Python writes it (the shortest digits that round-trip,
// in positional notation for exponents from -4 to 15 and in scientific notation otherwise).
// addDot adds ".0" to an integral value in positional notation (repr of a float, not of the
// imaginary part of a complex).
func reprFloat(x float64, addDot bool) string {
	switch {
	case math.IsInf(x, 1):
		return "inf"
	case math.IsInf(x, -1):
		return "-inf"
	case math.IsNaN(x):
		return "nan"
	}
	s := strconv.FormatFloat(x, 'e', -1, 64) // d.ddde±XX
	mant, expStr, _ := strings.Cut(s, "e")
	exp, _ := strconv.Atoi(expStr)
	neg := strings.HasPrefix(mant, "-")
	mant = strings.TrimPrefix(mant, "-")
	digits := strings.Replace(mant, ".", "", 1)
	var out string
	if exp >= -5+1 && exp < 16 {
		// Positional.
		decpt := exp + 1
		switch {
		case decpt <= 0:
			out = "0." + strings.Repeat("0", -decpt) + digits
		case decpt >= len(digits):
			out = digits + strings.Repeat("0", decpt-len(digits))
			if addDot {
				out += ".0"
			}
		default:
			out = digits[:decpt] + "." + digits[decpt:]
		}
	} else {
		out = digits[:1]
		if len(digits) > 1 {
			out += "." + digits[1:]
		}
		sign := "+"
		if exp < 0 {
			sign = "-"
			exp = -exp
		}
		out += fmt.Sprintf("e%s%02d", sign, exp)
	}
	if neg {
		out = "-" + out
	}
	return out
}
