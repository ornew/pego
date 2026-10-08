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
				c.errorf(a.Pos, "#stream must be attached to a repetition (*, +, or {n,m})")
			}
			if len(a.Args) > 0 {
				c.errorf(a.Pos, "#stream takes no arguments")
			}
		case "error":
			msg, ok := c.stringArg(a, "message")
			if ok {
				m = errorAttr(m, msgBit|c.desc(msg))
			}
		case "recover":
			skipExpr, ok := a.Arg("skip")
			if !ok {
				c.errorf(a.Pos, "#recover requires a skip argument")
				continue
			}
			c.noCaptures(skipExpr)
			// An expression without a value (a lookahead, a predicate, ...) has none after a
			// recovery either, so that the shape of the enclosing value does not depend on it.
			m = recoverAttr(m, c.expr(skipExpr, s, false), build && visible(e.Expr))
		default:
			c.errorf(a.Pos, "unknown attribute #%s", a.Name)
		}
		for _, arg := range a.Args {
			if !validArg(a.Name, arg.Name) {
				c.errorf(a.Pos, "#%s has no argument %s", a.Name, arg.Name)
			}
		}
	}
	return m
}

func validArg(attr, arg string) bool {
	switch attr {
	case "error":
		return arg == "message"
	case "recover":
		return arg == "skip"
	}
	return true
}

func (c *compiler) stringArg(a *grammar.Attribute, name string) (string, bool) {
	v, ok := a.Arg(name)
	if !ok {
		c.errorf(a.Pos, "#%s requires a %s argument", a.Name, name)
		return "", false
	}
	lit, ok := v.(*grammar.Literal)
	if !ok {
		c.errorf(a.Pos, "#%s: %s must be a string", a.Name, name)
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
		return p.newNode(Node{kind: kindError, Start: int32(m0.pos), End: int32(p.pos), Text: p.text(m0.pos, p.pos), Fields: Fields{{"message", e.Error()}}, fresh: true, terminal: true}), true
	}
}
