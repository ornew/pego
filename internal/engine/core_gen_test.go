package engine

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/engine/core"
)

// The tests here compare the Core IR of the typed Go rules, after the passes of typedCore, with
// the typed decisions frozen in testdata/typed_decisions.golden (core_typed_decisions_test.go),
// which the generator recorded before its typed output was printed from Flow IR.

// coreGenMode is a set of generator options under which the Core passes are compared.
type coreGenMode struct {
	name string
	opts GenOptions
}

var coreGenModes = []coreGenMode{
	{"lean", GenOptions{Types: true, TypedOnly: true}},
	{"lean observable", GenOptions{Types: true, TypedOnly: true, Observability: true}},
	{"controlled", GenOptions{Types: true, TypedOnly: true, WorkControls: true}},
	{"lean boxed", GenOptions{Types: true, TypedOnly: true, disableLiteralTextDispatch: true, boxedComparisons: true}},
}

// coreGenCase is a corpus grammar generated with recorded decisions, and the Core IR of its typed
// rules under the generator's options, with the expression each node is built from.
type coreGenCase struct {
	name string
	// table is the generator's rule table the module is compared with.
	table  string
	dec    *genDecisions
	m      *core.Module
	origin map[core.Node]grammar.Expr
	// rules holds the rules of the program by ID, value-free twins included.
	rules []*rule
	// passErrs holds the violations Verify found after a pass.
	passErrs []string
}

func (c *coreGenCase) g() *generator { return c.dec.gen }

// altOrigin returns the expression of the alternative i of the choice x, which the generator's
// guards are keyed by: the alternative of the grammar choice x is built from. (The origin of an
// alternative's body is the innermost expression it is built from: inside a capture that builds
// nothing, for one.)
func (c *coreGenCase) altOrigin(x *core.Choice, i int) (grammar.Expr, bool) {
	if ce, ok := c.origin[x].(*grammar.Choice); ok && len(ce.Alts) == len(x.Alts) {
		return ce.Alts[i], true
	}
	body := x.Alts[i].Body
	if t, ok := body.(*core.Terminal); ok {
		body = t.Body
	}
	e, ok := c.origin[body]
	return e, ok
}

// coreGenerate generates the parser of the grammar source with the options, recording the
// decisions of the generator. It reports false for a grammar that the generator rejects.
func coreGenerate(src string, opts GenOptions) (*genDecisions, bool) {
	g, err := parseTest(src)
	if err != nil {
		return nil, false
	}
	dec := &genDecisions{}
	opts.Module, opts.decisions = "example.com/p", dec
	if _, err := generate(g, opts); err != nil || dec.gen == nil {
		return nil, false
	}
	return dec, true
}

// coreTypedCase builds the Core IR of the typed rules of the generated parser dec with the passes
// of typedCore, verifying the module after each pass and following the repetitions the scan pass
// replaces, so that every scan has the origin of its repetition. If breaks is not nil, it is
// called after each pass, before the module is verified: a negative control breaks a pass there.
func coreTypedCase(name string, dec *genDecisions, breaks func(c *coreGenCase, pass string, m *core.Module)) (*coreGenCase, error) {
	c := &coreGenCase{name: name, table: "trules", dec: dec, origin: map[core.Node]grammar.Expr{}}
	prog := dec.gen.prog
	c.rules = append(append([]*rule(nil), prog.rules...), prog.twins...)
	var repeats []func()
	dec.coreHook = func(pass string, m *core.Module) {
		if breaks != nil {
			breaks(c, pass, m)
		}
		core.Analyze(m)
		if err := core.Verify(m); err != nil {
			c.passErrs = append(c.passErrs, fmt.Sprintf("after %s: %v", pass, err))
		}
		switch pass {
		case "guards":
			repeats = followRepeats(m, c.origin)
		case "scans":
			for _, f := range repeats {
				f()
			}
		}
	}
	defer func() { dec.coreHook = nil }()
	m, err := dec.gen.typedCore(c.origin)
	if err != nil {
		return nil, err
	}
	c.m = m
	return c, nil
}

// coreGenerated holds the corpus grammars generated in each mode, by mode name.
var coreGenerated = map[string][]coreGenerated1{}

type coreGenerated1 struct {
	name string
	dec  *genDecisions
}

// coreExtraGrammars are grammars with term forms that the typed rules of the corpus lack: text
// comparisons of two captures and comparisons of fields.
var coreExtraGrammars = []genCase{
	{name: "text equality", src: `rule main = a:[a-z]+ "=" b:[a-z]+ => text($a) != text($b) || text($a) == text($b)
`},
	{name: "field comparison", src: `type P = { N int; S string }
type R = { A bool; B bool; C bool }
rule main R = p:pair q:pair => R{A: $p.N == 1, B: $p.S != "x", C: $p.N < $q.N}
rule pair P = n:[0-9]+ s:[a-z] => P{N: len($n), S: text($s)}
`},
}

// coreGenerations generates every corpus grammar in the mode, once for all tests.
func coreGenerations(t *testing.T, mode coreGenMode) []coreGenerated1 {
	t.Helper()
	if out, ok := coreGenerated[mode.name]; ok {
		return out
	}
	// The grammars are those of the golden file in this mode, so a grammar whose typed decisions
	// are not recorded cannot drop out of the comparisons silently.
	want := typedDecisionsGrammarsOf(t, mode.name)
	var out []coreGenerated1
	for _, gc := range append(schemaCorpus(t), coreExtraGrammars...) {
		if !want[gc.name] {
			continue
		}
		dec, ok := coreGenerate(gc.src, mode.opts)
		if !ok {
			t.Fatalf("%s %s: the grammar does not generate", mode.name, gc.name)
		}
		loadTypedDecisions(t, mode.name, gc.name, dec)
		out = append(out, coreGenerated1{gc.name, dec})
		delete(want, gc.name)
	}
	if len(want) != 0 {
		t.Fatalf("%s: grammars of %s are not in the corpus: %v", mode.name, typedDecisionsGolden, want)
	}
	if len(out) < 100 {
		t.Fatalf("%s: %d grammars", mode.name, len(out))
	}
	coreGenerated[mode.name] = out
	return out
}

// writesTyped reports that the generator recorded typed decisions for the grammar. It is the filter
// of the writer of the golden file (core_typed_decisions_test.go): the Core comparisons take their
// grammars from the golden, because Flow IR prints some typed rules without recording them.
func writesTyped(dec *genDecisions) bool {
	for _, d := range dec.log {
		if d.table == "trules" {
			return true
		}
	}
	return false
}

// coreGenCases builds the Core IR of every corpus grammar generated in the mode, breaking the
// passes with breaks if it is not nil (coreTypedCase).
func coreGenCases(t *testing.T, mode coreGenMode, breaks func(c *coreGenCase, pass string, m *core.Module)) []*coreGenCase {
	t.Helper()
	var out []*coreGenCase
	for _, gen := range coreGenerations(t, mode) {
		c, err := coreTypedCase(gen.name, gen.dec, breaks)
		if err != nil {
			t.Fatalf("%s: %s: %v", mode.name, gen.name, err)
		}
		out = append(out, c)
	}
	return out
}

// genKey identifies the decisions of one kind for an expression of a rule.
type genKey struct {
	rule *rule
	kind genDecisionKind
	e    grammar.Expr
}

// genIndex returns the decisions of the table by rule, kind and expression. Decisions recorded
// more than once for the same key must agree; it reports those that do not.
func genIndex(dec *genDecisions, table string) (map[genKey]genDecision, []string) {
	idx := map[genKey]genDecision{}
	var errs []string
	for _, d := range dec.log {
		if d.table != table {
			continue
		}
		k := genKey{d.rule, d.kind, d.e}
		if prev, ok := idx[k]; ok && fmt.Sprint(prev.guard, prev.runs, prev.names) != fmt.Sprint(d.guard, d.runs, d.names) {
			errs = append(errs, fmt.Sprintf("rule %s: %s: decisions %+v and %+v", d.rule.name, grammar.FormatExpr(d.e), prev, d))
		}
		idx[k] = d
	}
	return idx, errs
}

// coreNodes calls f for every node of the rule with the alternative and the line it is in.
func coreNodes(r *core.Rule, f func(n core.Node)) { core.Walk(r.Body, f) }

// opLines returns the lines of an Operators Rule with their grammar lines, in order: the skip
// line, the operands and the operators.
func opLines(o *core.OperatorsRule, e *grammar.OperatorsRule) ([]*core.OpLine, []grammar.Expr, []grammar.Term) {
	var exprs []grammar.Expr
	var actions []grammar.Term
	if e.Skip != nil {
		exprs, actions = append(exprs, e.Skip), append(actions, nil)
	}
	for _, x := range e.Operands {
		exprs, actions = append(exprs, x.Expr), append(actions, x.Action)
	}
	for _, l := range e.Levels {
		for _, x := range l.Operators {
			exprs, actions = append(exprs, x.Expr), append(actions, x.Action)
		}
	}
	return o.Lines(), exprs, actions
}

// guardString is the guard of a Core alternative in the generator's terms: the condition on the
// next character and the expectation.
func guardString(g *generator, m *core.Module, f *core.First) (string, string) {
	if f.Lit {
		return fmt.Sprintf("ch == %d", f.Rune), m.Exps[f.Exp]
	}
	e := &grammar.CharClass{Negated: f.Class.Negated}
	for _, r := range f.Class.Ranges {
		e.Ranges = append(e.Ranges, grammar.CharRange{Lo: r.Lo, Hi: r.Hi})
	}
	return negateCondition(g.controlledClassReject(e)), m.Exps[f.Exp]
}

// coreDecisionCounts counts the decisions compared.
type coreDecisionCounts struct {
	terminals, guards, guardedClasses, classRuns, scans, scopes, terms int
	// forms counts the special forms of the terms compared.
	forms map[string]int
}

// compareCoreDecisions compares the Core IR of c with the decisions of the generator for its table
// and returns the differences, each prefixed with its aspect (terminal, guard, class runs,
// scan, scope, term; verify for a pass after which Verify fails, generator for decisions of the
// generator that disagree):
//
//   - the Terminals are the expressions the generator runs as terminal programs;
//   - the guard of every alternative is the generator's, and GuardedClass is set exactly where
//     the generator's guard consumes the alternative's first class (unless it runs the
//     alternative as a terminal program, which reads its characters itself);
//   - the class runs of every value-free sequence are the generator's;
//   - the scans are the repetitions the generator writes as scans;
//   - the scopes of repetition elements and Operators Rule lines are the generator's;
//   - every action, predicate and binding has the structure and forms the generator writes.
//
// Nodes built from expressions the generator does not write (alternatives after one that cannot
// fail) are not compared; every decision of the generator is.
func compareCoreDecisions(c *coreGenCase, n *coreDecisionCounts) []string {
	g := c.g()
	idx, errs := genIndex(c.dec, c.table)
	for _, e := range c.passErrs {
		errs = append(errs, "verify: "+e)
	}
	seen := map[genKey]bool{}
	written := func(r *rule, e grammar.Expr) bool {
		_, ok := idx[genKey{r, decExpr, e}]
		return ok
	}
	lookup := func(r *rule, kind genDecisionKind, e grammar.Expr) (genDecision, bool) {
		k := genKey{r, kind, e}
		d, ok := idx[k]
		if ok {
			seen[k] = true
		}
		return d, ok
	}
	for _, cr := range c.m.Rules {
		r := c.rules[cr.ID]
		where := func(aspect, format string, args ...any) {
			errs = append(errs, fmt.Sprintf("%s: rule %s: ", aspect, r.name)+fmt.Sprintf(format, args...))
		}
		coreNodes(cr, func(x core.Node) {
			switch x := x.(type) {
			case *core.Terminal:
				e := c.origin[x.Body]
				if _, ok := lookup(r, decTerminal, e); !ok {
					where("terminal", "terminal %s, generator none", grammar.FormatExpr(e))
				}
				n.terminals++
			case *core.Choice:
				for i, a := range x.Alts {
					e, ok := c.altOrigin(x, i)
					if !ok {
						where("guard", "alternative %d without origin", i)
						continue
					}
					d, ok := lookup(r, decGuard, e)
					if !ok {
						if written(r, e) {
							where("guard", "alternative %s: generator records no guard", grammar.FormatExpr(e))
						}
						continue
					}
					if (a.Guard != nil) != d.guard.ok {
						where("guard", "alternative %s: guard %v, generator %v", grammar.FormatExpr(e), a.Guard != nil, d.guard.ok)
						continue
					}
					_, terminal := lookup(r, decTerminal, e)
					if want := d.guard.class && !terminal; a.GuardedClass != want {
						where("guard", "alternative %s: guarded class %v, generator %v", grammar.FormatExpr(e), a.GuardedClass, want)
					}
					if a.Guard == nil {
						continue
					}
					cond, desc := guardString(g, c.m, a.Guard)
					if cond != d.guard.cond || desc != g.descs[d.guard.desc] || a.Guard.Depth != d.guard.depth {
						where("guard", "alternative %s: guard %q %q +%d, generator %q %q +%d", grammar.FormatExpr(e), cond, desc, a.Guard.Depth, d.guard.cond, g.descs[d.guard.desc], d.guard.depth)
					}
					n.guards++
					if a.GuardedClass {
						n.guardedClasses++
					}
				}
			case *core.Seq:
				e, ok := c.origin[x].(*grammar.Seq)
				if !ok || x.Attrs().Want {
					return
				}
				var got [][2]int
				at := 0
				for _, it := range x.Items {
					if run, ok := it.(*core.ClassRun); ok {
						got = append(got, [2]int{at, len(run.Classes)})
						at += len(run.Classes)
						n.classRuns++
						continue
					}
					at++
				}
				d, ok := lookup(r, decClassRuns, e)
				if !ok {
					if len(got) > 0 && written(r, e) {
						where("class runs", "class runs %v in %s, generator none", got, grammar.FormatExpr(e))
					}
					return
				}
				if at != len(e.Items) || !slices.Equal(got, d.runs) {
					where("class runs", "class runs %v in %s, generator %v", got, grammar.FormatExpr(e), d.runs)
				}
			case *core.Scan:
				e, ok := c.origin[x]
				if !ok {
					where("scan", "scan without origin")
					return
				}
				if _, ok := lookup(r, decScan, e); !ok && written(r, e) {
					where("scan", "scan %s, generator none", grammar.FormatExpr(e))
				}
				n.scans++
			case *core.Repeat:
				e, ok := c.origin[x].(*grammar.Repeat)
				if !ok || x.Scope == nil {
					return
				}
				d, ok := lookup(r, decScope, e.Expr)
				if !ok {
					if written(r, e) {
						where("scope", "element scope of %s: generator none", grammar.FormatExpr(e))
					}
					return
				}
				if got := core.SlotNames(x.Scope); !slices.Equal(got, d.names) && !(len(got) == 0 && len(d.names) == 0) {
					where("scope", "element scope of %s: %v, generator %v", grammar.FormatExpr(e), got, d.names)
				}
				n.scopes++
			case *core.OperatorsRule:
				lines, exprs, _ := opLines(x, r.def.Expr.(*grammar.OperatorsRule))
				for i, l := range lines {
					d, ok := lookup(r, decScope, exprs[i])
					if !ok {
						where("scope", "line %s: generator records no scope", grammar.FormatExpr(exprs[i]))
						continue
					}
					if got := core.SlotNames(l.Scope); !slices.Equal(got, d.names) && !(len(got) == 0 && len(d.names) == 0) {
						where("scope", "line %s: scope %v, generator %v", grammar.FormatExpr(exprs[i]), got, d.names)
					}
					n.scopes++
				}
			}
		})
	}
	errs = append(errs, compareCoreTerms(c, n)...)
	// Every decision of the generator is one of the Core IR's.
	for k, d := range idx {
		switch k.kind {
		case decExpr, decClassRuns, decGuard:
			if k.kind == decClassRuns && len(d.runs) > 0 && !seen[k] {
				errs = append(errs, fmt.Sprintf("class runs: rule %s: generator class runs %v in %s, Core none", k.rule.name, d.runs, grammar.FormatExpr(k.e)))
			}
			if k.kind == decGuard && !seen[k] && d.guard.ok {
				errs = append(errs, fmt.Sprintf("guard: rule %s: generator guard of %s, Core none", k.rule.name, grammar.FormatExpr(k.e)))
			}
		default:
			if !seen[k] {
				errs = append(errs, fmt.Sprintf("%s: rule %s: generator decision for %s, Core none", [...]string{decTerminal: "terminal", decScan: "scan", decScope: "scope"}[k.kind], k.rule.name, grammar.FormatExpr(k.e)))
			}
		}
	}
	slices.Sort(errs)
	return errs
}

// coreTermTree returns the structure and forms of a Core term as the generator records them
// (genTerm): a struct constructor is in declaration order only where the generator writes it so,
// in a parser without step charges.
func coreTermTree(t core.Term, noWork bool) *genTerm {
	n := &genTerm{}
	kids := core.TermChildren(t)
	switch t := t.(type) {
	case *core.Const:
		n.form = "const"
	case *core.SlotRef, *core.Local:
		n.form = "capture"
	case *core.VarRef:
		n.form = "var"
	case *core.Member:
		n.form = "member"
	case *core.New:
		n.form = "new"
		if noWork && t.InOrder {
			n.form = "new-in-order"
		}
	case *core.Convert:
		n.form = "convert"
	case *core.Tuple:
		n.form = "tuple"
	case *core.Builtin:
		n.form = "call:" + t.Func
	case *core.Lambda:
		n.form = "lambda"
	case *core.Binary:
		n.form = "binary:" + t.Op
	case *core.Unary:
		n.form = "unary:" + t.Op
	case *core.Cast:
		n.form = "cast"
	case *core.TextEq:
		n.form = "text-eq"
	case *core.FieldCompare:
		n.form = "field-cmp"
	case *core.TextIn:
		n.form = "text-in"
	case *core.ProjectedMap:
		n.form = "projected-map"
	case *core.ConcatFused:
		n.form = "concat-fused"
		kids = nil
		for _, p := range t.Parts {
			part := &genTerm{form: [...]string{core.PartList: "part-list", core.PartMap: "part-map", core.PartItems: "part-items"}[p.Kind]}
			for _, x := range p.Args {
				part.kids = append(part.kids, coreTermTree(x, noWork))
			}
			if p.X != nil {
				part.kids = append(part.kids, coreTermTree(p.X, noWork))
			}
			if p.Fn != nil {
				part.kids = append(part.kids, coreTermTree(p.Fn, noWork))
			}
			n.kids = append(n.kids, part)
		}
	default:
		n.form = fmt.Sprintf("%T", t)
	}
	for _, k := range kids {
		n.kids = append(n.kids, coreTermTree(k, noWork))
	}
	return n
}

func (n *genTerm) String() string {
	if len(n.kids) == 0 {
		return n.form
	}
	xs := make([]string, len(n.kids))
	for i, k := range n.kids {
		xs[i] = k.String()
	}
	return n.form + "(" + strings.Join(xs, " ") + ")"
}

// countForms adds the special forms of the tree to forms.
func countForms(n *genTerm, forms map[string]int) {
	switch n.form {
	case "text-eq", "field-cmp", "text-in", "projected-map", "concat-fused", "new-in-order", "part-items":
		forms[n.form]++
	}
	for _, k := range n.kids {
		countForms(k, forms)
	}
}

// compareCoreTerms compares every action, predicate and binding of the Core IR of c with the tree
// the generator records for it, and every term the generator records with one of the Core IR.
func compareCoreTerms(c *coreGenCase, n *coreDecisionCounts) []string {
	g := c.g()
	type termKey struct {
		rule *rule
		t    grammar.Term
	}
	gen := map[termKey]string{}
	for _, rec := range c.dec.terms {
		if rec.table != c.table {
			continue
		}
		k := termKey{rec.rule, rec.t}
		s := rec.tree.String()
		if prev, ok := gen[k]; ok && prev != s {
			return []string{fmt.Sprintf("generator: rule %s: term written as %s and %s", rec.rule.name, prev, s)}
		}
		gen[k] = s
	}
	idx, _ := genIndex(c.dec, c.table)
	var errs []string
	seen := map[termKey]bool{}
	compare := func(r *rule, e grammar.Expr, gt grammar.Term, ct core.Term) {
		k := termKey{r, gt}
		want, ok := gen[k]
		if !ok {
			if _, written := idx[genKey{r, decExpr, e}]; written || e == nil {
				errs = append(errs, fmt.Sprintf("term: rule %s: term %s, generator none", r.name, core.TermString(ct)))
			}
			return
		}
		seen[k] = true
		tree := coreTermTree(ct, g.noWork && c.table == "trules") // only the typed runtime takes fields in order
		if got := tree.String(); got != want {
			errs = append(errs, fmt.Sprintf("term: rule %s: term %s, generator %s", r.name, got, want))
		}
		if n.forms == nil {
			n.forms = map[string]int{}
		}
		countForms(tree, n.forms)
		n.terms++
	}
	for _, cr := range c.m.Rules {
		r := c.rules[cr.ID]
		if cr.Finish.Kind == core.FinishAction {
			compare(r, nil, r.action, cr.Finish.Action)
		}
		coreNodes(cr, func(x core.Node) {
			switch x := x.(type) {
			case *core.Pred:
				e := c.origin[x].(*grammar.Predicate)
				compare(r, e, e.Term, x.Term)
			case *core.BindNode:
				e := c.origin[x].(*grammar.Predicate)
				compare(r, e, e.Term.(*grammar.Bind).Value, x.Term)
			case *core.OperatorsRule:
				lines, _, actions := opLines(x, r.def.Expr.(*grammar.OperatorsRule))
				for i, l := range lines {
					if l.Action != nil {
						compare(r, nil, actions[i], l.Action)
					}
				}
			}
		})
	}
	for k := range gen {
		if !seen[k] {
			errs = append(errs, fmt.Sprintf("term: rule %s: generator term %s, Core none", k.rule.name, gen[k]))
		}
	}
	return errs
}

// coreAspectErrors returns the differences of the aspect among errs (compareCoreDecisions).
func coreAspectErrors(errs []string, aspect string) []string {
	var out []string
	for _, e := range errs {
		if strings.HasPrefix(e, aspect+": ") {
			out = append(out, e)
		}
	}
	return out
}

// TestCoreMatchesGenerator compares the Core IR passes with the generator over the corpus in each
// mode, in every aspect, and checks that every kind of decision is met.
func TestCoreMatchesGenerator(t *testing.T) {
	for _, mode := range coreGenModes {
		t.Run(mode.name, func(t *testing.T) {
			var n coreDecisionCounts
			for _, c := range coreGenCases(t, mode, nil) {
				for _, err := range compareCoreDecisions(c, &n) {
					t.Errorf("%s: %s", c.name, err)
				}
			}
			lean := !mode.opts.WorkControls
			terminals := lean && !mode.opts.Observability
			if n.guards == 0 || n.scans == 0 || n.scopes == 0 || n.terms == 0 ||
				(n.terminals > 0) != terminals || (n.classRuns > 0) != lean || (n.guardedClasses > 0) != lean {
				t.Errorf("counts %+v", n)
			}
			if mode.opts.boxedComparisons != (n.forms["field-cmp"] == 0) || mode.opts.disableLiteralTextDispatch != (n.forms["text-in"] == 0) ||
				n.forms["concat-fused"] == 0 || n.forms["projected-map"]+n.forms["part-items"] == 0 {
				t.Errorf("forms %v", n.forms)
			}
		})
	}
}
