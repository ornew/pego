package core

// Term is a term of the action language: an action, a predicate or the value of a binding, with
// its names resolved and its checked type (TypeOf).
type Term interface{ typ() *Type }

// termBase holds the checked type of a term.
type termBase struct{ t Type }

func (b *termBase) typ() *Type { return &b.t }

// TypeOf returns the checked type of the term t: the type of its value, nil for a lambda, which
// only built-ins call.
func TypeOf(t Term) Type { return *t.typ() }

// SetType sets the checked type of the term t.
func SetType(t Term, typ Type) { *t.typ() = typ }

// Const is a constant: an int, a string, a bool or nil.
type Const struct {
	termBase
	Value any
}

// SlotRef reads a capture slot of the scope that encloses the term.
type SlotRef struct {
	termBase
	Slot int
	Name string
}

// LocalKind is the kind of a local name.
type LocalKind uint8

const (
	// LocalParam is a lambda parameter.
	LocalParam LocalKind = iota
	// LocalLhs, LocalRhs and LocalOp are $lhs, $rhs and $op of an Operators Rule operator line.
	LocalLhs
	LocalRhs
	LocalOp
	// LocalValue is $value, the folded value, in the whole-rule action of an Operators Rule.
	LocalValue
)

// Local reads a lambda parameter or an Operators Rule local.
type Local struct {
	termBase
	Kind LocalKind
	Name string
	// Lambda is the lambda that binds a parameter.
	Lambda *Lambda
}

// VarRef reads a variable.
type VarRef struct {
	termBase
	Name string
}

// Member reads a member of a value: a field or a node member ($o, .Start, ...).
type Member struct {
	termBase
	X    Term
	Name string
}

// New makes a struct.
type New struct {
	termBase
	// Type is the struct type.
	Type string
	// Fields are the fields given, in the order written.
	Fields []FieldInit
	// Declared holds the struct's field names in declaration order.
	Declared []string
	// Final reports that the struct is the rule's or line's result: its range is the rule's.
	Final bool
	// InOrder reports that the fields given are written in declaration order (RewriteTerms).
	InOrder bool
}

// FieldInit is a field given to New.
type FieldInit struct {
	Name  string
	Value Term
}

// Convert converts a value to a named node type.
type Convert struct {
	termBase
	NodeType string
	X        Term
	// Proven reports that the checker proved that every value of X converts.
	Proven bool
}

// Tuple makes a tuple.
type Tuple struct {
	termBase
	Elems []Term
}

// Builtin calls a built-in function: len, text, foldl, foldr, map, filter, list or concat.
type Builtin struct {
	termBase
	Func string
	Args []Term
	// Projected reports a map call over a capture of a projected repetition whose function takes
	// out the projected field: the capture already holds the mapped values.
	Projected bool
}

// Lambda is a function literal, an argument of a built-in.
type Lambda struct {
	termBase
	Params []string
	Body   Term
}

// Binary is a binary operation.
type Binary struct {
	termBase
	Op   string
	L, R Term
}

// Unary is a unary operation.
type Unary struct {
	termBase
	Op string
	X  Term
}

// Cast converts a value to a checked type. Check and Key are the backend's handles to the
// checked type and to the cast.
type Cast struct {
	termBase
	X         Term
	Check     any
	Key       any
	Line, Col int
}

// TextEq compares the texts of two values, as text(L) == text(R) (or != with Neg), without
// boxing them. It is what RewriteTerms makes of such a comparison.
type TextEq struct {
	termBase
	L, R Term
	Neg  bool
}

// FieldCompare compares two operands, at least one a field read, without boxing string
// fields.
type FieldCompare struct {
	termBase
	Op   string
	L, R Operand
}

// Operand is an operand of FieldCompare: a field Name of X if Field, else the value of X.
type Operand struct {
	X     Term
	Field bool
	Name  string
	// Type is the checked type of the operand's value: the field's if Field.
	Type Type
}

// TextIn reports whether the text of a capture is one of Texts: a disjunction of text equality
// comparisons with literals (Orig), evaluated once.
type TextIn struct {
	termBase
	X     Term
	Texts []string
	Orig  Term
}

// ConcatFused is concat whose arguments push their elements directly.
type ConcatFused struct {
	termBase
	Parts []ConcatPart
}

// ConcatPartKind is the kind of a part of ConcatFused.
type ConcatPartKind uint8

const (
	// PartList pushes the values of Args (a list call).
	PartList ConcatPartKind = iota
	// PartMap pushes the results of Fn over the items of X.
	PartMap
	// PartItems pushes the items of X.
	PartItems
)

// ConcatPart is a part of ConcatFused.
type ConcatPart struct {
	Kind ConcatPartKind
	Args []Term
	X    Term
	Fn   *Lambda
}

// ProjectedMap is map over a projected repetition capture, whose items already are the mapped
// values: the list of the items of X.
type ProjectedMap struct {
	termBase
	X Term
}

// TermChildren returns the direct subterms of t.
func TermChildren(t Term) []Term {
	switch t := t.(type) {
	case *Member:
		return []Term{t.X}
	case *New:
		xs := make([]Term, len(t.Fields))
		for i, f := range t.Fields {
			xs[i] = f.Value
		}
		return xs
	case *Convert:
		return []Term{t.X}
	case *Tuple:
		return t.Elems
	case *Builtin:
		return t.Args
	case *Lambda:
		return []Term{t.Body}
	case *Binary:
		return []Term{t.L, t.R}
	case *Unary:
		return []Term{t.X}
	case *Cast:
		return []Term{t.X}
	case *TextEq:
		return []Term{t.L, t.R}
	case *FieldCompare:
		return []Term{t.L.X, t.R.X}
	case *TextIn:
		return []Term{t.Orig}
	case *ConcatFused:
		var xs []Term
		for _, p := range t.Parts {
			xs = append(xs, p.Args...)
			if p.X != nil {
				xs = append(xs, p.X)
			}
			if p.Fn != nil {
				xs = append(xs, p.Fn)
			}
		}
		return xs
	case *ProjectedMap:
		return []Term{t.X}
	}
	return nil
}

// WalkTerm calls f for t and its subterms in preorder.
func WalkTerm(t Term, f func(Term)) {
	if t == nil {
		return
	}
	f(t)
	for _, c := range TermChildren(t) {
		WalkTerm(c, f)
	}
}

// Raises reports whether evaluating t can end the parse with an error. Constants and reads of
// slots, locals and variables cannot, nor can a conversion that the checker proved; every other
// operation can (a missing field, a type error, a failed conversion or cast).
func Raises(t Term) bool {
	switch t := t.(type) {
	case *Const, *SlotRef, *Local, *VarRef:
		return false
	case *Lambda:
		return Raises(t.Body)
	case *Convert:
		return !t.Proven || Raises(t.X)
	}
	return true
}
