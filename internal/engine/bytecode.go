package engine

import "fmt"

// Module is a grammar compiled to bytecode. It is the only thing the VM runs.
// The instruction set and its semantics are specified in docs/bytecode.md.
type Module struct {
	Strings    []string   // string table
	Classes    []Class    // character class table
	Types      []TypeInfo // type table (for checking struct fields, and terminal types)
	FieldLists [][]int    // field name lists for new (string table indices)
	Scopes     [][]int    // scopes of repetition elements (lists of capture names)
	Rules      []RuleInfo // rule table
	Pratts     []PrattInfo
	Code       []Instr // matching code
	Exprs      []Instr // expression code (actions and predicates)
}

// Class is a character class. Ranges is a list of lower and upper bound pairs.
type Class struct {
	Ranges  []rune
	Negated bool
	Desc    int // expectation string (string table index)
}

// TypeInfo describes a type.
type TypeInfo struct {
	Name     int   // string table index
	Terminal bool  // whether it is a terminal type (a struct if false)
	Fields   []int // struct field names (string table indices)
}

// RuleInfo describes a rule.
type RuleInfo struct {
	Name         int // string table index
	Entry        int // start of the body code (for a Pratt expression rule, a PRATT instruction followed by RETURN)
	Scope        []int
	Action       int // start of the action's expression code (-1 if none)
	BodyIsSeq    bool
	TerminalType int // name of the terminal type (string table index, -1 if none)
	Memo         bool
	Leader       bool
	Positional   bool
	Pratt        int // Pratt table index (-1 if none)
	// NoValue reports that this is a value-free twin, called from places where the value is
	// discarded (its value is always nil). It has the same name as the original rule and is placed
	// after all the original rules in the rule table.
	NoValue bool
	// Transient reports that the rule is not memoized in ordinary parses (even if Memo is set). In
	// incremental parsing, Memo applies.
	Transient bool
	// Vars are the variables the rule or its callees read (string table indices, sorted by name).
	// The rule's memo entries are keyed by their values at the call.
	Vars []int
}

// PrattInfo is a Pratt expression table.
type PrattInfo struct {
	Skip     int // start of the skip code (-1 if none)
	Operands []LineInfo
	Prefix   []OpInfo
	Led      []OpInfo // infix and postfix
}

// LineInfo is a Pratt operand line or operator line. Its code ends with END.
type LineInfo struct {
	Entry  int
	Scope  []int
	Action int // start of the expression code (-1 if none)
	IsSeq  bool
}

// OpInfo is a Pratt operator.
type OpInfo struct {
	ID    int
	Kind  int // opPrefix, opPostfix, opInfix
	Assoc int // assocLeft, assocRight, assocNone
	Level int
	Line  LineInfo
}

// Pratt operator kinds and associativity
const (
	opPrefix = iota
	opPostfix
	opInfix
)

const (
	assocLeft = iota
	assocRight
	assocNone
)

// Instr is an instruction. The meaning of its operands depends on the instruction. Jump targets
// are instruction indices.
type Instr struct {
	Op      Op
	A, B, C int32
}

// Op is an opcode.
type Op uint8

// Matching instructions
const (
	// STR s, d, b: match string s of the string table. On failure, record string d as the
	// expectation. If b is 1, push a Match.
	OpStr Op = iota + 1
	// CLASS c, d, b: match one character of character class c of the character class table.
	OpClass
	// ANY b: match any one character.
	OpAny
	// TOP b: empty match. If b is 1, push an empty Match.
	OpTop
	// FAIL: fail.
	OpFail
	// ASSERT k: position condition (k: 0 start of input, 1 end of input, 2 start of line, 3 end of
	// line).
	OpAssert
	// JUMP l: go to l.
	OpJump
	// CHOICE l: push a choice entry. On failure, restore the state and go to l.
	OpChoice
	// COMMIT l: pop the topmost choice entry and go to l.
	OpCommit
	// CUT: mark the current choice entry as cut.
	OpCut
	// PUSHPOS: push the current position (start of Seq, List, @).
	OpPushPos
	// PUSHNIL: push nil.
	OpPushNil
	// SEQ n: pop n values and the position below them, and push a Seq.
	OpSeq
	// ATOMIC b: pop a position and push the Match from there to the current position (nothing is
	// pushed if b is 0).
	OpAtomic
	// CAPTURE s, b: write the top value to slot s of the current frame. If b is 1, pop the value
	// (if 0, leave it).
	OpCapture
	// REPEAT min, max, sc: start a repetition. max -1 means unbounded. sc is the index of the element
	// scope (-1 if none). When building values, the start position is pushed with PUSHPOS just
	// before.
	OpRepeat
	// ITER l: push an entry for one iteration. On failure, restore the state and go to l.
	OpIter
	// NEXT l, f, s: commit one iteration and go to l if the repetition can continue. f is 1 to keep
	// the value, 2 to pass it to the stream, 0 to discard it, and 3 to push the value of slot s of
	// the element's frame instead (a projected repetition, whose elements have no value).
	OpNext
	// ENDREPEAT b: end the repetition, check the count, and push a List if b is 1.
	OpEndRepeat
	// LOOK l, neg: start a lookahead (stop recording expectations). If neg is 1, it is a negative
	// lookahead: if the inner expression fails, restore the state and go to l. For a positive
	// lookahead, if the inner expression fails, restore the state and fail.
	OpLook
	// ENDLOOK neg: the inner expression succeeded. If positive, restore only the position and the
	// value stack and continue (captures and variables are kept). If negative, restore the state and
	// fail.
	OpEndLook
	// CALL r, min, k: call rule r at binding level min. If k is 1, push the value.
	OpCall
	// PRATT p: parse the expression of Pratt table p and push the value (used in rule bodies).
	OpPratt
	// PRED e: evaluate expression code e and fail if it is false or an error.
	OpPred
	// ASSIGN name, e: define the variable name with the value of expression code e.
	OpAssign
	// LABEL m: start #error.
	OpLabel
	// ENDLABEL: end #error.
	OpEndLabel
	// RECOVER l: start #recover. On failure, prepare for recovery and go to l (the skip code).
	OpRecover
	// ENDRECOVER l: the #recover expression succeeded. Go to l.
	OpEndRecover
	// ENDSKIP b: end skipping, record the error, and push an Error (nothing is pushed if b is 0).
	OpEndSkip
	// RETURN: end of a rule body.
	OpReturn
	// END: end of the code of a Pratt section.
	OpEnd
	// SCAN c, min, max: consume single characters of character class c (-1 for any character) until
	// one does not match or max times (-1 for unbounded), and fail if the count is less than min. It
	// is a fast form of a value-free single-character repetition that records the expectation once,
	// at the position that did not match.
	OpScan
	// GUARD c, d, l: first-character dispatch. If the next character is not in character class c
	// (or there is none), and d more rule calls would not exceed the nesting limit, record the
	// class's expectation and go to l (the next alternative of a choice, which this alternative
	// could not match). Otherwise continue.
	OpGuard
)

// Expression instructions
const (
	// EINT v: push the integer v.
	EInt Op = iota + 100
	// ESTR s: push the string s.
	EStr
	// EBOOL v: push a boolean.
	EBool
	// ENIL: push nil.
	ENil
	// ECAP s: push the value of capture slot s.
	ECap
	// EITEM n: push $n.
	EItem
	// ELOCAL i: push the i-th local (lambda argument, $lhs, $rhs, $op).
	ELocal
	// EVAR name: push the value of a variable.
	EVar
	// EMEMBER name: pop a value and push its field.
	EMember
	// ENEW t, f: build a struct of type t from as many values as there are names in field name list
	// f.
	ENew
	// EBIN op: binary operation (op is a string table index).
	EBin
	// EUNARY op: unary operation.
	EUnary
	// EAND l / EOR l: short-circuit evaluation. Check the boolean; if the result is determined, leave
	// the value and go to l.
	EAnd
	EOr
	// EBOOLCHK op: check that the top value is a boolean.
	EBoolChk
	// EFUNC entry, n: push a lambda with n parameters.
	EFunc
	// ECALL f, n: call built-in function f with n arguments.
	ECall
	// ERET: return the expression's value.
	ERet
	// ETEXTCHK: check that text(x) of the top value x is defined (instruction set 2).
	ETextChk
	// ETEXTEQ neg: pop two values and push whether their texts are equal (unequal if neg)
	// (instruction set 2).
	ETextEq
	// ELISTBEGIN: start gathering list elements (instruction set 2).
	EListBegin
	// ELISTPUSH n: pop n values and add them as elements, like list (instruction set 2).
	EListPush
	// EMAPPUSH: pop a list and a function and add the results of the function, like map
	// (instruction set 2).
	EMapPush
	// ELISTEND: push a list of the elements gathered since the matching ELISTBEGIN
	// (instruction set 2).
	EListEnd
)

// Built-in function indices
const (
	bLen = iota
	bText
	bFoldl
	bFoldr
	bMap
	bList
	bConcat
)

var builtinNames = []string{"len", "text", "foldl", "foldr", "map", "list", "concat"}

var opNames = map[Op]string{
	OpStr: "STR", OpClass: "CLASS", OpAny: "ANY", OpTop: "TOP", OpFail: "FAIL", OpAssert: "ASSERT",
	OpJump: "JUMP", OpChoice: "CHOICE", OpCommit: "COMMIT", OpCut: "CUT", OpPushPos: "PUSHPOS",
	OpPushNil: "PUSHNIL", OpSeq: "SEQ", OpAtomic: "ATOMIC", OpCapture: "CAPTURE", OpRepeat: "REPEAT",
	OpIter: "ITER", OpNext: "NEXT", OpEndRepeat: "ENDREPEAT", OpLook: "LOOK", OpEndLook: "ENDLOOK",
	OpCall: "CALL", OpPratt: "PRATT", OpPred: "PRED", OpAssign: "ASSIGN", OpLabel: "LABEL",
	OpEndLabel: "ENDLABEL", OpRecover: "RECOVER", OpEndRecover: "ENDRECOVER", OpEndSkip: "ENDSKIP",
	OpReturn: "RETURN", OpEnd: "END", OpScan: "SCAN", OpGuard: "GUARD",
	EInt: "EINT", EStr: "ESTR", EBool: "EBOOL", ENil: "ENIL", ECap: "ECAP", EItem: "EITEM",
	ELocal: "ELOCAL", EVar: "EVAR", EMember: "EMEMBER", ENew: "ENEW", EBin: "EBIN", EUnary: "EUNARY",
	EAnd: "EAND", EOr: "EOR", EBoolChk: "EBOOLCHK", EFunc: "EFUNC", ECall: "ECALL", ERet: "ERET",
	ETextChk: "ETEXTCHK", ETextEq: "ETEXTEQ", EListBegin: "ELISTBEGIN", EListPush: "ELISTPUSH",
	EMapPush: "EMAPPUSH", EListEnd: "ELISTEND",
}

func (o Op) String() string {
	if n, ok := opNames[o]; ok {
		return n
	}
	return fmt.Sprintf("OP%d", o)
}
