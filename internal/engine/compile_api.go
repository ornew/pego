package engine

import (
	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
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

// staticProgram and staticRule are the compiler's program and rule. Program and rule embed them
// under these names, so the embedded fields stay unexported.
type (
	staticProgram = compile.Program
	staticRule    = compile.Rule
)

// expID and msgBit are the compiler's expectation IDs and the bit that marks an @error message;
// the runtime records them under these names, which the runtime package of generated parsers
// exports as ExpID and MsgBit.
type expID = compile.ExpID

const msgBit = compile.MsgBit
