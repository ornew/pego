package pego

// PrefixResult reports a rule's value and successful cursor. Consumption comes
// from the runtime cursor, even when Node is nil or its span differs.
type PrefixResult struct {
	Node          *Node
	Consumed      int // Selected parser Unit; defaults to CodePoints.
	ConsumedBytes int // Exact UTF-8 byte endpoint in the supplied input.
}

// ParsePrefix parses the named rule at the beginning of input without requiring
// whole-input consumption. EOF assertions still refer to the complete input.
// Positions remain local to input. For a source slice, add its starting byte
// offset to ConsumedBytes to find the consumed endpoint in the original source.
// Recovered success returns the result and SyntaxErrors. Unrecovered syntax
// failures and fatal errors return a zero result. A zero-width success returns
// zero consumption; a nil Node does not imply failure. Parse remains whole-input.
// WithDiagnosticCallback and positive WithMissingTokens limits are invalid for
// this operation. Grammars containing #missing are unsupported even when
// insertion is disabled.
func (p *Parser) ParsePrefix(name, input string, opts ...ParseOption) (PrefixResult, error) {
	r, err := p.prog.ParsePrefixWith(name, input, parseOptions(opts))
	return PrefixResult{Node: r.Node, Consumed: r.Consumed, ConsumedBytes: r.ConsumedBytes}, err
}
