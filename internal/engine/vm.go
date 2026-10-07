package engine

import (
	"fmt"
)

// Backend is the backend used to run a parse.
type Backend int

const (
	// Default is the default backend. It uses the closure engine if the grammar's AST is available,
	// and bytecode otherwise.
	Default Backend = iota
	// Closure assembles parsing expressions into Go functions and runs them (the closure engine).
	Closure
	// Bytecode runs bytecode on the VM. Rule calls use host (Go) recursion (the recursive model).
	Bytecode
	// BytecodeIterative runs bytecode on the VM. Rule calls and Pratt expressions also run on the VM
	// stack, so deeply nested input does not consume the host stack (the iterative model).
	BytecodeIterative
)

func (b Backend) String() string {
	switch b {
	case Bytecode:
		return "bytecode"
	case BytecodeIterative:
		return "bytecode-iterative"
	case Closure:
		return "closure"
	}
	return "default"
}

// vmProgram is a module prepared for execution. Rule calls, memoization, left recursion, and
// end-of-rule processing use the runtime shared with the closure engine (runtime.go), while rule
// bodies, Pratt sections, actions, and predicates run as bytecode.
type vmProgram struct {
	m      *Module
	rules  []*rule
	byName map[string]*rule
	runes  [][]rune   // code points of each string in the string table
	bytes  [][]byte   // UTF-8 of each string in the string table
	scopes [][]string // names in the scope table
	fields [][]string // names in the field lists
	descs  []string   // expectation table: fixedDescs followed by the string table (string i has index numFixedDescs+i)
}

// newVMProgram prepares a module for execution. If iterative, it runs with the iterative model.
func newVMProgram(m *Module, iterative bool) *vmProgram {
	vm := &vmProgram{m: m, byName: map[string]*rule{}, descs: append(append([]string(nil), fixedDescs...), m.Strings...)}
	for _, s := range m.Strings {
		vm.runes = append(vm.runes, []rune(s))
		vm.bytes = append(vm.bytes, []byte(s))
	}
	for _, sc := range m.Scopes {
		vm.scopes = append(vm.scopes, vm.names(sc))
	}
	for _, fl := range m.FieldLists {
		vm.fields = append(vm.fields, vm.names(fl))
	}
	for i := range m.Rules {
		ri := &m.Rules[i]
		r := &rule{
			id: i, name: m.Strings[ri.Name], scope: vm.scope(ri.Scope),
			bodyIsSeq: ri.BodyIsSeq, memo: ri.Memo, leader: ri.Leader, positional: ri.Positional, novalue: ri.NoValue, transient: ri.Transient, vars: vm.names(ri.Vars),
		}
		if ri.TerminalType >= 0 {
			r.terminalType = m.Strings[ri.TerminalType]
		}
		if ri.Action >= 0 {
			r.act = vm.evaluator(ri.Action, false)
		}
		vm.rules = append(vm.rules, r)
		if _, twin := vm.byName[r.name]; !twin { // value-free twins follow the original rules
			vm.byName[r.name] = r
		}
	}
	for i := range m.Rules {
		ri, r := &m.Rules[i], vm.rules[i]
		if ri.Pratt >= 0 {
			r.pratt = vm.pratt(&m.Pratts[ri.Pratt])
		}
		entry := ri.Entry
		r.entry = entry
		r.stream = vm.streams(entry)
		if iterative {
			r.body = func(p *parser, min int) (*Node, bool) {
				res := p.iterate(vm, p.bodyFrame(entry, r, min, false))
				return res.v, res.ok
			}
		} else {
			r.body = func(p *parser, min int) (*Node, bool) {
				v, ok, _ := p.exec(vm, entry, r, min)
				return v, ok
			}
		}
	}
	return vm
}

// streams reports whether the rule body (from entry to RETURN) has a #stream repetition.
func (vm *vmProgram) streams(entry int) bool {
	for _, in := range vm.m.Code[entry:] {
		switch {
		case in.Op == OpReturn:
			return false
		case in.Op == OpNext && in.B == 2:
			return true
		}
	}
	return false
}

func (vm *vmProgram) names(ids []int) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = vm.m.Strings[id]
	}
	return out
}

func (vm *vmProgram) scope(ids []int) *scope {
	s := newScope()
	for _, n := range vm.names(ids) {
		s.slot(n)
	}
	return s
}

// block returns a matcher that runs the code of a Pratt section. It sets p.cut if a cut is passed
// within the section.
func (vm *vmProgram) block(entry int) matcher {
	return func(p *parser) (*Node, bool) {
		v, ok, cut := p.exec(vm, entry, nil, 0)
		if cut {
			p.cut = true
		}
		return v, ok
	}
}

func (vm *vmProgram) line(l *LineInfo, operator bool) *prattLine {
	pl := &prattLine{scope: vm.scope(l.Scope), m: vm.block(l.Entry), isSeq: l.IsSeq, entry: l.Entry}
	if l.Action >= 0 {
		pl.act = vm.evaluator(l.Action, operator)
	}
	return pl
}

func (vm *vmProgram) pratt(pi *PrattInfo) *pratt {
	pr := &pratt{skipEntry: pi.Skip}
	if pi.Skip >= 0 {
		pr.skip = vm.block(pi.Skip)
	}
	for i := range pi.Operands {
		pr.operands = append(pr.operands, vm.line(&pi.Operands[i], false))
	}
	op := func(o *OpInfo) *prattOp {
		po := &prattOp{id: o.ID, level: o.Level, line: vm.line(&o.Line, true)}
		po.kind = []string{"prefix", "postfix", "infix"}[o.Kind]
		if o.Kind == opInfix {
			po.assoc = []string{"left", "right", "none"}[o.Assoc]
		}
		return po
	}
	for i := range pi.Prefix {
		pr.prefix = append(pr.prefix, op(&pi.Prefix[i]))
	}
	for i := range pi.Led {
		pr.led = append(pr.led, op(&pi.Led[i]))
	}
	return pr
}

// --- Running matching code ---

// Entry kinds
const (
	eChoice = iota
	eIter
	eLook
	eNotLook
	eLabel
	eRecover
	eSkip
)

// vmEntry is an element of the entry stack (see the Failure section of docs/bytecode.md).
type vmEntry struct {
	kind uint8
	cut  bool
	ip   int32
	msg  int32 // message of eLabel
	save vmSave
	far  int     // eLabel, eRecover: saved farthest failure. eSkip: recovered failure
	base int     // eLabel, eRecover: saved start of the expectation record (expMark)
	exp  []expID // eSkip: expectations of the recovered failure
}

// vmSave is a saved state.
type vmSave struct {
	pos, vals, reps, trail, recovered int
	env                               *env
	frame                             *frame
}

type repState struct {
	min, max, count int
	scope           int
	base            int // bottom of the element values
}

func (p *parser) save() vmSave {
	return vmSave{pos: p.pos, vals: len(p.vals), reps: len(p.reps), trail: len(p.trail),
		recovered: len(p.recovered), env: p.env, frame: p.frame}
}

func (p *parser) restore(s *vmSave) {
	p.pos = s.pos
	p.vals = p.vals[:s.vals]
	p.reps = p.reps[:s.reps]
	for i := len(p.trail) - 1; i >= s.trail; i-- {
		u := p.trail[i]
		u.f.vals[u.slot] = u.old
	}
	p.trail = p.trail[:min2(s.trail, len(p.trail))]
	p.recovered = p.recovered[:s.recovered]
	p.env = s.env
	p.frame = s.frame
}

func (p *parser) push(v any) { p.vals = append(p.vals, v) }

func (p *parser) pop() any {
	v := p.vals[len(p.vals)-1]
	p.vals = p.vals[:len(p.vals)-1]
	return v
}

func asNodeValue(v any) *Node {
	if v == nil {
		return nil
	}
	return v.(*Node)
}

// exec runs matching code from ip until RETURN or END (the recursive model). On success it
// returns the pushed value. cut reports whether a cut belonging to no choice was passed (used
// in Pratt sections). Rule calls and Pratt expressions call the shared runtime (call,
// prattParse) through host recursion.
func (p *parser) exec(vm *vmProgram, ip int, r *rule, min int) (result *Node, ok bool, cut bool) {
	bv := p.newBody(ip, r, min)
	b := &bv
	ev, v, ok := p.step(vm, b, false, false, nil)
	for {
		switch ev {
		case evCall:
			in := &vm.m.Code[b.ip]
			v, ok = p.call(vm.rules[in.A], int(in.B))
		case evPratt:
			v, ok = p.prattParse(b.r, b.min)
		default:
			return v, ok, b.cut
		}
		ev, v, ok = p.step(vm, b, true, ok, v)
	}
}

// vmBody is the state of one run of code (a rule body or a Pratt section).
type vmBody struct {
	ip                  int
	vbase, ebase, rbase int
	r                   *rule
	min                 int
	cut                 bool // a cut belonging to no choice was passed
}

func (p *parser) newBody(ip int, r *rule, min int) vmBody {
	return vmBody{ip: ip, vbase: len(p.vals), ebase: len(p.ents), rbase: len(p.reps), r: r, min: min}
}

// Reasons step suspends
const (
	evDone  = iota // finished by RETURN, END, or failure
	evCall         // CALL (the instruction at b.ip); resumed with the result
	evPratt        // PRATT; resumed with the result
)

// step runs code until it finishes or reaches a rule call or a Pratt expression.
// If resume is set, it resumes with the result (ok, v) of the suspended instruction.
func (p *parser) step(vm *vmProgram, b *vmBody, resume, rok bool, rv *Node) (ev int, result *Node, ok bool) {
	m := vm.m
	code := m.Code
	ip, ebase := b.ip, b.ebase
	if resume {
		if rok {
			if in := &code[ip]; in.Op == OpPratt || in.C == 1 {
				p.push(rv)
			}
			ip++
		} else if ip, ok = p.unwind(m, ebase); !ok {
			p.vals = p.vals[:b.vbase]
			p.reps = p.reps[:b.rbase]
			return evDone, nil, false
		}
	}
	for {
		in := &code[ip]
		switch in.Op {
		case OpStr:
			start := p.pos
			if !p.matchLiteral(vm.runes[in.A], vm.bytes[in.A]) {
				p.expect(start, numFixedDescs+expID(in.B))
				goto fail
			}
			if in.C == 1 {
				p.push(p.newNode(Node{Type: TypeMatch, Start: start, End: p.pos, Text: m.Strings[in.A], terminal: true, fresh: true}))
			}
		case OpClass:
			ch, size, ok := p.peek()
			if !ok || !m.Classes[in.A].has(ch) {
				p.expect(p.pos, numFixedDescs+expID(in.B))
				goto fail
			}
			p.single(size, in.C == 1)
		case OpScan:
			count, max := 0, int(in.C)
			for max < 0 || count < max {
				ch, size, ok := p.peek()
				if !ok || in.A >= 0 && !m.Classes[in.A].has(ch) {
					if in.A >= 0 {
						p.expect(p.pos, numFixedDescs+expID(m.Classes[in.A].Desc))
					} else {
						p.expect(p.pos, idAny)
					}
					break
				}
				p.pos += size
				count++
			}
			if count < int(in.B) {
				goto fail
			}
		case OpAny:
			_, size, ok := p.peek()
			if !ok {
				p.expect(p.pos, idAny)
				goto fail
			}
			p.single(size, in.A == 1)
		case OpTop:
			if in.A == 1 {
				p.push(p.newNode(Node{Type: TypeMatch, Start: p.pos, End: p.pos, terminal: true, fresh: true}))
			}
		case OpFail:
			goto fail
		case OpAssert:
			var ok bool
			var desc expID
			switch in.A {
			case 0:
				ok, desc = p.pos == 0, idBeginInput
			case 1:
				ok, desc = p.atEOF(), idEndInput
			case 2:
				ok, desc = p.atLineStart(), idBeginLine
			default:
				ch, _, more := p.peek()
				ok, desc = !more || ch == '\n' || ch == '\r', idEndLine
			}
			if !ok {
				p.expect(p.pos, desc)
				goto fail
			}
		case OpJump:
			ip = int(in.A)
			continue
		case OpChoice:
			p.ents = append(p.ents, vmEntry{kind: eChoice, ip: in.A, save: p.save()})
		case OpCommit:
			p.ents = p.ents[:len(p.ents)-1]
			ip = int(in.A)
			continue
		case OpCut:
			marked := false
			for k := len(p.ents) - 1; k >= ebase; k-- {
				e := &p.ents[k]
				if e.kind == eChoice || e.kind == eIter {
					e.cut = true
					marked = true
					break
				}
				if e.kind == eLook || e.kind == eNotLook {
					marked = true // a cut inside a lookahead does not reach outside it
					break
				}
			}
			if !marked {
				b.cut = true
			}
		case OpPushPos:
			p.push(p.pos)
		case OpPushNil:
			p.push(nil)
		case OpSeq:
			n := int(in.A)
			kids := p.nodes(n)
			top := len(p.vals)
			for k := 0; k < n; k++ {
				kids[k] = asNodeValue(p.vals[top-n+k])
			}
			start := p.vals[top-n-1].(int)
			p.vals = p.vals[:top-n-1]
			p.push(p.newNode(Node{Type: TypeSeq, Start: start, End: p.pos, Children: kids, fresh: true}))
		case OpAtomic:
			start := p.pop().(int)
			if in.A == 1 {
				p.push(p.newNode(Node{Type: TypeMatch, Start: start, End: p.pos, Text: p.text(start, p.pos), terminal: true, fresh: true}))
			}
		case OpCapture:
			p.setCapture(int(in.A), asNodeValue(p.vals[len(p.vals)-1]))
			if in.B == 1 {
				p.vals = p.vals[:len(p.vals)-1]
			}
		case OpRepeat:
			p.reps = append(p.reps, repState{min: int(in.A), max: int(in.B), scope: int(in.C), base: len(p.vals)})
		case OpIter:
			p.ents = append(p.ents, vmEntry{kind: eIter, ip: in.A, save: p.save()})
			if sc := p.reps[len(p.reps)-1].scope; sc >= 0 {
				p.frame = p.newFrame(len(vm.scopes[sc]))
			}
		case OpNext:
			e := p.ents[len(p.ents)-1]
			p.ents = p.ents[:len(p.ents)-1]
			rep := &p.reps[len(p.reps)-1]
			if rep.scope >= 0 {
				if in.B != 0 { // in a value-free repetition, the element scope exists only for predicates
					top := len(p.vals) - 1
					p.vals[top] = p.attachCapturesNames(asNodeValue(p.vals[top]), vm.scopes[rep.scope], p.frame, e.save.pos, p.pos)
				}
				p.frame = e.save.frame
			}
			rep.count++
			if in.B == 2 && p.emit != nil && p.depth == 1 {
				// Elements of the start rule's #stream are passed on without being retained, and committed.
				v := asNodeValue(p.pop())
				if v != nil {
					v.fresh = false
				}
				if err := p.emit(v); err != nil {
					panic(fatal{err})
				}
				p.commit(p.pos)
				if p.pos == e.save.pos {
					ip = int(e.ip)
					continue
				}
				ip = int(in.A)
				continue
			}
			if p.pos == e.save.pos && rep.count >= rep.min || rep.max >= 0 && rep.count >= rep.max {
				ip = int(e.ip)
			} else {
				ip = int(in.A)
			}
			continue
		case OpEndRepeat:
			rep := p.reps[len(p.reps)-1]
			p.reps = p.reps[:len(p.reps)-1]
			if rep.count < rep.min {
				goto fail
			}
			if in.A == 1 {
				kids := p.nodes(len(p.vals) - rep.base)
				for k := range kids {
					kids[k] = asNodeValue(p.vals[rep.base+k])
				}
				start := p.vals[rep.base-1].(int)
				p.vals = p.vals[:rep.base-1]
				p.push(p.newNode(Node{Type: TypeList, Start: start, End: p.pos, Children: kids, fresh: true}))
			}
		case OpLook:
			kind := uint8(eLook)
			if in.B == 1 {
				kind = eNotLook
			}
			p.ents = append(p.ents, vmEntry{kind: kind, ip: in.A, save: p.save()})
			p.silent++
		case OpEndLook:
			e := p.ents[len(p.ents)-1]
			p.ents = p.ents[:len(p.ents)-1]
			p.silent--
			if in.A == 1 {
				p.restore(&e.save)
				goto fail
			}
			p.pos = e.save.pos
			p.vals = p.vals[:e.save.vals]
		case OpCall:
			b.ip = ip
			return evCall, nil, false
		case OpPratt:
			b.ip = ip
			return evPratt, nil, false
		case OpPred:
			if !vm.predicate(p, int(in.A)) {
				goto fail
			}
		case OpAssign:
			if !vm.assign(p, m.Strings[in.A], int(in.B)) {
				goto fail
			}
		case OpLabel:
			mk := p.isolate(p.pos)
			p.ents = append(p.ents, vmEntry{kind: eLabel, msg: in.A, far: mk.far, base: mk.base})
		case OpEndLabel:
			e := p.ents[len(p.ents)-1]
			p.ents = p.ents[:len(p.ents)-1]
			far, inner := p.unisolate(expMark{e.far, e.base})
			p.mergeExpected(far, inner)
		case OpRecover:
			mk := p.isolate(p.pos)
			p.ents = append(p.ents, vmEntry{kind: eRecover, ip: in.A, save: p.save(), far: mk.far, base: mk.base})
		case OpEndRecover:
			e := p.ents[len(p.ents)-1]
			p.ents = p.ents[:len(p.ents)-1]
			far, inner := p.unisolate(expMark{e.far, e.base})
			p.mergeExpected(far, inner)
			ip = int(in.A)
			continue
		case OpEndSkip:
			e := p.ents[len(p.ents)-1]
			p.ents = p.ents[:len(p.ents)-1]
			if p.pos == e.save.pos {
				p.restore(&e.save)
				p.mergeExpected(e.far, e.exp)
				goto fail
			}
			se := p.makeError(e.far, e.exp)
			p.recovered = append(p.recovered, se)
			if in.A == 1 {
				p.push(p.newNode(Node{Type: TypeError, Start: e.save.pos, End: p.pos, Text: p.text(e.save.pos, p.pos),
					Fields: Fields{{"message", se.Error()}}, fresh: true, terminal: true}))
			}
		case OpReturn, OpEnd:
			var v *Node
			if len(p.vals) > b.vbase { // skip code builds no values
				v = asNodeValue(p.vals[len(p.vals)-1])
			}
			p.vals = p.vals[:b.vbase]
			p.reps = p.reps[:b.rbase]
			return evDone, v, true
		default:
			panic(fmt.Sprintf("vm: invalid instruction %v at %d", in.Op, ip))
		}
		ip++
		continue

	fail:
		if ip, ok = p.unwind(m, ebase); !ok {
			p.vals = p.vals[:b.vbase]
			p.reps = p.reps[:b.rbase]
			return evDone, nil, false
		}
	}
}

// unwind unwinds the entry stack to ebase on failure and returns the index of the instruction to
// resume at. ok is false if there is no entry to resume from.
func (p *parser) unwind(m *Module, ebase int) (ip int, ok bool) {
	for {
		if len(p.ents) == ebase {
			return 0, false
		}
		e := p.ents[len(p.ents)-1]
		p.ents = p.ents[:len(p.ents)-1]
		switch e.kind {
		case eChoice, eIter:
			p.restore(&e.save)
			if e.cut {
				continue
			}
			return int(e.ip), true
		case eLook:
			p.silent--
			p.restore(&e.save)
			continue
		case eNotLook:
			p.silent--
			p.restore(&e.save)
			return int(e.ip), true
		case eLabel:
			far, _ := p.unisolate(expMark{e.far, e.base})
			p.expect(far, msgBit|(numFixedDescs+expID(e.msg)))
			continue
		case eRecover:
			far, inner := p.unisolate(expMark{e.far, e.base})
			exp := p.keep(inner) // keep them while skip runs
			p.restore(&e.save)
			p.ents = append(p.ents, vmEntry{kind: eSkip, save: e.save, far: far, exp: exp})
			return int(e.ip), true
		case eSkip:
			p.restore(&e.save)
			p.mergeExpected(e.far, e.exp)
		}
	}
}

// has reports whether the character ch is in the class.
func (cl *Class) has(ch rune) bool {
	for k := 0; k < len(cl.Ranges); k += 2 {
		if cl.Ranges[k] <= ch && ch <= cl.Ranges[k+1] {
			return !cl.Negated
		}
	}
	return cl.Negated
}

// single advances one character (size units) and pushes a Match if b is set.
func (p *parser) single(size int, b bool) {
	start := p.pos
	p.pos += size
	if b {
		p.push(p.newNode(Node{Type: TypeMatch, Start: start, End: p.pos, Text: p.text(start, p.pos), terminal: true, fresh: true}))
	}
}

// attachCapturesNames attaches captures by a list of names (same as attachCaptures).
func (p *parser) attachCapturesNames(v *Node, names []string, f *frame, start, end int) *Node {
	return p.attachCaptures(v, &scope{names: names}, f, start, end)
}

// --- Running expression code ---

// vmFunc is a bytecode lambda.
type vmFunc struct {
	vm     *vmProgram
	entry  int
	n      int
	locals []any
	ctx    *evalCtx
}

func (f *vmFunc) arity() int { return f.n }

func (f *vmFunc) apply(args ...any) (any, error) {
	// The locals are placed on the expression stack below the operands of the body.
	p := f.ctx.p
	base := len(p.estack)
	p.estack = append(append(p.estack, f.locals...), args...)
	v, err := f.vm.eval(f.ctx, f.entry, p.estack[base:len(p.estack):len(p.estack)])
	p.estack = p.estack[:base]
	return v, err
}

// evaluator returns an evaluator for an action in expression code. If operator is set, $lhs,
// $rhs, and $op are locals.
func (vm *vmProgram) evaluator(entry int, operator bool) evaluator {
	if !operator {
		return func(ctx *evalCtx) (any, error) { return vm.eval(ctx, entry, nil) }
	}
	return func(ctx *evalCtx) (any, error) {
		p := ctx.p
		base := len(p.estack)
		p.estack = append(p.estack, nil, nil, nil)
		locals := p.estack[base : base+3 : base+3]
		for l := ctx.locals; l != nil; l = l.next {
			switch l.name {
			case "lhs":
				locals[0] = l.val
			case "rhs":
				locals[1] = l.val
			case "op":
				locals[2] = l.val
			}
		}
		v, err := vm.eval(ctx, entry, locals)
		p.estack = p.estack[:base]
		return v, err
	}
}

func (vm *vmProgram) predicate(p *parser, entry int) bool {
	ctx := p.useCtx(evalCtx{p: p, frame: p.frame, start: p.pos, end: p.pos, cbase: len(p.created)})
	v, err := vm.eval(ctx, entry, nil)
	p.created = p.created[:ctx.cbase] // discard nodes created by the predicate
	if err != nil {
		return false
	}
	if b, ok := v.(bool); ok && !b {
		return false
	}
	return true
}

func (vm *vmProgram) assign(p *parser, name string, entry int) bool {
	ctx := p.useCtx(evalCtx{p: p, frame: p.frame, start: p.pos, end: p.pos, cbase: len(p.created)})
	v, err := vm.eval(ctx, entry, nil)
	p.created = p.created[:ctx.cbase] // discard nodes created by the predicate
	if err != nil {
		return false
	}
	if _, isFn := v.(function); isFn {
		return false
	}
	p.env = &env{name: name, val: v, next: p.env}
	return true
}

// eval evaluates expression code from ip until ERET. The operands are kept on the parser's
// expression stack (p.estack) above its current top, so evaluations nested through lambdas
// (vmFunc.apply) share one area; the stack is back at its original height when eval returns.
func (vm *vmProgram) eval(ctx *evalCtx, ip int, locals []any) (v any, err error) {
	m := vm.m
	p := ctx.p
	stack := p.estack
	base := len(stack)
	pop := func() any {
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		return v
	}
	defer func() { p.estack = stack[:base] }()
	for {
		in := &m.Exprs[ip]
		switch in.Op {
		case EInt:
			stack = append(stack, int(in.A))
		case EStr:
			stack = append(stack, m.Strings[in.A])
		case EBool:
			stack = append(stack, in.A == 1)
		case ENil:
			stack = append(stack, nil)
		case ECap:
			stack = append(stack, nodeOrNil(ctx.frame.vals[in.A]))
		case EItem:
			n := int(in.A)
			if n == 0 {
				stack = append(stack, ctx.newList(ctx.items))
			} else if n > len(ctx.items) {
				return nil, fmt.Errorf("$%d is out of range (%d elements)", n, len(ctx.items))
			} else {
				stack = append(stack, nodeOrNil(ctx.items[n-1]))
			}
		case ELocal:
			stack = append(stack, locals[in.A])
		case EVar:
			v, ok := ctx.p.env.lookup(m.Strings[in.A])
			if !ok {
				return nil, fmt.Errorf("variable %s is not defined", m.Strings[in.A])
			}
			stack = append(stack, v)
		case EMember:
			v, err := ctx.member(pop(), m.Strings[in.A])
			if err != nil {
				return nil, err
			}
			stack = append(stack, v)
		case ENew:
			names := vm.fields[in.B]
			top := len(stack) - len(names)
			v, err := ctx.newStruct(m.Strings[in.A], names, stack[top:]) // newStruct does not retain vals
			if err != nil {
				return nil, err
			}
			stack = append(stack[:top], v)
		case EBin:
			r := pop()
			l := pop()
			v, err := binaryOp(m.Strings[in.A], l, r)
			if err != nil {
				return nil, err
			}
			stack = append(stack, v)
		case EUnary:
			v, err := unaryOp(m.Strings[in.A], pop())
			if err != nil {
				return nil, err
			}
			stack = append(stack, v)
		case EAnd, EOr:
			op := "&&"
			if in.Op == EOr {
				op = "||"
			}
			top := stack[len(stack)-1]
			b, ok := top.(bool)
			if !ok {
				return nil, fmt.Errorf("invalid operand %s for %s", typeName(top), op)
			}
			if b == (in.Op == EOr) {
				ip = int(in.A)
				continue
			}
			stack = stack[:len(stack)-1]
		case EBoolChk:
			top := stack[len(stack)-1]
			if _, ok := top.(bool); !ok {
				return nil, fmt.Errorf("invalid operand %s for %s", typeName(top), m.Strings[in.A])
			}
		case EFunc:
			if len(locals) > 0 {
				// The locals may be on the expression stack, and a function can outlive the lambda that
				// created it (when the lambda returns it).
				locals = append([]any(nil), locals...)
			}
			stack = append(stack, &vmFunc{vm: vm, entry: int(in.A), n: int(in.B), locals: locals, ctx: ctx})
		case ECall:
			// The arguments stay on the stack during the call: lambdas called by the built-in push
			// above them, and built-ins do not retain args.
			top := len(stack) - int(in.B)
			p.estack = stack
			v, err := ctx.builtin(builtinNames[in.A], stack[top:])
			stack = p.estack
			if err != nil {
				return nil, err
			}
			stack = append(stack[:top], v)
		case ERet:
			return stack[len(stack)-1], nil
		default:
			return nil, fmt.Errorf("vm: invalid expression instruction %v at %d", in.Op, ip)
		}
		ip++
	}
}
