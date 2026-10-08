package pego

import (
	"fmt"
	"slices"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/lint"
)

// Finding is a likely mistake that Lint found in a grammar: where it is (Pos, the zero Pos for a
// grammar without positions), how serious it is, the check that found it, the rule it is in, a
// message, and a suggested fix ("" if there is no single obvious one).
type Finding = lint.Finding

// Severity tells how likely a Finding is to be a mistake.
type Severity = lint.Severity

const (
	// SeverityHint is a suggestion, usually about performance; the grammar is correct as written.
	SeverityHint = lint.Hint
	// SeverityWarning is a likely mistake, or an expression that has no effect.
	SeverityWarning = lint.Warning
	// SeverityError is a certain mistake: part of the grammar can never take effect.
	SeverityError = lint.Error
)

// LintCheck describes a check of Lint: its name, the highest severity it reports, and what it
// looks for.
type LintCheck = lint.Check

// LintChecks returns the checks that Lint runs.
func LintChecks() []LintCheck { return slices.Clone(lint.Checks) }

// LintOption configures Lint.
type LintOption func(*lint.Options)

// DisableChecks turns off the named checks (see LintChecks).
func DisableChecks(names ...string) LintOption {
	return func(o *lint.Options) {
		if o.Disable == nil {
			o.Disable = map[string]bool{}
		}
		for _, n := range names {
			o.Disable[n] = true
		}
	}
}

// Lint reports likely mistakes in the grammar g, whose start rule is start: rules the start rule
// never uses, alternatives and expressions that can never match or have no effect, captures that
// are never read, and grammar shapes that slow down incremental parsing. The findings are sorted
// by position. Comments of the form "// lint:ignore check reason" in the source suppress findings
// (see docs/guide/linting.md).
//
// Lint returns an error, and no findings, if g does not compile, if start is not a rule of g, or
// if an option names an unknown check.
func Lint(g *grammar.Grammar, start string, opts ...LintOption) ([]Finding, error) {
	var o lint.Options
	for _, opt := range opts {
		opt(&o)
	}
	for name := range o.Disable {
		if !lint.IsCheck(name) {
			return nil, fmt.Errorf("unknown check %q", name)
		}
	}
	prog, err := engine.Compile(g, engine.Options{})
	if err != nil {
		return nil, err
	}
	if !slices.Contains(prog.Rules(), start) {
		return nil, fmt.Errorf("start rule %s is not defined", start)
	}
	return lint.Run(g, start, o), nil
}
