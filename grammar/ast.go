// Package grammar defines the abstract syntax tree (AST) of PEGO grammars.
//
// A grammar file (File) is a package clause and a list of declarations (Decl):
// type definitions (TypeDef), type aliases (TypeAlias), variable definitions
// (VarDef) and rule definitions (RuleDef). A rule body is a parser expression (Expr),
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

// Grammar is a whole grammar. It is made of one file.
type Grammar struct {
	File File `json:"file"`
}

// File is a grammar file: a package clause and a list of declarations.
type File struct {
	SourceInfo `json:"source,omitempty"`
	Pkg        Pkg    `json:"pkg,omitempty"`
	Decls      []Decl `json:"decls"`

	// EndBreak holds the comments at the end of the file (see LineBreak).
	EndBreak *LineBreak `json:"-"`
	// HeaderComments records source comments inside declaration headers.
	// Parsing accepts them as trivia; compilation rejects their placement.
	HeaderComments []*Comment `json:"-"`
}

// Pkg is the package clause (package Name). A file without one has the zero Pkg.
type Pkg struct {
	SourceInfo `json:"source,omitempty"`
	Name       string `json:"name,omitempty"`

	// Break holds the comments before the package clause (see LineBreak).
	Break *LineBreak `json:"-"`
}

// Rules returns the rule declarations of the grammar in declaration order.
func (g *Grammar) Rules() []*RuleDef {
	return g.rules(nil)
}

func (g *Grammar) rules(control *metadataControl) []*RuleDef {
	var rs []*RuleDef
	for _, d := range g.File.Decls {
		control.poll()
		if r, ok := d.(*RuleDef); ok {
			rs = append(rs, r)
		}
	}
	return rs
}

// Vars returns the variable definitions of the grammar in declaration order.
func (g *Grammar) Vars() []*VarDef {
	var vs []*VarDef
	for _, d := range g.File.Decls {
		if v, ok := d.(*VarDef); ok {
			vs = append(vs, v)
		}
	}
	return vs
}

// Types returns the type declarations of the grammar in declaration order.
func (g *Grammar) Types() []TypeDecl {
	return g.types(nil)
}

func (g *Grammar) types(control *metadataControl) []TypeDecl {
	var ts []TypeDecl
	for _, d := range g.File.Decls {
		control.poll()
		if t, ok := d.(TypeDecl); ok {
			ts = append(ts, t)
		}
	}
	return ts
}

// Decl is a declaration of a file: a type, variable or rule declaration.
type Decl interface {
	// DeclName returns the declared name.
	DeclName() string
	// Diagnostic returns the position of the declaration for diagnostics.
	Diagnostic() Pos
	decl()
}

// TypeDecl is a type declaration: a type definition or a type alias.
type TypeDecl interface {
	Decl
	typeDecl()
}

// RuleDecl is a rule declaration: a rule definition.
type RuleDecl interface {
	Decl
	ruleDecl()
}

// VarDecl is a variable declaration: a variable definition.
type VarDecl interface {
	Decl
	varDecl()
}

// TypeDef is a type definition. It defines a new type: a struct type
// (type Name = { ... }), whose Type is a *StructType, or a named node type
// (type Name = node[T]), whose Type is a *TypeApp naming node with one argument.
type TypeDef struct {
	SourceInfo `json:"source,omitempty"`
	Name       string   `json:"name"`
	Type       TypeExpr `json:"type"`

	// Break holds the comments before the declaration. When it is nil,
	// Format separates the declaration from the previous one with a blank
	// line.
	Break *LineBreak `json:"-"`
}

// TypeAlias is a type alias (type Name ~ Type). It names a type without defining a new one.
type TypeAlias struct {
	SourceInfo `json:"source,omitempty"`
	Name       string   `json:"name"`
	Type       TypeExpr `json:"type"`

	// Break holds the comments before the declaration, as for TypeDef.
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

	// Break holds the comments before the declaration, as for TypeDef.
	Break *LineBreak `json:"-"`
	// BodyBreak, when not nil, puts the body on a new line after "=", and
	// ActionBreak puts "=>" on a new line.
	BodyBreak   *LineBreak `json:"-"`
	ActionBreak *LineBreak `json:"-"`
	CloseBreak  *LineBreak `json:"-"`
}

// VarDef is a variable definition (var Name Type = Value). It declares a
// parser variable with its type and its initial value. Type is nil when it is
// omitted and inferred from Value; Value is nil when it is omitted and the
// initial value is the zero value of the type.
type VarDef struct {
	SourceInfo `json:"source,omitempty"`
	Name       string   `json:"name"`
	Type       TypeExpr `json:"type,omitempty"`
	Value      Term     `json:"value,omitempty"`

	// Break holds the comments before the declaration, as for TypeDef.
	Break *LineBreak `json:"-"`
}

func (d *TypeDef) DeclName() string   { return d.Name }
func (d *TypeAlias) DeclName() string { return d.Name }
func (d *RuleDef) DeclName() string   { return d.Name }
func (d *VarDef) DeclName() string    { return d.Name }

// Struct returns the struct type that t defines, or nil when t defines a named
// node type.
func (t *TypeDef) Struct() *StructType {
	st, _ := t.Type.(*StructType)
	return st
}

// Fields returns the fields of the struct type that t defines, and nil when t defines a named
// node type.
func (t *TypeDef) Fields() []*Field {
	if st := t.Struct(); st != nil {
		return st.Fields
	}
	return nil
}

// NodeValue returns the value type T of a named node type defined as
// node[T], or nil when t defines a struct type.
func (t *TypeDef) NodeValue() TypeExpr {
	if app, ok := t.Type.(*TypeApp); ok && app.Name == "node" && len(app.Args) == 1 {
		return app.Args[0]
	}
	return nil
}

func (*TypeDef) decl()       {}
func (*TypeAlias) decl()     {}
func (*RuleDef) decl()       {}
func (*VarDef) decl()        {}
func (*TypeDef) typeDecl()   {}
func (*TypeAlias) typeDecl() {}
func (*RuleDef) ruleDecl()   {}
func (*VarDef) varDecl()     {}

// --- Types ---

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

// JoinType is the join T | U of types: the type of the values of any of them.
type JoinType struct {
	SourceInfo `json:"source,omitempty"`
	Types      []TypeExpr `json:"types"`
}

// MeetType is the meet T & U of types: the type of the values in all of them.
type MeetType struct {
	SourceInfo `json:"source,omitempty"`
	Types      []TypeExpr `json:"types"`
}

// TupleType is a tuple type: () is the unit, (T,) has one element and (T, U) two. Its
// parentheses keep their source comments like a TypeGroup's.
type TupleType struct {
	SourceInfo `json:"source,omitempty"`
	Types      []TypeExpr `json:"types"`
	OpenBreak  *LineBreak `json:"-"`
	BodyBreak  *LineBreak `json:"-"`
	CloseBreak *LineBreak `json:"-"`
	// TrailingComma records a comma after the last element. Format puts the
	// elements of a tuple of two or more on separate lines when it is set; the
	// comma of a one-element tuple is part of the syntax and does not count.
	TrailingComma bool `json:"-"`
}

// TypeApp applies a type to type arguments: Name[Args...], as in node[string].
type TypeApp struct {
	SourceInfo `json:"source,omitempty"`
	Name       string     `json:"name"`
	Args       []TypeExpr `json:"args"`

	// The brackets keep their comments like a TupleType's.
	OpenBreak  *LineBreak `json:"-"`
	BodyBreak  *LineBreak `json:"-"`
	CloseBreak *LineBreak `json:"-"`
	// TrailingComma records a comma after the last argument. Format puts the
	// arguments on separate lines when it is set.
	TrailingComma bool `json:"-"`
}

// Field is a field of a struct type.
type Field struct {
	SourceInfo `json:"source,omitempty"`
	Name       string   `json:"name"`
	Type       TypeExpr `json:"type"`

	Break *LineBreak `json:"-"`
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

func (*TypeRef) typeExpr()      {}
func (*ListType) typeExpr()     {}
func (*OptionalType) typeExpr() {}
func (*JoinType) typeExpr()     {}
func (*MeetType) typeExpr()     {}
func (*TupleType) typeExpr()    {}
func (*TypeApp) typeExpr()      {}
func (*StructType) typeExpr()   {}
func (*TypeGroup) typeExpr()    {}

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

	// Spelling records how the source wrote the pragma #repeat(...), which
	// Format keeps, and is nil for a repetition written * or +, or built
	// by code. It has no other effect.
	Spelling *RepeatSpelling `json:"-"`
}

// RepeatSpelling is how a #repeat pragma was written: which of its tokens were
// there, and the comments between them. The comma of #repeat is part of its
// syntax, not a list separator: #repeat(n) is exactly n, #repeat(n,) at least
// n, #repeat(,m) at most m and #repeat(n, m) from n to m.
//
// The tokens between the parentheses are, in order, the minimum if HasMin, the
// comma if Comma, and the maximum if HasMax. Format uses the spelling only
// while Min and Max are the bounds that were written; for a pragma edited since
// it prints the bounds in the canonical form.
type RepeatSpelling struct {
	HasMin, Comma, HasMax bool
	// Min and Max are the bounds as written.
	Min, Max int
	// Gaps holds the comments between the tokens: Gaps[0] follows the opening
	// parenthesis and the last gap precedes the closing one, so there is one
	// more gap than there are tokens.
	Gaps []RepeatGap
}

// RepeatGap is the space between two tokens of a #repeat pragma.
type RepeatGap struct {
	Comments []*Comment
	// PrevLine and NextLine are the lines of the tokens around the gap.
	PrevLine, NextLine int
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
// Argument values are int, string, bool, nil, or parser expressions. An empty
// argument is written nil.
type Attribute struct {
	SourceInfo `json:"source,omitempty"`
	Name       string `json:"name"`
	// Pragma distinguishes behavioral # directives from @ attributes.
	Pragma bool           `json:"pragma,omitempty"`
	Args   []any          `json:"args,omitempty"`
	KwArgs map[string]any `json:"kwArgs,omitempty"`
	// KeywordOrder preserves source order; JSON grammars format keys sorted.
	KeywordOrder []string `json:"-"`
	// TrailingComma records a comma after the last argument. Format puts the
	// arguments on separate lines when it is set.
	TrailingComma bool `json:"-"`
	// ArgsLayout keeps the comments around the arguments in source order,
	// positional ones first. Its Elems match Args and then KeywordOrder by
	// index. They go stale if Args or KwArgs are edited without it: Format
	// writes all the comments after the opening parenthesis when the number of
	// arguments differs, and otherwise keeps each comment with the argument at
	// its index.
	ArgsLayout *ListLayout `json:"-"`
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

// New creates a value of a defined type. A struct constructor names its
// fields: Type{Field: value, ...}. A node type constructor takes one
// positional value, Type{value}, held in Args. A constructor uses Fields or
// Args, never both.
type New struct {
	SourceInfo `json:"source,omitempty"`
	Type       string       `json:"type"`
	Fields     []*FieldInit `json:"fields,omitempty"`
	Args       []Term       `json:"args,omitempty"`

	// The braces keep their comments like a TupleLit's parentheses.
	OpenBreak  *LineBreak `json:"-"`
	BodyBreak  *LineBreak `json:"-"`
	CloseBreak *LineBreak `json:"-"`
	// TrailingComma records a comma after the last element. Format puts the
	// elements on separate lines when it is set.
	TrailingComma bool `json:"-"`
}

// TupleLit is a tuple value: () is the unit, (x,) has one element and (x, y) two. Its
// parentheses keep their source comments like a TermGroup's.
type TupleLit struct {
	SourceInfo `json:"source,omitempty"`
	Elems      []Term     `json:"elems"`
	OpenBreak  *LineBreak `json:"-"`
	BodyBreak  *LineBreak `json:"-"`
	CloseBreak *LineBreak `json:"-"`
	// TrailingComma records a comma after the last element. Format puts the
	// elements of a tuple of two or more on separate lines when it is set; the
	// comma of a one-element tuple is part of the syntax and does not count.
	TrailingComma bool `json:"-"`
}

// ListConstructor builds a node list with an explicit element type: []T{...}.
type ListConstructor struct {
	SourceInfo `json:"source,omitempty"`
	Elem       TypeExpr `json:"elem"`
	Items      []Term   `json:"items"`

	// The braces keep their comments like a TupleLit's parentheses.
	OpenBreak  *LineBreak `json:"-"`
	BodyBreak  *LineBreak `json:"-"`
	CloseBreak *LineBreak `json:"-"`
	// TrailingComma records a comma after the last item. Format puts the
	// items on separate lines when it is set.
	TrailingComma bool `json:"-"`
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

	// The parentheses keep their comments like a TupleLit's.
	OpenBreak  *LineBreak `json:"-"`
	BodyBreak  *LineBreak `json:"-"`
	CloseBreak *LineBreak `json:"-"`
	// TrailingComma records a comma after the last argument. Format puts the
	// arguments on separate lines when it is set.
	TrailingComma bool `json:"-"`
}

// Lambda is a function (a, b) => body.
type Lambda struct {
	SourceInfo `json:"source,omitempty"`
	Params     []string `json:"params"`
	Body       Term     `json:"body"`

	// TrailingComma records a comma after the last parameter. Format puts the
	// parameters on separate lines when it is set.
	TrailingComma bool `json:"-"`
	// ParamsLayout keeps the comments around the parameters.
	ParamsLayout *ListLayout `json:"-"`
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

// Bind binds a variable in a predicate action: name := value.
type Bind struct {
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

func (*IntLit) term()          {}
func (*StringLit) term()       {}
func (*BoolLit) term()         {}
func (*NilLit) term()          {}
func (*CaptureRef) term()      {}
func (*IndexRef) term()        {}
func (*VarRef) term()          {}
func (*Member) term()          {}
func (*New) term()             {}
func (*ListConstructor) term() {}
func (*TupleLit) term()        {}
func (*Call) term()            {}
func (*Lambda) term()          {}
func (*Binary) term()          {}
func (*Unary) term()           {}
func (*Cast) term()            {}
func (*Bind) term()            {}
