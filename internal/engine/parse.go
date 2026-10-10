package engine

import (
	"fmt"
	"io"
	"runtime"
)

// Parse parses the whole input with the rule start.
// It also returns a *SyntaxError if only part of the input matched.
// If errors were recovered with #recover, it returns SyntaxErrors along with the resulting node.
// If the whole input still did not match after recovery, it returns only the last error.
func (prog *Program) Parse(start, input string) (*Node, error) {
	return prog.ParseWith(start, input, ParseOptions{})
}

// ParseWith parses with options such as the position unit.
func (prog *Program) ParseWith(start, input string, o ParseOptions) (*Node, error) {
	if o.Recognize {
		rp, err := prog.recognizer()
		if err != nil {
			return nil, err
		}
		p := newPooledParser(rp, input, o.Unit, false) // recognition rarely needs token text
		p.maxDepth = o.maxDepth(o.Backend)
		p.deferMemo = true
		p.setTrace(o.Trace)
		_, err = rp.run(p, o.Backend, start)
		p.releaseScratch()
		return nil, err
	}
	p := newPooledParser(prog, input, o.Unit, true)
	p.maxDepth = o.maxDepth(o.Backend)
	p.deferMemo = true
	p.setTrace(o.Trace)
	n, err := prog.run(p, o.Backend, start)
	p.releaseScratch()
	return n, err
}

// recognizer returns a program that only checks whether the input conforms to the grammar,
// without building a tree (recognition); it is built on first use. The memo for value-free rules
// belongs to that separate program, so it does not mix with that of ordinary parses.
func (prog *Program) recognizer() (*Program, error) {
	prog.recOnce.Do(func() {
		if prog.Grammar == nil {
			prog.recErr = fmt.Errorf("recognition needs the grammar, which the compiled grammar omits")
			return
		}
		flags := make([]ruleFlags, len(prog.rules))
		for i, r := range prog.rules {
			flags[i] = ruleFlags{memo: r.memo, leader: r.leader, positional: r.positional, twin: r.twinOK}
		}
		prog.rec, prog.recErr = build(prog.Grammar, Options{recognize: true}, flags)
	})
	return prog.rec, prog.recErr
}

// backend resolves backend b, choosing one based on the grammar when b is the default.
func (prog *Program) backend(b Backend) Backend {
	if b == Default {
		if prog.Grammar == nil {
			return Bytecode
		}
		return Closure
	}
	return b
}

// rule returns the rule name for backend b.
func (prog *Program) rule(b Backend, name string) (*rule, error) {
	var r *rule
	switch prog.backend(b) {
	case Bytecode:
		prog.vmOnce.Do(func() { prog.vm = newVMProgram(prog.Module(), false) })
		r = prog.vm.byName[name]
	case BytecodeIterative:
		prog.ivmOnce.Do(func() { prog.ivm = newVMProgram(prog.Module(), true) })
		r = prog.ivm.byName[name]
	case Closure:
		if prog.Grammar == nil {
			return nil, fmt.Errorf("the closure backend needs the grammar, which the compiled grammar omits")
		}
		r = prog.byName[name]
	default:
		return nil, fmt.Errorf("unknown backend %d", b)
	}
	if r == nil {
		return nil, fmt.Errorf("rule %s is not defined", name)
	}
	return r, nil
}

// vmFor returns the prepared VM program of backend b (Bytecode or BytecodeIterative).
func (prog *Program) vmFor(b Backend) *vmProgram {
	if b == BytecodeIterative {
		return prog.ivm
	}
	return prog.vm
}

// descs returns the expectation table of backend b (call it after rule has prepared it).
func (prog *Program) descs(b Backend) []string {
	switch prog.backend(b) {
	case Bytecode:
		return prog.vm.descs
	case BytecodeIterative:
		return prog.ivm.descs
	}
	return prog.descTable
}

// hasRule reports whether the rule name is defined.
func (prog *Program) hasRule(name string) bool {
	if prog.Grammar == nil {
		_, err := prog.rule(Bytecode, name)
		return err == nil
	}
	return prog.byName[name] != nil
}

// recoverParse turns an aborted parse (fatal) into an error. Runtime faults in bytecode
// execution are reported as errors caused by an invalid module.
func recoverParse(x any, b Backend, err *error) {
	if x == nil {
		return
	}
	if f, ok := x.(fatal); ok {
		*err = f.err
		return
	}
	if tp, ok := x.(tracePanic); ok {
		panic(tp.value) // a panic of the trace function, not of the parse
	}
	if re, ok := x.(runtime.Error); ok && (b == Bytecode || b == BytecodeIterative) {
		*err = fmt.Errorf("invalid bytecode: %v", re)
		return
	}
	panic(x)
}

// run parses the whole input of p with the start rule start.
func (prog *Program) run(p *parser, b Backend, start string) (n *Node, err error) {
	p.aborted = false
	r, err := prog.rule(b, start)
	if err != nil {
		return nil, err
	}
	p.descs = prog.descs(b)
	p.memo.stride = prog.nseen
	if be := prog.backend(b); be == Bytecode || be == BytecodeIterative {
		p.memo.stride = prog.vmFor(be).nseen
	}
	p.memo.sparse = sparseMemo && p.memo.stride > 64 && p.loaded() > 1024
	defer func() {
		if x := recover(); x != nil {
			p.aborted = true
			n = nil
			recoverParse(x, prog.backend(b), &err)
		}
	}()
	v, ok := p.call(r, 0)
	if ok && p.atEOF() {
		if len(p.recovered) > 0 {
			return v, SyntaxErrors(p.recovered)
		}
		return v, nil
	}
	if ok {
		p.expect(p.pos, idEndInput)
	}
	return nil, p.syntaxError()
}

// Rules returns the rule names in declaration order.
func (prog *Program) Rules() []string {
	if prog.Grammar == nil {
		m := prog.Module()
		var names []string
		seen := map[string]bool{}
		for _, r := range m.Rules {
			if name := m.Strings[r.Name]; !seen[name] { // value-free twins follow the original rules
				seen[name] = true
				names = append(names, name)
			}
		}
		return names
	}
	names := make([]string, len(prog.rules))
	for i, r := range prog.rules {
		names[i] = r.name
	}
	return names
}

// ParseStream parses with the rule start while reading from r.
// Each element of the #stream repetition at the top level of start's body is passed to emit as
// soon as it matches. Passed elements are not retained and the input before them is discarded,
// so the whole input never has to be held in memory.
// If emit returns an error, the parse is aborted and that error is returned.
func (prog *Program) ParseStream(start string, r io.Reader, emit func(*Node) error) error {
	return prog.ParseStreamWith(start, r, emit, ParseOptions{})
}

// ParseStreamWith parses a stream with options such as the position unit.
func (prog *Program) ParseStreamWith(start string, r io.Reader, emit func(*Node) error, o ParseOptions) (err error) {
	if o.Recognize {
		return fmt.Errorf("recognition is not supported for stream parsing")
	}
	rule, err := prog.rule(o.Backend, start)
	if err != nil {
		return err
	}
	if !rule.stream {
		return fmt.Errorf("rule %s has no #stream repetition", start)
	}
	p := newStreamParser(prog, r, o.Unit)
	p.maxDepth = o.maxDepth(o.Backend)
	p.emit = emit
	p.descs = prog.descs(o.Backend)
	p.setTrace(o.Trace)
	defer func() {
		recoverParse(recover(), prog.backend(o.Backend), &err)
	}()
	_, ok := p.call(rule, 0)
	if ok && p.atEOF() {
		if len(p.recovered) > 0 {
			return SyntaxErrors(p.recovered)
		}
		return nil
	}
	if ok {
		p.expect(p.pos, idEndInput)
	}
	return p.syntaxError()
}
