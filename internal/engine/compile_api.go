package engine

import (
	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/compile"
)

// Options configures compilation.
type Options = compile.Options

// Error is a compile error.
type Error = compile.Error

// ErrorList is a list of compile errors.
type ErrorList = compile.ErrorList

// InlineActionNames maps the hidden rules that LowerInlineActions generated to the inline actions of
// the grammar as written. A nil value describes a grammar without inline actions.
type InlineActionNames = compile.InlineActionNames

// LowerInlineActions returns g with every inline action replaced by a reference to the hidden rule
// that the compiler runs in its place; see compile.LowerInlineActions.
func LowerInlineActions(g *grammar.Grammar) (*grammar.Grammar, *InlineActionNames, error) {
	return compile.LowerInlineActions(g)
}
