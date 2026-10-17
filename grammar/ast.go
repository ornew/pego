// Package grammar defines the abstract syntax tree (AST) of PEGO grammars.
//
// A grammar is a sequence of statements: type definitions (TypeDef) and
// rule definitions (RuleDef). A rule body is a parser expression (Expr),
// and actions and predicates are value expressions (Term).
package grammar

import (
	"cmp"
	"slices"
	"sort"
)

// Pos is a 1-based position in the source. The zero value means that the
// position is unknown.
type Pos struct {
	Line int `json:"line"`
	Col  int `json:"col"`
}

// IsValid reports whether the position is known.
func (p Pos) IsValid() bool { return p.Line > 0 }

// Grammar is a whole grammar.
type Grammar struct {
	SourceInfo `json:"source,omitempty"`
	Package    string      `json:"package,omitempty"`
	Statements []Statement `json:"statements"`

	// PackageBreak holds the comments before the package clause, and
	// EndBreak those at the end of the file (see LineBreak).
	PackageSource SourceInfo `json:"packageSource,omitempty"`
	PackageBreak  *LineBreak `json:"-"`
	EndBreak      *LineBreak `json:"-"`
	// HeaderComments records source comments inside statement headers.
	// Parsing accepts them as trivia; compilation rejects their placement.
	HeaderComments []*Comment `json:"-"`
}

// Rules returns the rule definitions of the grammar in declaration order.
func (g *Grammar) Rules() []*RuleDef {
	return g.rules(nil)
}

func (g *Grammar) rules(control *metadataControl) []*RuleDef {
	var rs []*RuleDef
	for _, s := range g.Statements {
		control.poll()
		if r, ok := s.(*RuleDef); ok {
			rs = append(rs, r)
		}
	}
	return rs
}

// Types returns the type definitions of the grammar in declaration order.
func (g *Grammar) Types() []*TypeDef {
	return g.types(nil)
}

func (g *Grammar) types(control *metadataControl) []*TypeDef {
	var ts []*TypeDef
	for _, s := range g.Statements {
		control.poll()
		if t, ok := s.(*TypeDef); ok {
			ts = append(ts, t)
		}
	}
	return ts
}

// Statement is a top-level definition in a grammar.
type Statement interface{ statement() }

// TypeDef is a type definition (type Name Spec).
type TypeDef struct {
	SourceInfo `json:"source,omitempty"`
	Name       string   `json:"name"`
	Spec       TypeSpec `json:"spec"`

	// Break holds the comments before the definition. When it is nil,
	// Format separates the definition from the previous one with a blank
	// line.
	Break *LineBreak `json:"-"`
}

// RuleDef is a rule definition (rule Name Type = Expr => Action).
type RuleDef struct {
	SourceInfo `json:"source,omitempty"`
	Name       string   `json:"name"`
	Type       TypeExpr `json:"type,omitempty"`
	Expr       Expr     `json:"expr"`
	Action     Term     `json:"action,omitempty"`
	// DefaultAction preserves an explicit => ...; it has the same value
	// semantics as an omitted action.
	DefaultAction bool `json:"defaultAction,omitempty"`

	// Break holds the comments before the definition. When it is nil,
	// Format separates the definition from the previous one with a blank
	// line.
	Break *LineBreak `json:"-"`
	// BodyBreak, when not nil, puts the body on a new line after "=", and
	// ActionBreak puts "=>" on a new line.
	BodyBreak   *LineBreak `json:"-"`
	ActionBreak *LineBreak `json:"-"`
	CloseBreak  *LineBreak `json:"-"`
}

func (*TypeDef) statement() {}
func (*RuleDef) statement() {}

// --- Types ---

// TypeSpec is the right-hand side of a type definition.
type TypeSpec interface{ typeSpec() }

// StructSpec is a struct type.
type StructSpec struct {
	SourceInfo `json:"source,omitempty"`
	Fields     []*Field `json:"fields"`

	// OneLine writes the struct on one line ("= { A T; B U }").
	OneLine bool `json:"-"`
	// CloseBreak holds the comments before the closing brace.
	CloseBreak *LineBreak `json:"-"`
}

// Field is a field of a struct type.
type Field struct {
	SourceInfo `json:"source,omitempty"`
	Name       string   `json:"name"`
	Type       TypeExpr `json:"type"`

	Break *LineBreak `json:"-"`
}

// AliasSpec is a type alias (type Name ~ Type), including an alias of a union type.
type AliasSpec struct {
	SourceInfo `json:"source,omitempty"`
	Type       TypeExpr `json:"type"`
}

// TerminalSpec is a terminal type.
type TerminalSpec struct {
	SourceInfo `json:"source,omitempty"`
}

func (*StructSpec) typeSpec()   {}
func (*AliasSpec) typeSpec()    {}
func (*TerminalSpec) typeSpec() {}

// TypeExpr is a reference to a type.
type TypeExpr interface{ typeExpr() }

// TypeRef is a reference to a type by name. The top type is named _ and the bottom type _|_.
type TypeRef struct {
	SourceInfo `json:"source,omitempty"`
	Name       string `json:"name"`
}

// ListType is a list type []T.
type ListType struct {
	SourceInfo `json:"source,omitempty"`
	Elem       TypeExpr `json:"elem"`
}

// OptionalType is an optional type *T.
type OptionalType struct {
	SourceInfo `json:"source,omitempty"`
	Elem       TypeExpr `json:"elem"`
}

// UnionType is a union type T | U.
type UnionType struct {
	SourceInfo `json:"source,omitempty"`
	Types      []TypeExpr `json:"types"`
}

// IntersectionType is the meet T & U of types: the type of the values in all of them.
type IntersectionType struct {
	SourceInfo `json:"source,omitempty"`
	Types      []TypeExpr `json:"types"`
}

// TupleType is a tuple type: () is the unit, (T,) has one element and (T, U) two. Its
// parentheses keep their source layout like a TypeGroup's.
type TupleType struct {
	SourceInfo `json:"source,omitempty"`
	Types      []TypeExpr `json:"types"`
	OpenBreak  *LineBreak `json:"-"`
	BodyBreak  *LineBreak `json:"-"`
	CloseBreak *LineBreak `json:"-"`
	OneLine    bool       `json:"-"`
}

// TypeApp applies a type to type arguments: Name[Args...], as in node[string].
type TypeApp struct {
	SourceInfo `json:"source,omitempty"`
	Name       string     `json:"name"`
	Args       []TypeExpr `json:"args"`
}

// StructType is an unnamed struct type written in a type expression: { A T; B U }. Its braces
// keep their source layout like a struct definition's.
type StructType struct {
	SourceInfo `json:"source,omitempty"`
	Fields     []*Field `json:"fields"`

	OneLine    bool       `json:"-"`
	CloseBreak *LineBreak `json:"-"`
}

// TypeGroup preserves parentheses and source layout in a type expression.
// It is transparent to type resolution.
type TypeGroup struct {
	SourceInfo `json:"source,omitempty"`
	Type       TypeExpr   `json:"type"`
	OpenBreak  *LineBreak `json:"-"`
	BodyBreak  *LineBreak `json:"-"`
	CloseBreak *LineBreak `json:"-"`
	OneLine    bool       `json:"-"`
}

func (*TypeRef) typeExpr()          {}
func (*ListType) typeExpr()         {}
func (*OptionalType) typeExpr()     {}
func (*UnionType) typeExpr()        {}
func (*IntersectionType) typeExpr() {}
func (*TupleType) typeExpr()        {}
func (*TypeApp) typeExpr()          {}
func (*StructType) typeExpr()       {}
func (*TypeGroup) typeExpr()        {}

// --- Parser expressions ---

// Expr is a parser expression.
type Expr interface{ expr() }

// Ref is a call of a rule. If Level is set, the call parses only with
// that level of the Operators Rule and the levels above it.
type Ref struct {
	SourceInfo `json:"source,omitempty"`
	Name       string `json:"name"`
	Level      string `json:"level,omitempty"`
}

// Literal is a string literal "...". Matching uses its decoded rune sequence:
// malformed UTF-8 bytes become U+FFFD in both position units. Compilation and
// generation derive matching data without changing Value.
type Literal struct {
	SourceInfo `json:"source,omitempty"`
	Value      string `json:"value"`
}

// CharRange is a character range [Lo, Hi].
// Both endpoints must be Unicode scalar values and Lo must not exceed Hi.
type CharRange struct {
	Lo rune `json:"lo"`
	Hi rune `json:"hi"`
}

// CharClass is a character class [...], or [^...] if Negated is set.
type CharClass struct {
	SourceInfo `json:"source,omitempty"`
	Ranges     []CharRange `json:"ranges"`
	Negated    bool        `json:"negated,omitempty"`
}

// Any matches any single character (.).
type Any struct {
	SourceInfo `json:"source,omitempty"`
}

// Seq is a sequence a b.
type Seq struct {
	SourceInfo `json:"source,omitempty"`
	Items      []Expr `json:"items"`

	// Breaks[i], when not nil, puts Items[i] on a new line. Format honors
	// it only in the top-level sequences of a rule body.
	Breaks []*LineBreak `json:"-"`
}

// Choice is an ordered choice a / b.
type Choice struct {
	SourceInfo `json:"source,omitempty"`
	Alts       []Expr `json:"alts"`

	// Breaks[i], when not nil, puts Alts[i] (with its "/") on a new line.
	// Format honors it only in the top-level choice of a rule body.
	Breaks []*LineBreak `json:"-"`
}

// Group preserves explicit parentheses and their source layout. It is
// transparent to matching and value construction.
type Group struct {
	SourceInfo `json:"source,omitempty"`
	Expr       Expr       `json:"expr"`
	BodyBreak  *LineBreak `json:"-"`
	CloseBreak *LineBreak `json:"-"`
	OneLine    bool       `json:"-"`
}

// InlineAction maps an expression's value inside a parenthesized grammar
// expression. Parsing and formatting are supported; compilation is not yet
// implemented. DefaultAction denotes an explicit => ... instead of Action.
type InlineAction struct {
	SourceInfo    `json:"source,omitempty"`
	Expr          Expr `json:"expr"`
	Action        Term `json:"action,omitempty"`
	DefaultAction bool `json:"defaultAction,omitempty"`
}

// Repeat is a repetition a #repeat(Min,Max). A negative Max means no upper
// bound. a* is #repeat(0,) and a+ is #repeat(1,).
//
// Pos is the position where the repeated expression begins (its opening
// parenthesis, if it is parenthesized).
type Repeat struct {
	SourceInfo  `json:"source,omitempty"`
	Expr        Expr       `json:"expr"`
	Min         int        `json:"min"`
	Max         int        `json:"max"`
	PragmaBreak *LineBreak `json:"-"`
}

// Optional is an optional expression a?. Its value is the value of a, or
// nil. Pos is the position where a begins, as in Repeat.
type Optional struct {
	SourceInfo `json:"source,omitempty"`
	Expr       Expr `json:"expr"`
}

// And is a positive lookahead &a.
type And struct {
	SourceInfo `json:"source,omitempty"`
	Expr       Expr `json:"expr"`
}

// Not is a negative lookahead !a.
type Not struct {
	SourceInfo `json:"source,omitempty"`
	Expr       Expr `json:"expr"`
}

// Recovery runs Skip when Expr fails, preserving the existing recovery semantics.
type Recovery struct {
	SourceInfo `json:"source,omitempty"`
	Expr       Expr       `json:"expr"`
	Skip       Expr       `json:"skip"`
	Break      *LineBreak `json:"-"`
}

// Stream hands repetition elements to the streaming parser's caller.
type Stream struct {
	SourceInfo `json:"source,omitempty"`
	Expr       Expr       `json:"expr"`
	Break      *LineBreak `json:"-"`
}

// Atomic is an atomic expression a #atomic.
type Atomic struct {
	SourceInfo `json:"source,omitempty"`
	Expr       Expr       `json:"expr"`
	Break      *LineBreak `json:"-"`
}

// Discard is a discarded expression -a.
type Discard struct {
	SourceInfo `json:"source,omitempty"`
	Expr       Expr `json:"expr"`
}

// Capture is a capture name:a.
type Capture struct {
	SourceInfo `json:"source,omitempty"`
	Name       string `json:"name"`
	Expr       Expr   `json:"expr"`
}

// Cut is a cut --.
type Cut struct {
	SourceInfo `json:"source,omitempty"`
}

// Top is the top expression _.
type Top struct {
	SourceInfo `json:"source,omitempty"`
}

// Bottom is the bottom expression _|_.
type Bottom struct {
	SourceInfo `json:"source,omitempty"`
}

// BeginInput matches the beginning of the input (^^).
type BeginInput struct {
	SourceInfo `json:"source,omitempty"`
}

// EndInput matches the end of the input ($$).
type EndInput struct {
	SourceInfo `json:"source,omitempty"`
}

// BeginLine matches the beginning of a line (^).
type BeginLine struct {
	SourceInfo `json:"source,omitempty"`
}

// EndLine matches the end of a line ($).
type EndLine struct {
	SourceInfo `json:"source,omitempty"`
}

// Predicate is a predicate action { Term }. A nonempty Block lowers to adjacent
// predicate actions in a sequence, or their grouped sequence in a single
// expression position, without a new variable or capture scope.
type Predicate struct {
	SourceInfo `json:"source,omitempty"`
	Term       Term         `json:"term"`
	Breaks     []*LineBreak `json:"-"`
	CloseBreak *LineBreak   `json:"-"`
	OneLine    bool         `json:"-"`
}

// Attributed is an expression with postfix @ attributes and # pragmas.
type Attributed struct {
	SourceInfo `json:"source,omitempty"`
	Expr       Expr         `json:"expr"`
	Attrs      []*Attribute `json:"attrs"`

	// Breaks[i], when not nil, puts Attrs[i] on a new line. Format honors
	// it only in the items of the top-level sequences of a rule body.
	Breaks []*LineBreak `json:"-"`
}

// Attribute is a postfix attribute @name(positional, keyword=value, ...).
// Argument values are int, string, bool, nil, or parser expressions. Nil in
// Args preserves an omitted positional slot, including a trailing slot.
type Attribute struct {
	SourceInfo `json:"source,omitempty"`
	Name       string `json:"name"`
	// Pragma distinguishes behavioral # directives from @ attributes.
	Pragma bool           `json:"pragma,omitempty"`
	Args   []any          `json:"args,omitempty"`
	KwArgs map[string]any `json:"kwArgs,omitempty"`
	// KeywordOrder preserves source order; JSON grammars format keys sorted.
	KeywordOrder []string `json:"-"`
}

// Arg returns the argument named name.
func (a *Attribute) Arg(name string) (Expr, bool) {
	v, ok := a.KwArgs[name]
	if !ok {
		return nil, false
	}
	if s, ok := v.(string); ok {
		return &Literal{Value: s}, true
	}
	e, ok := v.(Expr)
	return e, ok
}

// ExpressionArgs returns the parser-expression arguments. Scalar and omitted
// positional arguments are not parser expressions.
func (a *Attribute) ExpressionArgs() []Expr {
	var result []Expr
	for _, value := range a.Args {
		if e, ok := value.(Expr); ok {
			result = append(result, e)
		}
	}
	keys := make([]string, 0, len(a.KwArgs))
	for key := range a.KwArgs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if e, ok := a.KwArgs[key].(Expr); ok {
			result = append(result, e)
		}
	}
	return result
}

func (a *Attribute) expressionArgs(control *metadataControl) []Expr {
	if control == nil {
		return a.ExpressionArgs()
	}
	var result []Expr
	for _, value := range a.Args {
		control.poll()
		if e, ok := value.(Expr); ok {
			result = append(result, e)
		}
	}
	keys := make([]string, 0, len(a.KwArgs))
	for key := range a.KwArgs {
		control.poll()
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b string) int {
		control.poll()
		return cmp.Compare(a, b)
	})
	for _, key := range keys {
		control.poll()
		if e, ok := a.KwArgs[key].(Expr); ok {
			result = append(result, e)
		}
	}
	return result
}

// OperatorsRule is the body of an operators rule declaration. It may appear only at the
// top level of a rule body.
type OperatorsRule struct {
	SourceInfo `json:"source,omitempty"`
	Skip       Expr                    `json:"skip,omitempty"`
	SkipSource SourceInfo              `json:"skipSource,omitempty"`
	Operands   []*OperatorsRuleOperand `json:"operands"`
	Levels     []*OperatorsRuleLevel   `json:"levels"`

	// SkipBreak holds the comments before the skip line, and CloseBreak
	// those before the closing brace.
	SkipBreak  *LineBreak `json:"-"`
	CloseBreak *LineBreak `json:"-"`
}

// OperatorsRuleOperand is an operand line.
type OperatorsRuleOperand struct {
	SourceInfo    `json:"source,omitempty"`
	Expr          Expr `json:"expr"`
	Action        Term `json:"action,omitempty"`
	DefaultAction bool `json:"defaultAction,omitempty"`

	Break *LineBreak `json:"-"`
}

// OperatorsRuleLevel is a precedence level. Levels declared earlier bind more
// loosely.
type OperatorsRuleLevel struct {
	SourceInfo `json:"source,omitempty"`
	Name       string                   `json:"name,omitempty"`
	Operators  []*OperatorsRuleOperator `json:"operators"`

	Break *LineBreak `json:"-"`
	// OneLine writes the level on one line ("level { infix left "+" }").
	OneLine bool `json:"-"`
	// CloseBreak holds the comments before the closing brace.
	CloseBreak *LineBreak `json:"-"`
}

// Kinds of operators.
const (
	Prefix  = "prefix"
	Postfix = "postfix"
	Infix   = "infix"
)

// Associativities.
const (
	AssocLeft  = "left"
	AssocRight = "right"
	AssocNone  = "none"
)

// OperatorsRuleOperator is a declaration of an operator.
type OperatorsRuleOperator struct {
	SourceInfo    `json:"source,omitempty"`
	Kind          string `json:"kind"`
	Assoc         string `json:"assoc,omitempty"`
	Expr          Expr   `json:"expr"`
	Action        Term   `json:"action,omitempty"`
	DefaultAction bool   `json:"defaultAction,omitempty"`

	Break *LineBreak `json:"-"`
}

func (*Ref) expr()           {}
func (*Literal) expr()       {}
func (*CharClass) expr()     {}
func (*Any) expr()           {}
func (*Seq) expr()           {}
func (*Choice) expr()        {}
func (*Group) expr()         {}
func (*InlineAction) expr()  {}
func (*Repeat) expr()        {}
func (*Optional) expr()      {}
func (*And) expr()           {}
func (*Not) expr()           {}
func (*Recovery) expr()      {}
func (*Stream) expr()        {}
func (*Atomic) expr()        {}
func (*Discard) expr()       {}
func (*Capture) expr()       {}
func (*Cut) expr()           {}
func (*Top) expr()           {}
func (*Bottom) expr()        {}
func (*BeginInput) expr()    {}
func (*EndInput) expr()      {}
func (*BeginLine) expr()     {}
func (*EndLine) expr()       {}
func (*Predicate) expr()     {}
func (*Attributed) expr()    {}
func (*OperatorsRule) expr() {}

// --- Value expressions (actions and predicates) ---

// Term is a value expression used in actions and predicates.
type Term interface{ term() }

// TermGroup preserves parentheses and source layout in an action expression.
// It is transparent to value construction and predicate evaluation.
type TermGroup struct {
	SourceInfo `json:"source,omitempty"`
	Term       Term       `json:"term"`
	OpenBreak  *LineBreak `json:"-"`
	BodyBreak  *LineBreak `json:"-"`
	CloseBreak *LineBreak `json:"-"`
	OneLine    bool       `json:"-"`
}

func (*TermGroup) term() {}

// IntLit is an integer literal.
type IntLit struct {
	SourceInfo `json:"source,omitempty"`
	Value      int `json:"value"`
}

// StringLit is a string literal.
type StringLit struct {
	SourceInfo `json:"source,omitempty"`
	Value      string `json:"value"`
}

// BoolLit is a boolean literal, true or false.
type BoolLit struct {
	SourceInfo `json:"source,omitempty"`
	Value      bool `json:"value"`
}

// NilLit is the empty value nil.
type NilLit struct {
	SourceInfo `json:"source,omitempty"`
}

// CaptureRef is a reference $name to a capture, or name to a lambda parameter.
type CaptureRef struct {
	SourceInfo `json:"source,omitempty"`
	Name       string `json:"name"`
	// Param marks a reference to a lambda parameter, which is written without $.
	Param bool `json:"param,omitempty"`
	// Pragma distinguishes behavioral # directives from @ attributes.
	Pragma bool `json:"pragma,omitempty"`
}

// IndexRef is a positional reference $n. $0 is the list of all elements.
type IndexRef struct {
	SourceInfo `json:"source,omitempty"`
	Index      int `json:"index"`
}

// VarRef is a reference name to a variable in a predicate.
type VarRef struct {
	SourceInfo `json:"source,omitempty"`
	Name       string `json:"name"`
}

// Member is a field reference x.Name, or, when Name is "$" and a capture name, the capture
// access x.$y, which reads capture y of the node x.
type Member struct {
	SourceInfo `json:"source,omitempty"`
	X          Term   `json:"x"`
	Name       string `json:"name"`
}

// New creates a struct value: Type{Field: value, ...}.
type New struct {
	SourceInfo `json:"source,omitempty"`
	Type       string       `json:"type"`
	Fields     []*FieldInit `json:"fields,omitempty"`
}

// ListConstructor builds a node list with an explicit element type: []T{...}.
type ListConstructor struct {
	SourceInfo `json:"source,omitempty"`
	Elem       TypeExpr `json:"elem"`
	Items      []Term   `json:"items"`
}

// TerminalConstructor builds a declared terminal from a positional string:
// T{"text"}. Compilation requires exactly one string-valued argument.
type TerminalConstructor struct {
	SourceInfo `json:"source,omitempty"`
	Type       string `json:"type"`
	Args       []Term `json:"args"`
}

// FieldInit initializes a field of a struct value.
type FieldInit struct {
	SourceInfo `json:"source,omitempty"`
	Name       string `json:"name"`
	Value      Term   `json:"value"`
}

// Call is a call of a built-in function f(args...).
type Call struct {
	SourceInfo `json:"source,omitempty"`
	Func       string `json:"func"`
	Args       []Term `json:"args"`
}

// Lambda is a function (a, b) => body.
type Lambda struct {
	SourceInfo `json:"source,omitempty"`
	Params     []string `json:"params"`
	Body       Term     `json:"body"`
}

// Binary is a binary operation l op r. The operator "[]" is indexing, l[r].
type Binary struct {
	SourceInfo `json:"source,omitempty"`
	Op         string `json:"op"`
	L          Term   `json:"l"`
	R          Term   `json:"r"`
}

// Unary is a unary operation op x.
type Unary struct {
	SourceInfo `json:"source,omitempty"`
	Op         string `json:"op"`
	X          Term   `json:"x"`
}

// Cast is a type assertion x.(Type): its value is x, which must be a Type.
type Cast struct {
	SourceInfo `json:"source,omitempty"`
	X          Term     `json:"x"`
	Type       TypeExpr `json:"type"`
}

// Assign defines a variable in a predicate action: name := value.
type Assign struct {
	SourceInfo `json:"source,omitempty"`
	Name       string `json:"name"`
	Value      Term   `json:"value"`
}

// Block is a braced action statement list. Nonempty predicate blocks lower to
// adjacent predicate actions. Value-action and empty predicate blocks remain
// unsupported by compilation.
// Single-statement predicate actions use Predicate.Term directly.
type Block struct {
	SourceInfo `json:"source,omitempty"`
	Statements []Term       `json:"statements"`
	Breaks     []*LineBreak `json:"-"`
	CloseBreak *LineBreak   `json:"-"`
	OneLine    bool         `json:"-"`
}

func (*Block) term() {}

func (*IntLit) term()              {}
func (*StringLit) term()           {}
func (*BoolLit) term()             {}
func (*NilLit) term()              {}
func (*CaptureRef) term()          {}
func (*IndexRef) term()            {}
func (*VarRef) term()              {}
func (*Member) term()              {}
func (*New) term()                 {}
func (*ListConstructor) term()     {}
func (*TerminalConstructor) term() {}
func (*Call) term()                {}
func (*Lambda) term()              {}
func (*Binary) term()              {}
func (*Unary) term()               {}
func (*Cast) term()                {}
func (*Assign) term()              {}
