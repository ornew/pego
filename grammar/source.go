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
}
