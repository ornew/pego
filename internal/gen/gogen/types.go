package gogen

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
)

// Typed values in generated parsers (GenOptions.Types).
//
// With Types, the generated package also defines a Go type for each type of the grammar and a
// function ParseAST that parses like Parse and converts the resulting tree into those types:
//
//   - a struct type T becomes a struct T with the same fields and an embedded Span (its range);
//   - a string node becomes a TNode[string] and an error value Error;
//   - a join type U = A | B becomes an interface U that A, B and Error implement (and TNode, if
//     the join has other node members);
//   - []T becomes a slice, *T a pointer (or the nil value of an interface), and int, string and
//     bool stay as they are;
//   - tuple and list nodes become TNode[any], whose Value is a []any of the
//     elements, and node and any become any (holding the converted value, with tuple
//     and list values as []any).
//
// An Error node (left by #recover) where a struct type is expected becomes nil; it is
// reported among the SyntaxErrors anyway.

// typedOutput is the Go source of the typed values of a generated parser: the part of the parser
// package, and the types package (types/types.go) that declares the types and the methods that make
// them values of the typed runtime.
type typedOutput struct {
	parser, types string
	// needNodes reports that an entry point converts the Node result of a rule.
	needNodes bool
}

// typedGo returns the Go source of the typed values for a parser starting at rule start. Unless conv
// is set, ParseAST runs the typed runtime (runtime/typed.go) when every value it can produce has a Go
// type of its own; otherwise it converts the result of Parse.
//
// The source of the types package refers to the runtime by its package name and to the types by
// their names; that of the parser package refers to the types of the grammar as types.Name, and the
// names that only the types package declares (Error, TNode, Unit, TupleN, PegoHooks) unqualified,
// which QualifyRuntime resolves.
func (g *Generator) typedGo(start *compile.Rule, conv bool) (typedOutput, error) {
	info := g.Prog.Typed
	if info == nil {
		return typedOutput{}, fmt.Errorf("typed values need the grammar's types (type checking did not run)")
	}
	t := &TypedGen{G: g, Info: info, Joins: map[string]string{}, Anon: map[string]compile.JoinTy{}, lists: map[string]string{}, tlists: map[string]string{}, tupleSizes: map[int]bool{}}
	t.ValueResolver = gen.NewValueResolver(g.Prog, func(x compile.Ty) bool { _, ok := t.Joins[x.String()]; return ok })
	if err := t.Name(); err != nil {
		return typedOutput{}, err
	}
	ruleType := t.ruleType
	var b, tb, entries, named strings.Builder
	t.inTypes = true
	t.decls(&tb)
	t.valueMethods(&tb)
	for helpers := 0; t.backHelpers(&tb, &helpers); {
	}
	t.inTypes = false
	doc := `// ParseAST parses the input like Parse and returns the result as typed values. If the parse recovered
// from errors, the result is returned together with SyntaxErrors; an Error node where a struct
// type is expected becomes nil.
`
	st := ruleType(start)
	directStart := !conv && t.runtimeOK(st)
	enter := func(r *compile.Rule) {
		g.typedRoots = append(g.typedRoots, RuleKey{"trules", r.ID, true})
		if g.typedEntry == nil {
			g.typedEntry = map[int]bool{}
		}
		g.typedEntry[r.ID] = true
	}
	g.directTypedDocument = g.Document && directStart
	needTyped, needNodes := false, false
	if directStart {
		needTyped = true
		enter(start)
		result := t.dconv(st, "v", "a")
		fmt.Fprintf(&entries, `%s// It builds the values directly, without the nodes Parse returns.
func ParseAST(input string, opts ...ParseOption) (%s, error) {
	a := &tslabs{}
		return TParse(trules[%d], input, opts, a, func(v any) %s { return %s })
}

`, doc, t.goType(st), start.ID, t.goType(st), result)
	} else {
		needNodes = true
		result := t.conv(st, "n")
		newConv := ""
		if strings.Contains(result, "a.") {
			newConv = "\t\ta := &astConv{control: c}\n"
		}
		returnResult := "\t\treturn " + result + "\n"
		fmt.Fprintf(&entries, `%sfunc ParseAST(input string, opts ...ParseOption) (%s, error) {
 convert := func(n *Node, c *WorkControl, err error) %s {
%s%s }
 o, err := ResolveOptions(pegoGrammar, opts)
 if err != nil { return convert(nil, nil, err), err }
 return RunParseConverted(rules[%d], input, o, convert)
}

`, doc, t.goType(st), t.goType(st), newConv, returnResult, start.ID)
	}
	if g.Document && !g.directTypedDocument {
		// A Document reuses Node results. Convert only the finalized result, with
		// the same invocation controller and cleanup boundary as its Node parse.
		needNodes = true
		result := t.conv(st, "n")
		newConv := ""
		if strings.Contains(result, "a.") {
			newConv = "\t\ta := &astConv{control: c}\n"
		}
		returnResult := "\t\treturn " + result + "\n"
		fmt.Fprintf(&entries, `// ParseAST converts the current Document result to typed values. Typed values
// belong to this call; CST Node members may still share the Document's Node graph.
func (d *Document) ParseAST(opts ...ParseOption) (%s, error) {
%s o, err := d.ResolveParseOptions(opts)
 if err != nil { var zero %s; return zero, err }
 return DocumentParseConverted(d.State, o, func(n *Node, c *WorkControl, parseErr error) %s {
%s%s })
}

`, t.goType(st), generatedDocumentParseGuard(t.goType(st)), t.goType(st), t.goType(st), newConv, returnResult)
	}
	if r := g.streamStart; r != nil {
		// Elements are converted from the committed nodes that ParseStream
		// emits, each with a converter of its own, so that typed values
		// share nothing with later elements.
		needNodes = true
		v, ok := streamValue(g.Prog, r.Def)
		if !ok {
			return typedOutput{}, fmt.Errorf("internal error: no element type for the #stream repetition of %s", r.Name)
		}
		et := t.ResolveValue(v)
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
	var e func(*Node, *WorkControl) error
	if emit != nil {
		e = func(n *Node, c *WorkControl) error {
			if n == nil {
				var zero StreamValue
				return emit(zero)
			}
%[3]s			return emit(%[4]s)
		}
	}
	return RunParseStreamWork(streamRule, r, e, opts)
}

// StreamASTIterator returns the typed elements of a stream one at a time, like StreamIterator.
type StreamASTIterator struct{ pull *StreamPull[StreamValue] }

// IterateAST returns an iterator over the typed elements that ParseStreamAST would pass to emit, parsing
// only as far as each call to Next needs. Options are validated before it returns; r is first read by Next.
func IterateAST(r io.Reader, opts ...ParseOption) (*StreamASTIterator, error) {
	pull, err := IterateStream(pegoGrammar, opts, func(emit func(StreamValue) error) error { return ParseStreamAST(r, emit, opts...) })
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

`, r.Name, t.goType(et), newConv, result)
	}
	// Only start rules are entries, so only they need a case and its converters.
	starts := make([]*compile.Rule, len(g.Prog.StartRules))
	for i, name := range g.Prog.StartRules {
		starts[i] = g.Prog.ByName[name]
	}
	named.WriteString("// ParseRuleAST parses the whole input with the named start rule and returns its typed value; an empty\n// name selects the grammar's default start rule.\n")
	named.WriteString("// Recovered values accompany SyntaxErrors. Nil results and failed parses return the selected type's zero value.\n// Invalid options and names of rules that are not start rules return nil with an error. Start rule result types:\n")
	for _, r := range starts {
		fmt.Fprintf(&named, "//\t%s: %s\n", r.Name, t.goType(ruleType(r)))
	}
	named.WriteString(`func ParseRuleAST(name, input string, opts ...ParseOption) (any, error) {
	o, err := ResolveOptions(pegoGrammar, opts)
	if err != nil {
		return nil, err
	}
	switch name {
`)
	for _, r := range starts {
		x := ruleType(r)
		if r.Name == g.Prog.DefaultStartRule {
			// An empty name selects the default start rule.
			fmt.Fprintf(&named, "\tcase %q, \"\":\n", r.Name)
		} else {
			fmt.Fprintf(&named, "\tcase %q:\n", r.Name)
		}
		if !conv && t.runtimeOK(x) {
			needTyped = true
			enter(r)
			result := t.dconv(x, "v", "a")
			fmt.Fprintf(&named, "\t\ta := &tslabs{}\n\t\treturn TParseResolved(trules[%d], input, o, a, func(v any) %s { return %s })\n", r.ID, t.goType(x), result)
		} else {
			needNodes = true
			result := t.conv(x, "n")
			fmt.Fprintf(&named, "\t\treturn RunParseConverted(rules[%d], input, o, func(n *Node, c *WorkControl, err error) %s {\n", r.ID, t.goType(x))
			fmt.Fprintf(&named, "\t\t\tif n == nil { var zero %s; return zero }\n", t.goType(x))
			if strings.Contains(result, "a.") {
				named.WriteString("\t\t\ta := &astConv{control: c}\n")
			}
			fmt.Fprintf(&named, "\t\t\treturn %s\n\t\t})\n", result)
		}
	}
	named.WriteString("\tdefault:\n\t\treturn nil, pegoGrammar.StartError(name)\n\t}\n}\n")
	units := append([]ShakeUnit(nil), astHelperUnits...)
	bootstrap := !g.disableTypedOutputBootstrap
	// The default form of the chunk helpers is the runtime's (Chunk, Alloc, AllocSlice); only the
	// measurement references (separate allocation, no bootstrap, plain slabs) print local helpers.
	if bootstrap && !g.disableTypedObjectSlabs && !g.disableTypedListSlabs {
		g.chunksInRuntime = true
		units = units[:0:0]
		for _, u := range astHelperUnits {
			if u.Name != "astChunk" {
				units = append(units, u)
			}
		}
	} else {
		switch {
		case g.disableTypedObjectSlabs:
			units = append(units, ShakeUnit{"astNew", astIndividualObjectHelper})
		case bootstrap:
			units = append(units, ShakeUnit{"astNew", astBootstrapObjectHelper})
		default:
			units = append(units, ShakeUnit{"astNew", astObjectSlabHelper})
		}
		switch {
		case g.disableTypedListSlabs:
			units = append(units, ShakeUnit{"astSlice", astIndividualListHelper})
		case bootstrap:
			units = append(units, ShakeUnit{"astSlice", astBootstrapListHelper})
		default:
			units = append(units, ShakeUnit{"astSlice", astListSlabHelper})
		}
	}
	if g.directTypedDocument {
		enter(start)
		_ = t.dconv(st, "v", "a")
	}
	if needTyped {
		// Emit the typed table if any public entry point can use it.
		g.Table = "trules"
		if err := g.startFlow(); err != nil {
			return typedOutput{}, err
		}
		g.rules()
		if g.FlowErr != nil {
			return typedOutput{}, g.FlowErr
		}
		g.Flow = nil
		g.Table = "rules"
		t.typedRuntime(&b)
	}
	if needNodes {
		t.converters(&b)
	}
	if g.directTypedDocument {
		publication := t.documentClone(&b, st)
		result := t.dconv(st, "v", "a")
		fmt.Fprintf(&entries, `// ParseAST directly parses the Document and returns typed values. The values are shared with the
// Document and with earlier results, so a caller that modifies one may change later results, as with
// Node values. CloneAST returns an independent copy.
func (d *Document) ParseAST(opts ...ParseOption) (%s, error) {
%s o, err := d.ResolveParseOptions(opts)
 if err != nil { var zero %s; return zero, err }
 a := &tslabs{}
 return TypedDocumentParse(d.Document, trules[%d], o, a, func(v any) %s {
  return %s
 })
}

// CloneAST returns a copy of a value that ParseAST returned. The copy shares nothing with the
// Document or with other results, so the caller may modify it; aliases within value are preserved.
func CloneAST(value %s) %s {
 c := &typedDocumentClone{}
 out := %s
 c.Finish()
 c.Release()
 return out
}
`, t.goType(st), generatedDocumentParseGuard(t.goType(st)), t.goType(st), start.ID, t.goType(st), result, t.goType(st), t.goType(st), publication)
	}
	if len(t.NodeTypes) > 0 {
		// Registered by tinit when the typed runtime is used; the value helpers compare with it.
		fmt.Fprintf(&tb, "// %[1]s holds the registered indexes of the named node types by their generated index; index 0\n// is no named node type. It is for generated parsers.\nvar %[1]s [%[2]d]uint16\n\n", nodeTypesName, len(t.NodeTypes)+1)
	}
	t.tupleDecls(&tb)
	b.WriteString(entries.String())
	b.WriteString(named.String())
	if err := t.checkTypesNames(); err != nil {
		return typedOutput{}, err
	}
	// The helpers come first; only those that the typed code names are kept.
	code := b.String()
	kept := KeptUnits(units, code, g.Methods.String(), g.Init.String(), g.Vars.String())
	var parser strings.Builder
	for i, u := range units {
		if kept[i] {
			parser.WriteString(u.Text)
		}
	}
	parser.WriteString(code)
	return typedOutput{parser: parser.String(), types: tb.String(), needNodes: needNodes}, nil
}

type TypedGen struct {
	G    *Generator
	Info *compile.TypeInfo
	// inTypes is set while the source of the types package is written, where the types of the
	// grammar are named without a qualifier.
	inTypes bool
	// Joins maps the written form of a join type to the name of the alias that declares it, or to
	// the name that anonJoinName gives an undeclared join; anon holds the latter joins by name.
	Joins map[string]string
	Anon  map[string]compile.JoinTy
	// lists maps the Go type of a list to the name of its converter (from nodes); tlists, to the
	// name of its converter from values of the typed runtime.
	lists      map[string]string
	listOrder  []listConv
	tlists     map[string]string
	tlistOrder []listConv
	// fieldNames maps grammar field names to Go field names, which differ only for the names of
	// the methods and the embedded field of typed values (RuntimeMethodNames).
	fieldNames  map[string]string
	structs     []string // struct type names, sorted
	NodeTypes   []string // named node type names, sorted
	aliases     []string // canonical node join names, sorted, then those of the undeclared joins
	typeAliases []string // other declared aliases, sorted
	// valueResolver selects the rules whose Go types come from their value types.
	gen.ValueResolver
	tupleSizes map[int]bool // the sizes of the tuple types used
	unitUsed   bool
	// nhelpers, dhelpers and chelpers are the helpers converting value-model values from nodes,
	// from values of the typed runtime, and cloning them for Documents; bhelpers convert them back
	// into values of the typed runtime (back).
	nhelpers, dhelpers, chelpers, bhelpers vhelpers
}

type listConv struct {
	name string
	elem compile.Ty
}

// RuntimeMethodNames are the names the generated structs have for the typed runtime: the methods
// of TVal and of the embedded Span (Range), the fields Start and End that the struct promotes from
// it, and the name of the embedded Span; a field of the same name gets a "_" suffix in Go.
var RuntimeMethodNames = map[string]bool{
	"TName": true, "TText": true, "TKids": true, "TStruct": true, "TFresh": true, "TSetFresh": true,
	"TField": true, "TFieldWork": true, "TStrField": true, "TSpan": true, "TSetSpan": true, "Range": true,
	"Span": true, "Start": true, "End": true,
}

// hooksName is the name of the variable of the types package that makes the values of the types
// package from those the typed runtime builds, and back.
const hooksName = "PegoHooks"

// nodeTypesName is the name of the variable of the types package that holds the registered indexes
// of the named node types.
const nodeTypesName = "PegoNodeTypes"

// checkTypesNames rejects a type of the grammar that has the name of a type the types package
// declares for the typed values: TNode, the tuple types and Unit that the output uses, and the
// hooks variable. (Error is a reserved type name of the grammar language.)
func (t *TypedGen) checkTypesNames() error {
	declared := map[string]string{"TNode": "string nodes and nodes of other values", hooksName: "the typed runtime"}
	if len(t.NodeTypes) > 0 {
		declared[nodeTypesName] = "the named node types"
	}
	if t.unitUsed {
		declared["Unit"] = "the empty tuple"
	}
	for n := range t.tupleSizes {
		declared[fmt.Sprintf("Tuple%d", n)] = fmt.Sprintf("tuples of %d elements", n)
	}
	var names []string
	for name := range t.G.Prog.Types {
		if _, ok := declared[name]; ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) > 0 {
		return fmt.Errorf("the type %s has the name of a type that the generated types package declares for %s; rename the type", names[0], declared[names[0]])
	}
	return nil
}

// ref returns the name of the type of the grammar (or the alias, or the struct of a join) with the
// Go name name, as the code being written refers to it.
func (t *TypedGen) ref(name string) string {
	if t.inTypes {
		return name
	}
	return TypesAlias + "." + name
}

// convName returns the name of the converter method for the type name. The prefix keeps the
// names apart from those of the converters of the nodes and values that the generator names
// itself (toNode, toMatch, toOpen, ...).
func convName(name string) string { return "toType" + name }

// fieldGo returns the Go name of the struct field with the grammar name name.
func (t *TypedGen) fieldGo(name string) string {
	if g, ok := t.fieldNames[name]; ok {
		return g
	}
	return name
}

// Name classifies the types of the grammar. They keep their names as Go type names: the types
// package declares no other names than checkTypesNames rejects. Field names stay, too, unless
// they are the name of a method or of the embedded field of a typed value.
func (t *TypedGen) Name() error {
	var all []string
	for name := range t.G.Prog.Types {
		all = append(all, name)
	}
	sort.Strings(all)
	for _, name := range all {
		switch spec := t.G.Prog.Types[name].(type) {
		case *grammar.TypeDef:
			if spec.Struct() == nil {
				t.NodeTypes = append(t.NodeTypes, name)
				continue
			}
			t.structs = append(t.structs, name)
		case *grammar.TypeAlias:
			if u, ok := t.Info.Aliases[name].(compile.JoinTy); ok && t.nodeJoin(u) {
				if _, dup := t.Joins[u.String()]; !dup {
					t.Joins[u.String()] = name
					t.aliases = append(t.aliases, name)
					continue
				}
			}
			t.typeAliases = append(t.typeAliases, name)
		}
	}
	for _, j := range t.AnonJoins() {
		name := j.Alias
		if _, ok := t.Anon[name]; !ok && !slices.Contains(t.aliases, name) {
			name = anonJoinName(j.Members)
			if _, ok := t.G.Prog.Types[name]; ok {
				return fmt.Errorf("the join %s needs a generated interface named %s, which is also the name of a type of the grammar; rename the type", anonJoinText(j.Members), name)
			}
			if other, ok := t.Anon[name]; ok && other.String() != j.Members.String() {
				return fmt.Errorf("the joins %s and %s need generated interfaces with the same name %s", anonJoinText(other), anonJoinText(j.Members), name)
			}
			if _, ok := t.Anon[name]; !ok {
				t.Anon[name] = j.Members
				t.aliases = append(t.aliases, name)
			}
		}
		for _, s := range j.Spellings {
			if _, ok := t.Joins[s]; !ok {
				t.Joins[s] = name
			}
		}
		if _, ok := t.Joins[j.Members.String()]; !ok {
			t.Joins[j.Members.String()] = name
		}
	}
	t.fieldNames = map[string]string{}
	for _, name := range all {
		for _, f := range t.Info.Fields[name] {
			if _, ok := t.fieldNames[f.Name]; !ok {
				t.fieldNames[f.Name] = f.Name
			}
		}
	}
	for name := range t.fieldNames {
		goName := name
		for RuntimeMethodNames[goName] || (goName != name && t.fieldNames[goName] != "") {
			goName += "_"
		}
		t.fieldNames[name] = goName
	}
	return nil
}

// joinOf returns the join that the interface name stands for: the alias that declares it, or the
// undeclared join that it is named after.
func (t *TypedGen) joinOf(name string) compile.JoinTy {
	if u, ok := t.Anon[name]; ok {
		return u
	}
	return t.Info.Aliases[name].(compile.JoinTy)
}

// nodeJoin reports whether every member of the join is a node type (so it can be an interface).
func (t *TypedGen) nodeJoin(u compile.JoinTy) bool {
	for _, a := range u.Alts {
		if !compile.IsNode(a) {
			return false
		}
	}
	return true
}

// goType returns the Go type of values of type x.
func (t *TypedGen) goType(x compile.Ty) string {
	switch x := x.(type) {
	case compile.BasicTy:
		switch x {
		case compile.TyInt, compile.TyString, compile.TyBool:
			return string(x)
		case compile.TyErr:
			return "*Error"
		}
		return "any"
	case gen.Vrule:
		return t.goType(t.Vres[x.Name])
	case compile.TupleTy:
		return t.tupleType(x)
	case compile.NodeTy:
		if x.Name != "" {
			return "*" + t.ref(x.Name)
		}
		if x.Value == compile.TyTop || x.Value == compile.TyAny {
			return "*TNode[any]"
		}
		return "*TNode[" + t.goType(x.Value) + "]"
	case compile.NamedTy:
		switch {
		case x == compile.TyMatch:
			return "*TNode[string]"
		case x == compile.TyError:
			return "*Error"
		case x.Kind == 'c':
			return "*TNode[any]"
		}
		return "*" + t.ref(x.Name)
	case compile.ListTy:
		return "[]" + t.goType(x.Elem)
	case compile.OptTy:
		if b, ok := x.Elem.(compile.BasicTy); ok && (b == compile.TyInt || b == compile.TyString || b == compile.TyBool) {
			return "*" + string(b)
		}
		if _, ok := x.Elem.(compile.TupleTy); ok {
			return "*" + t.goType(x.Elem)
		}
		return t.goType(x.Elem)
	case compile.JoinTy:
		if name, ok := t.Joins[x.String()]; ok {
			return t.ref(name)
		}
		if g, ok := t.joinGoType(x); ok {
			return g
		}
		return "any"
	case compile.RecordTy:
		return "*TNode[any]"
	}
	return "any"
}

// conv returns a Go expression of type goType(x) converting the value v (an expression of
// type any or *Node). Typed values are allocated by the converter a (astConv).
func (t *TypedGen) conv(x compile.Ty, v string) string {
	switch x := x.(type) {
	case compile.BasicTy:
		switch x {
		case compile.TyInt:
			return "astInt(" + v + ")"
		case compile.TyString:
			return "astString(" + v + ")"
		case compile.TyBool:
			return "astBool(" + v + ")"
		case compile.TyErr:
			return "a.toError(astNode(" + v + "))"
		}
		return "a.anyValue(" + v + ")"
	case gen.Vrule:
		return "a." + t.vconvHelper(x) + "(" + v + ")"
	case compile.TupleTy:
		if len(x.Elems) == 0 {
			t.goType(x)
			return "Unit{}"
		}
		return "a." + t.vconvHelper(x) + "(" + v + ")"
	case compile.NodeTy:
		switch {
		case x.Name != "":
			return "a." + convName(x.Name) + "(astNode(" + v + "))"
		case x.Value == compile.TyTop || x.Value == compile.TyAny:
			return "a.toNode(astNode(" + v + "))"
		case x.Value == compile.TyString && len(x.Caps) == 0:
			return "a.toMatch(astNode(" + v + "))"
		}
		return "a." + t.vconvHelper(x) + "(" + v + ")"
	case compile.NamedTy:
		switch {
		case x == compile.TyMatch:
			return "a.toMatch(astNode(" + v + "))"
		case x == compile.TyError:
			return "a.toError(astNode(" + v + "))"
		case x.Kind == 'c':
			return "a.toNode(astNode(" + v + "))"
		}
		return "a." + convName(x.Name) + "(astNode(" + v + "))"
	case compile.ListTy:
		goType := t.goType(x)
		name, ok := t.lists[goType]
		if !ok {
			name = fmt.Sprintf("list%d", len(t.lists))
			t.lists[goType] = name
			t.listOrder = append(t.listOrder, listConv{name, x.Elem})
		}
		return "a." + name + "(astNode(" + v + "))"
	case compile.OptTy:
		if b, ok := x.Elem.(compile.BasicTy); ok && (b == compile.TyInt || b == compile.TyString || b == compile.TyBool) {
			return "astPtr" + strings.ToUpper(string(b)[:1]) + string(b)[1:] + "(" + v + ")"
		}
		if _, ok := x.Elem.(compile.TupleTy); ok {
			return "a." + t.vconvHelper(x) + "(" + v + ")"
		}
		return t.conv(x.Elem, v)
	case compile.JoinTy:
		if name, ok := t.Joins[x.String()]; ok {
			return "a." + convName(name) + "(astNode(" + v + "))"
		}
		if _, ok := t.joinGoType(x); ok {
			return t.conv(x.Alts[0], v)
		}
		return "a.anyValue(" + v + ")"
	case compile.RecordTy:
		return "a.toNode(astNode(" + v + "))"
	}
	return "a.anyValue(" + v + ")"
}

// decls writes the type declarations.
func (t *TypedGen) decls(b *strings.Builder) {
	for _, name := range t.structs {
		fmt.Fprintf(b, "// %s is the struct type %s.\n", name, name)
		fmt.Fprintf(b, "type %s struct {\n\t%s.Span\n", name, runtimeAlias)
		for _, f := range t.Info.Fields[name] {
			fmt.Fprintf(b, "\t%s %s\n", t.fieldGo(f.Name), t.goType(f.T))
		}
		b.WriteString("}\n\n")
	}
	for _, name := range t.NodeTypes {
		n, _ := t.G.Prog.NodeTypeOf(name)
		fmt.Fprintf(b, "// %s is the node type %s = %s.\n", name, name, n.Underlying())
		fmt.Fprintf(b, "type %s struct{ %s }\n\n", name, strings.TrimPrefix(t.goType(n.Underlying()), "*"))
	}
	for _, name := range t.typeAliases {
		fmt.Fprintf(b, "// %s is the alias type %s ~ %s.\n", name, name, t.Info.Aliases[name])
		fmt.Fprintf(b, "type %s = %s\n\n", name, t.goType(t.Info.Aliases[name]))
	}
	// Joins: an interface with a marker method that each member (and Error) implements.
	impl := map[string][]string{} // Go type (without *) -> markers
	for _, name := range t.aliases {
		u := t.joinOf(name)
		goName := name
		if _, anon := t.Anon[name]; anon {
			fmt.Fprintf(b, "// %s is the join type %s. Error (left by #recover) also implements it.\n", goName, anonJoinText(u))
		} else {
			fmt.Fprintf(b, "// %s is the join type %s ~ %s. Error (left by #recover) also implements it.\n", goName, name, u)
		}
		fmt.Fprintf(b, "type %s interface{ is%s() }\n\n", goName, goName)
		members := map[string]bool{"Error": true}
		for _, a := range u.Alts {
			if n, ok := a.(compile.NodeTy); ok && n.Name != "" {
				members[n.Name] = true
			}
			if n, ok := a.(compile.NamedTy); ok {
				switch {
				case n == compile.TyMatch:
					members["TNode"] = true
				case n.Kind == 's':
					members[n.Name] = true
				}
			}
		}
		if gen.CstJoin(u) {
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
func (t *TypedGen) converters(b *strings.Builder) {
	span := "Span"
	fmt.Fprintf(b, `// toMatch converts a string node, also one of a named node type, which converts as the node it
// was converted from. The typed value of a named node type's node is remembered by its own
// converter, so the typed value as a plain node is not remembered.
func (a *astConv) toMatch(n *Node) *TNode[string] {
	a.stepWork()
	if n == nil || n.RawKind == nil || n.RawKind.Base != BaseString {
		return nil
	}
	named := n.RawKind.NodeType()
	if !named {
		if v, ok := a.values[n]; ok { return v.(*TNode[string]) }
	}
	v := astNew(&a.match)
	*v = TNode[string]{Span: %[1]s{Start: int(n.RawStart), End: int(n.RawEnd)}, Value: n.RawText}
	if !named {
		a.remember(n, v)
	}
	return v
}

// toNode converts a node of a statically unknown Value: its Value is the text of a string node,
// and a []any of the converted elements of a tuple or list node.
func (a *astConv) toNode(n *Node) *TNode[any] {
	a.stepWork()
	if n == nil || n.Type() == "error" {
		return nil
	}
	if v, ok := a.values[n]; ok {
		if v, ok := v.(*TNode[any]); ok { return v }
	}
	v := &TNode[any]{Span: %[1]s{Start: int(n.RawStart), End: int(n.RawEnd)}}
	if !n.RawKind.NodeType() {
		a.remember(n, v) // as toMatch
	}
	if n.IsTerminal() {
		v.Value = n.RawText
	} else {
		v.Value = a.elems(n)
	}
	return v
}

// elems converts the elements of a tuple or list.
func (a *astConv) elems(n *Node) []any {
	out := make([]any, len(n.RawChildren))
	for i, c := range n.RawChildren {
		a.stepWork()
		out[i] = a.anyValue(c)
	}
	return out
}

func (a *astConv) toError(n *Node) *Error {
	a.stepWork()
	if n == nil || n.Type() != "error" {
		return nil
	}
	if v, ok := a.values[n]; ok { return v.(*Error) }
	msg, _ := a.field(n, "message").(string)
	v := astNew(&a.error)
	*v = Error{Span: %[1]s{Start: int(n.RawStart), End: int(n.RawEnd)}, Text: n.RawText, Message: msg}
	a.remember(n, v)
	return v
}

`, span)
	b.WriteString("// anyValue converts a value of a statically unknown type into the typed value of its type (tuple\n// and list nodes become TNode[any], and tuple and list values []any).\nfunc (a *astConv) anyValue(v any) any {\n\ta.stepWork()\n\tn, ok := v.(*Node)\n\tif !ok {\n\t\treturn v\n\t}\n\tif n == nil {\n\t\treturn nil\n\t}\n\tswitch n.Type() {\n")
	b.WriteString("\tcase \"string\":\n\t\treturn a.toMatch(n)\n\tcase \"error\":\n\t\treturn a.toError(n)\n\tcase \"tuple\", \"list\":\n\t\treturn a.toNode(n)\n\tcase \"(box)\":\n\t\treturn a.anyValue(UnboxNode(n))\n\tcase \"(tuple)\", \"(list)\":\n\t\treturn a.elems(n)\n")
	for _, name := range append(append([]string(nil), t.structs...), t.NodeTypes...) {
		fmt.Fprintf(b, "\tcase %q:\n\t\treturn a.%s(n)\n", name, convName(name))
	}
	b.WriteString("\t}\n\treturn n\n}\n\n")
	for _, name := range t.NodeTypes {
		// A node of a named node type converts as the node it was converted from (baseNode).
		n, _ := t.G.Prog.NodeTypeOf(name)
		fmt.Fprintf(b, "func (a *astConv) %[4]s(n *Node) *%[1]s {\n\ta.stepWork()\n\tif n == nil || n.Type() != %[2]q {\n\t\treturn nil\n\t}\n\tif v, ok := a.values[n]; ok { return v.(*%[1]s) }\n\tu := %[3]s\n\tif u == nil {\n\t\treturn nil\n\t}\n\tv := &%[1]s{*u}\n\ta.remember(n, v)\n\treturn v\n}\n\n", t.ref(name), name, t.conv(n.Underlying(), "n.BaseNode()"), convName(name))
	}
	for _, name := range t.structs {
		fmt.Fprintf(b, "func (a *astConv) %[4]s(n *Node) *%[1]s {\n\ta.stepWork()\n\tif n == nil || n.Type() != %[2]q {\n\t\treturn nil\n\t}\n\tif v, ok := a.values[n]; ok { return v.(*%[1]s) }\n\tv := astNew(&a.t%[5]s)\n\ta.remember(n, v)\n\tv.%[3]s = %[3]s{Start: int(n.RawStart), End: int(n.RawEnd)}\n", t.ref(name), name, span, convName(name), name)
		if len(t.Info.Fields[name]) > 0 {
			// Fields are set in the order the action set them; look each one up.
			for _, f := range t.Info.Fields[name] {
				fmt.Fprintf(b, "\tv.%s = %s\n", t.fieldGo(f.Name), t.conv(f.T, fmt.Sprintf("a.field(n, %q)", f.Name)))
			}
		}
		b.WriteString("\treturn v\n}\n\n")
	}
	for _, name := range t.aliases {
		u := t.joinOf(name)
		fmt.Fprintf(b, "func (a *astConv) %s(n *Node) %s {\n\ta.stepWork()\n\tif n == nil {\n\t\treturn nil\n\t}\n\tswitch n.Type() {\n\tcase \"error\":\n\t\treturn a.toError(n)\n", convName(name), t.ref(name))
		for _, m := range u.Alts {
			if m, ok := m.(compile.NodeTy); ok && m.Name != "" {
				fmt.Fprintf(b, "\tcase %q:\n\t\treturn a.%s(n)\n", m.Name, convName(m.Name))
			}
			if m, ok := m.(compile.NamedTy); ok {
				switch {
				case m == compile.TyMatch:
					b.WriteString("\tcase \"string\":\n\t\treturn a.toMatch(n)\n")
				case m.Kind == 's':
					fmt.Fprintf(b, "\tcase %q:\n\t\treturn a.%s(n)\n", m.Name, convName(m.Name))
				}
			}
		}
		if gen.CstJoin(u) {
			b.WriteString("\t}\n\treturn a.toNode(n)\n}\n\n")
		} else {
			b.WriteString("\t}\n\treturn nil\n}\n\n")
		}
	}
	// List converters and the helpers of value-model types (more may be added while writing them,
	// for lists of lists).
	b.WriteString("// nodeValue returns the Value of node n: its text, or its converted elements.\nfunc (a *astConv) nodeValue(n *Node) any {\n\tif n.IsTerminal() {\n\t\treturn n.RawText\n\t}\n\treturn a.elems(n)\n}\n\n")
	helpers := 0
	for i := 0; ; {
		for ; i < len(t.listOrder); i++ {
			l := t.listOrder[i]
			elem := t.goType(l.elem)
			conv := t.conv(l.elem, "UnboxNode(c)")
			fmt.Fprintf(b, "func (a *astConv) %[1]s(n *Node) []%[2]s {\n\ta.stepWork()\n\tif n == nil {\n\t\treturn nil\n\t}\n\tout := astSlice(&a.%[1]sChunk, len(n.RawChildren))\n\tfor i, c := range n.RawChildren {\n\t\ta.stepWork()\n\t\tout[i] = %[3]s\n\t}\n\treturn out\n}\n\n", l.name, elem, conv)
		}
		if !t.nodeHelpers(b, &helpers) && i == len(t.listOrder) {
			break
		}
	}
	// The converter, written last because the list converters are known only now: a chunk of
	// values per type and of elements per list type.
	b.WriteString("// astConv converts nodes into typed values.\ntype astConv struct {\n\tcontrol *WorkControl\n\tvalues map[*Node]any // preserve aliases when one node appears in multiple fields or lists\n\tmatch astChunk[TNode[string]]\n\terror astChunk[Error]\n")
	for _, name := range t.structs {
		fmt.Fprintf(b, "\tt%s astChunk[%s]\n", name, t.ref(name))
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
 if a.control != nil { a.control.Charge(0, 1) }
}
func (a *astConv) field(n *Node, name string) any {
 if a.control == nil { return n.RawField(name) }
 if n == nil { return nil }
 for _, f := range n.RawFields {
  a.stepWork()
  if f.Name == name { return f.Value }
 }
 return nil
}

`)
}

// runtimeOK reports whether the typed runtime can build every value an entry point may return: the
// result type and the field types of every struct type have Go types of their own (no CST
// node types, node or any).
func (t *TypedGen) runtimeOK(st compile.Ty) bool {
	if !t.representable(st) || t.readsFields() {
		return false
	}
	for _, name := range t.structs {
		for _, f := range t.Info.Fields[name] {
			if !t.representable(f.T) {
				return false
			}
		}
	}
	return true
}

// nodeMember reports whether the member access m reads only .Start, .End or .Value of nodes.
func (t *TypedGen) nodeMember(m *grammar.Member) bool {
	return t.Info.NodeMembers[m]
}

// readsFields reports whether an action or predicate may read a field of a struct: a member
// access whose name is a field of some struct type. A typed value keeps its fields converted to
// their Go types (a list as a slice, an Error where a struct is expected as nil, an omitted int as
// 0), so reading them back would not give what the node's fields hold. A string, int or bool field
// reads the same where every struct with a field of that name declares it with the same one of
// these types and every constructor of those structs sets it.
func (t *TypedGen) readsFields() bool {
	fields := map[string]bool{}
	// owners maps a field name to the structs with a field of that name, and scalarField reports
	// whether every one of them declares it with the same type, string, int or bool.
	owners := map[string][]string{}
	scalarField := map[string]bool{}
	fieldType := map[string]compile.Ty{}
	for _, name := range t.structs {
		for _, f := range t.Info.Fields[name] {
			if !fields[f.Name] {
				scalarField[f.Name] = f.T == compile.TyString || f.T == compile.TyInt || f.T == compile.TyBool
				fieldType[f.Name] = f.T
			}
			fields[f.Name] = true
			owners[f.Name] = append(owners[f.Name], name)
			scalarField[f.Name] = scalarField[f.Name] && f.T == fieldType[f.Name]
		}
	}
	var terms []grammar.Term
	var expr func(e grammar.Expr)
	expr = func(e grammar.Expr) {
		compile.WalkExpr(e, func(x grammar.Expr) {
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
	for _, r := range t.G.Prog.RuleList {
		expr(r.Def.Expr)
		if r.Def.Action != nil {
			terms = append(terms, r.Def.Action)
		}
	}
	// set[s][f] reports whether every constructor of struct s sets field f; a struct built under
	// another name, an alias, sets nothing it can vouch for.
	set := map[string]map[string]bool{}
	aliased := false
	for _, x := range terms {
		compile.WalkTerm(x, func(y grammar.Term) {
			n, ok := y.(*grammar.New)
			if !ok {
				return
			}
			if _, ok := t.Info.Fields[n.Type]; !ok {
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
		if aliased || !scalarField[field] {
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
		compile.WalkTerm(x, func(y grammar.Term) {
			if m, ok := y.(*grammar.Member); ok && fields[m.Name] && !t.nodeMember(m) && !faithful(m.Name) {
				found = true
			}
		})
	}
	return found
}

// representable reports whether the typed runtime can build values of type x and read them back
// from a field (back): every type of a value, but not a function.
func (t *TypedGen) representable(x compile.Ty) bool {
	switch x := x.(type) {
	case compile.BasicTy:
		switch x {
		case compile.TyInt, compile.TyString, compile.TyBool, compile.TyNode, compile.TyAny, compile.TyErr, compile.TyTop:
			return true
		}
		return false
	case compile.NamedTy, compile.RecordTy, gen.Vrule:
		return true
	case compile.TupleTy:
		for _, e := range x.Elems {
			if !t.representable(e) {
				return false
			}
		}
		return true
	case compile.NodeTy:
		if x.Name != "" {
			return t.representable(x.Underlying())
		}
		// The value of a node is its text, a tuple or a list.
		switch v := x.Value.(type) {
		case compile.BasicTy:
			return v == compile.TyString || v == compile.TyTop || v == compile.TyAny
		case compile.TupleTy, compile.ListTy:
			return t.representable(v)
		}
		return false
	case compile.ListTy:
		return t.representable(x.Elem)
	case compile.OptTy:
		return t.representable(x.Elem)
	case compile.JoinTy:
		for _, a := range x.Alts {
			if !t.representable(a) {
				return false
			}
		}
		return true
	}
	return false
}

// valueMethods writes the methods that make the struct types values of the typed
// runtime (tval).
func (t *TypedGen) valueMethods(b *strings.Builder) {
	for _, name := range t.structs {
		g := name
		fmt.Fprintf(b, "func (v *%[1]s) TName() string { return %[2]q }\nfunc (v *%[1]s) TText() (string, bool) { return \"\", false }\n", g, name)
		fmt.Fprintf(b, "func (v *%[1]s) TKids() []any { return nil }\nfunc (v *%[1]s) TStruct() bool { return true }\n", g)
		fmt.Fprintf(b, "func (v *%[1]s) TFresh() bool { return false }\nfunc (v *%[1]s) TSetFresh(bool) {}\n", g)
		fmt.Fprintf(b, "func (v *%s) TField(name string) (any, bool) {\n\tswitch name {\n", g)
		for _, f := range t.Info.Fields[name] {
			if t.representable(f.T) {
				fmt.Fprintf(b, "\tcase %q:\n\t\treturn %s, true\n", f.Name, t.back(f.T, "v."+t.fieldGo(f.Name)))
			} else {
				fmt.Fprintf(b, "\tcase %q:\n\t\treturn nil, true\n", f.Name)
			}
		}
		b.WriteString("\t}\n\treturn nil, false\n}\n\n")
		var strs []string
		for _, f := range t.Info.Fields[name] {
			if f.T == compile.TyString {
				strs = append(strs, f.Name)
			}
		}
		if len(strs) > 0 {
			fmt.Fprintf(b, "func (v *%s) TStrField(name string) (string, bool) {\n\tswitch name {\n", g)
			for _, f := range strs {
				fmt.Fprintf(b, "\tcase %q:\n\t\treturn v.%s, true\n", f, t.fieldGo(f))
			}
			b.WriteString("\t}\n\treturn \"\", false\n}\n\n")
		}
		fmt.Fprintf(b, "func (v *%s) TFieldWork(p *"+runtimeAlias+".TParser, name string) (any, bool) {\n\tswitch name {\n", g)
		for _, f := range t.Info.Fields[name] {
			if t.representable(f.T) {
				fmt.Fprintf(b, "\tcase %q:\n\t\treturn %s, true\n", f.Name, t.backWork(f.T, "v."+t.fieldGo(f.Name)))
			} else {
				fmt.Fprintf(b, "\tcase %q:\n\t\treturn nil, true\n", f.Name)
			}
		}
		b.WriteString("\t}\n\treturn nil, false\n}\n\n")
	}
}

// back returns a Go expression converting x, an expression of the Go type of type ty, into a
// value of the typed runtime. It is written in the types package.
func (t *TypedGen) back(x compile.Ty, v string) string {
	back := func(v string) string { return runtimeAlias + ".TBack(" + hooksName + ", " + v + ")" }
	switch x := x.(type) {
	case compile.BasicTy:
		if x == compile.TyInt || x == compile.TyString || x == compile.TyBool {
			return "any(" + v + ")"
		}
		return back(v)
	case compile.NamedTy:
		if x.Kind == 's' {
			return runtimeAlias + ".TPtr(" + v + ")"
		}
		return back(v)
	case compile.RecordTy, compile.JoinTy:
		return back(v)
	case compile.NodeTy:
		if x.Name != "" {
			return t.backHelper(x) + "(" + v + ")"
		}
		if x.Value == compile.TyString || x.Value == compile.TyTop || x.Value == compile.TyAny {
			return back(v)
		}
		return t.backHelper(x) + "(" + v + ")"
	case compile.TupleTy, gen.Vrule:
		return t.backHelper(x) + "(" + v + ")"
	case compile.OptTy:
		if b, ok := x.Elem.(compile.BasicTy); ok && (b == compile.TyInt || b == compile.TyString || b == compile.TyBool) {
			return runtimeAlias + ".TDeref(" + v + ")"
		}
		if _, ok := x.Elem.(compile.TupleTy); ok {
			return t.backHelper(x) + "(" + v + ")"
		}
		return t.back(x.Elem, v)
	case compile.ListTy:
		return fmt.Sprintf("func() any { if %[1]s == nil { return nil }; kids := make([]any, len(%[1]s)); for i, x := range %[1]s { kids[i] = %[2]s }; return %[3]s.TListOf(kids) }()", v, t.back(x.Elem, "x"), runtimeAlias)
	}
	return "nil"
}

// backWork preserves back's values while accounting for list materialization.
func (t *TypedGen) backWork(x compile.Ty, v string) string {
	switch x := x.(type) {
	case compile.ListTy:
		return fmt.Sprintf("func() any { if %[1]s == nil { return nil }; kids := make([]any, len(%[1]s)); for i, x := range %[1]s { %[3]skids[i] = %[2]s }; return p.ListField(kids) }()", v, t.backWork(x.Elem, "x"), strings.ReplaceAll(t.G.step("p"), "\n", "; "))
	case compile.OptTy:
		if b, ok := x.Elem.(compile.BasicTy); ok && (b == compile.TyInt || b == compile.TyString || b == compile.TyBool) {
			return t.back(x, v)
		}
		if _, ok := x.Elem.(compile.TupleTy); ok {
			return t.back(x, v) // a pointer to a tuple, which back's helper dereferences
		}
		return t.backWork(x.Elem, v)
	}
	return t.back(x, v)
}

// dconv returns a Go expression of type goType(x) converting v, a value of the typed runtime,
// with the chunks a (a *tslabs).
func (t *TypedGen) dconv(x compile.Ty, v, a string) string {
	switch x := x.(type) {
	case compile.BasicTy:
		switch x {
		case compile.TyInt:
			return "astInt(" + v + ")"
		case compile.TyString:
			return "astString(" + v + ")"
		case compile.TyBool:
			return "astBool(" + v + ")"
		case compile.TyErr:
			return "(*Error)(TPubError(" + v + "))"
		}
		return t.tpub(v, a)
	case gen.Vrule:
		return t.vdconvHelper(x) + "(" + a + ", " + v + ")"
	case compile.TupleTy:
		if len(x.Elems) == 0 {
			t.goType(x)
			return "Unit{}"
		}
		return t.vdconvHelper(x) + "(" + a + ", " + v + ")"
	case compile.NodeTy:
		switch {
		case x.Name != "":
			// A node of a named node type, made from the chunks and remembered (typedHelpers).
		case x.Value == compile.TyTop || x.Value == compile.TyAny:
			return t.tpubNode(v, a)
		case x.Value == compile.TyString && len(x.Caps) == 0:
			return "(*TNode[string])(TPubMatch(" + v + "))"
		}
		return t.vdconvHelper(x) + "(" + a + ", " + v + ")"
	case compile.RecordTy:
		return t.tpubNode(v, a)
	case compile.NamedTy:
		switch {
		case x == compile.TyMatch:
			return "(*TNode[string])(TPubMatch(" + v + "))"
		case x == compile.TyError:
			return "(*Error)(TPubError(" + v + "))"
		case x.Kind == 'c':
			return t.tpubNode(v, a)
		}
		return "TAs[" + t.goType(x) + "](" + v + ")"
	case compile.JoinTy:
		if _, ok := t.Joins[x.String()]; !ok {
			if _, ok := t.joinGoType(x); ok {
				return t.dconv(x.Alts[0], v, a)
			}
			return t.tpub(v, a)
		}
		return "TAs[" + t.goType(x) + "](" + t.tpub(v, a) + ")"
	case compile.OptTy:
		if b, ok := x.Elem.(compile.BasicTy); ok && (b == compile.TyInt || b == compile.TyString || b == compile.TyBool) {
			return "astPtr" + strings.ToUpper(string(b)[:1]) + string(b)[1:] + "(" + v + ")"
		}
		if _, ok := x.Elem.(compile.TupleTy); ok {
			return t.vdconvHelper(x) + "(" + a + ", " + v + ")"
		}
		return t.dconv(x.Elem, v, a)
	case compile.ListTy:
		goType := t.goType(x)
		name, ok := t.tlists[goType]
		if !ok {
			name = fmt.Sprintf("tl%d", len(t.tlists))
			t.tlists[goType] = name
			t.tlistOrder = append(t.tlistOrder, listConv{name, x.Elem})
		}
		return name + "(" + a + ", " + v + ")"
	}
	return "nil"
}

// tpub returns a Go expression converting v, a value of the typed runtime, into its public form
// (the runtime's tpub). With named node types, the public forms of their nodes are made from the
// chunks a and remembered there (tpubIn), so that a node converts to one value everywhere.
func (t *TypedGen) tpub(v, a string) string {
	if len(t.NodeTypes) == 0 {
		return "TPubIn(" + hooksName + ", nil, " + v + ")"
	}
	return "TPubIn(" + hooksName + ", " + a + ", " + v + ")"
}

// tpubNode is tpub for the runtime's tpubNode.
func (t *TypedGen) tpubNode(v, a string) string {
	if len(t.NodeTypes) == 0 {
		return "(*TNode[any])(TPubNodeIn(" + hooksName + ", nil, " + v + "))"
	}
	return "(*TNode[any])(TPubNodeIn(" + hooksName + ", " + a + ", " + v + "))"
}

// typedRuntime writes the constructors of the struct types and the list converters
// of the typed runtime, and the chunks they allocate from.
func (t *TypedGen) typedRuntime(b *strings.Builder) {
	span := "Span"
	var reg strings.Builder
	if len(t.NodeTypes) > 0 {
		// The public forms of the nodes of the named node types, and their registration
		// (nodeTypesName holds the registered indexes, nt, by the generated index).
		reg.WriteString("\ttnamedPub := func(a any, x TVal) any {\n\ts, _ := a.(*tslabs)\n\tif s == nil {\n\t\ts = &tslabs{}\n\t}\n\tswitch x.TName() {\n")
		for _, name := range t.NodeTypes {
			n, _ := t.G.Prog.NodeTypeOf(name)
			fmt.Fprintf(&reg, "\tcase %q:\n\t\treturn %s\n", name, t.dconv(n, "x", "s"))
		}
		reg.WriteString("\t}\n\treturn nil\n\t}\n")
		fmt.Fprintf(&reg, "\tfor i, name := range %s {\n\t\t%s[i+1] = TRegisterNodeType(name, tnamedPub)\n\t}\n", goStrings(t.NodeTypes), nodeTypesName)
	}
	for _, r := range append(append([]*compile.Rule(nil), t.G.Prog.RuleList...), t.G.Prog.Twins...) {
		if r.OpNodeType != "" {
			fmt.Fprintf(&reg, "\ttrules[%d].OpNt = %s\n", r.ID, t.G.nodeTypeID(r.OpNodeType))
		}
		if r.NodeType != "" {
			fmt.Fprintf(&reg, "\ttrules[%d].Nt = %s\n", r.ID, t.G.nodeTypeID(r.NodeType))
		}
	}
	var cons strings.Builder
	for _, name := range t.structs {
		fields := t.Info.Fields[name]
		params := []string{"c *TActCtx", "final bool"}
		for i := range fields {
			params = append(params, fmt.Sprintf("f%d any", i))
		}
		fmt.Fprintf(&cons, "// tmk_%s makes a %s in an action (newStruct in the typed runtime).\nfunc tmk_%s(%s) any {\n\ta := c.P.Ext.(*tslabs)\n\tv := astNew(&a.t%s)\n", name, name, name, strings.Join(params, ", "), name)
		for i, f := range fields {
			fmt.Fprintf(&cons, "\tv.%s = %s\n", t.fieldGo(f.Name), t.dconv(f.T, fmt.Sprintf("f%d", i), "a"))
		}
		fmt.Fprintf(&cons, "\tif final {\n\t\tv.%[1]s = %[1]s{Start: c.Start, End: c.End}\n\t\treturn v\n\t}\n\treturn c.MadeOf(v", span)
		for i := range fields {
			fmt.Fprintf(&cons, ", f%d", i)
		}
		cons.WriteString(")\n}\n\n")
	}
	// List converters and the helpers of value-model types (more may be added while writing them,
	// for lists of lists).
	var convs strings.Builder
	helpers := 0
	for i := 0; ; {
		for ; i < len(t.tlistOrder); i++ {
			l := t.tlistOrder[i]
			elem := t.goType(l.elem)
			fmt.Fprintf(&convs, "func %s(a *tslabs, v any) []%s {\n\ta.stepWork()\n\tif v == nil {\n\t\treturn nil\n\t}\n\tn, _ := v.(*TSeq)\n\tif n == nil {\n\t\treturn []%[2]s{} // another node (an Error), as conversion makes an empty list of it\n\t}\n\tout := astSlice(&a.%sChunk, len(n.Kids))\n\tfor i, c := range n.Kids {\n\t\ta.stepWork()\n\t\tout[i] = %s\n\t}\n\treturn out\n}\n\n", l.name, elem, l.name, t.dconv(l.elem, "c", "a"))
		}
		if !t.typedHelpers(&convs, &helpers) && i == len(t.tlistOrder) {
			break
		}
	}
	b.WriteString("// tinit sets up the typed rules (called at the end of init, after the rule tables).\nfunc tinit() {\n" + reg.String() + "}\n\n")
	b.WriteString(cons.String())
	b.WriteString(convs.String())
	b.WriteString("// tslabs holds the chunks the typed values of a parse are allocated from.\ntype tslabs struct {\n\tcontrol *WorkControl\n")
	for _, name := range t.structs {
		fmt.Fprintf(b, "\tt%s astChunk[%s]\n", name, t.ref(name))
	}
	// The public forms of tuple and list nodes of named node types are made from chunks and
	// remembered by their values; string nodes are their own public forms (tmatchAs).
	var remembered []string
	for _, name := range t.NodeTypes {
		if n, _ := t.G.Prog.NodeTypeOf(name); n.Value != compile.TyString {
			remembered = append(remembered, name)
		}
	}
	if len(remembered) > 0 {
		b.WriteString("\t// named holds the public forms of the tuple and list nodes of named node types by their values.\n\tnamed map[any]any\n")
		for _, name := range remembered {
			fmt.Fprintf(b, "\tt%s astChunk[%s]\n", name, t.ref(name))
		}
	}
	for _, l := range t.tlistOrder {
		fmt.Fprintf(b, "\t%sChunk astChunk[%s]\n", l.name, t.goType(l.elem))
	}
	b.WriteString("}\n\nfunc (a *tslabs) SetWorkControl(c *WorkControl) { a.control = c }\nfunc (a *tslabs) stepWork() { if a.control != nil { a.control.Charge(0, 1) } }\n\n")
}

// chunkNames rewrites the names of the local chunk helpers (astChunk, astNew, astSlice) to those of the
// runtime (Chunk, Alloc, AllocSlice).
var chunkNames = chunkRename{regexp.MustCompile(`\bast(Chunk|New|Slice)\b`)}

type chunkRename struct{ re *regexp.Regexp }

// Replace returns src with the names of the local chunk helpers written as the runtime's.
func (c chunkRename) Replace(src string) string {
	return c.re.ReplaceAllStringFunc(src, func(m string) string {
		switch m {
		case "astChunk":
			return "Chunk"
		case "astNew":
			return "Alloc"
		}
		return "AllocSlice"
	})
}

// astHelperUnits are the conversion helpers of typed values; typedGo keeps those that the typed
// code names (ShakeUnit). The object and list helpers that follow them are chosen by the options.
var astHelperUnits = []ShakeUnit{
	{"astNode", `func astNode(v any) *Node { n, _ := v.(*Node); return n }

`},
	{"astIsNil", `func astIsNil(v any) bool  { n, ok := v.(*Node); return v == nil || ok && n == nil }

`},
	{"astKid", `// astKid returns the value of element i of a tuple node's children (nil if there is none).
func astKid(kids []*Node, i int) any {
	if i < len(kids) {
		return UnboxNode(kids[i])
	}
	return nil
}

`},
	{"astInt", `// astInt returns the int that the box of a rule value or a list element holds.
func astInt(v any) int       { i, _ := UnboxValue(v).(int); return i }

`},
	{"astString", `// astString returns the string that the box of a rule value or a list element holds.
func astString(v any) string { s, _ := UnboxValue(v).(string); return s }

`},
	{"astBool", `// astBool returns the bool that the box of a rule value or a list element holds.
func astBool(v any) bool     { x, _ := UnboxValue(v).(bool); return x }

`},
	{"astPtrInt", `func astPtrInt(v any) *int {
	if i, ok := UnboxValue(v).(int); ok {
		return &i
	}
	return nil
}

`},
	{"astPtrString", `func astPtrString(v any) *string {
	if s, ok := UnboxValue(v).(string); ok {
		return &s
	}
	return nil
}

`},
	{"astPtrBool", `func astPtrBool(v any) *bool {
	if x, ok := UnboxValue(v).(bool); ok {
		return &x
	}
	return nil
}

`},
	{"astChunk", `// astChunk owns unused values and the next allocation size for one type.
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

`},
}

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
