// Package core is the Core IR of PEGO: typed parsing expressions whose observable effects are
// explicit, built once from a checked grammar (design 062). Optimizations are rewrites of Core IR
// with stated preconditions, and each target lowers from it.
//
// A Module holds the rules of one program variant with what they share: the interned
// expectations and character classes, the checked types, the declared variables and the call
// graph. A rule's body is a tree of Nodes; each node carries Attrs: whether its value is wanted,
// its value shape and the work it charges, set when it is built, and whether it is nullable, its
// first terminal and a summary of its effects, computed by Analyze. Actions, predicates and
// bindings are Terms, whose names are resolved to capture slots, lambda parameters, Operators
// Rule locals and variables, and which carry their checked types.
//
// The package depends on nothing of the engine: checked types are handles the engine makes
// (Type), and the handles of a cast (Cast.Check, Cast.Key) carry what only a backend needs.
package core

// Module is a program variant in Core IR.
type Module struct {
	// Rules holds the rules by ID, value-free twins included.
	Rules []*Rule
	// Starts holds the IDs of the start rules.
	Starts []int
	// Exps holds the expectations that nodes record, by ExpID.
	Exps []string
	// Classes holds the character classes of the nodes: equal classes are one Class.
	Classes []*Class
	// Types holds the named types of the grammar by name.
	Types map[string]Type
	// Vars holds the declared variables, in declaration order.
	Vars []*VarDecl
	// Calls holds, for each rule, the rules its body calls (Call nodes), in increasing order.
	Calls [][]int
	// SCC holds, for each rule, its strongly connected component of the call graph: rules call
	// each other, directly or not, exactly when they are in the same component.
	SCC []int
	// Cyclic reports, for each component, that its rules are on a cycle of calls: it has more
	// than one rule, or its rule calls itself.
	Cyclic []bool
	// Typed reports that the module carries the checked types of the program: every term but a
	// lambda, every capture slot and every rule's value have one.
	Typed bool
}

// Type is a checked type: a handle that the engine makes and the backends read. Core compares
// handles only for identity.
type Type any

// VarDecl is a declared variable.
type VarDecl struct {
	Name string
	Type Type
	// Init is the initial value: an int, a string, a bool or nil.
	Init any
}

// ExpID identifies an expectation of the module (Module.Exps).
type ExpID int

// Rule is a rule of a module.
type Rule struct {
	ID   int
	Name string
	// Where names the rule in the message of a failed action.
	Where string
	// Type is the checked type of the rule's value, or nil if it is not known (a value-free
	// twin).
	Type Type
	// Body is the expression the rule matches; an *OperatorsRule for an Operators Rule.
	Body Node
	// Want reports that the body's value is used: false for a value-free twin and for a rule
	// whose action reads only captures.
	Want bool
	// Scope holds the rule's capture slots, in slot order.
	Scope []Slot
	// Finish is how the rule makes its value from its body's value and its captures.
	Finish Finish
	Policy Policy
	// Twin is the value-free twin of the rule, if it has one.
	Twin *Rule
}

// Slot is a capture slot of a scope.
type Slot struct {
	Name string
	// Type is the checked type of the captured value, or nil if the capture has no value type
	// (a capture that never builds one, as in a value-free twin).
	Type Type
}

// SlotNames returns the names of the slots, in order.
func SlotNames(s []Slot) []string {
	if s == nil {
		return nil
	}
	names := make([]string, len(s))
	for i, x := range s {
		names[i] = x.Name
	}
	return names
}

// Policy is how a rule is called.
type Policy struct {
	// Memo reports that ordinary parses memoize the rule; Transient that only Documents do.
	Memo, Transient bool
	// Plain reports that the rule is never memoized outside Documents and has no captures.
	Plain bool
	// Leader reports a left-recursion leader, whose growth the runtime owns.
	Leader bool
	// Splice reports a rule that makes no node of its own.
	Splice bool
	// NoValue reports a value-free twin.
	NoValue bool
	// Seen is the rule's index among the rules memoized from their second call, or -1.
	Seen int
	// Vars are the variables the rule or its callees read.
	Vars []string
}

// FinishKind selects how a rule makes its value.
type FinishKind uint8

const (
	// FinishNone: the value is nil (a value-free twin).
	FinishNone FinishKind = iota
	// FinishAction: the value is the Action term.
	FinishAction
	// FinishSplice: the value is the body's value, which the rule does not wrap.
	FinishSplice
	// FinishScope: the value is the scope value of the body, with the captures attached.
	FinishScope
)

// Finish is how a rule makes its value.
type Finish struct {
	Kind   FinishKind
	Action Term
	// Shape is the shape of the body's value (FinishScope).
	Shape Shape
	// NodeType, if not "", is the named node type the value is converted to.
	NodeType string
}

// Shape is the static shape of the value of an expression (the runtime's Shaper).
type Shape struct {
	// Elems reports that the expression's value is the tuple node of its elements.
	Elems bool
	// K is the number of valued elements.
	K int
	// Mode is how the value where it takes one element is made.
	Mode SingleMode
	// Tuple reports that the value of a scope with this body is the tuple node.
	Tuple bool
	// Shared reports that the tuple node is shared through a splice rule's memo entry.
	Shared bool
}

// SingleMode is how the value of an expression is made where it takes one element (an
// alternative, the operand of an option, a captured or recovered expression).
type SingleMode uint8

const (
	// SingleNone: no value.
	SingleNone SingleMode = iota
	// SinglePass: the value of the element itself.
	SinglePass
	// SingleTuple: the tuple node of the elements.
	SingleTuple
)

// Class is a character class.
type Class struct {
	Ranges  []Range
	Negated bool
	// Exp is the expectation the class records on failure.
	Exp ExpID
}

// Range is an inclusive range of code points.
type Range struct{ Lo, Hi rune }
