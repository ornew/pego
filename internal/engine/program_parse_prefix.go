package engine

import (
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/value"

	"github.com/ornew/pego/internal/compile"
)

// PrefixResult records the successful cursor independently of the rule's value.
type PrefixResult struct {
	Node          *value.Node
	Consumed      int
	ConsumedBytes int
}

// ParsePrefixWith parses a named rule without an implicit whole-input check.
// Recovered success returns consumption and SyntaxErrors; unrecovered failures
// and fatal errors return a zero result. Explicit EOF assertions still see the
// entire supplied input.
func (prog *Program) ParsePrefixWith(start, input string, o ParseOptions) (result PrefixResult, err error) {
	if err := core.ValidateStreamDiagnostics(o, false); err != nil {
		return PrefixResult{}, err
	}
	if prog.HasMissing || o.MissingTokens > 0 {
		return PrefixResult{}, compile.MissingUnsupported("prefix parsing")
	}
	defer func() {
		if x := recover(); x != nil {
			if f, ok := x.(core.Fatal); ok {
				result, err = PrefixResult{}, f.Err
			} else if cp, ok := x.(core.ContextPanic); ok {
				panic(cp.Value)
			} else {
				panic(x)
			}
		}
	}()
	start = prog.ResolveStart(start) // an empty name selects the default start rule
	if err := prog.checkMode(o, false); err != nil {
		return PrefixResult{}, err
	}
	active, _, control, err := prog.admitParse(start, o, false)
	if err != nil {
		return PrefixResult{}, err
	}
	control.Check()
	p := core.AcquirePooledParser(&active.sharedProgram)
	defer p.ReleaseBatchScratch()
	p.Input = core.NewControlledTextInput(input, o.Unit, !o.Recognize, p.Scratch, control)
	p.MaxDepth = core.DepthLimit(o, o.Backend)
	p.DeferMemo = true
	p.SetTrace(o.Trace)
	result, err = active.runPrefix(p, o.Backend, start)
	if !p.Aborted {
		control.Check()
	}
	if o.Recognize {
		result.Node = nil
	}
	result.ConsumedBytes = p.Input.PrefixByteOffset(input, result.Consumed)

	if !p.Aborted {
		control.Check()
	}
	return result, err
}

func (prog *Program) runPrefix(p *core.Parser, b Backend, start string) (result PrefixResult, err error) {
	p.Aborted = false
	r, err := prog.rule(b, start)
	if err != nil {
		return PrefixResult{}, err
	}
	p.Descs = prog.descs(b)
	p.Memo.Stride = prog.NSeen
	if be := prog.backend(b); be == Bytecode || be == BytecodeIterative {
		p.Memo.Stride = prog.vmFor(be).NumSeen()
	}
	p.Memo.Sparse = core.SparseMemo && p.Memo.Stride > 64 && p.Loaded() > 1024
	defer func() {
		if x := recover(); x != nil {
			p.Aborted = true
			result = PrefixResult{}
			recoverParse(x, prog.backend(b), &err)
		}
	}()
	p.SeedVars()
	v, ok := p.Call(r, 0)
	if !ok {
		return PrefixResult{}, p.SyntaxError()
	}
	result = PrefixResult{Node: v, Consumed: p.Pos}
	if len(p.Recovered) > 0 {
		err = SyntaxErrors(p.Recovered)
	}
	return result, err
}
