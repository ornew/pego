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
//   - a terminal type T becomes a struct T{Span; Text string}, a string node (Match) a
//     TNode[string] and an error value Error;
//   - a union type U = A | B becomes an interface U that A, B and Error implement (and TNode, if
//     the union has other node members);
//   - []T becomes a slice, *T a pointer (or the nil value of an interface), and int, string and
//     bool stay as they are;
//   - tuple and list nodes (Seq, List, Operator) become TNode[any], whose Value is a []any of the
//     elements, and node, terminal and any become any (holding the converted value, with tuple
//     and list values as []any).
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
	t := &typedGen{g: g, info: info, names: map[string]string{}, unions: map[string]string{}, lists: map[string]string{}, tlists: map[string]string{}, tupleSizes: map[int]bool{}}
	t.valueResolver = newValueResolver(g.prog, func(x ty) bool { _, ok := t.unions[x.String()]; return ok })
	t.name()
	g.typedSpan = t.span
	ruleType := t.ruleType
	var b, entries, named strings.Builder
	t.decls(&b)
	t.valueMethods(&b)
	doc := `// ParseAST parses the input like Parse and returns the result as typed values. If the parse recovered
// from errors, the result is returned together with SyntaxErrors; an Error node where a struct or
// terminal type is expected becomes nil.
`
	st := ruleType(start)
	directStart := !conv && t.runtimeOK(st)
	g.directTypedDocument = g.document && directStart && !g.disableDirectTypedDocument
	needTyped, needNodes := false, false
	if directStart {
		needTyped = true
		result := t.dconv(st, "v", "a")
		fmt.Fprintf(&entries, `%s// It builds the values directly, without the nodes Parse returns.
func ParseAST(input string, opts ...ParseOption) (%s, error) {
	a := &tslabs{}
		return tparse(trules[%d], input, opts, a, func(v any) %s { return %s }%s)
}

`, doc, t.goType(st), start.id, t.goType(st), result, t.outputPublication(st))
	} else {
		needNodes = true
		result := t.conv(st, "n")
		newConv := ""
		if strings.Contains(result, "a.") {
			newConv = "\t\ta := &astConv{control: c}\n"
		}
		returnResult := "\t\treturn " + result + "\n"
		if (g.enableTypedOutputSweep || g.enableBoundedTypedOutputSweep) && newConv != "" {
			returnResult = "\t\tres := " + result + "\n\t\ta.publish(res, err)\n\t\treturn res\n"
		}
		fmt.Fprintf(&entries, `%sfunc ParseAST(input string, opts ...ParseOption) (%s, error) {
 convert := func(n *Node, c *workControl, err error) %s {
%s%s }
 o, err := resolveOptions(opts)
 if err != nil { return convert(nil, nil, err), err }
 return parseConverted(rules[%d], nseen, input, o, convert)
}

`, doc, t.goType(st), t.goType(st), newConv, returnResult, start.id)
	}
	if g.document && !g.directTypedDocument {
		// A Document reuses Node results. Convert only the finalized result, with
		// the same invocation controller and cleanup boundary as its Node parse.
		needNodes = true
		result := t.conv(st, "n")
		newConv := ""
		if strings.Contains(result, "a.") {
			newConv = "\t\ta := &astConv{control: c}\n"
		}
		returnResult := "\t\treturn " + result + "\n"
		if (g.enableTypedOutputSweep || g.enableBoundedTypedOutputSweep) && newConv != "" {
			returnResult = "\t\tres := " + result + "\n\t\ta.publish(res, parseErr)\n\t\treturn res\n"
		}
		fmt.Fprintf(&entries, `// ParseAST converts the current Document result to typed values. Typed values
// belong to this call; CST Node members may still share the Document's Node graph.
func (d *Document) ParseAST(opts ...ParseOption) (%s, error) {
%s o, err := d.resolveParseOptions(opts)
 if err != nil { var zero %s; return zero, err }
 return documentParseConverted(d.state, o, func(n *Node, c *workControl, parseErr error) %s {
%s%s })
}

`, t.goType(st), generatedDocumentParseGuard(g.observability, t.goType(st)), t.goType(st), t.goType(st), newConv, returnResult)
	}
	if r := g.streamStart; r != nil {
		// Elements are converted from the committed nodes that ParseStream
		// emits, each with a converter of its own, so that typed values
		// share nothing with later elements.
		needNodes = true
		et, ok := info.streams[r.name]
		if !ok {
			return "", "", fmt.Errorf("internal error: no element type for the #stream repetition of %s", r.name)
		}
		if _, ok := t.ruleType(r).(vrule); ok {
			if v, ok := streamValue(g.prog, r.def); ok {
				et = t.resolveValue(v)
			}
		}
		result := t.conv(et, "n")
		newConv := ""
		if strings.Contains(result, "a.") {
			newConv = "\t\t\ta := &astConv{control: c}\n"
		}
		fmt.Fprintf(&entries, `// StreamValue is the typed value of an element of the #stream repetition of the rule %[1]s.
type StreamValue = %[2]s

// ParseStreamAST parses the input read from r like ParseStream and passes each committed element of the
// #stream repetition of the rule %[1]s to emit as a typed value (the zero value if the element has no
// value). The values are independent of the parser and of later elements. Committed elements are final
// even if the parse fails later. A non-EOF error from r or an error returned by emit ends the parse and
// is returned unchanged. A nil emit collects the repetition instead.
func ParseStreamAST(r io.Reader, emit func(StreamValue) error, opts ...ParseOption) error {
	var e func(*Node, *workControl) error
	if emit != nil {
		e = func(n *Node, c *workControl) error {
			if n == nil {
				var zero StreamValue
				return emit(zero)
			}
%[3]s			return emit(%[4]s)
		}
	}
	return parseStreamWork(streamRule, nseen, r, e, opts)
}

// StreamASTIterator returns the typed elements of a stream one at a time, like StreamIterator.
type StreamASTIterator struct{ pull *streamPull[StreamValue] }

// IterateAST returns an iterator over the typed elements that ParseStreamAST would pass to emit, parsing
// only as far as each call to Next needs. Options are validated before it returns; r is first read by Next.
func IterateAST(r io.Reader, opts ...ParseOption) (*StreamASTIterator, error) {
	pull, err := iterateStream(opts, func(emit func(StreamValue) error) error { return ParseStreamAST(r, emit, opts...) })
	if err != nil {
		return nil, err
	}
	return &StreamASTIterator{pull}, nil
}

// Next returns (value, true, nil) for each committed element, with the zero value for an element without
// a value. At the end of a clean stream it returns ok false and a nil error, and after a failure the error;
// later calls return the same result.
func (it *StreamASTIterator) Next() (StreamValue, bool, error) { return it.pull.Next() }

// Close abandons unfinished parsing, like StreamIterator.Close.
func (it *StreamASTIterator) Close() { it.pull.Close() }

`, r.name, t.goType(et), newConv, result)
	}
	named.WriteString("// ParseRuleAST parses the whole input with the named rule and returns its typed value.\n")
	named.WriteString("// Recovered values accompany SyntaxErrors. Nil results and failed parses return the selected type's zero value.\n// Invalid options and undefined names return nil with an error. Rule result types:\n")
	for _, r := range g.prog.rules {
		fmt.Fprintf(&named, "//\t%s: %s\n", r.name, t.goType(ruleType(r)))
	}
	named.WriteString(`func ParseRuleAST(name, input string, opts ...ParseOption) (any, error) {
	o, err := resolveOptions(opts)
	if err != nil {
		return nil, err
	}
	switch name {
`)
	for _, r := range g.prog.rules {
		x := ruleType(r)
		fmt.Fprintf(&named, "\tcase %q:\n", r.name)
		if !conv && t.runtimeOK(x) {
			needTyped = true
			result := t.dconv(x, "v", "a")
			fmt.Fprintf(&named, "\t\ta := &tslabs{}\n\t\treturn tparseResolved(trules[%d], input, o, a, func(v any) %s { return %s }%s)\n", r.id, t.goType(x), result, t.outputPublication(x))
		} else {
			needNodes = true
			result := t.conv(x, "n")
			fmt.Fprintf(&named, "\t\treturn parseConverted(rules[%d], nseen, input, o, func(n *Node, c *workControl, err error) %s {\n", r.id, t.goType(x))
			fmt.Fprintf(&named, "\t\t\tif n == nil { var zero %s; return zero }\n", t.goType(x))
			if strings.Contains(result, "a.") {
				named.WriteString("\t\t\ta := &astConv{control: c}\n")
				if g.enableTypedOutputSweep || g.enableBoundedTypedOutputSweep {
					fmt.Fprintf(&named, "\t\t\tres := %s\n\t\t\ta.publish(res, err)\n\t\t\treturn res\n\t\t})\n", result)
					continue
				}
			}
			fmt.Fprintf(&named, "\t\t\treturn %s\n\t\t})\n", result)
		}
	}
	named.WriteString("\tdefault:\n\t\treturn nil, fmt.Errorf(\"rule %s is not defined\", name)\n\t}\n}\n")
	helpers := astHelpers
	if g.enableTypedOutputSweep || g.enableBoundedTypedOutputSweep {
		var err error
		helpers, err = typedOutputSweepHelpers(helpers)
		if err != nil {
			return "", "", err
		}
	}
	b.WriteString(helpers)
	bootstrap := !g.disableTypedOutputBootstrap && !g.enableTypedOutputSweep && !g.enableBoundedTypedOutputSweep
	if g.disableTypedObjectSlabs {
		b.WriteString(astIndividualObjectHelper)
	} else if bootstrap {
		b.WriteString(astBootstrapObjectHelper)
	} else {
		b.WriteString(astObjectSlabHelper)
	}
	if g.disableTypedListSlabs {
		b.WriteString(astIndividualListHelper)
	} else if bootstrap {
		b.WriteString(astBootstrapListHelper)
	} else {
		b.WriteString(astListSlabHelper)
	}
	if g.directTypedDocument {
		_ = t.dconv(st, "v", "a")
	}
	if needTyped {
		// Emit the typed table if any public entry point can use it.
		g.table = "trules"
		g.rules()
		g.table = "rules"
		t.typedRuntime(&b)
	}
	if needNodes {
		t.converters(&b)
	}
	if g.directTypedDocument {
		publication := t.documentClone(&b, st)
		document := typedDocumentSource
		if g.observability {
			var err error
			document, err = generatedTypedDocumentTraceRuntime(document)
			if err != nil {
				return "", "", err
			}
		}
		b.WriteString(document)
		result := t.dconv(st, "v", "a")
		fmt.Fprintf(&entries, `// ParseAST directly parses the Document and returns independently owned typed values.
func (d *Document) ParseAST(opts ...ParseOption) (%s, error) {
%s o, err := d.resolveParseOptions(opts)
 if err != nil { var zero %s; return zero, err }
 a := &tslabs{}
 return typedDocumentParse(d, trules[%d], o, a, func(v any, c *typedDocumentClone) %s {
  value := %s
  return %s
 })
}
`, t.goType(st), generatedDocumentParseGuard(g.observability, t.goType(st)), t.goType(st), start.id, t.goType(st), result, publication)
	}
	if g.enableBoundedTypedOutputSweep {
		t.outputBoundedSweep(&b, needTyped, needNodes)
	} else if g.enableTypedOutputSweep {
		t.outputSweep(&b, needTyped, needNodes)
	}
	t.tupleDecls(&b)
	b.WriteString(entries.String())
	b.WriteString(named.String())
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
	lists       map[string]string
	listOrder   []listConv
	tlists      map[string]string
	tlistOrder  []listConv
	structs     []string // struct type names, sorted
	terms       []string // terminal type names, sorted
	aliases     []string // canonical node union names, sorted
	typeAliases []string // other declared aliases, sorted
	// valueResolver selects the rules whose Go types come from their value types.
	valueResolver
	tupleSizes map[int]bool // the sizes of the tuple types used
	unitUsed   bool
	// nhelpers, dhelpers and chelpers are the helpers converting value-model values from nodes,
	// from values of the typed runtime, and cloning them for Documents.
	nhelpers, dhelpers, chelpers vhelpers
}

type listConv struct {
	name string
	elem ty
}

// runtimeNames are the exported names of the generated runtime, which typed values must avoid.
var runtimeNames = map[string]bool{
	"WalkTree": true, "WalkAction": true, "WalkContinue": true, "WalkSkip": true, "WalkStop": true,
	"WalkVisit": true, "WalkEdge": true, "WalkEdgeKind": true, "WalkRoot": true, "WalkChild": true, "WalkField": true, "WalkListElement": true, "CycleError": true, "Cursor": true,
	"Node": true, "NodeField": true, "Fields": true, "NodeKind": true, "KindString": true, "KindTuple": true, "KindList": true, "KindError": true, "KindStruct": true, "KindTupleValue": true, "KindListValue": true, "KindValue": true, "SyntaxError": true, "SyntaxErrors": true, "Unit": true, "PositionUnit": true,
	"WorkLimits": true, "WorkLimitKind": true, "WorkLimitError": true, "ErrWorkLimit": true, "InputWorkLimit": true, "StepWorkLimit": true, "WithContext": true, "WithWorkLimits": true, "ParseOption": true, "WithUnit": true, "WithMaxDepth": true, "CodePoints": true, "Bytes": true, "Parse": true, "ParseRule": true, "ParseAST": true, "ParseRuleAST": true, "Recognize": true, "Match": true, "Error": true, "TNode": true,
}

// name assigns Go names: grammar type names stay unless they clash with the runtime (then "_" is
// appended), and the helper type Span gets a name no type or field uses.
func (t *typedGen) name() {
	used := map[string]bool{}
	for n := range runtimeNames {
		used[n] = true
	}
	if t.g.observability {
		for _, name := range []string{"TraceKind", "TraceEvent", "TraceEnter", "TraceExit", "TraceSkipped", "WithTrace", "WithTraceFilter", "WithTraceMaxDepth", "WithProfile", "Profile", "ProfileSnapshot", "RuleProfile", "ProfileLocation", "SkipReasonCount"} {
			used[name] = true
		}
	}
	if t.g.streamStart != nil {
		for _, name := range []string{"ParseStream", "ParseStreamAST", "StreamValue", "Iterate", "IterateAST", "StreamIterator",
			"StreamASTIterator", "ErrStreamClosed", "WithDiagnosticCallback", "WithDiagnosticLimit", "TruncatedSyntaxErrors"} {
			used[name] = true
		}
	}
	if t.g.document {
		for _, name := range []string{"Document", "NewDocument", "ParseStats"} {
			used[name] = true
		}
		if t.g.observability {
			used["ErrDocumentBusy"] = true
		}
	}
	var all []string
	for name := range t.g.prog.types {
		all = append(all, name)
	}
	sort.Strings(all)
	for _, name := range all {
		goName := name
		for used[goName] || tupleName(goName) {
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
					continue
				}
			}
			t.typeAliases = append(t.typeAliases, name)
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
		case tyErr:
			return "*Error"
		}
		return "any"
	case vrule:
		return t.goType(t.vres[x.name])
	case tupleTy:
		return t.tupleType(x)
	case nodeTy:
		if x.value == tyTop || x.value == tyAny {
			return "*TNode[any]"
		}
		return "*TNode[" + t.goType(x.value) + "]"
	case namedTy:
		switch {
		case x == tyMatch:
			return "*TNode[string]"
		case x == tyError:
			return "*Error"
		case x.kind == 'c':
			return "*TNode[any]"
		}
		return "*" + t.names[x.name]
	case listTy:
		return "[]" + t.goType(x.elem)
	case optTy:
		if b, ok := x.elem.(basicTy); ok && (b == tyInt || b == tyString || b == tyBool) {
			return "*" + string(b)
		}
		if _, ok := x.elem.(tupleTy); ok {
			return "*" + t.goType(x.elem)
		}
		return t.goType(x.elem)
	case unionTy:
		if name, ok := t.unions[x.String()]; ok {
			return name
		}
		if g, ok := t.unionGoType(x); ok {
			return g
		}
		return "any"
	case recordTy:
		return "*TNode[any]"
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
		case tyErr:
			return "a.toError(astNode(" + v + "))"
		}
		return "a.anyValue(" + v + ")"
	case vrule:
		return "a." + t.vconvHelper(x) + "(" + v + ")"
	case tupleTy:
		if len(x.elems) == 0 {
			t.goType(x)
			return "Unit{}"
		}
		return "a." + t.vconvHelper(x) + "(" + v + ")"
	case nodeTy:
		if x.value == tyTop || x.value == tyAny {
			return "a.toNode(astNode(" + v + "))"
		}
		return "a." + t.vconvHelper(x) + "(" + v + ")"
	case namedTy:
		switch {
		case x == tyMatch:
			return "a.toMatch(astNode(" + v + "))"
		case x == tyError:
			return "a.toError(astNode(" + v + "))"
		case x.kind == 'c':
			return "a.toNode(astNode(" + v + "))"
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
		if _, ok := x.elem.(tupleTy); ok {
			return "a." + t.vconvHelper(x) + "(" + v + ")"
		}
		return t.conv(x.elem, v)
	case unionTy:
		if name, ok := t.unions[x.String()]; ok {
			return "a.to" + name + "(astNode(" + v + "))"
		}
		if _, ok := t.unionGoType(x); ok {
			return t.conv(x.alts[0], v)
		}
		return "a.anyValue(" + v + ")"
	case recordTy:
		return "a.toNode(astNode(" + v + "))"
	}
	return "a.anyValue(" + v + ")"
}

// decls writes the type declarations.
func (t *typedGen) decls(b *strings.Builder) {
	span := t.span
	b.WriteString("// --- Typed values (pego gen -types; Span, TNode and Error are in the typed runtime above) ---\n\n")
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
	for _, name := range t.typeAliases {
		fmt.Fprintf(b, "// %s is the alias type %s ~ %s.\n", t.names[name], name, t.info.aliases[name])
		fmt.Fprintf(b, "type %s = %s\n\n", t.names[name], t.goType(t.info.aliases[name]))
	}
	// Unions: an interface with a marker method that each member (and Error) implements.
	impl := map[string][]string{} // Go type (without *) -> markers
	for _, name := range t.aliases {
		u := t.info.aliases[name].(unionTy)
		goName := t.names[name]
		fmt.Fprintf(b, "// %s is the union type %s ~ %s. Error (left by #recover) also implements it.\n", goName, name, u)
		fmt.Fprintf(b, "type %s interface{ is%s() }\n\n", goName, goName)
		members := map[string]bool{"Error": true}
		for _, a := range u.alts {
			if n, ok := a.(namedTy); ok {
				switch {
				case n == tyMatch:
					members["TNode"] = true
				case n.kind == 's' || n.kind == 't':
					members[t.names[n.name]] = true
				}
			}
		}
		if cstUnion(u) {
			members["TNode"] = true
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
			if m == "TNode" {
				fmt.Fprintf(b, "func (*TNode[T]) is%s() {}\n", u)
				continue
			}
			fmt.Fprintf(b, "func (*%s) is%s() {}\n", m, u)
		}
	}
	b.WriteString("\n")
}

// converters writes the converter type and its methods. The converter allocates typed values of
// each type, and the elements of lists, in chunks, as the parser does nodes.
func (t *typedGen) converters(b *strings.Builder) {
	span := t.span
	fmt.Fprintf(b, `func (a *astConv) toMatch(n *Node) *TNode[string] {
	a.stepWork()
	if n == nil || n.Type() != "Match" {
		return nil
	}
	if v, ok := a.values[n]; ok { return v.(*TNode[string]) }
	v := astNew(&a.match)
	*v = TNode[string]{%[1]s: %[1]s{int(n.start), int(n.end)}, Value: n.text}
	a.remember(n, v)
	return v
}

// toNode converts a node of a statically unknown value: its Value is the text of a string node,
// and a []any of the converted elements of a tuple or list node.
func (a *astConv) toNode(n *Node) *TNode[any] {
	a.stepWork()
	if n == nil || n.Type() == "Error" {
		return nil
	}
	if v, ok := a.values[n]; ok {
		if v, ok := v.(*TNode[any]); ok { return v }
	}
	v := &TNode[any]{%[1]s: %[1]s{int(n.start), int(n.end)}}
	a.remember(n, v)
	if n.IsTerminal() {
		v.Value = n.text
	} else {
		v.Value = a.elems(n)
	}
	return v
}

// elems converts the elements of a tuple or list.
func (a *astConv) elems(n *Node) []any {
	out := make([]any, len(n.children))
	for i, c := range n.children {
		a.stepWork()
		out[i] = a.anyValue(c)
	}
	return out
}

func (a *astConv) toError(n *Node) *Error {
	a.stepWork()
	if n == nil || n.Type() != "Error" {
		return nil
	}
	if v, ok := a.values[n]; ok { return v.(*Error) }
	msg, _ := a.field(n, "message").(string)
	v := astNew(&a.error)
	*v = Error{%[1]s: %[1]s{int(n.start), int(n.end)}, Text: n.text, Message: msg}
	a.remember(n, v)
	return v
}

`, span)
	b.WriteString("// anyValue converts a value of a statically unknown type into the typed value of its type (tuple\n// and list nodes become TNode[any], and tuple and list values []any).\nfunc (a *astConv) anyValue(v any) any {\n\ta.stepWork()\n\tn, ok := v.(*Node)\n\tif !ok {\n\t\treturn v\n\t}\n\tif n == nil {\n\t\treturn nil\n\t}\n\tswitch n.Type() {\n")
	b.WriteString("\tcase \"Match\":\n\t\treturn a.toMatch(n)\n\tcase \"Error\":\n\t\treturn a.toError(n)\n\tcase \"Seq\", \"List\", \"Operator\":\n\t\treturn a.toNode(n)\n\tcase \"(box)\":\n\t\treturn a.anyValue(unboxNode(n))\n\tcase \"(tuple)\", \"(list)\":\n\t\treturn a.elems(n)\n")
	for _, name := range append(append([]string(nil), t.terms...), t.structs...) {
		fmt.Fprintf(b, "\tcase %q:\n\t\treturn a.to%s(n)\n", name, t.names[name])
	}
	b.WriteString("\t}\n\treturn n\n}\n\n")
	for _, name := range t.terms {
		fmt.Fprintf(b, "func (a *astConv) to%[1]s(n *Node) *%[1]s {\n\ta.stepWork()\n\tif n == nil || n.Type() != %[2]q {\n\t\treturn nil\n\t}\n\tif v, ok := a.values[n]; ok { return v.(*%[1]s) }\n\tv := astNew(&a.t%[1]s)\n\t*v = %[1]s{%[3]s: %[3]s{int(n.start), int(n.end)}, Text: n.text}\n\ta.remember(n, v)\n\treturn v\n}\n\n", t.names[name], name, span)
	}
	for _, name := range t.structs {
		fmt.Fprintf(b, "func (a *astConv) to%[1]s(n *Node) *%[1]s {\n\ta.stepWork()\n\tif n == nil || n.Type() != %[2]q {\n\t\treturn nil\n\t}\n\tif v, ok := a.values[n]; ok { return v.(*%[1]s) }\n\tv := astNew(&a.t%[1]s)\n\ta.remember(n, v)\n\tv.%[3]s = %[3]s{int(n.start), int(n.end)}\n", t.names[name], name, span)
		if len(t.info.fields[name]) > 0 {
			// Fields are set in the order the action set them; look each one up.
			for _, f := range t.info.fields[name] {
				fmt.Fprintf(b, "\tv.%s = %s\n", f.name, t.conv(f.t, fmt.Sprintf("a.field(n, %q)", f.name)))
			}
		}
		b.WriteString("\treturn v\n}\n\n")
	}
	for _, name := range t.aliases {
		u := t.info.aliases[name].(unionTy)
		goName := t.names[name]
		fmt.Fprintf(b, "func (a *astConv) to%[1]s(n *Node) %[1]s {\n\ta.stepWork()\n\tif n == nil {\n\t\treturn nil\n\t}\n\tswitch n.Type() {\n\tcase \"Error\":\n\t\treturn a.toError(n)\n", goName)
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
			b.WriteString("\t}\n\treturn a.toNode(n)\n}\n\n")
		} else {
			b.WriteString("\t}\n\treturn nil\n}\n\n")
		}
	}
	// List converters and the helpers of value-model types (more may be added while writing them,
	// for lists of lists).
	b.WriteString("// nodeValue returns the Value of node n: its text, or its converted elements.\nfunc (a *astConv) nodeValue(n *Node) any {\n\tif n.IsTerminal() {\n\t\treturn n.text\n\t}\n\treturn a.elems(n)\n}\n\n")
	helpers := 0
	for i := 0; ; {
		for ; i < len(t.listOrder); i++ {
			l := t.listOrder[i]
			elem := t.goType(l.elem)
			conv := t.conv(l.elem, "unboxNode(c)")
			fmt.Fprintf(b, "func (a *astConv) %[1]s(n *Node) []%[2]s {\n\ta.stepWork()\n\tif n == nil {\n\t\treturn nil\n\t}\n\tout := astSlice(&a.%[1]sChunk, len(n.children))\n\tfor i, c := range n.children {\n\t\ta.stepWork()\n\t\tout[i] = %[3]s\n\t}\n\treturn out\n}\n\n", l.name, elem, conv)
		}
		if !t.nodeHelpers(b, &helpers) && i == len(t.listOrder) {
			break
		}
	}
	// The converter, written last because the list converters are known only now: a chunk of
	// values per type and of elements per list type.
	b.WriteString("// astConv converts nodes into typed values.\ntype astConv struct {\n\tcontrol *workControl\n\tvalues map[*Node]any // preserve aliases when one node appears in multiple fields or lists\n\tmatch astChunk[TNode[string]]\n\terror astChunk[Error]\n")
	for _, name := range append(append([]string(nil), t.terms...), t.structs...) {
		fmt.Fprintf(b, "\tt%s astChunk[%s]\n", t.names[name], t.names[name])
	}
	for _, l := range t.listOrder {
		fmt.Fprintf(b, "\t%sChunk astChunk[%s]\n", l.name, t.goType(l.elem))
	}
	b.WriteString("}\n\n")
	b.WriteString(`func (a *astConv) remember(n *Node, v any) {
 if a.values == nil { a.values = make(map[*Node]any) }
 a.values[n] = v
}
func (a *astConv) stepWork() {
 if workControlsEnabled && a.control != nil { a.control.charge(0, 1) }
}
func (a *astConv) field(n *Node, name string) any {
 if !workControlsEnabled || a.control == nil { return n.field(name) }
 if n == nil { return nil }
 for _, f := range n.fields {
  a.stepWork()
  if f.Name == name { return f.Value }
 }
 return nil
}

`)
}

// runtimeOK reports whether the typed runtime can build every value an entry point may return: the
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

// nodeMember reports whether the member access m reads only .Start, .End or .Value of nodes.
func (t *typedGen) nodeMember(m *grammar.Member) bool {
	return t.info.checks != nil && t.info.checks.nodeMembers[m]
}

// readsFields reports whether an action or predicate may read a field of a struct: a member
// access whose name is a field of some struct type. A typed value keeps its fields converted to
// their Go types (a list as a slice, an Error where a struct is expected as nil, an omitted int as
// 0), so reading them back would not give what the node's fields hold. A string field reads the
// same where every constructor of every struct with a field of that name sets it to a string.
func (t *typedGen) readsFields() bool {
	fields := map[string]bool{}
	// owners maps a field name to the structs with a field of that name, and stringField reports
	// whether every one of them declares it string.
	owners := map[string][]string{}
	stringField := map[string]bool{}
	for _, name := range t.structs {
		for _, f := range t.info.fields[name] {
			if !fields[f.name] {
				stringField[f.name] = true
			}
			fields[f.name] = true
			owners[f.name] = append(owners[f.name], name)
			stringField[f.name] = stringField[f.name] && f.t == tyString
		}
	}
	var terms []grammar.Term
	var expr func(e grammar.Expr)
	expr = func(e grammar.Expr) {
		walkExpr(e, func(x grammar.Expr) {
			switch x := x.(type) {
			case *grammar.Predicate:
				terms = append(terms, x.Term)
			case *grammar.InlineAction:
				if x.Action != nil {
					terms = append(terms, x.Action)
				}
			case *grammar.OperatorsRule:
				for _, o := range x.Operands {
					expr(o.Expr)
					if o.Action != nil {
						terms = append(terms, o.Action)
					}
				}
				for _, l := range x.Levels {
					for _, op := range l.Operators {
						expr(op.Expr)
						if op.Action != nil {
							terms = append(terms, op.Action)
						}
					}
				}
			}
		})
	}
	for _, r := range t.g.prog.rules {
		expr(r.def.Expr)
		if r.def.Action != nil {
			terms = append(terms, r.def.Action)
		}
	}
	// set[s][f] reports whether every constructor of struct s sets field f; a struct built under
	// another name, an alias, sets nothing it can vouch for.
	set := map[string]map[string]bool{}
	aliased := false
	for _, x := range terms {
		walkTerm(x, func(y grammar.Term) {
			n, ok := y.(*grammar.New)
			if !ok {
				return
			}
			if _, ok := t.info.fields[n.Type]; !ok {
				aliased = true
				return
			}
			given := map[string]bool{}
			for _, f := range n.Fields {
				given[f.Name] = true
			}
			if set[n.Type] == nil {
				set[n.Type] = given
				return
			}
			for f := range set[n.Type] {
				set[n.Type][f] = set[n.Type][f] && given[f]
			}
		})
	}
	faithful := func(field string) bool {
		if aliased || !stringField[field] {
			return false
		}
		for _, s := range owners[field] {
			if !set[s][field] {
				return false
			}
		}
		return true
	}
	found := false
	for _, x := range terms {
		walkTerm(x, func(y grammar.Term) {
			if m, ok := y.(*grammar.Member); ok && fields[m.Name] && !t.nodeMember(m) && !faithful(m.Name) {
				found = true
			}
		})
	}
	return found
}

// representable reports whether the typed runtime can build values of type x and read them back
// from a field (back): every type of a value, but not a function.
func (t *typedGen) representable(x ty) bool {
	switch x := x.(type) {
	case basicTy:
		switch x {
		case tyInt, tyString, tyBool, tyNode, tyTerminal, tyAny, tyErr, tyTop:
			return true
		}
		return false
	case namedTy, recordTy, vrule:
		return true
	case tupleTy:
		for _, e := range x.elems {
			if !t.representable(e) {
				return false
			}
		}
		return true
	case nodeTy:
		return t.representable(x.value)
	case listTy:
		return t.representable(x.elem)
	case optTy:
		return t.representable(x.elem)
	case unionTy:
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
		if t.g.goWorkControls {
			fmt.Fprintf(b, "func (v *%s) tfieldWork(p *tparser, name string) (any, bool) {\n\tswitch name {\n", g)
			for _, f := range t.info.fields[name] {
				if t.representable(f.t) {
					fmt.Fprintf(b, "\tcase %q:\n\t\treturn %s, true\n", f.name, t.backWork(f.t, "v."+f.name))
				} else {
					fmt.Fprintf(b, "\tcase %q:\n\t\treturn nil, true\n", f.name)
				}
			}
			b.WriteString("\t}\n\treturn nil, false\n}\n\n")
		}
	}
}

// back returns a Go expression converting x, an expression of the Go type of type ty, into a
// value of the typed runtime.
func (t *typedGen) back(x ty, v string) string {
	switch x := x.(type) {
	case basicTy:
		if x == tyInt || x == tyString || x == tyBool {
			return "any(" + v + ")"
		}
		return "tback(" + v + ")"
	case namedTy:
		if x.kind == 's' || x.kind == 't' {
			return "tptr(" + v + ")"
		}
		return "tback(" + v + ")"
	case recordTy, unionTy:
		return "tback(" + v + ")"
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

// backWork preserves back's values while accounting for list materialization.
func (t *typedGen) backWork(x ty, v string) string {
	switch x := x.(type) {
	case listTy:
		return fmt.Sprintf("func() any { if %[1]s == nil { return nil }; kids := make([]any, len(%[1]s)); for i, x := range %[1]s { p.stepWork(); kids[i] = %[2]s }; return p.listField(kids) }()", v, t.backWork(x.elem, "x"))
	case optTy:
		if b, ok := x.elem.(basicTy); ok && (b == tyInt || b == tyString || b == tyBool) {
			return t.back(x, v)
		}
		return t.backWork(x.elem, v)
	}
	return t.back(x, v)
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
		case tyErr:
			return "tpubError(" + v + ")"
		}
		return "tpub(" + v + ")"
	case vrule:
		return t.vdconvHelper(x) + "(" + a + ", " + v + ")"
	case tupleTy:
		if len(x.elems) == 0 {
			t.goType(x)
			return "Unit{}"
		}
		return t.vdconvHelper(x) + "(" + a + ", " + v + ")"
	case nodeTy:
		if x.value == tyTop || x.value == tyAny {
			return "tpubNode(" + v + ")"
		}
		return t.vdconvHelper(x) + "(" + a + ", " + v + ")"
	case recordTy:
		return "tpubNode(" + v + ")"
	case namedTy:
		switch {
		case x == tyMatch:
			return "tpubMatch(" + v + ")"
		case x == tyError:
			return "tpubError(" + v + ")"
		case x.kind == 'c':
			return "tpubNode(" + v + ")"
		}
		return "tAs[" + t.goType(x) + "](" + v + ")"
	case unionTy:
		if _, ok := t.unions[x.String()]; !ok {
			if _, ok := t.unionGoType(x); ok {
				return t.dconv(x.alts[0], v, a)
			}
			return "tpub(" + v + ")"
		}
		return "tAs[" + t.goType(x) + "](tpub(" + v + "))"
	case optTy:
		if b, ok := x.elem.(basicTy); ok && (b == tyInt || b == tyString || b == tyBool) {
			return "astPtr" + strings.ToUpper(string(b)[:1]) + string(b)[1:] + "(" + v + ")"
		}
		if _, ok := x.elem.(tupleTy); ok {
			return t.vdconvHelper(x) + "(" + a + ", " + v + ")"
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
	var reg strings.Builder
	for _, r := range append(append([]*rule(nil), t.g.prog.rules...), t.g.prog.twins...) {
		if r.terminalType == "" {
			continue
		}
		g := t.names[r.terminalType]
		fmt.Fprintf(&reg, "\ttrules[%d].term = func(p *tparser, start, end int, text string) tval {\n\t\tp.stepWork()\n\t\tv := astNew(&p.ext.(*tslabs).t%s)\n\t\t*v = %s{%s: %s{start, end}, Text: text}\n\t\treturn v\n\t}\n", r.id, g, g, span, span)
	}
	var cons strings.Builder
	for _, name := range t.terms {
		if !t.g.terminalConstructors[name] {
			continue
		}
		g := t.names[name]
		fmt.Fprintf(&cons, "// tmk_%s constructs a terminal with supplied text.\nfunc tmk_%s(c *tctx, final bool, raw any) any {\n\ttext, ok := raw.(string)\n\tif !ok { evalErrorf(\"terminal constructor requires a string, got %%s\", ttypeName(raw)) }\n\ta := c.p.ext.(*tslabs)\n\tv := astNew(&a.t%s)\n\t*v = %s{%s: %s{c.start, c.end}, Text: text}\n\tif final { return v }\n\treturn c.made(v, true, 0, 0)\n}\n\n", name, name, g, g, span, span)
	}
	for _, name := range t.structs {
		g := t.names[name]
		fields := t.info.fields[name]
		params := []string{"c *tctx", "final bool"}
		for i := range fields {
			params = append(params, fmt.Sprintf("f%d any", i))
		}
		fmt.Fprintf(&cons, "// tmk_%s makes a %s in an action (newStruct in the typed runtime).\nfunc tmk_%s(%s) any {\n\tc.p.stepWork()\n\ta := c.p.ext.(*tslabs)\n\tv := astNew(&a.t%s)\n", name, name, name, strings.Join(params, ", "), g)
		for i, f := range fields {
			fmt.Fprintf(&cons, "\tc.p.stepWork()\n\tv.%s = %s\n", f.name, t.dconv(f.t, fmt.Sprintf("f%d", i), "a"))
		}
		fmt.Fprintf(&cons, "\tif final {\n\t\tv.%[1]s = %[1]s{c.start, c.end}\n\t\treturn v\n\t}\n\tfirst, start, end := true, 0, 0\n", span)
		for i := range fields {
			fmt.Fprintf(&cons, "\tc.p.stepWork()\n\tspanOf(f%d, &first, &start, &end)\n", i)
		}
		cons.WriteString("\treturn c.made(v, first, start, end)\n}\n\n")
	}
	// List converters and the helpers of value-model types (more may be added while writing them,
	// for lists of lists).
	var convs strings.Builder
	helpers := 0
	for i := 0; ; {
		for ; i < len(t.tlistOrder); i++ {
			l := t.tlistOrder[i]
			elem := t.goType(l.elem)
			fmt.Fprintf(&convs, "func %s(a *tslabs, v any) []%s {\n\ta.stepWork()\n\tif v == nil {\n\t\treturn nil\n\t}\n\tn, _ := v.(*tnode)\n\tif n == nil {\n\t\treturn []%[2]s{} // another node (an Error), as conversion makes an empty list of it\n\t}\n\tout := astSlice(&a.%sChunk, len(n.kids))\n\tfor i, c := range n.kids {\n\t\ta.stepWork()\n\t\tout[i] = %s\n\t}\n\treturn out\n}\n\n", l.name, elem, l.name, t.dconv(l.elem, "c", "a"))
		}
		if !t.typedHelpers(&convs, &helpers) && i == len(t.tlistOrder) {
			break
		}
	}
	b.WriteString("// tinit sets up the typed rules (called at the end of init, after the rule tables).\nfunc tinit() {\n" + reg.String() + "}\n\n")
	b.WriteString(cons.String())
	b.WriteString(convs.String())
	b.WriteString("// tslabs holds the chunks the typed values of a parse are allocated from.\ntype tslabs struct {\n\tcontrol *workControl\n")
	for _, name := range append(append([]string(nil), t.terms...), t.structs...) {
		fmt.Fprintf(b, "\tt%s astChunk[%s]\n", t.names[name], t.names[name])
	}
	for _, l := range t.tlistOrder {
		fmt.Fprintf(b, "\t%sChunk astChunk[%s]\n", l.name, t.goType(l.elem))
	}
	b.WriteString("}\n\nfunc (a *tslabs) setWorkControl(c *workControl) { a.control = c }\nfunc (a *tslabs) stepWork() { if workControlsEnabled && a.control != nil { a.control.charge(0, 1) } }\n\n")
}

// astHelpers are the conversion helpers of typed values.
const astHelpers = `func astNode(v any) *Node { n, _ := v.(*Node); return n }
func astIsNil(v any) bool  { n, ok := v.(*Node); return v == nil || ok && n == nil }

// astKid returns the value of element i of a tuple node's children (nil if there is none).
func astKid(kids []*Node, i int) any {
	if i < len(kids) {
		return unboxNode(kids[i])
	}
	return nil
}
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

// astChunk owns unused values and the next allocation size for one type.
// Handed-out values are never reused, even when a parse discards them.
type astChunk[T any] struct {
	free []T
	next int
	bootstrap uint8
}

// grow starts small and doubles up to limit. The request is always satisfied.
func (s *astChunk[T]) grow(n, initial, limit int) {
	size := s.next
	if size == 0 {
		size = initial
	}
	for size < n {
		size *= 2
	}
	s.free = make([]T, size)
	s.next = size * 2
	if s.next > limit {
		s.next = limit
	}
}

`

const astObjectSlabHelper = `// astNew returns a new zero value from a chunk that grows from 8 to 256 values.
func astNew[T any](s *astChunk[T]) *T {
	if len(s.free) == 0 {
		s.grow(1, 8, 256)
	}
	v := &s.free[0]
	s.free = s.free[1:]
	return v
}

`

const astBootstrapObjectHelper = `// astNew allocates the first three values independently so a later
// published value cannot retain discarded early siblings through a chunk.
// Later values use the original 8-to-256 chunk growth.
func astNew[T any](s *astChunk[T]) *T {
	if len(s.free) == 0 {
		if s.bootstrap < 3 {
			s.bootstrap++
			return new(T)
		}
		s.grow(1, 8, 256)
	}
	v := &s.free[0]
	s.free = s.free[1:]
	return v
}

`

const astListSlabHelper = `// astSlice returns n zero values with capacity n, so appending cannot overwrite
// another returned slice. Small-list chunks grow from 64 to 1,024 elements.
func astSlice[T any](s *astChunk[T], n int) []T {
	if n == 0 {
		return []T{} // an empty list, not nil
	}
	if n > 256 {
		return make([]T, n)
	}
	if len(s.free) < n {
		s.grow(n, 64, 1024)
	}
	v := s.free[:n:n]
	s.free = s.free[n:]
	return v
}


`

const astBootstrapListHelper = `// astSlice allocates the first nonempty small list independently to keep
// its backing values separate from later published lists of the same type.
func astSlice[T any](s *astChunk[T], n int) []T {
	if n == 0 {
		return []T{} // an empty list, not nil
	}
	if n > 256 {
		return make([]T, n)
	}
	if len(s.free) < n {
		if s.bootstrap == 0 {
			s.bootstrap = 1
			return make([]T, n)
		}
		s.grow(n, 64, 1024)
	}
	v := s.free[:n:n]
	s.free = s.free[n:]
	return v
}

`

// Separate allocation is a same-generator reference for retained-heap and
// throughput measurements. Selecting it adds no generated runtime selector.
const astIndividualObjectHelper = `// astNew returns an independently allocated zero value.
func astNew[T any](_ *astChunk[T]) *T {
	return new(T)
}

`

const astIndividualListHelper = `// astSlice returns an independent list with capacity n, including non-nil empties.
func astSlice[T any](_ *astChunk[T], n int) []T {
	return make([]T, n)
}


`

// tupleName reports whether name is that of a tuple type of typed values (Tuple1, Tuple2, ...).
func tupleName(name string) bool {
	digits := strings.TrimPrefix(name, "Tuple")
	if digits == name || digits == "" || digits[0] == '0' {
		return false
	}
	return strings.Trim(digits, "0123456789") == ""
}
