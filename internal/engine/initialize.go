package engine

// initialize evaluates grammar assignments in a fresh parse environment.
// The caller invokes it after admission and before the selected start rule.
func (prog *Program) initialize(p *parser, backend Backend) {
	for _, assignment := range prog.init {
		ctx := p.useCtx(evalCtx{p: p, scope: newScope(), frame: emptyFrame, start: p.pos, end: p.pos, cbase: len(p.created)})
		value, err := ctx.eval(assignment.Value)
		p.dropCreated(ctx.cbase)
		p.bindInit(assignment.Name, value, err)
	}
	if prog.Grammar != nil {
		return
	}
	initProg := prog
	if prog.initOwner != nil {
		initProg = prog.initOwner
	}
	m := initProg.Module()
	if len(m.Init) == 0 {
		return
	}
	be := initProg.backend(backend)
	if be == BytecodeIterative {
		initProg.ivmOnce.Do(func() { initProg.ivm = newVMProgram(m, true) })
	} else {
		initProg.vmOnce.Do(func() { initProg.vm = newVMProgram(m, false) })
	}
	vm := initProg.vmFor(be)
	for _, init := range m.Init {
		ctx := p.useCtx(evalCtx{p: p, scope: newScope(), frame: emptyFrame, start: p.pos, end: p.pos, cbase: len(p.created)})
		value, err := vm.evaluator(init.Expr, false)(ctx)
		p.dropCreated(ctx.cbase)
		p.bindInit(m.Strings[init.Name], value, err)
	}
}

func (p *parser) bindInit(name string, value any, err error) {
	if err != nil {
		p.failAction("init", err)
	}
	switch value.(type) {
	case int, string, bool:
	default:
		p.failAction("init", p.actionError("variable ", name, " must be int, string, or bool, got ", typeName(value)))
	}
	p.bind(name, value)
}
