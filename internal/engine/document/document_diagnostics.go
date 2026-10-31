package document

import "github.com/ornew/zeroparse/internal/engine/core"

// copyDocumentDiagnostics publishes owned recovered errors without exposing
// memoized error objects or their mutable slice containers. Ordinary failures
// are already freshly constructed by Parser.SyntaxError.
func copyDocumentDiagnostics(p *core.Parser, src core.SyntaxErrors) core.SyntaxErrors {
	if src == nil {
		return nil
	}
	copyStrings := func(src []string) []string {
		if src == nil {
			return nil
		}
		p.SpendValue(len(src))
		out := make([]string, len(src))
		core.CopySpend(p, out, src)
		return out
	}
	p.SpendValue(len(src))
	out := make(core.SyntaxErrors, len(src))
	for i, e := range src {
		p.Spend()
		if e == nil {
			continue
		}
		cloned := new(core.SyntaxError)
		*cloned = *e
		cloned.Expected = copyStrings(e.Expected)
		cloned.Messages = copyStrings(e.Messages)
		out[i] = cloned
	}
	return out
}
