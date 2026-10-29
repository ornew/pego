package engine

import (
	"fmt"

	"github.com/ornew/pego/internal/bytecode"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/value"
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
	// BytecodeIterative runs bytecode on the VM. Rule calls and Operators Rule expressions also run on the VM
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
// bodies, Operators Rule sections, actions, and predicates run as bytecode.
type vmProgram struct {
	m        *bytecode.Module
	rules    []*rule
	byName   map[string]*rule
	nseen    int        // number of rules with rule.seen set
	runes    [][]rune   // code points of each string in the string table
	bytes    [][]byte   // UTF-8 of the decoded literal strings
	literals []string   // matching text; aliases m.Strings unless normalization is needed
	scopes   [][]string // names in the scope table
	fields   [][]string // names in the field lists
	ascii    []asciiSet // the ASCII part of each class, as a bitmap
	descs    []string   // expectation table: fixedDescs followed by the string table (string i has index numFixedDescs+i)
	// runSites holds, at the index of each REPEAT that a Document can resume, how to resume it.
	runSites []*vmRunSite
	// fieldTypes holds the type of each ECAST, by the string index of its text
	// (nil when the module has none); typeNames are the module's type names it resolves.
	fieldTypes []*compile.FieldType
	typeNames  map[string]bool
	// nodeKinds holds the kinds of the module's named node types by the string index of their
	// names (nil when the module has none).
	nodeKinds []*value.NodeTypeKinds
	// vars is the module's variable table (nil when it declares none).
	vars *compile.VarTable
}

// vmRunSite is a repetition that a Document can resume (resume.go): the REPEAT at ip, whose ITER
// follows it, with NEXT just before the ENDREPEAT at end.
type vmRunSite struct {
	end       int  // the ENDREPEAT
	keep      bool // the elements push values
	shiftable bool // the elements' values contain no position values
}

// runSite returns how to resume the repetition whose REPEAT is at ip, or nil if it cannot be
// resumed (as compiler.resumable): its element evaluates predicates or calls rules that read
// variables.
func (vm *vmProgram) runSite(ip int) *vmRunSite {
	code := vm.m.Code
	if ip+1 >= len(code) || code[ip+1].Op != bytecode.OpIter {
		return nil
	}
	end := int(code[ip+1].A)
	if end <= ip+2 || end >= len(code) || code[end].Op != bytecode.OpEndRepeat || code[end-1].Op != bytecode.OpNext {
		return nil
	}
	site := &vmRunSite{end: end, keep: code[end-1].B != 0, shiftable: true}
	for _, in := range code[ip+2 : end-1] {
		switch in.Op {
		case bytecode.OpPred, bytecode.OpBind:
			return nil
		case bytecode.OpCapture:
			if code[ip].C < 0 { // the element writes captures to the enclosing scope (in &)
				return nil
			}
		case bytecode.OpCall:
			if in.A < 0 || int(in.A) >= len(vm.rules) {
				return nil
			}
			r := vm.rules[in.A]
			if len(r.Vars) > 0 {
				return nil
			}
			if r.Positional {
				site.shiftable = false
			}
		}
	}
	return site
}

// newVMProgram prepares a module for execution. If iterative, it runs with the iterative model.
func newVMProgram(m *bytecode.Module, iterative bool) *vmProgram {
	vm := &vmProgram{m: m, byName: map[string]*rule{}, descs: append(append([]string(nil), compile.FixedDescs...), m.Strings...)}
	vm.literals = m.Strings
	normalized := false
	for i, s := range m.Strings {
		val := value.LiteralValue(s)
		if val != s {
			if !normalized {
				vm.literals = append([]string(nil), m.Strings...)
				normalized = true
			}
			vm.literals[i] = val
		}
		vm.runes = append(vm.runes, []rune(val))
		vm.bytes = append(vm.bytes, []byte(val))
	}
	for _, sc := range m.Scopes {
		vm.scopes = append(vm.scopes, vm.names(sc))
	}
	for i := range m.Classes {
		vm.ascii = append(vm.ascii, newASCIISet(&m.Classes[i]))
	}
	for _, fl := range m.FieldLists {
		vm.fields = append(vm.fields, vm.names(fl))
	}
	for _, in := range m.Exprs {
		switch {
		case in.Op == bytecode.ECast:
			vm.checkType(int(in.A))
		}
	}
	for _, t := range m.Types {
		if t.Node >= 0 {
			if vm.nodeKinds == nil {
				vm.nodeKinds = make([]*value.NodeTypeKinds, len(m.Strings))
			}
			vm.nodeKinds[t.Name] = value.NewNodeTypeKinds(m.Strings[t.Name])
		}
	}
	vtable := moduleVarTable(m)
	vm.vars = vtable
	for i := range m.Rules {
		ri := &m.Rules[i]
		r := &rule{
			ID: i, Name: m.Strings[ri.Name], Scope: vm.scope(ri.Scope),
			Memo: ri.Memo, Leader: ri.Leader, Positional: ri.Positional, Novalue: ri.NoValue, Transient: ri.Transient, Splice: ri.Splice, Vars: vm.names(ri.Vars),
		}
		if ri.Where >= 0 {
			r.Where = m.Strings[ri.Where]
		}
		r.initVals = vtable.ValuesOf(r.Vars)
		if ri.NodeType >= 0 {
			if ri.OperatorsRule >= 0 {
				r.OpNodeType, r.opKinds = m.Strings[ri.NodeType], vm.nodeKinds[ri.NodeType]
			} else {
				r.NodeType, r.nodeKinds = m.Strings[ri.NodeType], vm.nodeKinds[ri.NodeType]
			}
		}
		r.kinds = value.NewRuleKinds(r.Name)
		r.value = func(_ *parser, v *value.Node, _ int) *value.Node { return v } // the code pushes the value of the body's scope
		if ri.Action >= 0 {
			r.act = vm.evaluator(ri.Action, false)
		}
		vm.rules = append(vm.rules, r)
		if _, twin := vm.byName[r.Name]; !twin { // value-free twins follow the original rules
			vm.byName[r.Name] = r
		}
	}
	statics := make([]*compile.Rule, len(vm.rules))
	for i, r := range vm.rules {
		statics[i] = &r.staticRule
	}
	vm.nseen = compile.NumberSeen(statics)
	vm.runSites = make([]*vmRunSite, len(m.Code))
	for ip, in := range m.Code {
		if in.Op == bytecode.OpRepeat || in.Op == bytecode.OpRepeatWide {
			vm.runSites[ip] = vm.runSite(ip)
		}
	}
	for _, r := range vm.rules {
		r.Plain = !r.Leader && (!r.Memo || r.Transient) && len(r.Scope.Names) == 0
	}
	for i := range m.Rules {
		ri, r := &m.Rules[i], vm.rules[i]
		if ri.OperatorsRule >= 0 {
			r.operatorsRule = vm.operatorsRule(&m.OperatorsRules[ri.OperatorsRule])
		}
		entry := ri.Entry
		r.entry = entry
		r.Stream = vm.streams(entry)
		if iterative {
			r.body = func(p *parser, min int) (*value.Node, bool) {
				res := p.iterate(vm, p.bodyFrame(entry, r, min, false))
				return res.v, res.ok
			}
		} else {
			r.body = func(p *parser, min int) (*value.Node, bool) {
				v, ok, _ := p.exec(vm, entry, r, min)
				return v, ok
			}
		}
	}
	return vm
}

// streams reports whether the rule body (from entry to RETURN) has a #stream repetition.
func (vm *vmProgram) streams(entry int) bool {
	return moduleStreams(vm.m, entry)
}

func (vm *vmProgram) names(ids []int) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = vm.m.Strings[id]
	}
	return out
}

func (vm *vmProgram) scope(ids []int) *compile.Scope {
	s := compile.NewScope()
	for _, n := range vm.names(ids) {
		s.Slot(n)
	}
	return s
}

// block returns a matcher that runs the code of an Operators Rule section. It sets p.cut if a cut is passed
// within the section.
func (vm *vmProgram) block(entry int) matcher {
	return func(p *parser) (*value.Node, bool) {
		v, ok, cut := p.exec(vm, entry, nil, 0)
		if cut {
			p.cut = true
		}
		return v, ok
	}
}

func (vm *vmProgram) line(l *bytecode.LineInfo, operator bool) *operatorsRuleLine {
	// The code pushes the line's value already: the value of an operand line's scope, or the
	// operator's value, which is the node of its tuple if IsSeq (its captures are attached to it).
	pl := &operatorsRuleLine{scope: vm.scope(l.Scope), m: vm.block(l.Entry), entry: l.Entry, single: compile.SinglePass,
		value: func(_ *parser, v *value.Node, _ int) *value.Node { return v }}
	if operator && l.IsSeq {
		pl.single, pl.shape = compile.SingleTuple, compile.Shaper{Elems: true}
	}
	if l.Action >= 0 {
		pl.act = vm.evaluator(l.Action, operator)
	}
	return pl
}

// checkType returns the check type whose text is string i of the module.
func (vm *vmProgram) checkType(i int) *compile.FieldType {
	if vm.fieldTypes == nil {
		vm.fieldTypes, vm.typeNames = make([]*compile.FieldType, len(vm.m.Strings)), compile.ModuleTypeNames(vm.m)
	}
	if vm.fieldTypes[i] == nil {
		// Loading validated the text. A module built in memory with an invalid one gets a type
		// that no value fits.
		t, err := compile.ParseFieldType(vm.m.Strings[i], vm.typeNames)
		if err != nil {
			t = &compile.FieldType{Kind: compile.FtJoin, Text: vm.m.Strings[i]}
		}
		vm.fieldTypes[i] = t
	}
	return vm.fieldTypes[i]
}

// cast runs ECAST at ip on the value v, and returns v if it fits.
//
//go:noinline
func (vm *vmProgram) cast(p *parser, v any, pos, ip int) (any, error) {
	in := &vm.m.Exprs[ip]
	if t := vm.fieldTypes[in.A]; t == nil || !p.fieldConforms(t, v) {
		if t == nil {
			t = &compile.FieldType{Text: vm.m.Strings[in.A]}
		}
		return nil, p.castError(v, t, pos, int(in.B), int(in.C))
	}
	return v, nil
}

// convert runs ENODE at ip on the value v, and returns it converted.
//
//go:noinline
func (vm *vmProgram) convert(p *parser, v any, ip int) (any, error) {
	in := &vm.m.Exprs[ip]
	name := vm.m.Strings[in.A]
	var ks *value.NodeTypeKinds
	if vm.nodeKinds != nil {
		ks = vm.nodeKinds[in.A]
	}
	if ks == nil {
		return nil, p.convertError(v, name)
	}
	n, ok := ks.Convert(v)
	if !ok {
		return nil, p.convertError(v, name)
	}
	return n, nil
}

func (vm *vmProgram) operatorsRule(pi *bytecode.OperatorsRuleInfo) *operatorsRule {
	pr := &operatorsRule{skipEntry: pi.Skip}
	if pi.Skip >= 0 {
		pr.skip = vm.block(pi.Skip)
	}
	for i := range pi.Operands {
		pr.operands = append(pr.operands, vm.line(&pi.Operands[i], false))
	}
	op := func(o *bytecode.OpInfo) *operatorsRuleOp {
		po := &operatorsRuleOp{id: o.ID, level: o.Level, line: vm.line(&o.Line, true)}
		po.kind = []string{"prefix", "postfix", "infix"}[o.Kind]
		if o.Kind == bytecode.OperatorInfix {
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
	eRunIter // an iteration of a repetition whose run is recorded (repState.run)
	eLook
	eNotLook
	eLabel
	eRecover
	eSkip
)

// vmEntry is an element of the entry stack (see the Failure section of spec/bytecode/README.md). It is
// kept small, since a choice or an iteration pushes one: the state of the rarer label, recovery
// and skip entries is on a separate stack (parser.labs).
type vmEntry struct {
	kind uint8
	cut  bool
	ip   int32
	lab  int32 // eLabel, eRecover, eSkip: index of the entry's state in parser.labs
	save vmSave
}

// labState is the state of a label, recovery or skip entry.
type labState struct {
	msg  int32   // eLabel: message
	mode int32   // eLabel: 0 for @error, 1 for @diagnostic
	far  int     // eLabel, eRecover: saved farthest failure. eSkip: recovered failure
	base int     // eLabel, eRecover: saved start of the expectation record (expMark)
	bits uint64  // eLabel, eRecover: saved membership filter
	exp  []expID // eSkip: expectations of the recovered failure
}

// pushLab pushes an entry of kind with the state l.
func (p *parser) pushLab(kind uint8, ip int32, save vmSave, l labState) {
	p.labs = append(p.labs, l)
	p.ents = append(p.ents, vmEntry{kind: kind, ip: ip, lab: int32(len(p.labs) - 1), save: save})
}

// popLab pops the state of the entry e, which has just been popped.
func (p *parser) popLab(e *vmEntry) labState {
	l := p.labs[e.lab]
	p.labs = p.labs[:e.lab]
	return l
}

// vmSave is a saved state. Stack heights are int32 to keep entries small.
type vmSave struct {
	pos                          int
	vals, reps, trail, recovered int32
	env                          *env
	frame                        *frame
}

type repState struct {
	min, max, count int
	scope           int
	base            int       // bottom of the element values
	run             *runState // the run being recorded in a Document parse, or nil
}

func (p *parser) save() vmSave {
	return vmSave{pos: p.pos, vals: int32(len(p.vals)), reps: int32(len(p.reps)), trail: int32(len(p.trail)),
		recovered: int32(len(p.recovered)), env: p.env, frame: p.frame}
}

func (p *parser) restore(s *vmSave) {
	p.pos = s.pos
	p.vals = p.vals[:s.vals]
	p.reps = p.reps[:s.reps]
	p.undoCaptures(int(s.trail))
	p.trail = p.trail[:min2(int(s.trail), len(p.trail))]
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

func asNodeValue(v any) *value.Node {
	if v == nil {
		return nil
	}
	return v.(*value.Node)
}

// exec runs matching code from ip until RETURN or END (the recursive model). On success it
// returns the pushed value. cut reports whether a cut belonging to no choice was passed (used
// in Operators Rule sections). Rule calls and Operators Rule expressions call the shared runtime (call,
// operatorsRuleParse) through host recursion.
func (p *parser) exec(vm *vmProgram, ip int, r *rule, min int) (result *value.Node, ok bool, cut bool) {
	bv := p.newBody(ip, r, min)
	b := &bv
	ev, v, ok := p.step(vm, b, false, false, nil)
	for {
		switch ev {
		case evCall:
			in := &vm.m.Code[b.ip]
			if r := vm.rules[in.A]; r.Plain && !p.noPlain {
				v, ok = p.invokePlain(r, int(in.B)) // what call does for such a rule
			} else {
				v, ok = p.call(r, int(in.B))
			}
		case evOperatorsRule:
			v, ok = p.operatorsRuleParse(b.r, b.min)
		default:
			return v, ok, b.cut
		}
		ev, v, ok = p.step(vm, b, true, ok, v)
	}
}

// vmBody is the state of one run of code (a rule body or an Operators Rule section).
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
	evDone          = iota // finished by RETURN, END, or failure
	evCall                 // CALL (the instruction at b.ip); resumed with the result
	evOperatorsRule        // OPERATORS_RULE; resumed with the result
)

// step runs code until it finishes or reaches a rule call or an Operators Rule expression.
// If resume is set, it resumes with the result (ok, v) of the suspended instruction.
func (p *parser) step(vm *vmProgram, b *vmBody, resume, rok bool, rv *value.Node) (ev int, result *value.Node, ok bool) {
	m := vm.m
	code := m.Code
	ip, ebase := b.ip, b.ebase
	if resume {
		if rok {
			if in := &code[ip]; in.Op == bytecode.OpOperatorsRule || in.C == 1 {
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
		p.stepWork()
		in := &code[ip]
		switch in.Op {
		case bytecode.OpStr:
			start := p.pos
			if !p.matchLiteral(vm.runes[in.A], vm.bytes[in.A]) {
				p.expect(start, compile.NumFixedDescs+expID(in.B))
				goto fail
			}
			if in.C == 1 {
				p.push(p.newNode(value.Data{Kind: value.MatchKind, Start: int32(start), End: int32(p.pos), Text: p.literalText(start, vm.literals[in.A]), Terminal: true, Fresh: true}))
			}
		case bytecode.OpStrFold:
			start := p.pos
			if !p.matchLiteralFold(vm.runes[in.A]) {
				p.expect(start, compile.NumFixedDescs+expID(in.B))
				goto fail
			}
			if in.C == 1 {
				p.push(p.newNode(value.Data{Kind: value.MatchKind, Start: int32(start), End: int32(p.pos), Text: p.text(start, p.pos), Terminal: true, Fresh: true}))
			}
		case bytecode.OpClass:
			ch, size, ok := p.peek()
			if !ok || !vm.has(p, in.A, ch) {
				p.expect(p.pos, compile.NumFixedDescs+expID(in.B))
				goto fail
			}
			p.single(size, in.C == 1)
		case bytecode.OpGuard:
			ch, _, ok := p.peek()
			if !(ok && vm.has(p, in.A, ch)) && p.depth+int(in.B) <= p.maxDepth {
				p.expect(p.pos, compile.NumFixedDescs+expID(m.Classes[in.A].Desc))
				ip = int(in.C)
				continue
			}
		case bytecode.OpScan, bytecode.OpScanWide:
			min, max := int(in.B), int(in.C)
			if in.Op == bytecode.OpScanWide {
				min, max = int(m.Exprs[in.B].Integer()), int(m.Exprs[in.C].Integer())
			}
			count := 0
			for max < 0 || count < max {
				ch, size, ok := p.peek()
				if !ok || in.A >= 0 && !vm.has(p, in.A, ch) {
					if in.A >= 0 {
						p.expect(p.pos, compile.NumFixedDescs+expID(m.Classes[in.A].Desc))
					} else {
						p.expect(p.pos, compile.IDAny)
					}
					break
				}
				p.pos += size
				count++
			}
			if count < min {
				goto fail
			}
		case bytecode.OpAny:
			_, size, ok := p.peek()
			if !ok {
				p.expect(p.pos, compile.IDAny)
				goto fail
			}
			p.single(size, in.A == 1)
		case bytecode.OpTop:
			if in.A == 1 {
				p.push(p.newNode(value.Data{Kind: value.MatchKind, Start: int32(p.pos), End: int32(p.pos), Terminal: true, Fresh: true}))
			}
		case bytecode.OpFail:
			goto fail
		case bytecode.OpAssert:
			var ok bool
			var desc expID
			switch in.A {
			case 0:
				ok, desc = p.atInputStart(), compile.IDBeginInput
			case 1:
				ok, desc = p.atEOF(), compile.IDEndInput
			case 2:
				ok, desc = p.atLineStart(), compile.IDBeginLine
			default:
				ch, _, more := p.peek()
				ok, desc = !more || ch == '\n' || ch == '\r', compile.IDEndLine
			}
			if !ok {
				p.expect(p.pos, desc)
				goto fail
			}
		case bytecode.OpJump:
			ip = int(in.A)
			continue
		case bytecode.OpChoice:
			p.ents = append(p.ents, vmEntry{kind: eChoice, ip: in.A, save: p.save()})
		case bytecode.OpCommit:
			p.ents = p.ents[:len(p.ents)-1]
			ip = int(in.A)
			continue
		case bytecode.OpCut:
			marked := false
			for k := len(p.ents) - 1; k >= ebase; k-- {
				p.stepWork()
				e := &p.ents[k]
				if e.kind == eChoice || e.kind == eIter || e.kind == eRunIter {
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
		case bytecode.OpPushPos:
			p.push(p.newPos(p.pos))
		case bytecode.OpPushNil:
			p.push(nil)
		case bytecode.OpSeq:
			n := int(in.A)
			kids := p.nodes(n)
			top := len(p.vals)
			for k := 0; k < n; k++ {
				p.stepWork()
				kids[k] = asNodeValue(p.vals[top-n+k])
			}
			start := *p.vals[top-n-1].(*int)
			p.vals = p.vals[:top-n-1]
			p.push(p.newNode(value.Data{Kind: value.SeqKind, Start: int32(start), End: int32(p.pos), Children: kids, Fresh: true}))
		case bytecode.OpSpread:
			v := p.pop()
			switch {
			case in.B == 1:
				if n := asNodeValue(v); n != nil {
					for _, k := range value.D(n).Children {
						p.stepWork()
						p.push(k)
					}
				}
			case in.A == 1:
				p.push(v)
			}
		case bytecode.OpAtomic:
			start := *p.pop().(*int)
			if in.A == 1 {
				p.push(p.newNode(value.Data{Kind: value.MatchKind, Start: int32(start), End: int32(p.pos), Text: p.text(start, p.pos), Terminal: true, Fresh: true}))
			}
		case bytecode.OpCapture:
			p.setCapture(int(in.A), asNodeValue(p.vals[len(p.vals)-1]))
			if in.B == 1 {
				p.vals = p.vals[:len(p.vals)-1]
			}
		case bytecode.OpRepeat, bytecode.OpRepeatWide:
			if streamPrefixIsolation && p.emit != nil && p.depth == 1 && vm.isStreamRepeat(ip) {
				p.startStreamChunks()
			}
			min, max := int(in.A), int(in.B)
			if in.Op == bytecode.OpRepeatWide {
				min, max = int(m.Exprs[in.A].Integer()), int(m.Exprs[in.B].Integer())
			}
			p.reps = append(p.reps, repState{min: min, max: max, scope: int(in.C), base: len(p.vals)})
			if p.runs != nil && p.silent == 0 {
				if site := vm.runSites[ip]; site != nil {
					// A Document parse: resume the run of the previous parse (resume.go).
					r := p.newRunState()
					p.startRun(r, ip, min, max, site.shiftable)
					rep := &p.reps[len(p.reps)-1]
					rep.run = r
					for el := p.nextPrefix(r); el != nil; el = p.nextPrefix(r) {
						if site.keep {
							p.push(el.v)
						}
					}
					rep.count = r.count
					if !r.more() {
						ip = site.end
						continue
					}
				}
			}
		case bytecode.OpIter:
			rep := &p.reps[len(p.reps)-1]
			if rep.max >= 0 && rep.count >= rep.max {
				ip = int(in.A)
				continue
			}
			kind := uint8(eIter)
			if rep.run != nil {
				r := rep.run
				if p.resync(r) {
					for el := p.nextTail(r); el != nil; el = p.nextTail(r) {
						if m.Code[in.A-1].B != 0 { // NEXT keeps values
							p.push(el.v)
						}
					}
					rep.count = r.count
					if !r.more() {
						ip = int(in.A)
						continue
					}
				}
				kind = eRunIter
				p.beginElem(r)
			}
			p.ents = append(p.ents, vmEntry{kind: kind, ip: in.A, save: p.save()})
			if sc := p.reps[len(p.reps)-1].scope; sc >= 0 {
				p.frame = p.newFrame(len(vm.scopes[sc]))
			}
		case bytecode.OpNext:
			e := p.ents[len(p.ents)-1]
			p.ents = p.ents[:len(p.ents)-1]
			rep := &p.reps[len(p.reps)-1]
			if rep.scope >= 0 {
				if in.B == 3 { // a projected repetition: the value is the field's capture
					var v *value.Node
					if int(in.C) < len(p.frame.vals) { // (a corrupted file may name another slot)
						v = p.frame.vals[in.C]
					}
					p.push(v)
				} else if in.B != 0 { // in a value-free repetition, the element scope exists only for predicates
					top := len(p.vals) - 1
					p.vals[top] = p.attachCapturesNames(asNodeValue(p.vals[top]), vm.scopes[rep.scope], p.frame, e.save.pos, p.pos)
				}
				p.frame = e.save.frame
			}
			rep.count++
			if rep.run != nil {
				var v *value.Node
				if in.B != 0 {
					v = asNodeValue(p.vals[len(p.vals)-1])
				}
				p.endElem(rep.run, true, v)
			}
			if in.B == 2 && p.emit != nil && p.depth == 1 {
				// Elements of the start rule's #stream are passed on without being retained, and committed.
				v := asNodeValue(p.pop())
				if v != nil {
					value.D(v).Fresh = false
				}
				if err := p.emitStream(v); err != nil {
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
			if p.pos == e.save.pos || rep.max >= 0 && rep.count >= rep.max {
				ip = int(e.ip)
			} else {
				ip = int(in.A)
			}
			continue
		case bytecode.OpEndRepeat:
			rep := p.reps[len(p.reps)-1]
			p.reps = p.reps[:len(p.reps)-1]
			if rep.count < rep.min {
				goto fail
			}
			if rep.run != nil {
				p.finishRun(rep.run)
				p.freeRunState(rep.run)
			}
			if in.A == 1 {
				kids := p.nodes(len(p.vals) - rep.base)
				for k := range kids {
					p.stepWork()
					kids[k] = asNodeValue(p.vals[rep.base+k])
				}
				start := *p.vals[rep.base-1].(*int)
				p.vals = p.vals[:rep.base-1]
				p.push(p.newNode(value.Data{Kind: value.ListKind, Start: int32(start), End: int32(p.pos), Children: kids, Fresh: true}))
			}
		case bytecode.OpLook:
			kind := uint8(eLook)
			if in.B == 1 {
				kind = eNotLook
			}
			p.ents = append(p.ents, vmEntry{kind: kind, ip: in.A, save: p.save()})
			p.silent++
		case bytecode.OpEndLook:
			e := p.ents[len(p.ents)-1]
			p.ents = p.ents[:len(p.ents)-1]
			p.silent--
			if in.A == 1 {
				p.restore(&e.save)
				goto fail
			}
			p.pos = e.save.pos
			p.vals = p.vals[:int(e.save.vals)]
		case bytecode.OpCall:
			b.ip = ip
			return evCall, nil, false
		case bytecode.OpOperatorsRule:
			b.ip = ip
			return evOperatorsRule, nil, false
		case bytecode.OpPred:
			if !vm.predicate(p, int(in.A)) {
				goto fail
			}
		case bytecode.OpBind:
			if !vm.bind(p, m.Strings[in.A], int(in.B)) {
				goto fail
			}
		case bytecode.OpLabel:
			mk := p.isolate(p.pos)
			p.pushLab(eLabel, 0, vmSave{}, labState{msg: in.A, mode: in.B, far: mk.far, base: mk.base, bits: mk.bits})
		case bytecode.OpEndLabel:
			e := p.ents[len(p.ents)-1]
			p.ents = p.ents[:len(p.ents)-1]
			l := p.popLab(&e)
			far, inner := p.unisolate(expMark{l.far, l.base, l.bits})
			p.mergeExpected(far, inner)
		case bytecode.OpRecover:
			mk := p.isolate(p.pos)
			p.pushLab(eRecover, in.A, p.save(), labState{far: mk.far, base: mk.base, bits: mk.bits})
		case bytecode.OpEndRecover:
			e := p.ents[len(p.ents)-1]
			p.ents = p.ents[:len(p.ents)-1]
			l := p.popLab(&e)
			far, inner := p.unisolate(expMark{l.far, l.base, l.bits})
			p.mergeExpected(far, inner)
			ip = int(in.A)
			continue
		case bytecode.OpEndSkip:
			e := p.ents[len(p.ents)-1]
			p.ents = p.ents[:len(p.ents)-1]
			l := p.popLab(&e)
			if p.pos == e.save.pos {
				p.restore(&e.save)
				p.mergeExpected(l.far, l.exp)
				goto fail
			}
			se := p.makeError(l.far, l.exp)
			p.recovered = append(p.recovered, se)
			if in.A == 1 {
				p.push(p.newNode(value.Data{Kind: value.ErrorKind, Start: int32(e.save.pos), End: int32(p.pos), Text: p.text(e.save.pos, p.pos), Fields: value.Fields{{Name: "message", Value: p.recoveryErrorText(se)}}, Fresh: true, Terminal: true}))
			}
		case bytecode.OpReturn, bytecode.OpEnd:
			var v *value.Node
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
func (p *parser) unwind(m *bytecode.Module, ebase int) (ip int, ok bool) {
	for {
		if len(p.ents) == ebase {
			return 0, false
		}
		p.stepWork()
		e := p.ents[len(p.ents)-1]
		p.ents = p.ents[:len(p.ents)-1]
		switch e.kind {
		case eRunIter:
			p.endElem(p.reps[e.save.reps-1].run, false, nil)
			p.restore(&e.save)
			if e.cut {
				continue
			}
			return int(e.ip), true
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
			l := p.popLab(&e)
			far, inner := p.unisolate(expMark{l.far, l.base, l.bits})
			if l.mode == 0 {
				p.expect(far, msgBit|(compile.NumFixedDescs+expID(l.msg)))
			} else {
				p.mergeExpected(far, renamedExpected(p, inner, compile.NumFixedDescs+expID(l.msg)))
			}
			continue
		case eRecover:
			l := p.popLab(&e)
			far, inner := p.unisolate(expMark{l.far, l.base, l.bits})
			exp := p.keep(inner) // keep them while skip runs
			p.restore(&e.save)
			p.pushLab(eSkip, 0, e.save, labState{far: far, exp: exp})
			return int(e.ip), true
		case eSkip:
			l := p.popLab(&e)
			p.restore(&e.save)
			p.mergeExpected(l.far, l.exp)
		}
	}
}

// asciiSet is the membership of the characters below 128 in a class.
type asciiSet [2]uint64

func newASCIISet(cl *bytecode.Class) asciiSet {
	var s asciiSet
	for ch := rune(0); ch < 128; ch++ {
		if cl.Has(ch) {
			s[ch>>6] |= 1 << (ch & 63)
		}
	}
	return s
}

// has reports whether the character ch is in class a.
func (vm *vmProgram) has(p *parser, a int32, ch rune) bool {
	if uint32(ch) < 128 {
		return vm.ascii[a][ch>>6]&(1<<(ch&63)) != 0
	}
	cl := &vm.m.Classes[a]
	if !workControlsEnabled || p.control == nil || len(cl.Ranges) <= 4 {
		return cl.Has(ch)
	}
	for k := 0; k < len(cl.Ranges); k += 2 {
		p.stepWork()
		if cl.Ranges[k] <= ch && ch <= cl.Ranges[k+1] {
			return !cl.Negated
		}
	}
	return cl.Negated
}

// single advances one character (size units) and pushes a string node if b is set.
func (p *parser) single(size int, b bool) {
	start := p.pos
	p.pos += size
	if b {
		p.push(p.newNode(value.Data{Kind: value.MatchKind, Start: int32(start), End: int32(p.pos), Text: p.text(start, p.pos), Terminal: true, Fresh: true}))
	}
}

// attachCapturesNames attaches captures by a list of names (same as attachCaptures).
func (p *parser) attachCapturesNames(v *value.Node, names []string, f *frame, start, end int) *value.Node {
	return p.attachCaptures(v, &compile.Scope{Names: names}, f, start, end)
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

func (f *vmFunc) apply(x, y any) (any, error) {
	// The locals are placed on the expression stack below the operands of the body.
	p := f.ctx.p
	p.stepWork()
	base := len(p.estack)
	p.estack = append(appendWork(p, p.estack, f.locals), x)
	if f.n == 2 {
		p.estack = append(p.estack, y)
	}
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
			p.stepWork()
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
	p.dropCreated(ctx.cbase)
	if err != nil {
		return false
	}
	if b, ok := v.(bool); ok && !b {
		return false
	}
	return true
}

func (vm *vmProgram) bind(p *parser, name string, entry int) bool {
	ctx := p.useCtx(evalCtx{p: p, frame: p.frame, start: p.pos, end: p.pos, cbase: len(p.created)})
	v, err := vm.eval(ctx, entry, nil)
	p.dropCreated(ctx.cbase)
	if err != nil {
		return false
	}
	if _, isFn := v.(function); isFn {
		return false
	}
	p.bind(name, v)
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
	kids, lists := len(p.kidStack), len(p.lists)
	defer func() {
		p.estack = stack[:base]
		if err != nil { // a failed predicate lets the parse go on: drop gathered list elements
			p.dropKids(kids)
			p.lists = p.lists[:lists]
		}
	}()
	for {
		p.stepWork()
		in := &m.Exprs[ip]
		switch in.Op {
		case bytecode.EInt:
			stack = append(stack, int(int64(in.A)+int64(in.B)<<32))
		case bytecode.EStr:
			stack = append(stack, m.Strings[in.A])
		case bytecode.EBool:
			stack = append(stack, in.A == 1)
		case bytecode.ENil:
			stack = append(stack, nil)
		case bytecode.ECap:
			stack = append(stack, value.UnboxNode(ctx.frame.vals[in.A]))
		case bytecode.EItem:
			n := int(in.A)
			if n == 0 {
				stack = append(stack, ctx.newList(ctx.items))
			} else if n > len(ctx.items) {
				return nil, p.actionIndexError(n, len(ctx.items))
			} else {
				stack = append(stack, nodeOrNil(ctx.items[n-1]))
			}
		case bytecode.ELocal:
			stack = append(stack, locals[in.A])
		case bytecode.EVar:
			v, ok := ctx.p.lookupValue(m.Strings[in.A])
			if !ok {
				return nil, p.actionError("variable ", m.Strings[in.A], " is not defined")
			}
			stack = append(stack, v)
		case bytecode.EMember:
			x := pop()
			o, err := ctx.member(x, m.Strings[in.A])
			if err != nil {
				return nil, err
			}
			stack = append(stack, o)
		case bytecode.ENew:
			names := vm.fields[in.B]
			top := len(stack) - len(names)
			v, err := ctx.newStruct(m.Strings[in.A], names, stack[top:]) // newStruct does not retain vals
			if err != nil {
				return nil, err
			}
			stack = append(stack[:top], v)
		case bytecode.ECast, bytecode.ENode:
			// ECAST and ENODE share the case: a case of its own slows down the whole loop.
			var v any
			var err error
			switch in.Op {
			case bytecode.ECast:
				v, err = vm.cast(p, pop(), ctx.start, ip)
			default:
				v, err = vm.convert(p, pop(), ip)
			}
			if err != nil {
				return nil, err
			}
			stack = append(stack, v)
		case bytecode.EBin:
			r := pop()
			l := pop()
			v, err := p.valueBinary(m.Strings[in.A], l, r)
			if err != nil {
				return nil, err
			}
			stack = append(stack, v)
		case bytecode.EUnary:
			v, err := p.valueUnary(m.Strings[in.A], pop())
			if err != nil {
				return nil, err
			}
			stack = append(stack, v)
		case bytecode.EAnd, bytecode.EOr:
			op := "&&"
			if in.Op == bytecode.EOr {
				op = "||"
			}
			top := stack[len(stack)-1]
			b, ok := top.(bool)
			if !ok {
				return nil, p.actionError("invalid operand ", typeName(top), " for ", op)
			}
			if b == (in.Op == bytecode.EOr) {
				ip = int(in.A)
				continue
			}
			stack = stack[:len(stack)-1]
		case bytecode.EBoolChk:
			top := stack[len(stack)-1]
			if _, ok := top.(bool); !ok {
				return nil, p.actionError("invalid operand ", typeName(top), " for ", m.Strings[in.A])
			}
		case bytecode.EFunc:
			if len(locals) > 0 {
				// The locals may be on the expression stack, and a function can outlive the lambda that
				// created it (when the lambda returns it).
				locals = appendWork(p, []any(nil), locals)
			}
			stack = append(stack, p.newFunc(vmFunc{vm: vm, entry: int(in.A), n: int(in.B), locals: locals, ctx: ctx}))
		case bytecode.ECall:
			// The arguments stay on the stack during the call: lambdas called by the built-in push
			// above them, and built-ins do not retain args.
			top := len(stack) - int(in.B)
			p.estack = stack
			v, err := ctx.builtin(bytecode.BuiltinNames[in.A], stack[top:])
			stack = p.estack
			if err != nil {
				return nil, err
			}
			stack = append(stack[:top], v)
		case bytecode.ETextChk:
			if _, err := ctx.textString(stack[len(stack)-1]); err != nil {
				return nil, err
			}
		case bytecode.ETextEq:
			// Both values were checked by ETEXTCHK.
			ls, _ := ctx.textString(stack[len(stack)-2])
			rs, _ := ctx.textString(stack[len(stack)-1])
			stack = append(stack[:len(stack)-2], p.valueEqual(ls, rs) != (in.A == 1))
		case bytecode.EListBegin:
			p.lists = append(p.lists, len(p.kidStack))
		case bytecode.EListPush:
			top := len(stack) - int(in.A)
			if err := ctx.pushElems("list", stack[top:]); err != nil {
				return nil, err
			}
			stack = stack[:top]
		case bytecode.ETuple:
			top := len(stack) - int(in.A)
			base := len(p.kidStack)
			for _, v := range stack[top:] {
				p.stepWork()
				n, err := ctx.element("tuple", v)
				if err != nil {
					p.dropKids(base)
					return nil, err
				}
				p.kidStack = append(p.kidStack, n)
			}
			stack = append(stack[:top], ctx.tupleNode(p.kids(base)))
		case bytecode.EMapPush:
			top := len(stack) - 2
			p.estack = stack // the function's calls push above its arguments
			err := ctx.pushElems("map", stack[top:])
			stack = p.estack
			if err != nil {
				return nil, err
			}
			stack = stack[:top]
		case bytecode.EListEnd:
			b := p.lists[len(p.lists)-1]
			p.lists = p.lists[:len(p.lists)-1]
			stack = append(stack, ctx.listNode(p.kids(b)))
		case bytecode.ERet:
			return stack[len(stack)-1], nil
		default:
			return nil, fmt.Errorf("vm: invalid expression instruction %v at %d", in.Op, ip)
		}
		ip++
	}
}

func (vm *vmProgram) isStreamRepeat(ip int) bool {
	code := vm.m.Code
	if ip+1 >= len(code) || code[ip+1].Op != bytecode.OpIter {
		return false
	}
	end := int(code[ip+1].A)
	return end > ip+2 && end < len(code) && code[end].Op == bytecode.OpEndRepeat && code[end-1].Op == bytecode.OpNext && code[end-1].B == 2
}
