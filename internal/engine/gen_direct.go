package engine

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ornew/pego/grammar"
)

// Direct rules of the typed runtime and value-free plain Node rules.
//
// The typed runtime (genrt/typed.go) runs a rule like the Node runtime does: a method per
// expression, captures in a frame with a trail that backtracking undoes, and an action evaluated
// in a context that reads the frame. A rule whose body uses only the constructs handled here is
// instead compiled into the one method that calls it (a direct rule):
//
//   - The body is inlined: an expression jumps to a label when it fails. Every variable is
//     declared at the top of the method, so each goto is legal.
//   - Captures are variables of the method; those of a repetition element with captures of its
//     own are cleared at the start of each iteration.
//   - A backtracking point (choice, optional, repetition element, !) saves the position, the
//     number of recovered errors, the variable environment if the expression assigns variables,
//     and the captures the expression may set, and restores them when it fails: what reset
//     undoes through the trail, which only setCapture into the rule's own frame extends.
//   - The action is a Go expression over the capture variables, evaluated in place.
//
// A direct rule never reads or sets p.cut (it has no cut, and every callee restores it), p.frame
// (callees set their own) or p.trail (callees truncate it to its length at the call), so it
// leaves them alone. Rules with a cut or #recover, Pratt rules and leaders of left recursion
// keep the general code. Value-free plain Node rules reuse the structural walk
// without captures or predicates; value-building and memoized Node rules keep
// the general code.

// directOK reports whether the rule r can be compiled as a direct rule.
func directOK(r *rule) bool {
	if r.leader {
		return false
	}
	if _, pratt := r.def.Expr.(*grammar.Pratt); pratt {
		return false
	}
	ok := true
	walkExpr(r.def.Expr, func(e grammar.Expr) {
		switch e := e.(type) {
		case *grammar.Cut, *grammar.Pratt:
			ok = false
		case *grammar.Attributed:
			for _, a := range e.Attrs {
				if a.Name == "recover" {
					ok = false
				}
			}
		}
	})
	return ok
}

// directLeanOK selects value-free plain rules of the Node runtime. Captures
// unused by recognition are omitted; predicates keep the general code because
// they can require values, capture frames and variable assignment.
func directLeanOK(r *rule) bool {
	if !r.plain || !r.lean || !r.novalue || !directOK(r) {
		return false
	}
	ok := true
	walkExpr(r.def.Expr, func(e grammar.Expr) {
		if _, predicate := e.(*grammar.Predicate); predicate {
			ok = false
		}
	})
	return ok
}

// dgen compiles the body of a direct rule into the statements of a method.
type dgen struct {
	g     *generator
	b     strings.Builder
	decls []dvar
	reads map[string]bool // variables read
	used  map[string]bool // labels jumped to
	n     int
	// dead reports that the current point is unreachable: statements are dropped (go vet
	// rejects unreachable code) until a label that is jumped to is placed.
	dead bool
	// bad reports an expression that cannot be compiled; the rule keeps the general code.
	bad bool
}

type dvar struct{ name, typ string }

// dscope maps the capture names of a scope to variables; names are in the order of the frame's
// slots (the order in which the general code assigns them).
type dscope struct {
	vars  map[string]string
	names []string
}

func newDscope() *dscope { return &dscope{vars: map[string]string{}} }

func (d *dgen) decl(prefix, typ string) string {
	d.n++
	name := fmt.Sprintf("%s%d", prefix, d.n)
	d.decls = append(d.decls, dvar{name, typ})
	return name
}

// shared declares a variable shared by the whole method (ok, ch, size) once.
func (d *dgen) shared(name, typ string) string {
	for _, v := range d.decls {
		if v.name == name {
			return name
		}
	}
	d.decls = append(d.decls, dvar{name, typ})
	return name
}

// rd marks the variables v (or "nil") as read, unless the statement that reads them is dropped
// (the point is unreachable), and returns the first.
func (d *dgen) rd(v ...string) string {
	if !d.dead {
		for _, x := range v {
			d.reads[x] = true
		}
	}
	return v[0]
}

// rdTerm marks the capture variables (caps) that the term t reads as read: those generator.term
// writes for its captures outside lambdas that bind the name. (Searching the Go text for the
// variable's name would also find it in string literals, such as a variable or field named k1.)
func (d *dgen) rdTerm(t grammar.Term, caps map[string]string) {
	capRefs(t, func(name string) {
		if v, ok := caps[name]; ok {
			d.rd(v)
		}
	})
}

func (d *dgen) label() string {
	d.n++
	return fmt.Sprintf("L%d", d.n)
}

// line writes a statement unless the current point is unreachable.
func (d *dgen) line(format string, args ...any) {
	if !d.dead {
		fmt.Fprintf(&d.b, format+"\n", args...)
	}
}

// raw writes structure (braces) whatever the reachability.
func (d *dgen) raw(s string) { d.b.WriteString(s) }

// jump writes an unconditional jump to the label l.
func (d *dgen) jump(l string) {
	if d.dead {
		return
	}
	d.used[l] = true
	d.b.WriteString("goto " + l + "\n")
	d.dead = true
}

// exit writes break or continue.
func (d *dgen) exit(stmt string) {
	d.line("%s", stmt)
	d.dead = true
}

// failIf writes a jump to the label l, after the statements pre, when cond holds.
func (d *dgen) failIf(cond, l string, pre ...string) {
	if d.dead {
		return
	}
	d.used[l] = true
	fmt.Fprintf(&d.b, "if %s {\n", cond)
	for _, s := range pre {
		d.b.WriteString(s + "\n")
	}
	fmt.Fprintf(&d.b, "goto %s\n}\n", l)
}

// place places the label l if anything jumps to it, which makes the point reachable.
func (d *dgen) place(l string) {
	if d.used[l] {
		fmt.Fprintf(&d.b, "%s:\n", l)
		d.dead = false
	}
}

// ok is the variable that holds whether a call succeeded.
func (d *dgen) ok() string { return d.rd(d.shared("ok", "bool")) }

// capVar returns the variable of the capture name in the scope s.
func (d *dgen) capVar(s *dscope, name string) string {
	if v, ok := s.vars[name]; ok {
		return v
	}
	v := d.decl("k", "any")
	s.vars[name] = v
	return v
}

// capSets calls f with the names of the captures of the current scope that e may set, in the
// order the general code assigns their slots (captures of repetition elements with captures of
// their own are in the element's scope; see hasCaptures).
func capSets(e grammar.Expr, f func(string)) {
	switch e := e.(type) {
	case *grammar.Capture:
		f(e.Name)
		capSets(e.Expr, f)
	case *grammar.Seq:
		for _, it := range e.Items {
			capSets(it, f)
		}
	case *grammar.Choice:
		for _, alt := range e.Alts {
			capSets(alt, f)
		}
	case *grammar.Repeat:
		if !elementScoped(e.Expr) {
			capSets(e.Expr, f)
		}
	case *grammar.Optional:
		capSets(e.Expr, f)
	case *grammar.And:
		capSets(e.Expr, f)
	case *grammar.Not:
		capSets(e.Expr, f)
	case *grammar.Atomic:
		capSets(e.Expr, f)
	case *grammar.Discard:
		capSets(e.Expr, f)
	case *grammar.Attributed:
		capSets(e.Expr, f)
	}
}

// assigns reports whether e assigns a variable ([x = ...]), anywhere within it.
func assigns(e grammar.Expr) bool {
	found := false
	walkExpr(e, func(x grammar.Expr) {
		if p, ok := x.(*grammar.Predicate); ok {
			if _, ok := p.Term.(*grammar.Assign); ok {
				found = true
			}
		}
	})
	return found
}

// dmark is the state a backtracking point restores: what mark records, with the captures the
// expression may set instead of the trail.
type dmark struct {
	pos, rec, env string
	saves         [][2]string // capture variable, saved value
}

// mark saves the state before e; with saves, also the captures e may set in the scope s.
func (d *dgen) mark(s *dscope, e grammar.Expr, saves bool) dmark {
	m := dmark{pos: d.decl("x", "int"), rec: d.decl("x", "int")}
	d.line("%s, %s = p.pos, len(p.recovered)", m.pos, m.rec)
	if assigns(e) {
		m.env = d.decl("x", "*env")
		d.line("%s = p.env", m.env)
	}
	if saves && d.g.table == "trules" {
		seen := map[string]bool{}
		capSets(e, func(name string) {
			if seen[name] {
				return
			}
			seen[name] = true
			k := d.capVar(s, name)
			sv := d.decl("x", "any")
			d.line("%s = %s", sv, d.rd(k))
			m.saves = append(m.saves, [2]string{k, sv})
		})
	}
	return m
}

// reset restores the state saved by m.
func (d *dgen) reset(m dmark) {
	d.line("p.pos = %s", d.rd(m.pos))
	d.line("p.recovered = p.recovered[:%s]", d.rd(m.rec))
	if m.env != "" {
		d.line("p.env = %s", d.rd(m.env))
	}
	for _, kv := range m.saves {
		d.line("%s = %s", kv[0], d.rd(kv[1]))
	}
}

var chIdent = regexp.MustCompile(`\bch\b`)

// expr writes the statements of the expression e, which jump to the label fail when it fails, and
// returns the variable that holds its value afterwards ("nil" unless build). It mirrors
// generator.expr: build is passed down the same way, so the same rules evaluate their actions.
func (d *dgen) expr(e grammar.Expr, s *dscope, build bool, fail string) string {
	if d.dead || d.bad {
		return "nil"
	}
	g := d.g
	switch e := e.(type) {
	case *grammar.Literal:
		value := literalValue(e.Value)
		lit := g.name("lit")
		fmt.Fprintf(&g.vars, "var %s = []rune(%q)\n", lit, value)
		desc := g.desc(strconv.Quote(e.Value))
		var start string
		if build {
			start = d.decl("x", "int")
			d.line("%s = p.pos", start)
		}
		ok := d.ok()
		match := fmt.Sprintf("_, %s = p.%smatchLiteral(%s, %q, %d, false); !%s", ok, g.pick("", "parser."), lit, value, desc, ok)
		if rs := []rune(value); len(rs) > 0 && len(rs) <= maxInlineLiteral {
			// The code points compared in place (see peek); matchLiteral records the expectation
			// when they differ, and does the work in Bytes.
			cond := fmt.Sprintf("p.pos+%d <= len(p.in)", len(rs))
			if len(rs) == 1 {
				cond = "p.pos < len(p.in)"
			}
			for i, r := range rs {
				if i == 0 {
					cond += fmt.Sprintf(" && p.in[p.pos] == %d", r)
				} else {
					cond += fmt.Sprintf(" && p.in[p.pos+%d] == %d", i, r)
				}
			}
			if !d.dead {
				d.used[fail] = true
			}
			d.line("if %s {\n%s\n} else if %s {\ngoto %s\n}", cond, advance(len(rs)), match, fail)
		} else {
			d.failIf(match, fail)
		}
		if !build {
			return "nil"
		}
		v := d.decl("v", "any")
		text := fmt.Sprintf("%q", value)
		if strings.ContainsRune(value, '\uFFFD') {
			text = fmt.Sprintf("p.literalText(%s, %q)", d.rd(start), value)
		}
		d.line("%s = p.newMatch(%s, p.pos, %s, true)", v, d.rd(start), text)
		return v
	case *grammar.CharClass:
		ch, size, ok := d.rd(d.shared("ch", "rune")), d.rd(d.shared("size", "int")), d.ok()
		d.line("%s", peek(ch, size, ok))
		d.failIf(fmt.Sprintf("!%s || %s", ok, classReject(e)), fail, fmt.Sprintf("p.expect(p.pos, %d)", g.desc(charClassString(e))))
		return d.single(build)
	case *grammar.Any:
		size, ok := d.rd(d.shared("size", "int")), d.ok()
		d.line("%s", peek("_", size, ok))
		d.failIf("!"+ok, fail, "p.expect(p.pos, idAny)")
		return d.single(build)
	case *grammar.Ref:
		call, ok := g.refCall(e, build), d.ok()
		if !build {
			d.failIf(fmt.Sprintf("_, %s = %s; !%s", ok, call, ok), fail)
			return "nil"
		}
		v := d.decl("v", "any")
		d.failIf(fmt.Sprintf("%s, %s = %s; !%s", v, ok, call, ok), fail)
		return v
	case *grammar.Seq:
		var start, kids string
		if build {
			nvis := 0
			for _, it := range e.Items {
				if visible(it) {
					nvis++
				}
			}
			start, kids = d.decl("x", "int"), d.decl("x", "[]any")
			d.line("%s = p.pos", start)
			d.line("%s = p.newVals(%d)[:0]", kids, nvis)
		}
		for _, it := range e.Items {
			v := d.expr(it, s, build, fail)
			if build && visible(it) {
				d.line("%s = append(%s, %s)", kids, d.rd(kids), d.rd(v))
			}
		}
		if !build {
			return "nil"
		}
		v := d.decl("v", "any")
		d.line("%s = p.newNode(\"Seq\", %s, p.pos, %s)", v, d.rd(start), d.rd(kids))
		return v
	case *grammar.Choice:
		return d.choice(e, s, build, fail)
	case *grammar.Repeat:
		return d.repeat(e, s, build, fail)
	case *grammar.Optional:
		m := d.mark(s, e.Expr, true)
		var v string
		if build {
			v = d.decl("v", "any")
		}
		f, done := d.label(), d.label()
		x := d.expr(e.Expr, s, build, f)
		if build {
			d.line("%s = %s", v, d.rd(x))
		}
		if d.used[f] {
			d.jump(done)
			d.place(f)
			d.reset(m)
			if build {
				d.line("%s = nil", v)
			}
			d.place(done)
		}
		if !build {
			return "nil"
		}
		return v
	case *grammar.And:
		start := d.decl("x", "int")
		d.line("%s = p.pos", start)
		d.line("p.silent++")
		f, done := d.label(), d.label()
		d.expr(e.Expr, s, build && hasCaptures(e.Expr), f)
		d.line("p.silent--")
		d.line("p.pos = %s", d.rd(start))
		if d.used[f] {
			d.jump(done)
			d.place(f)
			d.line("p.silent--")
			d.line("p.pos = %s", start)
			d.jump(fail)
			d.place(done)
		}
		return "nil"
	case *grammar.Not:
		m := d.mark(s, e.Expr, true)
		d.line("p.silent++")
		f := d.label()
		d.expr(e.Expr, s, false, f)
		d.line("p.silent--")
		d.reset(m)
		d.jump(fail)
		d.place(f)
		d.line("p.silent--")
		d.reset(m)
		return "nil"
	case *grammar.Atomic:
		var start string
		if build {
			start = d.decl("x", "int")
			d.line("%s = p.pos", start)
		}
		d.expr(e.Expr, s, false, fail)
		if !build {
			return "nil"
		}
		v := d.decl("v", "any")
		d.line("%s = p.newMatch(%s, p.pos, p.text(%s, p.pos), true)", v, d.rd(start), start)
		return v
	case *grammar.Discard:
		d.expr(e.Expr, s, false, fail)
		return "nil"
	case *grammar.Capture:
		if g.table != "trules" {
			// Only value-free rules without predicates reach this emitter.
			return d.expr(e.Expr, s, false, fail)
		}
		k := d.capVar(s, e.Name)
		if _, ok := indexOf(s.names, e.Name); !ok {
			s.names = append(s.names, e.Name)
		}
		var v string
		if field := g.proj[e.Name]; field != "" {
			v = d.projectRepeat(e.Expr.(*grammar.Repeat), field, fail)
		} else {
			v = d.expr(e.Expr, s, true, fail)
		}
		d.line("%s = %s", k, d.rd(v))
		if !build {
			return "nil"
		}
		return v
	case *grammar.Top:
		if !build {
			return "nil"
		}
		v := d.decl("v", "any")
		d.line("%s = p.newMatch(p.pos, p.pos, \"\", true)", v)
		return v
	case *grammar.Bottom:
		d.jump(fail)
		return "nil"
	case *grammar.BeginInput:
		d.failIf("p.pos != 0", fail, "p.expect(p.pos, idBeginInput)")
		return "nil"
	case *grammar.EndInput:
		d.failIf("!p.atEnd()", fail, "p.expect(p.pos, idEndInput)")
		return "nil"
	case *grammar.BeginLine:
		d.failIf("!p.atLineStart()", fail, "p.expect(p.pos, idBeginLine)")
		return "nil"
	case *grammar.EndLine:
		d.failIf("!p.atLineEnd()", fail, "p.expect(p.pos, idEndLine)")
		return "nil"
	case *grammar.Predicate:
		return d.predicate(e, s, fail)
	case *grammar.Attributed:
		return d.attr(e, len(e.Attrs)-1, s, build, fail)
	}
	d.bad = true // a cut, or anything else the general code handles
	return "nil"
}

func indexOf(xs []string, x string) (int, bool) {
	for i, y := range xs {
		if y == x {
			return i, true
		}
	}
	return 0, false
}

// advance returns the statement that moves the position n units forward.
func advance(n int) string {
	if n == 1 {
		return "p.pos++"
	}
	return fmt.Sprintf("p.pos += %d", n)
}

// maxInlineLiteral is the longest literal (in code points) compared in place.
const maxInlineLiteral = 4

// peek returns the statement that assigns what p.peek returns to ch, size and ok, with the
// common case written out: the compiler does not inline peek. In Bytes, p.in is empty.
func peek(ch, size, ok string) string {
	return fmt.Sprintf("if p.pos < len(p.in) {\n%s, %s, %s = p.in[p.pos], 1, true\n} else {\n%[1]s, %[2]s, %[3]s = p.peek()\n}", ch, size, ok)
}

// single consumes the character whose size is in size (after a successful peek), as single.
func (d *dgen) single(build bool) string {
	if !build {
		d.line("p.pos += size")
		return "nil"
	}
	start, v := d.decl("x", "int"), d.decl("v", "any")
	d.line("%s = p.pos", start)
	d.line("p.pos += size")
	d.line("%s = p.newMatch(%s, p.pos, p.text(%s, p.pos), true)", v, d.rd(start), start)
	return v
}

func (d *dgen) choice(e *grammar.Choice, s *dscope, build bool, fail string) string {
	g := d.g
	m := d.mark(s, e, true)
	var v, ch, more string
	if build {
		v = d.decl("v", "any")
	}
	done := d.label()
	for _, alt := range e.Alts {
		if d.dead {
			break // the alternatives after one that cannot fail are never tried
		}
		skip := ""
		if cond, desc, depth, ok := g.first(alt, 0, map[string]bool{}); ok {
			// The alternative is skipped when the next character cannot start it (see first).
			if ch == "" {
				ch, more = d.decl("x", "rune"), d.decl("x", "bool")
				d.line("%s", peek(ch, "_", more))
			}
			skip = d.label()
			d.failIf(fmt.Sprintf("!(%s && (%s)) && p.depth+%d <= maxDepth", d.rd(more), chIdent.ReplaceAllString(cond, d.rd(ch)), depth), skip,
				fmt.Sprintf("p.expect(p.pos, %d)", desc))
		}
		f := d.label()
		x := d.expr(alt, s, build, f)
		if build {
			d.line("%s = %s", v, d.rd(x))
		}
		d.jump(done)
		d.place(f)
		d.reset(m)
		if skip != "" {
			d.place(skip)
		}
	}
	d.jump(fail)
	d.place(done)
	if !build {
		return "nil"
	}
	return v
}

// loop opens a repetition loop of at most max iterations (unbounded if max < 0).
func (d *dgen) loop(max int) {
	if max < 0 {
		d.raw("for {\n")
		return
	}
	n := d.decl("x", "int")
	d.rd(n)
	d.raw(fmt.Sprintf("for %s = 0; %s < %d; %s++ {\n", n, n, max, n))
}

// endLoop closes a loop; the point after it is reachable (every loop can break).
func (d *dgen) endLoop() {
	d.raw("}\n")
	d.dead = false
}

func (d *dgen) repeat(e *grammar.Repeat, s *dscope, build bool, fail string) string {
	if !build {
		if v, ok := d.scan(e, fail); ok {
			return v
		}
	}
	// An element with captures has its own scope per iteration (elementScoped).
	own := elementScoped(e.Expr)
	es := s
	if own {
		es = newDscope()
	}
	var start, base string
	if build {
		start, base = d.decl("x", "int"), d.decl("x", "int")
		d.line("%s, %s = p.pos, len(p.kidStack)", start, base)
	}
	count := d.decl("x", "int")
	d.line("%s = 0", count)
	d.loop(e.Max)
	m := d.mark(s, e.Expr, !own)
	if own && d.g.table == "trules" {
		d.clearScope(es, e.Expr)
	}
	f := d.label()
	v := d.expr(e.Expr, es, build, f)
	if own && build {
		attached := d.decl("v", "any")
		d.line("%s = p.attachCaptures(%s, %s, %s, %s, p.pos)", attached, d.rd(v), d.scopeVar(es), d.vals(es), d.rd(m.pos))
		v = attached
	}
	d.line("%s++", count)
	if build {
		d.line("p.kidStack = append(p.kidStack, %s)", d.rd(v))
	}
	d.line("if p.pos == %s {\nbreak\n}", d.rd(m.pos))
	if d.used[f] {
		d.exit("continue")
		d.place(f)
		d.reset(m)
		d.exit("break")
	}
	d.endLoop()
	if e.Min > 0 {
		if build {
			d.failIf(fmt.Sprintf("%s < %d", count, e.Min), fail, fmt.Sprintf("p.dropKids(%s)", d.rd(base)))
		} else {
			d.failIf(fmt.Sprintf("%s < %d", count, e.Min), fail)
		}
	}
	if !build {
		return "nil"
	}
	list := d.decl("v", "any")
	d.line("%s = p.newNode(\"List\", %s, p.pos, p.kids(%s))", list, d.rd(start), d.rd(base))
	return list
}

// clearScope sets the captures of the element e in its own scope es to nil (a new frame).
func (d *dgen) clearScope(es *dscope, e grammar.Expr) {
	seen := map[string]bool{}
	capSets(e, func(name string) {
		if !seen[name] {
			seen[name] = true
			d.line("%s = nil", d.capVar(es, name))
		}
	})
}

// scopeVar declares the capture names of the scope es as a variable of the package.
func (d *dgen) scopeVar(es *dscope) string {
	v := d.g.name("scope")
	fmt.Fprintf(&d.g.vars, "var %s = %s\n", v, goStrings(es.names))
	return v
}

// vals returns a list of the capture values of the scope es, in slot order.
func (d *dgen) vals(es *dscope) string {
	var xs []string
	for _, name := range es.names {
		xs = append(xs, d.rd(es.vars[name]))
	}
	return "[]any{" + strings.Join(xs, ", ") + "}"
}

// scan writes a value-free repetition of a single character (see generator.scanRepeat).
func (d *dgen) scan(e *grammar.Repeat, fail string) (string, bool) {
	var cond string
	switch x := e.Expr.(type) {
	case *grammar.CharClass:
		cond = fmt.Sprintf("%s\nif !%s || %s {\np.expect(p.pos, %d)", peek(d.rd(d.shared("ch", "rune")), d.rd(d.shared("size", "int")), d.ok()), d.ok(),
			classReject(x), d.g.desc(charClassString(x)))
	case *grammar.Any:
		cond = fmt.Sprintf("%s\nif !%s {\np.expect(p.pos, idAny)", peek("_", d.rd(d.shared("size", "int")), d.ok()), d.ok())
	default:
		return "", false
	}
	count := ""
	if e.Max >= 0 || e.Min > 0 {
		count = d.decl("x", "int")
		d.line("%s = 0", count)
	}
	if e.Max < 0 {
		d.raw("for {\n")
	} else {
		d.raw(fmt.Sprintf("for %s < %d {\n", d.rd(count), e.Max))
	}
	d.line("%s\nbreak\n}", cond)
	d.line("p.pos += size")
	if count != "" {
		d.line("%s++", count)
	}
	d.endLoop()
	if e.Min > 0 {
		d.failIf(fmt.Sprintf("%s < %d", d.rd(count), e.Min), fail)
	}
	return "nil", true
}

// projectRepeat writes a repetition that gathers the values of the capture field of its elements
// (see generator.projectRepeat).
func (d *dgen) projectRepeat(e *grammar.Repeat, field, fail string) string {
	es := newDscope()
	start, base, count := d.decl("x", "int"), d.decl("x", "int"), d.decl("x", "int")
	d.line("%s, %s, %s = p.pos, len(p.kidStack), 0", start, base, count)
	d.loop(e.Max)
	m := d.mark(es, e.Expr, false)
	d.clearScope(es, e.Expr)
	f := d.label()
	d.expr(e.Expr, es, false, f)
	fv, ok := es.vars[field]
	if !ok {
		d.bad = true
		return "nil"
	}
	d.line("%s++", count)
	d.line("p.kidStack = append(p.kidStack, %s)", d.rd(fv))
	d.line("if p.pos == %s {\nbreak\n}", d.rd(m.pos))
	if d.used[f] {
		d.exit("continue")
		d.place(f)
		d.reset(m)
		d.exit("break")
	}
	d.endLoop()
	if e.Min > 0 {
		d.failIf(fmt.Sprintf("%s < %d", count, e.Min), fail, fmt.Sprintf("p.dropKids(%s)", d.rd(base)))
	}
	v := d.decl("v", "any")
	d.line("%s = p.newNode(\"List\", %s, p.pos, p.kids(%s))", v, d.rd(start), d.rd(base))
	return v
}

// predicate writes a predicate or an assignment; it reads the captures of the scope s that are
// assigned so far, as the general code reads the frame.
func (d *dgen) predicate(e *grammar.Predicate, s *dscope, fail string) string {
	caps := map[string]string{}
	for _, name := range s.names {
		caps[name] = s.vars[name]
	}
	t := e.Term
	if a, ok := t.(*grammar.Assign); ok {
		t = a.Value
	}
	if !refsKnown(t, caps) {
		d.bad = true
		return "nil"
	}
	g := d.g
	prev := g.caps
	g.caps = caps
	term := g.term(t, newScope(), nil)
	d.rdTerm(t, caps)
	g.caps = prev
	if a, ok := e.Term.(*grammar.Assign); ok {
		d.failIf(fmt.Sprintf("!p.assign(%q, func(c *tctx) any { return %s })", a.Name, term), fail)
	} else {
		d.failIf(fmt.Sprintf("!p.predicate(func(c *tctx) any { return %s })", term), fail)
	}
	return "nil"
}

// refsKnown reports whether every capture the term t refers to (outside lambdas binding the
// name) is in caps.
func refsKnown(t grammar.Term, caps map[string]string) bool {
	ok := true
	capRefs(t, func(name string) {
		if _, known := caps[name]; !known {
			ok = false
		}
	})
	return ok
}

// capRefs calls f with the name of each capture the term t refers to outside lambdas that bind
// the name.
func capRefs(t grammar.Term, f func(name string)) {
	var walk func(t grammar.Term, shadow map[string]bool)
	walk = func(t grammar.Term, shadow map[string]bool) {
		switch t := t.(type) {
		case *grammar.CaptureRef:
			if !shadow[t.Name] {
				f(t.Name)
			}
		case *grammar.Lambda:
			inner := map[string]bool{}
			for k := range shadow {
				inner[k] = true
			}
			for _, p := range t.Params {
				inner[p] = true
			}
			walk(t.Body, inner)
		default:
			walkChildren(t, func(x grammar.Term) { walk(x, shadow) })
		}
	}
	walk(t, map[string]bool{})
}

// walkChildren calls f with the direct subterms of t (those walkTerm visits next).
func walkChildren(t grammar.Term, f func(grammar.Term)) {
	switch t := t.(type) {
	case *grammar.Member:
		f(t.X)
	case *grammar.New:
		for _, fi := range t.Fields {
			f(fi.Value)
		}
	case *grammar.Call:
		for _, a := range t.Args {
			f(a)
		}
	case *grammar.Lambda:
		f(t.Body)
	case *grammar.Binary:
		f(t.L)
		f(t.R)
	case *grammar.Unary:
		f(t.X)
	case *grammar.Assign:
		f(t.Value)
	}
}

// attr writes the expression of e under its attributes Attrs[:i+1] (the last one outermost, as
// generator.attributed wraps them).
func (d *dgen) attr(e *grammar.Attributed, i int, s *dscope, build bool, fail string) string {
	if i < 0 {
		return d.expr(e.Expr, s, build, fail)
	}
	a := e.Attrs[i]
	switch a.Name {
	case "error":
		// errorAttr: the expectations of a failing expression become the message.
		mark, far, exp := d.decl("x", "expMark"), d.decl("x", "int"), d.decl("x", "[]expID")
		d.line("%s = p.isolate(p.pos)", mark)
		f, done := d.label(), d.label()
		v := d.attr(e, i-1, s, build, f)
		msg := d.g.desc(a.Args[0].Value.(*grammar.Literal).Value)
		d.line("%s, %s = p.unisolate(%s)", far, exp, d.rd(mark))
		d.line("p.mergeExpected(%s, %s)", d.rd(far), d.rd(exp))
		if d.used[f] {
			d.jump(done)
			d.place(f)
			d.line("%s, %s = p.unisolate(%s)", far, exp, mark)
			d.line("p.expect(%s, msgBit|%d)", far, msg)
			d.jump(fail)
			d.place(done)
		}
		return v
	case "recover":
		d.bad = true
		return "nil"
	}
	return d.attr(e, i-1, s, build, fail)
}

// genSnapshot is the state of the generator a failed direct compilation rolls back.
type genSnapshot struct {
	vars  string
	n     int
	descs int
}

func (g *generator) snapshot() genSnapshot {
	return genSnapshot{vars: g.vars.String(), n: g.n, descs: len(g.descs)}
}

func (g *generator) rollback(s genSnapshot) {
	g.vars.Reset()
	g.vars.WriteString(s.vars)
	g.n = s.n
	for _, x := range g.descs[s.descs:] {
		delete(g.descIDs, x)
	}
	g.descs = g.descs[:s.descs]
}

// direct is a direct rule's compiled body: the declarations and statements of the method that
// calls it, up to the rule's value in v (success falls through; failure jumps to fail).
type direct struct {
	decls, body string
	fail        bool // the body can fail (fail is jumped to)
	succeed     bool // the body can succeed
	assigns     bool // the body assigns variables (the environment is restored)
	start, ctx  bool // the value uses start, the context c
}

// directRule compiles the rule r as a direct rule, or returns nil if it keeps the general code.
func (g *generator) directRule(r *rule) *direct {
	if g.table == "trules" && !directOK(r) || g.table != "trules" && !directLeanOK(r) {
		return nil
	}
	snap := g.snapshot()
	d := &dgen{g: g, reads: map[string]bool{}, used: map[string]bool{}}
	s := newDscope()
	g.cur, g.proj = r, g.projections(r)
	defer func() { g.proj = nil }()
	const fail = "fail"
	body := d.expr(r.def.Expr, s, !r.lean, fail)
	out := &direct{fail: d.used[fail], succeed: !d.dead, assigns: assigns(r.def.Expr)}
	if out.succeed && !d.bad {
		out.start, out.ctx = d.finish(r, s, body)
	}
	if d.bad {
		g.rollback(snap)
		return nil
	}
	out.body = d.b.String()
	var decls strings.Builder
	var unread []string
	for _, v := range d.decls {
		fmt.Fprintf(&decls, "\t\t%s %s\n", v.name, v.typ)
		if !d.reads[v.name] {
			unread = append(unread, v.name)
		}
	}
	out.decls = decls.String()
	if len(unread) > 0 {
		// Assigned but never read (such as a capture nothing reads).
		out.body = "\t" + strings.Repeat("_, ", len(unread)-1) + "_ = " + strings.Join(unread, ", ") + "\n" + out.body
	}
	return out
}

// finish writes what finish does for the rule r with the body's value body and the captures s,
// leaving the rule's value in v. It reports whether the value uses start and the context c.
func (d *dgen) finish(r *rule, s *dscope, body string) (start, ctx bool) {
	g := d.g
	switch {
	case r.novalue:
		d.line("v = nil")
	case r.action != nil:
		caps := map[string]string{}
		for _, name := range s.names {
			caps[name] = s.vars[name]
		}
		if !refsKnown(r.action, caps) {
			d.bad = true
			return false, false
		}
		d.line("c = p.useCtx(tctx{p: p, start: start, end: p.pos, cbase: len(p.created)})")
		if usesItems(r.action) {
			if r.bodyIsSeq {
				d.line("if n := asTval(%s); n != nil {\nc.items = n.tkids()\n} else {\nc.one[0] = nil\nc.items = c.one[:]\n}", d.rd(body))
			} else {
				d.line("c.one[0] = %s\nc.items = c.one[:]", d.rd(body))
			}
		}
		// The action's errors are reported with the rule's name (see tctx.result; actions do not
		// nest, so it need not be restored).
		d.line("p.where = %q", r.name)
		g.caps, g.final = caps, r.action
		action := g.term(r.action, newScope(), nil)
		d.rdTerm(r.action, caps)
		g.caps, g.final = nil, nil
		d.line("v = %s", action)
		if _, isNew := r.action.(*grammar.New); isNew {
			// A struct made with the rule's range (tmk_T with final): what result checks holds.
			// Structs made inside it are dropped from created, as result does.
			nested := 0
			walkTerm(r.action, func(t grammar.Term) {
				if _, ok := t.(*grammar.New); ok {
					nested++
				}
			})
			if nested > 1 {
				d.line("p.dropCreated(c.cbase)")
			}
		} else {
			d.line("v = c.finish(v)")
		}
		return true, true
	case r.terminalType != "":
		d.line("v = trules[%d].term(p, start, p.pos, p.text(start, p.pos))", r.id)
		return true, false
	default:
		if len(s.names) > 0 {
			d.line("v = p.attachCaptures(%s, %s, %s, start, p.pos)", d.rd(body), d.scopeVar(s), d.vals(s))
			start = true
		} else {
			d.line("v = %s", d.rd(body))
		}
		d.line("if n := asTval(v); n != nil {\nn.tsetFresh(false)\n}")
	}
	return start, false
}

// directMethod writes the method name that calls the direct rule r: as invokePlain does if plain
// (restoring the position and the recovered errors when it fails), otherwise as invoke does.
func (g *generator) directMethod(r *rule, d *direct, name, comment string, plain bool) {
	m := &g.methods
	fmt.Fprintf(m, "// %s, %s (body inlined)\nfunc (p *%s) %s() (%s, bool) {\n", r.name, comment, g.recv(), name, g.valType())
	start, rec := d.start || plain && d.fail, plain && d.fail
	m.WriteString("\tvar (\n")
	if start {
		m.WriteString("\t\tstart int\n")
	}
	if d.succeed {
		fmt.Fprintf(m, "\t\tv %s\n", g.valType())
	}
	if d.ctx {
		m.WriteString("\t\tc *tctx\n")
	}
	if rec {
		m.WriteString("\t\trec int\n")
	}
	if d.assigns {
		m.WriteString("\t\tprevEnv *env\n")
	}
	m.WriteString(d.decls)
	m.WriteString("\t)\n")
	switch {
	case rec:
		m.WriteString("\tstart, rec = p.pos, len(p.recovered)\n")
	case start:
		m.WriteString("\tstart = p.pos\n")
	}
	if d.assigns {
		m.WriteString("\tprevEnv = p.env\n")
	}
	m.WriteString("\tp.depth++\n\tif p.depth > maxDepth {\n\t\tp.tooDeep()\n\t}\n")
	m.WriteString(d.body)
	if d.succeed {
		m.WriteString("\tp.depth--\n")
		if d.assigns {
			m.WriteString("\tp.env = prevEnv\n")
		}
		m.WriteString("\treturn v, true\n")
	}
	if d.fail {
		m.WriteString("fail:\n\tp.depth--\n")
		if rec {
			m.WriteString("\tp.pos = start\n\tp.recovered = p.recovered[:rec]\n")
		}
		if d.assigns {
			m.WriteString("\tp.env = prevEnv\n")
		}
		m.WriteString("\treturn nil, false\n")
	}
	m.WriteString("}\n\n")
}

// directLeanBody provides the rule-table body for external entry and generic
// fallback calls. Its caller owns depth and failure rollback, so it adds neither.
func (g *generator) directLeanBody(r *rule, d *direct) string {
	var b strings.Builder
	b.WriteString("var (\n")
	if d.succeed {
		b.WriteString("v *Node\n")
	}
	b.WriteString(d.decls)
	b.WriteString(")\n")
	b.WriteString(d.body)
	if d.succeed {
		b.WriteString("return v, true\n")
	}
	if d.fail {
		b.WriteString("fail:\nreturn nil, false\n")
	}
	return g.method(r.name+" (value-free body inlined)", b.String())
}
