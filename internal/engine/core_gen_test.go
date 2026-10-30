package engine

import (
	"fmt"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gen/gogen"
	"github.com/ornew/pego/internal/gentest"
	irbuild "github.com/ornew/pego/internal/ir/build"
	"github.com/ornew/pego/internal/ir/core"
)

// The tests here check the Core IR of the typed Go rules, after the passes of
// gogen.Generator.TypedCore, against the decisions frozen in testdata/typed_decisions.golden
// (core_typed_decisions_test.go).

// coreGenMode is a set of generator options under which the Core passes are frozen.
type coreGenMode struct {
	name string
	opts gen.GenOptions
}

var coreGenModes = []coreGenMode{
	{"lean", gen.GenOptions{Types: true, TypedOnly: true}},
	{"lean observable", gen.GenOptions{Types: true, TypedOnly: true, Observability: true}},
	{"controlled", gen.GenOptions{Types: true, TypedOnly: true, WorkControls: true}},
	{"lean boxed", gen.GenOptions{Types: true, TypedOnly: true, DisableLiteralTextDispatch: true, BoxedComparisons: true}},
}

// coreGenCase is a corpus grammar generated with the options of a mode, and the Core IR of its
// rules under them, with the expression each node is built from.
type coreGenCase struct {
	name, mode string
	// table is the generator's rule table of the module: trules for the typed rules, recRules for the
	// recognizer.
	table  string
	golden *decisionsGolden
	gen    *gogen.Generator
	m      *core.Module
	origin map[core.Node]grammar.Expr
	// rules holds the rules of the program by ID, value-free twins included.
	rules []*compile.Rule
	// passErrs holds the violations Verify found after a pass.
	passErrs []string
	// broken is set where a negative control broke a pass of this case.
	broken bool
	// lines holds the decisions of the Core IR (coreDecisionLines), once compared.
	lines []string
}

func (c *coreGenCase) g() *gogen.Generator { return c.gen }

// altOrigin returns the expression of the alternative i of the choice x, which the decisions are
// keyed by: the alternative of the grammar choice x is built from. (The origin of an alternative's
// body is the innermost expression it is built from: inside a capture that builds nothing, for
// one.)
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

// coreGenerate generates the parser of the grammar source with the options and returns the
// generator that wrote it, for the Core IR of the same options (gogen.Generator.TypedCore). It reports
// false for a grammar that the generator rejects.
func coreGenerate(src string, opts gen.GenOptions) (*gogen.Generator, bool) {
	g, err := parseTest(src)
	if err != nil {
		return nil, false
	}
	var gen *gogen.Generator
	h := &gogen.Hooks{Generator: func(x *gogen.Generator) { gen = x }}
	opts.Module = "example.com/p"
	if _, err := gogen.GenerateParser(g, compileForGenerator, opts, h); err != nil || gen == nil {
		return nil, false
	}
	return gen, true
}

// recognizerOf returns the recognizer of the program that the generator g prints, compiled as
// Program.Recognizer compiles it.
func recognizerOf(g *gogen.Generator) (*compile.Program, error) {
	rec, err := newProgram(g.Prog, nil).Recognizer()
	if err != nil {
		return nil, err
	}
	return rec.Static(), nil
}

// coreTypedCase builds the Core IR of the typed rules of the generated parser with the passes of
// gogen.Generator.TypedCore, verifying the module after each pass and following the repetitions
// the scan pass replaces, so that every scan has the origin of its repetition. If breaks is not nil, it is called
// after each pass, before the module is verified: a negative control breaks a pass there.
func coreTypedCase(name, mode string, gen *gogen.Generator, breaks func(c *coreGenCase, pass string, m *core.Module)) (*coreGenCase, error) {
	c := &coreGenCase{name: name, mode: mode, table: "trules", golden: typedGolden, gen: gen, origin: map[core.Node]grammar.Expr{}}
	c.rules = typedRules(gen)
	var repeats []func()
	gen.CoreHook = func(pass string, m *core.Module) {
		if breaks != nil {
			breaks(c, pass, m)
		}
		core.Analyze(m)
		if err := core.Verify(m); err != nil {
			c.passErrs = append(c.passErrs, fmt.Sprintf("after %s: %v", pass, err))
		}
		switch pass {
		case "guards":
			repeats = irbuild.FollowRepeats(m, c.origin)
		case "scans":
			for _, f := range repeats {
				f()
			}
		}
	}
	defer func() { gen.CoreHook = nil }()
	m, err := gen.TypedCore(c.origin)
	if err != nil {
		return nil, err
	}
	c.m = m
	return c, nil
}

// coreGenerated holds the corpus grammars generated in each mode, by mode name. The tests that read
// it change its generators (Generator.Table, Generator.CoreHook), so they do not run in parallel.
var coreGenerated = map[string][]coreGenerated1{}

type coreGenerated1 struct {
	name string
	gen  *gogen.Generator
}

// coreGenerations generates every corpus grammar in the mode, once for all tests: those that the
// generator accepts and whose typed rules have a Core IR. The grammars are generated concurrently,
// since the tests that read them run before the parallel tests and would otherwise wait for each.
func coreGenerations(t *testing.T, mode coreGenMode) []coreGenerated1 {
	t.Helper()
	if out, ok := coreGenerated[mode.name]; ok {
		return out
	}
	cases := append(gentest.CorpusWithParsers(t), gentest.CoreExtraGrammars...)
	gens := make([]*gogen.Generator, len(cases))
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	for i, gc := range cases {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			gen, ok := coreGenerate(gc.Src, mode.opts)
			if !ok {
				return
			}
			if m, err := gen.TypedCore(map[core.Node]grammar.Expr{}); err != nil || len(m.Rules) == 0 {
				return
			}
			gens[i] = gen
		})
	}
	wg.Wait()
	var out []coreGenerated1
	for i, gen := range gens {
		if gen != nil {
			out = append(out, coreGenerated1{cases[i].Name, gen})
		}
	}
	if len(out) < 100 {
		t.Fatalf("%s: %d grammars", mode.name, len(out))
	}
	coreGenerated[mode.name] = out
	return out
}

// coreGenCases builds the Core IR of every corpus grammar generated in the mode, breaking the
// passes with breaks if it is not nil (coreTypedCase).
func coreGenCases(t *testing.T, mode coreGenMode, breaks func(c *coreGenCase, pass string, m *core.Module)) []*coreGenCase {
	t.Helper()
	var out []*coreGenCase
	for _, gen := range coreGenerations(t, mode) {
		c, err := coreTypedCase(gen.name, mode.name, gen.gen, breaks)
		if err != nil {
			t.Fatalf("%s: %s: %v", mode.name, gen.name, err)
		}
		out = append(out, c)
	}
	return out
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

// workClassName matches the name of a work class table, whose number is the generator's to assign.
var workClassName = regexp.MustCompile(`workClass[0-9]+`)

// guardString is the guard of a Core alternative as a Go condition: the condition on the
// next character and the expectation. The numbers of work class tables are left out: they depend on
// the order in which the generator met the classes, and the ranges of the condition name the class.
func guardString(g *gogen.Generator, m *core.Module, f *core.First) (string, string) {
	if f.Lit {
		if f.Fold {
			return fmt.Sprintf("ch == %d || ch == %d", f.Rune, f.Rune-('a'-'A')), m.Exps[f.Exp]
		}
		return fmt.Sprintf("ch == %d", f.Rune), m.Exps[f.Exp]
	}
	e := &grammar.CharClass{Negated: f.Class.Negated}
	for _, r := range f.Class.Ranges {
		e.Ranges = append(e.Ranges, grammar.CharRange{Lo: r.Lo, Hi: r.Hi})
	}
	cond := gogen.NegateCondition(g.ControlledClassReject(e))
	return workClassName.ReplaceAllString(cond, "workClass"), m.Exps[f.Exp]
}

// coreDecisionCounts counts the decisions compared.
type coreDecisionCounts struct {
	terminals, guards, guardedClasses, classRuns, scans, scopes, terms int
	// forms counts the special forms of the terms compared.
	forms map[string]int
}

// termTree is a term's form and the terms inside it, in order.
type termTree struct {
	form string
	kids []*termTree
}

// coreDecisionLines encodes the decisions of the Core IR of c as the lines of the decisions
// golden (core_typed_decisions_test.go), sorted, with the guard conditions as quoted text, and
// counts them in n. It returns the problems of nodes it cannot encode, each prefixed with its
// aspect. The decisions are those of every node of the module:
//
//   - the Terminals;
//   - the guard of every alternative of a choice, and whether its first class is a guarded class;
//   - the class runs of every value-free sequence;
//   - the scans;
//   - the scopes of repetition elements and Operators Rule lines;
//   - for the typed rules, the structure and forms of every action, predicate and binding.
func coreDecisionLines(c *coreGenCase, n *coreDecisionCounts) (lines, problems []string) {
	g := c.g()
	for _, cr := range c.m.Rules {
		r := c.rules[cr.ID]
		_, idx := typedDecisionsWalk(r)
		problem := func(aspect, format string, args ...any) {
			problems = append(problems, fmt.Sprintf("%s: rule %s: ", aspect, r.Name)+fmt.Sprintf(format, args...))
		}
		// at returns the index of the expression in the rule's body.
		at := func(aspect string, e grammar.Expr) (int, bool) {
			i, ok := idx[e]
			if !ok {
				problem(aspect, "expression %s is not in the body", grammar.FormatExpr(e))
			}
			return i, ok
		}
		add := func(kind string, at int, format string, args ...any) {
			lines = append(lines, fmt.Sprintf("%s\t%d\t%d", kind, cr.ID, at)+fmt.Sprintf(format, args...))
		}
		names := func(s []core.Slot) string { return strings.Join(core.SlotNames(s), ",") }
		term := func(at int, t core.Term) {
			tree := coreTermTree(t, g.NoWork && c.table == "trules") // only the typed runtime takes fields in order
			if n.forms == nil {
				n.forms = map[string]int{}
			}
			countForms(tree, n.forms)
			add("term", at, "\t%s", tree)
			n.terms++
		}
		typed := c.table == "trules"
		if typed && cr.Finish.Kind == core.FinishAction {
			term(-1, cr.Finish.Action)
		}
		coreNodes(cr, func(x core.Node) {
			switch x := x.(type) {
			case *core.Terminal:
				e, ok := c.origin[x.Body]
				if !ok {
					problem("terminal", "terminal without origin")
					return
				}
				if i, ok := at("terminal", e); ok {
					add("terminal", i, "")
					n.terminals++
				}
			case *core.Choice:
				for i, a := range x.Alts {
					e, ok := c.altOrigin(x, i)
					if !ok {
						problem("guard", "alternative %d without origin", i)
						continue
					}
					ei, ok := at("guard", e)
					if !ok {
						continue
					}
					if a.Guard == nil {
						add("guard", ei, "\t0\t0\t%d\t%q\t%q", typedDecisionsBit(a.GuardedClass), "", "")
						continue
					}
					cond, desc := guardString(g, c.m, a.Guard)
					add("guard", ei, "\t1\t%d\t%d\t%q\t%q", a.Guard.Depth, typedDecisionsBit(a.GuardedClass), desc, cond)
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
				var runs []string
				pos := 0
				for _, it := range x.Items {
					if run, ok := it.(*core.ClassRun); ok {
						runs = append(runs, fmt.Sprintf("%d:%d", pos, len(run.Classes)))
						pos += len(run.Classes)
						n.classRuns++
						continue
					}
					pos++
				}
				if pos != len(e.Items) {
					problem("class runs", "%d items in %s, %d in the Core sequence", len(e.Items), grammar.FormatExpr(e), pos)
				}
				if len(runs) == 0 {
					return
				}
				if i, ok := at("class runs", e); ok {
					add("runs", i, "\t%s", strings.Join(runs, " "))
				}
			case *core.Scan:
				e, ok := c.origin[x]
				if !ok {
					problem("scan", "scan without origin")
					return
				}
				if i, ok := at("scan", e); ok {
					add("scan", i, "")
					n.scans++
				}
			case *core.Repeat:
				e, ok := c.origin[x].(*grammar.Repeat)
				if !ok || x.Scope == nil {
					return
				}
				if i, ok := at("scope", e.Expr); ok {
					add("scope", i, "\t%s", names(x.Scope))
					n.scopes++
				}
			case *core.OperatorsRule:
				lineList, exprs, actions := opLines(x, r.Def.Expr.(*grammar.OperatorsRule))
				for i, l := range lineList {
					if ei, ok := at("scope", exprs[i]); ok {
						add("scope", ei, "\t%s", names(l.Scope))
						n.scopes++
					}
					if typed && l.Action != nil && actions[i] != nil {
						term(-2-i, l.Action)
					}
				}
			case *core.Pred:
				if !typed {
					return
				}
				e, ok := c.origin[x].(*grammar.Predicate)
				if !ok {
					problem("term", "predicate without origin")
					return
				}
				if i, ok := at("term", e); ok {
					term(i, x.Term)
				}
			case *core.BindNode:
				if !typed {
					return
				}
				e, ok := c.origin[x].(*grammar.Predicate)
				if !ok {
					problem("term", "binding without origin")
					return
				}
				if i, ok := at("term", e); ok {
					term(i, x.Term)
				}
			}
		})
	}
	sort.Strings(lines)
	return lines, problems
}

// decisionAspects maps the kind of a decision line to its aspect.
var decisionAspects = map[string]string{"terminal": "terminal", "guard": "guard", "runs": "class runs", "scan": "scan", "scope": "scope", "term": "term"}

// compareCoreDecisions compares the decisions of the Core IR of c with the golden's, and returns
// the differences, each prefixed with its aspect (terminal, guard, class runs, scan, scope, term;
// verify for a pass after which Verify fails). It sets c.lines. With -update only the problems of the
// encoding and of Verify are returned: the frozen-decisions tests write the golden from c.lines.
func compareCoreDecisions(c *coreGenCase, n *coreDecisionCounts) []string {
	lines, errs := coreDecisionLines(c, n)
	c.lines = lines
	for _, e := range c.passErrs {
		errs = append(errs, "verify: "+e)
	}
	if !*gentest.UpdateGoldens {
		c.golden.read()
		want, ok := c.golden.lines[decisionsKey{c.mode, c.name}]
		if !ok {
			errs = append(errs, fmt.Sprintf("golden: no entry for this grammar in %s (run with -update after review)", c.golden.path))
		}
		count := map[string]int{}
		for _, l := range lines {
			count[l]++
		}
		for _, l := range want {
			count[l]--
		}
		for l, k := range count {
			if k == 0 {
				continue
			}
			f := strings.Split(l, "\t")
			id, _ := strconv.Atoi(f[1])
			name := "?"
			if id >= 0 && id < len(c.rules) {
				name = c.rules[id].Name
			}
			who, other := "Core", "golden"
			if k < 0 {
				who, other = other, who
			}
			errs = append(errs, fmt.Sprintf("%s: rule %s: %s %s, %s none", decisionAspects[f[0]], name, who, strings.Join(f[2:], " "), other))
		}
	}
	slices.Sort(errs)
	return errs
}

// coreTermTree returns the structure and forms of a Core term as the decisions record them
// (termTree): a struct constructor is in declaration order only where the generator writes it so,
// in a parser without step charges.
func coreTermTree(t core.Term, noWork bool) *termTree {
	n := &termTree{}
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
			part := &termTree{form: [...]string{core.PartList: "part-list", core.PartMap: "part-map", core.PartItems: "part-items"}[p.Kind]}
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

func (n *termTree) String() string {
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
func countForms(n *termTree, forms map[string]int) {
	switch n.form {
	case "text-eq", "field-cmp", "text-in", "projected-map", "concat-fused", "new-in-order", "part-items":
		forms[n.form]++
	}
	for _, k := range n.kids {
		countForms(k, forms)
	}
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

// TestCoreTypedDecisionsFrozen compares the decisions of the Core IR passes over the corpus in
// each mode with the typed decisions frozen in testdata/typed_decisions.golden, and checks that
// every kind of decision is met. -update rewrites the golden (core_typed_decisions_test.go).
func TestCoreTypedDecisionsFrozen(t *testing.T) {
	typedGolden.freeze(t, coreGenModes, func(t *testing.T, mode coreGenMode) []*coreGenCase {
		return coreGenCases(t, mode, nil)
	}, func(t *testing.T, mode coreGenMode, n *coreDecisionCounts) {
		lean := !mode.opts.WorkControls
		terminals := lean && !mode.opts.Observability
		if n.guards == 0 || n.scans == 0 || n.scopes == 0 || n.terms == 0 ||
			(n.terminals > 0) != terminals || (n.classRuns > 0) != lean || (n.guardedClasses > 0) != lean {
			t.Errorf("counts %+v", n)
		}
		if mode.opts.BoxedComparisons != (n.forms["field-cmp"] == 0) || mode.opts.DisableLiteralTextDispatch != (n.forms["text-in"] == 0) ||
			n.forms["concat-fused"] == 0 || n.forms["projected-map"]+n.forms["part-items"] == 0 {
			t.Errorf("forms %v", n.forms)
		}
	})
}
