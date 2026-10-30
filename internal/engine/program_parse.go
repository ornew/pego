package engine

import (
	"fmt"
	"io"
	"math"
	"runtime"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/engine/vm"
	"github.com/ornew/pego/internal/limits"
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
	if err := core.ValidateStreamDiagnostics(o, false); err != nil {
		return nil, err
	}
	// Initialization can interrupt before run installs its execution recovery.
	// Preserve caller panics: run already distinguishes bytecode faults from
	// caller panics, so an outer boundary must not classify them again.
	defer func() {
		if x := recover(); x != nil {
			if f, ok := x.(core.Fatal); ok {
				n, err = nil, f.Err
			} else if cp, ok := x.(core.ContextPanic); ok {
				panic(cp.Value)
			} else {
				panic(x)
			}
		}
	}()
	start = prog.ResolveStart(start) // an empty name selects the default start rule
	if err := prog.checkMode(o, false); err != nil {
		return nil, err
	}
	active, _, control, err := prog.admitParse(start, o, false, input)
	if err != nil {
		return nil, err
	}
	control.Check()
	p := core.AcquirePooledParser(&active.sharedProgram)
	defer p.ReleaseBatchScratch()
	p.Input = core.NewControlledTextInput(input, o.Unit, !o.Recognize, p.Scratch, control)
	p.MaxDepth = core.DepthLimit(o, o.Backend)
	p.Expectations = o.Expectations
	p.DeferMemo = true
	if o.MissingTokens > 0 && active.HasMissing {
		p.Missing, p.MissingLen = &core.MissingTokenState{Limit: o.MissingTokens}, 0
	}
	p.SetTrace(o.Trace)
	n, err = active.Run(p, o.Backend, start)
	if !p.Aborted {
		control.Check()
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
		prog.rec, prog.recErr = build(prog.SourceGrammar(), compile.Options{Recognize: true, Recognizer: true, SourceInfo: prog.RecordsSourceInfo, DefaultValue: prog.MakesDefaultValue}, flags)
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
func (prog *Program) closureRule(name string) *core.Rule {
	if sr := prog.ByName[name]; sr != nil {
		return prog.closureRules[sr.ID]
	}
	return nil
}

// rule returns the rule name for backend b.
func (prog *Program) rule(b Backend, name string) (*core.Rule, error) {
	if err := compile.EntryError(name); err != nil {
		return nil, err
	}
	if prog.HasMissing && prog.backend(b) != Closure {
		return nil, compile.MissingUnsupported("bytecode backends")
	}
	var r *core.Rule
	switch prog.backend(b) {
	case Bytecode:
		prog.vmOnce.Do(func() { prog.vm = vm.New(prog.Module()) })
		r = prog.vm.Rule(name)
	case BytecodeIterative:
		prog.ivmOnce.Do(func() { prog.ivm = vm.NewIterative(prog.Module()) })
		r = prog.ivm.Rule(name)
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
func (prog *Program) vmFor(b Backend) *vm.Program {
	if b == BytecodeIterative {
		return prog.ivm
	}
	return prog.vm
}

// descs returns the expectation table of backend b (call it after rule has prepared it).
func (prog *Program) descs(b Backend) []string {
	switch prog.backend(b) {
	case Bytecode:
		return prog.vm.Descs()
	case BytecodeIterative:
		return prog.ivm.Descs()
	}
	return prog.DescTable
}

// recoverParse turns an aborted parse (fatal) into an error. Runtime faults in bytecode
// execution are reported as errors caused by an invalid module.
func recoverParse(x any, b Backend, err *error) {
	if x == nil {
		return
	}
	if f, ok := x.(core.Fatal); ok {
		*err = f.Err
		return
	}
	if tp, ok := x.(core.TracePanic); ok {
		panic(tp.Value) // a panic of the trace function, not of the parse
	}
	if cp, ok := x.(core.ContextPanic); ok {
		panic(cp.Value)
	}
	if re, ok := x.(runtime.Error); ok && (b == Bytecode || b == BytecodeIterative) {
		*err = fmt.Errorf("invalid bytecode: %v", re)
		return
	}
	panic(x)
}

// Run parses the whole input of p with the start rule start.
func (prog *Program) Run(p *core.Parser, b Backend, start string) (n *value.Node, err error) {
	p.Aborted = false
	r, err := prog.rule(b, start)
	if err != nil {
		return nil, err
	}
	p.Descs = prog.descs(b)
	p.Memo.Stride = prog.NSeen
	if be := prog.backend(b); be == Bytecode || be == BytecodeIterative {
		p.Memo.Stride = prog.vmFor(be).NumSeen()
	}
	p.Memo.Sparse = core.SparseMemo && p.Memo.Stride > 64 && p.Loaded() > 1024
	p.SeedVars()
	defer func() {
		if x := recover(); x != nil {
			p.Aborted = true
			n = nil
			recoverParse(x, prog.backend(b), &err)
		}
	}()
	v, ok := p.Call(r, 0)
	if ok && p.AtEOF() {
		if len(p.Recovered) > 0 {
			return v, SyntaxErrors(p.Recovered)
		}
		return v, nil
	}
	if ok {
		p.Expect(p.Pos, compile.IDEndInput)
	}
	return nil, p.SyntaxError()
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
			if f, ok := x.(core.Fatal); ok {
				err = f.Err
			} else if cp, ok := x.(core.ContextPanic); ok {
				panic(cp.Value)
			} else {
				panic(x)
			}
		}
	}()
	if err := core.ValidateStreamDiagnostics(o, true); err != nil {
		return err
	}
	if o.Recognize {
		return fmt.Errorf("recognition is not supported for stream parsing")
	}
	start = prog.ResolveStart(start) // an empty name selects the default start rule
	if err := prog.checkMode(o, false); err != nil {
		return err
	}
	_, rule, control, err := prog.admitParse(start, o, true, "")
	if err != nil {
		return err
	}
	control.Check()
	p := core.NewControlledStreamParser(&prog.sharedProgram, r, o.Unit, control)
	// A limit beyond math.MaxInt bounds no stream.
	if lim := o.Limits.MaxInput; lim != 0 && lim != limits.Unlimited && lim <= math.MaxInt {
		p.Input.MaxInput = int(lim)
	}
	defer p.ReleaseTraceReferences() // after error recovery, including rethrown panics
	control.Check()
	p.MaxDepth = core.DepthLimit(o, o.Backend)
	p.Expectations = o.Expectations
	p.Emit, p.StreamID = emit, rule.ID
	p.Diagnostic = o.DiagnosticCallback
	p.DiagnosticTail.Limit = o.DiagnosticLimit
	if (p.Diagnostic != nil || o.DiagnosticLimit > 0) && p.Emit == nil {
		// A diagnostic-only stream still needs element commit boundaries.
		p.Emit = func(*value.Node) error { return nil }
	}
	if core.StreamPrefixIsolation && p.Emit == nil {
		p.NodeChunks = 0 // No callback: collect the ordinary repetition instead.
	}
	p.Descs = prog.descs(o.Backend)
	p.SetTrace(o.Trace)
	defer func() {
		x := recover()
		if x != nil && (p.ReaderCall || p.EmitCall) {
			// A caller panic is not a VM fault. Trace callbacks may have
			// already attributed a nested reader panic; unwrap it as usual.
			if tp, ok := x.(core.TracePanic); ok {
				panic(tp.Value)
			}
			panic(x)
		}
		recoverParse(x, prog.backend(o.Backend), &err)
	}()
	control.Check()
	p.SeedVars()
	_, ok := p.Call(rule, 0)
	if ok && p.AtEOF() {
		control.Check()
		if p.Diagnostic != nil || p.DiagnosticTail.Limit > 0 {
			if err := p.ReportRecovered(); err != nil {
				return err
			}
		}
		if p.DiagnosticTail.Limit > 0 {
			return p.DiagnosticTail.Result(p)
		}
		if len(p.Recovered) > 0 {
			return SyntaxErrors(p.Recovered)
		}
		return nil
	}
	if ok {
		p.Expect(p.Pos, compile.IDEndInput)
	}
	err = p.SyntaxError()
	control.Check()
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
