package gogen

import (
	"fmt"
	"strings"

	"github.com/ornew/zeroparse/internal/compile"
	"github.com/ornew/zeroparse/internal/gen"
)

// documentClone emits the CloneAST path, which copies the private values retained
// by a typed Document into independent values. Cloning uses an explicit task stack for recursive structs.
func (t *TypedGen) documentClone(b *strings.Builder, result compile.Ty) string {
	var methods, tasks strings.Builder
	listNames := map[string]string{}
	var lists []listConv
	var clone func(compile.Ty, string) string
	clone = func(x compile.Ty, value string) string {
		switch x := x.(type) {
		case compile.BasicTy:
			if x == compile.TyInt || x == compile.TyString || x == compile.TyBool {
				return value
			}
			if x == compile.TyErr {
				return "c.toError(" + value + ")"
			}
			return "c.toOpen(" + value + ")"
		case gen.Vrule:
			return "c." + t.chelpers.get("cv", "rule "+x.Name, x) + "(" + value + ")"
		case compile.TupleTy:
			if len(x.Elems) == 0 {
				return value
			}
			return "c." + t.chelpers.get("cv", t.goType(x), x) + "(" + value + ")"
		case compile.NodeTy:
			if x.Name == "" && (x.Value == compile.TyTop || x.Value == compile.TyAny) {
				return "c.toNode(" + value + ")"
			}
			return "c." + t.chelpers.get("cv", t.goType(x), x) + "(" + value + ")"
		case compile.RecordTy:
			return "c.toNode(" + value + ")"
		case compile.NamedTy:
			name := convName(x.Name)
			if x == compile.TyMatch {
				name = "toMatch"
			} else if x == compile.TyError {
				name = "toError"
			} else if x.Kind == 'c' {
				name = "toNode"
			}
			return "c." + name + "(" + value + ")"
		case compile.JoinTy:
			if name, ok := t.Joins[x.String()]; ok {
				return "c." + convName(name) + "(" + value + ")"
			}
			if _, ok := t.joinGoType(x); ok {
				return clone(x.Alts[0], value)
			}
			return "c.toOpen(" + value + ")"
		case compile.OptTy:
			if scalar, ok := x.Elem.(compile.BasicTy); ok && (scalar == compile.TyInt || scalar == compile.TyString || scalar == compile.TyBool) {
				return "typedDocumentClonePointer(c, " + value + ")"
			}
			if _, ok := x.Elem.(compile.TupleTy); ok {
				return "c." + t.chelpers.get("cv", t.goType(x), x) + "(" + value + ")"
			}
			return clone(x.Elem, value)
		case compile.ListTy:
			key := t.goType(x)
			name := listNames[key]
			if name == "" {
				name = fmt.Sprintf("list%d", len(lists))
				listNames[key] = name
				lists = append(lists, listConv{name: name, elem: x.Elem})
			}
			return "c." + name + "(" + value + ")"
		}
		panic("unrepresentable typed Document clone")
	}
	publication := clone(result, "value")
	// Values that hold text are copied whole: a string node (method Match, type TNode[string]) and
	// Error.
	terminals := []struct{ name, goType, chunk string }{{"Match", "TNode[string]", "match"}, {"Error", "Error", "error"}}
	for _, terminal := range terminals {
		fmt.Fprintf(&methods, `func (c *typedDocumentClone) to%[1]s(v *%[3]s) *%[3]s {
 c.Step(1)
 if v == nil { return nil }
 if old, ok := c.seen%[1]s[v]; ok { return old }
 out := astNew(&c.%[2]s)
 *out = *v
 if c.seen%[1]s == nil { c.seen%[1]s = make(map[*%[3]s]*%[3]s) }; c.seen%[1]s[v] = out
 return out
}
`, terminal.name, terminal.chunk, terminal.goType)
	}
	var containsStruct func(compile.Ty) bool
	containsStruct = func(x compile.Ty) bool {
		switch x := x.(type) {
		case compile.BasicTy:
			return x != compile.TyInt && x != compile.TyString && x != compile.TyBool // an open value may hold one
		case compile.RecordTy:
			return true
		case compile.NamedTy:
			_, ok := t.Info.Fields[x.Name]
			return ok || x.Kind == 'c'
		case compile.OptTy:
			return containsStruct(x.Elem)
		case compile.ListTy:
			return containsStruct(x.Elem)
		case compile.JoinTy:
			for _, a := range x.Alts {
				if containsStruct(a) {
					return true
				}
			}
		case gen.Vrule, compile.TupleTy, compile.NodeTy:
			return true
		}
		return false
	}
	for _, name := range t.structs {
		deferred := false
		for _, f := range t.Info.Fields[name] {
			deferred = deferred || containsStruct(f.T)
		}
		fmt.Fprintf(&methods, `func (c *typedDocumentClone) %[3]s(v *%[1]s) *%[1]s {
 c.Step(1)
 if v == nil { return nil }
 if old, ok := c.seen%[4]s[v]; ok { return old }
 out := astNew(&c.t%[4]s)
 out.%[2]s = v.%[2]s
 if c.seen%[4]s == nil { c.seen%[4]s = make(map[*%[1]s]*%[1]s) }; c.seen%[4]s[v] = out
`, t.ref(name), "Span", convName(name), name)
		target := &methods
		if deferred {
			methods.WriteString(" c.Tasks = append(c.Tasks, TypedDocumentCloneTask{Source: v, Target: out})\n return out\n}\n")
			fmt.Fprintf(&tasks, " case *%s:\n out := task.Target.(*%s)\n _ = out; _ = v\n", t.ref(name), t.ref(name))
			target = &tasks
		}
		for _, f := range t.Info.Fields[name] {
			fmt.Fprintf(target, " c.Step(1)\n out.%s = %s\n", t.fieldGo(f.Name), clone(f.T, "v."+t.fieldGo(f.Name)))
		}
		if !deferred {
			methods.WriteString(" return out\n}\n")
		}
	}
	for _, name := range t.aliases {
		u := t.joinOf(name)
		if !t.representable(u) {
			continue
		}
		fmt.Fprintf(&methods, "func (c *typedDocumentClone) %s(v %s) %s {\n c.Step(1)\n switch v := v.(type) {\n case *Error: return c.toError(v)\n", convName(name), t.ref(name), t.ref(name))
		seen := map[string]bool{"*Error": true}
		open := false
		for _, alternative := range u.Alts {
			goType := t.goType(alternative)
			if goType == "any" {
				// An open member, such as node, takes every other value of the join. A case
				// for it would hide the cases after it, and its clone is an any.
				open = true
				continue
			}
			if seen[goType] {
				continue // tuple and list nodes are all *TNode[any]
			}
			seen[goType] = true
			fmt.Fprintf(&methods, " case %s: return %s\n", goType, clone(alternative, "v"))
		}
		if open {
			fmt.Fprintf(&methods, " case nil:\n default: out, _ := c.toOpen(v).(%s); return out\n", t.ref(name))
		}
		methods.WriteString(" }\n return nil\n}\n")
	}
	// Values of open types (node, any, joins without a Go type of their own) are cloned by
	// their dynamic types.
	methods.WriteString(`func (c *typedDocumentClone) toOpen(v any) any {
 c.Step(1)
 switch v := v.(type) {
 case *TNode[string]: return c.toMatch(v)
 case *TNode[any]: return c.toNode(v)
 case *Error: return c.toError(v)
 case []any: return c.toElems(v)
`)
	for _, name := range t.structs {
		fmt.Fprintf(&methods, " case *%s: return c.%s(v)\n", t.ref(name), convName(name))
	}
	for _, name := range t.NodeTypes {
		n, _ := t.G.Prog.NodeTypeOf(name)
		fmt.Fprintf(&methods, " case *%s: return %s\n", t.ref(name), clone(n, "v"))
	}
	methods.WriteString(` }
 return v
}
func (c *typedDocumentClone) toNode(v *TNode[any]) *TNode[any] {
 c.Step(1)
 if v == nil { return nil }
 if old, ok := c.Values[v]; ok { return old.(*TNode[any]) }
 out := &TNode[any]{Span: v.Span}
 c.Remember(v, out)
 if elems, ok := v.Value.([]any); ok { out.Value = c.toElems(elems) } else { out.Value = v.Value }
 return out
}
func (c *typedDocumentClone) toElems(v []any) []any {
 c.Step(uint64(len(v)))
 out := make([]any, len(v))
 for i, x := range v { out[i] = c.toOpen(x) }
 return out
}
`)
	// The helpers of value-model types and the list cloners (more may be added while writing them).
	helpers := 0
	for i := 0; ; i++ {
		for ; helpers < len(t.chelpers.order); helpers++ {
			h := t.chelpers.order[helpers]
			g := t.goType(h.x)
			fmt.Fprintf(&methods, "func (c *typedDocumentClone) %s(v %s) %s {\n c.Step(1)\n", h.name, g, g)
			switch x := h.x.(type) {
			case gen.Vrule:
				fmt.Fprintf(&methods, " return %s\n", clone(t.Vres[x.Name], "v"))
			case compile.OptTy:
				fmt.Fprintf(&methods, " if v == nil { return nil }\n x := %s\n return &x\n", clone(x.Elem, "*v"))
			case compile.TupleTy:
				methods.WriteString(" out := v\n")
				for j, e := range x.Elems {
					fmt.Fprintf(&methods, " out.E%d = %s\n", j, clone(e, fmt.Sprintf("v.E%d", j)))
				}
				methods.WriteString(" return out\n")
			case compile.NodeTy:
				if x.Name != "" {
					// A node of a named node type clones the node it embeds.
					fmt.Fprintf(&methods, " if v == nil { return nil }\n if old, ok := c.Values[v]; ok { return old.(%s) }\n out := &%s{}\n c.Remember(v, out)\n if u := %s; u != nil { out.TNode = *u }\n return out\n", g, g[1:], clone(x.Underlying(), "&v.TNode"))
					break
				}
				fmt.Fprintf(&methods, " if v == nil { return nil }\n if old, ok := c.Values[v]; ok { return old.(%s) }\n out := &%s{Span: v.Span}\n c.Remember(v, out)\n out.Value = %s\n return out\n", g, g[1:], clone(x.Value, "v.Value"))
			}
			methods.WriteString("}\n")
		}
		if i >= len(lists) {
			break
		}
		list := lists[i]
		elem := t.goType(list.elem)
		value := clone(list.elem, "item")
		fmt.Fprintf(&methods, `func (c *typedDocumentClone) %[1]s(v []%[2]s) []%[2]s {
 c.Step(1)
 if v == nil { return nil }
 key := DocumentSliceKey{Length: len(v), Capacity: cap(v)}
 if cap(v) > 0 { key.First = &v[:1][0] }
 if key.First != nil { if old, ok := c.Slices[key]; ok { return old.([]%[2]s) } }
 c.Step(uint64(len(v)))
 out := astSlice(&c.%[1]sChunk, len(v))
 if key.First != nil { if c.Slices == nil { c.Slices = make(map[DocumentSliceKey]any) }; c.Slices[key] = out }
 for i, item := range v { out[i] = %[3]s }
 return out
}
`, list.name, elem, value)
	}
	b.WriteString(`// typedDocumentClone publishes independent values without exposing cached nodes.
type typedDocumentClone struct {
 TypedDocumentCloneBase
 match astChunk[TNode[string]]
 error astChunk[Error]
`)
	for _, name := range t.structs {
		fmt.Fprintf(b, " t%s astChunk[%s]\n", name, t.ref(name))
	}
	for _, name := range append([]string{"Match", "Error"}, t.structs...) {
		goType := t.ref(name)
		if name == "Match" {
			goType = "TNode[string]"
		} else if name == "Error" {
			goType = "Error"
		}
		fmt.Fprintf(b, " seen%s map[*%s]*%s\n", name, goType, goType)
	}
	for _, list := range lists {
		fmt.Fprintf(b, " %sChunk astChunk[%s]\n", list.name, t.goType(list.elem))
	}
	b.WriteString(`}
func (c *typedDocumentClone) Finish() {
 for len(c.Tasks) > 0 {
  i := len(c.Tasks)-1
  task := c.Tasks[i]
  c.Tasks[i] = TypedDocumentCloneTask{}
  c.Tasks = c.Tasks[:i]
  switch v := task.Source.(type) {
`)
	b.WriteString(tasks.String())
	b.WriteString(`
 default: _ = v
 }
 }
 c.Control.Check()
}
func (c *typedDocumentClone) Release() {
 c.ReleaseBase()
`)
	for _, name := range append([]string{"Match", "Error"}, t.structs...) {
		fmt.Fprintf(b, " c.seen%s = nil\n", name)
	}
	b.WriteString(`
}
func typedDocumentClonePointer[T any](c *typedDocumentClone, v *T) *T {
 c.Step(1)
 if v == nil { return nil }
 if old, ok := c.Values[v]; ok { return old.(*T) }
 out := new(T)
 *out = *v
 c.Remember(v, out)
 return out
}
`)
	b.WriteString(methods.String())
	return publication
}
