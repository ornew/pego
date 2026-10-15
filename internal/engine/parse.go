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
func (prog *Program) ParseWith(start, input string, o ParseOptions) (n *Node, err error) {
	if o.DiagnosticCallback != nil {
		return nil, fmt.Errorf("diagnostic callback is only supported for stream parsing")
	}
	// Initialization can interrupt before run installs its execution recovery.
	// Preserve caller panics: run already distinguishes bytecode faults from
	// caller panics, so an outer boundary must not classify them again.
	defer func() {
		if x := recover(); x != nil {
			if f, ok := x.(fatal); ok {
				n, err = nil, f.err
			} else if cp, ok := x.(contextPanic); ok {
				panic(cp.value)
			} else {
				panic(x)
			}
		}
	}()
	active, _, control, err := prog.admitParse(start, o, false)
	if err != nil {
		return nil, err
	}
	control.check()
	p := acquirePooledParser(active)
	defer p.releaseBatchScratch()
	p.input = newControlledTextInput(input, o.Unit, !o.Recognize, p.scratch, control)
	p.maxDepth = o.maxDepth(o.Backend)
	p.deferMemo = true
	p.setTrace(o.Trace)
	n, err = active.run(p, o.Backend, start)
	if !p.aborted {
		control.check()
	}
	if o.Recognize {
		n = nil
	}
	return n, err
}

// recognizer returns a program that only checks whether the input conforms to the grammar,
// without building a tree (recognition); it is built on first use. The memo for value-free rules
// belongs to that separate program, so it does not mix with that of ordinary parses.
func (prog *Program) recognizer() (*Program, error) {
	prog.recOnce.Do(func() {
		if prog.Grammar == nil {
			if prog.recognitionModule != nil {
				prog.rec = moduleProgram(prog.recognitionModule)
				prog.rec.recognitionStarts = prog.recognitionEntries
				return
			}
			prog.recErr = fmt.Errorf("recognition needs the grammar or a recognition bundle; recompile this compiled grammar from source")
			return
		}
		flags := make([]ruleFlags, len(prog.rules))
		for i, r := range prog.rules {
			flags[i] = ruleFlags{memo: r.memo, leader: r.leader, positional: r.positional, twin: r.twinOK}
		}
		prog.rec, prog.recErr = build(prog.Grammar, Options{recognize: true}, flags)
		if prog.recErr == nil && prog.recognitionModule != nil {
			prog.rec.moduleOnce.Do(func() { prog.rec.module = prog.recognitionModule })
			prog.rec.recognitionStarts = prog.recognitionEntries
		}
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
		r = prog.vmRule(prog.vm, name)
	case BytecodeIterative:
		prog.ivmOnce.Do(func() { prog.ivm = newVMProgram(prog.Module(), true) })
		r = prog.vmRule(prog.ivm, name)
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
	if cp, ok := x.(contextPanic); ok {
		panic(cp.value)
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
	// Admission invokes caller-owned Context methods before execution recovery.
	// Preserve their panic values rather than classifying them as VM faults.
	defer func() {
		if x := recover(); x != nil {
			if f, ok := x.(fatal); ok {
				err = f.err
			} else if cp, ok := x.(contextPanic); ok {
				panic(cp.value)
			} else {
				panic(x)
			}
		}
	}()
	if o.Recognize {
		return fmt.Errorf("recognition is not supported for stream parsing")
	}
	_, rule, control, err := prog.admitParse(start, o, true)
	if err != nil {
		return err
	}
	control.check()
	p := newControlledStreamParser(prog, r, o.Unit, control)
	defer p.releaseTraceReferences() // after error recovery, including rethrown panics
	control.check()
	p.maxDepth = o.maxDepth(o.Backend)
	p.emit = emit
	p.diagnostic = o.DiagnosticCallback
	if p.diagnostic != nil && p.emit == nil {
		// A diagnostic-only stream still needs element commit boundaries.
		p.emit = func(*Node) error { return nil }
	}
	if streamPrefixIsolation && p.emit == nil {
		p.nodeChunks = 0 // No callback: collect the ordinary repetition instead.
	}
	p.descs = prog.descs(o.Backend)
	p.setTrace(o.Trace)
	defer func() {
		x := recover()
		if x != nil && (p.readerCall || p.emitCall) {
			// A caller panic is not a VM fault. Trace callbacks may have
			// already attributed a nested reader panic; unwrap it as usual.
			if tp, ok := x.(tracePanic); ok {
				panic(tp.value)
			}
			panic(x)
		}
		recoverParse(x, prog.backend(o.Backend), &err)
	}()
	control.check()
	_, ok := p.call(rule, 0)
	if ok && p.atEOF() {
		control.check()
		if p.diagnostic != nil {
			if err := p.reportRecovered(); err != nil {
				return err
			}
		}
		if len(p.recovered) > 0 {
			return SyntaxErrors(p.recovered)
		}
		return nil
	}
	if ok {
		p.expect(p.pos, idEndInput)
	}
	err = p.syntaxError()
	control.check()
	return err
}

// emitStream marks only caller execution. Reset before handling a returned
// error or committing the prefix, so later VM faults retain their attribution.
func (p *parser) emitStream(n *Node) error {
	if workControlsEnabled {
		p.control.check()
	}
	if p.diagnostic != nil {
		if err := p.reportRecovered(); err != nil {
			return err
		}
	}
	p.emitCall = true
	err := p.emit(n)
	p.emitCall = false
	if err == nil && workControlsEnabled {
		p.control.check()
	}
	return err
}

// reportRecovered runs only at an irreversible stream boundary. Earlier
// recoveries in a speculative branch may have been removed by backtracking.
func (p *parser) reportRecovered() error {
	if p.diagnostic == nil {
		return nil
	}
	if p.diagnosed > len(p.recovered) {
		p.diagnosed = len(p.recovered)
	}
	for p.diagnosed < len(p.recovered) {
		if workControlsEnabled {
			p.control.check()
		}
		e := *p.recovered[p.diagnosed]
		e.Expected = append([]string(nil), e.Expected...)
		e.Messages = append([]string(nil), e.Messages...)
		p.emitCall = true
		err := p.diagnostic(&e)
		p.emitCall = false
		if err != nil {
			return err
		}
		p.diagnosed++
		if workControlsEnabled {
			p.control.check()
		}
	}
	return nil
}
