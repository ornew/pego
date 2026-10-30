package compile

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ornew/pego/grammar"
)

// ComplexityClass is the class of the cost of a parse from a start rule (see "Classification" in
// spec/runtime/parsing.md). The classes are ordered by what the classifier knows: Linear and
// LinearEnv are guarantees, Unknown is no claim, and Poly and Exp are estimates that come with a
// witness.
type ComplexityClass int

const (
	// ClassLinear promises a cost of at most c · (n + 1).
	ClassLinear ComplexityClass = iota
	// ClassLinearEnv promises a cost of at most c · K · (L + 1) · (n + 1).
	ClassLinearEnv
	// ClassUnknown makes no claim: a condition for linear cost fails without a witness.
	ClassUnknown
	// ClassPoly estimates a cost that grows as n^Degree.
	ClassPoly
	// ClassExp estimates a cost that can double when n grows by a constant.
	ClassExp
)

func (c ComplexityClass) String() string {
	switch c {
	case ClassLinear:
		return "linear"
	case ClassLinearEnv:
		return "linear given an environment bound"
	case ClassUnknown:
		return "unknown"
	case ClassPoly:
		return "polynomial"
	case ClassExp:
		return "exponential"
	}
	return fmt.Sprintf("ComplexityClass(%d)", int(c))
}

// ComplexityCause is one cause of a polynomial or exponential class, at the scanning source,
// binding or operation that causes it, or one reason for the class unknown.
type ComplexityCause struct {
	Class ComplexityClass
	// Degree is k of a polynomial cause.
	Degree int
	// Rule is the rule that holds Pos.
	Rule string
	Pos  grammar.Pos
	// Message names the class and the witness (or, for the class unknown, the reason).
	Message string
}

// GrowingBinding is a binding of a string variable that joins the variable's own value at least
// twice and whose value reaches a later evaluation of the same binding (the growing-variable lint
// check).
type GrowingBinding struct {
	Rule string
	Pos  grammar.Pos
	// Var is the variable, and Text the binding as written.
	Var, Text string
}

// Complexity is the classification of the cost of a grammar from one start rule.
type Complexity struct {
	Class ComplexityClass
	// Degree is the largest degree of the polynomial causes.
	Degree int
	// Causes are the causes of the classes polynomial and exponential, sorted by position.
	Causes []ComplexityCause
	// Reasons are the reasons for the class unknown, sorted by position. A grammar of another
	// class can have them too; they are shown only when the classification is requested.
	Reasons []ComplexityCause
	// Env says why the class is at least linear given an environment bound, or is empty.
	Env []ComplexityCause
	// Growing are the bindings that the growing-variable check reports.
	Growing []GrowingBinding
	// Memoized are the rules that the classifier assumes the memo answers, which are the rules
	// that every parse memoizes (see TestComplexityMemoPolicy).
	Memoized map[string]bool
}

// ClassifyComplexity classifies the cost of g from the start rule start ("" selects the default
// start rule). g must compile and start must be one of its start rules; the classification of any
// other grammar is unspecified. It analyzes the grammar that the compiler runs: inline actions are
// lowered to hidden rules and attributes are normalized. A non-nil control polls for
// cancellation.
func ClassifyComplexity(g *grammar.Grammar, start string, control *Control) (*Complexity, error) {
	k, names, err := prepareClassifier(g, start, control)
	if err != nil {
		return nil, err
	}
	return k.classify().written(names), nil
}

// GrowingBindings returns the Growing member of the classification of g from start, without the
// rest of the classification, which costs more. It has the preconditions of ClassifyComplexity.
func GrowingBindings(g *grammar.Grammar, start string, control *Control) ([]GrowingBinding, error) {
	k, names, err := prepareClassifier(g, start, control)
	if err != nil {
		return nil, err
	}
	k.collect()
	c := &Complexity{Growing: k.growingBindings()}
	return c.written(names).Growing, nil
}

// prepareClassifier lowers and normalizes g as the compiler does and returns a classifier of it
// from start, with the names of the hidden rules of its inline actions.
func prepareClassifier(g *grammar.Grammar, start string, control *Control) (*classifier, InlineNames, error) {
	control.check()
	names := InlineNames{}
	if hasInlineAction(g, control) {
		lowered, errs := lowerInlineActions(g, names, control)
		if errs != nil {
			return nil, nil, errs
		}
		g = lowered
	}
	g, err := NormalizeAttributesControlled(g, false, control)
	if err != nil {
		return nil, nil, err
	}
	rules := control.rules(g)
	if start == "" {
		_, start, _ = resolveStarts(rules, control)
	}
	return newClassifier(g, rules, start, control), names, nil
}

// written describes the causes as the grammar is written: a cause in the hidden rule of an inline
// action is in the rule that holds it, and the messages name the inline action.
func (c *Complexity) written(names InlineNames) *Complexity {
	if len(names) == 0 {
		return c
	}
	rule := func(r string) string {
		if site, ok := names[r]; ok {
			return site.rule
		}
		return r
	}
	for _, cs := range [][]ComplexityCause{c.Causes, c.Reasons, c.Env} {
		for i := range cs {
			cs[i].Rule, cs[i].Message = rule(cs[i].Rule), names.Text(cs[i].Message)
		}
	}
	for i := range c.Growing {
		c.Growing[i].Rule = rule(c.Growing[i].Rule)
	}
	return c
}

// rhsLevel marks the synthetic call that stands for the right operand of an operator part of an
// Operators Rule. A right operand is parsed at a new position like a call, but it is not one: the
// memo does not answer it and it shares nothing.
const rhsLevel = "\x00right operand"

type classifier struct {
	control *Control
	g       *grammar.Grammar
	rules   []*grammar.RuleDef
	byName  map[string]*grammar.RuleDef
	start   string
	an      *Analysis

	reach     map[string]bool // rules that the start rule reaches
	calls     map[string][]string
	scc       map[string]int // the strongly connected component of the call graph
	recursive map[string]bool
	memo      map[string]bool
	vars      map[string][]string
	varType   map[string]string

	nullRule    map[string]bool
	firstRule   map[string]charset
	boundedRule map[string]bool
	reachFrom   map[string]map[string]bool

	sites           []*site
	units           []*unit
	local           map[string]*source // the first scanning source in the body or action of a rule
	ruleSrc         map[string]*source // the first scanning source that a rule contains, through calls
	follow          map[string]charset
	growing         map[int]map[string]charset // FOLLOW without the seed reads of a cycle, by cycle
	binds           []*binding
	sourceIndex     map[sourceKey]*source
	predicateSource map[*grammar.Predicate]*source
	repeatedRules   map[string]string
	includesMemo    map[sizeKey]bool
	srcExprs        map[grammar.Expr]bool // the expressions that are scanning sources
	failRule        map[string]bool       // rules that can fail after a scanning source examined input
	srcMemo         map[grammar.Expr]bool
	captures        map[string]map[string][]grammar.Expr // captured expressions by rule and name

	causes, reasons, env []ComplexityCause
}

func newClassifier(g *grammar.Grammar, rules []*grammar.RuleDef, start string, control *Control) *classifier {
	k := &classifier{control: control, g: g, rules: rules, start: start, byName: map[string]*grammar.RuleDef{},
		calls: map[string][]string{}, scc: map[string]int{}, recursive: map[string]bool{}, memo: map[string]bool{},
		varType: map[string]string{}, nullRule: map[string]bool{}, firstRule: map[string]charset{},
		boundedRule: map[string]bool{}, reachFrom: map[string]map[string]bool{}, local: map[string]*source{},
		ruleSrc: map[string]*source{}, follow: map[string]charset{}, growing: map[int]map[string]charset{},
		captures: map[string]map[string][]grammar.Expr{}, sourceIndex: map[sourceKey]*source{},
		predicateSource: map[*grammar.Predicate]*source{}, includesMemo: map[sizeKey]bool{},
		srcExprs: map[grammar.Expr]bool{}, srcMemo: map[grammar.Expr]bool{}}
	for _, r := range rules {
		k.byName[r.Name] = r
	}
	k.an = AnalyzeControlled(rules, control)
	k.vars = ruleVariablesControlled(rules, control)
	k.memo = memoizedRules(rules, k.an, control)
	for _, r := range rules {
		k.calls[r.Name] = allCallsControlled(r.Expr, nil, control)
	}
	for i, comp := range Tarjan(rules, k.calls, control) {
		for _, n := range comp {
			k.scc[n] = i
			if len(comp) > 1 || slices.Contains(k.calls[n], n) {
				k.recursive[n] = true
			}
		}
	}
	k.reach = k.reachable(start)
	for _, v := range g.Vars() {
		k.varType[v.Name] = varTypeName(v)
	}
	return k
}

// memoizedRules returns the rules whose results every parse memoizes: the leaders of
// left-recursion cycles, and the rules that are neither on a cycle nor transient. A rule that
// contains #missing, directly or through its callees, is not memoized when missing tokens are
// enabled, so it counts as not memoized unless it is a leader.
func memoizedRules(rules []*grammar.RuleDef, an *Analysis, control *Control) map[string]bool {
	transient := transientRules(rules, control)
	missing := missingRules(rules, control)
	out := map[string]bool{}
	for _, r := range rules {
		out[r.Name] = an.leaders[r.Name] || !an.cyclic[r.Name] && !transient[r.Name] && !missing[r.Name]
	}
	return out
}

// missingRules returns the rules that contain #missing, directly or through their callees.
func missingRules(rules []*grammar.RuleDef, control *Control) map[string]bool {
	out := map[string]bool{}
	calls := map[string][]string{}
	for _, r := range rules {
		calls[r.Name] = allCallsControlled(r.Expr, nil, control)
		walkExprControlled(r.Expr, func(e grammar.Expr) {
			if a, ok := e.(*grammar.Attributed); ok && hasPragma(a, "missing") {
				out[r.Name] = true
			}
		}, control)
	}
	propagate(rules, calls, out, control)
	return out
}

func varTypeName(v *grammar.VarDef) string {
	if t, ok := v.Type.(*grammar.TypeRef); ok {
		return t.Name
	}
	switch v.Value.(type) {
	case *grammar.StringLit:
		return "string"
	case *grammar.IntLit:
		return "int"
	case *grammar.BoolLit:
		return "bool"
	}
	return ""
}

func (k *classifier) reachable(from string) map[string]bool {
	if m := k.reachFrom[from]; m != nil {
		return m
	}
	seen := map[string]bool{}
	var visit func(n string)
	visit = func(n string) {
		if seen[n] || k.byName[n] == nil {
			return
		}
		seen[n] = true
		for _, c := range k.calls[n] {
			k.control.poll()
			visit(c)
		}
	}
	visit(from)
	k.reachFrom[from] = seen
	return seen
}

// shareable reports whether a call of the rule is answered alike in a unit and its continuation:
// the rule reads no variable and is memoized or calls no rule.
func (k *classifier) shareable(r *grammar.Ref) bool {
	return r.Level != rhsLevel && len(k.vars[r.Name]) == 0 && (k.memo[r.Name] || len(k.calls[r.Name]) == 0)
}

// sameCycle reports whether a and b are rules of one left-recursion cycle.
func (k *classifier) sameCycle(a, b string) bool {
	return k.an.cyclic[a] && k.an.cyclic[b] && k.an.cycle[a] == k.an.cycle[b]
}

func (k *classifier) classify() *Complexity {
	k.collect()
	k.computeRuleSources()
	k.computeFailRules()
	k.computeFollow()
	k.checkUnits()
	k.checkGrowth()
	k.checkNesting()
	k.checkValues()
	return k.result()
}

// collect collects the units, sources and bindings of the rules that the start rule reaches.
func (k *classifier) collect() {
	k.computeBounded()
	k.computeFirst()
	for _, r := range k.rules {
		if k.reach[r.Name] {
			k.walkRule(r)
		}
	}
}

func (k *classifier) result() *Complexity {
	c := &Complexity{Memoized: k.memo}
	sortCauses := func(cs []ComplexityCause) []ComplexityCause {
		slices.SortStableFunc(cs, func(a, b ComplexityCause) int {
			if a.Pos.Line != b.Pos.Line {
				return a.Pos.Line - b.Pos.Line
			}
			if a.Pos.Col != b.Pos.Col {
				return a.Pos.Col - b.Pos.Col
			}
			return strings.Compare(a.Message, b.Message)
		})
		return slices.CompactFunc(cs, func(a, b ComplexityCause) bool { return a == b })
	}
	c.Causes, c.Reasons, c.Env = sortCauses(k.causes), sortCauses(k.reasons), sortCauses(k.env)
	c.Growing = k.growingBindings()
	switch {
	case slices.ContainsFunc(c.Causes, func(x ComplexityCause) bool { return x.Class == ClassExp }):
		c.Class = ClassExp
	case len(c.Causes) > 0:
		c.Class = ClassPoly
	case len(c.Reasons) > 0:
		c.Class = ClassUnknown
	case len(c.Env) > 0:
		c.Class = ClassLinearEnv
	}
	for _, x := range c.Causes {
		if x.Class == ClassPoly {
			c.Degree = max(c.Degree, x.Degree)
		}
	}
	return c
}

// --- Positions and descriptions ---

func exprPos(e grammar.Expr) grammar.Pos {
	if s, ok := e.(interface{ Source() *grammar.SourceInfo }); ok && s.Source() != nil {
		return s.Source().Diagnostic()
	}
	return grammar.Pos{}
}

func termPosOf(t grammar.Term) grammar.Pos {
	if s, ok := t.(interface{ Source() *grammar.SourceInfo }); ok && s.Source() != nil {
		return s.Source().Diagnostic()
	}
	return grammar.Pos{}
}

// at describes a position in a rule, as "3:12 in rule A", or "rule A" without a position.
func at(rule string, p grammar.Pos) string {
	if p.IsValid() {
		return fmt.Sprintf("%d:%d in rule %s", p.Line, p.Col, rule)
	}
	return "rule " + rule
}

func (k *classifier) show(e grammar.Expr) string {
	s := grammar.FormatExpr(e)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 40 {
		s = s[:37] + "..."
	}
	return s
}

func (k *classifier) showTerm(t grammar.Term) string {
	s := grammar.FormatTerm(t)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 40 {
		s = s[:37] + "..."
	}
	return s
}

func (k *classifier) cause(class ComplexityClass, degree int, rule string, pos grammar.Pos, format string, args ...any) {
	k.causes = append(k.causes, ComplexityCause{Class: class, Degree: degree, Rule: rule, Pos: pos, Message: fmt.Sprintf(format, args...)})
}

func (k *classifier) reason(rule string, pos grammar.Pos, format string, args ...any) {
	k.reasons = append(k.reasons, ComplexityCause{Class: ClassUnknown, Rule: rule, Pos: pos, Message: fmt.Sprintf(format, args...)})
}

func (k *classifier) envCause(rule string, pos grammar.Pos, format string, args ...any) {
	k.env = append(k.env, ComplexityCause{Class: ClassLinearEnv, Rule: rule, Pos: pos, Message: fmt.Sprintf(format, args...)})
}

func degreeText(class ComplexityClass, degree int) string {
	if class == ClassExp {
		return "the cost can grow exponentially"
	}
	return fmt.Sprintf("the cost can grow as n^%d", degree)
}
