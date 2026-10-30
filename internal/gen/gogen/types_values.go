package gogen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
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

// ruleType returns the type that selects the Go type of the values of rule r: its declared type, or
// a vrule for a rule whose Go type comes from its value type.
func (t *TypedGen) ruleType(r *compile.Rule) compile.Ty {
	return t.RuleRef(r.Name)
}

// tupleType returns the Go type of a tuple, recording the tuple types to declare.
func (t *TypedGen) tupleType(x compile.TupleTy) string {
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
func (t *TypedGen) joinGoType(x compile.JoinTy) (string, bool) {
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
func (t *TypedGen) tupleDecls(b *strings.Builder) {
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
func (t *TypedGen) vconvHelper(x compile.Ty) string {
	key := t.goType(x)
	if r, ok := x.(gen.Vrule); ok {
		key = "rule " + r.Name
	}
	return t.nhelpers.get("v", key, x)
}

// vdconvHelper returns the name of the function converting a value of the typed runtime.
func (t *TypedGen) vdconvHelper(x compile.Ty) string {
	key := t.goType(x)
	if r, ok := x.(gen.Vrule); ok {
		key = "rule " + r.Name
	}
	return t.dhelpers.get("tv", key, x)
}

// nodeHelpers writes the pending astConv helpers, and reports whether it wrote any.
func (t *TypedGen) nodeHelpers(b *strings.Builder, done *int) bool {
	wrote := false
	for ; *done < len(t.nhelpers.order); *done++ {
		wrote = true
		h := t.nhelpers.order[*done]
		g := t.goType(h.x)
		fmt.Fprintf(b, "func (a *astConv) %s(v any) %s {\n\ta.spend()\n", h.name, g)
		switch x := h.x.(type) {
		case gen.Vrule:
			fmt.Fprintf(b, "\treturn %s\n", t.conv(t.Vres[x.Name], "v"))
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
func (t *TypedGen) typedHelpers(b *strings.Builder, done *int) bool {
	wrote := false
	for ; *done < len(t.dhelpers.order); *done++ {
		wrote = true
		h := t.dhelpers.order[*done]
		g := t.goType(h.x)
		fmt.Fprintf(b, "func %s(a *tslabs, v any) %s {\n\ta.spend()\n", h.name, g)
		switch x := h.x.(type) {
		case gen.Vrule:
			fmt.Fprintf(b, "\treturn %s\n", t.dconv(t.Vres[x.Name], "v", "a"))
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
				fmt.Fprintf(b, "\tx, _ := v.(*TMatch)\n\tif x == nil || x.Nt != %s {\n\t\treturn nil\n\t}\n\treturn TMatchAs[%s](x)\n", t.G.nodeTypeID(x.Name), g)
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
func (t *TypedGen) backHelper(x compile.Ty) string {
	key := t.goType(x)
	if r, ok := x.(gen.Vrule); ok {
		key = "rule " + r.Name
	}
	return t.bhelpers.get("tb", key, x)
}

// backHelpers writes the pending helpers of back, and reports whether it wrote any.
func (t *TypedGen) backHelpers(b *strings.Builder, done *int) bool {
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
		case gen.Vrule:
			fmt.Fprintf(b, "\treturn %s\n", t.back(t.Vres[x.Name], "v"))
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
				fmt.Fprintf(b, "\treturn rt.TSetNodeType(%s, %s)\n}\n\n", t.back(x.Underlying(), "&v.TNode"), t.G.nodeTypeID(x.Name))
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
