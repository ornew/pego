package engine

import (
	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/value"
)

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
func tracked(p *parser, m matcher) (v *value.Node, ok bool, far int, expected []expID) {
	mark := p.isolate(p.pos)
	v, ok = m(p)
	far, expected = p.unisolate(mark)
	return v, ok, far, expected
}

// errorAttr replaces the expectations of a failed expression with the message msg (an ID with
// msgBit set). The message is reported at the farthest position reached within the expression.
func errorAttr(m matcher, msg expID) matcher {
	return func(p *parser) (*value.Node, bool) {
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
	return func(p *parser) (*value.Node, bool) {
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
	return func(p *parser) (*value.Node, bool) {
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
		return p.newNode(value.Data{Kind: value.ErrorKind, Start: int32(m0.pos), End: int32(p.pos), Text: p.text(m0.pos, p.pos), Fields: value.Fields{{Name: "message", Value: p.recoveryErrorText(e)}}, Fresh: true, Terminal: true}), true
	}
}
