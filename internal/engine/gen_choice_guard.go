package engine

import (
	"fmt"

	"github.com/ornew/pego/grammar"
)

// Guards of choices that already match the first class of an alternative.
//
// The guard of an alternative (dgen.choice, generator.first) reads the next character and skips
// the alternative when the character cannot start it. When the alternative begins with a
// character class, the guard has tested that class: the alternative runs only if the character
// is a member, because a guard of depth 0 also requires p.Depth <= p.MaxDepth, which holds
// wherever generated code runs (every increment of p.Depth is checked). The first class of the
// alternative then reads the same character again and tests it again. In a parser without work
// controls and Documents, where reading and testing charge nothing, the guard keeps the size of
// the character it read and the class only consumes it: the class cannot fail there, so the
// expectation it would record on failure is never recorded in either form. Only the first
// expression written after the alternative itself may be that class: dgen.expr forgets the
// guarded class at any other expression, so a class elsewhere in the alternative, or anywhere
// after it, is matched as written.

// guardedFirst returns the character class that alt begins with, if it begins with one that
// generator.first takes as its guard.
func guardedFirst(alt grammar.Expr) *grammar.CharClass {
	if seq, ok := alt.(*grammar.Seq); ok && len(seq.Items) > 0 {
		alt = seq.Items[0]
	}
	c, _ := alt.(*grammar.CharClass)
	return c
}

// guardSize returns the variable in which the guards of the choice keep the size of the
// character they read, or "_" when no alternative begins with a class or the parser charges
// work for reading.
func (d *dgen) guardSize(e *grammar.Choice) string {
	if !d.g.lean || d.g.disableGuardedClasses {
		return "_"
	}
	for _, alt := range e.Alts {
		if guardedFirst(alt) != nil {
			return d.decl("x", "int")
		}
	}
	return "_"
}

// guardedClass writes the class d.guarded, which the guard has matched: it consumes the
// character the guard read.
func (d *dgen) guardedClass(build bool) string {
	if !build {
		d.line("p.Pos += %s", d.rd(d.guardedSize))
		return "nil"
	}
	start, v := d.decl("x", "int"), d.decl("v", d.g.valType())
	d.line("%s = p.Pos", start)
	d.line("p.Pos += %s", d.rd(d.guardedSize))
	d.line("%s = %s", v, d.match(d.rd(start), fmt.Sprintf("p.Text(%s, p.Pos)", start)))
	return v
}
