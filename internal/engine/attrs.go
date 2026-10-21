package engine

import (
	"github.com/ornew/pego/grammar"
)

// attributed compiles an attributed expression. Attributes are applied from the inside out, in
// the order they are written.
func (c *compiler) attributed(e *grammar.Attributed, s *scope, build bool) matcher {
	var m matcher
	if rep, ok := e.Expr.(*grammar.Repeat); ok && isStream(e) {
		m = c.repeat(rep, s, build, true)
	} else {
		m = c.expr(e.Expr, s, build)
	}
	for _, a := range e.Attrs {
		switch a.Name {
		case "stream":
			if _, ok := e.Expr.(*grammar.Repeat); !ok {
				c.errorf(a.Diagnostic(), "#stream must be attached to a repetition (*, +, or #repeat(...))")
			}
			if len(a.Args)+len(a.KwArgs) > 0 {
				c.errorf(a.Diagnostic(), "#stream takes no arguments")
			}
		case "missing":
			lit, ok := e.Expr.(*grammar.Literal)
			if !ok || lit.Value == "" {
				c.errorf(a.Diagnostic(), "#missing requires a nonempty literal")
				continue
			}
			if len(a.Args)+len(a.KwArgs) != 0 {
				c.errorf(a.Diagnostic(), "#missing takes no arguments")
			}
			if c.missingSiteIDs == nil {
				c.missingSiteIDs = make(map[*grammar.Attribute]int)
			}
			site := c.missingSiteIDs[a]
			if site == 0 {
				c.missingSites++
				site = c.missingSites
				c.missingSiteIDs[a] = site
			}
			m = missingLiteral(m, site, lit.Value, build)
		case "error":
			msg, ok := c.stringArg(a, "message")
			if ok {
				id := msgBit | c.desc(msg)
				m = errorAttr(m, id)
				if c.prog.hasMissing {
					m = missingErrorScope(m, id)
				}
			}
		case "diagnostic":
			name, ok := c.stringArg(a, "name")
			if ok {
				m = diagnosticAttr(m, c.desc(name))
			}
		case "recover":
			skipExpr, ok := a.Arg("skip")
			if !ok {
				c.errorf(a.Diagnostic(), "#recover requires a skip argument")
				continue
			}
			c.noCaptures(skipExpr)
			// What recovers is one element: the value of the expression, or the error.
			if build {
				m = captureMatcher(c.prog, e.Expr, m)
			}
			m = recoverAttr(m, c.expr(skipExpr, s, false), build)
		default:
			c.errorf(a.Diagnostic(), "unknown attribute @%s", a.Name)
		}
		for name := range a.KwArgs {
			if !validArg(a.Name, name) {
				c.errorf(a.Diagnostic(), "%s%s has no argument %s", annotationMarker(a), a.Name, name)
			}
		}
	}
	return m
}

func validArg(attr, arg string) bool {
	switch attr {
	case "error":
		return arg == "message"
	case "diagnostic":
		return arg == "name"
	case "recover":
		return arg == "skip"
	}
	return true
}

func (c *compiler) stringArg(a *grammar.Attribute, name string) (string, bool) {
	v, ok := a.Arg(name)
	if !ok {
		c.errorf(a.Diagnostic(), "@%s requires a %s argument", a.Name, name)
		return "", false
	}
	lit, ok := v.(*grammar.Literal)
	if !ok {
		c.errorf(a.Diagnostic(), "@%s: %s must be a string", a.Name, name)
		return "", false
	}
	return lit.Value, true
}

// tracked runs m with a separate record of expectations. It returns the farthest position and
// the expectations of that separate record (the expectations are valid only until the next
// recording; see unisolate).
func tracked(p *parser, m matcher) (v *Node, ok bool, far int, expected []expID) {
	mark := p.isolate(p.pos)
	v, ok = m(p)
	far, expected = p.unisolate(mark)
	return v, ok, far, expected
}

// errorAttr replaces the expectations of a failed expression with the message msg (an ID with
// msgBit set). The message is reported at the farthest position reached within the expression.
func errorAttr(m matcher, msg expID) matcher {
	return func(p *parser) (*Node, bool) {
		v, ok, far, expected := tracked(p, m)
		if ok {
			p.mergeExpected(far, expected)
			return v, true
		}
		p.expect(far, msg)
		return nil, false
	}
}

// diagnosticAttr names ordinary expectations of a failure at the expression boundary.
// Explicit messages and the tracked failure position remain unchanged.
func diagnosticAttr(m matcher, name expID) matcher {
	return func(p *parser) (*Node, bool) {
		v, ok, far, expected := tracked(p, m)
		if ok {
			p.mergeExpected(far, expected)
			return v, true
		}
		p.mergeExpected(far, renamedExpected(p, expected, name))
		return nil, false
	}
}

func renamedExpected(p *parser, expected []expID, name expID) []expID {
	// unisolate leaves this record beyond the active expectation stack. Rename it
	// before mergeExpected reuses the stack; this avoids an unbounded copy.
	for i, id := range expected {
		p.stepWork()
		if id&msgBit == 0 {
			expected[i] = name
		}
	}
	return expected
}

// recoverAttr records an error when the expression fails, skips the input matched by skip, and
// returns an Error node. If skip fails or skips no characters, it fails without recovering.
func recoverAttr(m, skip matcher, build bool) matcher {
	return func(p *parser) (*Node, bool) {
		m0 := p.mark()
		v, ok, far, expected := tracked(p, m)
		if ok {
			p.mergeExpected(far, expected)
			return v, true
		}
		expected = p.keep(expected) // keep them while skip runs
		p.reset(m0)
		if _, ok := skip(p); !ok || p.pos == m0.pos {
			p.reset(m0)
			p.mergeExpected(far, expected)
			return nil, false
		}
		e := p.makeError(far, expected)
		p.recovered = append(p.recovered, e)
		if !build {
			return nil, true
		}
		return p.newNode(Node{kind: kindError, start: int32(m0.pos), end: int32(p.pos), text: p.text(m0.pos, p.pos), fields: Fields{{"message", p.recoveryErrorText(e)}}, fresh: true, terminal: true}), true
	}
}
