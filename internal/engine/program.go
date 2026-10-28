package engine

import (
	"slices"
	"sync"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/bytecode"
)

// Program is a compiled grammar. It can be used by multiple parses concurrently.
type Program struct {
	*staticProgram
	// closureRules holds the rules the closure backend runs, by ID (rule.id).
	closureRules []*rule

	workOnce    sync.Once
	workProgram *Program
	workErr     error

	recOnce sync.Once
	rec     *Program // program that builds no tree (recognizer)
	recErr  error
	vmOnce  sync.Once
	vm      *vmProgram
	ivmOnce sync.Once
	ivm     *vmProgram
	// loaded reports that the program was loaded from a compiled grammar, whose module serves the
	// parses of mode only (checkMode).
	loaded bool
	mode   CompiledMode
	// scratch holds the buffers of finished parses for later ones (*scratch).
	scratch sync.Pool
}

// SourceInfo reports whether the program records source information in its results
// (Options.SourceInfo).
func (p *Program) SourceInfo() bool { return p.sourceInfo }

// DefaultValue reports whether the program was compiled to make default values (Options.DefaultValue).
func (p *Program) DefaultValue() bool { return p.defaultValue }

// SourceGrammar returns the grammar the program was compiled from: the grammar as written, without
// the hidden rules that inline actions are lowered to.
func (p *Program) SourceGrammar() *grammar.Grammar {
	if p.source != nil {
		return p.source
	}
	return p.Grammar
}

// Starts returns the start rules of the grammar, the rules marked @start where
// parsing may begin, in declaration order.
func (prog *Program) Starts() []string { return slices.Clone(prog.starts) }

// DefaultStart returns the default start rule: the one marked
// @start(default), or the only start rule.
func (prog *Program) DefaultStart() string { return prog.defaultStart }

// CheckStart reports an error unless name selects a start rule: it is the name
// of a start rule, or empty for the default start rule.
func (prog *Program) CheckStart(name string) error { return prog.checkStart(name) }

// Module compiles the program to bytecode. The result is stored in the program and reused on
// subsequent calls.
func (prog *Program) Module() *bytecode.Module { return prog.bytecodeModule() }

// RuleType returns the type of the value of the rule name: its declared type, or the inferred
// type of its value (for example "[]Expr | node[[]node[string]]"). In an inferred type, a
// reference to another rule is that rule's declared type, or the rule's name if it has none. It
// reports false if the program has no such rule or its types were not checked
// (a program loaded from a compiled grammar).
func (prog *Program) RuleType(name string) (string, bool) { return prog.ruleType(name) }

// RuleTypeWithPoll is RuleType with invocation-owned polling for editor display.
// It does not retain poll. Callback panics propagate unchanged to the caller.
// Individual allocations and string comparisons/concatenations may finish
// before the next callback. A nil callback uses the ordinary type renderer.
func (prog *Program) RuleTypeWithPoll(name string, poll func()) (string, bool) {
	return prog.ruleTypeWithPoll(name, poll)
}

// ShapeSignature returns a text that describes the elements and captures that every subexpression
// of the rule name contributes to the tuple of its scope, in preorder, leaving out the lookaheads
// and what they hold. The type of a rule hides a change inside an action, such as a discard that
// moved to another element, but the shapes of the subexpressions show it. It reports false if
// the program has no such rule or its types were not checked. poll, when not nil, is called at
// every subexpression.
func (prog *Program) ShapeSignature(name string, poll func()) (string, bool) {
	if prog == nil {
		return "", false
	}
	return prog.shapeSignature(name, poll)
}

// ValueType returns the type of the value that the rule name builds in the value model (for
// example "node[(*node[string],)]"). It reports false if the program has no such rule or its
// types were not checked.
func (prog *Program) ValueType(name string) (string, bool) { return prog.valueType(name) }
