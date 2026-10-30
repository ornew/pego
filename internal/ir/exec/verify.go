package exec

import (
	"errors"
	"fmt"
	"slices"
)

// Verify checks the module and returns every violation it finds, joined:
//
//   - declarations: names are unique; handle types, operations and their reads and writes name
//     declared components, registers, handle types and features; an operation writes only its own
//     component and registers; cost terms have a positive coefficient and a quantity;
//   - functions: every expression has a type and every operator, call, assignment and return
//     matches the declared types; a local is declared before it is read or assigned and is not
//     declared twice in one scope; an Exit names an enclosing block, a Break or a Continue an
//     enclosing loop; a function calls only operations of the features it is specialized for.
func Verify(m *Module) error {
	v := &verifier{m: m}
	v.decls()
	for _, f := range m.Funcs {
		v.fn(f)
	}
	return errors.Join(v.errs...)
}

type verifier struct {
	m    *Module
	errs []error
	f    *Func
}

func (v *verifier) errorf(format string, args ...any) {
	if v.f != nil {
		format = "func " + v.f.Name + ": " + format
	}
	v.errs = append(v.errs, fmt.Errorf(format, args...))
}

func (v *verifier) unique(kind string, names []string) {
	seen := map[string]bool{}
	for _, n := range names {
		if n == "" {
			v.errorf("%s without a name", kind)
		} else if seen[n] {
			v.errorf("%s %s declared twice", kind, n)
		}
		seen[n] = true
	}
}

func names[T any](xs []T, name func(T) string) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = name(x)
	}
	return out
}

func (v *verifier) typ(t Type, where string) {
	if t.Kind == KInvalid || t.Kind > KHandle {
		v.errorf("%s: invalid type", where)
	} else if t.Kind == KHandle && v.m.handle(t.Handle) == nil {
		v.errorf("%s: undeclared handle type %s", where, t.Handle)
	}
}

func (v *verifier) decls() {
	m := v.m
	v.unique("feature", m.Features)
	v.unique("component", names(m.Components, func(c *Component) string { return c.Name }))
	// Registers and components share the namespace of reads and writes.
	v.unique("state", append(names(m.Components, func(c *Component) string { return c.Name }),
		names(m.Registers, func(r *Register) string { return r.Name })...))
	v.unique("handle type", names(m.Handles, func(h *HandleType) string { return h.Name }))
	// Operations and functions share the namespace of calls.
	v.unique("callee", append(names(m.Ops, func(o *Op) string { return o.Name }),
		names(m.Funcs, func(f *Func) string { return f.Name })...))
	for _, r := range m.Registers {
		v.typ(r.Type, "register "+r.Name)
		if r.Type.Kind == KHandle {
			v.errorf("register %s: a register holds a scalar", r.Name)
		}
	}
	for _, h := range m.Handles {
		if m.component(h.Component) == nil {
			v.errorf("handle type %s: undeclared component %s", h.Name, h.Component)
		}
	}
	for _, o := range m.Ops {
		where := "op " + o.Name
		if m.component(o.Component) == nil {
			v.errorf("%s: undeclared component %s", where, o.Component)
		}
		v.unique(where+": parameter", names(o.Params, func(p Param) string { return p.Name }))
		for _, p := range o.Params {
			v.typ(p.Type, where+" parameter "+p.Name)
		}
		for _, t := range o.Results {
			v.typ(t, where+" result")
		}
		for _, s := range o.Reads {
			if m.component(s) == nil && m.register(s) == nil {
				v.errorf("%s: reads undeclared state %s", where, s)
			}
		}
		for _, s := range o.Writes {
			switch {
			case m.register(s) != nil:
			case m.component(s) == nil:
				v.errorf("%s: writes undeclared state %s", where, s)
			case s != o.Component:
				v.errorf("%s: writes component %s, which is not its own (%s)", where, s, o.Component)
			}
		}
		for _, f := range o.Features {
			if !m.hasFeature(f) {
				v.errorf("%s: undeclared feature %s", where, f)
			}
		}
		if o.Contract == "" && !o.Transitional {
			v.errorf("%s: no contract", where)
		}
		for _, t := range o.Cost.Terms {
			if t.Coef <= 0 || t.Quantity == "" {
				v.errorf("%s: cost term needs a positive coefficient and a quantity", where)
			}
		}
		if o.Cost.Const < 0 {
			v.errorf("%s: negative cost", where)
		}
	}
}

// scope is a lexical scope of locals, with the labels of the enclosing blocks and loops.
type scope struct {
	parent *scope
	locals map[string]Type
	label  string
	loop   bool // label is a loop's
}

func (s *scope) lookup(name string) (Type, bool) {
	for ; s != nil; s = s.parent {
		if t, ok := s.locals[name]; ok {
			return t, true
		}
	}
	return Type{}, false
}

func (s *scope) hasLabel(label string, loop bool) bool {
	for ; s != nil; s = s.parent {
		if s.label == label && (!loop || s.loop) {
			return true
		}
	}
	return false
}

func (v *verifier) fn(f *Func) {
	v.f = f
	defer func() { v.f = nil }()
	for _, ft := range f.Features {
		if !v.m.hasFeature(ft) {
			v.errorf("undeclared feature %s", ft)
		}
	}
	top := &scope{locals: map[string]Type{}}
	for _, p := range f.Params {
		v.typ(p.Type, "parameter "+p.Name)
		if _, dup := top.locals[p.Name]; dup {
			v.errorf("parameter %s declared twice", p.Name)
		}
		top.locals[p.Name] = p.Type
	}
	for _, t := range f.Results {
		v.typ(t, "result")
	}
	if f.Body == nil {
		v.errorf("no body")
		return
	}
	v.block(f.Body, top, false)
}

func (v *verifier) block(b *Block, parent *scope, loop bool) {
	s := &scope{parent: parent, locals: map[string]Type{}, label: b.Label, loop: loop}
	for _, st := range b.Stmts {
		v.stmt(st, s)
	}
}

func (v *verifier) stmt(st Stmt, s *scope) {
	switch st := st.(type) {
	case *Block:
		v.block(st, s, false)
	case *Loop:
		if st.Label == "" {
			v.errorf("loop without a label")
		}
		v.block(&Block{Label: st.Label, Stmts: st.Body.Stmts}, s, true)
	case *If:
		v.want(st.Cond, s, TBool, "if condition")
		v.block(st.Then, s, false)
		if st.Else != nil {
			v.block(st.Else, s, false)
		}
	case *Let:
		var types []Type
		if c, ok := st.Value.(*Call); ok && len(st.Names) != 1 {
			types = v.call(c, s)
			if types != nil && len(types) != len(st.Names) {
				v.errorf("let %v: %s has %d results", st.Names, c.Callee, len(types))
				types = nil
			}
		} else if len(st.Names) == 1 {
			if t, ok := v.expr(st.Value, s); ok {
				types = []Type{t}
			}
		} else {
			v.errorf("let %v: several names need a call", st.Names)
		}
		for i, n := range st.Names {
			if _, dup := s.locals[n]; dup {
				v.errorf("local %s declared twice in one scope", n)
			} else if _, outer := s.lookup(n); outer {
				v.errorf("local %s shadows an enclosing local", n)
			}
			t := Type{}
			if types != nil {
				t = types[i]
			}
			s.locals[n] = t
		}
	case *Set:
		var t Type
		if st.Reg {
			r := v.m.register(st.Name)
			if r == nil {
				v.errorf("set of undeclared register %s", st.Name)
				return
			}
			t = r.Type
		} else {
			var ok bool
			if t, ok = s.lookup(st.Name); !ok {
				v.errorf("set of undeclared local %s", st.Name)
				return
			}
		}
		v.want(st.Value, s, t, "set "+st.Name)
	case *Do:
		v.call(st.Call, s)
	case *Exit:
		if !s.hasLabel(st.Label, false) || st.Label == "" {
			v.errorf("exit %q names no enclosing block", st.Label)
		}
	case *Break:
		if !s.hasLabel(st.Label, true) || st.Label == "" {
			v.errorf("break %q names no enclosing loop", st.Label)
		}
	case *Continue:
		if !s.hasLabel(st.Label, true) || st.Label == "" {
			v.errorf("continue %q names no enclosing loop", st.Label)
		}
	case *Return:
		if len(st.Values) != len(v.f.Results) {
			v.errorf("return of %d values, want %d", len(st.Values), len(v.f.Results))
			return
		}
		for i, x := range st.Values {
			v.want(x, s, v.f.Results[i], "return")
		}
	case *Assert:
		v.want(st.Cond, s, TBool, "assert")
	default:
		v.errorf("unknown statement %T", st)
	}
}

func (v *verifier) want(x Expr, s *scope, t Type, where string) {
	if got, ok := v.expr(x, s); ok && got != t {
		v.errorf("%s: %s, want %s", where, got, t)
	}
}

// call checks a call and returns its result types (nil after an error).
func (v *verifier) call(c *Call, s *scope) []Type {
	var params []Param
	var results []Type
	if o := v.m.Op(c.Callee); o != nil {
		for _, ft := range o.Features {
			if !slices.Contains(v.f.Features, ft) {
				v.errorf("call of %s, which needs feature %s", o.Name, ft)
			}
		}
		params, results = o.Params, o.Results
	} else if f := v.m.Func(c.Callee); f != nil {
		params, results = f.Params, f.Results
	} else {
		v.errorf("call of undeclared %s", c.Callee)
		return nil
	}
	if len(c.Args) != len(params) {
		v.errorf("call of %s with %d arguments, want %d", c.Callee, len(c.Args), len(params))
		return nil
	}
	for i, a := range c.Args {
		v.want(a, s, params[i].Type, c.Callee+" argument "+params[i].Name)
	}
	if results == nil {
		results = []Type{}
	}
	return results
}

// expr returns the type of x, or false after reporting an error.
func (v *verifier) expr(x Expr, s *scope) (Type, bool) {
	switch x := x.(type) {
	case *Const:
		switch {
		case x.Type == TBool:
			_, ok := x.Value.(bool)
			if !ok {
				v.errorf("bool constant holds %T", x.Value)
			}
			return x.Type, ok
		case x.Type.integer():
			var ok bool
			switch x.Type.Kind {
			case KInt:
				_, ok = x.Value.(int64)
			case KI32:
				_, ok = x.Value.(int32)
			case KU32:
				_, ok = x.Value.(uint32)
			case KU64:
				_, ok = x.Value.(uint64)
			}
			if !ok {
				v.errorf("%s constant holds %T", x.Type, x.Value)
			}
			return x.Type, ok
		}
		v.errorf("constant of type %s", x.Type)
	case *Local:
		t, ok := s.lookup(x.Name)
		if !ok {
			v.errorf("read of undeclared local %s", x.Name)
		}
		return t, ok && t.Kind != KInvalid
	case *Reg:
		if r := v.m.register(x.Name); r != nil {
			return r.Type, true
		}
		v.errorf("read of undeclared register %s", x.Name)
	case *Feature:
		if v.m.hasFeature(x.Name) {
			return TBool, true
		}
		v.errorf("undeclared feature %s", x.Name)
	case *Unary:
		t, ok := v.expr(x.X, s)
		if !ok {
			return t, false
		}
		switch {
		case x.Op == "!" && t == TBool, x.Op == "-" && t.integer():
			return t, true
		}
		v.errorf("operator %s on %s", x.Op, t)
	case *Binary:
		tx, okx := v.expr(x.X, s)
		ty, oky := v.expr(x.Y, s)
		if !okx || !oky {
			return Type{}, false
		}
		if tx != ty {
			v.errorf("operator %s on %s and %s", x.Op, tx, ty)
			return Type{}, false
		}
		switch x.Op {
		case "&&", "||":
			if tx == TBool {
				return TBool, true
			}
		case "==", "!=":
			return TBool, true
		case "<", "<=", ">", ">=":
			if tx.integer() {
				return TBool, true
			}
		case "+", "-", "*", "/", "%", "&", "|", "^", "<<", ">>":
			if tx.integer() {
				return tx, true
			}
		default:
			v.errorf("unknown operator %s", x.Op)
			return Type{}, false
		}
		v.errorf("operator %s on %s", x.Op, tx)
	case *Convert:
		t, ok := v.expr(x.X, s)
		if ok && t.integer() && x.To.integer() {
			return x.To, true
		}
		if ok {
			v.errorf("conversion of %s to %s", t, x.To)
		}
	case *Call:
		r := v.call(x, s)
		if r == nil {
			return Type{}, false
		}
		if len(r) != 1 {
			v.errorf("%s has %d results where one value is needed", x.Callee, len(r))
			return Type{}, false
		}
		return r[0], true
	default:
		v.errorf("unknown expression %T", x)
	}
	return Type{}, false
}
