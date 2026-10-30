// Package flow is the Flow IR of PEGO (design 062): one function per rule variant, with explicit
// parser state and structured control, from which the host-language backends print code.
//
// A Func has variables (the saved state of backtracking points, capture values, intermediate
// values and flags) and a body of statements. Control is structured: a Block has a label, and an
// Exit to it continues after the block; a Loop repeats its body until an Exit leaves it, and a
// Continue starts its next iteration; an If branches. The other statements and the expressions
// are primitive operations on the parser state that mirror the runtime's helpers: reading and
// moving the position, matching terminals, recording expectations, calling rules, building
// values, evaluating terms and charging work.
//
// A function is local or framed. The body of a local function, a whole rule, succeeds by running
// to its end, with the rule's value in the Result variable, and fails by an Exit to the function's
// Fail label; its captures are variables. A framed function matches an expression whose captures
// the runtime keeps in a frame (the body of a left-recursion leader or of a rule that recovers,
// or a line of an Operators Rule): it succeeds with the value Value, and the runtime owns the
// frame, the rule's value and its finish. Statements after an Exit, up to the end of a block that
// is exited to or of a loop, are unreachable; Flow IR keeps them, and a printer leaves out what is
// unreachable.
package flow

import "github.com/ornew/pego/internal/ir/core"

// Func is a function of Flow IR: the body of a rule variant.
type Func struct {
	Name string
	Rule *core.Rule
	// Vars holds the function's local variables, in the order they were made.
	Vars []*Var
	// Body is the statements of the function. Running to its end is success; an Exit to Fail is
	// failure.
	Body []Stmt
	Fail *Label
	// Start, Result and Ctx are the entry variables: the position at which the rule was entered,
	// the rule's value, and the context of the rule's action. The function's entry declares them;
	// Start is set before the body runs. A framed function has none.
	Start, Result, Ctx *Var
	// Binds reports that the body may bind variables, which the entry restores.
	Binds bool
	// Frame reports a framed function. Value is the value it succeeds with, nil where it cannot
	// succeed; Cut, if not nil, is the cut flag of its outermost scope, which it reads from the
	// runtime (RegCut) on entry. On both returns, success and failure, the function publishes Cut
	// to RegCut again. The body holds no statement for the publication: the target writes it
	// where the function returns, after the body and at Fail.
	Frame bool
	Value Expr
	Cut   *Var
	// Node reports a function that builds the values of the Node runtime (NewTerminalNode,
	// NewBranchNode). A function builds the values of one runtime: a Node function has no typed value
	// operation, and a typed function has no Node one (Verify).
	Node bool
	// Work reports a function lowered with work controls (Options.Work); Verify then checks the
	// charging of work (verifyWork).
	Work bool
	// Document reports a function lowered for a build with Documents (Options.Document); Verify then checks
	// that every in-place read has the recording primitive as its alternative.
	Document bool
	// Entered reports a function lowered with work controls, whose entry the generator charges: the method
	// that holds the function (g.method, flatMethod or plainCall) begins with one StepWork, for the body of
	// a rule, of a frame and of a line or the skip of an Operators Rule alike. The function charges nothing
	// for its entry and verifyWork takes the entry as its first charge; TestGeneratedRuleEntryCharge checks
	// the methods of the checked-in parsers.
	Entered bool
}

// Type is the type of a variable.
type Type uint8

const (
	// TypeInt is a position, a count or a length.
	TypeInt Type = iota
	TypeBool
	TypeRune
	// TypeValue is a value of the runtime.
	TypeValue
	// TypeEnv is a variable environment.
	TypeEnv
	// TypeExpMark is a mark of the recorded expectations.
	TypeExpMark
	// TypeExpIDs is a list of expectations.
	TypeExpIDs
	// TypeVals is a list of values being gathered.
	TypeVals
	// TypeCtx is the context of an action.
	TypeCtx
	// TypeMark is a mark of the whole parser state (Mark).
	TypeMark
	// TypeFrame is a capture frame of the runtime.
	TypeFrame
	// TypeError is a syntax error.
	TypeError
)

var typeNames = [...]string{"int", "bool", "rune", "value", "env", "expmark", "expids", "vals", "ctx", "mark", "frame", "error"}

func (t Type) String() string { return typeNames[t] }

// Var is a variable of a function.
type Var struct {
	// Name is unique in the function.
	Name string
	Type Type
	// Entry reports an entry variable (Func.Start, Result or Ctx).
	Entry bool
}

// Label names a block or a loop.
type Label struct{ Name string }

// Stmt is a statement.
type Stmt interface{ stmt() }

// Block runs its body; an Exit to its label continues after it.
type Block struct {
	Label *Label
	Body  []Stmt
}

// Loop runs its body repeatedly until an Exit to its label leaves it. With Counter, the loop
// sets Counter to 0 first, adds one after each iteration and ends after Max iterations; with
// While, it ends when While no longer holds before an iteration.
type Loop struct {
	Label   *Label
	Counter *Var
	Max     int
	While   Expr
	Body    []Stmt
}

// If runs Init, if not nil, and then Then when Cond holds, otherwise Else.
type If struct {
	Init       *Set
	Cond       Expr
	Then, Else []Stmt
}

// Exit leaves the block or loop To, which encloses it.
type Exit struct{ To *Label }

// Continue starts the next iteration of the loop To, which encloses it.
type Continue struct{ To *Label }

// Set assigns the values X to Dst, in order. A single X may be an operation with as many results
// as Dst has places (Results).
type Set struct {
	Dst []LValue
	X   []Expr
}

// Inc adds one to V.
type Inc struct{ V *Var }

// Step charges one step of work.
type Step struct{}

// Silent adds Delta to the silent depth: expectations are not recorded while it is positive.
type Silent struct{ Delta int }

// Peek reads the character at the position without consuming it: its code point into Ch, its
// size into Size and whether there is one into OK. Ch and Size may be nil (not kept).
//
// Peek reads as the runtime's Peek does, which also charges the work of the read and, in a
// Document parse, records how far the parse examined the input. A target that inlines the read of
// a code point in memory must do the same: charge one unit of work unless the function is
// lowered without work (Options.Work), and leave Document parses to the runtime's Peek.
type Peek struct{ Ch, Size, OK *Var }

// Advance moves the position forward by N.
type Advance struct{ N Expr }

// Expect records the expectation Exp at At; with Message, as the message of a failed @error.
type Expect struct {
	At      Expr
	Exp     core.ExpID
	Message bool
}

// ObserveSkip reports to the parser's observation that the guard of a choice skips the alternative
// Alt.
type ObserveSkip struct{ Alt core.Node }

// Restore goes back to the position Pos and the number of recovered errors Rec.
type Restore struct{ Pos, Rec Expr }

// MergeExpected merges the expectations Exps, recorded at Far, into the enclosing expectation
// scope.
type MergeExpected struct{ Far, Exps Expr }

// MergeRenamed merges the expectations Exps, recorded at Far, into the enclosing expectation
// scope, renamed to Exp (@diagnostic).
type MergeRenamed struct {
	Far, Exps Expr
	Exp       core.ExpID
}

// PushKid pushes X on the stack of gathered values.
type PushKid struct{ X Expr }

// DropKids truncates the stack of gathered values to Base.
type DropKids struct{ Base Expr }

// StreamStart begins the stream of a stream parse: the repetition that delivers its elements is about
// to run (runtime StreamStart). It is guarded by StreamOn.
type StreamStart struct{}

// EmitStream delivers the committed element X, which is nil for a value-free repetition, to the
// callback of a stream parse; the parse has consumed input up to the position.
type EmitStream struct{ X Expr }

// Append appends X to the list Vals; with Spread, the children of the tuple node X.
type Append struct {
	Vals   *Var
	X      Expr
	Spread bool
}

// SetWhere names the rule whose action runs, for the messages of its errors.
type SetWhere struct{ Name string }

// DropCreated drops the structs made since the context C began.
type DropCreated struct{ C Expr }

// Unfresh marks the node X, if X is one, as no longer owned by its maker.
type Unfresh struct{ X Expr }

// Reset goes back to the parser state of the mark M (MarkState).
type Reset struct{ M Expr }

// SetCapture sets the capture Slot of the current frame to X.
type SetCapture struct {
	Slot int
	X    Expr
}

// ClearVals sets the values of the frame F to nil.
type ClearVals struct{ F *Var }

// DropTrail drops the trail entries recorded since the mark M.
type DropTrail struct{ M Expr }

// FreeFrame returns the frame F to the runtime.
type FreeFrame struct{ F *Var }

// AppendError appends the syntax error X to the recovered errors.
type AppendError struct{ X Expr }

// DeclareNames declares the names of a frame scope, which framed repetitions name before their
// element and declare after it.
type DeclareNames struct{ N *Names }

// Comment is a comment for readers of the printed code.
type Comment struct{ Text string }

func (*Block) stmt()         {}
func (*Loop) stmt()          {}
func (*If) stmt()            {}
func (*Exit) stmt()          {}
func (*Continue) stmt()      {}
func (*Set) stmt()           {}
func (*Inc) stmt()           {}
func (*Step) stmt()          {}
func (*Silent) stmt()        {}
func (*Peek) stmt()          {}
func (*Advance) stmt()       {}
func (*Expect) stmt()        {}
func (*Restore) stmt()       {}
func (*MergeExpected) stmt() {}
func (*MergeRenamed) stmt()  {}
func (*PushKid) stmt()       {}
func (*DropKids) stmt()      {}
func (*StreamStart) stmt()   {}
func (*EmitStream) stmt()    {}
func (*Append) stmt()        {}
func (*SetWhere) stmt()      {}
func (*DropCreated) stmt()   {}
func (*Unfresh) stmt()       {}
func (*Reset) stmt()         {}
func (*SetCapture) stmt()    {}
func (*ClearVals) stmt()     {}
func (*DropTrail) stmt()     {}
func (*FreeFrame) stmt()     {}
func (*AppendError) stmt()   {}
func (*DeclareNames) stmt()  {}
func (*Comment) stmt()       {}
func (*ObserveSkip) stmt()   {}

// LValue is what Set assigns: a variable, a register or nothing.
type LValue interface{ lvalue() }

// Reg is a register of the parser.
type Reg uint8

const (
	// RegPos is the position.
	RegPos Reg = iota
	// RegEnv is the variable environment.
	RegEnv
	// RegFrame is the current capture frame.
	RegFrame
	// RegCut is the cut flag of the scope the runtime entered last.
	RegCut
)

// Discard is a place whose value is dropped.
type Discard struct{}

func (*Var) lvalue()     {}
func (Reg) lvalue()      {}
func (*Discard) lvalue() {}

// Expr is an expression.
type Expr interface{ expr() }

// Use reads a variable.
type Use struct{ V *Var }

// Read reads a register.
type Read struct{ R Reg }

// RecLen is the number of recovered errors.
type RecLen struct{}

// KidsLen is the height of the stack of gathered values.
type KidsLen struct{}

// StreamOn reports that the invocation delivers the elements of its #stream repetition: a stream
// parse with a callback, at the top call of its start rule.
type StreamOn struct{}

// Nil is the nil value.
type Nil struct{}

// Int is an integer constant.
type Int struct{ N int }

// Bool is a boolean constant.
type Bool struct{ B bool }

// Not negates X.
type Not struct{ X Expr }

// Or is X || Y.
type Or struct{ X, Y Expr }

// Cmp compares two integers: Op is ==, != or <.
type Cmp struct {
	Op   string
	L, R Expr
}

// HasLit reports that the input at the position begins with the literal, read in place without
// charging work or recording anything; it is false where the input cannot be read in place.
type HasLit struct{ Lit *core.Lit }

// MatchLit matches the literal, recording its expectation on failure. Its results are its value
// (not used) and whether it matched.
type MatchLit struct{ Lit *core.Lit }

// MatchClass matches one character of the class through its membership table, recording the
// class's expectation on failure, and reports whether it matched.
type MatchClass struct{ Class *core.Class }

// InClass reports whether the code point Ch is in the class.
type InClass struct {
	Class *core.Class
	Ch    *Var
}

// GuardFails reports that the guard skips its alternative: the character that More (there is
// one) and Ch describe cannot begin it, and the nesting limit allows the guard's depth of calls.
type GuardFails struct {
	Guard    *core.First
	Ch, More *Var
}

// AtAnchor reports whether the position is at the anchor.
type AtAnchor struct{ Kind core.AnchorKind }

// SkipTerminal runs the terminal region as one program and reports whether it matched.
type SkipTerminal struct{ T *core.Terminal }

// SkipClasses matches the classes of the run in turn and reports whether they matched.
type SkipClasses struct{ Run *core.ClassRun }

// CallRule calls rule Rule from Operators Rule level Level. Its results are the rule's value and
// whether it matched.
type CallRule struct{ Rule, Level int }

// CallOutline runs the node Node, which a function of its own lowers (LowerOutline), under the current
// frame, whose capture scope has the slots Slots. Its results are the value of the node, built if Build,
// and whether it matched.
type CallOutline struct {
	Node  core.Node
	Slots []core.Slot
	Build bool
}

// Isolate begins an expectation scope at the position and is its mark.
type Isolate struct{}

// Unisolate ends the expectation scope Mark. Its results are the farthest failure inside it and
// its expectations.
type Unisolate struct{ Mark Expr }

// Text is the input from Start to the position.
type Text struct{ Start Expr }

// LiteralText is the text of the literal Lit matched from Start, which differs from Lit where Lit
// holds the replacement character.
type LiteralText struct {
	Start Expr
	Lit   string
}

// Str is a string constant.
type Str struct{ S string }

// NewMatch is a match value from Start to the position with the text Text.
type NewMatch struct{ Start, Text Expr }

// NewNode is a tuple node, or with List a list node, from Start to the position with the
// children Kids.
type NewNode struct {
	List        bool
	Start, Kids Expr
}

// NewTerminalNode is a terminal of the Node runtime from Start to the position, with the text Text.
// Only a Node function (Func.Node) builds it; a typed function builds NewMatch.
type NewTerminalNode struct{ Start, Text Expr }

// NewBranchNode is a branch of the Node runtime from Start to the position: a sequence, or with List a
// list, of the children Kids. Only a Node function (Func.Node) builds it; a typed function builds
// NewNode.
type NewBranchNode struct {
	List        bool
	Start, Kids Expr
}

// NewVals is an empty list of values with room for N.
type NewVals struct{ N int }

// Kids is the list of the values gathered above Base, which it removes from the stack.
type Kids struct{ Base Expr }

// ShapeSingle is the value of X, matched from Start by an expression of the shape, where it takes
// one element. Its results are the value and whether it is a single element (not used).
type ShapeSingle struct {
	Shape    core.Shape
	X, Start Expr
}

// ShapeCapture is the value of X, matched from Start by an expression of the shape, where it is
// captured.
type ShapeCapture struct {
	Shape    core.Shape
	X, Start Expr
}

// ScopeValue is the value of a scope whose body, of the shape, matched X from Start.
type ScopeValue struct {
	Shape    core.Shape
	X, Start Expr
}

// Names are the slot names of a capture scope, which printers declare once.
type Names struct {
	List []string
	// Declared reports that a DeclareNames statement declares them; otherwise their first use
	// does.
	Declared bool
}

// AttachCaptures is X, matched from Start, with the captures of the scope whose slots are Names
// attached: the values of the variables Vals, or of the frame Frame if it is not nil.
type AttachCaptures struct {
	X     Expr
	Names *Names
	Vals  []*Var
	Frame *Var
	Start Expr
}

// MarkState is a mark of the whole parser state, which Reset returns to. The position is always part of
// it; Env, Trail and Rec report whether the code between the mark and its Reset may change the variable
// environment, add to the capture trail, or append recovered errors, so that a target may restore
// only those.
type MarkState struct{ Env, Trail, Rec bool }

// MarkPos is the position of the mark M.
type MarkPos struct{ M *Var }

// NewFrame is a new frame for the scope of the names N.
type NewFrame struct{ N *Names }

// FrameVal is the value of the slot Slot of the frame F.
type FrameVal struct {
	F    *Var
	Slot int
}

// Keep is the list of expectations Exps, kept beyond the expectation scope that recorded them.
type Keep struct{ Exps Expr }

// MakeError is the syntax error at Far with the expectations Exps.
type MakeError struct{ Far, Exps Expr }

// ErrorValue is the value of a recovered error X over the input from Start to the position.
type ErrorValue struct {
	Start Expr
	X     Expr
}

// NewErrorNode is the value of a recovered error X in the Node runtime over the input from Start to the
// position: an error node. Only a Node function (Func.Node) builds it; a typed function builds ErrorValue.
type NewErrorNode struct {
	Start Expr
	X     Expr
}

// FreshNode is X if it is a node its maker still owns, or nil.
type FreshNode struct{ X Expr }

// ConvertResult is the value X of rule Rule converted to the rule's node type; Owned, if not
// nil, is the fresh node of the body, which converts in place.
type ConvertResult struct {
	Rule     int
	X, Owned Expr
}

// UseCtx is a new context for the action of the rule entered at Start.
type UseCtx struct{ Start Expr }

// Captures are the captures a term reads: Vars holds the variable of each slot of the scope,
// whose names are Names, and a nil variable reads as nil. In a framed function Vars is nil: the
// term reads the current frame.
type Captures struct {
	Names []string
	Vars  []*Var
}

// Pred evaluates the term of the predicate Node with the captures Caps and reports whether it
// holds.
type Pred struct {
	Node *core.Pred
	Caps Captures
}

// Bind binds the variable of Node to the value of its term, evaluated with the captures Caps,
// and reports whether the evaluation succeeded.
type Bind struct {
	Node *core.BindNode
	Caps Captures
}

// Eval is the value of the action Term of the function's rule, evaluated in the context C with
// the captures Caps.
type Eval struct {
	Term core.Term
	C    *Var
	Caps Captures
}

// CtxFinish is the value X of an action finished in the context C.
type CtxFinish struct {
	C *Var
	X Expr
}

func (*Use) expr()             {}
func (*Read) expr()            {}
func (*RecLen) expr()          {}
func (*KidsLen) expr()         {}
func (*StreamOn) expr()        {}
func (*Nil) expr()             {}
func (*Int) expr()             {}
func (*Bool) expr()            {}
func (*Not) expr()             {}
func (*Or) expr()              {}
func (*Cmp) expr()             {}
func (*HasLit) expr()          {}
func (*MatchLit) expr()        {}
func (*MatchClass) expr()      {}
func (*InClass) expr()         {}
func (*GuardFails) expr()      {}
func (*AtAnchor) expr()        {}
func (*SkipTerminal) expr()    {}
func (*SkipClasses) expr()     {}
func (*CallRule) expr()        {}
func (*CallOutline) expr()     {}
func (*Isolate) expr()         {}
func (*Unisolate) expr()       {}
func (*Text) expr()            {}
func (*LiteralText) expr()     {}
func (*Str) expr()             {}
func (*NewMatch) expr()        {}
func (*NewNode) expr()         {}
func (*NewTerminalNode) expr() {}
func (*NewBranchNode) expr()   {}
func (*NewVals) expr()         {}
func (*Kids) expr()            {}
func (*ShapeSingle) expr()     {}
func (*ShapeCapture) expr()    {}
func (*ScopeValue) expr()      {}
func (*AttachCaptures) expr()  {}
func (*FreshNode) expr()       {}
func (*ConvertResult) expr()   {}
func (*UseCtx) expr()          {}
func (*Pred) expr()            {}
func (*Bind) expr()            {}
func (*Eval) expr()            {}
func (*CtxFinish) expr()       {}
func (*MarkState) expr()       {}
func (*MarkPos) expr()         {}
func (*NewFrame) expr()        {}
func (*FrameVal) expr()        {}
func (*Keep) expr()            {}
func (*MakeError) expr()       {}
func (*ErrorValue) expr()      {}
func (*NewErrorNode) expr()    {}

// Results returns the number of results of x: 2 for the operations that have two, 1 otherwise.
func Results(x Expr) int {
	switch x.(type) {
	case *MatchLit, *CallRule, *CallOutline, *Unisolate, *ShapeSingle:
		return 2
	}
	return 1
}

// Subexprs calls f with each expression that x directly holds, in order.
func Subexprs(x Expr, f func(Expr)) {
	each := func(xs ...Expr) {
		for _, y := range xs {
			if y != nil {
				f(y)
			}
		}
	}
	switch x := x.(type) {
	case *Not:
		each(x.X)
	case *Or:
		each(x.X, x.Y)
	case *Cmp:
		each(x.L, x.R)
	case *Unisolate:
		each(x.Mark)
	case *Text:
		each(x.Start)
	case *LiteralText:
		each(x.Start)
	case *NewMatch:
		each(x.Start, x.Text)
	case *NewNode:
		each(x.Start, x.Kids)
	case *NewTerminalNode:
		each(x.Start, x.Text)
	case *NewBranchNode:
		each(x.Start, x.Kids)
	case *Kids:
		each(x.Base)
	case *ShapeSingle:
		each(x.X, x.Start)
	case *ShapeCapture:
		each(x.X, x.Start)
	case *ScopeValue:
		each(x.X, x.Start)
	case *AttachCaptures:
		each(x.X, x.Start)
	case *Keep:
		each(x.Exps)
	case *MakeError:
		each(x.Far, x.Exps)
	case *ErrorValue:
		each(x.Start, x.X)
	case *NewErrorNode:
		each(x.Start, x.X)
	case *FreshNode:
		each(x.X)
	case *ConvertResult:
		each(x.X, x.Owned)
	case *UseCtx:
		each(x.Start)
	case *CtxFinish:
		each(x.X)
	}
}

// Uses calls f with each variable that x reads, in order.
func Uses(x Expr, f func(*Var)) {
	switch x := x.(type) {
	case *Use:
		f(x.V)
	case *Not:
		Uses(x.X, f)
	case *Or:
		Uses(x.X, f)
		Uses(x.Y, f)
	case *Cmp:
		Uses(x.L, f)
		Uses(x.R, f)
	case *InClass:
		f(x.Ch)
	case *GuardFails:
		f(x.More)
		f(x.Ch)
	case *Unisolate:
		Uses(x.Mark, f)
	case *Text:
		Uses(x.Start, f)
	case *LiteralText:
		Uses(x.Start, f)
	case *NewMatch:
		Uses(x.Start, f)
		Uses(x.Text, f)
	case *NewNode:
		Uses(x.Start, f)
		Uses(x.Kids, f)
	case *NewTerminalNode:
		Uses(x.Start, f)
		Uses(x.Text, f)
	case *NewBranchNode:
		Uses(x.Start, f)
		Uses(x.Kids, f)
	case *Kids:
		Uses(x.Base, f)
	case *ShapeSingle:
		Uses(x.X, f)
		Uses(x.Start, f)
	case *ShapeCapture:
		Uses(x.X, f)
		Uses(x.Start, f)
	case *ScopeValue:
		Uses(x.X, f)
		Uses(x.Start, f)
	case *AttachCaptures:
		Uses(x.X, f)
		for _, v := range x.Vals {
			f(v)
		}
		if x.Frame != nil {
			f(x.Frame)
		}
		Uses(x.Start, f)
	case *MarkPos:
		f(x.M)
	case *FrameVal:
		f(x.F)
	case *Keep:
		Uses(x.Exps, f)
	case *MakeError:
		Uses(x.Far, f)
		Uses(x.Exps, f)
	case *ErrorValue:
		Uses(x.Start, f)
		Uses(x.X, f)
	case *NewErrorNode:
		Uses(x.Start, f)
		Uses(x.X, f)
	case *FreshNode:
		Uses(x.X, f)
	case *ConvertResult:
		Uses(x.X, f)
		if x.Owned != nil {
			Uses(x.Owned, f)
		}
	case *UseCtx:
		Uses(x.Start, f)
	case *Pred:
		capUses(x.Node.Term, x.Caps, f)
	case *Bind:
		capUses(x.Node.Term, x.Caps, f)
	case *Eval:
		f(x.C)
		capUses(x.Term, x.Caps, f)
	case *CtxFinish:
		f(x.C)
		Uses(x.X, f)
	}
}

// capUses calls f with the variables of caps that the term t reads.
func capUses(t core.Term, caps Captures, f func(*Var)) {
	core.WalkTerm(t, func(t core.Term) {
		if r, ok := t.(*core.SlotRef); ok && r.Slot < len(caps.Vars) && caps.Vars[r.Slot] != nil {
			f(caps.Vars[r.Slot])
		}
	})
}
