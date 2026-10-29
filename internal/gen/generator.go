package gen

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	irbuild "github.com/ornew/pego/internal/ir/build"
	"github.com/ornew/pego/internal/ir/core"
	"github.com/ornew/pego/internal/ir/flow"
	"github.com/ornew/pego/internal/limits"
)

// Generator is the state that the Go and TypeScript generators share: the program and rule being
// printed, the generation switches both read, the expectation table, the shared names of
// literals, methods and field type variables, and the Flow IR of the rules printed from it.
type Generator struct {
	Observability bool
	// NoWork leaves the step charges out of Go output generated without work controls, where
	// no control exists to charge: the statements, and the closures that only held one.
	NoWork bool
	// Ordinary prints the bodies in their ordinary form (no work controls and no Documents): the
	// helpers with an Ordinary form, no charges, inline class tests, and for typed rule bodies the
	// lean lowering (terminal programs, class runs and tables, guarded classes).
	Ordinary              bool
	Document              bool
	FlowErr               error // the first rule, line or stream body that Flow IR does not print, or the first internal error of the unread-variable check
	DisableMethodSharing  bool
	DisableLiteralSharing bool
	// Only exact emitted bodies with the same signature share a helper. Rule
	// wrappers, memo IDs, frame layouts and expectation IDs remain in the code.
	methodNames                map[generatedMethod]string
	literalNames               map[string]string
	methodKeys                 []generatedMethod
	literalKeys                []string
	DisableLiteralTextDispatch bool
	DisableGuardedClasses      bool
	BoxedComparisons           bool
	// Flow holds the Core IR of the typed rules while they are printed from Flow IR, or nil;
	// flowRules counts the rules printed from Flow IR as flat rules and flowFrames the bodies
	// and lines printed as matchers of runtime-owned frames.
	Flow  *FlowState
	Prog  *compile.Program
	Table string        // the rule table being generated: rules, or recRules for Recognize
	Cur   *compile.Rule // the rule being generated
	// Proj holds the projected repetition captures of the rule being generated (Projections).
	Proj    map[string]string
	Vars    strings.Builder
	Init    strings.Builder
	Methods strings.Builder
	n       int
	// Descs is the expectation table of the generated parser (descs in runtime); descIDs maps a
	// description to its ID.
	Descs   []string
	DescIDs map[string]compile.ExpID
	// FieldTypes names the field type variable of each checked constructed field, result and cast (by
	// *grammar.FieldInit or checkInfo.results key), numbered separately so that checks do not
	// rename other generated names (runtime/fieldcheck.go).
	FieldTypes map[any]string
	// fieldTypeKeys are the keys of fieldTypes in the order of their variables, for rollback.
	fieldTypeKeys []any
}

// FieldTypeVar returns the variable holding check type ft of key, declaring it with decl the
// first time.
func (g *Generator) FieldTypeVar(key any, ft compile.Ty, decl func(v string, t *compile.FieldType) string) string {
	v, ok := g.FieldTypes[key]
	if !ok {
		if g.FieldTypes == nil {
			g.FieldTypes = map[any]string{}
		}
		v = fmt.Sprintf("fieldType%d", len(g.FieldTypes)+1)
		g.FieldTypes[key] = v
		g.fieldTypeKeys = append(g.fieldTypeKeys, key)
		g.Vars.WriteString(decl(v, compile.NewFieldType(ft)))
	}
	return v
}

// UseSparseMemo removes the representation selector entirely when neither
// emitted rule table can cross the sparse-storage threshold.
func UseSparseMemo(opts GenOptions, prog, rec *compile.Program) bool {
	return !opts.DisableSparseMemo && (prog.NSeen > 64 || rec != nil && rec.NSeen > 64)
}

func GeneratedMaxDepth(o GenOptions) int {
	if o.MaxDepth == 0 {
		return limits.DefaultMaxDepth
	}
	return o.MaxDepth
}

type generatedMethod struct {
	signature string
	body      string
}

// MethodName returns an existing exact helper or reserves the next name.
// The signature keeps Node, typed Go and TypeScript functions distinct.
func (g *Generator) MethodName(signature, body string) (string, bool) {
	if g.DisableMethodSharing {
		return g.Name("e"), false
	}
	key := generatedMethod{signature, body}
	if name, ok := g.methodNames[key]; ok {
		return name, true
	}
	if g.methodNames == nil {
		g.methodNames = make(map[generatedMethod]string)
	}
	name := g.Name("e")
	g.methodNames[key] = name
	g.methodKeys = append(g.methodKeys, key)
	return name, false
}

// LiteralName shares immutable literal tables by decoded text. Descriptions
// belong to each matcher, so spellings with different expectations stay distinct.
func (g *Generator) LiteralName(value string) (string, bool) {
	if g.DisableLiteralSharing {
		return g.Name("lit"), false
	}
	if name, ok := g.literalNames[value]; ok {
		return name, true
	}
	if g.literalNames == nil {
		g.literalNames = make(map[string]string)
	}
	name := g.Name("lit")
	g.literalNames[value] = name
	g.literalKeys = append(g.literalKeys, value)
	return name, false
}

// SeedDescs starts the table of expectation descriptions with the fixed ones.
func (g *Generator) SeedDescs() {
	if g.DescIDs != nil {
		return
	}
	g.DescIDs = map[string]compile.ExpID{}
	for i, d := range compile.FixedDescs {
		g.Descs = append(g.Descs, d)
		g.DescIDs[d] = compile.ExpID(i)
	}
}

// Desc returns the expectation ID of description s, adding it to the table.
func (g *Generator) Desc(s string) compile.ExpID {
	g.SeedDescs()
	if id, ok := g.DescIDs[s]; ok {
		return id
	}
	id := compile.ExpID(len(g.Descs))
	g.Descs = append(g.Descs, s)
	g.DescIDs[s] = id
	return id
}

func (g *Generator) Name(prefix string) string {
	g.n++
	return fmt.Sprintf("%s%d", prefix, g.n)
}

// Projections returns the projected repetition captures of the rule r (see ruleProjections), none
// in the recognizer.
func (g *Generator) Projections(r *compile.Rule) map[string]string {
	if g.Table == "recRules" {
		return nil
	}
	return compile.RuleProjections(g.Prog, r)
}

// Projected reports whether the map call t reads a projected capture (Projections), and returns
// the capture.
func (g *Generator) Projected(t *grammar.Call, locals map[string]string) (grammar.Term, bool) {
	x, ok := compile.ProjectedMap(t, g.Proj, func(name string) bool { _, local := locals[name]; return local })
	return x, ok
}

// LiteralTextMembership recognizes a side-effect-free disjunction of text
// equality checks against one captured value. Restricting the capture to a
// direct reference makes evaluating text once observably equivalent to the
// original ordered comparisons.
func LiteralTextMembership(t grammar.Term) (string, []string, bool) {
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

// FlowState holds the Core IR of the typed rules while they are printed from Flow IR.
type FlowState struct {
	M      *core.Module
	Origin map[core.Node]grammar.Expr
	Opts   flow.Options
	// Rules holds the rules of the program by ID, value-free twins included.
	Rules []*compile.Rule
}

// FailFlow records that Flow IR does not print a part of a table, which fails the generation: every
// table is printed from Flow IR and has no other route.
func (g *Generator) FailFlow(what string) {
	if g.FlowErr == nil {
		g.FlowErr = fmt.Errorf("Flow IR does not print %s", what)
	}
}

// FlowLineOK reports whether the Operators Rule line e is a framed body that Flow IR can print
// inside the runtime-owned frame of its rule.
func FlowLineOK(e grammar.Expr) bool {
	return FlowBodyOK(e, true, true)
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
	return FlowBodyOK(r.BodyExpr(), true, true)
}

// FlowNodeFrameOK reports whether the rule r of the Node or recognizer table is printed from Flow IR as
// a framed body, with its Core rule.
//
// Core IR gathers the values of a projected repetition (Project), and the actions that read it expect
// that shape. The Node table writes its actions with the projections of the rule (Generator.Projections),
// which are the ones Core IR makes, so a Core-lowered body agrees with them. The recognizer's table writes
// its actions without projections, so its Core IR is built without them (irbuild.CoreProjecting) and agrees with
// them as well; either table's body is eligible when its action reads only captures of the frame.
func (g *Generator) FlowNodeFrameOK(r *compile.Rule) (*core.Rule, bool) {
	if g.Flow == nil || g.Table == "trules" || !flowFrameExprOK(r) {
		return nil, false
	}
	cr := g.Flow.M.Rules[r.ID]
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

// FrameScope returns the scope of the frame whose slots the captures c name.
func FrameScope(c flow.Captures) *compile.Scope {
	s := compile.NewScope()
	for _, name := range c.Names {
		s.Slot(name)
	}
	return s
}

// RecFlowEligible reports whether the rule r of the Node table or of the recognizer table is printed
// from Flow IR as a flat rule: a lean value-free rule (leanRuleOK). Its captures
// are Flow statements as in any rule; a semantic predicate keeps a rule out of leanRuleOK.
func RecFlowEligible(r *compile.Rule) bool {
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

// StartRecFlow builds the Core IR of the program rec (the recognizer, or the program of the Node
// table), with the passes of the untyped tables (no terminal programs and no class runs), for
// printing its eligible rules from Flow IR. It runs the passes of gogen.Generator.TypedCore that the
// Node runtime's rules use: rewrites, Analyze, Guards, scans and the repeats, which
// irbuild.FollowRepeats keeps in step with the scans. It omits Terminals and ClassRuns, which
// gogen.Generator.TypedCore runs only for lean typed rules (leanTyped): the Node recognizer writes its
// terminals and classes as inline tests.
func (g *Generator) StartRecFlow(rec *compile.Program) error {
	origin := map[core.Node]grammar.Expr{}
	m, err := irbuild.CoreProjecting(rec, origin, g.Table != "recRules")
	if err != nil {
		return err
	}
	core.RewriteTerms(m, core.TermOptions{TextDispatch: !g.DisableLiteralTextDispatch, FieldCompare: !g.BoxedComparisons})
	// Guards read the First sets that Analyze computes, as gogen.Generator.TypedCore does: without this
	// pass the alternatives of a choice get no first-character guard, and the Flow form calls the rules of
	// the later alternatives at every position (duckdb: block_comment, 84k calls against none).
	core.Analyze(m)
	core.Guards(m, g.Ordinary && !g.DisableGuardedClasses)
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
	opts := flow.Options{Work: !g.NoWork, Recovers: flow.Recovers(m), GuardedClasses: g.Ordinary && !g.DisableGuardedClasses,
		InlineLiterals: MaxInlineLiteral, Observe: g.Observability, Node: true, Document: g.Document}
	g.Flow = &FlowState{M: m, Origin: origin, Opts: opts, Rules: append(append([]*compile.Rule(nil), rec.RuleList...), rec.Twins...)}
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
	return !r.Leader && FlowBodyOK(r.BodyExpr(), false, false)
}

// FlowBodyOK reports whether Flow IR can print the expression e in place. A cut is allowed if cuts is
// set and a recovery attribute if recovery is set; an Operators Rule never is.
func FlowBodyOK(expr grammar.Expr, cuts, recovery bool) bool {
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

// MaxInlineLiteral is the longest literal (in code points) compared in place.
const MaxInlineLiteral = 4

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

// CstJoin reports whether the join has members that stay *Node. The members are read as
// joinMembers writes them, so a join that is spelled with node[_] or error has the same answer as
// the alias that names it: node[_] stays a node, and error converts to an Error.
func CstJoin(u compile.JoinTy) bool {
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
