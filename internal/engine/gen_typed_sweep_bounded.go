package engine

import (
	"fmt"
	"sort"
	"strings"
)

// outputBoundedSweep is selected only at generation time. Neither the default
// output nor the full-marker control receives these helpers or budget fields.
func (t *typedGen) outputBoundedSweep(b *strings.Builder, direct, conversion bool) {
	b.WriteString(astBoundedOutputSweepHelpers)
	b.WriteString(`func (m *astOutputMark) run(root any, err error) {
	m.complete = false
	if !m.push(root) || !m.push(err) { return }
	for len(m.todo) != 0 && !m.keepAll {
		i := len(m.todo)-1
		v := m.todo[i]
		m.todo[i] = nil
		m.todo = m.todo[:i]
		switch x := v.(type) {
		case nil, int, string, bool:
		case *int:
			if x != nil { m.scalarRefs = true; m.mark(x) }
		case *string:
			if x != nil { m.scalarRefs = true; m.mark(x) }
		case *bool:
			if x != nil { m.scalarRefs = true; m.mark(x) }
		case *TNode[string]:
			if x != nil { m.mark(x) }
		case *TNode[any]:
			if x == nil || !m.mark(x) { continue }
			if !m.canPush() { break }
			m.push(x.Value)
		case *Error:
			if x != nil { m.mark(x) }
		case *SyntaxError:
			// Diagnostic fields contain only positions and strings.
		case SyntaxErrors:
			astMarkCells(m, x)
		case Fields:
			if !m.cells(cap(x)) { break }
			values := x[:cap(x)]
			for i := range values {
				if !m.mark(&values[i]) {
					if m.keepAll { break }
					continue
				}
				if !m.canPush() { break }
				m.push(values[i].Value)
			}
		case *Node:
			if x == nil || !m.mark(x) { continue }
			astMarkCells(m, x.children)
			if !m.canPush() { break }
			m.push(x.fields)
`)
	for _, name := range t.structs {
		fmt.Fprintf(b, "\t\tcase *%s:\n\t\t\tif x == nil || !m.mark(x) { continue }\n", t.names[name])
		for _, f := range t.info.fields[name] {
			if !outputLeaf(f.t) {
				fmt.Fprintf(b, "\t\t\tif !m.canPush() { break }\n\t\t\tm.push(x.%s)\n", f.name)
			}
		}
	}
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
	b.WriteString(`		default:
			// Unknown aliases inhibit every domain's sweep.
			m.keepAll = true
		}
	}
	m.complete = !m.keepAll && len(m.todo) == 0
}

`)
	if direct {
		t.outputBoundedHolder(b, "tslabs", t.tlistOrder, true)
	}
	if conversion {
		t.outputBoundedHolder(b, "astConv", t.listOrder, false)
	}
}

func (t *typedGen) outputBoundedHolder(b *strings.Builder, holder string, lists []listConv, direct bool) {
	args := "root any, err error"
	if direct {
		args += ", p *tparser"
	}
	var fields []string
	if !direct {
		fields = append(fields, "match", "error")
	}
	for _, name := range t.structs {
		fields = append(fields, "t"+t.names[name])
	}
	for _, list := range lists {
		fields = append(fields, list.name+"Chunk")
	}
	fitArgs, fitCall := "", ""
	if direct {
		fitArgs, fitCall = "p *tparser", "p"
	}
	fmt.Fprintf(b, "func (a *%s) outputFits(%s) bool {\n", holder, fitArgs)
	b.WriteString("\tbudget := astDefaultOutputBudget()\n\tpreflight := astOutputPreflight{records: budget.records, slots: budget.slots}\n")
	for _, field := range fields {
		fmt.Fprintf(b, "\tif !astCheckChunk(&a.%s, &preflight) { return false }\n", field)
	}
	if direct {
		b.WriteString("\tif !astCheckBlocks(p.matchBlocks, &preflight) { return false }\n")
	}
	b.WriteString("\treturn true\n}\n\n")
	fmt.Fprintf(b, "func (a *%s) publish(%s) {\n\tfits := a.outputFits(%s)\n", holder, args, fitCall)
	b.WriteString("\tm := &astOutputMark{budget: astDefaultOutputBudget(), keepAll: !fits}\n\tif fits { m.run(root, err) }\n")
	call := "m"
	if direct {
		call += ", p"
	}
	fmt.Fprintf(b, "\tif m.scalarRefs && !m.keepAll { a.promoteScalarOwners(%s) }\n", call)
	b.WriteString("\tif m.complete && !m.keepAll {\n")
	for _, field := range fields {
		fmt.Fprintf(b, "\t\tastSweepChunk(&a.%s, m.live)\n", field)
	}
	if direct {
		b.WriteString("\t\tfor _, block := range p.matchBlocks {\n\t\t\tfor i := range block {\n\t\t\t\tif _, live := m.live[&block[i].TNode]; !live { block[i] = tmatch{} }\n\t\t\t}\n\t\t}\n")
	}
	b.WriteString("\t}\n")
	for _, field := range fields {
		fmt.Fprintf(b, "\tastDropChunk(&a.%s)\n", field)
	}
	if direct {
		b.WriteString("\tclear(p.matchBlocks)\n\tp.matchBlocks = nil\n")
	}
	b.WriteString("}\n\n")
	t.outputOwnerPromotion(b, holder, direct)
}

const astBoundedOutputSweepHelpers = `// Budgets are fixed by generation; these private values also support whitebox tests.
// They bound examination/sweep, not ownership-directory collection during parsing.
type astOutputBudget struct {
	work, keys, pending, records, slots int
}

func astDefaultOutputBudget() astOutputBudget {
	return astOutputBudget{work: 256, keys: 256, pending: 64, records: 64, slots: 4096}
}

type astOutputPreflight struct { records, slots int }

func astCheckBlocks[T any](blocks [][]T, p *astOutputPreflight) bool {
	// Charge the domain, including an empty directory, before examining it.
	if p.records <= 0 || p.slots < 0 { return false }
	p.records--
	for i := range blocks {
		if p.records <= 0 { return false }
		p.records--
		n := len(blocks[i])
		if n > p.slots { return false }
		p.slots -= n
	}
	return true
}

func astCheckChunk[T any](s *astChunk[T], p *astOutputPreflight) bool {
	return astCheckBlocks(s.blocks, p)
}

type astOutputMark struct {
	live map[any]struct{}
	todo []any
	budget astOutputBudget
	keepAll, complete, scalarRefs bool
}

func (m *astOutputMark) protectOwner(owner any, fields ...any) {
	if !m.charge(1) { m.complete = false; return }
	if _, found := m.live[owner]; found { return }
	for _, field := range fields {
		if !m.charge(1) { m.complete = false; return }
		if _, found := m.live[field]; found { m.run(owner, nil); return }
	}
}

func (m *astOutputMark) charge(n int) bool {
	if m.keepAll || n < 0 || n > m.budget.work {
		m.keepAll = true
		return false
	}
	m.budget.work -= n
	return true
}

func (m *astOutputMark) canPush() bool {
	if m.keepAll || m.budget.work <= 0 || len(m.todo) >= m.budget.pending {
		m.keepAll = true
		return false
	}
	return true
}

func (m *astOutputMark) push(v any) bool {
	if !m.canPush() || !m.charge(1) { return false }
	if m.todo == nil { m.todo = make([]any, 0, m.budget.pending) }
	m.todo = append(m.todo, v)
	return true
}

func (m *astOutputMark) cells(n int) bool {
	// Reject an oversized capacity before its loop, key allocation or value boxing.
	if m.keepAll || n < 0 || n > m.budget.work {
		m.keepAll = true
		return false
	}
	return true
}

func (m *astOutputMark) mark(pointer any) bool {
	// Every attempted identity/cell examination costs work, including aliases.
	if !m.charge(1) { return false }
	if _, found := m.live[pointer]; found { return false }
	if m.budget.keys <= 0 {
		m.keepAll = true
		return false
	}
	if m.live == nil { m.live = make(map[any]struct{}, 8) }
	m.live[pointer] = struct{}{}
	m.budget.keys--
	return true
}

func astMarkCells[T any](m *astOutputMark, values []T) {
	if !m.cells(cap(values)) { return }
	values = values[:cap(values)]
	for i := range values {
		if !m.mark(&values[i]) {
			if m.keepAll { return }
			continue
		}
		if !m.canPush() { return }
		m.push(values[i])
	}
}

func astMarkLeafCells[T any](m *astOutputMark, values []T) {
	if !m.cells(cap(values)) { return }
	values = values[:cap(values)]
	for i := range values {
		m.mark(&values[i])
		if m.keepAll { return }
	}
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
