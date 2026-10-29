package engine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
)

// Typed values of rules in the value model.
//
// A rule without a declared type has the Go type of its value in the value model
// (Program.valueTypes); a rule with a declared type has the Go type of its declared type:
//
//   - node[T] becomes *TNode[T'], with T' the Go type of T (TNode[any] for an unknown T);
//   - a tuple (A, B, ...) becomes TupleN[A', B', ...], a generic struct with the fields E0, E1, ...,
//     and the unit () becomes Unit;
//   - a reference to another rule becomes the Go type of that rule, and error becomes *Error.
//
// A reference that closes a cycle of rules becomes any, as do joins whose arguments have different
// Go types.

// vrule is a reference to a rule whose Go type is that of its value in the value model; the rule's
// resolved value type, with references replaced, is typedGen.vres[name].
type vrule struct{ name string }

func (r vrule) String() string { return r.name }

// vhelper is a generated helper that converts or clones values of one Go type.
type vhelper struct {
	name string
	x    compile.Ty
}

// vhelpers registers helpers by the Go type they produce.
type vhelpers struct {
	names map[string]string
	order []vhelper
}

func (h *vhelpers) get(prefix, key string, x compile.Ty) string {
	if h.names == nil {
		h.names = map[string]string{}
	}
	if name, ok := h.names[key]; ok {
		return name
	}
	name := fmt.Sprintf("%s%d", prefix, len(h.order))
	h.names[key] = name
	h.order = append(h.order, vhelper{name, x})
	return name
}

// valueResolver resolves the value types of the rules whose typed values come from them.
type valueResolver struct {
	prog *compile.Program
	info *compile.TypeInfo
	// values holds the value types of the rules and ruleDefs their definitions; vres holds the
	// resolved value types of the rules whose typed values come from them, and resolving the rules
	// being resolved.
	values    map[string]compile.Ty
	ruleDefs  map[string]*grammar.RuleDef
	vres      map[string]compile.Ty
	resolving map[string]bool
	// ownType reports that a legacy type has a typed representation of its own (a named join).
	ownType func(compile.Ty) bool
}

func newValueResolver(p *compile.Program, ownType func(compile.Ty) bool) valueResolver {
	r := valueResolver{prog: p, info: p.Typed, values: p.ValueTypes(), ruleDefs: map[string]*grammar.RuleDef{}, vres: map[string]compile.Ty{}, resolving: map[string]bool{}, ownType: ownType}
	for _, rl := range p.RuleList {
		r.ruleDefs[rl.Name] = rl.Def
	}
	return r
}

// ruleType returns the type that selects the Go type of the values of rule r: its declared type, or
// a vrule for a rule whose Go type comes from its value type.
func (t *typedGen) ruleType(r *compile.Rule) compile.Ty {
	return t.ruleRef(r.Name)
}

func (t *valueResolver) ruleRef(name string) compile.Ty {
	var legacy compile.Ty = compile.TyAny
	if def := t.ruleDefs[name]; def != nil && def.Type != nil {
		legacy = (&compile.TypeResolver{Prog: t.prog}).Resolve(def.Type)
	}
	if !t.usesValue(name, legacy) {
		return legacy
	}
	if _, ok := t.vres[name]; ok {
		return vrule{name}
	}
	if t.resolving[name] {
		return compile.TyTop // a cycle: the reference becomes any
	}
	t.resolving[name] = true
	res := t.resolveValue(t.values[name])
	delete(t.resolving, name)
	t.vres[name] = res
	return vrule{name}
}

// A rule has the Go type of its value if it has no declared type; a declared type is the rule's Go
// type, node[_] included.
func (t *valueResolver) usesValue(name string, legacy compile.Ty) bool {
	v, ok := t.values[name]
	def := t.ruleDefs[name]
	if !ok || v == nil || v == compile.TyAny || def == nil {
		return false
	}
	if def.Type != nil && def.Action != nil {
		return false // the action's value has the declared type
	}
	if _, operators := def.Expr.(*grammar.OperatorsRule); operators && def.Type != nil {
		return false
	}
	if t.ownType(legacy) {
		return false
	}
	return def.Type == nil
}

// resolveValue replaces the rule references in a value type.
func (t *valueResolver) resolveValue(x compile.Ty) compile.Ty {
	switch x := x.(type) {
	case compile.RuleTy:
		return t.ruleRef(x.Name)
	case compile.TupleTy:
		elems := make([]compile.Ty, len(x.Elems))
		for i, e := range x.Elems {
			elems[i] = t.resolveValue(e)
		}
		return compile.TupleTy{Elems: elems}
	case compile.NodeTy:
		return compile.NodeTy{Value: t.resolveValue(x.Value)}
	case compile.ListTy:
		return compile.ListTy{Elem: t.resolveValue(x.Elem)}
	case compile.OptTy:
		return compile.OptTy{Elem: t.resolveValue(x.Elem)}
	case compile.JoinTy:
		alts := make([]compile.Ty, len(x.Alts))
		for i, a := range x.Alts {
			alts[i] = t.resolveValue(a)
		}
		return compile.JoinTy{Alts: alts}
	case compile.BasicTy:
		if x == compile.TyBottom || x == compile.TyNil {
			return compile.TyTop
		}
	}
	return x
}

// tupleType returns the Go type of a tuple, recording the tuple types to declare.
func (t *typedGen) tupleType(x compile.TupleTy) string {
	if len(x.Elems) == 0 {
		t.unitUsed = true
		return "Unit"
	}
	t.tupleSizes[len(x.Elems)] = true
	elems := make([]string, len(x.Elems))
	for i, e := range x.Elems {
		elems[i] = t.goType(e)
	}
	return fmt.Sprintf("Tuple%d[%s]", len(x.Elems), strings.Join(elems, ", "))
}

// joinGoType returns the Go type shared by the arguments of a join, if they have one.
// The tuple types of the arguments are declared only if the join has their Go type.
func (t *typedGen) joinGoType(x compile.JoinTy) (string, bool) {
	sizes, unit := map[int]bool{}, t.unitUsed
	for n := range t.tupleSizes {
		sizes[n] = true
	}
	common := ""
	for _, a := range x.Alts {
		g := t.goType(a)
		if common != "" && g != common {
			t.tupleSizes, t.unitUsed = sizes, unit
			return "", false
		}
		common = g
	}
	return common, common != ""
}

// tupleDecls writes the declarations of the tuple types used.
func (t *typedGen) tupleDecls(b *strings.Builder) {
	if t.unitUsed {
		b.WriteString("// Unit is the value of the empty tuple ().\ntype Unit struct{}\n\n")
	}
	var sizes []int
	for n := range t.tupleSizes {
		sizes = append(sizes, n)
	}
	sort.Ints(sizes)
	for _, n := range sizes {
		params := make([]string, n)
		fields := make([]string, n)
		for i := range params {
			params[i] = fmt.Sprintf("E%d", i)
			fields[i] = fmt.Sprintf("\tE%d E%d\n", i, i)
		}
		fmt.Fprintf(b, "// Tuple%d is a tuple of %d elements.\ntype Tuple%d[%s any] struct {\n%s}\n\n", n, n, n, strings.Join(params, ", "), strings.Join(fields, ""))
	}
}

// vconvHelper returns the name of the astConv method converting a value of the value-model type x
// (a vrule, tuple, node or optional tuple) from a node.
func (t *typedGen) vconvHelper(x compile.Ty) string {
	key := t.goType(x)
	if r, ok := x.(vrule); ok {
		key = "rule " + r.name
	}
	return t.nhelpers.get("v", key, x)
}

// vdconvHelper returns the name of the function converting a value of the typed runtime.
func (t *typedGen) vdconvHelper(x compile.Ty) string {
	key := t.goType(x)
	if r, ok := x.(vrule); ok {
		key = "rule " + r.name
	}
	return t.dhelpers.get("tv", key, x)
}

// nodeHelpers writes the pending astConv helpers, and reports whether it wrote any.
func (t *typedGen) nodeHelpers(b *strings.Builder, done *int) bool {
	wrote := false
	for ; *done < len(t.nhelpers.order); *done++ {
		wrote = true
		h := t.nhelpers.order[*done]
		g := t.goType(h.x)
		fmt.Fprintf(b, "func (a *astConv) %s(v any) %s {\n\ta.stepWork()\n", h.name, g)
		switch x := h.x.(type) {
		case vrule:
			fmt.Fprintf(b, "\treturn %s\n", t.conv(t.vres[x.name], "v"))
		case compile.OptTy:
			fmt.Fprintf(b, "\tif astIsNil(v) {\n\t\treturn nil\n\t}\n\tx := %s\n\treturn &x\n", t.conv(x.Elem, "v"))
		case compile.TupleTy:
			fmt.Fprintf(b, "\tvar out %s\n\tn := astNode(v)\n\tif n == nil {\n\t\treturn out\n\t}\n", g)
			for i, e := range x.Elems {
				fmt.Fprintf(b, "\tout.E%d = %s\n", i, t.conv(e, fmt.Sprintf("astKid(n.RawChildren, %d)", i)))
			}
			b.WriteString("\treturn out\n")
		case compile.NodeTy:
			fmt.Fprintf(b, "\tn := astNode(v)\n\tif n == nil || n.Type() == \"error\" {\n\t\treturn nil\n\t}\n\tout := &%s{Span: Span{Start: int(n.RawStart), End: int(n.RawEnd)}}\n", g[1:])
			switch value := x.Value.(type) {
			case compile.BasicTy:
				if value == compile.TyString {
					b.WriteString("\tout.Value = n.RawText\n")
				} else {
					fmt.Fprintf(b, "\tout.Value = %s\n", t.conv(value, "a.nodeValue(n)"))
				}
			case compile.TupleTy, compile.ListTy:
				fmt.Fprintf(b, "\tout.Value = %s\n", t.conv(value, "n"))
			default:
				fmt.Fprintf(b, "\tout.Value = %s\n", t.conv(value, "a.nodeValue(n)"))
			}
			b.WriteString("\treturn out\n")
		}
		b.WriteString("}\n\n")
	}
	return wrote
}

// typedHelpers writes the pending typed runtime helpers, and reports whether it wrote any.
func (t *typedGen) typedHelpers(b *strings.Builder, done *int) bool {
	wrote := false
	for ; *done < len(t.dhelpers.order); *done++ {
		wrote = true
		h := t.dhelpers.order[*done]
		g := t.goType(h.x)
		fmt.Fprintf(b, "func %s(a *tslabs, v any) %s {\n\ta.stepWork()\n", h.name, g)
		switch x := h.x.(type) {
		case vrule:
			fmt.Fprintf(b, "\treturn %s\n", t.dconv(t.vres[x.name], "v", "a"))
		case compile.OptTy:
			fmt.Fprintf(b, "\tif v == nil {\n\t\treturn nil\n\t}\n\tx := %s\n\treturn &x\n", t.dconv(x.Elem, "v", "a"))
		case compile.TupleTy:
			fmt.Fprintf(b, "\tvar out %s\n\tx, _ := v.(*TSeq)\n\tif x == nil {\n\t\treturn out\n\t}\n", g)
			for i, e := range x.Elems {
				fmt.Fprintf(b, "\tout.E%d = %s\n", i, t.dconv(e, fmt.Sprintf("TKid(x.Kids, %d)", i), "a"))
			}
			b.WriteString("\treturn out\n")
		case compile.NodeTy:
			if x.Name != "" && x.Value == compile.TyString {
				// A string node of a named node type is its own public value (tmatchAs): the type is declared as
				// struct{ TNode[string] } in the types package, which the schema constant ties to this parser.
				g := t.ref(x.Name)
				fmt.Fprintf(b, "\tx, _ := v.(*TMatch)\n\tif x == nil || x.Nt != %s {\n\t\treturn nil\n\t}\n\treturn TMatchAs[%s](x)\n", t.g.nodeTypeID(x.Name), g)
				break
			}
			if x.Name != "" {
				// A tuple or list node of a named node type, as the node it was converted from,
				// remembered so that the node converts to one value everywhere (as astConv does).
				g := t.ref(x.Name)
				fmt.Fprintf(b, "\tx := AsTval(v)\n\tif x == nil || x.TName() != %q {\n\t\treturn nil\n\t}\n\tif out, ok := a.named[x]; ok {\n\t\treturn out.(*%s)\n\t}\n", x.Name, g)
				fmt.Fprintf(b, "\tu := %s\n\tif u == nil {\n\t\treturn nil\n\t}\n\tout := astNew(&a.t%s)\n\tout.TNode = *u\n\tif a.named == nil {\n\t\ta.named = map[any]any{}\n\t}\n\ta.named[x] = out\n\treturn out\n", t.dconv(x.Underlying(), "v", "a"), x.Name)
				break
			}
			fmt.Fprintf(b, "\tswitch x := v.(type) {\n\tcase *TSeq:\n\t\tif x.Typ == TypeTupleValue || x.Typ == TypeListValue {\n\t\t\treturn nil\n\t\t}\n\t\tout := &%s{Span: x.Span}\n", g[1:])
			switch value := x.Value.(type) {
			case compile.TupleTy, compile.ListTy:
				fmt.Fprintf(b, "\t\tout.Value = %s\n", t.dconv(value, "x", "a"))
			default:
				if value != compile.TyString {
					fmt.Fprintf(b, "\t\tout.Value = %s\n", t.dconv(value, "tnodeValue(x)", "a"))
				}
			}
			fmt.Fprintf(b, "\t\treturn out\n\tcase *TError:\n\t\treturn nil\n\tcase TVal:\n\t\tif x.TStruct() {\n\t\t\treturn nil\n\t\t}\n\t\ts, e := x.TSpan()\n\t\ttext, _ := x.TText()\n\t\t_ = text\n\t\tout := &%s{Span: Span{Start: s, End: e}}\n", g[1:])
			fmt.Fprintf(b, "\t\tout.Value = %s\n\t\treturn out\n\t}\n\treturn nil\n", t.dconv(x.Value, "text", "a"))
		}
		b.WriteString("}\n\n")
	}
	return wrote
}

// streamValue returns the value type of the elements of the #stream repetition of the rule rd.
func streamValue(p *compile.Program, rd *grammar.RuleDef) (compile.Ty, bool) {
	var rep *grammar.Repeat
	compile.WalkExpr(rd.Expr, func(e grammar.Expr) {
		if a, ok := e.(*grammar.Attributed); ok {
			for _, at := range a.Attrs {
				if r, ok := compile.SeeThrough(a.Expr).(*grammar.Repeat); ok && at.Name == "stream" {
					rep = r
				}
			}
		}
	})
	if rep == nil {
		return nil, false
	}
	v := &compile.ValueInference{Prog: p, Rules: map[string]*grammar.RuleDef{}, Out: map[string]compile.Ty{}, Approx: map[string]compile.Ty{}, Computing: map[string]bool{}, Quiet: true, Rule: rd}
	for _, r := range p.Grammar.Rules() {
		v.Rules[r.Name] = r
	}
	return v.ScopeValue(rep.Expr), true
}

// backHelper returns the name of the function converting a value of the value-model type x (a
// vrule, tuple, node or optional tuple) into a value of the typed runtime.
func (t *typedGen) backHelper(x compile.Ty) string {
	key := t.goType(x)
	if r, ok := x.(vrule); ok {
		key = "rule " + r.name
	}
	return t.bhelpers.get("tb", key, x)
}

// backHelpers writes the pending helpers of back, and reports whether it wrote any.
func (t *typedGen) backHelpers(b *strings.Builder, done *int) bool {
	wrote := false
	for ; *done < len(t.bhelpers.order); *done++ {
		wrote = true
		h := t.bhelpers.order[*done]
		fmt.Fprintf(b, "func %s(v %s) any {\n", h.name, t.goType(h.x))
		elems := func(x compile.TupleTy, v string) string {
			kids := make([]string, len(x.Elems))
			for i, e := range x.Elems {
				kids[i] = t.back(e, fmt.Sprintf("%s.E%d", v, i))
			}
			return "[]any{" + strings.Join(kids, ", ") + "}"
		}
		switch x := h.x.(type) {
		case vrule:
			fmt.Fprintf(b, "\treturn %s\n", t.back(t.vres[x.name], "v"))
		case compile.OptTy:
			fmt.Fprintf(b, "\tif v == nil {\n\t\treturn nil\n\t}\n\treturn %s\n", t.back(x.Elem, "(*v)"))
		case compile.TupleTy:
			if len(x.Elems) == 0 {
				b.WriteString("\treturn &rt.TSeq{Typ: rt.TypeTupleValue}\n")
			} else {
				fmt.Fprintf(b, "\treturn &rt.TSeq{Typ: rt.TypeTupleValue, Kids: %s}\n", elems(x, "v"))
			}
		case compile.NodeTy:
			b.WriteString("\tif v == nil {\n\t\treturn nil\n\t}\n")
			if x.Name != "" {
				fmt.Fprintf(b, "\treturn rt.TSetNodeType(%s, %s)\n}\n\n", t.back(x.Underlying(), "&v.TNode"), t.g.nodeTypeID(x.Name))
				continue
			}
			switch value := x.Value.(type) {
			case compile.ListTy:
				fmt.Fprintf(b, "\tkids := make([]any, len(v.Value))\n\tfor i, x := range v.Value {\n\t\tkids[i] = %s\n\t}\n\treturn &rt.TSeq{Span: v.Span, Typ: \"list\", Kids: kids}\n", t.back(value.Elem, "x"))
			case compile.TupleTy:
				if len(value.Elems) == 0 {
					fmt.Fprintf(b, "\treturn &rt.TSeq{Span: v.Span, Typ: \"tuple\"}\n")
				} else {
					fmt.Fprintf(b, "\treturn &rt.TSeq{Span: v.Span, Typ: \"tuple\", Kids: %s}\n", elems(value, "v.Value"))
				}
			default:
				b.WriteString("\treturn nil\n")
			}
		}
		b.WriteString("}\n\n")
	}
	return wrote
}
