package engine

import "fmt"

// admitParse validates fixed metadata before caller Context methods. Lazy setup
// is not charged, but active calls check immediately around each component.
func (prog *Program) admitParse(start string, o ParseOptions, stream bool) (*Program, *rule, *workControl, error) {
	if err := prog.admitMissing(o, stream); err != nil {
		return nil, nil, nil, err
	}
	if o.Context == nil && o.WorkLimits.MaxInputWork == 0 && o.WorkLimits.MaxSteps == 0 {
		if o.Recognize {
			if _, err := prog.preflightRule(o.Backend, start, stream); err != nil {
				return nil, nil, nil, err
			}
			active, err := prog.recognizer()
			if err != nil {
				return nil, nil, nil, err
			}
			r, err := active.rule(o.Backend, start)
			return active, r, nil, err
		}
		r, err := prog.rule(o.Backend, start)
		if err != nil {
			return nil, nil, nil, err
		}
		if stream && !r.stream {
			return nil, nil, nil, fmt.Errorf("rule %s has no #stream repetition", start)
		}
		return prog, r, nil, nil
	}
	isStream, err := prog.preflightRule(o.Backend, start, stream)
	if err != nil {
		return nil, nil, nil, err
	}
	if stream && !isStream {
		return nil, nil, nil, fmt.Errorf("rule %s has no #stream repetition", start)
	}
	if o.Recognize && prog.Grammar == nil && prog.recognitionModule == nil {
		return nil, nil, nil, fmt.Errorf("recognition needs the grammar or a recognition bundle; recompile this compiled grammar from source")
	}
	c := newWorkControl(o.Context, o.WorkLimits)
	if !workControlsEnabled && c != nil {
		return nil, nil, nil, fmt.Errorf("work controls are unavailable in the reference build")
	}
	c.check()
	active := prog
	if o.Recognize {
		active, err = prog.recognizer()
		c.check()
		if err != nil {
			return nil, nil, nil, err
		}
	}
	active, err = active.prepareControlledProgram(o.Backend, c)
	if err != nil {
		return nil, nil, nil, err
	}
	r, err := active.prepareControlledRule(o.Backend, start, c)
	return active, r, c, err
}

// preflightRule uses compilation metadata without materializing a Module or VM.
// Programs loaded without an AST already own their validated immutable module.
func (prog *Program) preflightRule(b Backend, name string, stream bool) (bool, error) {
	switch prog.backend(b) {
	case Closure:
		if prog.Grammar == nil {
			return false, fmt.Errorf("the closure backend needs the grammar, which the compiled grammar omits")
		}
	case Bytecode, BytecodeIterative:
	default:
		return false, fmt.Errorf("unknown backend %d", b)
	}
	if prog.Grammar != nil {
		r := prog.byName[name]
		if r == nil {
			return false, fmt.Errorf("rule %s is not defined", name)
		}
		return r.stream, nil
	}
	if prog.module != nil {
		for _, ri := range prog.module.Rules {
			if prog.module.Strings[ri.Name] == name {
				return stream && moduleStreams(prog.module, ri.Entry), nil
			}
		}
	}
	return false, fmt.Errorf("rule %s is not defined", name)
}

func (prog *Program) prepareControlledRule(b Backend, name string, c *workControl) (*rule, error) {
	if !workControlsEnabled || c == nil || prog.backend(b) == Closure {
		return prog.rule(b, name)
	}
	c.check()
	m := prog.Module()
	c.check() // Also the checkpoint immediately before VM preparation.
	switch prog.backend(b) {
	case Bytecode:
		prog.vmOnce.Do(func() { prog.vm = newVMProgram(m, false) })
	case BytecodeIterative:
		prog.ivmOnce.Do(func() { prog.ivm = newVMProgram(m, true) })
	}
	c.check()
	return prog.rule(b, name)
}

// moduleStreams inspects rule bytecode metadata without building an executable VM.
func moduleStreams(m *Module, entry int) bool {
	for _, in := range m.Code[entry:] {
		switch {
		case in.Op == OpReturn:
			return false
		case in.Op == OpNext && in.B == 2:
			return true
		}
	}
	return false
}

// prepareControlledProgram builds the metered closure graph only when needed.
// Context methods stay outside Once so reentrant calls cannot deadlock setup.
func (prog *Program) prepareControlledProgram(b Backend, c *workControl) (*Program, error) {
	if !workControlsEnabled || c == nil || prog.backend(b) != Closure {
		return prog, nil
	}
	c.check()
	prog.workOnce.Do(func() {
		flags := make([]ruleFlags, len(prog.rules))
		for i, r := range prog.rules {
			flags[i] = ruleFlags{memo: r.memo, leader: r.leader, positional: r.positional, twin: r.twinOK}
		}
		prog.workProgram, prog.workErr = build(prog.Grammar, Options{
			recognize:          prog.workRecognize,
			noProjections:      prog.noProjections,
			noTextStructs:      prog.noTextStructs,
			boxedComparisons:   prog.boxedComparisons,
			controlledDispatch: true,
			checks:             prog.checks,
			SourceInfo:         prog.sourceInfo,
		}, flags)
	})
	c.check()
	if prog.workProgram == nil && prog.workErr == nil {
		return nil, fmt.Errorf("controlled closure initialization did not complete")
	}
	return prog.workProgram, prog.workErr
}
