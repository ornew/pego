package engine

import (
	"fmt"
	"unsafe"

	"github.com/ornew/pego/internal/bytecode"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/value"
)

// vmProgram is a module prepared for execution. Rule calls, memoization, left recursion, and
// end-of-rule processing use the runtime shared with the closure engine (core/runtime.go), while rule
// bodies, Operators Rule sections, actions, and predicates run as bytecode.
type vmProgram struct {
	m        *bytecode.Module
	rules    []*core.Rule
	byName   map[string]*core.Rule
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

// vmRunSite is a repetition that a Document can resume (core/resume.go): the REPEAT at ip, whose ITER
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

// vmBodies returns the body of the rule r, whose code starts at entry in vm. Each driver of the
// VM passes its own to buildVMProgram, deciding how a body runs the code: the recursive VM's
// execBody calls exec, which recurses for each rule call.
type vmBodies func(vm *vmProgram, r *core.Rule, entry int) func(p *core.Parser, min int) (*value.Node, bool)

// newVMProgram prepares a module for execution with the recursive model.
func newVMProgram(m *bytecode.Module) *vmProgram { return buildVMProgram(m, execBody) }

// execBody runs a rule body with the recursive model: each call recurses into exec.
func execBody(vm *vmProgram, r *core.Rule, entry int) func(p *core.Parser, min int) (*value.Node, bool) {
	return func(p *core.Parser, min int) (*value.Node, bool) {
		v, ok, _ := vmMachineOf(p).exec(vm, entry, r, min)
		return v, ok
	}
}

// buildVMProgram prepares a module for execution. bodies makes the rule bodies.
func buildVMProgram(m *bytecode.Module, bodies vmBodies) *vmProgram {
	vm := &vmProgram{m: m, byName: map[string]*core.Rule{}, descs: append(append([]string(nil), compile.FixedDescs...), m.Strings...)}
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
		r := &core.Rule{
			ID: i, Name: m.Strings[ri.Name], Scope: vm.scope(ri.Scope),
			Memo: ri.Memo, Leader: ri.Leader, Positional: ri.Positional, Novalue: ri.NoValue, Transient: ri.Transient, Splice: ri.Splice, Vars: vm.names(ri.Vars),
		}
		if ri.Where >= 0 {
			r.Where = m.Strings[ri.Where]
		}
		r.InitVals = vtable.ValuesOf(r.Vars)
		if ri.NodeType >= 0 {
			if ri.OperatorsRule >= 0 {
				r.OpNodeType, r.OpKinds = m.Strings[ri.NodeType], vm.nodeKinds[ri.NodeType]
			} else {
				r.NodeType, r.NodeKinds = m.Strings[ri.NodeType], vm.nodeKinds[ri.NodeType]
			}
		}
		r.Kinds = value.NewRuleKinds(r.Name)
		r.Value = func(_ *core.Parser, v *value.Node, _ int) *value.Node { return v } // the code pushes the value of the body's scope
		if ri.Action >= 0 {
			r.Act = vm.evaluator(ri.Action, false)
		}
		vm.rules = append(vm.rules, r)
		if _, twin := vm.byName[r.Name]; !twin { // value-free twins follow the original rules
			vm.byName[r.Name] = r
		}
	}
	statics := make([]*compile.Rule, len(vm.rules))
	for i, r := range vm.rules {
		statics[i] = core.StaticRuleOf(r)
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
			r.OperatorsRule = vm.operatorsRule(&m.OperatorsRules[ri.OperatorsRule])
		}
		entry := ri.Entry
		r.Entry = entry
		r.Stream = vm.streams(entry)
		r.Body = bodies(vm, r, entry)
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
func (vm *vmProgram) block(entry int) core.Matcher {
	return func(p *core.Parser) (*value.Node, bool) {
		v, ok, cut := vmMachineOf(p).exec(vm, entry, nil, 0)
		if cut {
			p.Cut = true
		}
		return v, ok
	}
}

func (vm *vmProgram) line(l *bytecode.LineInfo, operator bool) *core.OperatorsRuleLine {
	// The code pushes the line's value already: the value of an operand line's scope, or the
	// operator's value, which is the node of its tuple if IsSeq (its captures are attached to it).
	pl := &core.OperatorsRuleLine{Scope: vm.scope(l.Scope), M: vm.block(l.Entry), Entry: l.Entry, Single: compile.SinglePass,
		Value: func(_ *core.Parser, v *value.Node, _ int) *value.Node { return v }}
	if operator && l.IsSeq {
		pl.Single, pl.Shape = compile.SingleTuple, compile.Shaper{Elems: true}
	}
	if l.Action >= 0 {
		pl.Act = vm.evaluator(l.Action, operator)
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
func (vm *vmProgram) cast(p *core.Parser, v any, pos, ip int) (any, error) {
	in := &vm.m.Exprs[ip]
	if t := vm.fieldTypes[in.A]; t == nil || !p.FieldConforms(t, v) {
		if t == nil {
			t = &compile.FieldType{Text: vm.m.Strings[in.A]}
		}
		return nil, p.CastError(v, t, pos, int(in.B), int(in.C))
	}
	return v, nil
}

// convert runs ENODE at ip on the value v, and returns it converted.
//
//go:noinline
func (vm *vmProgram) convert(p *core.Parser, v any, ip int) (any, error) {
	in := &vm.m.Exprs[ip]
	name := vm.m.Strings[in.A]
	var ks *value.NodeTypeKinds
	if vm.nodeKinds != nil {
		ks = vm.nodeKinds[in.A]
	}
	if ks == nil {
		return nil, p.ConvertError(v, name)
	}
	n, ok := ks.Convert(v)
	if !ok {
		return nil, p.ConvertError(v, name)
	}
	return n, nil
}

func (vm *vmProgram) operatorsRule(pi *bytecode.OperatorsRuleInfo) *core.OperatorsRule {
	pr := &core.OperatorsRule{SkipEntry: pi.Skip}
	if pi.Skip >= 0 {
		pr.Skip = vm.block(pi.Skip)
	}
	for i := range pi.Operands {
		pr.Operands = append(pr.Operands, vm.line(&pi.Operands[i], false))
	}
	op := func(o *bytecode.OpInfo) *core.OperatorsRuleOp {
		po := &core.OperatorsRuleOp{Id: o.ID, Level: o.Level, Line: vm.line(&o.Line, true)}
		po.Kind = []string{"prefix", "postfix", "infix"}[o.Kind]
		if o.Kind == bytecode.OperatorInfix {
			po.Assoc = []string{"left", "right", "none"}[o.Assoc]
		}
		return po
	}
	for i := range pi.Prefix {
		pr.Prefix = append(pr.Prefix, op(&pi.Prefix[i]))
	}
	for i := range pi.Led {
		pr.Led = append(pr.Led, op(&pi.Led[i]))
	}
	return pr
}

// vmMachine is the state of the bytecode VM for one parse, beside the parser it runs on: the
// value, entry, label and repetition stacks of the running code, the starts of the lists being
// gathered, the chunks that lambdas and pushed positions are allocated from, and the runStates of
// finished repetitions for reuse. The parser holds it as its backend state (vmMachineOf). A
// driver built on this VM that needs state of its own, such as frames, embeds vmMachine in that
// state and points driver at it.
type vmMachine struct {
	p        *core.Parser
	vals     []any      // value stack
	ents     []vmEntry  // entry stack
	labs     []labState // state of the label, recovery and skip entries on ents
	reps     []repState
	lists    []int // starts on KidStack of the lists being gathered by ELISTBEGIN
	funcSlab []vmFunc
	posSlab  []int            // positions pushed as values (newPos); stream splits keep it
	freeRuns []*core.RunState // runStates of finished repetitions (newRunState)
	driver   vmDriverState
}

// vmDriverState is the state that a driver built on the VM keeps beside vmMachine.
type vmDriverState interface {
	// reset drops the state of the last parse. vmMachine keeps the driver state for the next
	// parse only through keep, which resets it.
	reset()
}

// vmMachineOf returns the VM state of the parse that p runs, making it on first use.
func vmMachineOf(p *core.Parser) *vmMachine {
	if mc, ok := p.Backend.(*vmMachine); ok {
		return mc
	}
	mc := &vmMachine{p: p}
	p.Backend = mc
	return mc
}

func (mc *vmMachine) SplitChunks() { mc.funcSlab = nil }

func (mc *vmMachine) ClearInactive() { core.ClearWork(mc.p, mc.vals[len(mc.vals):cap(mc.vals)]) }

func (mc *vmMachine) Attach(p *core.Parser) { mc.p = p }

// Keep keeps only the value stack, whose capacity a Document reuses (reuseDocumentVMValues), and
// the reset driver state.
func (mc *vmMachine) Keep(n int, clearUsed bool) {
	vals := mc.vals[:0]
	// A large document may have been replaced with a small input. Bound the kept capacity by
	// the current size, with room for small grammars' fixed stack overhead. Division avoids int
	// overflow.
	if cap(vals)/2 > max(512, n) {
		vals = nil
	}
	if clearUsed {
		clear(vals[:cap(vals)])
	}
	if mc.driver != nil {
		mc.driver.reset()
	}
	*mc = vmMachine{vals: vals, driver: mc.driver}
}

func (mc *vmMachine) Release() {
	clear(mc.vals[:cap(mc.vals)])
	if mc.driver != nil {
		mc.driver.reset()
	}
	*mc = vmMachine{driver: mc.driver}
}

func (mc *vmMachine) RetainedBytes() uint64 {
	return uint64(cap(mc.vals)) * uint64(unsafe.Sizeof(any(nil)))
}

// newFunc allocates a copy of the lambda f from a chunk and returns it.
func (mc *vmMachine) newFunc(f vmFunc) *vmFunc {
	if len(mc.funcSlab) == 0 {
		if mc.p.PrivatePrefixAlloc() {
			n := new(vmFunc)
			*n = f
			return n
		}
		mc.funcSlab = make([]vmFunc, core.NodeChunk/4)
	}
	n := &mc.funcSlab[0]
	mc.funcSlab = mc.funcSlab[1:]
	*n = f
	return n
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
// and skip entries is on a separate stack (vmMachine.labs).
type vmEntry struct {
	kind uint8
	cut  bool
	ip   int32
	lab  int32 // eLabel, eRecover, eSkip: index of the entry's state in vmMachine.labs
	save vmSave
}

// labState is the state of a label, recovery or skip entry.
type labState struct {
	msg  int32        // eLabel: message
	mode int32        // eLabel: 0 for @error, 1 for @diagnostic
	far  int          // eLabel, eRecover: saved farthest failure. eSkip: recovered failure
	base int          // eLabel, eRecover: saved start of the expectation record (ExpMark)
	bits uint64       // eLabel, eRecover: saved membership filter
	exp  []core.ExpID // eSkip: expectations of the recovered failure
}

// pushLab pushes an entry of kind with the state l.
func (mc *vmMachine) pushLab(kind uint8, ip int32, save vmSave, l labState) {
	mc.labs = append(mc.labs, l)
	mc.ents = append(mc.ents, vmEntry{kind: kind, ip: ip, lab: int32(len(mc.labs) - 1), save: save})
}

// popLab pops the state of the entry e, which has just been popped.
func (mc *vmMachine) popLab(e *vmEntry) labState {
	l := mc.labs[e.lab]
	mc.labs = mc.labs[:e.lab]
	return l
}

// vmSave is a saved state. Stack heights are int32 to keep entries small.
type vmSave struct {
	pos                          int
	vals, reps, trail, recovered int32
	env                          *core.Env
	frame                        *core.Frame
}

type repState struct {
	min, max, count int
	scope           int
	base            int            // bottom of the element values
	run             *core.RunState // the run being recorded in a Document parse, or nil
}

func (mc *vmMachine) save() vmSave {
	p := mc.p
	return vmSave{pos: p.Pos, vals: int32(len(mc.vals)), reps: int32(len(mc.reps)), trail: int32(len(p.Trail)),
		recovered: int32(len(p.Recovered)), env: p.Env, frame: p.Frame}
}

func (mc *vmMachine) restore(s *vmSave) {
	p := mc.p
	p.Pos = s.pos
	mc.vals = mc.vals[:s.vals]
	mc.reps = mc.reps[:s.reps]
	p.UndoCaptures(int(s.trail))
	p.Trail = p.Trail[:core.Min2(int(s.trail), len(p.Trail))]
	p.Recovered = p.Recovered[:s.recovered]
	p.Env = s.env
	p.Frame = s.frame
}

func (mc *vmMachine) push(v any) { mc.vals = append(mc.vals, v) }
func (mc *vmMachine) pop() any {
	v := mc.vals[len(mc.vals)-1]
	mc.vals = mc.vals[:len(mc.vals)-1]
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
// OperatorsRuleParse) through host recursion.
func (mc *vmMachine) exec(vm *vmProgram, ip int, r *core.Rule, min int) (result *value.Node, ok bool, cut bool) {
	p := mc.p
	bv := mc.newBody(ip, r, min)
	b := &bv
	ev, v, ok := mc.step(vm, b, false, false, nil)
	for {
		switch ev {
		case evCall:
			in := &vm.m.Code[b.ip]
			if r := vm.rules[in.A]; r.Plain && !p.NoPlain {
				v, ok = p.InvokePlain(r, int(in.B)) // what call does for such a rule
			} else {
				v, ok = p.Call(r, int(in.B))
			}
		case evOperatorsRule:
			v, ok = p.OperatorsRuleParse(b.r, b.min)
		default:
			return v, ok, b.cut
		}
		ev, v, ok = mc.step(vm, b, true, ok, v)
	}
}

// vmBody is the state of one run of code (a rule body or an Operators Rule section).
type vmBody struct {
	ip                  int
	vbase, ebase, rbase int
	r                   *core.Rule
	min                 int
	cut                 bool // a cut belonging to no choice was passed
}

func (mc *vmMachine) newBody(ip int, r *core.Rule, min int) vmBody {
	return vmBody{ip: ip, vbase: len(mc.vals), ebase: len(mc.ents), rbase: len(mc.reps), r: r, min: min}
}

// Reasons step suspends
const (
	evDone          = iota // finished by RETURN, END, or failure
	evCall                 // CALL (the instruction at b.ip); resumed with the result
	evOperatorsRule        // OPERATORS_RULE; resumed with the result
)

// step runs code until it finishes or reaches a rule call or an Operators Rule expression.
// If resume is set, it resumes with the result (ok, v) of the suspended instruction.
func (mc *vmMachine) step(vm *vmProgram, b *vmBody, resume, rok bool, rv *value.Node) (ev int, result *value.Node, ok bool) {
	p := mc.p
	m := vm.m
	code := m.Code
	ip, ebase := b.ip, b.ebase
	if resume {
		if rok {
			if in := &code[ip]; in.Op == bytecode.OpOperatorsRule || in.C == 1 {
				mc.push(rv)
			}
			ip++
		} else if ip, ok = mc.unwind(m, ebase); !ok {
			mc.vals = mc.vals[:b.vbase]
			mc.reps = mc.reps[:b.rbase]
			return evDone, nil, false
		}
	}
	for {
		p.StepWork()
		in := &code[ip]
		switch in.Op {
		case bytecode.OpStr:
			start := p.Pos
			if !p.MatchLiteral(vm.runes[in.A], vm.bytes[in.A]) {
				p.Expect(start, compile.NumFixedDescs+core.ExpID(in.B))
				goto fail
			}
			if in.C == 1 {
				mc.push(p.NewNode(value.Data{Kind: value.MatchKind, Start: int32(start), End: int32(p.Pos), Text: p.LiteralText(start, vm.literals[in.A]), Terminal: true, Fresh: true}))
			}
		case bytecode.OpStrFold:
			start := p.Pos
			if !p.MatchLiteralFold(vm.runes[in.A]) {
				p.Expect(start, compile.NumFixedDescs+core.ExpID(in.B))
				goto fail
			}
			if in.C == 1 {
				mc.push(p.NewNode(value.Data{Kind: value.MatchKind, Start: int32(start), End: int32(p.Pos), Text: p.Text(start, p.Pos), Terminal: true, Fresh: true}))
			}
		case bytecode.OpClass:
			ch, size, ok := p.Peek()
			if !ok || !vm.has(p, in.A, ch) {
				p.Expect(p.Pos, compile.NumFixedDescs+core.ExpID(in.B))
				goto fail
			}
			mc.single(size, in.C == 1)
		case bytecode.OpGuard:
			ch, _, ok := p.Peek()
			if !(ok && vm.has(p, in.A, ch)) && p.Depth+int(in.B) <= p.MaxDepth {
				p.Expect(p.Pos, compile.NumFixedDescs+core.ExpID(m.Classes[in.A].Desc))
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
				ch, size, ok := p.Peek()
				if !ok || in.A >= 0 && !vm.has(p, in.A, ch) {
					if in.A >= 0 {
						p.Expect(p.Pos, compile.NumFixedDescs+core.ExpID(m.Classes[in.A].Desc))
					} else {
						p.Expect(p.Pos, compile.IDAny)
					}
					break
				}
				p.Pos += size
				count++
			}
			if count < min {
				goto fail
			}
		case bytecode.OpAny:
			_, size, ok := p.Peek()
			if !ok {
				p.Expect(p.Pos, compile.IDAny)
				goto fail
			}
			mc.single(size, in.A == 1)
		case bytecode.OpTop:
			if in.A == 1 {
				mc.push(p.NewNode(value.Data{Kind: value.MatchKind, Start: int32(p.Pos), End: int32(p.Pos), Terminal: true, Fresh: true}))
			}
		case bytecode.OpFail:
			goto fail
		case bytecode.OpAssert:
			var ok bool
			var desc core.ExpID
			switch in.A {
			case 0:
				ok, desc = p.AtInputStart(), compile.IDBeginInput
			case 1:
				ok, desc = p.AtEOF(), compile.IDEndInput
			case 2:
				ok, desc = p.AtLineStart(), compile.IDBeginLine
			default:
				ch, _, more := p.Peek()
				ok, desc = !more || ch == '\n' || ch == '\r', compile.IDEndLine
			}
			if !ok {
				p.Expect(p.Pos, desc)
				goto fail
			}
		case bytecode.OpJump:
			ip = int(in.A)
			continue
		case bytecode.OpChoice:
			mc.ents = append(mc.ents, vmEntry{kind: eChoice, ip: in.A, save: mc.save()})
		case bytecode.OpCommit:
			mc.ents = mc.ents[:len(mc.ents)-1]
			ip = int(in.A)
			continue
		case bytecode.OpCut:
			marked := false
			for k := len(mc.ents) - 1; k >= ebase; k-- {
				p.StepWork()
				e := &mc.ents[k]
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
			mc.push(mc.newPos(p.Pos))
		case bytecode.OpPushNil:
			mc.push(nil)
		case bytecode.OpSeq:
			n := int(in.A)
			kids := p.Nodes(n)
			top := len(mc.vals)
			for k := 0; k < n; k++ {
				p.StepWork()
				kids[k] = asNodeValue(mc.vals[top-n+k])
			}
			start := *mc.vals[top-n-1].(*int)
			mc.vals = mc.vals[:top-n-1]
			mc.push(p.NewNode(value.Data{Kind: value.SeqKind, Start: int32(start), End: int32(p.Pos), Children: kids, Fresh: true}))
		case bytecode.OpSpread:
			v := mc.pop()
			switch {
			case in.B == 1:
				if n := asNodeValue(v); n != nil {
					for _, k := range value.D(n).Children {
						p.StepWork()
						mc.push(k)
					}
				}
			case in.A == 1:
				mc.push(v)
			}
		case bytecode.OpAtomic:
			start := *mc.pop().(*int)
			if in.A == 1 {
				mc.push(p.NewNode(value.Data{Kind: value.MatchKind, Start: int32(start), End: int32(p.Pos), Text: p.Text(start, p.Pos), Terminal: true, Fresh: true}))
			}
		case bytecode.OpCapture:
			p.SetCapture(int(in.A), asNodeValue(mc.vals[len(mc.vals)-1]))
			if in.B == 1 {
				mc.vals = mc.vals[:len(mc.vals)-1]
			}
		case bytecode.OpRepeat, bytecode.OpRepeatWide:
			if core.StreamPrefixIsolation && p.Emit != nil && p.Depth == 1 && vm.isStreamRepeat(ip) {
				p.StartStreamChunks()
			}
			min, max := int(in.A), int(in.B)
			if in.Op == bytecode.OpRepeatWide {
				min, max = int(m.Exprs[in.A].Integer()), int(m.Exprs[in.B].Integer())
			}
			mc.reps = append(mc.reps, repState{min: min, max: max, scope: int(in.C), base: len(mc.vals)})
			if p.Runs != nil && p.Silent == 0 {
				if site := vm.runSites[ip]; site != nil {
					// A Document parse: resume the run of the previous parse (core/resume.go).
					r := mc.newRunState()
					p.StartRun(r, ip, min, max, site.shiftable)
					rep := &mc.reps[len(mc.reps)-1]
					rep.run = r
					for el := p.NextPrefix(r); el != nil; el = p.NextPrefix(r) {
						if site.keep {
							mc.push(el.V)
						}
					}
					rep.count = r.Count
					if !r.More() {
						ip = site.end
						continue
					}
				}
			}
		case bytecode.OpIter:
			rep := &mc.reps[len(mc.reps)-1]
			if rep.max >= 0 && rep.count >= rep.max {
				ip = int(in.A)
				continue
			}
			kind := uint8(eIter)
			if rep.run != nil {
				r := rep.run
				if p.Resync(r) {
					for el := p.NextTail(r); el != nil; el = p.NextTail(r) {
						if m.Code[in.A-1].B != 0 { // NEXT keeps values
							mc.push(el.V)
						}
					}
					rep.count = r.Count
					if !r.More() {
						ip = int(in.A)
						continue
					}
				}
				kind = eRunIter
				p.BeginElem(r)
			}
			mc.ents = append(mc.ents, vmEntry{kind: kind, ip: in.A, save: mc.save()})
			if sc := mc.reps[len(mc.reps)-1].scope; sc >= 0 {
				p.Frame = p.NewFrame(len(vm.scopes[sc]))
			}
		case bytecode.OpNext:
			e := mc.ents[len(mc.ents)-1]
			mc.ents = mc.ents[:len(mc.ents)-1]
			rep := &mc.reps[len(mc.reps)-1]
			if rep.scope >= 0 {
				if in.B == 3 { // a projected repetition: the value is the field's capture
					var v *value.Node
					if int(in.C) < len(p.Frame.Vals) { // (a corrupted file may name another slot)
						v = p.Frame.Vals[in.C]
					}
					mc.push(v)
				} else if in.B != 0 { // in a value-free repetition, the element scope exists only for predicates
					top := len(mc.vals) - 1
					mc.vals[top] = p.AttachCaptures(asNodeValue(mc.vals[top]), &compile.Scope{Names: vm.scopes[rep.scope]}, p.Frame, e.save.pos, p.Pos)
				}
				p.Frame = e.save.frame
			}
			rep.count++
			if rep.run != nil {
				var v *value.Node
				if in.B != 0 {
					v = asNodeValue(mc.vals[len(mc.vals)-1])
				}
				p.EndElem(rep.run, true, v)
			}
			if in.B == 2 && p.Emit != nil && p.Depth == 1 {
				// Elements of the start rule's #stream are passed on without being retained, and committed.
				v := asNodeValue(mc.pop())
				if v != nil {
					value.D(v).Fresh = false
				}
				if err := p.EmitStream(v); err != nil {
					panic(core.Fatal{Err: err})
				}
				p.Commit(p.Pos)
				if p.Pos == e.save.pos {
					ip = int(e.ip)
					continue
				}
				ip = int(in.A)
				continue
			}
			if p.Pos == e.save.pos || rep.max >= 0 && rep.count >= rep.max {
				ip = int(e.ip)
			} else {
				ip = int(in.A)
			}
			continue
		case bytecode.OpEndRepeat:
			rep := mc.reps[len(mc.reps)-1]
			mc.reps = mc.reps[:len(mc.reps)-1]
			if rep.count < rep.min {
				goto fail
			}
			if rep.run != nil {
				p.FinishRun(rep.run)
				mc.freeRunState(rep.run)
			}
			if in.A == 1 {
				kids := p.Nodes(len(mc.vals) - rep.base)
				for k := range kids {
					p.StepWork()
					kids[k] = asNodeValue(mc.vals[rep.base+k])
				}
				start := *mc.vals[rep.base-1].(*int)
				mc.vals = mc.vals[:rep.base-1]
				mc.push(p.NewNode(value.Data{Kind: value.ListKind, Start: int32(start), End: int32(p.Pos), Children: kids, Fresh: true}))
			}
		case bytecode.OpLook:
			kind := uint8(eLook)
			if in.B == 1 {
				kind = eNotLook
			}
			mc.ents = append(mc.ents, vmEntry{kind: kind, ip: in.A, save: mc.save()})
			p.Silent++
		case bytecode.OpEndLook:
			e := mc.ents[len(mc.ents)-1]
			mc.ents = mc.ents[:len(mc.ents)-1]
			p.Silent--
			if in.A == 1 {
				mc.restore(&e.save)
				goto fail
			}
			p.Pos = e.save.pos
			mc.vals = mc.vals[:int(e.save.vals)]
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
			mk := p.Isolate(p.Pos)
			mc.pushLab(eLabel, 0, vmSave{}, labState{msg: in.A, mode: in.B, far: mk.Far, base: mk.Base, bits: mk.Bits})
		case bytecode.OpEndLabel:
			e := mc.ents[len(mc.ents)-1]
			mc.ents = mc.ents[:len(mc.ents)-1]
			l := mc.popLab(&e)
			far, inner := p.Unisolate(core.ExpMark{Far: l.far, Base: l.base, Bits: l.bits})
			p.MergeExpected(far, inner)
		case bytecode.OpRecover:
			mk := p.Isolate(p.Pos)
			mc.pushLab(eRecover, in.A, mc.save(), labState{far: mk.Far, base: mk.Base, bits: mk.Bits})
		case bytecode.OpEndRecover:
			e := mc.ents[len(mc.ents)-1]
			mc.ents = mc.ents[:len(mc.ents)-1]
			l := mc.popLab(&e)
			far, inner := p.Unisolate(core.ExpMark{Far: l.far, Base: l.base, Bits: l.bits})
			p.MergeExpected(far, inner)
			ip = int(in.A)
			continue
		case bytecode.OpEndSkip:
			e := mc.ents[len(mc.ents)-1]
			mc.ents = mc.ents[:len(mc.ents)-1]
			l := mc.popLab(&e)
			if p.Pos == e.save.pos {
				mc.restore(&e.save)
				p.MergeExpected(l.far, l.exp)
				goto fail
			}
			se := p.MakeError(l.far, l.exp)
			p.Recovered = append(p.Recovered, se)
			if in.A == 1 {
				mc.push(p.NewNode(value.Data{Kind: value.ErrorKind, Start: int32(e.save.pos), End: int32(p.Pos), Text: p.Text(e.save.pos, p.Pos), Fields: value.Fields{{Name: "message", Value: p.RecoveryErrorText(se)}}, Fresh: true, Terminal: true}))
			}
		case bytecode.OpReturn, bytecode.OpEnd:
			var v *value.Node
			if len(mc.vals) > b.vbase { // skip code builds no values
				v = asNodeValue(mc.vals[len(mc.vals)-1])
			}
			mc.vals = mc.vals[:b.vbase]
			mc.reps = mc.reps[:b.rbase]
			return evDone, v, true
		default:
			panic(fmt.Sprintf("vm: invalid instruction %v at %d", in.Op, ip))
		}
		ip++
		continue

	fail:
		if ip, ok = mc.unwind(m, ebase); !ok {
			mc.vals = mc.vals[:b.vbase]
			mc.reps = mc.reps[:b.rbase]
			return evDone, nil, false
		}
	}
}

// unwind unwinds the entry stack to ebase on failure and returns the index of the instruction to
// resume at. ok is false if there is no entry to resume from.
func (mc *vmMachine) unwind(m *bytecode.Module, ebase int) (ip int, ok bool) {
	p := mc.p
	for {
		if len(mc.ents) == ebase {
			return 0, false
		}
		p.StepWork()
		e := mc.ents[len(mc.ents)-1]
		mc.ents = mc.ents[:len(mc.ents)-1]
		switch e.kind {
		case eRunIter:
			p.EndElem(mc.reps[e.save.reps-1].run, false, nil)
			mc.restore(&e.save)
			if e.cut {
				continue
			}
			return int(e.ip), true
		case eChoice, eIter:
			mc.restore(&e.save)
			if e.cut {
				continue
			}
			return int(e.ip), true
		case eLook:
			p.Silent--
			mc.restore(&e.save)
			continue
		case eNotLook:
			p.Silent--
			mc.restore(&e.save)
			return int(e.ip), true
		case eLabel:
			l := mc.popLab(&e)
			far, inner := p.Unisolate(core.ExpMark{Far: l.far, Base: l.base, Bits: l.bits})
			if l.mode == 0 {
				p.Expect(far, core.MsgBit|(compile.NumFixedDescs+core.ExpID(l.msg)))
			} else {
				p.MergeExpected(far, core.RenamedExpected(p, inner, compile.NumFixedDescs+core.ExpID(l.msg)))
			}
			continue
		case eRecover:
			l := mc.popLab(&e)
			far, inner := p.Unisolate(core.ExpMark{Far: l.far, Base: l.base, Bits: l.bits})
			exp := p.Keep(inner) // keep them while skip runs
			mc.restore(&e.save)
			mc.pushLab(eSkip, 0, e.save, labState{far: far, exp: exp})
			return int(e.ip), true
		case eSkip:
			l := mc.popLab(&e)
			mc.restore(&e.save)
			p.MergeExpected(l.far, l.exp)
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
func (vm *vmProgram) has(p *core.Parser, a int32, ch rune) bool {
	if uint32(ch) < 128 {
		return vm.ascii[a][ch>>6]&(1<<(ch&63)) != 0
	}
	cl := &vm.m.Classes[a]
	if !core.WorkControlsEnabled || p.Control == nil || len(cl.Ranges) <= 4 {
		return cl.Has(ch)
	}
	for k := 0; k < len(cl.Ranges); k += 2 {
		p.StepWork()
		if cl.Ranges[k] <= ch && ch <= cl.Ranges[k+1] {
			return !cl.Negated
		}
	}
	return cl.Negated
}

// single advances one character (size units) and pushes a string node if b is set.
func (mc *vmMachine) single(size int, b bool) {
	p := mc.p
	start := p.Pos
	p.Pos += size
	if b {
		mc.push(p.NewNode(value.Data{Kind: value.MatchKind, Start: int32(start), End: int32(p.Pos), Text: p.Text(start, p.Pos), Terminal: true, Fresh: true}))
	}
}

// --- Running expression code ---

// vmFunc is a bytecode lambda.
type vmFunc struct {
	vm     *vmProgram
	entry  int
	n      int
	locals []any
	ctx    *core.EvalCtx
}

func (f *vmFunc) Arity() int { return f.n }

// The VM implements the interfaces of core for lambdas and backend state.
var (
	_ core.Function     = (*vmFunc)(nil)
	_ core.BackendState = (*vmMachine)(nil)
)

func (f *vmFunc) Apply(x, y any) (any, error) {
	// The locals are placed on the expression stack below the operands of the body.
	p := f.ctx.P
	p.StepWork()
	base := len(p.Estack)
	p.Estack = append(core.AppendWork(p, p.Estack, f.locals), x)
	if f.n == 2 {
		p.Estack = append(p.Estack, y)
	}
	v, err := f.vm.eval(f.ctx, f.entry, p.Estack[base:len(p.Estack):len(p.Estack)])
	p.Estack = p.Estack[:base]
	return v, err
}

// evaluator returns an evaluator for an action in expression code. If operator is set, $lhs,
// $rhs, and $op are locals.
func (vm *vmProgram) evaluator(entry int, operator bool) core.Evaluator {
	if !operator {
		return func(ctx *core.EvalCtx) (any, error) { return vm.eval(ctx, entry, nil) }
	}
	return func(ctx *core.EvalCtx) (any, error) {
		p := ctx.P
		base := len(p.Estack)
		p.Estack = append(p.Estack, nil, nil, nil)
		locals := p.Estack[base : base+3 : base+3]
		for l := ctx.Locals; l != nil; l = l.Next {
			p.StepWork()
			switch l.Name {
			case "lhs":
				locals[0] = l.Val
			case "rhs":
				locals[1] = l.Val
			case "op":
				locals[2] = l.Val
			}
		}
		v, err := vm.eval(ctx, entry, locals)
		p.Estack = p.Estack[:base]
		return v, err
	}
}

func (vm *vmProgram) predicate(p *core.Parser, entry int) bool {
	ctx := p.UseCtx(core.EvalCtx{P: p, Frame: p.Frame, Start: p.Pos, End: p.Pos, Cbase: len(p.Created)})
	v, err := vm.eval(ctx, entry, nil)
	p.DropCreated(ctx.Cbase)
	if err != nil {
		return false
	}
	if b, ok := v.(bool); ok && !b {
		return false
	}
	return true
}

func (vm *vmProgram) bind(p *core.Parser, name string, entry int) bool {
	ctx := p.UseCtx(core.EvalCtx{P: p, Frame: p.Frame, Start: p.Pos, End: p.Pos, Cbase: len(p.Created)})
	v, err := vm.eval(ctx, entry, nil)
	p.DropCreated(ctx.Cbase)
	if err != nil {
		return false
	}
	if _, isFn := v.(core.Function); isFn {
		return false
	}
	p.Bind(name, v)
	return true
}

// eval evaluates expression code from ip until ERET. The operands are kept on the parser's
// expression stack (p.Estack) above its current top, so evaluations nested through lambdas
// (vmFunc.apply) share one area; the stack is back at its original height when eval returns.
func (vm *vmProgram) eval(ctx *core.EvalCtx, ip int, locals []any) (v any, err error) {
	m := vm.m
	p := ctx.P
	mc := vmMachineOf(p)
	stack := p.Estack
	base := len(stack)
	pop := func() any {
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		return v
	}
	kids, lists := len(p.KidStack), len(mc.lists)
	defer func() {
		p.Estack = stack[:base]
		if err != nil { // a failed predicate lets the parse go on: drop gathered list elements
			p.DropKids(kids)
			mc.lists = mc.lists[:lists]
		}
	}()
	for {
		p.StepWork()
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
			stack = append(stack, value.UnboxNode(ctx.Frame.Vals[in.A]))
		case bytecode.EItem:
			n := int(in.A)
			if n == 0 {
				stack = append(stack, ctx.NewList(ctx.Items))
			} else if n > len(ctx.Items) {
				return nil, p.ActionIndexError(n, len(ctx.Items))
			} else {
				stack = append(stack, core.NodeOrNil(ctx.Items[n-1]))
			}
		case bytecode.ELocal:
			stack = append(stack, locals[in.A])
		case bytecode.EVar:
			v, ok := ctx.P.LookupValue(m.Strings[in.A])
			if !ok {
				return nil, p.ActionError("variable ", m.Strings[in.A], " is not defined")
			}
			stack = append(stack, v)
		case bytecode.EMember:
			x := pop()
			o, err := ctx.Member(x, m.Strings[in.A])
			if err != nil {
				return nil, err
			}
			stack = append(stack, o)
		case bytecode.ENew:
			names := vm.fields[in.B]
			top := len(stack) - len(names)
			v, err := ctx.NewStruct(m.Strings[in.A], names, stack[top:]) // NewStruct does not retain vals
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
				v, err = vm.cast(p, pop(), ctx.Start, ip)
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
			v, err := p.ValueBinary(m.Strings[in.A], l, r)
			if err != nil {
				return nil, err
			}
			stack = append(stack, v)
		case bytecode.EUnary:
			v, err := p.ValueUnary(m.Strings[in.A], pop())
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
				return nil, p.ActionError("invalid operand ", core.TypeName(top), " for ", op)
			}
			if b == (in.Op == bytecode.EOr) {
				ip = int(in.A)
				continue
			}
			stack = stack[:len(stack)-1]
		case bytecode.EBoolChk:
			top := stack[len(stack)-1]
			if _, ok := top.(bool); !ok {
				return nil, p.ActionError("invalid operand ", core.TypeName(top), " for ", m.Strings[in.A])
			}
		case bytecode.EFunc:
			if len(locals) > 0 {
				// The locals may be on the expression stack, and a function can outlive the lambda that
				// created it (when the lambda returns it).
				locals = core.AppendWork(p, []any(nil), locals)
			}
			stack = append(stack, mc.newFunc(vmFunc{vm: vm, entry: int(in.A), n: int(in.B), locals: locals, ctx: ctx}))
		case bytecode.ECall:
			// The arguments stay on the stack during the call: lambdas called by the built-in push
			// above them, and built-ins do not retain args.
			top := len(stack) - int(in.B)
			p.Estack = stack
			v, err := ctx.Builtin(bytecode.BuiltinNames[in.A], stack[top:])
			stack = p.Estack
			if err != nil {
				return nil, err
			}
			stack = append(stack[:top], v)
		case bytecode.ETextChk:
			if _, err := ctx.TextString(stack[len(stack)-1]); err != nil {
				return nil, err
			}
		case bytecode.ETextEq:
			// Both values were checked by ETEXTCHK.
			ls, _ := ctx.TextString(stack[len(stack)-2])
			rs, _ := ctx.TextString(stack[len(stack)-1])
			stack = append(stack[:len(stack)-2], p.ValueEqual(ls, rs) != (in.A == 1))
		case bytecode.EListBegin:
			mc.lists = append(mc.lists, len(p.KidStack))
		case bytecode.EListPush:
			top := len(stack) - int(in.A)
			if err := ctx.PushElems("list", stack[top:]); err != nil {
				return nil, err
			}
			stack = stack[:top]
		case bytecode.ETuple:
			top := len(stack) - int(in.A)
			base := len(p.KidStack)
			for _, v := range stack[top:] {
				p.StepWork()
				n, err := ctx.Element("tuple", v)
				if err != nil {
					p.DropKids(base)
					return nil, err
				}
				p.KidStack = append(p.KidStack, n)
			}
			stack = append(stack[:top], ctx.TupleNode(p.Kids(base)))
		case bytecode.EMapPush:
			top := len(stack) - 2
			p.Estack = stack // the function's calls push above its arguments
			err := ctx.PushElems("map", stack[top:])
			stack = p.Estack
			if err != nil {
				return nil, err
			}
			stack = stack[:top]
		case bytecode.EListEnd:
			b := mc.lists[len(mc.lists)-1]
			mc.lists = mc.lists[:len(mc.lists)-1]
			stack = append(stack, ctx.ListNode(p.Kids(b)))
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

// moduleVarTable returns the variable table of the module m, or nil when it declares no variable.
func moduleVarTable(m *bytecode.Module) *compile.VarTable {
	var t *compile.VarTable
	for _, v := range m.Vars {
		d := &compile.VarDecl{Name: m.Strings[v.Name]}
		switch v.Type {
		case bytecode.VarInt:
			d.Typ, d.Init = compile.TyInt, v.Int
		case bytecode.VarString:
			d.Typ, d.Init = compile.TyString, m.Strings[v.Str]
		default:
			d.Typ, d.Init = compile.TyBool, v.Int == 1
		}
		if t == nil {
			t = compile.NewVarTable()
		}
		t.Add(d)
	}
	return t
}

// moduleStreams inspects rule bytecode metadata without building an executable VM.
func moduleStreams(m *bytecode.Module, entry int) bool {
	for _, in := range m.Code[entry:] {
		switch {
		case in.Op == bytecode.OpReturn:
			return false
		case in.Op == bytecode.OpNext && in.B == 2:
			return true
		}
	}
	return false
}

// newPos allocates a copy of the position pos from a chunk and returns it. Start positions are
// pushed onto the VM value stack as *int: converting an int to an interface would allocate each
// time.
func (mc *vmMachine) newPos(pos int) *int {
	if len(mc.posSlab) == 0 {
		mc.posSlab = make([]int, core.PtrChunk)
	}
	n := &mc.posSlab[0]
	mc.posSlab = mc.posSlab[1:]
	*n = pos
	return n
}

// newRunState returns a RunState for a VM repetition, reusing a freed one.
func (mc *vmMachine) newRunState() *core.RunState {
	if n := len(mc.freeRuns); n > 0 {
		r := mc.freeRuns[n-1]
		mc.freeRuns = mc.freeRuns[:n-1]
		return r
	}
	return &core.RunState{}
}

// freeRunState makes the RunState of a finished repetition available to later ones.
func (mc *vmMachine) freeRunState(r *core.RunState) {
	r.Old = nil
	core.ClearWork(mc.p, r.Mid)
	core.ClearWork(mc.p, r.Post)
	mc.freeRuns = append(mc.freeRuns, r)
}
