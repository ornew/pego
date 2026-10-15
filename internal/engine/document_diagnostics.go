package engine

// copyDocumentDiagnostics publishes owned recovered errors without exposing
// memoized error objects or their mutable slice containers. Ordinary failures
// are already freshly constructed by syntaxError.
func copyDocumentDiagnostics(p *parser, src SyntaxErrors) SyntaxErrors {
	if src == nil {
		return nil
	}
	copyStrings := func(src []string) []string {
		if src == nil {
			return nil
		}
		p.valueWork(len(src))
		out := make([]string, len(src))
		copyWork(p, out, src)
		return out
	}
	p.valueWork(len(src))
	out := make(SyntaxErrors, len(src))
	for i, e := range src {
		p.stepWork()
		if e == nil {
			continue
		}
		cloned := new(SyntaxError)
		*cloned = *e
		cloned.Expected = copyStrings(e.Expected)
		cloned.Messages = copyStrings(e.Messages)
		out[i] = cloned
	}
	return out
}
