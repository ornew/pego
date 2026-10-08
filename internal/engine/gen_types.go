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

// typedGo returns the Go source of the typed values for a parser starting at rule start.
func (g *generator) typedGo(start *rule) (string, error) {
	info := g.prog.typed
	if info == nil {
		return "", fmt.Errorf("typed values need the grammar's types (type checking did not run)")
	}
	t := &typedGen{g: g, info: info, names: map[string]string{}, unions: map[string]string{}, lists: map[string]string{}}
	t.name()
	st := info.rules[start.name]
	if st == nil {
		st = tyAny
	}
	result := t.conv(st, "n") // before the converters are written: it may need a list converter
	newConv := ""
	if strings.Contains(result, "a.") {
		newConv = "\ta := &astConv{}\n"
	}
	var b strings.Builder
	t.decls(&b)
	t.converters(&b)
	fmt.Fprintf(&b, `// ParseAST parses the input like Parse and returns the result as typed values. If the parse recovered
// from errors, the result is returned together with SyntaxErrors; an Error node where a struct or
// terminal type is expected becomes nil.
func ParseAST(input string, unit ...Unit) (%s, error) {
	n, err := Parse(input, unit...)
%s	return %s, err
}
`, t.goType(st), newConv, result)
	return b.String(), nil
}

type typedGen struct {
	g    *generator
	info *typeInfo
	// names maps grammar type names (and Span) to Go type names; unions maps the written form of
	// a union type to the Go name of the alias that declares it.
	names  map[string]string
	unions map[string]string
	// lists maps the Go type of a list to the name of its converter.
	lists     map[string]string
	listOrder []listConv
	structs   []string // struct type names, sorted
	terms     []string // terminal type names, sorted
	aliases   []string // union alias names, sorted
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
	t.names["Span"] = span
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
	span := t.names["Span"]
	b.WriteString("// --- Typed values (pego gen -types) ---\n\n")
	fmt.Fprintf(b, "// %s is the range of input a typed value covers, in the parse's position unit (End is exclusive).\n", span)
	fmt.Fprintf(b, "type %s struct{ Start, End int }\n\n", span)
	b.WriteString("// Match is a terminal made by a literal, a character class, ., @a or _.\n")
	fmt.Fprintf(b, "type Match struct {\n\t%s\n\tText string\n}\n\n", span)
	b.WriteString("// Error is input skipped by error recovery (#recover).\n")
	fmt.Fprintf(b, "type Error struct {\n\t%s\n\tText    string\n\tMessage string\n}\n\n", span)
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
	span := t.names["Span"]
	b.WriteString(`func astNode(v any) *Node { n, _ := v.(*Node); return n }
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

`)
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
