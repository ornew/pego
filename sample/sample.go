// Package sample generates inputs that a grammar accepts, for testing code that consumes parse results
// and for seeding fuzz tests.
//
// A Generator walks the grammar AST of a parser from its start rule and writes candidate inputs, making
// random decisions at ordered choices, repetitions, optional expressions and Pratt expressions. The
// decisions are reproducible: the same parser, options and seed give the same inputs. Recursion is
// bounded (WithMaxDepth, WithMaxLen): past the bounds, the generator takes the shortest way to finish
// the input.
//
// Every input that the generator returns has been parsed with the parser and accepted without errors
// (also without errors recovered with #recover). Generating a candidate by walking the grammar is not
// enough under PEG semantics: an ordered choice may never reach a later alternative for some inputs,
// a greedy repetition consumes as much as it can (no input generated for a* a parses), and lookaheads,
// predicates and variables depend on the input around them. The generator steers away from such
// candidates where it can tell, evaluating predicates on the text it generated and checking what the
// parser would match as the text grows, and retries otherwise; what it cannot evaluate, such as
// predicates over the results of actions, is handled only by generating and checking. The design is
// described in docs/design/015-input-generation.md.
//
// With WithCoverage, the generator prefers rules and alternatives that no accepted input has exercised
// yet, and Coverage reports those that remain. Invalid generates near-miss invalid inputs: single
// mutations of valid inputs that the parser rejects. Seed adds generated inputs to the seed corpus of a
// fuzz test.
package sample

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ornew/pego"
	"github.com/ornew/pego/grammar"
)

// Defaults of the options.
const (
	DefaultMaxDepth  = 5
	DefaultMaxRepeat = 3
	DefaultMaxLen    = 512
	DefaultAttempts  = 100
	DefaultBudget    = 20000
	defaultRetry     = 4096
)

type config struct {
	seed      uint64
	maxDepth  int
	maxRepeat int
	maxLen    int
	attempts  int
	budget    int
	retry     int
	coverage  bool
}

// Option configures a Generator.
type Option func(*config)

// WithSeed sets the seed of the random decisions (default 0). The same parser, options and seed give
// the same inputs.
func WithSeed(seed uint64) Option { return func(c *config) { c.seed = seed } }

// WithMaxDepth bounds recursion: once rule calls are nested d deep inside calls of the same rules, the
// generator prefers the shortest way to finish (default DefaultMaxDepth). Calls of rules that are not
// already active do not count. The bound is soft: when the input requires deeper nesting (a predicate
// that counts it, for example), the search goes deeper, but never beyond d + 8: raise d for inputs that
// must nest deeper.
func WithMaxDepth(d int) Option { return func(c *config) { c.maxDepth = max(d, 0) } }

// WithMaxRepeat sets how many iterations an unbounded repetition aims for beyond its minimum, at most
// (default DefaultMaxRepeat). It also bounds the operators of a Pratt expression chain. The search may
// take more iterations when the input requires them (a predicate after the repetition wants a longer
// match), but never more than 8 × n + 4 beyond the minimum (28 by default): raise n for inputs with
// longer repetitions.
func WithMaxRepeat(n int) Option { return func(c *config) { c.maxRepeat = max(n, 0) } }

// WithMaxLen sets a soft limit on the length of an input in bytes (default DefaultMaxLen): once the input
// is that long, the generator takes the shortest way to finish it.
func WithMaxLen(n int) Option { return func(c *config) { c.maxLen = max(n, 0) } }

// WithBudget sets the number of steps one attempt may take before it gives up (default DefaultBudget).
// A step is a parsing expression generated; checks have a budget of their own in proportion. Past
// WithMaxDepth and WithMaxLen, the generator only prefers the shortest way to finish, so the budget and
// the limits on nesting and iterations (see WithMaxDepth and WithMaxRepeat) are what bound an attempt. Inputs that need many steps (an
// expression repeated 25,000 times) need a larger budget; the budget also bounds the stack the search
// uses, which grows with the steps of an attempt.
func WithBudget(steps int) Option { return func(c *config) { c.budget = max(steps, 1) } }

// WithAttempts sets how many candidates the generator tries for one input before giving up (default
// DefaultAttempts).
func WithAttempts(n int) Option { return func(c *config) { c.attempts = max(n, 1) } }

// WithCoverage biases the generator toward rules, alternatives of ordered choices, and Pratt operands
// and operators that no accepted input has exercised yet.
func WithCoverage() Option { return func(c *config) { c.coverage = true } }

// ErrNoInput is returned when the generator finds no input within its attempts.
var ErrNoInput = errors.New("sample: no input found that the parser accepts")

// Generator generates inputs for a parser. It is not safe for concurrent use.
type Generator struct {
	p     *pego.Parser
	cfg   config
	in    *info
	g     *gen
	stats Stats
}

// Stats counts the work of a Generator.
type Stats struct {
	Candidates int // candidates passed to the parser
	Rejected   int // candidates that the parser rejected
	Failed     int // searches that ended without a candidate (budget exhausted or no way found)
}

// New returns a generator of inputs for p, starting at p's start rule. The parser must have its grammar
// AST (a parser loaded from a .pegoc saved without it cannot be sampled).
func New(p *pego.Parser, opts ...Option) (*Generator, error) {
	g := p.Grammar()
	if g == nil {
		return nil, errors.New("sample: the parser has no grammar AST")
	}
	cfg := config{
		maxDepth:  DefaultMaxDepth,
		maxRepeat: DefaultMaxRepeat,
		maxLen:    DefaultMaxLen,
		attempts:  DefaultAttempts,
		budget:    DefaultBudget,
		retry:     defaultRetry,
	}
	for _, o := range opts {
		o(&cfg)
	}
	in := analyze(g, p.Start())
	if in.start == nil {
		return nil, fmt.Errorf("sample: start rule %s is not defined", p.Start())
	}
	if in.start.height >= inf {
		return nil, fmt.Errorf("sample: start rule %s can never match", p.Start())
	}
	gen := &Generator{p: p, cfg: cfg, in: in}
	gen.g = newGen(in, &gen.cfg)
	return gen, nil
}

// Next returns an input that the parser accepts. Inputs may repeat. It returns ErrNoInput if no
// candidate was accepted within the attempts.
func (g *Generator) Next() (string, error) {
	for range g.cfg.attempts {
		s, trail, ok := g.g.attempt()
		if !ok {
			g.stats.Failed++
			g.g.failed()
			continue
		}
		g.stats.Candidates++
		if _, err := g.p.Parse(s); err != nil {
			g.stats.Rejected++
			g.g.failed()
			continue
		}
		g.g.accepted(trail)
		return s, nil
	}
	return "", ErrNoInput
}

// Generate returns up to n distinct inputs that the parser accepts. It returns fewer if it cannot find
// more (a grammar may accept only a few inputs; it gives up after as many inputs in a row that it already
// returned as the attempts per input), and ErrNoInput if it finds none.
func (g *Generator) Generate(n int) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for dups := 0; len(out) < n && dups < g.cfg.attempts; {
		s, err := g.Next()
		if err != nil {
			break
		}
		if seen[s] {
			dups++
			continue
		}
		seen[s] = true
		dups = 0
		out = append(out, s)
	}
	if len(out) == 0 && n > 0 {
		return nil, ErrNoInput
	}
	return out, nil
}

// Stats returns the counts of the work done so far.
func (g *Generator) Stats() Stats { return g.stats }

// Generate returns up to n distinct inputs that p accepts (see Generator.Generate).
func Generate(p *pego.Parser, n int, opts ...Option) ([]string, error) {
	g, err := New(p, opts...)
	if err != nil {
		return nil, err
	}
	return g.Generate(n)
}

// Coverage reports which parts of the grammar the accepted inputs exercised.
//
// Coverage is measured on the way the generator derived each accepted input. The parser takes the same
// way unless the generator could not evaluate a lookahead, a predicate or an earlier alternative of a
// choice (see the package documentation); counts are therefore close to, but not guaranteed to be, the
// parser's own coverage.
type Coverage struct {
	// Rules counts the rules that the start rule can reach and that can match; RulesCovered those that
	// an accepted input exercised.
	Rules        int `json:"rules"`
	RulesCovered int `json:"rulesCovered"`
	// Alternatives counts the alternatives of ordered choices and the operands and operators of Pratt
	// expressions in those rules, leaving out those that can never match or that occur only inside an
	// expression that can never match.
	Alternatives        int `json:"alternatives"`
	AlternativesCovered int `json:"alternativesCovered"`
	// MissedRules and MissedAlternatives list what was not exercised.
	MissedRules        []string      `json:"missedRules,omitempty"`
	MissedAlternatives []Alternative `json:"missedAlternatives,omitempty"`
	// Unreachable lists the rules that generation cannot exercise from the start rule: rules that are
	// not called, or called only inside negative lookaheads (!keyword), the skip of #recover, or
	// expressions that can never match. Impossible lists the rules that the start rule calls (outside
	// negative lookaheads and #recover) but that can never match, such as rules that end in _|_ to
	// report an error. Neither is counted in Rules.
	Unreachable []string `json:"unreachable,omitempty"`
	Impossible  []string `json:"impossible,omitempty"`
}

// Alternative identifies an alternative of an ordered choice, or an operand or operator of a Pratt
// expression.
type Alternative struct {
	Rule string `json:"rule"`
	// Kind is "choice", "operand" or "operator".
	Kind string `json:"kind"`
	// Index is the position of the alternative in its choice, or of the operand or operator in the
	// Pratt expression (operators are counted across all levels).
	Index int `json:"index"`
	// Expr is the alternative in PEGO syntax.
	Expr string `json:"expr"`
}

func (a Alternative) String() string {
	return fmt.Sprintf("%s: %s %d: %s", a.Rule, a.Kind, a.Index, a.Expr)
}

// Coverage returns the coverage of the inputs accepted so far.
func (g *Generator) Coverage() Coverage {
	in := g.in
	var c Coverage
	for _, ri := range in.order {
		switch {
		case ri.height >= inf && in.called[ri.index]:
			c.Impossible = append(c.Impossible, ri.def.Name)
		case !in.reachable[ri.index]:
			c.Unreachable = append(c.Unreachable, ri.def.Name)
		}
	}
	kinds := map[int]string{targetAlt: "choice", targetOperand: "operand", targetOperator: "operator"}
	for i, t := range in.targets {
		ri := in.rules[t.rule]
		if !in.reachable[ri.index] || ri.height >= inf || !t.possible {
			continue
		}
		hit := g.g.covered.has(i)
		if t.kind == targetRule {
			c.Rules++
			if hit {
				c.RulesCovered++
			} else {
				c.MissedRules = append(c.MissedRules, t.rule)
			}
			continue
		}
		c.Alternatives++
		if hit {
			c.AlternativesCovered++
		} else {
			c.MissedAlternatives = append(c.MissedAlternatives,
				Alternative{Rule: t.rule, Kind: kinds[t.kind], Index: t.index, Expr: grammar.FormatExpr(t.expr)})
		}
	}
	return c
}

// String summarizes the coverage on one line.
func (c Coverage) String() string {
	return fmt.Sprintf("rules %d/%d (%s), alternatives %d/%d (%s)",
		c.RulesCovered, c.Rules, percent(c.RulesCovered, c.Rules),
		c.AlternativesCovered, c.Alternatives, percent(c.AlternativesCovered, c.Alternatives))
}

// Report describes the coverage in detail, one item per line.
func (c Coverage) Report() string {
	var b strings.Builder
	b.WriteString(c.String() + "\n")
	for _, r := range c.MissedRules {
		fmt.Fprintf(&b, "missed rule: %s\n", r)
	}
	for _, a := range c.MissedAlternatives {
		fmt.Fprintf(&b, "missed %s\n", a)
	}
	if len(c.Unreachable) > 0 {
		fmt.Fprintf(&b, "unreachable: %s\n", strings.Join(c.Unreachable, ", "))
	}
	if len(c.Impossible) > 0 {
		fmt.Fprintf(&b, "never match: %s\n", strings.Join(c.Impossible, ", "))
	}
	return b.String()
}

func percent(a, b int) string {
	if b == 0 {
		return "100%"
	}
	return fmt.Sprintf("%.0f%%", 100*float64(a)/float64(b))
}
