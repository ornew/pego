// Package grammar defines the abstract syntax tree (AST) of PEGO grammars.
//
// A grammar is a sequence of statements: type definitions (TypeDef) and
// rule definitions (RuleDef). A rule body is a parser expression (Expr),
// and actions and predicates are value expressions (Term).
package grammar

// Pos is a 1-based position in the source. The zero value means that the
// position is unknown.
type Pos struct {
	Line int
	Col  int
}

// IsValid reports whether the position is known.
func (p Pos) IsValid() bool { return p.Line > 0 }

// Grammar is a whole grammar.
type Grammar struct {
	Package    string      `json:"package,omitempty"`
	Statements []Statement `json:"statements"`

	// PackageBreak holds the comments before the package clause, and
	// EndBreak those at the end of the file (see LineBreak).
	PackageBreak *LineBreak `json:"-"`
	EndBreak     *LineBreak `json:"-"`
}

// Rules returns the rule definitions of the grammar in declaration order.
func (g *Grammar) Rules() []*RuleDef {
	var rs []*RuleDef
	for _, s := range g.Statements {
		if r, ok := s.(*RuleDef); ok {
			rs = append(rs, r)
		}
	}
	return rs
}

// Types returns the type definitions of the grammar in declaration order.
func (g *Grammar) Types() []*TypeDef {
	var ts []*TypeDef
	for _, s := range g.Statements {
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
	Pos  Pos      `json:"-"`
	Name string   `json:"name"`
	Spec TypeSpec `json:"spec"`

	// Break holds the comments before the definition. When it is nil,
	// Format separates the definition from the previous one with a blank
	// line.
	Break *LineBreak `json:"-"`
}

// RuleDef is a rule definition (def Name: Type = Expr -> Action).
type RuleDef struct {
	Pos    Pos      `json:"-"`
	Name   string   `json:"name"`
	Type   TypeExpr `json:"type,omitempty"`
	Expr   Expr     `json:"expr"`
	Action Term     `json:"action,omitempty"`

	// Break holds the comments before the definition. When it is nil,
	// Format separates the definition from the previous one with a blank
	// line.
	Break *LineBreak `json:"-"`
	// BodyBreak, when not nil, puts the body on a new line after "=", and
	// ActionBreak puts "->" on a new line.
	BodyBreak   *LineBreak `json:"-"`
	ActionBreak *LineBreak `json:"-"`
}

func (*TypeDef) statement() {}
func (*RuleDef) statement() {}

// --- Types ---

// TypeSpec is the right-hand side of a type definition.
type TypeSpec interface{ typeSpec() }

// StructSpec is a struct type.
type StructSpec struct {
	Fields []*Field `json:"fields"`

	// OneLine writes the struct on one line ("struct { A T, B U }").
	OneLine bool `json:"-"`
	// CloseBreak holds the comments before the closing brace.
	CloseBreak *LineBreak `json:"-"`
}

// Field is a field of a struct type.
type Field struct {
	Pos  Pos      `json:"-"`
	Name string   `json:"name"`
	Type TypeExpr `json:"type"`

	Break *LineBreak `json:"-"`
}

// AliasSpec is a type alias, including an alias of a union type.
type AliasSpec struct {
	Type TypeExpr `json:"type"`
}

// TerminalSpec is a terminal type.
type TerminalSpec struct{}

func (*StructSpec) typeSpec()   {}
func (*AliasSpec) typeSpec()    {}
func (*TerminalSpec) typeSpec() {}

// TypeExpr is a reference to a type.
type TypeExpr interface{ typeExpr() }

// TypeRef is a reference to a type by name.
type TypeRef struct {
	Pos  Pos    `json:"-"`
	Name string `json:"name"`
}

// ListType is a list type []T.
type ListType struct {
	Elem TypeExpr `json:"elem"`
}

// OptionalType is an optional type *T.
type OptionalType struct {
	Elem TypeExpr `json:"elem"`
}

// UnionType is a union type T | U.
type UnionType struct {
	Types []TypeExpr `json:"types"`
}

func (*TypeRef) typeExpr()      {}
func (*ListType) typeExpr()     {}
func (*OptionalType) typeExpr() {}
func (*UnionType) typeExpr()    {}

// --- Parser expressions ---

// Expr is a parser expression.
type Expr interface{ expr() }

// Ref is a call of a rule. If Level is set, the call parses only with
// that level of the Pratt expression and the levels above it.
type Ref struct {
	Pos   Pos    `json:"-"`
	Name  string `json:"name"`
	Level string `json:"level,omitempty"`
}

// Literal is a string literal "...".
type Literal struct {
	Pos   Pos    `json:"-"`
	Value string `json:"value"`
}

// CharRange is a character range [Lo, Hi].
// Both endpoints must be Unicode scalar values and Lo must not exceed Hi.
type CharRange struct {
	Lo rune `json:"lo"`
	Hi rune `json:"hi"`
}

// CharClass is a character class (?...), or (?^...) if Negated is set.
type CharClass struct {
	Pos     Pos         `json:"-"`
	Ranges  []CharRange `json:"ranges"`
	Negated bool        `json:"negated,omitempty"`
}

// Any matches any single character (.).
type Any struct {
	Pos Pos `json:"-"`
}

// Seq is a sequence a b.
type Seq struct {
	Items []Expr `json:"items"`

	// Breaks[i], when not nil, puts Items[i] on a new line. Format honors
	// it only in the top-level sequences of a rule body.
	Breaks []*LineBreak `json:"-"`
}

// Choice is an ordered choice a / b.
type Choice struct {
	Alts []Expr `json:"alts"`

	// Breaks[i], when not nil, puts Alts[i] (with its "/") on a new line.
	// Format honors it only in the top-level choice of a rule body.
	Breaks []*LineBreak `json:"-"`
}

// Repeat is a repetition a{Min,Max}. A negative Max means no upper
// bound. a* is {0,} and a+ is {1,}.
//
// Pos is the position where the repeated expression begins (its opening
// parenthesis, if it is parenthesized).
type Repeat struct {
	Pos  Pos  `json:"-"`
	Expr Expr `json:"expr"`
	Min  int  `json:"min"`
	Max  int  `json:"max"`
}

// Optional is an optional expression a?. Its value is the value of a, or
// nil. Pos is the position where a begins, as in Repeat.
type Optional struct {
	Pos  Pos  `json:"-"`
	Expr Expr `json:"expr"`
}

// And is a positive lookahead &a.
type And struct {
	Pos  Pos  `json:"-"`
	Expr Expr `json:"expr"`
}

// Not is a negative lookahead !a.
type Not struct {
	Pos  Pos  `json:"-"`
	Expr Expr `json:"expr"`
}

// Atomic is an atomic expression @a.
type Atomic struct {
	Pos  Pos  `json:"-"`
	Expr Expr `json:"expr"`
}

// Discard is a discarded expression -a.
type Discard struct {
	Pos  Pos  `json:"-"`
	Expr Expr `json:"expr"`
}

// Capture is a capture name:a.
type Capture struct {
	Pos  Pos    `json:"-"`
	Name string `json:"name"`
	Expr Expr   `json:"expr"`
}

// Cut is a cut --.
type Cut struct {
	Pos Pos `json:"-"`
}

// Top is the top expression _.
type Top struct {
	Pos Pos `json:"-"`
}

// Bottom is the bottom expression _|_.
type Bottom struct {
	Pos Pos `json:"-"`
}

// BeginInput matches the beginning of the input (^^).
type BeginInput struct {
	Pos Pos `json:"-"`
}

// EndInput matches the end of the input ($$).
type EndInput struct {
	Pos Pos `json:"-"`
}

// BeginLine matches the beginning of a line (^).
type BeginLine struct {
	Pos Pos `json:"-"`
}

// EndLine matches the end of a line ($).
type EndLine struct {
	Pos Pos `json:"-"`
}

// Predicate is a predicate [Term].
type Predicate struct {
	Pos  Pos  `json:"-"`
	Term Term `json:"term"`
}

// Attributed is an expression with attributes a #name(...).
type Attributed struct {
	Expr  Expr         `json:"expr"`
	Attrs []*Attribute `json:"attrs"`

	// Breaks[i], when not nil, puts Attrs[i] on a new line. Format honors
	// it only in the items of the top-level sequences of a rule body.
	Breaks []*LineBreak `json:"-"`
}

// Attribute is an attribute #name(arg=value, ...).
type Attribute struct {
	Pos  Pos        `json:"-"`
	Name string     `json:"name"`
	Args []*AttrArg `json:"args,omitempty"`
}

// AttrArg is an argument of an attribute. Its value is a parser
// expression; a string value is a Literal.
type AttrArg struct {
	Name  string `json:"name"`
	Value Expr   `json:"value"`
}

// Arg returns the argument named name.
func (a *Attribute) Arg(name string) (Expr, bool) {
	for _, arg := range a.Args {
		if arg.Name == name {
			return arg.Value, true
		}
	}
	return nil, false
}

// Pratt is a Pratt expression pratt { ... }. It may appear only at the
// top level of a rule body.
type Pratt struct {
	Pos      Pos             `json:"-"`
	Skip     Expr            `json:"skip,omitempty"`
	Operands []*PrattOperand `json:"operands"`
	Levels   []*PrattLevel   `json:"levels"`

	// SkipBreak holds the comments before the skip line, and CloseBreak
	// those before the closing brace.
	SkipBreak  *LineBreak `json:"-"`
	CloseBreak *LineBreak `json:"-"`
}

// PrattOperand is an operand line.
type PrattOperand struct {
	Expr   Expr `json:"expr"`
	Action Term `json:"action,omitempty"`

	Break *LineBreak `json:"-"`
}

// PrattLevel is a precedence level. Levels declared earlier bind more
// loosely.
type PrattLevel struct {
	Name      string           `json:"name,omitempty"`
	Operators []*PrattOperator `json:"operators"`

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

// PrattOperator is a declaration of an operator.
type PrattOperator struct {
	Pos    Pos    `json:"-"`
	Kind   string `json:"kind"`
	Assoc  string `json:"assoc,omitempty"`
	Expr   Expr   `json:"expr"`
	Action Term   `json:"action,omitempty"`

	Break *LineBreak `json:"-"`
}

func (*Ref) expr()        {}
func (*Literal) expr()    {}
func (*CharClass) expr()  {}
func (*Any) expr()        {}
func (*Seq) expr()        {}
func (*Choice) expr()     {}
func (*Repeat) expr()     {}
func (*Optional) expr()   {}
func (*And) expr()        {}
func (*Not) expr()        {}
func (*Atomic) expr()     {}
func (*Discard) expr()    {}
func (*Capture) expr()    {}
func (*Cut) expr()        {}
func (*Top) expr()        {}
func (*Bottom) expr()     {}
func (*BeginInput) expr() {}
func (*EndInput) expr()   {}
func (*BeginLine) expr()  {}
func (*EndLine) expr()    {}
func (*Predicate) expr()  {}
func (*Attributed) expr() {}
func (*Pratt) expr()      {}

// --- Value expressions (actions and predicates) ---

// Term is a value expression used in actions and predicates.
type Term interface{ term() }

// IntLit is an integer literal.
type IntLit struct {
	Value int `json:"value"`
}

// StringLit is a string literal.
type StringLit struct {
	Value string `json:"value"`
}

// BoolLit is a boolean literal, true or false.
type BoolLit struct {
	Value bool `json:"value"`
}

// NilLit is the empty value nil.
type NilLit struct{}

// CaptureRef is a reference $name to a capture or a lambda parameter.
type CaptureRef struct {
	Pos  Pos    `json:"-"`
	Name string `json:"name"`
}

// IndexRef is a positional reference $n. $0 is the list of all elements.
type IndexRef struct {
	Pos   Pos `json:"-"`
	Index int `json:"index"`
}

// VarRef is a reference name to a variable in a predicate.
type VarRef struct {
	Pos  Pos    `json:"-"`
	Name string `json:"name"`
}

// Member is a field reference x.Name.
type Member struct {
	Pos  Pos    `json:"-"`
	X    Term   `json:"x"`
	Name string `json:"name"`
}

// New creates a struct value: new Type{Field: value, ...}.
type New struct {
	Pos    Pos          `json:"-"`
	Type   string       `json:"type"`
	Fields []*FieldInit `json:"fields,omitempty"`
}

// FieldInit initializes a field of a struct value.
type FieldInit struct {
	Pos   Pos    `json:"-"`
	Name  string `json:"name"`
	Value Term   `json:"value"`
}

// Call is a call of a built-in function f(args...).
type Call struct {
	Pos  Pos    `json:"-"`
	Func string `json:"func"`
	Args []Term `json:"args"`
}

// Lambda is a function (a, b) => body.
type Lambda struct {
	Params []string `json:"params"`
	Body   Term     `json:"body"`
}

// Binary is a binary operation l op r.
type Binary struct {
	Pos Pos    `json:"-"`
	Op  string `json:"op"`
	L   Term   `json:"l"`
	R   Term   `json:"r"`
}

// Unary is a unary operation op x.
type Unary struct {
	Pos Pos    `json:"-"`
	Op  string `json:"op"`
	X   Term   `json:"x"`
}

// Assign defines a variable in a predicate: name = value.
type Assign struct {
	Pos   Pos    `json:"-"`
	Name  string `json:"name"`
	Value Term   `json:"value"`
}

func (*IntLit) term()     {}
func (*StringLit) term()  {}
func (*BoolLit) term()    {}
func (*NilLit) term()     {}
func (*CaptureRef) term() {}
func (*IndexRef) term()   {}
func (*VarRef) term()     {}
func (*Member) term()     {}
func (*New) term()        {}
func (*Call) term()       {}
func (*Lambda) term()     {}
func (*Binary) term()     {}
func (*Unary) term()      {}
func (*Assign) term()     {}
