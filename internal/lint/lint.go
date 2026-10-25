// Package lint reports likely mistakes in PEGO grammars: rules and alternatives that can never
// match, expressions that have no effect, captures that are never read, and grammar shapes that
// defeat incremental parsing.
//
// The checks work on the grammar AST alone. Each check reports only what it can prove from the
// grammar (the exceptions are the performance hints, which have the severity Hint); see
// docs/design/records/018-grammar-linting.md for the reasoning behind each one.
package lint

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/engine"
)

// Severity tells how likely a finding is to be a mistake.
type Severity int

const (
	// Hint is a suggestion, usually about performance; the grammar is correct as written.
	Hint Severity = iota + 1
	// Warning is a likely mistake, or an expression that has no effect.
	Warning
	// Error is a certain mistake: part of the grammar can never take effect.
	Error
)

func (s Severity) String() string {
	switch s {
	case Hint:
		return "hint"
	case Warning:
		return "warning"
	case Error:
		return "error"
	}
	return fmt.Sprintf("Severity(%d)", int(s))
}

// MarshalText encodes the severity as its name.
func (s Severity) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// UnmarshalText decodes the name of a severity.
func (s *Severity) UnmarshalText(text []byte) error {
	for _, v := range []Severity{Hint, Warning, Error} {
		if v.String() == string(text) {
			*s = v
			return nil
		}
	}
	return fmt.Errorf("unknown severity %q", text)
}

// Finding is a problem found in a grammar.
type Finding struct {
	// Pos is where the problem is in the source. It is the zero Pos for a grammar without
	// positions (decoded from JSON, or loaded from a compiled grammar).
	Pos      grammar.Pos
	Severity Severity
	// Check is the name of the check that reported the finding (see Checks).
	Check string
	// Rule is the rule the finding is in, if any.
	Rule    string
	Message string
	// Fix is a suggested change, or "" if there is no single obvious one.
	Fix string
}

func (f Finding) String() string {
	s := fmt.Sprintf("%s: %s [%s]", f.Severity, f.Message, f.Check)
	if f.Pos.IsValid() {
		s = fmt.Sprintf("%d:%d: %s", f.Pos.Line, f.Pos.Col, s)
	}
	return s
}

// Check describes a check.
type Check struct {
	Name string
	// Severity is the highest severity the check reports.
	Severity Severity
	Summary  string
}

// Names of the checks.
const (
	CheckUnreachableRule    = "unreachable-rule"
	CheckShadowedAlt        = "shadowed-alternative"
	CheckNeverMatches       = "never-matches"
	CheckUselessLookahead   = "useless-lookahead"
	CheckNullableRepetition = "nullable-repetition"
	CheckRedundantOptional  = "redundant-optional"
	CheckUnusedCapture      = "unused-capture"
	CheckDuplicateCapture   = "duplicate-capture"
	CheckCharClass          = "char-class"
	CheckRightRecursion     = "right-recursion"
	CheckPositions          = "positions"
	CheckLongLookahead      = "long-lookahead"
	CheckCaseInsensitive    = "case-insensitive-literal"
	CheckDirective          = "lint-directive"
)

// Checks lists every check, in the order of the documentation.
var Checks = []Check{
	{CheckShadowedAlt, Error, "an alternative of an ordered choice can never match, because an earlier one matches wherever it would"},
	{CheckNeverMatches, Error, "an expression or rule can never match (anchors that contradict their neighbors, recursion without a base case)"},
	{CheckUselessLookahead, Error, "a lookahead whose result is known in advance (& that always succeeds, ! that never does)"},
	{CheckNullableRepetition, Warning, "a repetition of an expression that can match without consuming input"},
	{CheckRedundantOptional, Warning, "e? where e always succeeds"},
	{CheckUnusedCapture, Warning, "a capture whose value is discarded"},
	{CheckDuplicateCapture, Warning, "the same name captured twice in one match, the second overwriting the first"},
	{CheckCharClass, Warning, "a character class with overlapping ranges, or a range that spans punctuation between letters and digits"},
	{CheckUnreachableRule, Warning, "a rule that the start rule never calls"},
	{CheckRightRecursion, Hint, "a list written as right recursion instead of a repetition"},
	{CheckPositions, Hint, ".Start or .End in a rule used repeatedly, which blocks reuse in incremental parsing"},
	{CheckLongLookahead, Hint, "a lookahead that can examine input across lines, which blocks reuse in incremental parsing"},
	{CheckCaseInsensitive, Hint, "a run of [sS]-style classes that spells a word, which a literal with #ignorecase writes more clearly and faster"},
	{CheckDirective, Warning, "a lint:ignore comment that names an unknown check or suppresses nothing"},
}

// IsCheck reports whether name is the name of a check.
func IsCheck(name string) bool {
	return slices.ContainsFunc(Checks, func(c Check) bool { return c.Name == name })
}

// Options configures Run.
type Options struct {
	// Disable holds the names of the checks not to run.
	Disable map[string]bool
}

// Run lints the grammar g, whose start rule is start. g must be a valid grammar (one that
// compiles); start may be "". unreachable-rule reports the rules that neither start nor a rule
// marked @start reaches. The findings are sorted by position.
//
// Findings can be suppressed with comments in the source (see the directive syntax in
// docs/guide/linting.md).
func Run(g *grammar.Grammar, start string, opts Options) []Finding {
	return runControlled(g, start, opts, nil)
}

func runControlled(g *grammar.Grammar, start string, opts Options, control *lintControl) []Finding {
	control.check()
	// Each inline action is a rule of its own with its captures and cuts: lint the grammar with
	// them lowered to hidden rules, and report their findings as the inline actions written.
	lowered, names, err := engine.LowerInlineActions(g)
	if err != nil {
		lowered, names = g, nil // a grammar that does not compile is not linted properly
	}
	control.check()
	l := &linter{control: control, opts: opts, an: newAnalysisControlled(lowered, control)}
	for _, c := range []struct {
		name string
		run  func(*linter)
	}{
		{CheckUnreachableRule, func(l *linter) { l.unreachable(start) }},
		{CheckShadowedAlt, (*linter).shadowed},
		{CheckNeverMatches, (*linter).neverMatches},
		{CheckUselessLookahead, (*linter).lookaheads},
		{CheckNullableRepetition, (*linter).nullableRepetitions},
		{CheckRedundantOptional, (*linter).redundantOptionals},
		{CheckUnusedCapture, (*linter).unusedCaptures},
		{CheckDuplicateCapture, (*linter).duplicateCaptures},
		{CheckCharClass, (*linter).charClasses},
		{CheckRightRecursion, (*linter).rightRecursion},
		{CheckPositions, func(l *linter) { l.positions(start) }},
		{CheckLongLookahead, (*linter).longLookaheads},
		{CheckCaseInsensitive, (*linter).caseInsensitiveLiterals},
	} {
		control.check()
		if !opts.Disable[c.name] {
			c.run(l)
		}
	}
	control.check()
	l.findings = l.written(names)
	findings := l.suppress(g)
	slices.SortStableFunc(findings, func(a, b Finding) int {
		control.poll()
		return cmp.Or(cmp.Compare(a.Pos.Line, b.Pos.Line), cmp.Compare(a.Pos.Col, b.Pos.Col),
			strings.Compare(a.Check, b.Check), strings.Compare(a.Message, b.Message))
	})
	findings = slices.CompactFunc(findings, func(a, b Finding) bool { control.poll(); return a == b })
	control.check()
	return findings
}

type linter struct {
	control  *lintControl
	opts     Options
	an       *analysis
	findings []Finding
	rule     *grammar.RuleDef // the rule being checked
}

func (l *linter) report(check string, sev Severity, pos grammar.Pos, fix, format string, args ...any) {
	control := l.control
	control.poll()

	f := Finding{Pos: pos, Severity: sev, Check: check, Message: fmt.Sprintf(format, args...), Fix: fix}
	if l.rule != nil {
		f.Rule = l.rule.Name
		if !f.Pos.IsValid() {
			f.Pos = l.rule.Diagnostic()
		}
	}
	l.findings = append(l.findings, f)
}

// written turns the findings about the lowered grammar into findings about the grammar as written:
// a finding in the hidden rule of an inline action is in the rule that holds it, a generated name
// in a message or a suggestion becomes the inline action, and the rule of an inline action is not
// reported as unreachable apart from the rule that holds it.
func (l *linter) written(names *engine.InlineActionNames) []Finding {
	if names == nil {
		return l.findings
	}
	out := make([]Finding, 0, len(l.findings))
	for _, f := range l.findings {
		l.control.poll()
		if holder, generated := names.Holder(f.Rule); generated {
			if f.Check == CheckUnreachableRule {
				continue
			}
			f.Rule = holder
		}
		f.Message, f.Fix = names.Message(f.Message), names.Message(f.Fix)
		out = append(out, f)
	}
	return out
}

// eachRule calls f for every rule, with l.rule set to it.
func (l *linter) eachRule(f func(r *grammar.RuleDef)) {
	control := l.control
	control.poll()

	for _, r := range l.an.order {
		control.poll()
		l.rule = r
		f(r)
	}
	l.rule = nil
}

// --- Suppression ---

// A directive is a comment that suppresses findings:
//
//	// lint:ignore check[,check...] [reason]       the line of the comment and the next line,
//	                                               or the whole rule when it precedes a definition
//	// lint:file-ignore check[,check...] [reason]  the whole grammar
type directive struct {
	c      *grammar.Comment
	checks []string
	file   bool
	rule   string // set for a directive before a rule definition
	used   bool
}

func parseDirective(c *grammar.Comment) *directive {
	text := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
	kind, rest, _ := strings.Cut(text, " ")
	if kind != "lint:ignore" && kind != "lint:file-ignore" {
		return nil
	}
	d := &directive{c: c, file: kind == "lint:file-ignore"}
	if names := strings.Fields(rest); len(names) > 0 {
		d.checks = strings.Split(names[0], ",")
	}
	return d
}

func (d *directive) covers(f Finding) bool {
	if !slices.Contains(d.checks, f.Check) {
		return false
	}
	switch {
	case d.file:
		return true
	case d.rule != "":
		return f.Rule == d.rule
	}
	return f.Pos.Line == d.c.Pos.Line || f.Pos.Line == d.c.Pos.Line+1
}

// suppress removes the findings that directives suppress, and reports directives that are
// malformed or suppress nothing.
func (l *linter) suppress(g *grammar.Grammar) []Finding {
	control := l.control
	control.poll()

	var ds []*directive
	ruleOf := map[*grammar.Comment]string{}
	for _, r := range control.rules(g) {
		control.poll()
		if r.Break != nil {
			for _, c := range r.Break.Comments {
				control.poll()
				ruleOf[c] = r.Name
			}
		}
	}
	for _, c := range control.comments(g) {
		control.poll()
		if d := parseDirective(c); d != nil {
			d.rule = ruleOf[c]
			ds = append(ds, d)
		}
	}
	var kept []Finding
	for _, f := range l.findings {
		control.poll()
		suppressed := false
		for _, d := range ds {
			control.poll()
			if d.covers(f) {
				d.used, suppressed = true, true
			}
		}
		if !suppressed {
			kept = append(kept, f)
		}
	}
	if l.opts.Disable[CheckDirective] {
		return kept
	}
	for _, d := range ds {
		control.poll()
		kind := "lint:ignore"
		if d.file {
			kind = "lint:file-ignore"
		}
		f := Finding{Pos: d.c.Pos, Severity: Warning, Check: CheckDirective, Rule: d.rule}
		var unknown []string
		for _, name := range d.checks {
			control.poll()
			if !IsCheck(name) {
				unknown = append(unknown, name)
			}
		}
		switch {
		case len(d.checks) == 0:
			f.Message = kind + " names no check"
			f.Fix = "write the names of the checks to suppress, as in // " + kind + " unused-capture reason"
		case len(unknown) > 0:
			f.Message = fmt.Sprintf("%s names unknown checks: %s", kind, strings.Join(unknown, ", "))
			f.Fix = "use the names shown in brackets after the findings"
		case d.used || slices.ContainsFunc(d.checks, func(n string) bool { control.poll(); return l.opts.Disable[n] }):
			continue
		default:
			f.Message = kind + " suppresses no finding"
			f.Fix = "remove the comment"
		}
		kept = append(kept, f)
	}
	return kept
}
