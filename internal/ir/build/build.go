// Package build builds Core IR (internal/ir/core) from a compiled program.
//
// Core makes the module of the program variant whose rule bodies build values where the
// compiled rules do: a rule body builds its value unless the rule is lean (its action reads only
// captures, or it is a value-free twin); a capture, and the sequence, choice, optional and
// repetition around a built expression, build theirs; text (#atomic), drop (-), negative
// lookaheads and recovery skips build none; a positive lookahead builds only when it contains
// captures; the element of a projected repetition builds none. A call where no value is wanted
// calls the rule's value-free twin, if it has one.
//
// Capture slots follow the frame layouts of the compiled rules: the scope of a rule body, a
// repetition element with captures of its own (elementScoped) or an Operators Rule line lists
// every capture written in it, including unreachable ones, in the order they are first met
// (coreLayout).
//
// A type-checked program gives the module its checked types (compile.ValueRecord): those of the rules'
// values, of the capture slots, of every term and of the named types and variables. The
// expectations of the module begin with the fixed ones of the runtime (compile.FixedDescs), so that Any
// and the anchors, which name none, record those of the same index. A #stream repetition at the
// top of a start rule is a Stream; elsewhere it streams nothing and is built as its repetition.
package build

import (
	"fmt"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/ir/core"
	"github.com/ornew/pego/internal/value"
)

// CoreFrom builds the Core IR of the program: its rules and then its value-free twins, each at
// the position of its ID. Shapes are those of the value model (internal/compile/value_shape.go);
// Analyze has not run. If origin is not nil, it records the expression each node is built from
// (the innermost one, for nodes built from a chain of expressions).
func CoreFrom(prog *compile.Program, origin map[core.Node]grammar.Expr) (*core.Module, error) {
	return CoreProjecting(prog, origin, true)
}

// CoreProjecting is CoreFrom; if project is false, no repetition gathers the values of a
// capture (compile.RuleProjections is ignored), the shape of the values the recognizer's table
// writes its actions for (the Go generator's projections).
func CoreProjecting(prog *compile.Program, origin map[core.Node]grammar.Expr, project bool) (*core.Module, error) {
	all := append(append([]*compile.Rule(nil), prog.RuleList...), prog.Twins...)
	b := &coreBuilder{prog: prog, m: &core.Module{Rules: make([]*core.Rule, len(all))}, origin: origin,
		exps: map[string]core.ExpID{}, classes: map[string]*core.Class{}, starts: map[*compile.Rule]bool{}}
	b.noProject = !project
	b.rec = prog.ValueRecord()
	for _, d := range compile.FixedDescs {
		b.exp(d)
	}
	for _, name := range prog.StartRules {
		r := prog.ByName[name]
		b.starts[r] = true
		b.m.Starts = append(b.m.Starts, r.ID)
	}
	for i, r := range all {
		if r.ID != i {
			return nil, fmt.Errorf("internal error: rule %s has ID %d at position %d", r.Name, r.ID, i)
		}
		cr, err := b.rule(r)
		if err != nil {
			return nil, fmt.Errorf("rule %s: %w", r.Name, err)
		}
		b.m.Rules[i] = cr
	}
	for _, r := range all {
		if r.Twin != nil {
			b.m.Rules[r.ID].Twin = b.m.Rules[r.Twin.ID]
		}
	}
	if b.rec != nil {
		b.m.Typed = true
		b.m.Types = map[string]core.Type{}
		k := &compile.TypeResolver{Prog: prog}
		for name := range prog.Types {
			b.m.Types[name] = k.ResolveNamed(name, grammar.Pos{})
		}
	}
	if prog.Vars != nil {
		for _, d := range prog.Vars.List {
			b.m.Vars = append(b.m.Vars, &core.VarDecl{Name: d.Name, Type: d.Typ, Init: d.Init})
		}
	}
	core.CallGraph(b.m)
	return b.m, nil
}

type coreBuilder struct {
	prog *compile.Program
	m    *core.Module
	cur  *compile.Rule
	// noProject builds no projected repetition: proj stays empty.
	noProject bool
	// proj holds the projected repetition captures of the rule being built (ruleProjections).
	proj map[string]string
	// final is the action whose value is the result of the rule or line being built.
	final grammar.Term
	// origin, if not nil, records the expression each node is built from.
	origin map[core.Node]grammar.Expr
	// rec holds the checked types of the program, nil if it has none.
	rec *compile.ValueRecord
	// exps and classes intern the expectations and classes of the module.
	exps    map[string]core.ExpID
	classes map[string]*core.Class
	// starts holds the start rules.
	starts map[*compile.Rule]bool
	// streamTop holds the expressions of the rule being built where #stream makes a Stream: its
	// body, the items of its body sequence and the operands of captures that are one of these.
	streamTop map[grammar.Expr]bool
}

// callLevel returns the minimum binding level of a call of the rule r (core.Call.Level).
func callLevel(e *grammar.Ref, r *compile.Rule) (int, error) {
	if e.Level == "" {
		return 0, nil
	}
	o, ok := r.Def.Expr.(*grammar.OperatorsRule)
	if !ok {
		return 0, fmt.Errorf("rule %s has no Operators Rule levels", e.Name)
	}
	for i, l := range o.Levels {
		if l.Name == e.Level {
			return i, nil
		}
	}
	return 0, fmt.Errorf("rule %s has no level %s", e.Name, e.Level)
}

// exp returns the ID of the expectation d, interning it.
func (b *coreBuilder) exp(d string) core.ExpID {
	id, ok := b.exps[d]
	if !ok {
		id = core.ExpID(len(b.m.Exps))
		b.exps[d] = id
		b.m.Exps = append(b.m.Exps, d)
	}
	return id
}

// class returns the class of a character class expression, interning it.
func (b *coreBuilder) class(e *grammar.CharClass) *core.Class {
	d := compile.CharClassString(e)
	key := fmt.Sprint(e.Negated, e.Ranges, d)
	if c, ok := b.classes[key]; ok {
		return c
	}
	c := &core.Class{Negated: e.Negated, Exp: b.exp(d)}
	for _, r := range e.Ranges {
		c.Ranges = append(c.Ranges, core.Range{Lo: r.Lo, Hi: r.Hi})
	}
	b.classes[key] = c
	b.m.Classes = append(b.m.Classes, c)
	return c
}

// coreScope is a capture scope being built: the names of its slots and the checked types of the
// captures of its body.
type coreScope struct {
	names []string
	types map[string]compile.Ty
}

// scope returns a scope with the slots names for the scope body e.
func (b *coreBuilder) scope(names []string, e grammar.Expr) *coreScope {
	s := &coreScope{names: names}
	if b.rec != nil {
		s.types = b.rec.Scopes[e]
	}
	return s
}

// slots returns the slots of the scope s, each with the checked type of its capture.
func (s *coreScope) slots() []core.Slot {
	if s.names == nil {
		return nil
	}
	out := make([]core.Slot, len(s.names))
	for i, n := range s.names {
		out[i] = core.Slot{Name: n}
		if t, ok := s.types[n]; ok {
			out[i].Type = t
		}
	}
	return out
}

func (s *coreScope) slot(name string) int {
	for i, n := range s.names {
		if n == name {
			return i
		}
	}
	s.names = append(s.names, name)
	return len(s.names) - 1
}

func (s *coreScope) lookup(name string) (int, bool) {
	for i, n := range s.names {
		if n == name {
			return i, true
		}
	}
	return 0, false
}

func (b *coreBuilder) rule(r *compile.Rule) (*core.Rule, error) {
	b.cur = r
	cr := &core.Rule{ID: r.ID, Name: r.Name, Where: r.WhereName(), Want: !r.Lean}
	if b.rec != nil && !r.Novalue {
		if t, ok := b.rec.Rules[r.Def.Name]; ok {
			cr.Type = t
		}
	}
	cr.Policy = core.Policy{Memo: r.Memo, Transient: r.Transient, Plain: r.Plain, Rec: r.Rec, Leader: r.Leader, Splice: r.Splice,
		NoValue: r.Novalue, Seen: r.Seen, Vars: r.Vars}
	if o, ok := r.Def.Expr.(*grammar.OperatorsRule); ok {
		b.proj = nil
		body, err := b.operatorsRule(o)
		if err != nil {
			return nil, err
		}
		cr.Body = body
		cr.Finish = core.Finish{Kind: core.FinishSplice}
		if r.Action != nil && !r.Novalue {
			// The whole-rule action runs once per call on the folded value, $value.
			env := map[string]*core.Local{compile.OperatorsRuleValue: {Kind: core.LocalValue, Name: compile.OperatorsRuleValue}}
			b.final = r.Action
			cr.Finish.Action, err = b.term(r.Action, b.scope(nil, o), env)
			b.final = nil
			if err != nil {
				return nil, err
			}
			cr.Finish.Kind, cr.Finish.NodeType = core.FinishAction, r.NodeType
		}
		return cr, nil
	}
	if !b.noProject {
		b.proj = compile.RuleProjections(b.prog, r)
	}
	defer func() { b.proj = nil }()
	body := r.BodyExpr()
	b.streamTop = compile.StreamTops(body)
	s := b.scope(coreLayout(b.prog, r, b.proj, body, !r.Lean).names, body)
	n, err := b.expr(body, s, !r.Lean)
	if err != nil {
		return nil, err
	}
	cr.Body = n
	cr.Finish.NodeType = r.NodeType
	cr.Finish.Shape = coreShape(b.prog, body)
	switch {
	case r.Novalue:
		cr.Finish.Kind = core.FinishNone
		cr.Finish.NodeType = ""
	case r.Action != nil:
		cr.Finish.Kind = core.FinishAction
		b.final = r.Action
		cr.Finish.Action, err = b.term(r.Action, s, nil)
		b.final = nil
		if err != nil {
			return nil, err
		}
	case r.Splice:
		cr.Finish.Kind = core.FinishSplice
	default:
		cr.Finish.Kind = core.FinishScope
	}
	cr.Scope = s.slots()
	return cr, nil
}

func (b *coreBuilder) operatorsRule(e *grammar.OperatorsRule) (*core.OperatorsRule, error) {
	o := &core.OperatorsRule{}
	var err error
	if e.Skip != nil {
		if o.Skip, err = b.opLine(e.Skip, nil, false, true); err != nil {
			return nil, err
		}
	}
	for _, x := range e.Operands {
		l, err := b.opLine(x.Expr, x.Action, false, false)
		if err != nil {
			return nil, err
		}
		o.Operands = append(o.Operands, l)
	}
	id := 0
	for i, level := range e.Levels {
		for _, x := range level.Operators {
			l, err := b.opLine(x.Expr, x.Action, true, false)
			if err != nil {
				return nil, err
			}
			o.Ops = append(o.Ops, &core.Operator{ID: id, Kind: x.Kind, Assoc: x.Assoc, Level: i + 1, Line: l})
			id++
		}
	}
	return o, nil
}

// opLine builds an Operators Rule line, the skip line if skip. Lines whose value is unused are
// matched without values, as are the skip line and all lines of a value-free rule.
func (b *coreBuilder) opLine(e grammar.Expr, action grammar.Term, operator, skip bool) (*core.OpLine, error) {
	var locals []string
	if operator {
		locals = []string{"lhs", "rhs", "op"}
	}
	build := !skip && !b.cur.Novalue && !compile.LeanLine(action, locals)
	s := b.scope(coreLayout(b.prog, b.cur, nil, e, build).names, e)
	body, err := b.expr(e, s, build)
	if err != nil {
		return nil, err
	}
	l := &core.OpLine{Body: body, Shape: coreShape(b.prog, e)}
	if action != nil && !b.cur.Novalue {
		env := map[string]*core.Local{}
		if operator {
			env["lhs"] = &core.Local{Kind: core.LocalLhs, Name: "lhs"}
			env["rhs"] = &core.Local{Kind: core.LocalRhs, Name: "rhs"}
			env["op"] = &core.Local{Kind: core.LocalOp, Name: "op"}
		}
		b.final = action
		l.Action, err = b.term(action, s, env)
		b.final = nil
		if err != nil {
			return nil, err
		}
	}
	l.Scope = s.slots()
	return l, nil
}

// coreShape returns the static shape of the value of e.
func coreShape(prog *compile.Program, e grammar.Expr) core.Shape {
	elems := compile.IsElems(prog, e)
	return core.Shape{Elems: elems, K: compile.ValuedCount(prog, e), Mode: core.SingleMode(compile.SingleModeOf(prog, e)), Tuple: compile.ScopeTuple(prog, e),
		Shared: elems && compile.SharesTuple(prog, e)}
}

// coreLayout returns the frame layout of a scope with the body e: the captures written in it in
// the order they are first met, unreachable ones included, following the build policy of the
// rule r with the projections proj.
func coreLayout(prog *compile.Program, r *compile.Rule, proj map[string]string, e grammar.Expr, build bool) *coreScope {
	s := &coreScope{}
	var visit func(grammar.Expr, bool)
	visit = func(e grammar.Expr, build bool) {
		switch e := e.(type) {
		case *grammar.Capture:
			if !build && r.PredCaps != nil && !r.PredCaps[e.Name] {
				visit(e.Expr, false)
				return
			}
			s.slot(e.Name)
			if proj[e.Name] == "" {
				visit(e.Expr, true)
			}
		case *grammar.Seq:
			for _, it := range e.Items {
				visit(it, build)
			}
		case *grammar.Choice:
			for _, alt := range e.Alts {
				visit(alt, build)
			}
		case *grammar.Repeat:
			if !compile.ElementScoped(e.Expr) {
				visit(e.Expr, build)
			}
		case *grammar.Optional:
			visit(e.Expr, build)
		case *grammar.And:
			visit(e.Expr, build && compile.HasCaptures(e.Expr))
		case *grammar.Not:
			visit(e.Expr, false)
		case *grammar.Atomic:
			visit(e.Expr, false)
		case *grammar.Discard:
			visit(e.Expr, false)
		case *grammar.Attributed:
			visit(e.Expr, build)
			for _, a := range e.Attrs {
				if a.Name == "recover" {
					skip, _ := a.Arg("skip")
					visit(skip, false)
				}
			}
		}
	}
	visit(e, build)
	return s
}

// set sets the attributes of the node n built from the expression e that the builder sets: whether
// its value is wanted, and its shape.
func (b *coreBuilder) set(n core.Node, e grammar.Expr, want bool) core.Node {
	a := n.Attrs()
	a.Want, a.Shape = want, coreShape(b.prog, e)
	return n
}

// expr builds the expression e in the scope s; build reports whether its value is wanted.
func (b *coreBuilder) expr(e grammar.Expr, s *coreScope, build bool) (core.Node, error) {
	n, err := b.node(e, s, build)
	if err == nil && b.origin != nil {
		if _, ok := b.origin[n]; !ok {
			b.origin[n] = e
		}
	}
	return n, err
}

func (b *coreBuilder) node(e grammar.Expr, s *coreScope, build bool) (core.Node, error) {
	switch e := e.(type) {
	case *grammar.Literal:
		n := &core.Lit{Text: value.LiteralValue(e.Value), Fold: e.IgnoreCase}
		if n.Text != "" {
			n.Exp = b.exp(compile.LiteralDesc(e))
		}
		return b.set(n, e, build), nil
	case *grammar.CharClass:
		return b.set(&core.ClassNode{Class: b.class(e)}, e, build), nil
	case *grammar.Any:
		return b.set(&core.Any{}, e, build), nil
	case *grammar.Ref:
		r := b.prog.ByName[e.Name]
		if r == nil {
			return nil, fmt.Errorf("undefined rule %s", e.Name)
		}
		level, err := callLevel(e, r)
		if err != nil {
			return nil, err
		}
		if !build && r.Twin != nil {
			r = r.Twin
		}
		return b.set(&core.Call{Rule: r.ID, Level: level}, e, build), nil
	case *grammar.Seq:
		n := &core.Seq{}
		for _, it := range e.Items {
			x, err := b.expr(it, s, build)
			if err != nil {
				return nil, err
			}
			n.Items = append(n.Items, x)
		}
		return b.set(n, e, build), nil
	case *grammar.Choice:
		n := &core.Choice{}
		for _, alt := range e.Alts {
			x, err := b.expr(alt, s, build)
			if err != nil {
				return nil, err
			}
			n.Alts = append(n.Alts, &core.Alt{Body: x})
		}
		return b.set(n, e, build), nil
	case *grammar.Repeat:
		return b.repeat(e, s, build, "")
	case *grammar.Optional:
		x, err := b.expr(e.Expr, s, build)
		if err != nil {
			return nil, err
		}
		return b.set(&core.Opt{Body: x}, e, build), nil
	case *grammar.And:
		x, err := b.expr(e.Expr, s, build && compile.HasCaptures(e.Expr))
		if err != nil {
			return nil, err
		}
		return b.set(&core.Look{Body: x}, e, false), nil
	case *grammar.Not:
		x, err := b.expr(e.Expr, s, false)
		if err != nil {
			return nil, err
		}
		return b.set(&core.Look{Body: x, Neg: true}, e, false), nil
	case *grammar.Atomic:
		x, err := b.expr(e.Expr, s, false)
		if err != nil {
			return nil, err
		}
		return b.set(&core.Text{Body: x}, e, build), nil
	case *grammar.Discard:
		x, err := b.expr(e.Expr, s, false)
		if err != nil {
			return nil, err
		}
		return b.set(&core.Drop{Body: x}, e, false), nil
	case *grammar.Capture:
		if !build && b.cur.PredCaps != nil && !b.cur.PredCaps[e.Name] {
			// Captures that no predicate reads build nothing where no value is wanted.
			return b.expr(e.Expr, s, false)
		}
		slot := s.slot(e.Name)
		n := &core.Capture{Slot: slot, Name: e.Name}
		if field := b.proj[e.Name]; field != "" {
			rep, ok := e.Expr.(*grammar.Repeat)
			if !ok {
				return nil, fmt.Errorf("projected capture %s is not a repetition", e.Name)
			}
			x, err := b.repeat(rep, s, true, field)
			if err != nil {
				return nil, err
			}
			if b.origin != nil {
				b.origin[x] = rep
			}
			n.Body, n.Project = x, field
		} else {
			x, err := b.expr(e.Expr, s, true)
			if err != nil {
				return nil, err
			}
			n.Body = x
		}
		return b.set(n, e, build), nil
	case *grammar.Cut:
		return b.set(&core.Cut{}, e, build), nil
	case *grammar.Top:
		return b.set(&core.Succeed{}, e, build), nil
	case *grammar.Bottom:
		return b.set(&core.FailNode{}, e, build), nil
	case *grammar.BeginInput:
		return b.set(&core.Anchor{Kind: core.BeginInput}, e, build), nil
	case *grammar.EndInput:
		return b.set(&core.Anchor{Kind: core.EndInput}, e, build), nil
	case *grammar.BeginLine:
		return b.set(&core.Anchor{Kind: core.BeginLine}, e, build), nil
	case *grammar.EndLine:
		return b.set(&core.Anchor{Kind: core.EndLine}, e, build), nil
	case *grammar.Predicate:
		if a, ok := e.Term.(*grammar.Bind); ok {
			t, err := b.term(a.Value, s, nil)
			if err != nil {
				return nil, err
			}
			return b.set(&core.BindNode{Var: a.Name, Term: t}, e, build), nil
		}
		t, err := b.term(e.Term, s, nil)
		if err != nil {
			return nil, err
		}
		return b.set(&core.Pred{Term: t}, e, build), nil
	case *grammar.Attributed:
		return b.attributed(e, len(e.Attrs)-1, s, build)
	}
	return nil, fmt.Errorf("unsupported expression %T", e)
}

// attributed builds e under its attributes Attrs[:i+1], the last one outermost.
func (b *coreBuilder) attributed(e *grammar.Attributed, i int, s *coreScope, build bool) (core.Node, error) {
	if i < 0 {
		if rep, ok := e.Expr.(*grammar.Repeat); ok && compile.IsStream(e) && b.starts[b.cur] && b.streamTop[e] {
			x, err := b.expr(rep, s, build)
			if err != nil {
				return nil, err
			}
			n := &core.Stream{Repeat: x.(*core.Repeat)}
			n.Attrs().Want, n.Attrs().Shape = x.Attrs().Want, x.Attrs().Shape
			return n, nil
		}
		return b.expr(e.Expr, s, build)
	}
	a := e.Attrs[i]
	switch a.Name {
	case "error", "diagnostic":
		x, err := b.attributed(e, i-1, s, build)
		if err != nil {
			return nil, err
		}
		n := &core.Label{Body: x, Diagnostic: a.Name == "diagnostic"}
		if n.Diagnostic {
			n.Exp = b.exp(compile.AttributeDiagnosticName(a))
		} else {
			n.Exp = b.exp(compile.AttributeMessage(a))
		}
		// The value of a label is that of its operand: the attributed expression once it recovers.
		shaped := grammar.Expr(e.Expr)
		if i == len(e.Attrs)-1 {
			shaped = e
		}
		for _, inner := range e.Attrs[:i] {
			if inner.Name == "recover" {
				shaped = e
			}
		}
		return b.set(n, shaped, build), nil
	case "recover":
		x, err := b.attributed(e, i-1, s, build)
		if err != nil {
			return nil, err
		}
		skipExpr, _ := a.Arg("skip")
		skip, err := b.expr(skipExpr, s, false)
		if err != nil {
			return nil, err
		}
		n := &core.Recover{Body: x, Skip: skip}
		if build {
			// What recovers is one element: the value of the expression, or the error.
			n.Attrs().Want, n.Attrs().Shape = true, coreShape(b.prog, e)
		}
		return n, nil
	case "stream":
		// The innermost attribute applies to the repetition itself (attributed).
		return b.attributed(e, i-1, s, build)
	case "missing":
		x, err := b.attributed(e, i-1, s, build)
		if err != nil {
			return nil, err
		}
		l, ok := x.(*core.Lit)
		if !ok || l.Text == "" {
			return nil, fmt.Errorf("#missing on %T, not a nonempty literal", x)
		}
		n := &core.Missing{Lit: l}
		n.Attrs().Want, n.Attrs().Shape = l.Attrs().Want, l.Attrs().Shape
		return n, nil
	}
	return nil, fmt.Errorf("unknown attribute %s", a.Name)
}

// repeat builds a repetition; with field, a projected repetition gathering the values of the
// capture field of its elements.
func (b *coreBuilder) repeat(e *grammar.Repeat, s *coreScope, build bool, field string) (core.Node, error) {
	n := &core.Repeat{Min: e.Min, Max: e.Max, Project: field}
	es := s
	if field != "" || compile.ElementScoped(e.Expr) {
		es = b.scope(coreLayout(b.prog, b.cur, b.proj, e.Expr, build && field == "").names, e.Expr)
	}
	x, err := b.expr(e.Expr, es, build && field == "")
	if err != nil {
		return nil, err
	}
	n.Body = x
	if es != s {
		n.Scope = es.slots()
		if n.Scope == nil {
			n.Scope = []core.Slot{}
		}
		if field != "" {
			if _, ok := es.lookup(field); !ok {
				return nil, fmt.Errorf("projected field %s is not captured in the element", field)
			}
		}
	}
	return b.set(n, e, build), nil
}

// term builds the term t in the scope s, with its checked type; env maps the names of lambda
// parameters and Operators Rule locals in scope to their locals.
func (b *coreBuilder) term(t grammar.Term, s *coreScope, env map[string]*core.Local) (core.Term, error) {
	if g, ok := t.(*grammar.TermGroup); ok {
		return b.term(g.Term, s, env)
	}
	x, err := b.termNode(t, s, env)
	if err != nil {
		return nil, err
	}
	if b.rec != nil {
		if typ, ok := b.rec.Terms[t]; ok {
			core.SetType(x, typ)
		}
	}
	return x, nil
}

func (b *coreBuilder) termNode(t grammar.Term, s *coreScope, env map[string]*core.Local) (core.Term, error) {
	sub := func(x grammar.Term) (core.Term, error) { return b.term(x, s, env) }
	switch t := t.(type) {
	case *grammar.IntLit:
		return &core.Const{Value: t.Value}, nil
	case *grammar.StringLit:
		return &core.Const{Value: t.Value}, nil
	case *grammar.BoolLit:
		return &core.Const{Value: t.Value}, nil
	case *grammar.NilLit:
		return &core.Const{Value: nil}, nil
	case *grammar.CaptureRef:
		if l, ok := env[t.Name]; ok {
			ref := *l
			return &ref, nil
		}
		slot, ok := s.lookup(t.Name)
		if !ok {
			return nil, fmt.Errorf("capture %s is not in scope %v", t.Name, s.names)
		}
		return &core.SlotRef{Slot: slot, Name: t.Name}, nil
	case *grammar.VarRef:
		return &core.VarRef{Name: t.Name}, nil
	case *grammar.Member:
		x, err := sub(t.X)
		if err != nil {
			return nil, err
		}
		return &core.Member{X: x, Name: t.Name}, nil
	case *grammar.New:
		if len(t.Args) != 0 {
			x, err := sub(t.Args[0])
			if err != nil {
				return nil, err
			}
			n, ok := b.prog.NodeTypeOf(t.Type) // the type's own name, also for an alias of it
			if !ok {
				return nil, fmt.Errorf("%s is not a named node type", t.Type)
			}
			proven := b.rec != nil && b.rec.Proven[t]
			return &core.Convert{NodeType: n.Name, X: x, Proven: proven}, nil
		}
		name, spec := b.prog.StructType(t.Type)
		n := &core.New{Type: name, Final: grammar.Term(t) == b.final}
		for _, f := range compile.DefFields(spec) {
			n.Declared = append(n.Declared, f.Name)
		}
		for _, f := range t.Fields {
			v, err := sub(f.Value)
			if err != nil {
				return nil, err
			}
			n.Fields = append(n.Fields, core.FieldInit{Name: f.Name, Value: v})
		}
		return n, nil
	case *grammar.ListConstructor:
		n := &core.Builtin{Func: "list"}
		for _, x := range t.Items {
			v, err := sub(x)
			if err != nil {
				return nil, err
			}
			n.Args = append(n.Args, v)
		}
		return n, nil
	case *grammar.TupleLit:
		n := &core.Tuple{}
		for _, x := range t.Elems {
			v, err := sub(x)
			if err != nil {
				return nil, err
			}
			n.Elems = append(n.Elems, v)
		}
		return n, nil
	case *grammar.Call:
		n := &core.Builtin{Func: t.Func}
		if t.Func == "map" {
			_, n.Projected = compile.ProjectedMap(t, b.proj, func(name string) bool { _, local := env[name]; return local })
		}
		for _, a := range t.Args {
			v, err := sub(a)
			if err != nil {
				return nil, err
			}
			n.Args = append(n.Args, v)
		}
		return n, nil
	case *grammar.Lambda:
		l := &core.Lambda{Params: t.Params}
		inner := make(map[string]*core.Local, len(env)+len(t.Params))
		for k, v := range env {
			inner[k] = v
		}
		for _, p := range t.Params {
			inner[p] = &core.Local{Kind: core.LocalParam, Name: p, Lambda: l}
		}
		body, err := b.term(t.Body, s, inner)
		if err != nil {
			return nil, err
		}
		l.Body = body
		return l, nil
	case *grammar.Binary:
		l, err := sub(t.L)
		if err != nil {
			return nil, err
		}
		r, err := sub(t.R)
		if err != nil {
			return nil, err
		}
		return &core.Binary{Op: t.Op, L: l, R: r}, nil
	case *grammar.Unary:
		x, err := sub(t.X)
		if err != nil {
			return nil, err
		}
		return &core.Unary{Op: t.Op, X: x}, nil
	case *grammar.Cast:
		x, err := sub(t.X)
		if err != nil {
			return nil, err
		}
		check := b.prog.ResolveCheck(t.Type)
		if check == nil {
			return x, nil // every value fits the type
		}
		line, col := b.prog.CastPos(t)
		return &core.Cast{X: x, Check: check, Key: t, Line: line, Col: col}, nil
	}
	return nil, fmt.Errorf("unsupported term %T", t)
}

// FollowRepeats returns functions that, once the scan pass has run, give the scan that took the
// place of each repetition with an origin the origin of the repetition.
func FollowRepeats(m *core.Module, origin map[core.Node]grammar.Expr) []func() {
	var out []func()
	visit := func(at func() core.Node) {
		if rep, ok := at().(*core.Repeat); ok {
			if e, ok := origin[rep]; ok {
				out = append(out, func() {
					if s, ok := at().(*core.Scan); ok {
						origin[s] = e
					}
				})
			}
		}
	}
	for _, r := range m.Rules {
		EachSlot(r, visit)
	}
	return out
}

// EachSlot calls f with an accessor of every place of the rule's body where a node is: the body,
// the bodies of Operators Rule lines and the children of nodes.
func EachSlot(r *core.Rule, f func(at func() core.Node)) {
	var walk func(at func() core.Node)
	walk = func(at func() core.Node) {
		f(at)
		n := at()
		if o, ok := n.(*core.OperatorsRule); ok {
			for _, l := range o.Lines() {
				walk(func() core.Node { return l.Body })
			}
			return
		}
		for i := range core.Children(n) {
			walk(func() core.Node { return core.Children(n)[i] })
		}
	}
	walk(func() core.Node { return r.Body })
}
