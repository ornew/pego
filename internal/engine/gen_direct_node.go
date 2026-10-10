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
	if d.g.table == "trules" {
		return fmt.Sprintf("p.newMatch(%s, p.pos, %s, true)", start, text)
	}
	return fmt.Sprintf("p.newNode(Node{kind: kindMatch, Start: int32(%s), End: int32(p.pos), Text: %s, terminal: true, fresh: true})", start, text)
}

func (d *dgen) node(typ, start, kids string) string {
	if d.g.table == "trules" {
		return fmt.Sprintf("p.newNode(%q, %s, p.pos, %s)", typ, start, kids)
	}
	return fmt.Sprintf("p.newNode(Node{kind: kind%s, Start: int32(%s), End: int32(p.pos), Children: %s, fresh: true})", typ, start, kids)
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
		v = d.expr(e.Expr, s, true, fail)
	}
	d.line("p.setCapture(%d, %s)", slot, d.rd(v))
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
		d.line("%s, %s = p.pos, len(p.kidStack)", start, base)
	}
	if own {
		frame, prev = d.decl("x", d.g.pick("*frame", "*tframe")), d.decl("x", d.g.pick("*frame", "*tframe"))
		names = d.g.name("scope")
		if field != "" {
			d.line("%s = p.newFrame(len(%s))", frame, names)
		}
	}
	count := d.decl("x", "int")
	d.line("%s = 0", count)
	d.loop(e.Max)
	if field != "" {
		d.line("clear(%s.vals)", d.rd(frame))
	}
	m := d.mark(s, e.Expr, true)
	if own {
		d.line("%s = p.frame", prev)
		if field == "" {
			d.line("%s = p.newFrame(len(%s))", frame, names)
		}
		d.line("p.frame = %s", d.rd(frame))
	}
	f := d.label()
	v := d.expr(e.Expr, es, build && field == "", f)
	if own {
		d.line("p.frame = %s", d.rd(prev))
		if build {
			if field != "" {
				slot, ok := d.frameScope(es).slots[field]
				if !ok {
					d.bad = true
				}
				v = fmt.Sprintf("%s.vals[%d]", d.rd(frame), slot)
			} else {
				attached := d.decl("v", d.g.valType())
				vals := d.rd(frame)
				if d.g.table == "trules" {
					vals += ".vals"
				}
				d.line("%s = p.attachCaptures(%s, %s, %s, %s, p.pos)", attached, d.rd(v), names, vals, d.rd(m.pos))
				v = attached
			}
		}
		fmt.Fprintf(&d.g.vars, "var %s = %s\n", names, goStrings(d.frameScope(es).names))
		if d.g.table == "trules" && field == "" {
			d.line("p.dropTrail(%s.trail)", d.rd(m.frameMark))
			d.line("p.freeFrame(%s)", d.rd(frame))
		}
	}
	d.line("%s++", count)
	if build {
		d.line("p.kidStack = append(p.kidStack, %s)", d.rd(v))
	}
	d.line("if p.pos == %s {\nbreak\n}", d.rd(m.pos))
	if d.used[f] {
		d.exit("continue")
		d.place(f)
		if own {
			d.line("p.frame = %s", d.rd(prev))
		}
		d.reset(m)
		d.exit("break")
	}
	d.endLoop()
	if e.Min > 0 {
		if build {
			d.failIf(fmt.Sprintf("%s < %d", d.rd(count), e.Min), fail, fmt.Sprintf("p.dropKids(%s)", d.rd(base)))
		} else {
			d.failIf(fmt.Sprintf("%s < %d", d.rd(count), e.Min), fail)
		}
	}
	if !build {
		return "nil"
	}
	list := d.decl("v", d.g.valType())
	d.line("%s = %s", list, d.node("List", d.rd(start), fmt.Sprintf("p.kids(%s)", d.rd(base))))
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

func (g *generator) directTypedLRBody(r *rule, scope *scope) string {
	if g.disableTypedLRBodies || !r.leader || !directExprEligible(r, false) {
		return ""
	}
	return g.directFrameBody(r, scope, "typed LR body inlined")
}

func (g *generator) directFrameBody(r *rule, scope *scope, comment string) string {
	snap := g.snapshot()
	d := &dgen{g: g, nodeBody: true, reads: map[string]bool{}, used: map[string]bool{}}
	s := newDscope()
	g.cur, g.proj = r, g.projections(r)
	defer func() { g.proj = nil }()
	v := d.expr(r.def.Expr, s, !r.lean, "fail")
	if r.action != nil && !frameRefsKnown(r.action, d.frameScope(s)) {
		d.bad = true
	}
	if g.table == "trules" && !slices.Equal(d.frameScope(s).names, g.nodeScopes[r]) {
		// Runtime-owned frames use the Node rule's shared scope metadata.
		// Keep the general body when structural lowering changes that layout.
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
	b.WriteString("var (\n")
	var unread []string
	for _, v := range d.decls {
		fmt.Fprintf(&b, "%s %s\n", v.name, v.typ)
		if !d.reads[v.name] {
			unread = append(unread, v.name)
		}
	}
	b.WriteString(")\n")
	if len(unread) > 0 {
		fmt.Fprintf(&b, "%s = %s\n", strings.TrimSuffix(strings.Repeat("_, ", len(unread)), ", "), strings.Join(unread, ", "))
	}
	b.WriteString(d.b.String())
	*scope = *d.frameScope(s)
	return g.method(comment, b.String())
}
