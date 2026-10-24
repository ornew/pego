package core

// Node is an expression of Core IR.
type Node interface {
	// Attrs returns the node's attributes.
	Attrs() *Attrs
}

// Attrs are the attributes of a node. Want, Shape and Steps are set when the node is built;
// Analyze computes the others.
type Attrs struct {
	// Want reports that the node's value is used. A node with want false builds no value.
	Want bool
	// Shape is the static shape of the node's value, which the enclosing node converts where the
	// node takes one element. A node that builds no value has the shape of its expression too: its
	// value is nil, converted like any other.
	Shape Shape
	// Nullable reports that the node can succeed without consuming input.
	Nullable bool
	// First is the terminal the node must begin with, if it has one (see First).
	First *First
	// Eff summarizes the node's effects.
	Eff Effects
	// Steps is the number of work steps a work-controlled parse charges on entering the node: one
	// for each grammar expression the node is built from.
	Steps int
}

// base holds the attributes of a node.
type base struct{ attrs Attrs }

// Attrs returns the attributes of the node.
func (b *base) Attrs() *Attrs { return &b.attrs }

// First is the terminal a node must match first: if the next character is not the first code
// point of a literal (Lit) or a member of Class, the node fails at once, recording only Exp at
// the current position, after entering Depth rule calls (Call and Nest) on the way.
type First struct {
	// Lit reports a literal, whose first code point is Rune; otherwise Class is the class.
	Lit   bool
	Rune  rune
	Class *Class
	// Exp is the expectation the node records when the next character cannot begin it: that of
	// the literal or the class.
	Exp   ExpID
	Depth int
}

// Effect is one kind of effect a node may have.
type Effect uint16

const (
	// EffConsume: may advance the position.
	EffConsume Effect = 1 << iota
	// EffFail: may fail.
	EffFail
	// EffExpect: may record expectations.
	EffExpect
	// EffCapture: may write capture slots of the enclosing scope (Effects.Slots).
	EffCapture
	// EffBind: may bind a variable.
	EffBind
	// EffRecover: may append recovered errors.
	EffRecover
	// EffCut: may set the cut flag of the enclosing choice, option or repetition.
	EffCut
	// EffRaise: may end the parse with an error.
	EffRaise
	// EffCall: calls rules or enters a Nest.
	EffCall
	// EffCommit: may deliver stream elements.
	EffCommit
)

// Effects is an effect summary.
type Effects struct {
	Set Effect
	// Slots holds the capture slots of the enclosing scope the node may write, in the order they
	// are first written.
	Slots []int
}

// Lit matches a literal.
type Lit struct {
	base
	Text string
	// Fold makes the literal case-insensitive: each ASCII letter of the input matches either case
	// of the same letter in Text, which is written as the grammar wrote it.
	Fold bool
	// Exp is the expectation the literal records on failure.
	Exp ExpID
}

// ClassNode matches one character of a class.
type ClassNode struct {
	base
	Class *Class
}

// Any matches any one character, recording the expectation of any character on failure.
type Any struct{ base }

// AnchorKind is the kind of an anchor.
type AnchorKind uint8

const (
	BeginInput AnchorKind = iota
	EndInput
	BeginLine
	EndLine
)

// Anchor matches a position: the beginning or end of the input or of a line, recording the
// anchor's expectation on failure.
type Anchor struct {
	base
	Kind AnchorKind
}

// Seq matches its items in turn.
type Seq struct {
	base
	Items []Node
}

// Choice matches the first of its alternatives that matches.
type Choice struct {
	base
	Alts []*Alt
}

// Alt is an alternative of a choice.
type Alt struct {
	Body Node
	// Guard, if not nil, is the first terminal of Body: the alternative is skipped, recording
	// Guard.Exp, when the next character is not in Guard.Class and the nesting limit allows
	// Guard.Depth more levels.
	Guard *First
	// GuardedClass reports that Body begins with the guard's class (Body is the class, or a
	// sequence whose first item is), so that a guard that read the character can consume it.
	GuardedClass bool
}

// Opt matches its body or nothing.
type Opt struct {
	base
	Body Node
}

// Repeat matches its body between Min and Max times (Max < 0 is unbounded).
type Repeat struct {
	base
	Min, Max int
	Body     Node
	// Scope, if not nil, holds the capture slots of the element's own scope: each iteration has
	// fresh captures, which the element's value carries.
	Scope []Slot
	// Project, if not "", is the capture of the element whose values the repetition gathers
	// (a projected repetition); the element builds no value.
	Project string
}

// Look is a lookahead: a silent scope that restores the position.
type Look struct {
	base
	Body Node
	Neg  bool
}

// Cut commits the enclosing choice, option or repetition.
type Cut struct{ base }

// Succeed matches the empty input.
type Succeed struct{ base }

// FailNode never matches.
type FailNode struct{ base }

// Call calls a rule through the runtime.
type Call struct {
	base
	Rule int
	// Level is the minimum binding level of a call of an Operators Rule: the call applies only
	// the operators whose Operator.Level is greater, those of the level the reference names and
	// of the tighter levels after it. It is 0, every operator, for a call that names no level.
	Level int
}

// Nest is an inlined call of Rule with the scope of a rule call, without memoization: a cut
// barrier and a variable scope with the captures of Rule's scope, which on failure removes the
// errors recovered inside it and counts one nesting level.
type Nest struct {
	base
	Rule int
	Body Node
}

// OperatorsRule is the body of an Operators Rule: the runtime's loop over its lines.
type OperatorsRule struct {
	base
	// Skip is the skip line, if any.
	Skip     *OpLine
	Operands []*OpLine
	Ops      []*Operator
}

// OpLine is a line of an Operators Rule: a body with a scope of its own and an optional action.
type OpLine struct {
	Body   Node
	Scope  []Slot
	Action Term
	// Shape is the shape of the line body's value.
	Shape Shape
}

// Operator is an operator line.
type Operator struct {
	ID          int
	Kind, Assoc string
	// Level counts from 1, the loosest.
	Level int
	Line  *OpLine
}

// Text matches its body and has the matched text as its value.
type Text struct {
	base
	Body Node
}

// Drop matches its body and has no value.
type Drop struct {
	base
	Body Node
}

// Capture matches its body and stores its value in a capture slot.
type Capture struct {
	base
	Slot int
	Name string
	Body Node
	// Project, if not "", is the field of the elements of Body, a repetition, gathered instead of
	// its values.
	Project string
}

// Pred is a Predicate Action: it fails unless the term is true.
type Pred struct {
	base
	Term Term
}

// BindNode binds a variable to the value of a term.
type BindNode struct {
	base
	Var  string
	Term Term
}

// Label is @error or @diagnostic: on failure, the expectations recorded inside are replaced by
// the message Exp (@error) or renamed to Exp (@diagnostic).
type Label struct {
	base
	Body       Node
	Exp        ExpID
	Diagnostic bool
}

// Recover is #recover: on failure it records the error, skips with Skip and succeeds.
type Recover struct {
	base
	Body, Skip Node
}

// Missing is #missing: matches the literal or, while missing-token insertion is active, inserts
// it.
type Missing struct {
	base
	Lit *Lit
}

// Stream is the #stream repetition of a start rule: in a stream parse, the invocation of the
// rule that starts the parse delivers each element as it completes (EffCommit). Elsewhere it is
// its repetition.
type Stream struct {
	base
	Repeat *Repeat
}

// Scan is a value-free repetition of one character of a class (any character if Class is nil).
type Scan struct {
	base
	Class    *Class
	Min, Max int
}

// ClassRun is a value-free sequence of single characters of classes.
type ClassRun struct {
	base
	Classes []*Class
}

// Terminal is a value-free terminal region (literals, classes, sequences, choices, options and
// negative lookaheads) that a target may run as a table.
type Terminal struct {
	base
	Body Node
}

// Children returns the direct subexpressions of n, in order. The skip expression of a Recover
// follows its body; the lines of an Operators Rule are not children.
func Children(n Node) []Node {
	switch n := n.(type) {
	case *Seq:
		return n.Items
	case *Choice:
		xs := make([]Node, len(n.Alts))
		for i, a := range n.Alts {
			xs[i] = a.Body
		}
		return xs
	case *Opt:
		return []Node{n.Body}
	case *Repeat:
		return []Node{n.Body}
	case *Look:
		return []Node{n.Body}
	case *Nest:
		return []Node{n.Body}
	case *Text:
		return []Node{n.Body}
	case *Drop:
		return []Node{n.Body}
	case *Capture:
		return []Node{n.Body}
	case *Label:
		return []Node{n.Body}
	case *Recover:
		return []Node{n.Body, n.Skip}
	case *Missing:
		return []Node{n.Lit}
	case *Stream:
		return []Node{n.Repeat}
	case *Terminal:
		return []Node{n.Body}
	}
	return nil
}

// Walk calls f for n and its subexpressions in preorder, including the lines of Operators Rules.
func Walk(n Node, f func(Node)) {
	if n == nil {
		return
	}
	f(n)
	if o, ok := n.(*OperatorsRule); ok {
		for _, l := range o.Lines() {
			Walk(l.Body, f)
		}
		return
	}
	for _, c := range Children(n) {
		Walk(c, f)
	}
}

// Lines returns the lines of the Operators Rule: the skip line, the operands and the operators.
func (o *OperatorsRule) Lines() []*OpLine {
	var ls []*OpLine
	if o.Skip != nil {
		ls = append(ls, o.Skip)
	}
	ls = append(ls, o.Operands...)
	for _, op := range o.Ops {
		ls = append(ls, op.Line)
	}
	return ls
}

// Rewrite replaces each subexpression of n, bottom up, by what f returns for it, and returns
// the result for n. f sees nodes whose children are already rewritten. A Stream keeps its
// repetition where f replaces it by a node that is not a Repeat.
func Rewrite(n Node, f func(Node) Node) Node {
	if n == nil {
		return nil
	}
	switch x := n.(type) {
	case *Seq:
		for i, it := range x.Items {
			x.Items[i] = Rewrite(it, f)
		}
	case *Choice:
		for _, a := range x.Alts {
			a.Body = Rewrite(a.Body, f)
		}
	case *Opt:
		x.Body = Rewrite(x.Body, f)
	case *Repeat:
		x.Body = Rewrite(x.Body, f)
	case *Look:
		x.Body = Rewrite(x.Body, f)
	case *Nest:
		x.Body = Rewrite(x.Body, f)
	case *Text:
		x.Body = Rewrite(x.Body, f)
	case *Drop:
		x.Body = Rewrite(x.Body, f)
	case *Capture:
		x.Body = Rewrite(x.Body, f)
	case *Label:
		x.Body = Rewrite(x.Body, f)
	case *Recover:
		x.Body = Rewrite(x.Body, f)
		x.Skip = Rewrite(x.Skip, f)
	case *Stream:
		if r, ok := Rewrite(x.Repeat, f).(*Repeat); ok {
			x.Repeat = r
		}
	case *Terminal:
		x.Body = Rewrite(x.Body, f)
	case *OperatorsRule:
		for _, l := range x.Lines() {
			l.Body = Rewrite(l.Body, f)
		}
	}
	return f(n)
}
