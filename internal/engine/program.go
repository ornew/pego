package engine

import (
	"context"
	"slices"
	"sync"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/bytecode"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine/core"
)

// sharedProgram names the part of a program that every backend shares, so that Program embeds it
// under an unexported field name.
type sharedProgram = core.SharedProgram

// Program is a compiled grammar. It can be used by multiple parses concurrently.
type Program struct {
	sharedProgram
	// closureRules holds the rules the closure backend runs, by ID (rule.id).
	closureRules []*core.Rule

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
}

// newProgram returns the program that runs the static program sp with the rules of the closure
// backend.
func newProgram(sp *compile.Program, closureRules []*core.Rule) *Program {
	prog := &Program{closureRules: closureRules}
	core.InitSharedProgram(&prog.sharedProgram, sp)
	return prog
}

// SourceInfo reports whether the program records source information in its results
// (Options.SourceInfo).
func (p *Program) SourceInfo() bool { return p.RecordsSourceInfo }

// DefaultValue reports whether the program was compiled to make default values (Options.DefaultValue).
func (p *Program) DefaultValue() bool { return p.MakesDefaultValue }

// SourceGrammar returns the grammar the program was compiled from: the grammar as written, without
// the hidden rules that inline actions are lowered to.
func (p *Program) SourceGrammar() *grammar.Grammar {
	if p.Source != nil {
		return p.Source
	}
	return p.Grammar
}

// Starts returns the start rules of the grammar, the rules marked @start where
// parsing may begin, in declaration order.
func (prog *Program) Starts() []string { return slices.Clone(prog.StartRules) }

// DefaultStart returns the default start rule: the one marked
// @start(default), or the only start rule.
func (prog *Program) DefaultStart() string { return prog.DefaultStartRule }

// CheckStart reports an error unless name selects a start rule: it is the name
// of a start rule, or empty for the default start rule.
func (prog *Program) CheckStart(name string) error { return prog.Static().CheckStart(name) }

// Module compiles the program to bytecode. The result is stored in the program and reused on
// subsequent calls.
func (prog *Program) Module() *bytecode.Module { return prog.BytecodeModule() }

// RuleType returns the type of the value of the rule name: its declared type, or the inferred
// type of its value (for example "[]Expr | node[[]node[string]]"). In an inferred type, a
// reference to another rule is that rule's declared type, or the rule's name if it has none. It
// reports false if the program has no such rule or its types were not checked
// (a program loaded from a compiled grammar).
func (prog *Program) RuleType(name string) (string, bool) { return prog.Static().RuleType(name) }

// RuleTypeWithPoll is RuleType with invocation-owned polling for editor display.
// It does not retain poll. Callback panics propagate unchanged to the caller.
// Individual allocations and string comparisons/concatenations may finish
// before the next callback. A nil callback uses the ordinary type renderer.
func (prog *Program) RuleTypeWithPoll(name string, poll func()) (string, bool) {
	return prog.Static().RuleTypeWithPoll(name, poll)
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
	return prog.Static().ShapeSignature(name, poll)
}

// ValueType returns the type of the value that the rule name builds in the value model (for
// example "node[(*node[string],)]"). It reports false if the program has no such rule or its
// types were not checked.
func (prog *Program) ValueType(name string) (string, bool) { return prog.Static().ValueType(name) }

// Compile compiles the grammar.
func Compile(g *grammar.Grammar, opts Options) (*Program, error) {
	return build(g, opts, nil)
}

// build builds a program from the grammar. If flags is not nil, it skips static analysis and
// type checking and uses flags instead.
func build(g *grammar.Grammar, opts compile.Options, flags []compile.RuleFlags) (*Program, error) {
	return buildControlled(g, opts, flags, nil)
}

func buildControlled(g *grammar.Grammar, opts compile.Options, flags []compile.RuleFlags, control *compile.Control) (*Program, error) {
	names := compile.InlineNames{}
	prog, err := buildNamed(g, opts, flags, names, control)
	if err != nil {
		return nil, names.Rewrite(err)
	}
	return prog, nil
}

// CompileContext is the internal editor compilation entry point. Cancellation
// discards the entire program and accumulated diagnostics. Existing Compile and
// saved-program loading keep their ordinary path with no active control.
// Polls cover grammar AST validation, source normalization, rule/dependency
// loops, shared success proofs, expression construction and type checking.
// Shared metadata and individual operations still have uncancellable stretches.
// This is not a wall-clock latency guarantee.
func CompileContext(ctx context.Context, g *grammar.Grammar, opts Options) (prog *Program, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ctx.Done() == nil {
		return Compile(g, opts)
	}
	defer func() {
		if failure := recover(); failure != nil {
			if cancelled, ok := failure.(compile.Cancellation); ok {
				prog, err = nil, cancelled.Err
			} else {
				panic(failure)
			}
		}
	}()
	return buildControlled(g, opts, nil, &compile.Control{Ctx: ctx})
}

// buildNamed is buildControlled; names receives the hidden rules that the lowering of inline actions
// generates.
func buildNamed(g *grammar.Grammar, opts compile.Options, flags []compile.RuleFlags, names compile.InlineNames, control *compile.Control) (*Program, error) {
	var b *closureBuilder
	prog, err := compile.CompileProgram(g, opts, flags, names, control, func(c *compile.Compiler) compile.ExprBuilder[core.Matcher] {
		b = newClosureBuilder(c, !opts.ControlledDispatch)
		return b
	})
	if err != nil {
		return nil, err
	}
	return newProgram(prog, b.finish()), nil
}
