package engine

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ornew/pego/grammar"
)

// match and node select the value representation without changing the typed
// emitter's construction or ownership policy.
func (d *dgen) match(start, text string) string {
	return fmt.Sprintf("p.NewTerminal(%s, %s)", start, text)
}

func (d *dgen) node(typ, start, kids string) string {
	kind := "RawKindSeq"
	if typ == TypeList {
		kind = "RawKindList"
	}
	return fmt.Sprintf("p.NewBranch(%s, %s, %s)", kind, start, kids)
}

func (d *dgen) frameScope(s *dscope) *scope {
	if s.frame == nil {
		s.frame = newScope()
	}
	return s.frame
}

func frameRefsKnown(t grammar.Term, s *scope, locals ...string) bool {
	ok := true
	capRefs(t, func(name string) {
		if slices.Contains(locals, name) {
			return
		}
		if _, found := s.slots[name]; !found {
			ok = false
		}
	})
	return ok
}

func (d *dgen) nodeCapture(e *grammar.Capture, s *dscope, build bool, fail string) string {
	g := d.g
	if !build && g.cur.predCaps != nil && !g.cur.predCaps[e.Name] {
		return d.expr(e.Expr, s, false, fail)
	}
	slot := d.frameScope(s).slot(e.Name)
	var v string
	if field := g.proj[e.Name]; field != "" {
		v = d.nodeRepeat(e.Expr.(*grammar.Repeat), s, true, fail, field)
	} else {
		v = d.captured(e.Expr, s, fail)
	}
	d.line("p.SetCapture(%d, %s)", slot, d.rd(v))
	if !build {
		return "nil"
	}
	return v
}

// nodeRepeat keeps capture frames and the undo trail owned by the selected runtime.
// Projected repeats reuse a cleared element frame, as projectRepeat does.
func (d *dgen) nodeRepeat(e *grammar.Repeat, s *dscope, build bool, fail, field string) string {
	if !build && field == "" {
		if v, ok := d.scan(e, fail); ok {
			return v
		}
	}
	own := field != "" || elementScoped(e.Expr)
	es := s
	if own {
		es = newDscope()
	}
	var start, base, frame, prev, names string
	if build {
		start, base = d.decl("x", "int"), d.decl("x", "int")
		d.line("%s, %s = p.Pos, len(p.KidStack)", start, base)
	}
	if own {
		frame, prev = d.decl("x", d.g.pick("*CaptureFrame", "*TCaptureFrame")), d.decl("x", d.g.pick("*CaptureFrame", "*TCaptureFrame"))
		names = d.g.name("scope")
		if field != "" {
			d.line("%s = p.NewFrame(len(%s))", frame, names)
		}
	}
	count := d.decl("x", "int")
	d.line("%s = 0", count)
	d.loop(e.Max)
	if field != "" {
		d.line("clear(%s.Vals)", d.rd(frame))
	}
	m := d.mark(s, e.Expr, true)
	if own {
		d.line("%s = p.Frame", prev)
		if field == "" {
			d.line("%s = p.NewFrame(len(%s))", frame, names)
		}
		d.line("p.Frame = %s", d.rd(frame))
	}
	f := d.label()
	v, cut := d.cutExpr(e.Expr, es, build && field == "", f)
	if build && field == "" {
		v = d.scopeValue(e.Expr, v, m.pos)
	}
	if own {
		d.line("p.Frame = %s", d.rd(prev))
		if build {
			if field != "" {
				slot, ok := d.frameScope(es).slots[field]
				if !ok {
					d.bad = true
				}
				v = fmt.Sprintf("%s.Vals[%d]", d.rd(frame), slot)
			} else {
				attached := d.decl("v", d.g.valType())
				vals := d.rd(frame)
				d.line("%s = p.AttachCaptures(%s, %s, %s, %s, p.Pos)", attached, d.rd(v), names, vals, d.rd(m.pos))
				v = attached
			}
		}
		fmt.Fprintf(&d.g.vars, "var %s = %s\n", names, goStrings(d.frameScope(es).names))
		d.g.decide(decScope, e.Expr, genDecision{names: d.frameScope(es).names})
	}
	d.line("%s++", count)
	if build {
		d.line("p.KidStack = append(p.KidStack, %s)", d.rd(v))
	}
	d.line("if p.Pos == %s {\nbreak\n}", d.rd(m.pos))
	if d.used[f] {
		d.exit("continue")
		d.place(f)
		if own {
			d.line("p.Frame = %s", d.rd(prev))
		}
		d.reset(m)
		if build {
			d.cutFail(cut, fail, fmt.Sprintf("p.DropKids(%s)", d.rd(base)))
		} else {
			d.cutFail(cut, fail)
		}
		d.exit("break")
	}
	d.endLoop()
	if e.Min > 0 {
		if build {
			d.failIf(fmt.Sprintf("%s < %d", d.rd(count), e.Min), fail, fmt.Sprintf("p.DropKids(%s)", d.rd(base)))
		} else {
			d.failIf(fmt.Sprintf("%s < %d", d.rd(count), e.Min), fail)
		}
	}
	if !build {
		return "nil"
	}
	list := d.decl("v", d.g.valType())
	d.line("%s = %s", list, d.node("list", d.rd(start), fmt.Sprintf("p.Kids(%s)", d.rd(base))))
	return list
}

// directNodeBody inlines an expression body, leaving rule-level depth, memo,
// frame lifetime, actions and finish to invoke/invokePlain. This body therefore
// serves both normal calls and external rule-table entry without double finish.
func (g *generator) directNodeBody(r *rule, scope *scope) string {
	if !directOK(r) {
		return ""
	}
	return g.directFrameBody(r, scope, "Node body inlined")
}

// typedFrameComment reports whether the body of the typed rule r is inlined under the
// runtime-owned frame, a left-recursion leader or a rule that recovers, and returns the comment of
// its matcher.
func (g *generator) typedFrameComment(r *rule) (string, bool) {
	comment := "typed recovery body inlined"
	if r.leader {
		comment = "typed LR body inlined"
	} else if !directHasRecovery(r.bodyExpr()) {
		return "", false
	}
	if !directFrameExprOK(r.bodyExpr(), true, true) {
		return "", false
	}
	return comment, true
}

func (g *generator) directFrameBody(r *rule, scope *scope, comment string) string {
	snap := g.snapshot()
	d := &dgen{g: g, nodeBody: true, reads: map[string]bool{}, used: map[string]bool{}}
	s := newDscope()
	g.cur, g.proj = r, g.projections(r)
	defer func() { g.proj = nil }()
	v := d.expr(r.bodyExpr(), s, !r.lean, "fail")
	if r.action != nil && !frameRefsKnown(r.action, d.frameScope(s)) {
		d.bad = true
	}
	if d.bad {
		g.rollback(snap)
		return ""
	}
	return g.finishFrameBody(d, s, scope, v, r.name+" ("+comment+")")
}

// finishFrameBody emits only an expression matcher. Its caller owns the frame,
// action, rule finalization and recursion/depth bookkeeping.
func (g *generator) finishFrameBody(d *dgen, s *dscope, scope *scope, v, comment string) string {
	if !d.dead {
		d.line("return %s, true", d.rd(v))
	}
	if d.used["fail"] {
		d.place("fail")
		d.line("return nil, false")
	}
	var b strings.Builder
	var unread []string
	for _, v := range d.decls {
		if !d.reads[v.name] {
			unread = append(unread, v.name)
		}
	}
	b.WriteString(varLines(d.decls))
	if len(unread) > 0 {
		fmt.Fprintf(&b, "%s = %s\n", strings.TrimSuffix(strings.Repeat("_, ", len(unread)), ", "), strings.Join(unread, ", "))
	}
	b.WriteString(d.b.String())
	*scope = *d.frameScope(s)
	return g.method(comment, b.String())
}
