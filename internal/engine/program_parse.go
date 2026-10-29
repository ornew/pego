package engine

import (
	"fmt"
	"io"
	"runtime"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/value"
)

// Parse parses the whole input with the rule start.
// It also returns a *SyntaxError if only part of the input matched.
// If errors were recovered with #recover, it returns SyntaxErrors along with the resulting node.
// If the whole input still did not match after recovery, it returns only the last error.
func (prog *Program) Parse(start, input string) (*value.Node, error) {
	return prog.ParseWith(start, input, ParseOptions{})
}

// ParseWith parses with options such as the position unit.
func (prog *Program) ParseWith(start, input string, o ParseOptions) (n *value.Node, err error) {
	if err := o.validateStreamDiagnostics(false); err != nil {
		return nil, err
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
	start = prog.ResolveStart(start) // an empty name selects the default start rule
	if err := prog.checkMode(o, false); err != nil {
		return nil, err
	}
	active, _, control, err := prog.admitParse(start, o, false)
	if err != nil {
		return nil, err
	}
	control.check()
	p := acquirePooledParser(&active.sharedProgram)
	defer p.releaseBatchScratch()
	p.input = newControlledTextInput(input, o.Unit, !o.Recognize, p.scratch, control)
	p.maxDepth = o.maxDepth(o.Backend)
	p.deferMemo = true
	if o.MissingTokens > 0 && active.HasMissing {
		p.missing, p.missingLen = &missingTokenState{limit: o.MissingTokens}, 0
	}
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

// recognizable reports an error when prog cannot recognize: without the grammar, only the module
// of a compiled grammar in recognize mode recognizes. It builds nothing, so parses check it before
// any other work.
func (prog *Program) recognizable() error {
	if prog.Grammar != nil || prog.loaded && prog.mode == CompiledRecognize {
		return nil
	}
	return modeError(prog.mode, CompiledRecognize, "recognition needs")
}

// Recognizer returns a program that only checks whether the input conforms to the grammar,
// without building a tree (recognition); it is built on first use. The memo for value-free rules
// belongs to that separate program, so it does not mix with that of ordinary parses.
func (prog *Program) Recognizer() (*Program, error) {
	prog.recOnce.Do(func() {
		if prog.Grammar == nil {
			if prog.recErr = prog.recognizable(); prog.recErr == nil {
				prog.rec = prog // the module of a compiled grammar in recognize mode is the recognizer's
			}
			return
		}
		flags := make([]compile.RuleFlags, len(prog.RuleList))
		for i, r := range prog.RuleList {
			flags[i] = compile.RuleFlags{Memo: r.Memo, Leader: r.Leader, Positional: r.Positional, Twin: r.TwinOK}
		}
		prog.rec, prog.recErr = build(prog.Grammar, compile.Options{Recognize: true, Recognizer: true, SourceInfo: prog.RecordsSourceInfo, DefaultValue: prog.MakesDefaultValue}, flags)
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

// closureRule returns the rule the closure backend runs for the rule name, or nil if there is none.
func (prog *Program) closureRule(name string) *rule {
	if sr := prog.ByName[name]; sr != nil {
		return prog.closureRules[sr.ID]
	}
	return nil
}

// rule returns the rule name for backend b.
func (prog *Program) rule(b Backend, name string) (*rule, error) {
	if err := compile.EntryError(name); err != nil {
		return nil, err
	}
	if prog.HasMissing && prog.backend(b) != Closure {
		return nil, compile.MissingUnsupported("bytecode backends")
	}
	var r *rule
	switch prog.backend(b) {
	case Bytecode:
		prog.vmOnce.Do(func() { prog.vm = newVMProgram(prog.Module()) })
		r = prog.vm.byName[name]
	case BytecodeIterative:
		prog.ivmOnce.Do(func() { prog.ivm = newIVMProgram(prog.Module()) })
		r = prog.ivm.byName[name]
	case Closure:
		if prog.Grammar == nil {
			return nil, errClosureNeedsSource
		}
		r = prog.closureRule(name)
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
	return prog.DescTable
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
func (prog *Program) run(p *parser, b Backend, start string) (n *value.Node, err error) {
	p.aborted = false
	r, err := prog.rule(b, start)
	if err != nil {
		return nil, err
	}
	p.descs = prog.descs(b)
	p.memo.stride = prog.NSeen
	if be := prog.backend(b); be == Bytecode || be == BytecodeIterative {
		p.memo.stride = prog.vmFor(be).nseen
	}
	p.memo.sparse = sparseMemo && p.memo.stride > 64 && p.loaded() > 1024
	p.seedVars()
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
		p.expect(p.pos, compile.IDEndInput)
	}
	return nil, p.syntaxError()
}

// Rules returns the names of the rules that can be entry points, in declaration order. A hidden
// rule is not one.
func (prog *Program) Rules() []string {
	if prog.Grammar == nil {
		m := prog.Module()
		var names []string
		seen := map[string]bool{}
		for _, r := range m.Rules {
			if name := m.Strings[r.Name]; !seen[name] && !compile.HiddenRule(name) { // value-free twins follow the original rules
				seen[name] = true
				names = append(names, name)
			}
		}
		return names
	}
	names := make([]string, 0, len(prog.RuleList))
	for _, r := range prog.RuleList {
		if !compile.HiddenRule(r.Name) {
			names = append(names, r.Name)
		}
	}
	return names
}

// ParseStream parses with the rule start while reading from r.
// Each element of the #stream repetition at the top level of start's body is passed to emit as
// soon as it matches. Passed elements are not retained and the input before them is discarded,
// so the whole input never has to be held in memory.
// If emit returns an error, the parse is aborted and that error is returned.
func (prog *Program) ParseStream(start string, r io.Reader, emit func(*value.Node) error) error {
	return prog.ParseStreamWith(start, r, emit, ParseOptions{})
}

// ParseStreamWith parses a stream with options such as the position unit.
func (prog *Program) ParseStreamWith(start string, r io.Reader, emit func(*value.Node) error, o ParseOptions) (err error) {
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
	if err := o.validateStreamDiagnostics(true); err != nil {
		return err
	}
	if o.Recognize {
		return fmt.Errorf("recognition is not supported for stream parsing")
	}
	start = prog.ResolveStart(start) // an empty name selects the default start rule
	if err := prog.checkMode(o, false); err != nil {
		return err
	}
	_, rule, control, err := prog.admitParse(start, o, true)
	if err != nil {
		return err
	}
	control.check()
	p := newControlledStreamParser(&prog.sharedProgram, r, o.Unit, control)
	defer p.releaseTraceReferences() // after error recovery, including rethrown panics
	control.check()
	p.maxDepth = o.maxDepth(o.Backend)
	p.emit = emit
	p.diagnostic = o.DiagnosticCallback
	p.diagnosticTail.limit = o.DiagnosticLimit
	if (p.diagnostic != nil || o.DiagnosticLimit > 0) && p.emit == nil {
		// A diagnostic-only stream still needs element commit boundaries.
		p.emit = func(*value.Node) error { return nil }
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
	p.seedVars()
	_, ok := p.call(rule, 0)
	if ok && p.atEOF() {
		control.check()
		if p.diagnostic != nil || p.diagnosticTail.limit > 0 {
			if err := p.reportRecovered(); err != nil {
				return err
			}
		}
		if p.diagnosticTail.limit > 0 {
			return p.diagnosticTail.result(p)
		}
		if len(p.recovered) > 0 {
			return SyntaxErrors(p.recovered)
		}
		return nil
	}
	if ok {
		p.expect(p.pos, compile.IDEndInput)
	}
	err = p.syntaxError()
	control.check()
	return err
}

func (prog *Program) admitMissing(o ParseOptions, stream bool) error {
	if o.MissingTokens < 0 {
		return fmt.Errorf("missing token limit must be nonnegative")
	}
	if stream && (prog.HasMissing || o.MissingTokens > 0) {
		return compile.MissingUnsupported("stream parsing")
	}
	if prog.backend(o.Backend) != Closure && (prog.HasMissing || o.MissingTokens > 0) {
		return compile.MissingUnsupported("bytecode backends")
	}
	return nil
}
