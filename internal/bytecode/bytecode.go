package bytecode

import (
	"fmt"
)

// Module is a grammar compiled to bytecode. It is the only thing the VM runs.
// The instruction set and its semantics are specified in spec/bytecode/README.md.
type Module struct {
	Strings    []string   // string table
	Classes    []Class    // character class table
	Types      []TypeInfo // type table (struct fields and named node types)
	FieldLists [][]int    // field name lists for new (string table indices)
	Scopes     [][]int    // scopes of repetition elements (lists of capture names)
	Vars       []VarInfo  // variable table
	Rules      []RuleInfo // rule table
	// Starts holds the rule table indices of the start rules, the rules where parsing may begin,
	// in declaration order, and DefaultStart that of the default one.
	Starts         []int
	DefaultStart   int
	OperatorsRules []OperatorsRuleInfo
	Code           []Instr // matching code
	Exprs          []Instr // expression code (actions and predicates)
	// SourceInfo reports that the values record source information (Options.SourceInfo).
	SourceInfo bool
	// DefaultValue reports that value actions were dropped (Options.DefaultValue).
	DefaultValue bool
}

// RuleStreams reports whether the rule body that starts at entry passes values to the stream:
// whether an instruction that passes a value to the stream (Instr.StreamsValue) comes
// before its RETURN.
func (m *Module) RuleStreams(entry int) bool {
	for _, in := range m.Code[entry:] {
		switch {
		case in.Op == OpReturn:
			return false
		case in.StreamsValue():
			return true
		}
	}
	return false
}

// StreamsValue reports whether in is a NEXT that passes the value of the iteration to the stream
// (f = 2).
func (in Instr) StreamsValue() bool { return in.Op == OpNext && in.B == 2 }

// InstructionSetVersion is the instruction set of every module: the value model's.
func (m *Module) InstructionSetVersion() int { return ISAVersion }

// Class is a character class. Ranges is a list of lower and upper bound pairs.
type Class struct {
	Ranges  []rune
	Negated bool
	Desc    int // expectation string (string table index)
}

// Has reports whether the character ch is in the class.
func (cl *Class) Has(ch rune) bool {
	for k := 0; k < len(cl.Ranges); k += 2 {
		if cl.Ranges[k] <= ch && ch <= cl.Ranges[k+1] {
			return !cl.Negated
		}
	}
	return cl.Negated
}

// TypeInfo describes a type: a struct type, or a named node type.
type TypeInfo struct {
	Name   int   // string table index
	Fields []int // struct field names (string table indices)
	// Node is, for a named node type, the underlying node type (string table index of its text,
	// as ECAST writes types), and -1 for a struct type (instruction set 16).
	Node int
}

// Variable types
const (
	VarInt = iota
	VarString
	VarBool
)

// VarInfo describes a variable. Every parse starts with the variable bound to its initial value.
type VarInfo struct {
	Name int // string table index
	Type int // varInt, varString or varBool
	// Int is the initial value of an int variable, and 0 or 1 for a bool variable.
	Int int
	// Str is the string table index of the initial value of a string variable, and -1 for
	// the other types.
	Str int
}

// RuleInfo describes a rule.
type RuleInfo struct {
	Name   int // string table index
	Entry  int // start of the body code (for a OperatorsRule expression rule, a OPERATORS_RULE instruction followed by RETURN)
	Scope  []int
	Action int // start of the action's expression code (-1 if none)
	// NodeType is the named node type the rule's value is converted to (string table index, -1 if
	// none).
	NodeType      int
	Memo          bool
	Leader        bool
	Positional    bool
	OperatorsRule int // OperatorsRule table index (-1 if none)
	// NoValue reports that this is a value-free twin, called from places where the value is
	// discarded (its value is always nil). It has the same name as the original rule and is placed
	// after all the original rules in the rule table.
	NoValue bool
	// Transient reports that the rule is not memoized in ordinary parses (even if Memo is set). In
	// incremental parsing, Memo applies.
	Transient bool
	// Rec reports that the rule is on a cycle of calls (or calls itself): its entries count
	// against the nesting limit, those of other rules do not.
	Rec bool
	// Where is the text that names the rule in the message of a failed action (string table index),
	// or -1 for the name of the rule. The hidden rule of an inline action names the rule that
	// holds it.
	Where int
	// Splice reports that the rule makes no node of its own: the elements of its value are the
	// elements of the sequence that calls it (a hidden rule without an action).
	Splice bool
	// Vars are the variables the rule or its callees read (string table indices, sorted by name).
	// The rule's memo entries are keyed by their values at the call.
	Vars []int
}

// OperatorsRuleInfo is an Operators Rule expression table.
type OperatorsRuleInfo struct {
	Skip     int // start of the skip code (-1 if none)
	Operands []LineInfo
	Prefix   []OpInfo
	Led      []OpInfo // infix and postfix
}

// LineInfo is an Operators Rule operand line or operator line. Its code ends with END.
type LineInfo struct {
	Entry  int
	Scope  []int
	Action int // start of the expression code (-1 if none)
	IsSeq  bool
}

// OpInfo is an Operators Rule operator.
type OpInfo struct {
	ID    int
	Kind  int // opPrefix, opPostfix, opInfix
	Assoc int // assocLeft, assocRight, assocNone
	Level int
	Line  LineInfo
}

// Operators Rule operator kinds and associativity
const (
	OperatorPrefix = iota
	OperatorPostfix
	OperatorInfix
)

const (
	AssocLeft = iota
	AssocRight
	AssocNone
)

// Instr is an instruction. The meaning of its operands depends on the instruction. Jump targets
// are instruction indices.
type Instr struct {
	Op      Op
	A, B, C int32
}

// Integer decodes the signed integer held by an EINT instruction.
func (in Instr) Integer() int64 {
	return int64(in.A) + int64(in.B)<<32
}

// Op is an opcode.
type Op uint8

// Matching instructions
const (
	// STR s, d, b: match string s of the string table. On failure, record string d as the
	// expectation. If b is 1, push a string node.
	OpStr Op = iota + 1
	// CLASS c, d, b: match one character of character class c of the character class table.
	OpClass
	// ANY b: match any one character.
	OpAny
	// TOP b: empty match. If b is 1, push an empty string node.
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
	// PUSHPOS: push the current position (start of a tuple, a list, @).
	OpPushPos
	// PUSHNIL: push nil.
	OpPushNil
	// SEQ n: pop n values and the position below them, and push a tuple node.
	OpSeq
	// ATOMIC b: pop a position and push the string node from there to the current position (nothing is
	// pushed if b is 0).
	OpAtomic
	// CAPTURE s, b: write the top value to slot s of the current frame. If b is 1, pop the value
	// (if 0, leave it).
	OpCapture
	// REPEAT min, max, sc: start a repetition. max -1 means unbounded. sc is the index of the element
	// scope (-1 if none). When building values, the start position is pushed with PUSHPOS just
	// before.
	OpRepeat
	// ITER l: go to l if the repetition has reached its finite maximum; otherwise push an entry
	// for one iteration. On failure, restore the state and go to l.
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
	// OPERATORS_RULE p: parse the expression of OperatorsRule table p and push the value (used in rule bodies).
	OpOperatorsRule
	// PRED e: evaluate expression code e and fail if it is false or an error.
	OpPred
	// BIND name, e: define the variable name with the value of expression code e.
	OpBind
	// LABEL m, d: start @error (d=0) or @diagnostic (d=1).
	OpLabel
	// ENDLABEL: end @error.
	OpEndLabel
	// RECOVER l: start #recover. On failure, prepare for recovery and go to l (the skip code).
	OpRecover
	// ENDRECOVER l: the #recover expression succeeded. Go to l.
	OpEndRecover
	// ENDSKIP b: end skipping, record the error, and push an Error (nothing is pushed if b is 0).
	OpEndSkip
	// RETURN: end of a rule body.
	OpReturn
	// END: end of the code of an Operators Rule section.
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
	// REPEATW min, max, sc: REPEAT with bounds stored in EINT instructions of Exprs.
	OpRepeatWide
	// SCANW c, min, max: SCAN with bounds stored in EINT instructions of Exprs.
	OpScanWide
	// SPREAD n, t: pop the value a splice rule returned and push its n elements: the children of the
	// tuple node it returned if t is 1, else its one element (n is 1) or none (n is 0).
	OpSpread
	// STRF s, d, b: match the ASCII-folded string s of the string table, comparing each input
	// character folded the same way (A-Z as a-z). B is the expectation (the literal with its
	// suffix i), and if b is 1, push a string node with the consumed input as its text.
	OpStrFold
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
	// eRetired124 was ETERMINAL, which built a terminal of a declared type (ISA 5 to 13).
	eRetired124
	// eRetired125 was ECHECK, which checked a constructed field value against its declared type
	// (ISA 9 to 23).
	eRetired125
	// ECAST t: fail unless the top value fits type t, the type of a cast x.(T); the value stays
	// (ISA 12).
	ECast
	// ENODE t: pop a node and push it converted to the named node type t (B is 0).
	ENode
	// ETUPLE n: pop n values and push a tuple value of them, in order.
	ETuple
)

// Built-in function indices
const (
	BuiltinLen = iota
	BuiltinText
	BuiltinFoldl
	BuiltinFoldr
	BuiltinMap
	BuiltinList
	BuiltinConcat
	BuiltinFilter
)

var BuiltinNames = []string{"len", "text", "foldl", "foldr", "map", "list", "concat", "filter"}

var opNames = map[Op]string{
	OpStr: "STR", OpClass: "CLASS", OpAny: "ANY", OpTop: "TOP", OpFail: "FAIL", OpAssert: "ASSERT",
	OpJump: "JUMP", OpChoice: "CHOICE", OpCommit: "COMMIT", OpCut: "CUT", OpPushPos: "PUSHPOS",
	OpPushNil: "PUSHNIL", OpSeq: "SEQ", OpSpread: "SPREAD", OpAtomic: "ATOMIC", OpCapture: "CAPTURE", OpRepeat: "REPEAT",
	OpIter: "ITER", OpNext: "NEXT", OpEndRepeat: "ENDREPEAT", OpLook: "LOOK", OpEndLook: "ENDLOOK",
	OpCall: "CALL", OpOperatorsRule: "OPERATORS_RULE", OpPred: "PRED", OpBind: "BIND", OpLabel: "LABEL",
	OpEndLabel: "ENDLABEL", OpRecover: "RECOVER", OpEndRecover: "ENDRECOVER", OpEndSkip: "ENDSKIP",
	OpReturn: "RETURN", OpEnd: "END", OpScan: "SCAN", OpGuard: "GUARD",
	OpRepeatWide: "REPEATW", OpScanWide: "SCANW", OpStrFold: "STRF",
	EInt: "EINT", EStr: "ESTR", EBool: "EBOOL", ENil: "ENIL", ECap: "ECAP", EItem: "EITEM",
	ELocal: "ELOCAL", EVar: "EVAR", EMember: "EMEMBER", ENew: "ENEW", EBin: "EBIN", EUnary: "EUNARY",
	EAnd: "EAND", EOr: "EOR", EBoolChk: "EBOOLCHK", EFunc: "EFUNC", ECall: "ECALL", ERet: "ERET",
	ETextChk: "ETEXTCHK", ETextEq: "ETEXTEQ", EListBegin: "ELISTBEGIN", EListPush: "ELISTPUSH",
	EMapPush: "EMAPPUSH", EListEnd: "ELISTEND",
	ECast: "ECAST", ENode: "ENODE", ETuple: "ETUPLE",
}

func (o Op) String() string {
	if n, ok := opNames[o]; ok {
		return n
	}
	return fmt.Sprintf("OP%d", o)
}

// ISAVersion is the instruction set of the value model, the only one supported. Earlier
// versions built the previous syntax trees, so their files are not loaded and need to be
// compiled again.
const ISAVersion = 28
