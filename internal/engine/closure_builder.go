package engine

import (
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/value"
)

// closureBuilder builds the closure backend's form of a program: a matcher per expression, and the
// bodies, values and actions of the rules.
type closureBuilder struct {
	prog    *compile.Program
	control *compile.Control
	// plainDispatch leaves out the per-expression work charge (Options.ControlledDispatch builds
	// the metered closure graph).
	plainDispatch bool
	// rules holds the rules the parser runs, by ID (staticRule.ID). They are made when a body or a
	// call needs them and get the facts of their static rules in finish.
	rules []*core.Rule
}

var _ compile.ExprBuilder[core.Matcher] = (*closureBuilder)(nil)

// newClosureBuilder returns the builder of the closure backend's form for the compiler c.
func newClosureBuilder(c *compile.Compiler, plainDispatch bool) *closureBuilder {
	return &closureBuilder{prog: c.Prog, control: c.Control, plainDispatch: plainDispatch}
}

// rule returns the rule the parser runs for the static rule r.
func (b *closureBuilder) rule(r *compile.Rule) *core.Rule {
	for len(b.rules) <= r.ID {
		b.rules = append(b.rules, nil)
	}
	if b.rules[r.ID] == nil {
		b.rules[r.ID] = &core.Rule{}
	}
	return b.rules[r.ID]
}

// finish gives the rules the facts that the compiler found out about them, once it is done, and
// returns them by ID.
func (b *closureBuilder) finish() []*core.Rule {
	for _, rs := range [][]*compile.Rule{b.prog.RuleList, b.prog.Twins} {
		for _, sr := range rs {
			r := b.rule(sr)
			*core.StaticRuleOf(r) = *sr
			r.InitVals = b.prog.Vars.ValuesOf(sr.Vars)
			if sr.NodeType != "" {
				r.NodeKinds = value.NewNodeTypeKinds(sr.NodeType)
			}
			if sr.OpNodeType != "" {
				r.OpKinds = value.NewNodeTypeKinds(sr.OpNodeType)
			}
		}
	}
	return b.rules
}

func (b *closureBuilder) Step(m core.Matcher) core.Matcher {
	if !core.WorkControlsEnabled || b.plainDispatch {
		return m
	}
	return func(p *core.Parser) (*value.Node, bool) { p.StepWork(); return m(p) }
}

func (b *closureBuilder) RuleBody(sr *compile.Rule, m core.Matcher) {
	r := b.rule(sr)
	r.Kinds = value.NewRuleKinds(sr.Name)
	if sr.Splice {
		r.Value = func(_ *core.Parser, v *value.Node, _ int) *value.Node { return v }
	} else {
		r.Value = scopeValueOf(b.prog, sr.BodyExpr())
	}
	r.Body = func(p *core.Parser, _ int) (*value.Node, bool) { return m(p) }
	if sr.Action != nil {
		r.Act = termEvaluator(sr.Action)
	}
}

func (b *closureBuilder) OperatorsRule(sr *compile.Rule, skip core.Matcher, hasSkip bool, operands, operators []compile.OpLine[core.Matcher]) {
	r := b.rule(sr)
	r.Kinds = value.NewRuleKinds(sr.Name)
	// The lines build the rule's value.
	r.Value = func(_ *core.Parser, v *value.Node, _ int) *value.Node { return v }
	pr := &core.OperatorsRule{}
	r.OperatorsRule = pr
	if hasSkip {
		pr.Skip = skip
	}
	for _, l := range operands {
		pr.Operands = append(pr.Operands, b.line(l))
	}
	for _, l := range operators {
		o := &core.OperatorsRuleOp{Id: l.ID, Kind: l.Kind, Assoc: l.Assoc, Level: l.Level, Line: b.line(l)}
		if l.Kind == grammar.Prefix {
			pr.Prefix = append(pr.Prefix, o)
		} else {
			pr.Led = append(pr.Led, o)
		}
	}
	r.Body = func(p *core.Parser, min int) (*value.Node, bool) { return p.OperatorsRuleParse(r, min) }
}

func (b *closureBuilder) line(l compile.OpLine[core.Matcher]) *core.OperatorsRuleLine {
	pl := &core.OperatorsRuleLine{Scope: l.Scope, M: l.Form, Key: l.Key}
	pl.Value, pl.Single, pl.Shape = scopeValueOf(b.prog, l.Expr), compile.SingleModeOf(b.prog, l.Expr), compile.ShaperOf(b.prog, l.Expr)
	if l.Action != nil {
		pl.Act = termEvaluator(l.Action)
	}
	return pl
}

func (b *closureBuilder) Literal(e *grammar.Literal, desc core.ExpID, build bool) core.Matcher {
	val := value.LiteralValue(e.Value)
	if e.IgnoreCase {
		rs := compile.FoldedRunes([]rune(val))
		return func(p *core.Parser) (*value.Node, bool) {
			start := p.Pos
			if !p.MatchLiteralFold(rs) {
				p.Expect(start, desc)
				return nil, false
			}
			if !build {
				return nil, true
			}
			// The node holds the input as matched: its letters may differ in case from the literal.
			return p.NewNode(value.Data{Kind: value.MatchKind, Start: int32(start), End: int32(p.Pos), Text: p.Text(start, p.Pos), Terminal: true, Fresh: true}), true
		}
	}
	rs, bs := []rune(val), []byte(val)
	return func(p *core.Parser) (*value.Node, bool) {
		start := p.Pos
		if !p.MatchLiteral(rs, bs) {
			p.Expect(start, desc)
			return nil, false
		}
		if !build {
			return nil, true
		}
		return p.NewNode(value.Data{Kind: value.MatchKind, Start: int32(start), End: int32(p.Pos), Text: p.LiteralText(start, val), Terminal: true, Fresh: true}), true
	}
}

func (b *closureBuilder) Class(e *grammar.CharClass, desc core.ExpID, build bool) core.Matcher {
	return single(build, desc, classAcceptControlled(e, b.control))
}

func (b *closureBuilder) Any(build bool) core.Matcher {
	return single(build, compile.IDAny, func(*core.Parser, rune) bool { return true })
}

func (b *closureBuilder) Call(sr *compile.Rule, min int) core.Matcher {
	r := b.rule(sr)
	return func(p *core.Parser) (*value.Node, bool) {
		if r.Plain && !p.NoPlain {
			return p.InvokePlain(r, min) // what call does for such a rule, without its checks
		}
		return p.Call(r, min)
	}
}

func (b *closureBuilder) Seq(ms []core.Matcher, flat, valued []bool, n int, build bool) core.Matcher {
	// The value is the tuple node of the elements, flattened through nested sequences.
	return func(p *core.Parser) (*value.Node, bool) {
		start := p.Pos
		var kids []*value.Node
		if build {
			kids = p.Nodes(n)[:0]
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
		return p.NewNode(value.Data{Kind: value.SeqKind, Start: int32(start), End: int32(p.Pos), Children: kids, Fresh: true}), true
	}
}

func (b *closureBuilder) Choice(e *grammar.Choice, ms []core.Matcher, firsts []compile.First, build bool) core.Matcher {
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
		return func(p *core.Parser) (*value.Node, bool) {
			m0 := p.Mark()
			var ch rune
			more, peeked := false, false
			for i, m := range ms {
				if g := &guards[i]; g.accept != nil && p.Missing == nil {
					if !peeked {
						ch, _, more = p.Peek()
						peeked = true
					}
					if !(more && g.accept(p, ch)) && p.Depth+g.depth <= p.MaxDepth {
						p.StepWork()            // a skipped matcher still consumes dispatch work
						p.Expect(p.Pos, g.desc) // what the alternative would record
						continue
					}
				}
				prevCut := p.Cut
				p.Cut = false
				v, ok := m(p)
				cut := p.Cut
				p.Cut = prevCut
				if ok {
					return v, true
				}
				p.Reset(m0)
				if cut {
					return nil, false
				}
			}
			return nil, false
		}
	}
	return func(p *core.Parser) (*value.Node, bool) {
		m0 := p.Mark()
		for _, m := range ms {
			prevCut := p.Cut
			p.Cut = false
			v, ok := m(p)
			cut := p.Cut
			p.Cut = prevCut
			if ok {
				return v, true
			}
			p.Reset(m0)
			if cut {
				return nil, false
			}
		}
		return nil, false
	}
}

// firstGuard tells when an alternative of a choice can be skipped (see firstTerminal).
type firstGuard struct {
	accept func(*core.Parser, rune) bool // nil if it cannot be
	desc   core.ExpID
	depth  int
}

// guard returns the test of the next character that the alternative beginning with f.term needs.
func (b *closureBuilder) guard(f compile.First) firstGuard {
	switch t := f.Term.(type) {
	case *grammar.Literal:
		r0, _ := utf8.DecodeRuneInString(t.Value)
		if t.IgnoreCase {
			f0 := compile.FoldASCIIRune(r0)
			return firstGuard{func(_ *core.Parser, r rune) bool { return compile.FoldASCIIRune(r) == f0 }, f.Desc, f.Depth}
		}
		return firstGuard{func(_ *core.Parser, r rune) bool { return r == r0 }, f.Desc, f.Depth}
	case *grammar.CharClass:
		return firstGuard{classAcceptControlled(t, b.control), f.Desc, f.Depth}
	}
	return firstGuard{}
}

func (b *closureBuilder) Optional(e *grammar.Optional, m core.Matcher, build bool) core.Matcher {
	if build {
		m = singleMatcher(b.prog, e.Expr, m)
	}
	return func(p *core.Parser) (*value.Node, bool) {
		m0 := p.Mark()
		prevCut := p.Cut
		p.Cut = false
		v, ok := m(p)
		cut := p.Cut
		p.Cut = prevCut
		if ok {
			return v, true
		}
		p.Reset(m0)
		if cut {
			return nil, false
		}
		return nil, true
	}
}

func (b *closureBuilder) And(m core.Matcher) core.Matcher {
	return func(p *core.Parser) (*value.Node, bool) {
		start, prevCut := p.Pos, p.Cut
		p.Silent++
		_, ok := m(p)
		p.Silent--
		p.Cut = prevCut
		p.Pos = start
		return nil, ok
	}
}

func (b *closureBuilder) Not(m core.Matcher) core.Matcher {
	return func(p *core.Parser) (*value.Node, bool) {
		m0 := p.Mark()
		prevCut := p.Cut
		p.Silent++
		_, ok := m(p)
		p.Silent--
		p.Cut = prevCut
		p.Reset(m0)
		return nil, !ok
	}
}

func (b *closureBuilder) Atomic(m core.Matcher, build bool) core.Matcher {
	return func(p *core.Parser) (*value.Node, bool) {
		start := p.Pos
		if _, ok := m(p); !ok {
			return nil, false
		}
		if !build {
			return nil, true
		}
		return p.NewNode(value.Data{Kind: value.MatchKind, Start: int32(start), End: int32(p.Pos), Text: p.Text(start, p.Pos), Terminal: true, Fresh: true}), true
	}
}

func (b *closureBuilder) Discard(m core.Matcher) core.Matcher {
	return func(p *core.Parser) (*value.Node, bool) {
		_, ok := m(p)
		return nil, ok
	}
}

func (b *closureBuilder) Capture(e *grammar.Capture, slot int, m core.Matcher, projected, build bool) core.Matcher {
	if !projected {
		m = captureMatcher(b.prog, e.Expr, m)
	}
	return func(p *core.Parser) (*value.Node, bool) {
		v, ok := m(p)
		if ok {
			p.SetCapture(slot, v)
		}
		if !build {
			return nil, ok
		}
		return v, ok
	}
}

func (b *closureBuilder) Cut() core.Matcher {
	return func(p *core.Parser) (*value.Node, bool) {
		p.Cut = true
		return nil, true
	}
}

func (b *closureBuilder) Empty() core.Matcher {
	// _ matches the empty input and has no value.
	return func(p *core.Parser) (*value.Node, bool) { return nil, true }
}

func (b *closureBuilder) Fail() core.Matcher {
	return func(p *core.Parser) (*value.Node, bool) { return nil, false }
}

func (b *closureBuilder) Anchor(e grammar.Expr, desc core.ExpID) core.Matcher {
	switch e.(type) {
	case *grammar.BeginInput:
		return anchor(func(p *core.Parser) bool { return p.AtInputStart() }, desc)
	case *grammar.EndInput:
		return anchor(func(p *core.Parser) bool { return p.AtEOF() }, desc)
	case *grammar.BeginLine:
		return anchor(func(p *core.Parser) bool { return p.AtLineStart() }, desc)
	}
	return anchor(func(p *core.Parser) bool {
		ch, _, ok := p.Peek()
		return !ok || ch == '\n' || ch == '\r'
	}, desc)
}

func anchor(cond func(p *core.Parser) bool, desc core.ExpID) core.Matcher {
	return func(p *core.Parser) (*value.Node, bool) {
		if cond(p) {
			return nil, true
		}
		p.Expect(p.Pos, desc)
		return nil, false
	}
}

// single returns a matcher of one character that accept accepts.
func single(build bool, desc core.ExpID, accept func(*core.Parser, rune) bool) core.Matcher {
	return func(p *core.Parser) (*value.Node, bool) {
		ch, size, ok := p.Peek()
		if !ok || !accept(p, ch) {
			p.Expect(p.Pos, desc)
			return nil, false
		}
		start := p.Pos
		p.Pos += size
		if !build {
			return nil, true
		}
		return p.NewNode(value.Data{Kind: value.MatchKind, Start: int32(start), End: int32(p.Pos), Text: p.Text(start, p.Pos), Terminal: true, Fresh: true}), true
	}
}

func (b *closureBuilder) Predicate(e *grammar.Predicate, s *compile.Scope) core.Matcher {
	if a, ok := e.Term.(*grammar.Bind); ok {
		return func(p *core.Parser) (*value.Node, bool) {
			ctx := p.UseCtx(core.EvalCtx{P: p, Scope: s, Frame: p.Frame, Start: p.Pos, End: p.Pos, Cbase: len(p.Created)})
			v, err := evalTerm(ctx, a.Value)
			p.DropCreated(ctx.Cbase)
			if err != nil {
				return nil, false
			}
			if _, isFn := v.(*closure); isFn {
				return nil, false
			}
			p.Bind(a.Name, v)
			return nil, true
		}
	}
	return func(p *core.Parser) (*value.Node, bool) {
		ctx := p.UseCtx(core.EvalCtx{P: p, Scope: s, Frame: p.Frame, Start: p.Pos, End: p.Pos, Cbase: len(p.Created)})
		v, err := evalTerm(ctx, e.Term)
		p.DropCreated(ctx.Cbase)
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
func (b *closureBuilder) ScanRepeat(e *grammar.Repeat, desc core.ExpID) core.Matcher {
	accept := func(*core.Parser, rune) bool { return true }
	if x, ok := e.Expr.(*grammar.CharClass); ok {
		accept = classAcceptControlled(x, b.control)
	}
	min, max := e.Min, e.Max
	return func(p *core.Parser) (*value.Node, bool) {
		count := 0
		for max < 0 || count < max {
			ch, size, ok := p.Peek()
			if !ok || !accept(p, ch) {
				p.Expect(p.Pos, desc)
				break
			}
			p.Pos += size
			count++
		}
		return nil, count >= min
	}
}

func (b *closureBuilder) Repeat(e *grammar.Repeat, m core.Matcher, elemScope *compile.Scope, ownScope, build, stream bool, site int, shiftable bool) core.Matcher {
	min, max := e.Min, e.Max
	elemValue := scopeValueOf(b.prog, e.Expr)
	var rs *core.RunSite
	if site != 0 {
		rs = &core.RunSite{Id: site, M: m, Scope: elemScope, OwnScope: ownScope, Build: build, Min: min, Max: max, Shiftable: shiftable, Value: elemValue}
	}
	return func(p *core.Parser) (*value.Node, bool) {
		if rs != nil && p.Runs != nil && p.Silent == 0 {
			return p.ResumeRepeat(rs)
		}
		start := p.Pos
		base := len(p.KidStack) // collect the element values
		if core.StreamPrefixIsolation && stream && p.Emit != nil && p.Depth == 1 {
			p.StartStreamChunks()
		}
		count := 0
		for n := 0; max < 0 || n < max; n++ {
			m0 := p.Mark()
			prevCut, prevFrame := p.Cut, p.Frame
			p.Cut = false
			var f *core.Frame
			if ownScope {
				f = p.NewFrame(len(elemScope.Names))
				p.Frame = f
			}
			v, ok := m(p)
			cut := p.Cut
			p.Cut, p.Frame = prevCut, prevFrame
			if !ok {
				p.Reset(m0)
				if cut {
					p.Kids(base)
					return nil, false
				}
				break
			}
			if build {
				v = elemValue(p, v, m0.Pos)
				if ownScope {
					v = p.AttachCaptures(v, elemScope, f, m0.Pos, p.Pos)
				}
			}
			count++
			if stream && p.Emit != nil && p.Depth == 1 {
				// Elements of the start rule's #stream are passed on without being retained, and committed.
				if v != nil {
					value.D(v).Fresh = false
				}
				if err := p.EmitStream(v); err != nil {
					panic(core.Fatal{Err: err})
				}
				p.Commit(p.Pos)
				if p.Pos == m0.Pos {
					break
				}
				continue
			}
			if build {
				p.KidStack = append(p.KidStack, v)
			}
			if p.Pos == m0.Pos {
				// Stop a repetition that consumes no input.
				break
			}
		}
		if count < min {
			p.Kids(base)
			return nil, false
		}
		if !build {
			return nil, true
		}
		return p.NewNode(value.Data{Kind: value.ListKind, Start: int32(start), End: int32(p.Pos), Children: p.Kids(base), Fresh: true}), true
	}
}

// ProjectRepeat gathers the values of a capture of the elements, which share one capture frame:
// nothing refers to an element's frame once its value is taken.
func (b *closureBuilder) ProjectRepeat(e *grammar.Repeat, m core.Matcher, slot, n int) core.Matcher {
	min, max := e.Min, e.Max
	return func(p *core.Parser) (*value.Node, bool) {
		start := p.Pos
		base := len(p.KidStack)
		f := p.NewFrame(n)
		count := 0
		for i := 0; max < 0 || i < max; i++ {
			core.ClearWork(p, f.Vals)
			m0 := p.Mark()
			prevCut, prevFrame := p.Cut, p.Frame
			p.Cut, p.Frame = false, f
			_, ok := m(p)
			cut := p.Cut
			p.Cut, p.Frame = prevCut, prevFrame
			if !ok {
				p.Reset(m0)
				if cut {
					p.Kids(base)
					return nil, false
				}
				break
			}
			count++
			p.KidStack = append(p.KidStack, f.Vals[slot])
			if p.Pos == m0.Pos {
				break
			}
		}
		if count < min {
			p.Kids(base)
			return nil, false
		}
		return p.NewNode(value.Data{Kind: value.ListKind, Start: int32(start), End: int32(p.Pos), Children: p.Kids(base), Fresh: true}), true
	}
}

func (b *closureBuilder) Missing(m core.Matcher, site int, expected string, build bool) core.Matcher {
	return core.MissingLiteral(m, site, expected, build)
}

func (b *closureBuilder) ErrorMessage(m core.Matcher, msg core.ExpID, missingScope bool) core.Matcher {
	m = errorAttr(m, msg)
	if missingScope {
		m = core.MissingErrorScope(m, msg)
	}
	return m
}

func (b *closureBuilder) Diagnostic(m core.Matcher, name core.ExpID) core.Matcher {
	return diagnosticAttr(m, name)
}

func (b *closureBuilder) Recover(e grammar.Expr, m, skip core.Matcher, build bool) core.Matcher {
	// What recovers is one element: the value of the expression, or the error.
	if build {
		m = captureMatcher(b.prog, e, m)
	}
	return recoverAttr(m, skip, build)
}

// errorAttr replaces the expectations of a failed expression with the message msg (an ID with
// MsgBit set). The message is reported at the farthest position reached within the expression.
func errorAttr(m core.Matcher, msg core.ExpID) core.Matcher {
	return func(p *core.Parser) (*value.Node, bool) {
		v, ok, far, expected := core.Tracked(p, m)
		if ok {
			p.MergeExpected(far, expected)
			return v, true
		}
		p.Expect(far, msg)
		return nil, false
	}
}

// diagnosticAttr names ordinary expectations of a failure at the expression boundary.
// Explicit messages and the tracked failure position remain unchanged.
func diagnosticAttr(m core.Matcher, name core.ExpID) core.Matcher {
	return func(p *core.Parser) (*value.Node, bool) {
		v, ok, far, expected := core.Tracked(p, m)
		if ok {
			p.MergeExpected(far, expected)
			return v, true
		}
		p.MergeExpected(far, core.RenamedExpected(p, expected, name))
		return nil, false
	}
}

// recoverAttr records an error when the expression fails, skips the input matched by skip, and
// returns an Error node. If skip fails or skips no characters, it fails without recovering.
func recoverAttr(m, skip core.Matcher, build bool) core.Matcher {
	return func(p *core.Parser) (*value.Node, bool) {
		m0 := p.Mark()
		v, ok, far, expected := core.Tracked(p, m)
		if ok {
			p.MergeExpected(far, expected)
			return v, true
		}
		expected = p.Keep(expected) // keep them while skip runs
		p.Reset(m0)
		if _, ok := skip(p); !ok || p.Pos == m0.Pos {
			p.Reset(m0)
			p.MergeExpected(far, expected)
			return nil, false
		}
		e := p.MakeError(far, expected)
		p.Recovered = append(p.Recovered, e)
		if !build {
			return nil, true
		}
		return p.NewNode(value.Data{Kind: value.ErrorKind, Start: int32(m0.Pos), End: int32(p.Pos), Text: p.Text(m0.Pos, p.Pos), Fields: value.Fields{{Name: "message", Value: p.RecoveryErrorText(e)}}, Fresh: true, Terminal: true}), true
	}
}

// singleMatcher returns a matcher of the value of e where it takes one element (an alternative or
// the operand of ?), from the building matcher m of e. Without a value the result is nil.
func singleMatcher(prog *compile.Program, e grammar.Expr, m core.Matcher) core.Matcher {
	mode, sh := compile.SingleModeOf(prog, e), compile.ShaperOf(prog, e)
	if mode == compile.SinglePass && !sh.Elems {
		return m
	}
	return func(p *core.Parser) (*value.Node, bool) {
		start := p.Pos
		v, ok := m(p)
		if !ok {
			return nil, false
		}
		v, _ = core.ShapeSingle(p, sh, mode, v, start)
		return v, true
	}
}

// captureMatcher is singleMatcher for a capture or a recovered expression, whose value is the
// unit node of the range matched when e has no value.
func captureMatcher(prog *compile.Program, e grammar.Expr, m core.Matcher) core.Matcher {
	mode, sh := compile.SingleModeOf(prog, e), compile.ShaperOf(prog, e)
	if mode == compile.SinglePass && !sh.Elems {
		return m
	}
	return func(p *core.Parser) (*value.Node, bool) {
		start := p.Pos
		v, ok := m(p)
		if !ok {
			return nil, false
		}
		if v, ok = core.ShapeSingle(p, sh, mode, v, start); !ok {
			v = p.NewNode(value.Data{Kind: value.SeqKind, Start: int32(start), End: int32(p.Pos), Fresh: true})
		}
		return v, true
	}
}

// scopeValue returns the value of a scope whose body e matched [start, p.pos) with the result v.
func scopeValueOf(prog *compile.Program, e grammar.Expr) func(p *core.Parser, v *value.Node, start int) *value.Node {
	sh := compile.ShaperOf(prog, e)
	if compile.ScopeTuple(prog, e) {
		return func(p *core.Parser, v *value.Node, start int) *value.Node { return core.ShapeTuple(p, sh, v, start) }
	}
	return func(_ *core.Parser, v *value.Node, _ int) *value.Node { return sh.One(v) }
}
