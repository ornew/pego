package engine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ornew/pego/grammar"
)

// Typed values of rules in the value model.
//
// A rule whose declared or inferred type is a node kind (a tuple or list node, node, or a record), or a rule without a declared type and without an action, has the Go type of its value
// in the value model (Program.valueTypes) instead:
//
//   - node[T] becomes *TNode[T'], with T' the Go type of T (TNode[any] for an unknown T);
//   - a tuple (A, B, ...) becomes TupleN[A', B', ...], a generic struct with the fields E0, E1, ...,
//     and the unit () becomes Unit;
//   - a reference to another rule becomes the Go type of that rule, and error becomes *Error.
//
// A reference that closes a cycle of rules becomes any, as do unions whose arguments have different
// Go types.

// vrule is a reference to a rule whose Go type is that of its value in the value model; the rule's
// resolved value type, with references replaced, is typedGen.vres[name].
type vrule struct{ name string }

func (r vrule) String() string { return r.name }

// vhelper is a generated helper that converts or clones values of one Go type.
type vhelper struct {
	name string
	x    ty
}

// vhelpers registers helpers by the Go type they produce.
type vhelpers struct {
	names map[string]string
	order []vhelper
}

func (h *vhelpers) get(prefix, key string, x ty) string {
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

// vague reports whether a legacy type leaves the shape of a value open: a CST node type, node
// or a record.
func vague(x ty) bool {
	switch x := x.(type) {
	case basicTy:
		return x == tyNode
	case recordTy:
		return true
	case namedTy:
		return x == tySeq || x == tyList
	case listTy:
		return vague(x.elem)
	case optTy:
		return vague(x.elem)
	case unionTy:
		for _, a := range x.alts {
			if vague(a) {
				return true
			}
		}
	}
	return false
}

// valueResolver resolves the value types of the rules whose typed values come from them.
type valueResolver struct {
	info *typeInfo
	// values holds the value types of the rules and ruleDefs their definitions; vres holds the
	// resolved value types of the rules whose typed values come from them, and resolving the rules
	// being resolved.
	values    map[string]ty
	ruleDefs  map[string]*grammar.RuleDef
	vres      map[string]ty
	resolving map[string]bool
	// ownType reports that a legacy type has a typed representation of its own (a named union).
	ownType func(ty) bool
}

func newValueResolver(p *Program, ownType func(ty) bool) valueResolver {
	r := valueResolver{info: p.typed, values: p.valueTypes(), ruleDefs: map[string]*grammar.RuleDef{}, vres: map[string]ty{}, resolving: map[string]bool{}, ownType: ownType}
	for _, rl := range p.rules {
		r.ruleDefs[rl.name] = rl.def
	}
	return r
}

// ruleType returns the type that selects the Go type of the values of rule r: its legacy type, or a
// vrule for a rule whose Go type comes from its value type.
func (t *typedGen) ruleType(r *rule) ty {
	return t.ruleRef(r.name)
}

func (t *valueResolver) ruleRef(name string) ty {
	legacy := t.info.rules[name]
	if legacy == nil {
		legacy = tyAny
	}
	if !t.usesValue(name, legacy) {
		return legacy
	}
	if _, ok := t.vres[name]; ok {
		return vrule{name}
	}
	if t.resolving[name] {
		return tyTop // a cycle: the reference becomes any
	}
	t.resolving[name] = true
	res := t.resolveValue(t.values[name])
	delete(t.resolving, name)
	t.vres[name] = res
	return vrule{name}
}

// A rule has the Go type of its value if it has no action and either no declared type or a declared
// type that leaves the shape open, unless its type is a union with a Go type of its own.
func (t *valueResolver) usesValue(name string, legacy ty) bool {
	v, ok := t.values[name]
	def := t.ruleDefs[name]
	if !ok || v == nil || v == tyAny || def == nil || def.Action != nil {
		return false
	}
	if _, operators := def.Expr.(*grammar.OperatorsRule); operators {
		return false
	}
	if t.ownType(legacy) {
		return false
	}
	return def.Type == nil || vague(legacy)
}

// resolveValue replaces the rule references in a value type.
func (t *valueResolver) resolveValue(x ty) ty {
	switch x := x.(type) {
	case ruleTy:
		return t.ruleRef(x.name)
	case tupleTy:
		elems := make([]ty, len(x.elems))
		for i, e := range x.elems {
			elems[i] = t.resolveValue(e)
		}
		return tupleTy{elems}
	case nodeTy:
		return nodeTy{value: t.resolveValue(x.value)}
	case listTy:
		return listTy{t.resolveValue(x.elem)}
	case optTy:
		return optTy{t.resolveValue(x.elem)}
	case unionTy:
		alts := make([]ty, len(x.alts))
		for i, a := range x.alts {
			alts[i] = t.resolveValue(a)
		}
		return unionTy{alts}
	case basicTy:
		if x == tyBottom || x == tyNil {
			return tyTop
		}
	}
	return x
}

// tupleType returns the Go type of a tuple, recording the tuple types to declare.
func (t *typedGen) tupleType(x tupleTy) string {
	if len(x.elems) == 0 {
		t.unitUsed = true
		return "Unit"
	}
	t.tupleSizes[len(x.elems)] = true
	elems := make([]string, len(x.elems))
	for i, e := range x.elems {
		elems[i] = t.goType(e)
	}
	return fmt.Sprintf("Tuple%d[%s]", len(x.elems), strings.Join(elems, ", "))
}

// unionGoType returns the Go type shared by the arguments of a union, if they have one.
// The tuple types of the arguments are declared only if the union has their Go type.
func (t *typedGen) unionGoType(x unionTy) (string, bool) {
	sizes, unit := map[int]bool{}, t.unitUsed
	for n := range t.tupleSizes {
		sizes[n] = true
	}
	common := ""
	for _, a := range x.alts {
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
func (t *typedGen) vconvHelper(x ty) string {
	key := t.goType(x)
	if r, ok := x.(vrule); ok {
		key = "rule " + r.name
	}
	return t.nhelpers.get("v", key, x)
}

// vdconvHelper returns the name of the function converting a value of the typed runtime.
func (t *typedGen) vdconvHelper(x ty) string {
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
		case optTy:
			fmt.Fprintf(b, "\tif astIsNil(v) {\n\t\treturn nil\n\t}\n\tx := %s\n\treturn &x\n", t.conv(x.elem, "v"))
		case tupleTy:
			fmt.Fprintf(b, "\tvar out %s\n\tn := astNode(v)\n\tif n == nil {\n\t\treturn out\n\t}\n", g)
			for i, e := range x.elems {
				fmt.Fprintf(b, "\tout.E%d = %s\n", i, t.conv(e, fmt.Sprintf("astKid(n.children, %d)", i)))
			}
			b.WriteString("\treturn out\n")
		case nodeTy:
			fmt.Fprintf(b, "\tn := astNode(v)\n\tif n == nil || n.Type() == \"error\" {\n\t\treturn nil\n\t}\n\tout := &%s{%s: %s{int(n.start), int(n.end)}}\n", g[1:], t.span, t.span)
			switch value := x.value.(type) {
			case basicTy:
				if value == tyString {
					b.WriteString("\tout.Value = n.text\n")
				} else {
					fmt.Fprintf(b, "\tout.Value = %s\n", t.conv(value, "a.nodeValue(n)"))
				}
			case tupleTy, listTy:
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
		case optTy:
			fmt.Fprintf(b, "\tif v == nil {\n\t\treturn nil\n\t}\n\tx := %s\n\treturn &x\n", t.dconv(x.elem, "v", "a"))
		case tupleTy:
			fmt.Fprintf(b, "\tvar out %s\n\tx, _ := v.(*tnode)\n\tif x == nil {\n\t\treturn out\n\t}\n", g)
			for i, e := range x.elems {
				fmt.Fprintf(b, "\tout.E%d = %s\n", i, t.dconv(e, fmt.Sprintf("tkid(x.kids, %d)", i), "a"))
			}
			b.WriteString("\treturn out\n")
		case nodeTy:
			fmt.Fprintf(b, "\tswitch x := v.(type) {\n\tcase *tnode:\n\t\tif x.typ == typeTupleValue || x.typ == typeListValue {\n\t\t\treturn nil\n\t\t}\n\t\tout := &%s{%s: x.%s}\n", g[1:], t.span, t.span)
			switch value := x.value.(type) {
			case tupleTy, listTy:
				fmt.Fprintf(b, "\t\tout.Value = %s\n", t.dconv(value, "x", "a"))
			default:
				if value != tyString {
					fmt.Fprintf(b, "\t\tout.Value = %s\n", t.dconv(value, "tnodeValue(x)", "a"))
				}
			}
			fmt.Fprintf(b, "\t\treturn out\n\tcase *terror:\n\t\treturn nil\n\tcase tval:\n\t\tif x.tstruct() {\n\t\t\treturn nil\n\t\t}\n\t\ts, e := x.tspan()\n\t\ttext, _ := x.ttext()\n\t\t_ = text\n\t\tout := &%s{%s: %s{s, e}}\n", g[1:], t.span, t.span)
			fmt.Fprintf(b, "\t\tout.Value = %s\n\t\treturn out\n\t}\n\treturn nil\n", t.dconv(x.value, "text", "a"))
		}
		b.WriteString("}\n\n")
	}
	return wrote
}

// streamValue returns the value type of the elements of the #stream repetition of the rule rd.
func streamValue(p *Program, rd *grammar.RuleDef) (ty, bool) {
	var rep *grammar.Repeat
	walkExpr(rd.Expr, func(e grammar.Expr) {
		if a, ok := e.(*grammar.Attributed); ok {
			for _, at := range a.Attrs {
				if r, ok := ungroupExpr(a.Expr).(*grammar.Repeat); ok && at.Name == "stream" {
					rep = r
				}
			}
		}
	})
	if rep == nil {
		return nil, false
	}
	v := &valueInference{prog: p, rules: map[string]*grammar.RuleDef{}, out: map[string]ty{}, approx: map[string]ty{}, computing: map[string]bool{}, quiet: true, rule: rd}
	for _, r := range p.Grammar.Rules() {
		v.rules[r.Name] = r
	}
	return v.scopeValue(rep.Expr), true
}
