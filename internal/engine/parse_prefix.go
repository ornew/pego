package engine

import "unicode/utf8"

// PrefixResult records the successful cursor independently of the rule's value.
type PrefixResult struct {
	Node          *Node
	Consumed      int
	ConsumedBytes int
}

// ParsePrefixWith parses a named rule without an implicit whole-input check.
// Recovered success returns consumption and SyntaxErrors; unrecovered failures
// and fatal errors return a zero result. Explicit EOF assertions still see the
// entire supplied input.
func (prog *Program) ParsePrefixWith(start, input string, o ParseOptions) (result PrefixResult, err error) {
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
	active, _, control, err := prog.admitParse(start, o, false)
	if err != nil {
		return PrefixResult{}, err
	}
	control.check()
	p := acquirePooledParser(active)
	defer p.releaseBatchScratch()
	p.input = newControlledTextInput(input, o.Unit, !o.Recognize, p.scratch, control)
	p.maxDepth = o.maxDepth(o.Backend)
	p.deferMemo = true
	p.setTrace(o.Trace)
	result, err = active.runPrefix(p, o.Backend, start)
	if !p.aborted {
		control.check()
	}
	if o.Recognize {
		result.Node = nil
	}
	if result.Consumed > 0 {
		if o.Unit == Bytes {
			result.ConsumedBytes = result.Consumed
		} else if len(p.offs) > result.Consumed {
			result.ConsumedBytes = int(p.offs[result.Consumed])
		} else {
			// Recognition need not build the input's lazy byte-offset table. Decode
			// the original bytes, including malformed one-byte replacement scalars.
			off := 0
			for i := 0; i < result.Consumed; i++ {
				p.inputWork(uint64(min(utf8.UTFMax, len(input)-off)))
				_, n := utf8.DecodeRuneInString(input[off:])
				off += n
			}
			result.ConsumedBytes = off
		}
	}
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
	p.memo.stride = prog.nseen
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
