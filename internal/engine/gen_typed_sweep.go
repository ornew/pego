package engine

import (
	"fmt"
	"sort"
	"strings"
)

// typedOutputSweepRuntime changes only the statically selected prototype runtime.
// Exact anchors make a later runtime change fail generation rather than omit an
// ownership hook silently. Reference output uses typedSource without rewriting.
func typedOutputSweepRuntime(source string) (string, error) {
	replacements := []struct {
		old, new string
		count    int
	}{
		{"conv func(any) T) (T, error)", "conv func(any) T, publish func(T, error, *tparser)) (T, error)", 1},
		{"conv func(any) T) (res T, err error)", "conv func(any) T, publish func(T, error, *tparser)) (res T, err error)", 1},
		{"return tparseResolved(r, input, o, ext, conv)", "return tparseResolved(r, input, o, ext, conv, publish)", 1},
		{"\tres = conv(v)\n\to.control.check()", "\tres = conv(v)\n\tpublish(res, err, p)\n\to.control.check()", 1},
		{"\tmatches  []tmatch // returned in values, so not kept for the next parse", "\tmatches  []tmatch // returned in values, so not kept for the next parse\n\tmatchBlocks [][]tmatch // output owners, dropped at publication/recycle", 1},
		{"\t\tp.matches = make([]tmatch, nodeChunk)\n", "\t\tp.matches = make([]tmatch, nodeChunk)\n\t\tp.matchBlocks = append(p.matchBlocks, p.matches)\n", 1},
	}
	for _, r := range replacements {
		if strings.Count(source, r.old) != r.count {
			return "", fmt.Errorf("typed output sweep runtime anchor changed: %q", r.old)
		}
		source = strings.ReplaceAll(source, r.old, r.new)
	}
	return source, nil
}

// typedOutputSweepHelpers requires both ownership hooks in the slab template.
// A missing or duplicated hook must fail before emitting a partial sweep.
func typedOutputSweepHelpers(source string) (string, error) {
	replacements := []struct{ old, new string }{
		{"\tnext int\n", "\tnext int\n\tblocks [][]T\n"},
		{"\ts.free = make([]T, size)\n", "\ts.free = make([]T, size)\n\ts.blocks = append(s.blocks, s.free)\n"},
	}
	for _, r := range replacements {
		if strings.Count(source, r.old) != 1 {
			return "", fmt.Errorf("typed output sweep slab anchor changed: %q", r.old)
		}
		source = strings.Replace(source, r.old, r.new, 1)
	}
	return source, nil
}

func (t *typedGen) outputPublication(x ty) string {
	if !t.g.enableTypedOutputSweep && !t.g.enableBoundedTypedOutputSweep {
		return ""
	}
	return fmt.Sprintf(", func(res %s, err error, p *tparser) { a.publish(res, err, p) }", t.goType(x))
}

func outputLeaf(x ty) bool {
	return x == tyInt || x == tyString || x == tyBool
}

// outputSweep emits an iterative marker over public representations and sweeps
// only allocation blocks owned by the current invocation. Lists protect backing
// cells independently of their pointees; unknown open values inhibit clearing.
func (t *typedGen) outputSweep(b *strings.Builder, direct, conversion bool) {
	b.WriteString(astOutputSweepHelpers)
	b.WriteString("func (m *astOutputMark) run(root any, err error) {\n\tm.todo = append(m.todo, root, err)\n\tfor len(m.todo) != 0 && !m.keepAll {\n\t\ti := len(m.todo)-1\n\t\tv := m.todo[i]\n\t\tm.todo[i] = nil\n\t\tm.todo = m.todo[:i]\n\t\tswitch x := v.(type) {\n")
	b.WriteString("\t\tcase nil, int, string, bool:\n\t\tcase *int:\n\t\t\tif x != nil { m.scalarRefs = true; m.mark(x) }\n\t\tcase *string:\n\t\t\tif x != nil { m.scalarRefs = true; m.mark(x) }\n\t\tcase *bool:\n\t\t\tif x != nil { m.scalarRefs = true; m.mark(x) }\n\t\tcase *Match:\n\t\t\tif x != nil { m.mark(x) }\n\t\tcase *Error:\n\t\t\tif x != nil { m.mark(x) }\n\t\tcase *SyntaxError:\n\t\t\t// Diagnostic fields contain only positions and strings.\n\t\tcase SyntaxErrors:\n\t\t\tastMarkCells(m, x)\n\t\tcase Fields:\n\t\t\tfor i := range x[:cap(x)] { if m.mark(&x[:cap(x)][i]) { m.todo = append(m.todo, x[:cap(x)][i].Value) } }\n\t\tcase *Node:\n\t\t\tif x == nil || !m.mark(x) { continue }\n\t\t\tastMarkCells(m, x.Children)\n\t\t\tm.todo = append(m.todo, x.Fields)\n")
	for _, name := range append(append([]string(nil), t.terms...), t.structs...) {
		fmt.Fprintf(b, "\t\tcase *%s:\n\t\t\tif x == nil || !m.mark(x) { continue }\n", t.names[name])
		for _, f := range t.info.fields[name] {
			if !outputLeaf(f.t) {
				fmt.Fprintf(b, "\t\t\tm.todo = append(m.todo, x.%s)\n", f.name)
			}
		}
	}
	// Conversion discovery includes nested lists; Go representations are the
	// dispatch keys, so grammar aliases do not create duplicate cases.
	lists := map[string]bool{"[]any": false, "[]*Node": false, "[]*SyntaxError": false,
		"[]string": true, "[]int": true, "[]bool": true,
		"[]*string": false, "[]*int": false, "[]*bool": false}
	for _, list := range append(append([]listConv(nil), t.listOrder...), t.tlistOrder...) {
		lists["[]"+t.goType(list.elem)] = outputLeaf(list.elem)
	}
	var names []string
	for name := range lists {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		helper := "astMarkCells"
		if lists[name] {
			helper = "astMarkLeafCells"
		}
		fmt.Fprintf(b, "\t\tcase %s:\n\t\t\t%s(m, x)\n", name, helper)
	}
	b.WriteString("\t\tdefault:\n\t\t\t// An opaque value may hide an owned reference. Do not partially clear.\n\t\t\tm.keepAll = true\n\t\t}\n\t}\n}\n\n")
	if direct {
		t.outputHolder(b, "tslabs", t.tlistOrder, true)
	}
	if conversion {
		t.outputHolder(b, "astConv", t.listOrder, false)
	}
}

func (t *typedGen) outputHolder(b *strings.Builder, holder string, lists []listConv, direct bool) {
	args := "root any, err error"
	if direct {
		args += ", p *tparser"
	}
	fmt.Fprintf(b, "func (a *%s) publish(%s) {\n\tm := &astOutputMark{live: make(map[any]struct{})}\n\tm.run(root, err)\n", holder, args)
	call := "m"
	if direct {
		call += ", p"
	}
	fmt.Fprintf(b, "\tif m.scalarRefs && !m.keepAll { a.promoteScalarOwners(%s) }\n", call)
	var fields []string
	if !direct {
		fields = append(fields, "match", "error")
	}
	for _, name := range append(append([]string(nil), t.terms...), t.structs...) {
		fields = append(fields, "t"+t.names[name])
	}
	for _, list := range lists {
		fields = append(fields, list.name+"Chunk")
	}
	for _, field := range fields {
		fmt.Fprintf(b, "\tif !m.keepAll { astSweepChunk(&a.%s, m.live) }\n\tastDropChunk(&a.%s)\n", field, field)
	}
	if direct {
		b.WriteString("\tif !m.keepAll {\n\t\tfor _, block := range p.matchBlocks {\n\t\t\tfor i := range block {\n\t\t\t\tif _, live := m.live[&block[i].Match]; !live { block[i] = tmatch{} }\n\t\t\t}\n\t\t}\n\t}\n\tclear(p.matchBlocks)\n\tp.matchBlocks = nil\n")
	}
	b.WriteString("}\n\n")
	t.outputOwnerPromotion(b, holder, direct)
}

// A published scalar pointer may name a field inside an owned object. Protect
// its enclosing cell and graph before clearing any domain. Iterate to a fixed
// point because a newly protected object can expose more interior references.
func (t *typedGen) outputOwnerPromotion(b *strings.Builder, holder string, direct bool) {
	args := "m *astOutputMark"
	if direct {
		args += ", p *tparser"
	}
	fmt.Fprintf(b, "func (a *%s) promoteScalarOwners(%s) {\n\tfor !m.keepAll {\n\t\tbefore := len(m.live)\n", holder, args)
	emit := func(blocks, owner string, fields []string) {
		fmt.Fprintf(b, "\t\tfor _, block := range %s {\n\t\t\tfor i := range block {\n\t\t\t\tv := %s\n\t\t\t\tm.protectOwner(v", blocks, owner)
		for _, field := range fields {
			fmt.Fprintf(b, ", &v.%s", field)
		}
		b.WriteString(")\n\t\t\t\tif m.keepAll { return }\n\t\t\t}\n\t\t}\n")
	}
	span := []string{t.span + ".Start", t.span + ".End"}
	if !direct {
		emit("a.match.blocks", "&block[i]", append(append([]string{}, span...), "Text"))
		emit("a.error.blocks", "&block[i]", append(append([]string{}, span...), "Text", "Message"))
	}
	for _, name := range append(append([]string(nil), t.terms...), t.structs...) {
		fields := append([]string{}, span...)
		for _, f := range t.info.fields[name] {
			if outputLeaf(f.t) {
				fields = append(fields, f.name)
			}
		}
		// Terminal Text is implicit rather than a grammar struct field.
		for _, term := range t.terms {
			if term == name {
				fields = append(fields, "Text")
				break
			}
		}
		emit("a.t"+t.names[name]+".blocks", "&block[i]", fields)
	}
	if direct {
		emit("p.matchBlocks", "&block[i].Match", append(append([]string{}, span...), "Text"))
	}
	b.WriteString("\t\tif len(m.live) == before { return }\n\t}\n}\n\n")
}

const astOutputSweepHelpers = `// astOutputMark records only comparable public object or backing-cell pointers.
// Its graph worklist and ownership directories belong to one invocation only.
type astOutputMark struct {
	live map[any]struct{}
	todo []any
	keepAll bool
	scalarRefs bool
}

func (m *astOutputMark) protectOwner(owner any, fields ...any) {
	if _, found := m.live[owner]; found { return }
	for _, field := range fields {
		if _, found := m.live[field]; found { m.run(owner, nil); return }
	}
}

func (m *astOutputMark) mark(pointer any) bool {
	if _, found := m.live[pointer]; found { return false }
	m.live[pointer] = struct{}{}
	return true
}

func astMarkCells[T any](m *astOutputMark, values []T) {
	values = values[:cap(values)]
	for i := range values {
		if m.mark(&values[i]) { m.todo = append(m.todo, values[i]) }
	}
}

func astMarkLeafCells[T any](m *astOutputMark, values []T) {
	values = values[:cap(values)]
	for i := range values { m.mark(&values[i]) }
}

func astSweepChunk[T any](s *astChunk[T], live map[any]struct{}) {
	for _, block := range s.blocks {
		for i := range block {
			if _, found := live[&block[i]]; !found {
				var zero T
				block[i] = zero
			}
		}
	}
}

func astDropChunk[T any](s *astChunk[T]) {
	clear(s.blocks)
	s.blocks = nil
}

`
