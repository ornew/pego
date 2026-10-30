// Package exec is the Exec IR of PEGO (design 065): the specification of the parser VM that every
// target implements, and the compositions that drive it.
//
// A Module declares the parser VM. Its abstract state is a set of Components (the memo table, the
// budget, ...) and Registers (scalar state such as the position). An Op is an operation of the
// parser VM: a transition of the abstract state with a signature, a behavior contract, the
// precondition that its callers guarantee, the components it reads and writes, a bound on what it
// charges the budget, and the features it belongs to. An Op states what happens, not how: each
// target implements it in its own language, and the conformance suite checks the implementation
// against the contract. Handles are opaque values that operations return and take, such as a memo
// entry.
//
// A Func is a composition: structured control over operations, other functions and scalar values.
// A Block has a label, and an Exit to it continues after the block; a Loop repeats its body until a
// Break or an Exit leaves it, and a Continue starts its next iteration; an If branches. Locals are
// scalars and handles; data structures live inside operations, where targets choose them. A Func
// has no exceptions: an operation marked NoReturn ends the parse when it does not return (for
// example when the budget is exhausted). Statements after an Exit, Break, Continue or Return are
// unreachable; Exec IR may hold them, and a backend leaves them out.
//
// Modules are built with the functions of build.go, checked by Verify, listed by String and run by
// Run, which executes a Func against a Machine that implements the operations. Run is how Go runs
// conformance cases against reference models and bound implementations.
package exec

import "fmt"

// Kind is the kind of a Type.
type Kind uint8

const (
	KInvalid Kind = iota
	// KBool is a truth value.
	KBool
	// KInt is a position or a count: at least 32 bits and exact up to 2^53 on every target.
	KInt
	// KI32, KU32 and KU64 are sized integers with defined wraparound, for packed fields and counters.
	KI32
	KU32
	KU64
	// KHandle is an opaque value of a declared HandleType.
	KHandle
)

// Type is the type of a value: a scalar kind, or a handle with the name of its HandleType.
type Type struct {
	Kind   Kind
	Handle string
}

// The scalar types.
var (
	TBool = Type{Kind: KBool}
	TInt  = Type{Kind: KInt}
	TI32  = Type{Kind: KI32}
	TU32  = Type{Kind: KU32}
	TU64  = Type{Kind: KU64}
)

// THandle returns the type of handles of the HandleType name.
func THandle(name string) Type { return Type{Kind: KHandle, Handle: name} }

func (t Type) String() string {
	switch t.Kind {
	case KBool:
		return "bool"
	case KInt:
		return "int"
	case KI32:
		return "i32"
	case KU32:
		return "u32"
	case KU64:
		return "u64"
	case KHandle:
		return t.Handle
	}
	return "invalid"
}

// integer reports whether t is one of the integer types.
func (t Type) integer() bool { return t.Kind >= KInt && t.Kind <= KU64 }

// Module is a parser VM declaration with its compositions.
type Module struct {
	Name string
	// Features are the boolean constants that a function can be specialized for (budget,
	// document, ...).
	Features   []string
	Components []*Component
	Registers  []*Register
	Handles    []*HandleType
	Ops        []*Op
	Funcs      []*Func
}

// Component is a part of the abstract state that only the operations declaring it may write.
type Component struct {
	Name string
	Doc  string
}

// Register is a scalar of the abstract state, such as the position.
type Register struct {
	Name string
	Type Type
	Doc  string
}

// HandleType is a type of opaque values. Its Component is the state component whose operations
// make its values.
type HandleType struct {
	Name      string
	Component string
	Doc       string
}

// Param is a named parameter of an operation or a function.
type Param struct {
	Name string
	Type Type
}

// Op is an operation of the parser VM.
type Op struct {
	Name string
	// Component is the state component the operation belongs to.
	Component string
	Params    []Param
	Results   []Type
	// Reads and Writes are the components and registers the operation reads and writes. An operation
	// may write only its own component and registers.
	Reads, Writes []string
	// Features are the features the operation exists in; a function may call it only if it is
	// specialized for all of them. Empty means every build.
	Features []string
	// NoReturn reports that the operation may end the parse instead of returning (an interruption).
	NoReturn bool
	// Contract is the behavior every implementation provides, in prose.
	Contract string
	// Requires is the precondition that callers guarantee; behavior outside it is unspecified, and
	// the conformance suite does not generate it.
	Requires string
	// Cost bounds the units an implementation charges the budget per call.
	Cost Cost
	// Laws are facts about the operation that passes may use.
	Laws []string
	// Transitional reports an operation that stands for an existing runtime function with its
	// current behavior and no conformance cases, until its mechanism migrates.
	Transitional bool
	// Imm describes the immediate operand of the operation, a static datum of the call that is not
	// a value of Exec IR (a literal, a character class, an expectation, a term of the grammar), or
	// is empty when the operation takes none. A call of the operation holds it in Call.Imm, which
	// backends read and passes keep.
	Imm string
}

// Cost bounds the units of budget that one call of an operation charges: at most Const plus, for
// each term, Coef times the value of its Quantity. A quantity is an abstract measure of the state
// before the call, such as the number of entries at a position, which the reference model of the
// component computes. Charges themselves are the implementation's: they are deterministic for a
// given target and within the bound (design 065, decision 9).
type Cost struct {
	Const int
	Terms []CostTerm
}

// CostTerm is one term of a Cost.
type CostTerm struct {
	Coef     int
	Quantity string
}

func (c Cost) String() string {
	s := fmt.Sprint(c.Const)
	for _, t := range c.Terms {
		s += fmt.Sprintf(" + %d*%s", t.Coef, t.Quantity)
	}
	return s
}

// Func is a composition: a function of Exec IR.
type Func struct {
	Name    string
	Params  []Param
	Results []Type
	// Locals are declared at the function's entry, with the zero value of their type (false, 0,
	// the null handle), and are visible in the whole body. Code lowered from a lower level
	// declares its variables here; compositions declare theirs with Let where they are needed.
	Locals []Param
	// Features are the features the function is specialized for.
	Features []string
	Body     *Block
	// Doc describes the function; backends print it as the comment of the printed function.
	Doc string
}

// Stmt is a statement.
type Stmt interface{ stmt() }

// Block is a sequence of statements. An Exit to its Label (if not empty) continues after it.
type Block struct {
	Label string
	Stmts []Stmt
}

// Loop repeats Body. A Break or an Exit to Label leaves it; a Continue to Label starts the next
// iteration. Init, if not nil, runs once before the loop; Cond, if not nil, is checked before each
// iteration, and the loop ends when it does not hold; Post, if not nil, runs after each iteration,
// including one that a Continue ends.
type Loop struct {
	Label string
	Init  *Set
	Cond  Expr
	Post  *Set
	Body  *Block
}

// If runs Init, if not nil, and then Then when Cond holds, and Else (which may be nil) otherwise.
type If struct {
	Init       *Set
	Cond       Expr
	Then, Else *Block
}

// Let declares new locals Names with the values of Value: one name for an expression, as many as
// the results of a call.
type Let struct {
	Names []string
	Value Expr
}

// Set assigns the places Dst: with one value per place, the values in order (all are evaluated
// first); with one call of as many results as places, its results.
type Set struct {
	Dst    []Place
	Values []Expr
}

// Place is what a Set assigns: a local, a register if Reg, or nothing if Name is empty (the value
// is discarded).
type Place struct {
	Name string
	Reg  bool
}

// Comment is a comment for readers of printed code; it does nothing.
type Comment struct{ Text string }

// Do calls an operation or a function for its effects and discards its results.
type Do struct{ Call *Call }

// Exit continues after the enclosing block labeled Label.
type Exit struct{ Label string }

// Break leaves the enclosing loop labeled Label; Continue starts its next iteration.
type Break struct{ Label string }
type Continue struct{ Label string }

// Return returns from the function with Values.
type Return struct{ Values []Expr }

// Assert fails a conformance case when Cond does not hold. Compositions do not use it.
type Assert struct {
	Cond Expr
	Msg  string
}

func (*Block) stmt()    {}
func (*Loop) stmt()     {}
func (*If) stmt()       {}
func (*Let) stmt()      {}
func (*Set) stmt()      {}
func (*Do) stmt()       {}
func (*Exit) stmt()     {}
func (*Break) stmt()    {}
func (*Continue) stmt() {}
func (*Return) stmt()   {}
func (*Assert) stmt()   {}
func (*Comment) stmt()  {}

// Expr is an expression.
type Expr interface{ expr() }

// Const is a constant: Value holds a bool for KBool, an int64 for KInt, an int32 for KI32, a
// uint32 for KU32 and a uint64 for KU64. The only constant of a handle type is the null handle,
// whose Value is nil.
type Const struct {
	Type  Type
	Value any
}

// Local reads a local or a parameter.
type Local struct{ Name string }

// Reg reads a register.
type Reg struct{ Name string }

// Feature is the value of a feature constant in the function's specialization.
type Feature struct{ Name string }

// Unary is a unary operator: "!" on bool, "-" on integers.
type Unary struct {
	Op string
	X  Expr
}

// Binary is a binary operator. Arithmetic ("+", "-", "*", "/", "%") takes two integers of one type
// and wraps at its width, and "/" and "%" truncate toward zero; the bit operators ("&", "|", "^",
// "<<", ">>") take two integers of one sized type (i32, u32, u64), because int has no fixed width;
// comparisons ("==", "!=", "<", "<=", ">", ">=") give bool, and "==" and "!=" also compare bools and
// handles; "&&" and "||" take bools and short-circuit.
type Binary struct {
	Op   string
	X, Y Expr
}

// Convert converts an integer to another integer type, wrapping at the target's width.
type Convert struct {
	To Type
	X  Expr
}

// Call calls the operation or function Callee. As an expression it must have exactly one result,
// except as the value of a Let or a Set with as many names or places as results. Imm is the
// immediate operand of an operation that declares one (Op.Imm).
type Call struct {
	Callee string
	Args   []Expr
	Imm    any
}

func (*Const) expr()   {}
func (*Local) expr()   {}
func (*Reg) expr()     {}
func (*Feature) expr() {}
func (*Unary) expr()   {}
func (*Binary) expr()  {}
func (*Convert) expr() {}
func (*Call) expr()    {}

// Op returns the operation named name, or nil.
func (m *Module) Op(name string) *Op {
	for _, o := range m.Ops {
		if o.Name == name {
			return o
		}
	}
	return nil
}

// Func returns the function named name, or nil.
func (m *Module) Func(name string) *Func {
	for _, f := range m.Funcs {
		if f.Name == name {
			return f
		}
	}
	return nil
}

func (m *Module) register(name string) *Register {
	for _, r := range m.Registers {
		if r.Name == name {
			return r
		}
	}
	return nil
}

func (m *Module) handle(name string) *HandleType {
	for _, h := range m.Handles {
		if h.Name == name {
			return h
		}
	}
	return nil
}

func (m *Module) component(name string) *Component {
	for _, c := range m.Components {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func (m *Module) hasFeature(name string) bool {
	for _, f := range m.Features {
		if f == name {
			return true
		}
	}
	return false
}
