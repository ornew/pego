package engine

import (
	"fmt"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/engine/vm"
)

// admitParse validates fixed metadata before caller Context methods. Callers
// resolve an empty start to the default start rule first. Lazy setup
// is not charged, but active calls check immediately around each component.
func (prog *Program) admitParse(start string, o ParseOptions, stream bool) (*Program, *core.Rule, *core.BudgetControl, error) {
	if err := prog.CheckStart(start); err != nil {
		return nil, nil, nil, err
	}
	if err := prog.admitMissing(o, stream); err != nil {
		return nil, nil, nil, err
	}
	if o.Context == nil && o.Limits.Budget == 0 {
		if o.Recognize {
			if _, err := prog.preflightRule(o.Backend, start, stream); err != nil {
				return nil, nil, nil, err
			}
			active, err := prog.Recognizer()
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
		if stream && !r.Stream {
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
	if o.Recognize {
		if err := prog.recognizable(); err != nil {
			return nil, nil, nil, err
		}
	}
	c := core.NewBudgetControl(o.Context, o.Limits)
	if !core.BudgetControlsEnabled && c != nil {
		return nil, nil, nil, fmt.Errorf("budget controls are unavailable in the reference build")
	}
	c.Check()
	active := prog
	if o.Recognize {
		active, err = prog.Recognizer()
		c.Check()
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
// Programs loaded from a compiled grammar already own their validated immutable module.
func (prog *Program) preflightRule(b Backend, name string, stream bool) (bool, error) {
	switch prog.backend(b) {
	case Closure:
		if prog.Grammar == nil {
			return false, errClosureNeedsSource
		}
	case Bytecode, BytecodeIterative:
	default:
		return false, fmt.Errorf("unknown backend %d", b)
	}
	if prog.Grammar != nil {
		r := prog.ByName[name]
		if r == nil {
			return false, fmt.Errorf("rule %s is not defined", name)
		}
		return r.Stream, nil
	}
	if prog.Bytecode != nil {
		for _, ri := range prog.Bytecode.Rules {
			if prog.Bytecode.Strings[ri.Name] == name {
				return stream && prog.Bytecode.RuleStreams(ri.Entry), nil
			}
		}
	}
	return false, fmt.Errorf("rule %s is not defined", name)
}

func (prog *Program) prepareControlledRule(b Backend, name string, c *core.BudgetControl) (*core.Rule, error) {
	if !core.BudgetControlsEnabled || c == nil || prog.backend(b) == Closure {
		return prog.rule(b, name)
	}
	c.Check()
	m := prog.Module()
	c.Check() // Also the checkpoint immediately before VM preparation.
	switch prog.backend(b) {
	case Bytecode:
		prog.vmOnce.Do(func() { prog.vm = vm.New(m) })
	case BytecodeIterative:
		prog.ivmOnce.Do(func() { prog.ivm = vm.NewIterative(m) })
	}
	c.Check()
	return prog.rule(b, name)
}

// prepareControlledProgram builds the metered closure graph only when needed.
// Context methods stay outside Once so reentrant calls cannot deadlock setup.
func (prog *Program) prepareControlledProgram(b Backend, c *core.BudgetControl) (*Program, error) {
	if !core.BudgetControlsEnabled || c == nil || prog.backend(b) != Closure {
		return prog, nil
	}
	c.Check()
	prog.budgetOnce.Do(func() {
		flags := make([]compile.RuleFlags, len(prog.RuleList))
		for i, r := range prog.RuleList {
			flags[i] = compile.RuleFlags{Memo: r.Memo, Leader: r.Leader, Positional: r.Positional, Twin: r.TwinOK}
		}
		prog.budgetProgram, prog.budgetErr = build(prog.Grammar, compile.Options{
			Recognize:          prog.BudgetRecognize,
			Recognizer:         prog.BudgetRecognizer,
			NoProjections:      prog.NoProjections,
			ControlledDispatch: true,
			SourceInfo:         prog.RecordsSourceInfo,
		}, flags)
	})
	c.Check()
	if prog.budgetProgram == nil && prog.budgetErr == nil {
		return nil, fmt.Errorf("controlled closure initialization did not complete")
	}
	return prog.budgetProgram, prog.budgetErr
}
