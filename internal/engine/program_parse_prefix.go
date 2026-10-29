package engine

import (
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
	if err := validateStreamDiagnostics(o, false); err != nil {
		return PrefixResult{}, err
	}
	if prog.HasMissing || o.MissingTokens > 0 {
		return PrefixResult{}, compile.MissingUnsupported("prefix parsing")
	}
	defer func() {
		if x := recover(); x != nil {
			if f, ok := x.(fatal); ok {
				result, err = PrefixResult{}, f.err
			} else if cp, ok := x.(contextPanic); ok {
				panic(cp.value)
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
	control.check()
	p := acquirePooledParser(&active.sharedProgram)
	defer p.releaseBatchScratch()
	p.input = newControlledTextInput(input, o.Unit, !o.Recognize, p.scratch, control)
	p.maxDepth = depthLimit(o, o.Backend)
	p.deferMemo = true
	p.setTrace(o.Trace)
	result, err = active.runPrefix(p, o.Backend, start)
	if !p.aborted {
		control.check()
	}
	if o.Recognize {
		result.Node = nil
	}
	result.ConsumedBytes = p.input.prefixByteOffset(input, result.Consumed)

	if !p.aborted {
		control.check()
	}
	return result, err
}

func (prog *Program) runPrefix(p *parser, b Backend, start string) (result PrefixResult, err error) {
	p.aborted = false
	r, err := prog.rule(b, start)
	if err != nil {
		return PrefixResult{}, err
	}
	p.descs = prog.descs(b)
	p.memo.stride = prog.NSeen
	if be := prog.backend(b); be == Bytecode || be == BytecodeIterative {
		p.memo.stride = prog.vmFor(be).nseen
	}
	p.memo.sparse = sparseMemo && p.memo.stride > 64 && p.loaded() > 1024
	defer func() {
		if x := recover(); x != nil {
			p.aborted = true
			result = PrefixResult{}
			recoverParse(x, prog.backend(b), &err)
		}
	}()
	p.seedVars()
	v, ok := p.call(r, 0)
	if !ok {
		return PrefixResult{}, p.syntaxError()
	}
	result = PrefixResult{Node: v, Consumed: p.pos}
	if len(p.recovered) > 0 {
		err = SyntaxErrors(p.recovered)
	}
	return result, err
}
