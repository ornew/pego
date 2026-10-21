package grammar

// SourceInfo holds the syntax span and documentation owned by an AST node.
// Pos and End are 1-based rune positions; End is exclusive. The span excludes
// comments outside the node. Zero positions indicate unavailable source data.
//
// Comment strings omit their markers and preserve content and line endings.
// LeadingDetachedComments contains separate paragraphs before the node.
// Formatter comment tokens and layout are stored separately.
type SourceInfo struct {
	// Layout preserves raw token-boundary trivia for in-memory formatting.
	// It is separate from normalized documentation and is not serialized.
	Layout                  *SourceLayout `json:"-"`
	Pos                     Pos           `json:"pos,omitempty"`
	End                     Pos           `json:"end,omitempty"`
	LeadingComments         string        `json:"leadingComments,omitempty"`
	TrailingComments        string        `json:"trailingComments,omitempty"`
	LeadingDetachedComments []string      `json:"leadingDetachedComments,omitempty"`
	// DiagnosticPos identifies a diagnostic token when it differs from Pos,
	// such as an operator inside a binary expression.
	DiagnosticPos Pos `json:"diagnosticPos,omitempty"`
}

// Source returns mutable source information. Embedding SourceInfo makes this
// method available on all source-bearing grammar nodes.
func (s *SourceInfo) Source() *SourceInfo { return s }

// Diagnostic returns the diagnostic token position, falling back to Pos.
func (s SourceInfo) Diagnostic() Pos {
	if s.DiagnosticPos.IsValid() {
		return s.DiagnosticPos
	}
	return s.Pos
}

// SourceNode is implemented by source-bearing grammar nodes.
type SourceNode interface {
	Source() *SourceInfo
}

// ListLayout keeps the comments of a parenthesized list whose elements are not
// nodes of their own, such as the arguments of an attribute and the parameters
// of a lambda. The parentheses keep their comments like those of a TupleType,
// and Elems[i] holds the comments around the i-th element, which may be nil.
type ListLayout struct {
	OpenBreak  *LineBreak
	BodyBreak  *LineBreak
	CloseBreak *LineBreak
	Elems      []*SourceLayout
}

// SourceLayout retains spelling and placement without assigning comment owners.
// A nil value uses ordinary canonical formatting. Positions only distinguish
// adjacent lines and blank lines; they are not offsets in the formatted output.
type SourceLayout struct {
	Before       []*Comment
	PreviousLine int
	NextLine     int
	// After holds the comments between the node and the comma that follows it
	// in a list of elements, fields or arguments.
	After []*Comment
	// ElemLine is the line the node ends on, and AfterLine the line of that
	// comma.
	ElemLine, AfterLine int
	// Trail holds the comments on the line of that comma after it, when they
	// end the line.
	Trail []*Comment
	// Operator holds the comments between the operand of a postfix rule
	// expression (a repetition, an optional, an #atomic, #stream or #recover
	// pragma) and its operator, all on one line with both.
	Operator []*Comment
	// Close holds the comments before the closing parenthesis of a #recover
	// pragma.
	Close *Gap
	// OpGap holds the comments between the left operand of a binary operation
	// and its operator.
	OpGap *Gap
	// Lead holds the comments between the "/" before an alternative, or the
	// "=" after "skip" in a #recover pragma, and the first token of the node.
	Lead *Gap
}

// Gap holds the comments between two tokens of an expression inside
// delimiters, where a comment may stand on a line of its own. PreviousLine is
// the line the token before ends on, NextLine the line the token after starts
// on, and the comments keep their lines, so the formatter writes a comment on
// the same line as a token, or on a line of its own, as in the source, and a
// comment never changes what it belongs to.
type Gap struct {
	Comments     []*Comment
	PreviousLine int
	NextLine     int
}

// In a rule expression, a node's Layout holds the block comments that stand on
// one line with the tokens around them: Before has those between the token
// before the node and its first token, and After those between its last token
// and the next, when that is "/" or "=>". Comments that need a line of their
// own are kept in the LineBreak fields of the enclosing nodes.
