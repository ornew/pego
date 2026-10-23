package core

// Passes rewrite Core IR. Each states its precondition; each leaves Analyze to be run again, after
// which Verify holds if it held before.

// MinTerminalLeaves is the smallest number of literals and classes for which Terminals makes a
// terminal region of an expression with a choice. Smaller ones stay as they are, which costs less
// to run than an interpreter's calls.
const MinTerminalLeaves = 16

// TerminalLeaves returns the number of literals and classes of n, and whether n is a terminal
// form (literals, classes, sequences, choices, options, negative lookaheads, Text and Drop)
// containing a choice. It returns ok false for any other expression.
func TerminalLeaves(n Node) (leaves int, choice, ok bool) {
	switch n := n.(type) {
	case *Lit, *ClassNode:
		return 1, false, true
	case *Seq:
		for _, it := range n.Items {
			k, c, ok := TerminalLeaves(it)
			if !ok {
				return 0, false, false
			}
			leaves, choice = leaves+k, choice || c
		}
		return leaves, choice, true
	case *Choice:
		for _, a := range n.Alts {
			k, _, ok := TerminalLeaves(a.Body)
			if !ok {
				return 0, false, false
			}
			leaves += k
		}
		return leaves, true, true
	case *Opt:
		return TerminalLeaves(n.Body)
	case *Look:
		if n.Neg {
			return TerminalLeaves(n.Body)
		}
	case *Drop:
		return TerminalLeaves(n.Body)
	case *Text:
		return TerminalLeaves(n.Body)
	}
	return 0, false, false
}

// Terminals makes a Terminal of each outermost value-free expression of the given rules that is a
// terminal form with a choice and at least MinTerminalLeaves literals and classes, and that is in
// a value-free context: a lookahead or a discarded expression where the expression around it
// builds a value is not one, though its body may be. A target runs a Terminal as one program,
// which consumes the same input and records the same expectations as the expression, and charges
// the steps of the expression's entry only. Precondition: Want is set, and the guards of the
// rules' choices are not.
func Terminals(m *Module, rule func(*Rule) bool) {
	for _, r := range m.Rules {
		if r == nil || !rule(r) {
			continue
		}
		r.Body = terminals(r.Body, r.Want)
	}
}

// terminals makes the Terminals of n, whose context builds a value if build.
func terminals(n Node, build bool) Node {
	if n == nil {
		return nil
	}
	if !build && !n.Attrs().Want {
		if k, choice, ok := TerminalLeaves(n); ok && choice && k >= MinTerminalLeaves {
			t := &Terminal{Body: n}
			t.attrs.Steps = n.Attrs().Steps
			return t
		}
	}
	// The context of a child: the value of a lookahead's or a discarded expression's body is
	// never used, and a captured body always builds; otherwise a body builds where it is wanted.
	want := func(x Node) bool { return x.Attrs().Want }
	switch x := n.(type) {
	case *Seq:
		for i, it := range x.Items {
			x.Items[i] = terminals(it, x.attrs.Want)
		}
	case *Choice:
		for _, a := range x.Alts {
			a.Body = terminals(a.Body, x.attrs.Want)
		}
	case *Opt:
		x.Body = terminals(x.Body, x.attrs.Want)
	case *Repeat:
		x.Body = terminals(x.Body, want(x.Body))
	case *Stream:
		// The element of a #stream repetition is the repetition's body, as in any repetition.
		x.Repeat.Body = terminals(x.Repeat.Body, want(x.Repeat.Body))
	case *Look:
		x.Body = terminals(x.Body, want(x.Body))
	case *Nest:
		x.Body = terminals(x.Body, want(x.Body))
	case *Text:
		x.Body = terminals(x.Body, false)
	case *Drop:
		x.Body = terminals(x.Body, false)
	case *Capture:
		x.Body = terminals(x.Body, true)
	case *Label:
		x.Body = terminals(x.Body, x.attrs.Want)
	case *Recover:
		x.Body = terminals(x.Body, want(x.Body))
		x.Skip = terminals(x.Skip, false)
	case *OperatorsRule:
		for _, l := range x.Lines() {
			l.Body = terminals(l.Body, want(l.Body))
		}
	}
	return n
}

// Guards sets the guard of every alternative of a choice outside Terminals that has a first
// terminal (Attrs.First) to it: the alternative is skipped when the next character cannot start
// it. With classes, an alternative that is a class, or a sequence whose first item is one, is
// marked GuardedClass: the guard's test of the class is the class's own, so the class only
// consumes the character the guard read. A Terminal's program does its own dispatch. Precondition:
// Analyze has run.
func Guards(m *Module, classes bool) {
	for _, r := range m.Rules {
		if r != nil {
			guards(r.Body, classes)
		}
	}
}

func guards(n Node, classes bool) {
	switch x := n.(type) {
	case *Terminal:
		return
	case *OperatorsRule:
		for _, l := range x.Lines() {
			guards(l.Body, classes)
		}
		return
	case *Choice:
		for _, a := range x.Alts {
			a.Guard, a.GuardedClass = a.Body.Attrs().First, false
			if a.Guard != nil && classes {
				a.GuardedClass = leadingClassNode(a.Body) != nil
			}
		}
	}
	for _, c := range Children(n) {
		guards(c, classes)
	}
}

// leadingClassNode returns the class that n is, or that begins the sequence n.
func leadingClassNode(n Node) *ClassNode {
	if s, ok := n.(*Seq); ok && len(s.Items) > 0 {
		n = s.Items[0]
	}
	c, _ := n.(*ClassNode)
	return c
}

// Scans replaces every value-free repetition of a class or of any character by a Scan.
func Scans(m *Module) {
	for _, r := range m.Rules {
		if r == nil {
			continue
		}
		r.Body = Rewrite(r.Body, func(n Node) Node {
			rep, ok := n.(*Repeat)
			if !ok || rep.Attrs().Want || rep.Project != "" || rep.Scope != nil {
				return n
			}
			switch body := rep.Body.(type) {
			case *ClassNode:
				return &Scan{base: rep.base, Class: body.Class, Min: rep.Min, Max: rep.Max}
			case *Any:
				return &Scan{base: rep.base, Min: rep.Min, Max: rep.Max}
			}
			return n
		})
	}
}

// ClassRuns replaces every run of two or more consecutive classes in a value-free sequence by a
// ClassRun, except inside Terminals and except the first class of an alternative marked
// GuardedClass, which only consumes the character its guard read. A ClassRun charges no steps on
// entry: the classes charge nothing for their own entries.
func ClassRuns(m *Module) {
	for _, r := range m.Rules {
		if r == nil {
			continue
		}
		classRuns(r.Body, false)
	}
}

func classRuns(n Node, guarded bool) {
	switch x := n.(type) {
	case *Terminal:
		return
	case *Choice:
		for _, a := range x.Alts {
			classRuns(a.Body, a.GuardedClass)
		}
		return
	case *OperatorsRule:
		for _, l := range x.Lines() {
			classRuns(l.Body, false)
		}
		return
	case *Seq:
		for _, it := range x.Items {
			classRuns(it, false)
		}
		if x.Attrs().Want {
			return
		}
		var items []Node
		for i := 0; i < len(x.Items); i++ {
			run := 0
			for i+run < len(x.Items) {
				if _, ok := x.Items[i+run].(*ClassNode); !ok {
					break
				}
				run++
			}
			if run < 2 || i == 0 && guarded {
				items = append(items, x.Items[i])
				continue
			}
			cr := &ClassRun{}
			for _, it := range x.Items[i : i+run] {
				cr.Classes = append(cr.Classes, it.(*ClassNode).Class)
			}
			items = append(items, cr)
			i += run - 1
		}
		x.Items = items
		return
	}
	for _, c := range Children(n) {
		classRuns(c, false)
	}
}
