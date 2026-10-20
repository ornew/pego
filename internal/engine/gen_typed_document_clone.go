package engine

import (
	"fmt"
	"strings"
)

// documentClone emits an owned publication path for the private values retained
// by a typed Document. Cloning uses an explicit task stack for recursive structs.
func (t *typedGen) documentClone(b *strings.Builder, result ty) string {
	var methods, tasks strings.Builder
	listNames := map[string]string{}
	var lists []listConv
	var clone func(ty, string) string
	clone = func(x ty, value string) string {
		switch x := x.(type) {
		case basicTy:
			if x == tyInt || x == tyString || x == tyBool {
				return value
			}
			if x == tyErr {
				return "c.toError(" + value + ")"
			}
			return "c.toOpen(" + value + ")"
		case vrule:
			return "c." + t.chelpers.get("cv", "rule "+x.name, x) + "(" + value + ")"
		case tupleTy:
			if len(x.elems) == 0 {
				return value
			}
			return "c." + t.chelpers.get("cv", t.goType(x), x) + "(" + value + ")"
		case nodeTy:
			if x.name == "" && (x.value == tyTop || x.value == tyAny) {
				return "c.toNode(" + value + ")"
			}
			return "c." + t.chelpers.get("cv", t.goType(x), x) + "(" + value + ")"
		case recordTy:
			return "c.toNode(" + value + ")"
		case namedTy:
			name := t.names[x.name]
			if x == tyMatch {
				name = "Match"
			} else if x == tyError {
				name = "Error"
			} else if x.kind == 'c' {
				name = "Node"
			}
			return "c.to" + name + "(" + value + ")"
		case unionTy:
			if name, ok := t.unions[x.String()]; ok {
				return "c.to" + name + "(" + value + ")"
			}
			if _, ok := t.unionGoType(x); ok {
				return clone(x.alts[0], value)
			}
			return "c.toOpen(" + value + ")"
		case optTy:
			if scalar, ok := x.elem.(basicTy); ok && (scalar == tyInt || scalar == tyString || scalar == tyBool) {
				return "typedDocumentClonePointer(c, " + value + ")"
			}
			if _, ok := x.elem.(tupleTy); ok {
				return "c." + t.chelpers.get("cv", t.goType(x), x) + "(" + value + ")"
			}
			return clone(x.elem, value)
		case listTy:
			key := t.goType(x)
			name := listNames[key]
			if name == "" {
				name = fmt.Sprintf("list%d", len(lists))
				listNames[key] = name
				lists = append(lists, listConv{name: name, elem: x.elem})
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
	var containsStruct func(ty) bool
	containsStruct = func(x ty) bool {
		switch x := x.(type) {
		case basicTy:
			return x != tyInt && x != tyString && x != tyBool // an open value may hold one
		case recordTy:
			return true
		case namedTy:
			_, ok := t.info.fields[x.name]
			return ok || x.kind == 'c'
		case optTy:
			return containsStruct(x.elem)
		case listTy:
			return containsStruct(x.elem)
		case unionTy:
			for _, a := range x.alts {
				if containsStruct(a) {
					return true
				}
			}
		case vrule, tupleTy, nodeTy:
			return true
		}
		return false
	}
	for _, name := range t.structs {
		goName := t.names[name]
		deferred := false
		for _, f := range t.info.fields[name] {
			deferred = deferred || containsStruct(f.t)
		}
		fmt.Fprintf(&methods, `func (c *typedDocumentClone) to%[1]s(v *%[1]s) *%[1]s {
 c.Step(1)
 if v == nil { return nil }
 if old, ok := c.seen%[1]s[v]; ok { return old }
 out := astNew(&c.t%[1]s)
 out.%[2]s = v.%[2]s
 if c.seen%[1]s == nil { c.seen%[1]s = make(map[*%[1]s]*%[1]s) }; c.seen%[1]s[v] = out
`, goName, t.span)
		target := &methods
		if deferred {
			methods.WriteString(" c.Tasks = append(c.Tasks, TypedDocumentCloneTask{Source: v, Target: out})\n return out\n}\n")
			fmt.Fprintf(&tasks, " case *%s:\n out := task.Target.(*%s)\n _ = out; _ = v\n", goName, goName)
			target = &tasks
		}
		for _, f := range t.info.fields[name] {
			fmt.Fprintf(target, " c.Step(1)\n out.%s = %s\n", t.fieldGo(f.name), clone(f.t, "v."+t.fieldGo(f.name)))
		}
		if !deferred {
			methods.WriteString(" return out\n}\n")
		}
	}
	for _, name := range t.aliases {
		u := t.info.aliases[name].(unionTy)
		if !t.representable(u) {
			continue
		}
		fmt.Fprintf(&methods, "func (c *typedDocumentClone) to%s(v %s) %s {\n c.Step(1)\n switch v := v.(type) {\n case *Error: return c.toError(v)\n", t.names[name], t.names[name], t.names[name])
		seen := map[string]bool{"*Error": true}
		open := false
		for _, alternative := range u.alts {
			goType := t.goType(alternative)
			if goType == "any" {
				// An open member, such as node, takes every other value of the union. A case
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
			fmt.Fprintf(&methods, " case nil:\n default: out, _ := c.toOpen(v).(%s); return out\n", t.names[name])
		}
		methods.WriteString(" }\n return nil\n}\n")
	}
	// Values of open types (node, any, unions without a Go type of their own) are cloned by
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
		fmt.Fprintf(&methods, " case *%[1]s: return c.to%[1]s(v)\n", t.names[name])
	}
	for _, name := range t.nodeTypes {
		n, _ := t.g.prog.nodeTypeOf(name)
		fmt.Fprintf(&methods, " case *%s: return %s\n", t.names[name], clone(n, "v"))
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
			case vrule:
				fmt.Fprintf(&methods, " return %s\n", clone(t.vres[x.name], "v"))
			case optTy:
				fmt.Fprintf(&methods, " if v == nil { return nil }\n x := %s\n return &x\n", clone(x.elem, "*v"))
			case tupleTy:
				methods.WriteString(" out := v\n")
				for j, e := range x.elems {
					fmt.Fprintf(&methods, " out.E%d = %s\n", j, clone(e, fmt.Sprintf("v.E%d", j)))
				}
				methods.WriteString(" return out\n")
			case nodeTy:
				if x.name != "" {
					// A node of a named node type clones the node it embeds.
					fmt.Fprintf(&methods, " if v == nil { return nil }\n if old, ok := c.Values[v]; ok { return old.(%s) }\n out := &%s{}\n c.Remember(v, out)\n if u := %s; u != nil { out.TNode = *u }\n return out\n", g, g[1:], clone(x.underlying(), "&v.TNode"))
					break
				}
				fmt.Fprintf(&methods, " if v == nil { return nil }\n if old, ok := c.Values[v]; ok { return old.(%s) }\n out := &%s{Span: v.Span}\n c.Remember(v, out)\n out.Value = %s\n return out\n", g, g[1:], clone(x.value, "v.Value"))
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
		fmt.Fprintf(b, " t%s astChunk[%s]\n", t.names[name], t.names[name])
	}
	for _, name := range append([]string{"Match", "Error"}, t.structs...) {
		goType := name
		if g, ok := t.names[name]; ok {
			name, goType = g, g
		} else if name == "Match" {
			goType = "TNode[string]"
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
		if g, ok := t.names[name]; ok {
			name = g
		}
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
