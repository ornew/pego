package sample

import (
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
)

// Kinds of generated values (see val).
const (
	vNone    = iota // no value: lookahead, discard, anchors, predicates
	vNil            // an optional expression or a capture that did not match
	vText           // a terminal
	vList           // a repetition
	vNode           // another node (Seq, ...)
	vUnknown        // the result of an action, or of a Pratt expression
)

// val describes the value that the parser will build for a generated expression, as far as the
// generator can tell. Predicates read it through captures.
type val struct {
	kind       int
	start, end int // the range of the input it covers
	n          int // the number of elements of a list
}

// capture is a capture made in the current scope (a persistent list).
type capture struct {
	name string
	v    val
	next *capture
}

// variable is a variable definition (a persistent list; the nearest definition comes first).
type variable struct {
	name string
	v    pval
	next *variable
}

// Kinds of predicate values.
const (
	pUnknown = iota
	pNil
	pInt
	pString
	pBool
	pNode
)

// pval is the value of a term in a predicate. pUnknown means that the generator cannot compute it.
type pval struct {
	kind int
	i    int
	s    string
	b    bool
	node val
}

var unknownVal = pval{kind: pUnknown}

// predicate evaluates the predicate e. It returns false only when the parser's predicate would
// certainly fail; when the generator cannot tell, it returns true and leaves the decision to the
// parser. A variable definition is returned as def.
func (g *gen) predicate(e *grammar.Predicate) (ok bool, def *variable) {
	if a, isAssign := e.Term.(*grammar.Assign); isAssign {
		v, err := g.eval(a.Value)
		if err {
			return false, nil
		}
		return true, &variable{name: a.Name, v: v}
	}
	v, err := g.eval(e.Term)
	if err {
		return false, nil
	}
	return !(v.kind == pBool && !v.b), nil
}

// eval evaluates the term t. err is true if the evaluation certainly fails.
func (g *gen) eval(t grammar.Term) (v pval, err bool) {
	switch t := t.(type) {
	case *grammar.IntLit:
		return pval{kind: pInt, i: t.Value}, false
	case *grammar.StringLit:
		return pval{kind: pString, s: t.Value}, false
	case *grammar.BoolLit:
		return pval{kind: pBool, b: t.Value}, false
	case *grammar.NilLit:
		return pval{kind: pNil}, false
	case *grammar.CaptureRef:
		for c := g.caps; c != nil; c = c.next {
			if c.name == t.Name {
				if c.v.kind == vNil || c.v.kind == vNone {
					return pval{kind: pNil}, false
				}
				return pval{kind: pNode, node: c.v}, false
			}
		}
		// A capture that did not match is nil.
		return pval{kind: pNil}, false
	case *grammar.VarRef:
		for v := g.vars; v != nil; v = v.next {
			if v.name == t.Name {
				return v.v, false
			}
		}
		// Reading an undefined variable is an error.
		return unknownVal, true
	case *grammar.Call:
		return g.call(t)
	case *grammar.Unary:
		x, err := g.eval(t.X)
		if err || x.kind == pUnknown {
			return unknownVal, err
		}
		switch {
		case t.Op == "!" && x.kind == pBool:
			return pval{kind: pBool, b: !x.b}, false
		case t.Op == "-" && x.kind == pInt:
			return pval{kind: pInt, i: -x.i}, false
		}
		return unknownVal, false
	case *grammar.Binary:
		return g.binary(t)
	}
	return unknownVal, false
}

func (g *gen) call(t *grammar.Call) (pval, bool) {
	if len(t.Args) != 1 || (t.Func != "len" && t.Func != "text") {
		return unknownVal, false
	}
	x, err := g.eval(t.Args[0])
	if err || x.kind == pUnknown {
		return unknownVal, err
	}
	switch t.Func {
	case "len":
		switch {
		case x.kind == pString:
			return pval{kind: pInt, i: utf8.RuneCountInString(x.s)}, false
		case x.kind == pNode && x.node.kind == vText:
			return pval{kind: pInt, i: utf8.RuneCountInString(g.slice(x.node.start, x.node.end))}, false
		case x.kind == pNode && x.node.kind == vList:
			return pval{kind: pInt, i: x.node.n}, false
		}
	case "text":
		if x.kind == pNode && x.node.kind != vUnknown {
			return pval{kind: pString, s: g.slice(x.node.start, x.node.end)}, false
		}
	}
	return unknownVal, false
}

func (g *gen) binary(t *grammar.Binary) (pval, bool) {
	l, err := g.eval(t.L)
	if err {
		return unknownVal, true
	}
	// Short-circuit operators with three-valued logic.
	if t.Op == "&&" || t.Op == "||" {
		if l.kind == pBool && l.b == (t.Op == "||") {
			return l, false
		}
		r, err := g.eval(t.R)
		if err {
			return unknownVal, true
		}
		switch {
		case r.kind == pBool && r.b == (t.Op == "||"):
			return r, false
		case l.kind == pBool && r.kind == pBool:
			return pval{kind: pBool, b: t.Op == "&&"}, false
		}
		return unknownVal, false
	}
	r, err := g.eval(t.R)
	if err {
		return unknownVal, true
	}
	if l.kind == pUnknown || r.kind == pUnknown {
		return unknownVal, false
	}
	switch t.Op {
	case "==", "!=":
		eq, known := equal(l, r)
		if !known {
			return unknownVal, false
		}
		return pval{kind: pBool, b: eq == (t.Op == "==")}, false
	case "<", ">", "<=", ">=":
		var c int
		switch {
		case l.kind == pInt && r.kind == pInt:
			c = cmpInt(l.i, r.i)
		case l.kind == pString && r.kind == pString:
			c = cmpString(l.s, r.s)
		default:
			return unknownVal, false
		}
		var b bool
		switch t.Op {
		case "<":
			b = c < 0
		case ">":
			b = c > 0
		case "<=":
			b = c <= 0
		default:
			b = c >= 0
		}
		return pval{kind: pBool, b: b}, false
	case "+":
		switch {
		case l.kind == pInt && r.kind == pInt:
			return pval{kind: pInt, i: l.i + r.i}, false
		case l.kind == pString && r.kind == pString:
			return pval{kind: pString, s: l.s + r.s}, false
		}
	case "-", "*", "/", "%":
		if l.kind != pInt || r.kind != pInt {
			return unknownVal, false
		}
		switch t.Op {
		case "-":
			return pval{kind: pInt, i: l.i - r.i}, false
		case "*":
			return pval{kind: pInt, i: l.i * r.i}, false
		}
		if r.i == 0 {
			return unknownVal, true
		}
		if t.Op == "/" {
			return pval{kind: pInt, i: l.i / r.i}, false
		}
		return pval{kind: pInt, i: l.i % r.i}, false
	}
	return unknownVal, false
}

// equal compares two known values.
func equal(l, r pval) (eq, known bool) {
	if l.kind == pNil || r.kind == pNil {
		other := l
		if l.kind == pNil {
			other = r
		}
		switch {
		case other.kind == pNil:
			return true, true
		case other.kind == pNode && other.node.kind != vUnknown:
			return false, true
		}
		return false, false
	}
	if l.kind != r.kind {
		return false, false
	}
	switch l.kind {
	case pInt:
		return l.i == r.i, true
	case pString:
		return l.s == r.s, true
	case pBool:
		return l.b == r.b, true
	}
	return false, false
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func cmpString(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
