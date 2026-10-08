package engine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ornew/pego/grammar"
)

// Typed values in generated parsers (GenOptions.Types).
//
// With Types, the generated package also defines a Go type for each type of the grammar and a
// function ParseAST that parses like Parse and converts the resulting tree into those types:
//
//   - a struct type T becomes a struct T with the same fields and an embedded Span (its range);
//   - a terminal type T becomes a struct T{Span; Text string}, and the reserved Match and Error
//     node types become Match and Error;
//   - a union type U = A | B becomes an interface U that A, B and Error implement (and Node, if
//     the union has CST members);
//   - []T becomes a slice, *T a pointer (or the nil value of an interface), and int, string and
//     bool stay as they are;
//   - CST node types (Seq, List, Operator) stay *Node, and node, terminal and any become any
//     (holding the converted value).
//
// An Error node (left by #recover) where a struct or terminal type is expected becomes nil; it is
// reported among the SyntaxErrors anyway.

// typedGo returns the Go source of the typed values for a parser starting at rule start, and
// the name of the helper type Span. Unless conv is set, ParseAST runs the typed runtime
// (genrt/typed.go) when every value it can produce has a Go type of its own; otherwise it converts
// the result of Parse.
func (g *generator) typedGo(start *rule, conv bool) (string, string, error) {
	info := g.prog.typed
	if info == nil {
		return "", "", fmt.Errorf("typed values need the grammar's types (type checking did not run)")
	}
	t := &typedGen{g: g, info: info, names: map[string]string{}, unions: map[string]string{}, lists: map[string]string{}, tlists: map[string]string{}}
	t.name()
	st := info.rules[start.name]
	if st == nil {
		st = tyAny
	}
	var b strings.Builder
	t.decls(&b)
	t.valueMethods(&b)
	doc := `// ParseAST parses the input like Parse and returns the result as typed values. If the parse recovered
// from errors, the result is returned together with SyntaxErrors; an Error node where a struct or
// terminal type is expected becomes nil.
`
	if !conv && t.runtimeOK(st) {
		// The typed rules: the rules generated once more for the typed runtime.
		g.table = "trules"
		g.rules()
		g.table = "rules"
		result := t.dconv(st, "v", "a") // before the converters are written
		t.typedRuntime(&b)
		fmt.Fprintf(&b, `%s// It builds the values directly, without the nodes Parse returns.
func ParseAST(input string, unit ...Unit) (%s, error) {
	a := &tslabs{}
	return tparse(trules[%d], input, unit, a, func(v any) %s { return %s })
}
`, doc, t.goType(st), start.id, t.goType(st), result)
		return b.String(), t.span, nil
	}
	result := t.conv(st, "n") // before the converters are written: it may need a list converter
	newConv := ""
	if strings.Contains(result, "a.") {
		newConv = "\ta := &astConv{}\n"
	}
	t.converters(&b)
	fmt.Fprintf(&b, `%sfunc ParseAST(input string, unit ...Unit) (%s, error) {
	n, err := Parse(input, unit...)
%s	return %s, err
}
`, doc, t.goType(st), newConv, result)
	return b.String(), t.span, nil
}

type typedGen struct {
	g    *generator
	info *typeInfo
	// names maps grammar type names to Go type names, and span is the Go name of the helper type
	// Span; unions maps the written form of a union type to the Go name of the alias that declares
	// it.
	names  map[string]string
	span   string
	unions map[string]string
	// lists maps the Go type of a list to the name of its converter (from nodes); tlists, to the
	// name of its converter from values of the typed runtime.
	lists      map[string]string
	listOrder  []listConv
	tlists     map[string]string
	tlistOrder []listConv
	structs    []string // struct type names, sorted
	terms      []string // terminal type names, sorted
	aliases    []string // union alias names, sorted
}

type listConv struct {
	name string
	elem ty
}

// runtimeNames are the exported names of the generated runtime, which typed values must avoid.
var runtimeNames = map[string]bool{
	"Node": true, "NodeField": true, "Fields": true, "SyntaxError": true, "SyntaxErrors": true, "Unit": true,
	"CodePoints": true, "Bytes": true, "Parse": true, "ParseRule": true, "ParseAST": true, "Recognize": true, "Match": true, "Error": true,
}

// name assigns Go names: grammar type names stay unless they clash with the runtime (then "_" is
// appended), and the helper type Span gets a name no type or field uses.
func (t *typedGen) name() {
	used := map[string]bool{}
	for n := range runtimeNames {
		used[n] = true
	}
	var all []string
	for name := range t.g.prog.types {
		all = append(all, name)
	}
	sort.Strings(all)
	for _, name := range all {
		goName := name
		for used[goName] {
			goName += "_"
		}
		used[goName] = true
		t.names[name] = goName
	}
	for _, name := range all {
		switch t.g.prog.types[name].(type) {
		case *grammar.StructSpec:
			t.structs = append(t.structs, name)
			for _, f := range t.info.fields[name] {
				used[f.name] = true
			}
		case *grammar.TerminalSpec:
			t.terms = append(t.terms, name)
		case *grammar.AliasSpec:
			if u, ok := t.info.aliases[name].(unionTy); ok && t.nodeUnion(u) {
				if _, dup := t.unions[u.String()]; !dup {
					t.unions[u.String()] = t.names[name]
					t.aliases = append(t.aliases, name)
				}
			}
		}
	}
	span := "Span"
	for used[span] {
		span += "_"
	}
	t.span = span // a field of its own: a grammar type may be named Span
}

// nodeUnion reports whether every member of the union is a node type (so it can be an interface).
func (t *typedGen) nodeUnion(u unionTy) bool {
	for _, a := range u.alts {
		if !isNode(a) {
			return false
		}
	}
	return true
}

// cstUnion reports whether the union has members that stay *Node.
func cstUnion(u unionTy) bool {
	for _, a := range u.alts {
		switch a := a.(type) {
		case namedTy:
			if a.kind == 'c' && a != tyMatch && a != tyError {
				return true
			}
		case basicTy, listTy, recordTy:
			return true
		}
	}
	return false
}

// goType returns the Go type of values of type x.
func (t *typedGen) goType(x ty) string {
	switch x := x.(type) {
	case basicTy:
		switch x {
		case tyInt, tyString, tyBool:
			return string(x)
		}
		return "any"
	case namedTy:
		switch {
		case x == tyMatch:
			return "*Match"
		case x == tyError:
			return "*Error"
		case x.kind == 'c':
			return "*Node"
		}
		return "*" + t.names[x.name]
	case listTy:
		return "[]" + t.goType(x.elem)
	case optTy:
		if b, ok := x.elem.(basicTy); ok && (b == tyInt || b == tyString || b == tyBool) {
			return "*" + string(b)
		}
		return t.goType(x.elem)
	case unionTy:
		if name, ok := t.unions[x.String()]; ok {
			return name
		}
		return "any"
	case recordTy:
		return "*Node"
	}
	return "any"
}

// conv returns a Go expression of type goType(x) converting the value v (an expression of
// type any or *Node). Typed values are allocated by the converter a (astConv).
func (t *typedGen) conv(x ty, v string) string {
	switch x := x.(type) {
	case basicTy:
		switch x {
		case tyInt:
			return "astInt(" + v + ")"
		case tyString:
			return "astString(" + v + ")"
		case tyBool:
			return "astBool(" + v + ")"
		}
		return "a.anyValue(" + v + ")"
	case namedTy:
		switch {
		case x == tyMatch:
			return "a.toMatch(astNode(" + v + "))"
		case x == tyError:
			return "a.toError(astNode(" + v + "))"
		case x.kind == 'c':
			return "astNode(" + v + ")"
		}
		return "a.to" + t.names[x.name] + "(astNode(" + v + "))"
	case listTy:
		goType := t.goType(x)
		name, ok := t.lists[goType]
		if !ok {
			name = fmt.Sprintf("list%d", len(t.lists))
			t.lists[goType] = name
			t.listOrder = append(t.listOrder, listConv{name, x.elem})
		}
		return "a." + name + "(astNode(" + v + "))"
	case optTy:
		if b, ok := x.elem.(basicTy); ok && (b == tyInt || b == tyString || b == tyBool) {
			return "astPtr" + strings.ToUpper(string(b)[:1]) + string(b)[1:] + "(" + v + ")"
		}
		return t.conv(x.elem, v)
	case unionTy:
		if name, ok := t.unions[x.String()]; ok {
			return "a.to" + name + "(astNode(" + v + "))"
		}
		return "a.anyValue(" + v + ")"
	case recordTy:
		return "astNode(" + v + ")"
	}
	return "a.anyValue(" + v + ")"
}

// decls writes the type declarations.
func (t *typedGen) decls(b *strings.Builder) {
	span := t.span
	b.WriteString("// --- Typed values (pego gen -types; Span, Match and Error are in the typed runtime above) ---\n\n")
	for _, name := range t.terms {
		fmt.Fprintf(b, "// %s is the terminal type %s.\n", t.names[name], name)
		fmt.Fprintf(b, "type %s struct {\n\t%s\n\tText string\n}\n\n", t.names[name], span)
	}
	for _, name := range t.structs {
		fmt.Fprintf(b, "// %s is the struct type %s.\n", t.names[name], name)
		fmt.Fprintf(b, "type %s struct {\n\t%s\n", t.names[name], span)
		for _, f := range t.info.fields[name] {
			fmt.Fprintf(b, "\t%s %s\n", f.name, t.goType(f.t))
		}
		b.WriteString("}\n\n")
	}
	// Unions: an interface with a marker method that each member (and Error) implements.
	impl := map[string][]string{} // Go type (without *) -> markers
	for _, name := range t.aliases {
		u := t.info.aliases[name].(unionTy)
		goName := t.names[name]
		fmt.Fprintf(b, "// %s is the union type %s = %s. Error (left by #recover) also implements it.\n", goName, name, u)
		fmt.Fprintf(b, "type %s interface{ is%s() }\n\n", goName, goName)
		members := map[string]bool{"Error": true}
		for _, a := range u.alts {
			if n, ok := a.(namedTy); ok {
				switch {
				case n == tyMatch:
					members["Match"] = true
				case n.kind == 's' || n.kind == 't':
					members[t.names[n.name]] = true
				}
			}
		}
		if cstUnion(u) {
			members["Node"] = true
		}
		for m := range members {
			impl[m] = append(impl[m], goName)
		}
	}
	var types []string
	for m := range impl {
		types = append(types, m)
	}
	sort.Strings(types)
	for _, m := range types {
		markers := impl[m]
		sort.Strings(markers)
		for _, u := range markers {
			fmt.Fprintf(b, "func (*%s) is%s() {}\n", m, u)
		}
	}
	b.WriteString("\n")
}

// converters writes the converter type and its methods. The converter allocates typed values of
// each type, and the elements of lists, in chunks, as the parser does nodes.
func (t *typedGen) converters(b *strings.Builder) {
	span := t.span
	b.WriteString(astHelpers)
	fmt.Fprintf(b, `func (a *astConv) toMatch(n *Node) *Match {
	if n == nil || n.Type != "Match" {
		return nil
	}
	v := astNew(&a.match)
	*v = Match{%[1]s: %[1]s{n.Start, n.End}, Text: n.Text}
	return v
}

func (a *astConv) toError(n *Node) *Error {
	if n == nil || n.Type != "Error" {
		return nil
	}
	msg, _ := n.Field("message").(string)
	v := astNew(&a.error)
	*v = Error{%[1]s: %[1]s{n.Start, n.End}, Text: n.Text, Message: msg}
	return v
}

`, span)
	b.WriteString("// anyValue converts a value of a statically unknown type into the typed value of its type (CST\n// nodes stay *Node).\nfunc (a *astConv) anyValue(v any) any {\n\tn, ok := v.(*Node)\n\tif !ok || n == nil {\n\t\treturn v\n\t}\n\tswitch n.Type {\n")
	b.WriteString("\tcase \"Match\":\n\t\treturn a.toMatch(n)\n\tcase \"Error\":\n\t\treturn a.toError(n)\n")
	for _, name := range append(append([]string(nil), t.terms...), t.structs...) {
		fmt.Fprintf(b, "\tcase %q:\n\t\treturn a.to%s(n)\n", name, t.names[name])
	}
	b.WriteString("\t}\n\treturn n\n}\n\n")
	for _, name := range t.terms {
		fmt.Fprintf(b, "func (a *astConv) to%[1]s(n *Node) *%[1]s {\n\tif n == nil || n.Type != %[2]q {\n\t\treturn nil\n\t}\n\tv := astNew(&a.t%[1]s)\n\t*v = %[1]s{%[3]s: %[3]s{n.Start, n.End}, Text: n.Text}\n\treturn v\n}\n\n", t.names[name], name, span)
	}
	for _, name := range t.structs {
		fmt.Fprintf(b, "func (a *astConv) to%[1]s(n *Node) *%[1]s {\n\tif n == nil || n.Type != %[2]q {\n\t\treturn nil\n\t}\n\tv := astNew(&a.t%[1]s)\n\tv.%[3]s = %[3]s{n.Start, n.End}\n", t.names[name], name, span)
		if len(t.info.fields[name]) > 0 {
			// Fields are set in the order the action set them; look each one up.
			for _, f := range t.info.fields[name] {
				fmt.Fprintf(b, "\tv.%s = %s\n", f.name, t.conv(f.t, fmt.Sprintf("n.Field(%q)", f.name)))
			}
		}
		b.WriteString("\treturn v\n}\n\n")
	}
	for _, name := range t.aliases {
		u := t.info.aliases[name].(unionTy)
		goName := t.names[name]
		fmt.Fprintf(b, "func (a *astConv) to%[1]s(n *Node) %[1]s {\n\tif n == nil {\n\t\treturn nil\n\t}\n\tswitch n.Type {\n\tcase \"Error\":\n\t\treturn a.toError(n)\n", goName)
		for _, m := range u.alts {
			if m, ok := m.(namedTy); ok {
				switch {
				case m == tyMatch:
					b.WriteString("\tcase \"Match\":\n\t\treturn a.toMatch(n)\n")
				case m.kind == 's' || m.kind == 't':
					fmt.Fprintf(b, "\tcase %q:\n\t\treturn a.to%s(n)\n", m.name, t.names[m.name])
				}
			}
		}
		if cstUnion(u) {
			b.WriteString("\t}\n\treturn n\n}\n\n")
		} else {
			b.WriteString("\t}\n\treturn nil\n}\n\n")
		}
	}
	// List converters (more may be added while writing them, for lists of lists).
	for i := 0; i < len(t.listOrder); i++ {
		l := t.listOrder[i]
		elem := t.goType(l.elem)
		conv := t.conv(l.elem, "c")
		fmt.Fprintf(b, "func (a *astConv) %[1]s(n *Node) []%[2]s {\n\tif n == nil {\n\t\treturn nil\n\t}\n\tout := astSlice(&a.%[1]sChunk, len(n.Children))\n\tfor i, c := range n.Children {\n\t\tout[i] = %[3]s\n\t}\n\treturn out\n}\n\n", l.name, elem, conv)
	}
	// The converter, written last because the list converters are known only now: a chunk of
	// values per type and of elements per list type.
	b.WriteString("// astConv converts nodes into typed values.\ntype astConv struct {\n\tmatch []Match\n\terror []Error\n")
	for _, name := range append(append([]string(nil), t.terms...), t.structs...) {
		fmt.Fprintf(b, "\tt%s []%s\n", t.names[name], t.names[name])
	}
	for _, l := range t.listOrder {
		fmt.Fprintf(b, "\t%sChunk []%s\n", l.name, t.goType(l.elem))
	}
	b.WriteString("}\n\n")
}

// runtimeOK reports whether the typed runtime can build every value ParseAST may return: the
// result type and the field types of every struct type have Go types of their own (no CST
// node types, node, terminal or any).
func (t *typedGen) runtimeOK(st ty) bool {
	if !t.representable(st) || t.readsFields() {
		return false
	}
	for _, name := range t.structs {
		for _, f := range t.info.fields[name] {
			if !t.representable(f.t) {
				return false
			}
		}
	}
	return true
}

// readsFields reports whether an action or predicate may read a field of a struct: a member
// access whose name is a field of some struct type. A typed value keeps its fields converted to
// their Go types (a list as a slice, an Error where a struct is expected as nil, an omitted int as
// 0), so reading them back would not give what the node's fields hold.
func (t *typedGen) readsFields() bool {
	fields := map[string]bool{}
	for _, name := range t.structs {
		for _, f := range t.info.fields[name] {
			fields[f.name] = true
		}
	}
	found := false
	term := func(x grammar.Term) {
		walkTerm(x, func(y grammar.Term) {
			if m, ok := y.(*grammar.Member); ok && fields[m.Name] {
				found = true
			}
		})
	}
	var expr func(e grammar.Expr)
	expr = func(e grammar.Expr) {
		walkExpr(e, func(x grammar.Expr) {
			switch x := x.(type) {
			case *grammar.Predicate:
				term(x.Term)
			case *grammar.Pratt:
				for _, o := range x.Operands {
					expr(o.Expr)
					if o.Action != nil {
						term(o.Action)
					}
				}
				for _, l := range x.Levels {
					for _, op := range l.Operators {
						expr(op.Expr)
						if op.Action != nil {
							term(op.Action)
						}
					}
				}
			}
		})
	}
	for _, r := range t.g.prog.rules {
		expr(r.def.Expr)
		if r.def.Action != nil {
			term(r.def.Action)
		}
	}
	return found
}

func (t *typedGen) representable(x ty) bool {
	switch x := x.(type) {
	case basicTy:
		return x == tyInt || x == tyString || x == tyBool
	case namedTy:
		return x == tyMatch || x == tyError || x.kind == 's' || x.kind == 't'
	case listTy:
		return t.representable(x.elem)
	case optTy:
		return t.representable(x.elem)
	case unionTy:
		if _, ok := t.unions[x.String()]; !ok || cstUnion(x) {
			return false
		}
		for _, a := range x.alts {
			if !t.representable(a) {
				return false
			}
		}
		return true
	}
	return false
}

// valueMethods writes the methods that make the struct and terminal types values of the typed
// runtime (tval).
func (t *typedGen) valueMethods(b *strings.Builder) {
	for _, name := range t.terms {
		g := t.names[name]
		fmt.Fprintf(b, "func (v *%[1]s) tname() string { return %[2]q }\nfunc (v *%[1]s) ttext() (string, bool) { return v.Text, true }\n", g, name)
		fmt.Fprintf(b, "func (v *%[1]s) tkids() []any { return nil }\nfunc (v *%[1]s) tfield(string) (any, bool) { return nil, true }\n", g)
		fmt.Fprintf(b, "func (v *%[1]s) tstruct() bool { return false }\nfunc (v *%[1]s) tfresh() bool { return false }\nfunc (v *%[1]s) tsetFresh(bool) {}\n\n", g)
	}
	for _, name := range t.structs {
		g := t.names[name]
		fmt.Fprintf(b, "func (v *%[1]s) tname() string { return %[2]q }\nfunc (v *%[1]s) ttext() (string, bool) { return \"\", false }\n", g, name)
		fmt.Fprintf(b, "func (v *%[1]s) tkids() []any { return nil }\nfunc (v *%[1]s) tstruct() bool { return true }\n", g)
		fmt.Fprintf(b, "func (v *%[1]s) tfresh() bool { return false }\nfunc (v *%[1]s) tsetFresh(bool) {}\n", g)
		fmt.Fprintf(b, "func (v *%s) tfield(name string) (any, bool) {\n\tswitch name {\n", g)
		for _, f := range t.info.fields[name] {
			if t.representable(f.t) {
				fmt.Fprintf(b, "\tcase %q:\n\t\treturn %s, true\n", f.name, t.back(f.t, "v."+f.name))
			} else {
				fmt.Fprintf(b, "\tcase %q:\n\t\treturn nil, true\n", f.name)
			}
		}
		b.WriteString("\t}\n\treturn nil, false\n}\n\n")
	}
}

// back returns a Go expression converting x, an expression of the Go type of type ty, into a
// value of the typed runtime.
func (t *typedGen) back(x ty, v string) string {
	switch x := x.(type) {
	case basicTy:
		return "any(" + v + ")"
	case namedTy:
		return "tptr(" + v + ")"
	case unionTy:
		return "any(" + v + ")"
	case optTy:
		if b, ok := x.elem.(basicTy); ok && (b == tyInt || b == tyString || b == tyBool) {
			return "tderef(" + v + ")"
		}
		return t.back(x.elem, v)
	case listTy:
		return fmt.Sprintf("func() any { if %[1]s == nil { return nil }; kids := make([]any, len(%[1]s)); for i, x := range %[1]s { kids[i] = %[2]s }; return tlistOf(kids) }()", v, t.back(x.elem, "x"))
	}
	return "nil"
}

// dconv returns a Go expression of type goType(x) converting v, a value of the typed runtime,
// with the chunks a (a *tslabs).
func (t *typedGen) dconv(x ty, v, a string) string {
	switch x := x.(type) {
	case basicTy:
		switch x {
		case tyInt:
			return "astInt(" + v + ")"
		case tyString:
			return "astString(" + v + ")"
		case tyBool:
			return "astBool(" + v + ")"
		}
	case namedTy:
		switch x {
		case tyMatch:
			return "tpubMatch(" + v + ")"
		case tyError:
			return "tpubError(" + v + ")"
		}
		return "tAs[" + t.goType(x) + "](" + v + ")"
	case unionTy:
		return "tAs[" + t.goType(x) + "](tpub(" + v + "))"
	case optTy:
		if b, ok := x.elem.(basicTy); ok && (b == tyInt || b == tyString || b == tyBool) {
			return "astPtr" + strings.ToUpper(string(b)[:1]) + string(b)[1:] + "(" + v + ")"
		}
		return t.dconv(x.elem, v, a)
	case listTy:
		goType := t.goType(x)
		name, ok := t.tlists[goType]
		if !ok {
			name = fmt.Sprintf("tl%d", len(t.tlists))
			t.tlists[goType] = name
			t.tlistOrder = append(t.tlistOrder, listConv{name, x.elem})
		}
		return name + "(" + a + ", " + v + ")"
	}
	return "nil"
}

// typedRuntime writes the constructors of the struct and terminal types and the list converters
// of the typed runtime, and the chunks they allocate from.
func (t *typedGen) typedRuntime(b *strings.Builder) {
	span := t.span
	b.WriteString(astHelpers)
	var reg strings.Builder
	for _, r := range append(append([]*rule(nil), t.g.prog.rules...), t.g.prog.twins...) {
		if r.terminalType == "" {
			continue
		}
		g := t.names[r.terminalType]
		fmt.Fprintf(&reg, "\ttrules[%d].term = func(p *tparser, start, end int, text string) tval {\n\t\tv := astNew(&p.ext.(*tslabs).t%s)\n\t\t*v = %s{%s: %s{start, end}, Text: text}\n\t\treturn v\n\t}\n", r.id, g, g, span, span)
	}
	var cons strings.Builder
	for _, name := range t.structs {
		g := t.names[name]
		fields := t.info.fields[name]
		params := []string{"c *tctx", "final bool"}
		for i := range fields {
			params = append(params, fmt.Sprintf("f%d any", i))
		}
		fmt.Fprintf(&cons, "// tmk_%s makes a %s in an action (newStruct in the typed runtime).\nfunc tmk_%s(%s) any {\n\ta := c.p.ext.(*tslabs)\n\tv := astNew(&a.t%s)\n", name, name, name, strings.Join(params, ", "), g)
		for i, f := range fields {
			fmt.Fprintf(&cons, "\tv.%s = %s\n", f.name, t.dconv(f.t, fmt.Sprintf("f%d", i), "a"))
		}
		fmt.Fprintf(&cons, "\tif final {\n\t\tv.%[1]s = %[1]s{c.start, c.end}\n\t\treturn v\n\t}\n\tfirst, start, end := true, 0, 0\n", span)
		for i := range fields {
			fmt.Fprintf(&cons, "\tspanOf(f%d, &first, &start, &end)\n", i)
		}
		cons.WriteString("\treturn c.made(v, first, start, end)\n}\n\n")
	}
	// List converters (more may be added while writing them, for lists of lists).
	var convs strings.Builder
	for i := 0; i < len(t.tlistOrder); i++ {
		l := t.tlistOrder[i]
		elem := t.goType(l.elem)
		fmt.Fprintf(&convs, "func %s(a *tslabs, v any) []%s {\n\tif v == nil {\n\t\treturn nil\n\t}\n\tn, _ := v.(*tnode)\n\tif n == nil {\n\t\treturn []%[2]s{} // another node (an Error), as conversion makes an empty list of it\n\t}\n\tout := astSlice(&a.%sChunk, len(n.kids))\n\tfor i, c := range n.kids {\n\t\tout[i] = %s\n\t}\n\treturn out\n}\n\n", l.name, elem, l.name, t.dconv(l.elem, "c", "a"))
	}
	b.WriteString("// tinit sets up the typed rules (called at the end of init, after the rule tables).\nfunc tinit() {\n" + reg.String() + "}\n\n")
	b.WriteString(cons.String())
	b.WriteString(convs.String())
	b.WriteString("// tslabs holds the chunks the typed values of a parse are allocated from.\ntype tslabs struct {\n")
	for _, name := range append(append([]string(nil), t.terms...), t.structs...) {
		fmt.Fprintf(b, "\tt%s []%s\n", t.names[name], t.names[name])
	}
	for _, l := range t.tlistOrder {
		fmt.Fprintf(b, "\t%sChunk []%s\n", l.name, t.goType(l.elem))
	}
	b.WriteString("}\n\n")
}

// astHelpers are the conversion helpers of typed values.
const astHelpers = `func astNode(v any) *Node { n, _ := v.(*Node); return n }
func astInt(v any) int       { i, _ := v.(int); return i }
func astString(v any) string { s, _ := v.(string); return s }
func astBool(v any) bool     { x, _ := v.(bool); return x }
func astPtrInt(v any) *int {
	if i, ok := v.(int); ok {
		return &i
	}
	return nil
}
func astPtrString(v any) *string {
	if s, ok := v.(string); ok {
		return &s
	}
	return nil
}
func astPtrBool(v any) *bool {
	if x, ok := v.(bool); ok {
		return &x
	}
	return nil
}

// astNew returns a new zero value from the chunk *s.
func astNew[T any](s *[]T) *T {
	if len(*s) == 0 {
		*s = make([]T, 256)
	}
	v := &(*s)[0]
	*s = (*s)[1:]
	return v
}

// astSlice returns a slice of n zero values from the chunk *s. Its capacity is n, so appending
// to it does not overwrite other slices.
func astSlice[T any](s *[]T, n int) []T {
	if n == 0 {
		return []T{} // an empty list, not nil
	}
	if n > 256 {
		return make([]T, n)
	}
	if len(*s) < n {
		*s = make([]T, 1024)
	}
	v := (*s)[:n:n]
	*s = (*s)[n:]
	return v
}

`
