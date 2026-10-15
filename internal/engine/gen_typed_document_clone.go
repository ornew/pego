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
			return value
		case namedTy:
			name := t.names[x.name]
			if x == tyMatch {
				name = "Match"
			} else if x == tyError {
				name = "Error"
			}
			return "c.to" + name + "(" + value + ")"
		case unionTy:
			return "c.to" + t.unions[x.String()] + "(" + value + ")"
		case optTy:
			if scalar, ok := x.elem.(basicTy); ok && (scalar == tyInt || scalar == tyString || scalar == tyBool) {
				return "typedDocumentClonePointer(c, " + value + ")"
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
	terminals := []struct{ name, chunk string }{{"Match", "match"}, {"Error", "error"}}
	for _, name := range t.terms {
		terminals = append(terminals, struct{ name, chunk string }{t.names[name], "t" + t.names[name]})
	}
	for _, terminal := range terminals {
		goName, chunk := terminal.name, terminal.chunk
		fmt.Fprintf(&methods, `func (c *typedDocumentClone) to%[1]s(v *%[1]s) *%[1]s {
 c.step(1)
 if v == nil { return nil }
 if old, ok := c.seen%[1]s[v]; ok { return old }
 out := astNew(&c.%[2]s)
 *out = *v
 if c.seen%[1]s == nil { c.seen%[1]s = make(map[*%[1]s]*%[1]s) }; c.seen%[1]s[v] = out
 return out
}
`, goName, chunk)
	}
	var containsStruct func(ty) bool
	containsStruct = func(x ty) bool {
		switch x := x.(type) {
		case namedTy:
			_, ok := t.info.fields[x.name]
			return ok
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
 c.step(1)
 if v == nil { return nil }
 if old, ok := c.seen%[1]s[v]; ok { return old }
 out := astNew(&c.t%[1]s)
 out.%[2]s = v.%[2]s
 if c.seen%[1]s == nil { c.seen%[1]s = make(map[*%[1]s]*%[1]s) }; c.seen%[1]s[v] = out
`, goName, t.span)
		target := &methods
		if deferred {
			methods.WriteString(" c.tasks = append(c.tasks, typedDocumentCloneTask{v, out})\n return out\n}\n")
			fmt.Fprintf(&tasks, " case *%s:\n out := task.target.(*%s)\n _ = out; _ = v\n", goName, goName)
			target = &tasks
		}
		for _, f := range t.info.fields[name] {
			fmt.Fprintf(target, " c.step(1)\n out.%s = %s\n", f.name, clone(f.t, "v."+f.name))
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
		fmt.Fprintf(&methods, "func (c *typedDocumentClone) to%s(v %s) %s {\n c.step(1)\n switch v := v.(type) {\n case *Error: return c.toError(v)\n", t.names[name], t.names[name], t.names[name])
		for _, alternative := range u.alts {
			if alternative == tyError {
				continue
			}
			fmt.Fprintf(&methods, " case %s: return %s\n", t.goType(alternative), clone(alternative, "v"))
		}
		methods.WriteString(" }\n return nil\n}\n")
	}
	for i := 0; i < len(lists); i++ {
		list := lists[i]
		elem := t.goType(list.elem)
		value := clone(list.elem, "item")
		fmt.Fprintf(&methods, `func (c *typedDocumentClone) %[1]s(v []%[2]s) []%[2]s {
 c.step(1)
 if v == nil { return nil }
 key := documentSliceKey{length: len(v), capacity: cap(v)}
 if cap(v) > 0 { key.first = &v[:1][0] }
 if key.first != nil { if old, ok := c.slices[key]; ok { return old.([]%[2]s) } }
 c.step(uint64(len(v)))
 out := astSlice(&c.%[1]sChunk, len(v))
 if key.first != nil { if c.slices == nil { c.slices = make(map[documentSliceKey]any) }; c.slices[key] = out }
 for i, item := range v { out[i] = %[3]s }
 return out
}
`, list.name, elem, value)
	}
	b.WriteString(`// typedDocumentClone publishes independent values without exposing cached nodes.
type typedDocumentClone struct {
 control *workControl
 values map[any]any
 slices map[documentSliceKey]any
 tasks []typedDocumentCloneTask
 match astChunk[Match]
 error astChunk[Error]
`)
	for _, name := range append(append([]string(nil), t.terms...), t.structs...) {
		fmt.Fprintf(b, " t%s astChunk[%s]\n", t.names[name], t.names[name])
	}
	for _, name := range append([]string{"Match", "Error"}, append(t.terms, t.structs...)...) {
		if g, ok := t.names[name]; ok {
			name = g
		}
		fmt.Fprintf(b, " seen%s map[*%s]*%s\n", name, name, name)
	}
	for _, list := range lists {
		fmt.Fprintf(b, " %sChunk astChunk[%s]\n", list.name, t.goType(list.elem))
	}
	b.WriteString(`}
type typedDocumentCloneTask struct {source, target any}
func (c *typedDocumentClone) step(n uint64) {
 if workControlsEnabled && c.control != nil { c.control.charge(0, n) }
}
func (c *typedDocumentClone) remember(from, to any) {
 if c.values == nil { c.values = make(map[any]any) }
 c.values[from] = to
}
func (c *typedDocumentClone) finish() {
 for len(c.tasks) > 0 {
  i := len(c.tasks)-1
  task := c.tasks[i]
  c.tasks[i] = typedDocumentCloneTask{}
  c.tasks = c.tasks[:i]
  switch v := task.source.(type) {
`)
	b.WriteString(tasks.String())
	b.WriteString(`
 default: _ = v
 }
 }
 c.control.check()
}
func (c *typedDocumentClone) release() {
 c.control, c.values, c.slices, c.tasks = nil, nil, nil, nil
`)
	for _, name := range append([]string{"Match", "Error"}, append(t.terms, t.structs...)...) {
		if g, ok := t.names[name]; ok {
			name = g
		}
		fmt.Fprintf(b, " c.seen%s = nil\n", name)
	}
	b.WriteString(`
}
func typedDocumentClonePointer[T any](c *typedDocumentClone, v *T) *T {
 c.step(1)
 if v == nil { return nil }
 if old, ok := c.values[v]; ok { return old.(*T) }
 out := new(T)
 *out = *v
 c.remember(v, out)
 return out
}
`)
	b.WriteString(methods.String())
	return publication
}
