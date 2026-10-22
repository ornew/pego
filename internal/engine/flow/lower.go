package flow

import (
	"fmt"
	"strings"

	"github.com/ornew/pego/internal/engine/core"
)

// Lowering a rule of Core IR to a Flow IR function.
//
// Lower threads a failure label through each node: a node's statements leave the block of that
// label when the node fails, and run to their end when it succeeds, leaving the node's value in a
// variable. Captures are variables of the function; those of a repetition element with captures
// of its own are cleared at the start of each iteration. A backtracking point (choice, option,
// repetition element, negative lookahead) saves the position, the number of recovered errors if
// the module can recover, the environment if the expression binds variables, and the capture
// slots the expression may write (Effects.Slots), and restores them when the expression fails.
// Cuts are flags of the function, one per backtracking point inside a rule whose body cuts.
//
// A framed function (LowerFrame) keeps the captures in the runtime's frames instead: a capture
// sets its slot of the current frame, a repetition element with captures of its own runs under a
// frame of its own, and a backtracking point marks the whole parser state, frames included, and
// resets to the mark when it fails. Its outermost scope cuts the flag the runtime gave it.
//
// The lowering follows reachability as it goes: after an Exit the point is unreachable until a
// block that is exited to from a reachable point ends, or a loop ends. A node that begins at an
// unreachable point is not lowered, nor are the alternatives of a choice after one that cannot
// fail, nor the failure path of a backtracking point whose body cannot fail. The other statements
// written at unreachable points are kept (see the package comment).
//
// Variables and labels are numbered from one counter in the order they are made; the variables
// shared by the whole function (ok, ch, size, bv) are made once, at their first use.

// Options selects the forms the lowering writes.
type Options struct {
	// Work charges a step on entering each node and on saving or restoring each capture slot.
	Work bool
	// Recovers reports that the module can append recovered errors (Recovers), so backtracking
	// points save their number.
	Recovers bool
	// ClassTables matches a class of more than four ranges through its membership table.
	ClassTables bool
	// GuardedClasses keeps the size of the character a choice's guards read, for the classes
	// that begin alternatives marked GuardedClass, which only consume it.
	GuardedClasses bool
	// InlineLiterals compares literals of at most this many code points in place first.
	InlineLiterals int
	// Observe reports the alternatives that the guards of choices skip (ObserveSkip).
	Observe bool
}

// Recovers reports whether the module has a Recover, which appends recovered errors.
func Recovers(m *core.Module) bool {
	found := false
	for _, r := range m.Rules {
		if r != nil {
			core.Walk(r.Body, func(n core.Node) {
				if _, ok := n.(*core.Recover); ok {
					found = true
				}
			})
		}
	}
	return found
}

// LocalRule reports whether Lower can lower the rule r: its captures can be variables of its
// function. Left-recursion leaders, whose growth the runtime owns, Operators Rules and rules with
// recovery keep runtime frames.
func LocalRule(r *core.Rule) bool {
	if r.Policy.Leader {
		return false
	}
	if _, ok := r.Body.(*core.OperatorsRule); ok {
		return false
	}
	ok := true
	core.Walk(r.Body, func(n core.Node) {
		switch n.(type) {
		case *core.Recover, *core.Nest, *core.OperatorsRule:
			ok = false
		}
	})
	return ok
}

// FramedBody reports whether LowerFrame can lower the body n: it has no Operators Rule, inlined
// call or terminal region, which the runtime or a local function matches.
func FramedBody(n core.Node) bool {
	ok := true
	core.Walk(n, func(n core.Node) {
		switch n.(type) {
		case *core.Nest, *core.OperatorsRule, *core.Terminal:
			ok = false
		}
	})
	return ok
}

// LowerFrame lowers the body n with the capture scope slots, building its value if build, to the
// framed function name of the rule r: the body of a left-recursion leader or of a rule that
// recovers, or a line of an Operators Rule of r. Analyze must have run on m.
func LowerFrame(m *core.Module, r *core.Rule, n core.Node, slots []core.Slot, build bool, name string, opts Options) (*Func, error) {
	if !FramedBody(n) {
		return nil, fmt.Errorf("a body of rule %s is not lowered to a framed function", r.Name)
	}
	f := &Func{Name: name, Rule: r, Fail: &Label{Name: "fail"}, Frame: true, Binds: n.Attrs().Eff.Set&core.EffBind != 0}
	l := &lowerer{m: m, opts: opts, f: f, used: map[*Label]bool{}, frame: true}
	l.stack = []*[]Stmt{&f.Body}
	core.Walk(n, func(n core.Node) {
		if _, ok := n.(*core.Cut); ok {
			l.localCuts = true
		}
	})
	if l.localCuts {
		f.Cut = l.decl("cut", TypeBool)
		l.set(f.Cut, &Read{R: RegCut})
		l.cut = f.Cut
	}
	v := l.node(n, newScope(slots), build, f.Fail)
	if !l.dead {
		f.Value = v
	}
	return f, nil
}

// Lower lowers the rule r of the module m, a LocalRule, to the function name. Analyze must have
// run on m.
func Lower(m *core.Module, r *core.Rule, name string, opts Options) (*Func, error) {
	if !LocalRule(r) {
		return nil, fmt.Errorf("rule %s is not lowered to a local function", r.Name)
	}
	f := &Func{Name: name, Rule: r, Fail: &Label{Name: "fail"},
		Start:  &Var{Name: "start", Type: TypeInt, Entry: true},
		Result: &Var{Name: "v", Type: TypeValue, Entry: true},
		Ctx:    &Var{Name: "c", Type: TypeCtx, Entry: true},
		Binds:  r.Body.Attrs().Eff.Set&core.EffBind != 0}
	l := &lowerer{m: m, opts: opts, f: f, used: map[*Label]bool{}}
	l.stack = []*[]Stmt{&f.Body}
	core.Walk(r.Body, func(n core.Node) {
		if _, ok := n.(*core.Cut); ok {
			l.localCuts = true
		}
	})
	s := newScope(r.Scope)
	body := l.node(r.Body, s, r.Want, f.Fail)
	if !l.dead {
		l.finish(r, s, body)
	}
	return f, nil
}

type lowerer struct {
	m    *core.Module
	opts Options
	f    *Func
	n    int
	// stack holds the statement lists being written, the innermost last.
	stack []*[]Stmt
	// dead reports that the current point is unreachable; used holds the labels exited to from
	// reachable points.
	dead bool
	used map[*Label]bool
	// localCuts reports that the rule's body cuts; cut is the flag of the innermost backtracking
	// point, nil where cuts are discarded (at the rule's top level and inside lookaheads).
	localCuts bool
	cut       *Var
	// guarded is the class that begins the alternative guardedAlt being lowered, which the guard
	// has read and accepted; guardedSize holds the size of its character. node forgets guarded at
	// any other node.
	guarded     *core.ClassNode
	guardedAlt  core.Node
	guardedSize *Var
	// frame reports a framed function.
	frame bool
}

// scope is a capture scope: its slots and the variables made for them.
type scope struct {
	slots []core.Slot
	vars  []*Var
}

func newScope(slots []core.Slot) *scope {
	return &scope{slots: slots, vars: make([]*Var, len(slots))}
}

func (l *lowerer) emit(s Stmt) {
	p := l.stack[len(l.stack)-1]
	*p = append(*p, s)
}

// open begins a block of the label lb in the current statement list; close ends it.
func (l *lowerer) open(lb *Label) *Block {
	b := &Block{Label: lb}
	l.emit(b)
	l.stack = append(l.stack, &b.Body)
	return b
}

func (l *lowerer) close(b *Block) {
	l.stack = l.stack[:len(l.stack)-1]
	if l.used[b.Label] {
		l.dead = false
	}
}

// loop begins a loop in the current statement list; endLoop ends it. The point after a loop is
// reachable: every loop can be left.
func (l *lowerer) loop(lp *Loop) {
	l.emit(lp)
	l.stack = append(l.stack, &lp.Body)
}

func (l *lowerer) endLoop() {
	l.stack = l.stack[:len(l.stack)-1]
	l.dead = false
}

func (l *lowerer) decl(prefix string, t Type) *Var {
	l.n++
	v := &Var{Name: fmt.Sprintf("%s%d", prefix, l.n), Type: t}
	l.f.Vars = append(l.f.Vars, v)
	return v
}

// shared returns the variable name shared by the whole function, making it at its first use.
func (l *lowerer) shared(name string, t Type) *Var {
	for _, v := range l.f.Vars {
		if v.Name == name {
			return v
		}
	}
	v := &Var{Name: name, Type: t}
	l.f.Vars = append(l.f.Vars, v)
	return v
}

func (l *lowerer) label() *Label {
	l.n++
	return &Label{Name: fmt.Sprintf("L%d", l.n)}
}

func (l *lowerer) ok() *Var { return l.shared("ok", TypeBool) }

// exit leaves the block or loop to; the point after it is unreachable.
func (l *lowerer) exit(to *Label) {
	if !l.dead {
		l.used[to] = true
	}
	l.emit(&Exit{To: to})
	l.dead = true
}

func (l *lowerer) cont(to *Label) {
	l.emit(&Continue{To: to})
	l.dead = true
}

// failIf leaves the block fail, after the statements pre, when cond holds.
func (l *lowerer) failIf(init *Set, cond Expr, fail *Label, pre ...Stmt) {
	if !l.dead {
		l.used[fail] = true
	}
	l.emit(&If{Init: init, Cond: cond, Then: append(pre, &Exit{To: fail})})
}

func (l *lowerer) set(dst LValue, x Expr) {
	l.emit(&Set{Dst: []LValue{dst}, X: []Expr{x}})
}

func (l *lowerer) step() {
	if l.opts.Work {
		l.emit(&Step{})
	}
}

// steps charges the steps of entering the node n (Attrs.Steps).
func (l *lowerer) steps(n core.Node) {
	for i := 0; i < n.Attrs().Steps; i++ {
		l.step()
	}
}

func use(v *Var) Expr { return &Use{V: v} }

var pos = &Read{R: RegPos}

// capVar returns the variable of the slot of s, making it at its first use.
func (l *lowerer) capVar(s *scope, slot int) *Var {
	if s.vars[slot] == nil {
		s.vars[slot] = l.decl("k", TypeValue)
	}
	return s.vars[slot]
}

// mark is the state a backtracking point restores: in a framed function, the mark state of the
// whole parser state.
type mark struct {
	pos, rec, env *Var
	saves         [][2]*Var // capture variable, saved value
	state         *Var
}

// at is the position of the mark.
func (m mark) at() Expr {
	if m.state != nil {
		return &MarkPos{M: m.state}
	}
	return use(m.pos)
}

// mark saves the state before n; with saves, also the capture slots of s that n may write.
func (l *lowerer) mark(s *scope, n core.Node, saves bool) mark {
	if l.frame {
		m := mark{state: l.decl("x", TypeMark)}
		l.set(m.state, &MarkState{})
		return m
	}
	m := mark{pos: l.decl("x", TypeInt)}
	if l.opts.Recovers {
		m.rec = l.decl("x", TypeInt)
		l.emit(&Set{Dst: []LValue{m.pos, m.rec}, X: []Expr{pos, &RecLen{}}})
	} else {
		l.set(m.pos, pos)
	}
	eff := n.Attrs().Eff
	if eff.Set&core.EffBind != 0 {
		m.env = l.decl("x", TypeEnv)
		l.set(m.env, &Read{R: RegEnv})
	}
	if saves {
		for _, slot := range eff.Slots {
			k := l.capVar(s, slot)
			sv := l.decl("x", TypeValue)
			l.step()
			l.set(sv, use(k))
			m.saves = append(m.saves, [2]*Var{k, sv})
		}
	}
	return m
}

// reset restores the state saved by m.
func (l *lowerer) reset(m mark) {
	if m.state != nil {
		l.emit(&Reset{M: use(m.state)})
		return
	}
	if m.rec == nil {
		l.set(RegPos, use(m.pos))
	} else {
		l.emit(&Restore{Pos: use(m.pos), Rec: use(m.rec)})
	}
	if m.env != nil {
		l.set(RegEnv, use(m.env))
	}
	for _, kv := range m.saves {
		l.step()
		l.set(kv[0], use(kv[1]))
	}
}

// cutNode lowers a backtracking point's body with a cut flag of its own, if the rule cuts.
func (l *lowerer) cutNode(n core.Node, s *scope, build bool, fail *Label) (Expr, *Var) {
	if !l.localCuts {
		return l.node(n, s, build, fail), nil
	}
	prev := l.cut
	cut := l.decl("cut", TypeBool)
	l.set(cut, &Bool{})
	l.cut = cut
	v := l.node(n, s, build, fail)
	l.cut = prev
	return v, cut
}

// cutFail fails, after the statements cleanup, when the body of a backtracking point cut.
func (l *lowerer) cutFail(cut *Var, fail *Label, cleanup ...Stmt) {
	if cut != nil {
		l.failIf(nil, use(cut), fail, cleanup...)
	}
}

var anchorExps = map[core.AnchorKind]core.ExpID{core.BeginInput: 1, core.EndInput: 2, core.BeginLine: 3, core.EndLine: 4}

// node lowers n, leaving fail when it fails, and returns its value: nil unless build.
func (l *lowerer) node(n core.Node, s *scope, build bool, fail *Label) Expr {
	if l.dead {
		return &Nil{}
	}
	if l.guarded != nil && n != l.guarded && n != l.guardedAlt {
		// Only the first node the alternative matches may consume the guard's character.
		l.guarded = nil
	}
	l.steps(n)
	switch n := n.(type) {
	case *core.Terminal:
		l.failIf(nil, &Not{X: &SkipTerminal{T: n}}, fail)
		return &Nil{}
	case *core.Lit:
		return l.lit(n, build, fail)
	case *core.Missing:
		l.steps(n.Lit)
		return l.lit(n.Lit, build, fail)
	case *core.ClassNode:
		if n == l.guarded {
			return l.guardedClass(build)
		}
		if l.opts.ClassTables && len(n.Class.Ranges) > 4 {
			if !build {
				l.failIf(nil, &Not{X: &MatchClass{Class: n.Class}}, fail)
				return &Nil{}
			}
			start := l.decl("x", TypeInt)
			l.set(start, pos)
			l.failIf(nil, &Not{X: &MatchClass{Class: n.Class}}, fail)
			v := l.decl("v", TypeValue)
			l.set(v, &NewMatch{Start: use(start), Text: &Text{Start: use(start)}})
			return use(v)
		}
		ch, size, ok := l.shared("ch", TypeRune), l.shared("size", TypeInt), l.ok()
		l.emit(&Peek{Ch: ch, Size: size, OK: ok})
		l.failIf(nil, &Or{X: &Not{X: use(ok)}, Y: &Not{X: &InClass{Class: n.Class, Ch: ch}}}, fail,
			&Expect{At: pos, Exp: n.Class.Exp})
		return l.single(size, build)
	case *core.Any:
		size, ok := l.shared("size", TypeInt), l.ok()
		l.emit(&Peek{Size: size, OK: ok})
		l.failIf(nil, &Not{X: use(ok)}, fail, &Expect{At: pos, Exp: 0})
		return l.single(size, build)
	case *core.Call:
		ok := l.ok()
		call := &CallRule{Rule: n.Rule, Level: n.Level}
		if !build {
			l.failIf(&Set{Dst: []LValue{&Discard{}, ok}, X: []Expr{call}}, &Not{X: use(ok)}, fail)
			return &Nil{}
		}
		v := l.decl("v", TypeValue)
		l.failIf(&Set{Dst: []LValue{v, ok}, X: []Expr{call}}, &Not{X: use(ok)}, fail)
		return use(v)
	case *core.Seq:
		return l.seq(n, s, build, fail)
	case *core.ClassRun:
		l.failIf(nil, &Not{X: &SkipClasses{Run: n}}, fail)
		return &Nil{}
	case *core.Choice:
		return l.choice(n, s, build, fail)
	case *core.Stream:
		l.steps(n.Repeat)
		return l.repeat(n.Repeat, s, build, fail)
	case *core.Repeat:
		return l.repeat(n, s, build, fail)
	case *core.Scan:
		return l.scan(n, fail)
	case *core.Opt:
		m := l.mark(s, n.Body, true)
		var v *Var
		if build {
			v = l.decl("v", TypeValue)
		}
		f, done := l.label(), l.label()
		bd := l.open(done)
		bf := l.open(f)
		x, cut := l.cutNode(n.Body, s, build, f)
		if build {
			l.set(v, l.shapeOne(n.Body, x, m.at(), false))
		}
		if l.used[f] {
			l.exit(done)
			l.close(bf)
			l.reset(m)
			l.cutFail(cut, fail)
			if build {
				l.set(v, &Nil{})
			}
			l.close(bd)
		} else {
			l.close(bf)
			l.close(bd)
		}
		if !build {
			return &Nil{}
		}
		return use(v)
	case *core.Look:
		if n.Neg {
			return l.not(n, s, fail)
		}
		start := l.decl("x", TypeInt)
		l.set(start, pos)
		l.emit(&Silent{Delta: 1})
		f, done := l.label(), l.label()
		bd := l.open(done)
		bf := l.open(f)
		prev := l.cut
		l.cut = nil
		l.node(n.Body, s, n.Body.Attrs().Want, f)
		l.cut = prev
		l.emit(&Silent{Delta: -1})
		l.set(RegPos, use(start))
		if l.used[f] {
			l.exit(done)
			l.close(bf)
			l.emit(&Silent{Delta: -1})
			l.set(RegPos, use(start))
			l.exit(fail)
			l.close(bd)
		} else {
			l.close(bf)
			l.close(bd)
		}
		return &Nil{}
	case *core.Text:
		var start *Var
		if build {
			start = l.decl("x", TypeInt)
			l.set(start, pos)
		}
		l.node(n.Body, s, false, fail)
		if !build {
			return &Nil{}
		}
		v := l.decl("v", TypeValue)
		l.set(v, &NewMatch{Start: use(start), Text: &Text{Start: use(start)}})
		return use(v)
	case *core.Drop:
		l.node(n.Body, s, false, fail)
		return &Nil{}
	case *core.Capture:
		if l.frame {
			return l.frameCapture(n, s, build, fail)
		}
		k := l.capVar(s, n.Slot)
		var v Expr
		if n.Project != "" {
			v = l.projectRepeat(n.Body.(*core.Repeat), fail)
		} else {
			start := l.decl("x", TypeInt)
			l.set(start, pos)
			v = l.shapeOne(n.Body, l.node(n.Body, s, true, fail), use(start), true)
		}
		l.set(k, v)
		if !build {
			return &Nil{}
		}
		return v
	case *core.Succeed:
		return &Nil{}
	case *core.FailNode:
		l.exit(fail)
		return &Nil{}
	case *core.Cut:
		if l.cut != nil {
			l.set(l.cut, &Bool{B: true})
		}
		return &Nil{}
	case *core.Anchor:
		l.failIf(nil, &Not{X: &AtAnchor{Kind: n.Kind}}, fail, &Expect{At: pos, Exp: anchorExps[n.Kind]})
		return &Nil{}
	case *core.Pred:
		caps := l.termCaps(n.Term, s)
		l.failIf(nil, &Not{X: &Pred{Node: n, Caps: caps}}, fail)
		return &Nil{}
	case *core.BindNode:
		caps := l.termCaps(n.Term, s)
		l.failIf(nil, &Not{X: &Bind{Node: n, Caps: caps}}, fail)
		return &Nil{}
	case *core.Label:
		return l.label_(n, s, build, fail)
	case *core.Recover:
		if l.frame {
			return l.recover(n, s, build, fail)
		}
	}
	panic(fmt.Sprintf("flow: cannot lower %T", n))
}

// lit lowers a literal.
func (l *lowerer) lit(n *core.Lit, build bool, fail *Label) Expr {
	var start *Var
	if build {
		start = l.decl("x", TypeInt)
		l.set(start, pos)
	}
	ok := l.ok()
	match := &Set{Dst: []LValue{&Discard{}, ok}, X: []Expr{&MatchLit{Lit: n}}}
	if k := len([]rune(n.Text)); k > 0 && k <= l.opts.InlineLiterals {
		// The code points compared in place; MatchLit records the expectation when they differ.
		if !l.dead {
			l.used[fail] = true
		}
		l.emit(&If{Cond: &HasLit{Lit: n}, Then: []Stmt{&Advance{N: &Int{N: k}}},
			Else: []Stmt{&If{Init: match, Cond: &Not{X: use(ok)}, Then: []Stmt{&Exit{To: fail}}}}})
	} else {
		l.failIf(match, &Not{X: use(ok)}, fail)
	}
	if !build {
		return &Nil{}
	}
	v := l.decl("v", TypeValue)
	var text Expr = &Str{S: n.Text}
	if strings.ContainsRune(n.Text, '�') {
		text = &LiteralText{Start: use(start), Lit: n.Text}
	}
	l.set(v, &NewMatch{Start: use(start), Text: text})
	return use(v)
}

// single consumes the character of the given size, after a successful peek.
func (l *lowerer) single(size *Var, build bool) Expr {
	if !build {
		l.emit(&Advance{N: use(size)})
		return &Nil{}
	}
	start, v := l.decl("x", TypeInt), l.decl("v", TypeValue)
	l.set(start, pos)
	l.emit(&Advance{N: use(size)})
	l.set(v, &NewMatch{Start: use(start), Text: &Text{Start: use(start)}})
	return use(v)
}

// guardedClass consumes the character the guard read for the class l.guarded.
func (l *lowerer) guardedClass(build bool) Expr {
	if !build {
		l.emit(&Advance{N: use(l.guardedSize)})
		return &Nil{}
	}
	start, v := l.decl("x", TypeInt), l.decl("v", TypeValue)
	l.set(start, pos)
	l.emit(&Advance{N: use(l.guardedSize)})
	l.set(v, &NewMatch{Start: use(start), Text: &Text{Start: use(start)}})
	return use(v)
}

func (l *lowerer) seq(n *core.Seq, s *scope, build bool, fail *Label) Expr {
	var start, kids *Var
	if build {
		start, kids = l.decl("x", TypeInt), l.decl("x", TypeVals)
		l.set(start, pos)
		l.set(kids, &NewVals{N: n.Attrs().Shape.K})
	}
	// The value is the tuple node of the elements, flattened through nested sequences.
	for _, it := range n.Items {
		v := l.node(it, s, build, fail)
		if !build {
			continue
		}
		switch sh := it.Attrs().Shape; {
		case sh.Elems:
			l.emit(&Append{Vals: kids, X: v, Spread: true})
		case sh.K == 1:
			l.emit(&Append{Vals: kids, X: v})
		}
	}
	if !build {
		return &Nil{}
	}
	v := l.decl("v", TypeValue)
	l.set(v, &NewNode{Start: use(start), Kids: use(kids)})
	return use(v)
}

// leadingClass returns the class that begins the alternative body n, also inside a Terminal.
func leadingClass(n core.Node) *core.ClassNode {
	if t, ok := n.(*core.Terminal); ok {
		n = t.Body
	}
	if s, ok := n.(*core.Seq); ok && len(s.Items) > 0 {
		n = s.Items[0]
	}
	c, _ := n.(*core.ClassNode)
	return c
}

func (l *lowerer) choice(n *core.Choice, s *scope, build bool, fail *Label) Expr {
	m := l.mark(s, n, true)
	var v *Var
	if build {
		v = l.decl("v", TypeValue)
	}
	done := l.label()
	// The guards keep the size of the character they read when an alternative begins with a
	// class.
	var size *Var
	if l.opts.GuardedClasses {
		for _, a := range n.Alts {
			if leadingClass(a.Body) != nil {
				size = l.decl("x", TypeInt)
				break
			}
		}
	}
	bd := l.open(done)
	var ch, more *Var
	for _, a := range n.Alts {
		if l.dead {
			break // the alternatives after one that cannot fail are never tried
		}
		var bs *Block
		var guarded *core.ClassNode
		if a.Guard != nil {
			// The alternative is skipped when the next character cannot begin it.
			if ch == nil {
				ch, more = l.decl("x", TypeRune), l.decl("x", TypeBool)
				l.emit(&Peek{Ch: ch, Size: size, OK: more})
			}
			if size != nil && a.GuardedClass {
				guarded = leadingClass(a.Body)
			}
			skip := l.label()
			bs = l.open(skip)
			pre := []Stmt{&Expect{At: pos, Exp: a.Guard.Exp}}
			if l.opts.Observe {
				pre = append(pre, &ObserveSkip{Alt: a.Body})
			}
			l.failIf(nil, &GuardFails{Guard: a.Guard, Ch: ch, More: more}, skip, pre...)
		}
		f := l.label()
		bf := l.open(f)
		l.guarded, l.guardedAlt, l.guardedSize = guarded, a.Body, size
		x, cut := l.cutNode(a.Body, s, build, f)
		if build {
			l.set(v, l.shapeOne(a.Body, x, m.at(), false))
		}
		l.exit(done)
		l.close(bf)
		l.reset(m)
		l.cutFail(cut, fail)
		if bs != nil {
			l.close(bs)
		}
	}
	l.exit(fail)
	l.close(bd)
	if !build {
		return &Nil{}
	}
	return use(v)
}

// shapeOne converts the value x of n, matched from start, to its value where it takes one
// element; with capture, where it is captured.
func (l *lowerer) shapeOne(n core.Node, x Expr, start Expr, capture bool) Expr {
	sh := n.Attrs().Shape
	if sh.Mode == core.SinglePass && !sh.Elems {
		return x
	}
	v := l.decl("v", TypeValue)
	if capture {
		l.set(v, &ShapeCapture{Shape: sh, X: x, Start: start})
	} else {
		l.emit(&Set{Dst: []LValue{v, &Discard{}}, X: []Expr{&ShapeSingle{Shape: sh, X: x, Start: start}}})
	}
	return use(v)
}

// not lowers a negative lookahead.
func (l *lowerer) not(n *core.Look, s *scope, fail *Label) Expr {
	m := l.mark(s, n.Body, true)
	l.emit(&Silent{Delta: 1})
	f := l.label()
	bf := l.open(f)
	prev := l.cut
	l.cut = nil
	l.node(n.Body, s, false, f)
	l.cut = prev
	l.emit(&Silent{Delta: -1})
	l.reset(m)
	l.exit(fail)
	l.close(bf)
	l.emit(&Silent{Delta: -1})
	l.reset(m)
	return &Nil{}
}

func (l *lowerer) repeat(n *core.Repeat, s *scope, build bool, fail *Label) Expr {
	if l.frame {
		return l.frameRepeat(n, s, build, fail)
	}
	// An element with captures has its own scope per iteration.
	own := n.Scope != nil
	es := s
	if own {
		es = newScope(n.Scope)
	}
	var start, base *Var
	if build {
		start, base = l.decl("x", TypeInt), l.decl("x", TypeInt)
		l.emit(&Set{Dst: []LValue{start, base}, X: []Expr{pos, &KidsLen{}}})
	}
	count := l.decl("x", TypeInt)
	l.set(count, &Int{})
	lp := l.newLoop(n.Max)
	m := l.mark(s, n.Body, !own)
	if own {
		l.clearScope(es, n.Body)
	}
	f := l.label()
	bf := l.open(f)
	v, cut := l.cutNode(n.Body, es, build, f)
	if build {
		sv := l.decl("v", TypeValue)
		l.set(sv, &ScopeValue{Shape: n.Body.Attrs().Shape, X: v, Start: use(m.pos)})
		v = use(sv)
	}
	if own && build {
		attached := l.decl("v", TypeValue)
		x := &AttachCaptures{X: v, Names: &Names{List: core.SlotNames(es.slots)}, Start: use(m.pos)}
		x.Vals = l.scopeVals(es)
		l.set(attached, x)
		v = use(attached)
	}
	l.emit(&Inc{V: count})
	if build {
		l.emit(&PushKid{X: v})
	}
	l.emit(&If{Cond: &Cmp{Op: "==", L: pos, R: use(m.pos)}, Then: []Stmt{&Exit{To: lp.Label}}})
	if l.used[f] {
		l.cont(lp.Label)
		l.close(bf)
		l.reset(m)
		if build {
			l.cutFail(cut, fail, &DropKids{Base: use(base)})
		} else {
			l.cutFail(cut, fail)
		}
		l.exitLoop(lp)
	} else {
		l.close(bf)
	}
	l.endLoop()
	if n.Min > 0 {
		cond := &Cmp{Op: "<", L: use(count), R: &Int{N: n.Min}}
		if build {
			l.failIf(nil, cond, fail, &DropKids{Base: use(base)})
		} else {
			l.failIf(nil, cond, fail)
		}
	}
	if !build {
		return &Nil{}
	}
	list := l.decl("v", TypeValue)
	l.set(list, &NewNode{List: true, Start: use(start), Kids: &Kids{Base: use(base)}})
	return use(list)
}

// newLoop begins a loop of at most max iterations (unbounded if max < 0).
func (l *lowerer) newLoop(max int) *Loop {
	lp := &Loop{Label: &Label{}, Max: max}
	if max >= 0 {
		lp.Counter = l.decl("x", TypeInt)
	}
	l.loop(lp)
	return lp
}

// exitLoop leaves the loop lp from its body.
func (l *lowerer) exitLoop(lp *Loop) {
	l.emit(&Exit{To: lp.Label})
	l.dead = true
}

// clearScope sets the capture slots of the element n in its own scope es to nil.
func (l *lowerer) clearScope(es *scope, n core.Node) {
	for _, slot := range n.Attrs().Eff.Slots {
		l.step()
		l.set(l.capVar(es, slot), &Nil{})
	}
}

// scopeVals returns the variables of the slots of es, in slot order.
func (l *lowerer) scopeVals(es *scope) []*Var {
	vals := make([]*Var, len(es.slots))
	for i := range es.slots {
		vals[i] = l.capVar(es, i)
	}
	return vals
}

// scan lowers a value-free repetition of one character.
func (l *lowerer) scan(n *core.Scan, fail *Label) Expr {
	var ch *Var
	if n.Class != nil {
		ch = l.shared("ch", TypeRune)
	}
	size, ok := l.shared("size", TypeInt), l.ok()
	var count *Var
	if n.Max >= 0 || n.Min > 0 {
		count = l.decl("x", TypeInt)
		l.set(count, &Int{})
	}
	lp := &Loop{Label: &Label{}, Max: -1}
	if n.Max >= 0 {
		lp.While = &Cmp{Op: "<", L: use(count), R: &Int{N: n.Max}}
	}
	l.loop(lp)
	l.emit(&Peek{Ch: ch, Size: size, OK: ok})
	var stop Expr = &Not{X: use(ok)}
	exp := core.ExpID(0)
	if n.Class != nil {
		stop = &Or{X: stop, Y: &Not{X: &InClass{Class: n.Class, Ch: ch}}}
		exp = n.Class.Exp
	}
	l.emit(&If{Cond: stop, Then: []Stmt{&Expect{At: pos, Exp: exp}, &Exit{To: lp.Label}}})
	l.emit(&Advance{N: use(size)})
	if count != nil {
		l.emit(&Inc{V: count})
	}
	l.endLoop()
	if n.Min > 0 {
		l.failIf(nil, &Cmp{Op: "<", L: use(count), R: &Int{N: n.Min}}, fail)
	}
	return &Nil{}
}

// projectRepeat lowers a repetition that gathers the values of the capture Project of its
// elements.
func (l *lowerer) projectRepeat(n *core.Repeat, fail *Label) Expr {
	es := newScope(n.Scope)
	start, base, count := l.decl("x", TypeInt), l.decl("x", TypeInt), l.decl("x", TypeInt)
	l.emit(&Set{Dst: []LValue{start, base, count}, X: []Expr{pos, &KidsLen{}, &Int{}}})
	lp := l.newLoop(n.Max)
	m := l.mark(es, n.Body, false)
	l.clearScope(es, n.Body)
	f := l.label()
	bf := l.open(f)
	_, cut := l.cutNode(n.Body, es, false, f)
	field := -1
	for i, sl := range es.slots {
		if sl.Name == n.Project {
			field = i
		}
	}
	fv := l.capVar(es, field)
	l.emit(&Inc{V: count})
	l.emit(&PushKid{X: use(fv)})
	l.emit(&If{Cond: &Cmp{Op: "==", L: pos, R: use(m.pos)}, Then: []Stmt{&Exit{To: lp.Label}}})
	if l.used[f] {
		l.cont(lp.Label)
		l.close(bf)
		l.reset(m)
		l.cutFail(cut, fail, &DropKids{Base: use(base)})
		l.exitLoop(lp)
	} else {
		l.close(bf)
	}
	l.endLoop()
	if n.Min > 0 {
		l.failIf(nil, &Cmp{Op: "<", L: use(count), R: &Int{N: n.Min}}, fail, &DropKids{Base: use(base)})
	}
	v := l.decl("v", TypeValue)
	l.set(v, &NewNode{List: true, Start: use(start), Kids: &Kids{Base: use(base)}})
	return use(v)
}

// termCaps returns the captures of the scope s for a term, making the variables of the slots it
// refers to; in a framed function, the term reads the current frame.
func (l *lowerer) termCaps(t core.Term, s *scope) Captures {
	if l.frame {
		return Captures{Names: core.SlotNames(s.slots)}
	}
	core.WalkTerm(t, func(t core.Term) {
		if r, ok := t.(*core.SlotRef); ok {
			l.capVar(s, r.Slot)
		}
	})
	return Captures{Names: core.SlotNames(s.slots), Vars: append([]*Var(nil), s.vars...)}
}

// label_ lowers @error and @diagnostic: the expectations recorded inside are isolated, so that
// the label changes only those of a failed body.
func (l *lowerer) label_(n *core.Label, s *scope, build bool, fail *Label) Expr {
	mk, far, exps := l.decl("x", TypeExpMark), l.decl("x", TypeInt), l.decl("x", TypeExpIDs)
	l.set(mk, &Isolate{})
	f, done := l.label(), l.label()
	bd := l.open(done)
	bf := l.open(f)
	v := l.node(n.Body, s, build, f)
	unisolate := &Set{Dst: []LValue{far, exps}, X: []Expr{&Unisolate{Mark: use(mk)}}}
	l.emit(unisolate)
	l.emit(&MergeExpected{Far: use(far), Exps: use(exps)})
	if l.used[f] {
		l.exit(done)
		l.close(bf)
		l.emit(&Set{Dst: []LValue{far, exps}, X: []Expr{&Unisolate{Mark: use(mk)}}})
		if n.Diagnostic {
			l.emit(&MergeRenamed{Far: use(far), Exps: use(exps), Exp: n.Exp})
		} else {
			l.emit(&Expect{At: use(far), Exp: n.Exp, Message: true})
		}
		l.exit(fail)
		l.close(bd)
	} else {
		l.close(bf)
		l.close(bd)
	}
	return v
}

// finish makes the rule's value from the body's value body and the captures s, leaving it in
// the Result variable.
func (l *lowerer) finish(r *core.Rule, s *scope, body Expr) {
	f := l.f
	var owned Expr
	if r.Finish.NodeType != "" && (r.Finish.Kind == core.FinishSplice || r.Finish.Kind == core.FinishScope) {
		bv := l.shared("bv", TypeValue)
		l.set(bv, &FreshNode{X: body})
		owned = use(bv)
	}
	switch r.Finish.Kind {
	case core.FinishNone:
		l.set(f.Result, &Nil{})
	case core.FinishAction:
		caps := Captures{Names: core.SlotNames(s.slots), Vars: l.scopeVals(s)}
		l.set(f.Ctx, &UseCtx{Start: use(f.Start)})
		l.emit(&SetWhere{Name: r.Where})
		l.set(f.Result, &Eval{Term: r.Finish.Action, C: f.Ctx, Caps: caps})
		switch r.Finish.Action.(type) {
		case *core.New, *core.Convert:
			// A struct made with the rule's range; structs made inside it are dropped.
			nested := 0
			core.WalkTerm(r.Finish.Action, func(t core.Term) {
				switch t.(type) {
				case *core.New, *core.Convert:
					nested++
				}
			})
			if nested > 1 {
				l.emit(&DropCreated{C: use(f.Ctx)})
			}
		default:
			l.set(f.Result, &CtxFinish{C: f.Ctx, X: use(f.Result)})
		}
	case core.FinishSplice:
		l.set(f.Result, body)
		l.emit(&Unfresh{X: use(f.Result)})
	case core.FinishScope:
		l.set(f.Result, &ScopeValue{Shape: r.Finish.Shape, X: body, Start: use(f.Start)})
		if len(s.slots) > 0 {
			x := &AttachCaptures{X: use(f.Result), Names: &Names{List: core.SlotNames(s.slots)}, Start: use(f.Start)}
			x.Vals = l.scopeVals(s)
			l.set(f.Result, x)
		}
		l.emit(&Unfresh{X: use(f.Result)})
	}
	if r.Finish.NodeType != "" {
		l.set(f.Result, &ConvertResult{Rule: r.ID, X: use(f.Result), Owned: owned})
	}
}

// frameCapture lowers a capture in a framed function: it sets the slot of the current frame.
func (l *lowerer) frameCapture(n *core.Capture, s *scope, build bool, fail *Label) Expr {
	var v Expr
	if n.Project != "" {
		v = l.frameRepeat(n.Body.(*core.Repeat), s, true, fail)
	} else {
		start := l.decl("x", TypeInt)
		l.set(start, pos)
		v = l.shapeOne(n.Body, l.node(n.Body, s, true, fail), use(start), true)
	}
	l.emit(&SetCapture{Slot: n.Slot, X: v})
	if !build {
		return &Nil{}
	}
	return v
}

// frameRepeat lowers a repetition in a framed function. An element with captures of its own runs
// under a new frame, which a projected repetition (Project) makes once and clears for each
// element.
func (l *lowerer) frameRepeat(n *core.Repeat, s *scope, build bool, fail *Label) Expr {
	field := n.Project
	own := n.Scope != nil
	es := s
	var start, base, frame, prev *Var
	var names *Names
	if build {
		start, base = l.decl("x", TypeInt), l.decl("x", TypeInt)
		l.emit(&Set{Dst: []LValue{start, base}, X: []Expr{pos, &KidsLen{}}})
	}
	if own {
		es = newScope(n.Scope)
		frame, prev = l.decl("x", TypeFrame), l.decl("x", TypeFrame)
		names = &Names{List: core.SlotNames(es.slots), Declared: true}
		if field != "" {
			l.set(frame, &NewFrame{N: names})
		}
	}
	count := l.decl("x", TypeInt)
	l.set(count, &Int{})
	lp := l.newLoop(n.Max)
	if field != "" {
		l.emit(&ClearVals{F: frame})
	}
	m := l.mark(s, n.Body, true)
	frameReg := &Read{R: RegFrame}
	if own {
		l.set(prev, frameReg)
		if field == "" {
			l.set(frame, &NewFrame{N: names})
		}
		l.set(RegFrame, use(frame))
	}
	f := l.label()
	bf := l.open(f)
	v, cut := l.cutNode(n.Body, es, build && field == "", f)
	if build && field == "" {
		sv := l.decl("v", TypeValue)
		l.set(sv, &ScopeValue{Shape: n.Body.Attrs().Shape, X: v, Start: m.at()})
		v = use(sv)
	}
	if own {
		l.set(RegFrame, use(prev))
		if build {
			if field != "" {
				v = &FrameVal{F: frame, Slot: slotOf(es.slots, field)}
			} else {
				attached := l.decl("v", TypeValue)
				l.set(attached, &AttachCaptures{X: v, Names: names, Frame: frame, Start: m.at()})
				v = use(attached)
			}
		}
		l.emit(&DeclareNames{N: names})
		if field == "" {
			l.emit(&DropTrail{M: use(m.state)})
			l.emit(&FreeFrame{F: frame})
		}
	}
	l.emit(&Inc{V: count})
	if build {
		l.emit(&PushKid{X: v})
	}
	l.emit(&If{Cond: &Cmp{Op: "==", L: pos, R: m.at()}, Then: []Stmt{&Exit{To: lp.Label}}})
	if l.used[f] {
		l.cont(lp.Label)
		l.close(bf)
		if own {
			l.set(RegFrame, use(prev))
		}
		l.reset(m)
		if build {
			l.cutFail(cut, fail, &DropKids{Base: use(base)})
		} else {
			l.cutFail(cut, fail)
		}
		l.exitLoop(lp)
	} else {
		l.close(bf)
	}
	l.endLoop()
	if n.Min > 0 {
		cond := &Cmp{Op: "<", L: use(count), R: &Int{N: n.Min}}
		if build {
			l.failIf(nil, cond, fail, &DropKids{Base: use(base)})
		} else {
			l.failIf(nil, cond, fail)
		}
	}
	if !build {
		return &Nil{}
	}
	list := l.decl("v", TypeValue)
	l.set(list, &NewNode{List: true, Start: use(start), Kids: &Kids{Base: use(base)}})
	return use(list)
}

// slotOf returns the index of the slot name among slots.
func slotOf(slots []core.Slot, name string) int {
	for i, sl := range slots {
		if sl.Name == name {
			return i
		}
	}
	panic(fmt.Sprintf("flow: no capture slot %s", name))
}

// recover lowers #recover in a framed function. The expectations recorded inside are isolated;
// when the body fails, the parser state goes back to the mark, the skip expression consumes at
// least one character, and the error is recorded and is the value.
func (l *lowerer) recover(n *core.Recover, s *scope, build bool, fail *Label) Expr {
	l.emit(&Comment{Text: "Recovery inlined under the runtime-owned frame."})
	m := l.mark(s, n, true)
	mk, far, exps := l.decl("x", TypeExpMark), l.decl("x", TypeInt), l.decl("x", TypeExpIDs)
	l.set(mk, &Isolate{})
	f, done := l.label(), l.label()
	var v *Var
	if build {
		v = l.decl("v", TypeValue)
	}
	bd := l.open(done)
	bf := l.open(f)
	inner := l.node(n.Body, s, build, f)
	if build {
		l.set(v, l.shapeOne(n.Body, inner, m.at(), true))
	}
	l.emit(&Set{Dst: []LValue{far, exps}, X: []Expr{&Unisolate{Mark: use(mk)}}})
	l.emit(&MergeExpected{Far: use(far), Exps: use(exps)})
	if l.used[f] {
		l.exit(done)
		l.close(bf)
		l.emit(&Set{Dst: []LValue{far, exps}, X: []Expr{&Unisolate{Mark: use(mk)}}})
		l.set(exps, &Keep{Exps: use(exps)})
		l.reset(m)
		skipFail := l.label()
		bs := l.open(skipFail)
		l.node(n.Skip, s, false, skipFail)
		l.failIf(nil, &Cmp{Op: "==", L: pos, R: m.at()}, skipFail)
		err := l.decl("x", TypeError)
		l.set(err, &MakeError{Far: use(far), Exps: use(exps)})
		l.emit(&AppendError{X: use(err)})
		if build {
			l.set(v, &ErrorValue{Start: m.at(), X: use(err)})
		}
		l.exit(done)
		l.close(bs)
		l.reset(m)
		l.emit(&MergeExpected{Far: use(far), Exps: use(exps)})
		l.exit(fail)
		l.close(bd)
	} else {
		l.close(bf)
		l.close(bd)
	}
	if !build {
		return &Nil{}
	}
	return use(v)
}
