package engine

import (
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/value"
)

// closureBuilder builds the closure backend's form of a program: a matcher per expression, and the
// bodies, values and actions of the rules.
type closureBuilder struct {
	prog    *Program
	control *compileControl
	// plainDispatch leaves out the per-expression work charge (Options.controlledDispatch builds
	// the metered closure graph).
	plainDispatch bool
	// rules holds the rules the parser runs, by ID (staticRule.id). They are made when a body or a
	// call needs them and get the facts of their static rules in finish.
	rules []*rule
}

var _ exprBuilder[matcher] = (*closureBuilder)(nil)

// newClosureWalker returns the walker that builds the closure backend's form for the compiler c.
func newClosureWalker(c *compiler, plainDispatch bool) *walker[matcher] {
	return &walker[matcher]{compiler: c, b: &closureBuilder{prog: c.prog, control: c.control, plainDispatch: plainDispatch}}
}

// rule returns the rule the parser runs for the static rule r.
func (b *closureBuilder) rule(r *staticRule) *rule {
	for len(b.rules) <= r.id {
		b.rules = append(b.rules, nil)
	}
	if b.rules[r.id] == nil {
		b.rules[r.id] = &rule{}
	}
	return b.rules[r.id]
}

// finish gives the rules the facts that the compiler found out about them, once it is done, and
// returns them by ID.
func (b *closureBuilder) finish() []*rule {
	for _, rs := range [][]*staticRule{b.prog.rules, b.prog.twins} {
		for _, sr := range rs {
			r := b.rule(sr)
			r.staticRule = *sr
			r.initVals = b.prog.vars.valuesOf(sr.vars)
			if sr.nodeType != "" {
				r.nodeKinds = value.NewNodeTypeKinds(sr.nodeType)
			}
			if sr.opNodeType != "" {
				r.opKinds = value.NewNodeTypeKinds(sr.opNodeType)
			}
		}
	}
	return b.rules
}

func (b *closureBuilder) Step(m matcher) matcher {
	if !workControlsEnabled || b.plainDispatch {
		return m
	}
	return func(p *parser) (*value.Node, bool) { p.stepWork(); return m(p) }
}

func (b *closureBuilder) RuleBody(sr *staticRule, m matcher) {
	r := b.rule(sr)
	r.kinds = value.NewRuleKinds(sr.name)
	if sr.splice {
		r.value = func(_ *parser, v *value.Node, _ int) *value.Node { return v }
	} else {
		r.value = scopeValueOf(b.prog, sr.bodyExpr())
	}
	r.body = func(p *parser, _ int) (*value.Node, bool) { return m(p) }
	if sr.action != nil {
		r.act = termEvaluator(sr.action)
	}
}

func (b *closureBuilder) OperatorsRule(sr *staticRule, skip matcher, hasSkip bool, operands, operators []opLine[matcher]) {
	r := b.rule(sr)
	r.kinds = value.NewRuleKinds(sr.name)
	// The lines build the rule's value.
	r.value = func(_ *parser, v *value.Node, _ int) *value.Node { return v }
	pr := &operatorsRule{}
	r.operatorsRule = pr
	if hasSkip {
		pr.skip = skip
	}
	for _, l := range operands {
		pr.operands = append(pr.operands, b.line(l))
	}
	for _, l := range operators {
		o := &operatorsRuleOp{id: l.id, kind: l.kind, assoc: l.assoc, level: l.level, line: b.line(l)}
		if l.kind == grammar.Prefix {
			pr.prefix = append(pr.prefix, o)
		} else {
			pr.led = append(pr.led, o)
		}
	}
	r.body = func(p *parser, min int) (*value.Node, bool) { return p.operatorsRuleParse(r, min) }
}

func (b *closureBuilder) line(l opLine[matcher]) *operatorsRuleLine {
	pl := &operatorsRuleLine{scope: l.scope, m: l.m, key: l.key}
	pl.value, pl.single, pl.shape = scopeValueOf(b.prog, l.expr), singleModeOf(b.prog, l.expr), shaperOf(b.prog, l.expr)
	if l.action != nil {
		pl.act = termEvaluator(l.action)
	}
	return pl
}

func (b *closureBuilder) Literal(e *grammar.Literal, desc expID, build bool) matcher {
	val := value.LiteralValue(e.Value)
	if e.IgnoreCase {
		rs := foldedRunes([]rune(val))
		return func(p *parser) (*value.Node, bool) {
			start := p.pos
			if !p.matchLiteralFold(rs) {
				p.expect(start, desc)
				return nil, false
			}
			if !build {
				return nil, true
			}
			// The node holds the input as matched: its letters may differ in case from the literal.
			return p.newNode(value.Data{Kind: value.MatchKind, Start: int32(start), End: int32(p.pos), Text: p.text(start, p.pos), Terminal: true, Fresh: true}), true
		}
	}
	rs, bs := []rune(val), []byte(val)
	return func(p *parser) (*value.Node, bool) {
		start := p.pos
		if !p.matchLiteral(rs, bs) {
			p.expect(start, desc)
			return nil, false
		}
		if !build {
			return nil, true
		}
		return p.newNode(value.Data{Kind: value.MatchKind, Start: int32(start), End: int32(p.pos), Text: p.literalText(start, val), Terminal: true, Fresh: true}), true
	}
}

func (b *closureBuilder) Class(e *grammar.CharClass, desc expID, build bool) matcher {
	return single(build, desc, classAcceptControlled(e, b.control))
}

func (b *closureBuilder) Any(build bool) matcher {
	return single(build, idAny, func(*parser, rune) bool { return true })
}

func (b *closureBuilder) Call(sr *staticRule, min int) matcher {
	r := b.rule(sr)
	return func(p *parser) (*value.Node, bool) {
		if r.plain && !p.noPlain {
			return p.invokePlain(r, min) // what call does for such a rule, without its checks
		}
		return p.call(r, min)
	}
}

func (b *closureBuilder) Seq(ms []matcher, flat, valued []bool, n int, build bool) matcher {
	// The value is the tuple node of the elements, flattened through nested sequences.
	return func(p *parser) (*value.Node, bool) {
		start := p.pos
		var kids []*value.Node
		if build {
			kids = p.nodes(n)[:0]
		}
		for i, m := range ms {
			v, ok := m(p)
			if !ok {
				return nil, false
			}
			if !build {
				continue
			}
			switch {
			case flat[i]:
				kids = append(kids, value.D(v).Children...)
			case valued[i]:
				kids = append(kids, v)
			}
		}
		if !build {
			return nil, true
		}
		return p.newNode(value.Data{Kind: value.SeqKind, Start: int32(start), End: int32(p.pos), Children: kids, Fresh: true}), true
	}
}

func (b *closureBuilder) Choice(e *grammar.Choice, ms []matcher, firsts []first, build bool) matcher {
	guards := make([]firstGuard, len(e.Alts))
	guarded := false
	for i, alt := range e.Alts {
		if build {
			ms[i] = singleMatcher(b.prog, alt, ms[i])
		}
		guards[i] = b.guard(firsts[i])
		guarded = guarded || guards[i].accept != nil
	}
	if guarded {
		return func(p *parser) (*value.Node, bool) {
			m0 := p.mark()
			var ch rune
			more, peeked := false, false
			for i, m := range ms {
				if g := &guards[i]; g.accept != nil && p.missing == nil {
					if !peeked {
						ch, _, more = p.peek()
						peeked = true
					}
					if !(more && g.accept(p, ch)) && p.depth+g.depth <= p.maxDepth {
						p.stepWork()            // a skipped matcher still consumes dispatch work
						p.expect(p.pos, g.desc) // what the alternative would record
						continue
					}
				}
				prevCut := p.cut
				p.cut = false
				v, ok := m(p)
				cut := p.cut
				p.cut = prevCut
				if ok {
					return v, true
				}
				p.reset(m0)
				if cut {
					return nil, false
				}
			}
			return nil, false
		}
	}
	return func(p *parser) (*value.Node, bool) {
		m0 := p.mark()
		for _, m := range ms {
			prevCut := p.cut
			p.cut = false
			v, ok := m(p)
			cut := p.cut
			p.cut = prevCut
			if ok {
				return v, true
			}
			p.reset(m0)
			if cut {
				return nil, false
			}
		}
		return nil, false
	}
}

// firstGuard tells when an alternative of a choice can be skipped (see firstTerminal).
type firstGuard struct {
	accept func(*parser, rune) bool // nil if it cannot be
	desc   expID
	depth  int
}

// guard returns the test of the next character that the alternative beginning with f.term needs.
func (b *closureBuilder) guard(f first) firstGuard {
	switch t := f.term.(type) {
	case *grammar.Literal:
		r0, _ := utf8.DecodeRuneInString(t.Value)
		if t.IgnoreCase {
			f0 := foldASCIIRune(r0)
			return firstGuard{func(_ *parser, r rune) bool { return foldASCIIRune(r) == f0 }, f.desc, f.depth}
		}
		return firstGuard{func(_ *parser, r rune) bool { return r == r0 }, f.desc, f.depth}
	case *grammar.CharClass:
		return firstGuard{classAcceptControlled(t, b.control), f.desc, f.depth}
	}
	return firstGuard{}
}

func (b *closureBuilder) Optional(e *grammar.Optional, m matcher, build bool) matcher {
	if build {
		m = singleMatcher(b.prog, e.Expr, m)
	}
	return func(p *parser) (*value.Node, bool) {
		m0 := p.mark()
		prevCut := p.cut
		p.cut = false
		v, ok := m(p)
		cut := p.cut
		p.cut = prevCut
		if ok {
			return v, true
		}
		p.reset(m0)
		if cut {
			return nil, false
		}
		return nil, true
	}
}

func (b *closureBuilder) And(m matcher) matcher {
	return func(p *parser) (*value.Node, bool) {
		start, prevCut := p.pos, p.cut
		p.silent++
		_, ok := m(p)
		p.silent--
		p.cut = prevCut
		p.pos = start
		return nil, ok
	}
}

func (b *closureBuilder) Not(m matcher) matcher {
	return func(p *parser) (*value.Node, bool) {
		m0 := p.mark()
		prevCut := p.cut
		p.silent++
		_, ok := m(p)
		p.silent--
		p.cut = prevCut
		p.reset(m0)
		return nil, !ok
	}
}

func (b *closureBuilder) Atomic(m matcher, build bool) matcher {
	return func(p *parser) (*value.Node, bool) {
		start := p.pos
		if _, ok := m(p); !ok {
			return nil, false
		}
		if !build {
			return nil, true
		}
		return p.newNode(value.Data{Kind: value.MatchKind, Start: int32(start), End: int32(p.pos), Text: p.text(start, p.pos), Terminal: true, Fresh: true}), true
	}
}

func (b *closureBuilder) Discard(m matcher) matcher {
	return func(p *parser) (*value.Node, bool) {
		_, ok := m(p)
		return nil, ok
	}
}

func (b *closureBuilder) Capture(e *grammar.Capture, slot int, m matcher, projected, build bool) matcher {
	if !projected {
		m = captureMatcher(b.prog, e.Expr, m)
	}
	return func(p *parser) (*value.Node, bool) {
		v, ok := m(p)
		if ok {
			p.setCapture(slot, v)
		}
		if !build {
			return nil, ok
		}
		return v, ok
	}
}

func (b *closureBuilder) Cut() matcher {
	return func(p *parser) (*value.Node, bool) {
		p.cut = true
		return nil, true
	}
}

func (b *closureBuilder) Empty() matcher {
	// _ matches the empty input and has no value.
	return func(p *parser) (*value.Node, bool) { return nil, true }
}

func (b *closureBuilder) Fail() matcher {
	return func(p *parser) (*value.Node, bool) { return nil, false }
}

func (b *closureBuilder) Anchor(e grammar.Expr, desc expID) matcher {
	switch e.(type) {
	case *grammar.BeginInput:
		return anchor(func(p *parser) bool { return p.atInputStart() }, desc)
	case *grammar.EndInput:
		return anchor(func(p *parser) bool { return p.atEOF() }, desc)
	case *grammar.BeginLine:
		return anchor(func(p *parser) bool { return p.atLineStart() }, desc)
	}
	return anchor(func(p *parser) bool {
		ch, _, ok := p.peek()
		return !ok || ch == '\n' || ch == '\r'
	}, desc)
}

func anchor(cond func(p *parser) bool, desc expID) matcher {
	return func(p *parser) (*value.Node, bool) {
		if cond(p) {
			return nil, true
		}
		p.expect(p.pos, desc)
		return nil, false
	}
}

// single returns a matcher of one character that accept accepts.
func single(build bool, desc expID, accept func(*parser, rune) bool) matcher {
	return func(p *parser) (*value.Node, bool) {
		ch, size, ok := p.peek()
		if !ok || !accept(p, ch) {
			p.expect(p.pos, desc)
			return nil, false
		}
		start := p.pos
		p.pos += size
		if !build {
			return nil, true
		}
		return p.newNode(value.Data{Kind: value.MatchKind, Start: int32(start), End: int32(p.pos), Text: p.text(start, p.pos), Terminal: true, Fresh: true}), true
	}
}

func (b *closureBuilder) Predicate(e *grammar.Predicate, s *scope) matcher {
	if a, ok := e.Term.(*grammar.Bind); ok {
		return func(p *parser) (*value.Node, bool) {
			ctx := p.useCtx(evalCtx{p: p, scope: s, frame: p.frame, start: p.pos, end: p.pos, cbase: len(p.created)})
			v, err := ctx.eval(a.Value)
			p.dropCreated(ctx.cbase)
			if err != nil {
				return nil, false
			}
			if _, isFn := v.(*closure); isFn {
				return nil, false
			}
			p.bind(a.Name, v)
			return nil, true
		}
	}
	return func(p *parser) (*value.Node, bool) {
		ctx := p.useCtx(evalCtx{p: p, scope: s, frame: p.frame, start: p.pos, end: p.pos, cbase: len(p.created)})
		v, err := ctx.eval(e.Term)
		p.dropCreated(ctx.cbase)
		if err != nil {
			return nil, false
		}
		if b, ok := v.(bool); ok && !b {
			return nil, false
		}
		return nil, true
	}
}

// ScanRepeat advances over the repetition without saving and restoring state per element.
// Expectations are recorded as for a general repetition (once, at the position that did not match
// last).
func (b *closureBuilder) ScanRepeat(e *grammar.Repeat, desc expID) matcher {
	accept := func(*parser, rune) bool { return true }
	if x, ok := e.Expr.(*grammar.CharClass); ok {
		accept = classAcceptControlled(x, b.control)
	}
	min, max := e.Min, e.Max
	return func(p *parser) (*value.Node, bool) {
		count := 0
		for max < 0 || count < max {
			ch, size, ok := p.peek()
			if !ok || !accept(p, ch) {
				p.expect(p.pos, desc)
				break
			}
			p.pos += size
			count++
		}
		return nil, count >= min
	}
}

func (b *closureBuilder) Repeat(e *grammar.Repeat, m matcher, elemScope *scope, ownScope, build, stream bool, site int, shiftable bool) matcher {
	min, max := e.Min, e.Max
	elemValue := scopeValueOf(b.prog, e.Expr)
	var rs *runSite
	if site != 0 {
		rs = &runSite{id: site, m: m, scope: elemScope, ownScope: ownScope, build: build, min: min, max: max, shiftable: shiftable, value: elemValue}
	}
	return func(p *parser) (*value.Node, bool) {
		if rs != nil && p.runs != nil && p.silent == 0 {
			return p.resumeRepeat(rs)
		}
		start := p.pos
		base := len(p.kidStack) // collect the element values
		if streamPrefixIsolation && stream && p.emit != nil && p.depth == 1 {
			p.startStreamChunks()
		}
		count := 0
		for n := 0; max < 0 || n < max; n++ {
			m0 := p.mark()
			prevCut, prevFrame := p.cut, p.frame
			p.cut = false
			var f *frame
			if ownScope {
				f = p.newFrame(len(elemScope.names))
				p.frame = f
			}
			v, ok := m(p)
			cut := p.cut
			p.cut, p.frame = prevCut, prevFrame
			if !ok {
				p.reset(m0)
				if cut {
					p.kids(base)
					return nil, false
				}
				break
			}
			if build {
				v = elemValue(p, v, m0.pos)
				if ownScope {
					v = p.attachCaptures(v, elemScope, f, m0.pos, p.pos)
				}
			}
			count++
			if stream && p.emit != nil && p.depth == 1 {
				// Elements of the start rule's #stream are passed on without being retained, and committed.
				if v != nil {
					value.D(v).Fresh = false
				}
				if err := p.emitStream(v); err != nil {
					panic(fatal{err})
				}
				p.commit(p.pos)
				if p.pos == m0.pos {
					break
				}
				continue
			}
			if build {
				p.kidStack = append(p.kidStack, v)
			}
			if p.pos == m0.pos {
				// Stop a repetition that consumes no input.
				break
			}
		}
		if count < min {
			p.kids(base)
			return nil, false
		}
		if !build {
			return nil, true
		}
		return p.newNode(value.Data{Kind: value.ListKind, Start: int32(start), End: int32(p.pos), Children: p.kids(base), Fresh: true}), true
	}
}

// ProjectRepeat gathers the values of a capture of the elements, which share one capture frame:
// nothing refers to an element's frame once its value is taken.
func (b *closureBuilder) ProjectRepeat(e *grammar.Repeat, m matcher, slot, n int) matcher {
	min, max := e.Min, e.Max
	return func(p *parser) (*value.Node, bool) {
		start := p.pos
		base := len(p.kidStack)
		f := p.newFrame(n)
		count := 0
		for i := 0; max < 0 || i < max; i++ {
			clearWork(p, f.vals)
			m0 := p.mark()
			prevCut, prevFrame := p.cut, p.frame
			p.cut, p.frame = false, f
			_, ok := m(p)
			cut := p.cut
			p.cut, p.frame = prevCut, prevFrame
			if !ok {
				p.reset(m0)
				if cut {
					p.kids(base)
					return nil, false
				}
				break
			}
			count++
			p.kidStack = append(p.kidStack, f.vals[slot])
			if p.pos == m0.pos {
				break
			}
		}
		if count < min {
			p.kids(base)
			return nil, false
		}
		return p.newNode(value.Data{Kind: value.ListKind, Start: int32(start), End: int32(p.pos), Children: p.kids(base), Fresh: true}), true
	}
}

func (b *closureBuilder) Missing(m matcher, site int, expected string, build bool) matcher {
	return missingLiteral(m, site, expected, build)
}

func (b *closureBuilder) ErrorMessage(m matcher, msg expID, missingScope bool) matcher {
	m = errorAttr(m, msg)
	if missingScope {
		m = missingErrorScope(m, msg)
	}
	return m
}

func (b *closureBuilder) Diagnostic(m matcher, name expID) matcher {
	return diagnosticAttr(m, name)
}

func (b *closureBuilder) Recover(e grammar.Expr, m, skip matcher, build bool) matcher {
	// What recovers is one element: the value of the expression, or the error.
	if build {
		m = captureMatcher(b.prog, e, m)
	}
	return recoverAttr(m, skip, build)
}
