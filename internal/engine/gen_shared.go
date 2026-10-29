package engine

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	irbuild "github.com/ornew/pego/internal/ir/build"
	"github.com/ornew/pego/internal/ir/core"
	"github.com/ornew/pego/internal/ir/flow"
	"github.com/ornew/pego/internal/limits"
)

// genBase is the state that the Go and TypeScript generators share: the program and rule being
// printed, the generation switches both read, the expectation table, the shared names of
// literals, methods and field type variables, and the Flow IR of the rules printed from it.
type genBase struct {
	observability bool
	// noWork leaves the step charges out of Go output generated without work controls, where
	// no control exists to charge: the statements, and the closures that only held one.
	noWork bool
	// ordinary prints the bodies in their ordinary form (no work controls and no Documents): the
	// helpers with an Ordinary form, no charges, inline class tests, and for typed rule bodies the
	// lean lowering (terminal programs, class runs and tables, guarded classes).
	ordinary              bool
	document              bool
	flowErr               error // the first rule, line or stream body that Flow IR does not print, or the first internal error of the unread-variable check
	disableMethodSharing  bool
	disableLiteralSharing bool
	// Only exact emitted bodies with the same signature share a helper. Rule
	// wrappers, memo IDs, frame layouts and expectation IDs remain in the code.
	methodNames                map[generatedMethod]string
	literalNames               map[string]string
	methodKeys                 []generatedMethod
	literalKeys                []string
	disableLiteralTextDispatch bool
	disableGuardedClasses      bool
	boxedComparisons           bool
	// flow holds the Core IR of the typed rules while they are printed from Flow IR, or nil;
	// flowRules counts the rules printed from Flow IR as flat rules and flowFrames the bodies
	// and lines printed as matchers of runtime-owned frames.
	flow  *flowState
	prog  *compile.Program
	table string        // the rule table being generated: rules, or recRules for Recognize
	cur   *compile.Rule // the rule being generated
	// proj holds the projected repetition captures of the rule being generated (projections).
	proj    map[string]string
	vars    strings.Builder
	init    strings.Builder
	methods strings.Builder
	n       int
	// descs is the expectation table of the generated parser (descs in runtime); descIDs maps a
	// description to its ID.
	descs   []string
	descIDs map[string]compile.ExpID
	// fieldTypes names the field type variable of each checked constructed field, result and cast (by
	// *grammar.FieldInit or checkInfo.results key), numbered separately so that checks do not
	// rename other generated names (runtime/fieldcheck.go).
	fieldTypes map[any]string
	// fieldTypeKeys are the keys of fieldTypes in the order of their variables, for rollback.
	fieldTypeKeys []any
}

// fieldTypeVar returns the variable holding check type ft of key, declaring it with decl the
// first time.
func (g *genBase) fieldTypeVar(key any, ft compile.Ty, decl func(v string, t *compile.FieldType) string) string {
	v, ok := g.fieldTypes[key]
	if !ok {
		if g.fieldTypes == nil {
			g.fieldTypes = map[any]string{}
		}
		v = fmt.Sprintf("fieldType%d", len(g.fieldTypes)+1)
		g.fieldTypes[key] = v
		g.fieldTypeKeys = append(g.fieldTypeKeys, key)
		g.vars.WriteString(decl(v, compile.NewFieldType(ft)))
	}
	return v
}

// useSparseMemo removes the representation selector entirely when neither
// emitted rule table can cross the sparse-storage threshold.
func useSparseMemo(opts gen.GenOptions, prog, rec *compile.Program) bool {
	return !opts.DisableSparseMemo && (prog.NSeen > 64 || rec != nil && rec.NSeen > 64)
}

func generatedMaxDepth(o gen.GenOptions) int {
	if o.MaxDepth == 0 {
		return limits.DefaultMaxDepth
	}
	return o.MaxDepth
}

type generatedMethod struct {
	signature string
	body      string
}

// methodName returns an existing exact helper or reserves the next name.
// The signature keeps Node, typed Go and TypeScript functions distinct.
func (g *genBase) methodName(signature, body string) (string, bool) {
	if g.disableMethodSharing {
		return g.name("e"), false
	}
	key := generatedMethod{signature, body}
	if name, ok := g.methodNames[key]; ok {
		return name, true
	}
	if g.methodNames == nil {
		g.methodNames = make(map[generatedMethod]string)
	}
	name := g.name("e")
	g.methodNames[key] = name
	g.methodKeys = append(g.methodKeys, key)
	return name, false
}

// literalName shares immutable literal tables by decoded text. Descriptions
// belong to each matcher, so spellings with different expectations stay distinct.
func (g *genBase) literalName(value string) (string, bool) {
	if g.disableLiteralSharing {
		return g.name("lit"), false
	}
	if name, ok := g.literalNames[value]; ok {
		return name, true
	}
	if g.literalNames == nil {
		g.literalNames = make(map[string]string)
	}
	name := g.name("lit")
	g.literalNames[value] = name
	g.literalKeys = append(g.literalKeys, value)
	return name, false
}

// seedDescs starts the table of expectation descriptions with the fixed ones.
func (g *genBase) seedDescs() {
	if g.descIDs != nil {
		return
	}
	g.descIDs = map[string]compile.ExpID{}
	for i, d := range compile.FixedDescs {
		g.descs = append(g.descs, d)
		g.descIDs[d] = compile.ExpID(i)
	}
}

// desc returns the expectation ID of description s, adding it to the table.
func (g *genBase) desc(s string) compile.ExpID {
	g.seedDescs()
	if id, ok := g.descIDs[s]; ok {
		return id
	}
	id := compile.ExpID(len(g.descs))
	g.descs = append(g.descs, s)
	g.descIDs[s] = id
	return id
}

func (g *genBase) name(prefix string) string {
	g.n++
	return fmt.Sprintf("%s%d", prefix, g.n)
}

// projections returns the projected repetition captures of the rule r (see ruleProjections), none
// in the recognizer.
func (g *genBase) projections(r *compile.Rule) map[string]string {
	if g.table == "recRules" {
		return nil
	}
	return compile.RuleProjections(g.prog, r)
}

// projected reports whether the map call t reads a projected capture (projections), and returns
// the capture.
func (g *genBase) projected(t *grammar.Call, locals map[string]string) (grammar.Term, bool) {
	x, ok := compile.ProjectedMap(t, g.proj, func(name string) bool { _, local := locals[name]; return local })
	return x, ok
}

// literalTextMembership recognizes a side-effect-free disjunction of text
// equality checks against one captured value. Restricting the capture to a
// direct reference makes evaluating text once observably equivalent to the
// original ordered comparisons.
func literalTextMembership(t grammar.Term) (string, []string, bool) {
	var name string
	var literals []string
	var walk func(grammar.Term) bool
	walk = func(t grammar.Term) bool {
		if b, ok := t.(*grammar.Binary); ok && b.Op == "||" {
			return walk(b.L) && walk(b.R)
		}
		b, ok := t.(*grammar.Binary)
		if !ok || b.Op != "==" {
			return false
		}
		var capture *grammar.CaptureRef
		var literal *grammar.StringLit
		if x, ok := compile.TextArg(b.L); ok {
			capture, _ = x.(*grammar.CaptureRef)
			literal, _ = b.R.(*grammar.StringLit)
		}
		if capture == nil || literal == nil {
			if x, ok := compile.TextArg(b.R); ok {
				capture, _ = x.(*grammar.CaptureRef)
				literal, _ = b.L.(*grammar.StringLit)
			}
		}
		if capture == nil || literal == nil || (name != "" && name != capture.Name) {
			return false
		}
		name = capture.Name
		literals = append(literals, literal.Value)
		return true
	}
	if !walk(t) || len(literals) < 2 {
		return "", nil, false
	}
	return name, literals, true
}

// flowState holds the Core IR of the typed rules while they are printed from Flow IR.
type flowState struct {
	m      *core.Module
	origin map[core.Node]grammar.Expr
	opts   flow.Options
	// rules holds the rules of the program by ID, value-free twins included.
	rules []*compile.Rule
}

// failFlow records that Flow IR does not print a part of a table, which fails the generation: every
// table is printed from Flow IR and has no other route.
func (g *genBase) failFlow(what string) {
	if g.flowErr == nil {
		g.flowErr = fmt.Errorf("Flow IR does not print %s", what)
	}
}

// flowLineOK reports whether the Operators Rule line e is a framed body that Flow IR can print
// inside the runtime-owned frame of its rule.
func flowLineOK(e grammar.Expr) bool {
	return flowBodyOK(e, true, true)
}

// flowFrameExprOK reports whether the body of the rule r is one that Flow IR prints as a framed body of the
// Node or recognizer table: as flatRuleOK, except that a body may cut or recover, and that the rule may be a
// left-recursion leader. Flow IR lowers a cut as the typed table does (a flag per backtracking point,
// published to the runtime's cut flag on return), so the
// runtime's frame, memo and finish ownership are the same. The runtime owns the growth of a leader's seed
// around its body, so the body is the matcher of any other rule; its left-recursive alternatives have no
// first set and are never guarded. The runtime's RecoverAttr sequence is lowered inline, with NewErrorNode
// building the error node.
func flowFrameExprOK(r *compile.Rule) bool {
	return flowBodyOK(r.BodyExpr(), true, true)
}

// flowNodeFrameOK reports whether the rule r of the Node or recognizer table is printed from Flow IR as
// a framed body, with its Core rule.
//
// Core IR gathers the values of a projected repetition (Project), and the actions that read it expect
// that shape. The Node table writes its actions with the projections of the rule (generator.projections),
// which are the ones Core IR makes, so a Core-lowered body agrees with them. The recognizer's table writes
// its actions without projections, so its Core IR is built without them (irbuild.CoreProjecting) and agrees with
// them as well; either table's body is eligible when its action reads only captures of the frame.
func (g *genBase) flowNodeFrameOK(r *compile.Rule) (*core.Rule, bool) {
	if g.flow == nil || g.table == "trules" || !flowFrameExprOK(r) {
		return nil, false
	}
	cr := g.flow.m.Rules[r.ID]
	if !flow.FramedBody(cr.Body) {
		return nil, false
	}
	if r.Action != nil {
		known := compile.NewScope()
		for _, sl := range cr.Scope {
			known.Slot(sl.Name)
		}
		if !frameRefsKnown(r.Action, known) {
			return nil, false
		}
	}
	return cr, true
}

// frameScope returns the scope of the frame whose slots the captures c name.
func frameScope(c flow.Captures) *compile.Scope {
	s := compile.NewScope()
	for _, name := range c.Names {
		s.Slot(name)
	}
	return s
}

// recFlowEligible reports whether the rule r of the Node table or of the recognizer table is printed
// from Flow IR as a flat rule: a lean value-free rule (leanRuleOK). Its captures
// are Flow statements as in any rule; a semantic predicate keeps a rule out of leanRuleOK.
func recFlowEligible(r *compile.Rule) bool {
	if !leanRuleOK(r) {
		return false
	}
	ok := true
	compile.WalkExpr(r.BodyExpr(), func(e grammar.Expr) {
		switch e.(type) {
		case *grammar.Predicate:
			ok = false
		}
	})
	return ok
}

// startRecFlow builds the Core IR of the program rec (the recognizer, or the program of the Node table),
// with the passes of the untyped tables (no terminal programs and no class runs), for printing its
// eligible rules from Flow IR. It runs the passes of typedCore that the Node runtime's rules use:
// rewrites, Analyze, Guards, scans and the repeats, which irbuild.FollowRepeats keeps in step with the scans.
// It omits Terminals and ClassRuns, which typedCore runs only for lean typed rules (leanTyped): the Node recognizer writes
// its terminals and classes as inline tests.
func (g *genBase) startRecFlow(rec *compile.Program) error {
	origin := map[core.Node]grammar.Expr{}
	m, err := irbuild.CoreProjecting(rec, origin, g.table != "recRules")
	if err != nil {
		return err
	}
	core.RewriteTerms(m, core.TermOptions{TextDispatch: !g.disableLiteralTextDispatch, FieldCompare: !g.boxedComparisons})
	// Guards read the First sets that Analyze computes, as typedCore does: without this pass the
	// alternatives of a choice get no first-character guard, and the Flow form calls the rules of
	// the later alternatives at every position (duckdb: block_comment, 84k calls against none).
	core.Analyze(m)
	core.Guards(m, g.ordinary && !g.disableGuardedClasses)
	repeats := irbuild.FollowRepeats(m, origin)
	core.Scans(m)
	for _, f := range repeats {
		f()
	}
	core.Analyze(m)
	if err := core.Verify(m); err != nil {
		return fmt.Errorf("internal error: %w", err)
	}
	// Classes are inline tests, as the Node runtime's generated code writes them.
	opts := flow.Options{Work: !g.noWork, Recovers: flow.Recovers(m), GuardedClasses: g.ordinary && !g.disableGuardedClasses,
		InlineLiterals: maxInlineLiteral, Observe: g.observability, Node: true, Document: g.document}
	g.flow = &flowState{m: m, origin: origin, opts: opts, rules: append(append([]*compile.Rule(nil), rec.RuleList...), rec.Twins...)}
	return nil
}

func frameRefsKnown(t grammar.Term, s *compile.Scope, locals ...string) bool {
	ok := true
	capRefs(t, func(name string) {
		if slices.Contains(locals, name) {
			return
		}
		if _, found := s.Slots[name]; !found {
			ok = false
		}
	})
	return ok
}

// flatRuleOK reports whether the rule r can be printed as a flat rule: it is not a left-recursion
// leader and its body has no cut, recovery or Operators Rule.
func flatRuleOK(r *compile.Rule) bool {
	return !r.Leader && flowBodyOK(r.BodyExpr(), false, false)
}

// flowBodyOK reports whether Flow IR can print the expression e in place. A cut is allowed if cuts is
// set and a recovery attribute if recovery is set; an Operators Rule never is.
func flowBodyOK(expr grammar.Expr, cuts, recovery bool) bool {
	ok := true
	compile.WalkExpr(expr, func(e grammar.Expr) {
		switch e := e.(type) {
		case *grammar.Cut:
			if !cuts {
				ok = false
			}
		case *grammar.OperatorsRule:
			ok = false
		case *grammar.Attributed:
			for _, a := range e.Attrs {
				if a.Name == "recover" && !recovery {
					ok = false
				}
			}
		}
	})
	return ok
}

// leanRuleOK selects value-free plain rules of the Node runtime. Captures
// unused by recognition are omitted; predicates keep the general code because
// they can require values, capture frames and variable bindings.
func leanRuleOK(r *compile.Rule) bool {
	if !r.Plain || !r.Lean || !r.Novalue || !flatRuleOK(r) {
		return false
	}
	ok := true
	compile.WalkExpr(r.BodyExpr(), func(e grammar.Expr) {
		if _, predicate := e.(*grammar.Predicate); predicate {
			ok = false
		}
	})
	return ok
}

// maxInlineLiteral is the longest literal (in code points) compared in place.
const maxInlineLiteral = 4

// capRefs calls f with the name of each capture the term t refers to outside lambdas that bind
// the name.
func capRefs(t grammar.Term, f func(name string)) {
	var walk func(t grammar.Term, shadow map[string]bool)
	walk = func(t grammar.Term, shadow map[string]bool) {
		switch t := t.(type) {
		case *grammar.CaptureRef:
			if !shadow[t.Name] {
				f(t.Name)
			}
		case *grammar.Lambda:
			inner := map[string]bool{}
			for k := range shadow {
				inner[k] = true
			}
			for _, p := range t.Params {
				inner[p] = true
			}
			walk(t.Body, inner)
		default:
			walkChildren(t, func(x grammar.Term) { walk(x, shadow) })
		}
	}
	walk(t, map[string]bool{})
}

// walkChildren calls f with the direct subterms of t (those walkTerm visits next).
func walkChildren(t grammar.Term, f func(grammar.Term)) {
	switch t := t.(type) {
	case *grammar.Member:
		f(t.X)
	case *grammar.New:
		for _, fi := range t.Fields {
			f(fi.Value)
		}
	case *grammar.ListConstructor:
		for _, item := range t.Items {
			f(item)
		}
	case *grammar.TupleLit:
		for _, x := range t.Elems {
			f(x)
		}
	case *grammar.Call:
		for _, a := range t.Args {
			f(a)
		}
	case *grammar.Lambda:
		f(t.Body)
	case *grammar.Binary:
		f(t.L)
		f(t.R)
	case *grammar.Unary:
		f(t.X)
	case *grammar.Cast:
		f(t.X)
	case *grammar.Bind:
		f(t.Value)
	}
}

// cstJoin reports whether the join has members that stay *Node. The members are read as
// joinMembers writes them, so a join that is spelled with node[_] or error has the same answer as
// the alias that names it: node[_] stays a node, and error converts to an Error.
func cstJoin(u compile.JoinTy) bool {
	if m, ok := compile.JoinMembers(u).(compile.JoinTy); ok {
		u = m
	}
	for _, a := range u.Alts {
		switch a := a.(type) {
		case compile.NamedTy:
			if a.Kind == 'c' && a != compile.TyMatch && a != compile.TyError {
				return true
			}
		case compile.BasicTy, compile.ListTy, compile.RecordTy:
			return true
		}
	}
	return false
}
