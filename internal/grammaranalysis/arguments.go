package grammaranalysis

import (
	"cmp"
	"slices"

	"github.com/ornew/pego/grammar"
)

// ExpressionArgsWithPoll preserves Attribute.ExpressionArgs order and fresh
// storage while sharing an analysis invocation's polling counter. This avoids
// starting a separate context operation for every small attribute. A polling
// panic passes unchanged to the enclosing analysis entry point.
func ExpressionArgsWithPoll(a *grammar.Attribute, poll func()) []grammar.Expr {
	if poll == nil {
		return a.ExpressionArgs()
	}
	var result []grammar.Expr
	for _, value := range a.Args {
		poll()
		if e, ok := value.(grammar.Expr); ok {
			result = append(result, e)
		}
	}
	keys := make([]string, 0, len(a.KwArgs))
	for key := range a.KwArgs {
		poll()
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b string) int { poll(); return cmp.Compare(a, b) })
	for _, key := range keys {
		poll()
		if e, ok := a.KwArgs[key].(grammar.Expr); ok {
			result = append(result, e)
		}
	}
	return result
}
