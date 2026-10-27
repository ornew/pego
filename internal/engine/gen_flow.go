package engine

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/engine/flow"
)

// Typed Go rules printed from Flow IR.
//
// The typed rules are built as Core IR (typedCore) and printed from Flow IR here. The rules that
// flow.LocalRule accepts are lowered to local functions (flow.Lower) and printed into the direct
// rule that typedCall and directMethod wrap. The bodies of left-recursion leaders and of rules
// that recover, and the lines of Operators Rules, are lowered to framed functions
// (flow.LowerFrame) and printed into the matcher methods that the runtime calls under the frames
// it owns (finishFrameBody). A construct that neither form accepts is reported as an error
// (failFlow): no table has another route.
//
// The printer follows reachability: a statement at an unreachable point is left out,
// but its text is still formed, so that the package variables it names (literals, expectations,
// class tables, shapers, scopes) are declared in the same order. A variable that no printed statement
// reads is not declared and its assignments are left out (flowVars); one that must stay is assigned to the
// blank identifier on the first line.

// flowState holds the Core IR of the typed rules while they are printed from Flow IR.
type flowState struct {
	m      *core.Module
	origin map[core.Node]grammar.Expr
	opts   flow.Options
	// rules holds the rules of the program by ID, value-free twins included.
	rules []*rule
}

// flowVarsErr records the internal error that resolving the variables of a printed function found.
func (g *generator) flowVarsErr(res flowVarsResult) {
	if res.err != nil && g.flowErr == nil {
		g.flowErr = res.err
	}
}

// failFlow records that Flow IR does not print a part of a table, which fails the generation: every
// table is printed from Flow IR and has no other route.
func (g *generator) failFlow(what string) {
	if g.flowErr == nil {
		g.flowErr = fmt.Errorf("Flow IR does not print %s", what)
	}
}

// flowLineOK reports whether the Operators Rule line e is a framed body that Flow IR can print
// inside the runtime-owned frame of its rule.
func flowLineOK(e grammar.Expr) bool {
	return directFrameExprOK(e, true, true)
}

// startFlow builds the Core IR of the typed rules for printing them from Flow IR.
func (g *generator) startFlow() error {
	origin := map[core.Node]grammar.Expr{}
	m, err := g.typedCore(origin)
	if err != nil {
		return err
	}
	g.flow = &flowState{m: m, origin: origin, rules: append(append([]*rule(nil), g.prog.rules...), g.prog.twins...),
		opts: g.flowOptions(m)}
	return nil
}

// flowOptions returns the options of lowering the typed rules of the Core IR m, while the typed
// table is generated.
func (g *generator) flowOptions(m *core.Module) flow.Options {
	// Node is false: the options of the Node and recognizer tables (startRecFlow) are built apart, and
	// a function lowered with one set of options is never printed for the other runtime.
	return flow.Options{Work: !g.noWork, Recovers: flow.Recovers(m), ClassTables: g.leanTyped(),
		GuardedClasses: g.lean && !g.disableGuardedClasses, InlineLiterals: maxInlineLiteral, Observe: g.observability, Document: g.document}
}

// flowDirectRule prints the rule r from Flow IR as a direct rule, or returns nil if r is not
// printed from Flow IR.
func (g *generator) flowDirectRule(r *rule) *direct {
	if g.flow == nil || g.table != "trules" {
		return nil
	}
	cr := g.flow.m.Rules[r.id]
	if !flow.LocalRule(cr) {
		return nil
	}
	f, err := flow.Lower(g.flow.m, cr, r.name, g.flow.opts)
	if err != nil {
		panic(fmt.Errorf("rule %s of table %s: %w", r.name, g.table, err)) // LocalRule holds
	}
	g.cur, g.proj = r, g.projections(r)
	defer func() { g.proj = nil }()
	p := newFlowPrinter(g, false)
	p.document = f.Document
	p.stmts(f.Body)
	res := p.vars.resolve(f.Vars, p.b.String())
	g.flowVarsErr(res)
	out := &direct{fail: p.used[f.Fail], succeed: !p.dead, binds: f.Binds, start: res.read(f.Start), ctx: res.read(f.Ctx)}
	var unread []string
	for _, v := range f.Vars {
		out.vars = append(out.vars, dvar{v.Name, goType(v.Type)})
		switch {
		case res.dead(v):
			out.markVar(v.Name, true)
		case !p.reads[v]:
			unread = append(unread, v.Name)
		case !res.read(v):
			out.markVar(v.Name, false)
		}
	}
	out.body = res.body
	if len(unread) > 0 {
		out.body = "\t" + strings.Repeat("_, ", len(unread)-1) + "_ = " + strings.Join(unread, ", ") + "\n" + out.body
	}
	g.flowRules++
	return out
}

// flowFrameBody prints the body of the typed rule r from Flow IR as the matcher of its frame
// body, or returns "" if Flow IR does not print r that way.
func (g *generator) flowFrameBody(r *rule, scope *scope) string {
	if g.flow == nil || g.table != "trules" {
		return ""
	}
	comment, ok := g.typedFrameComment(r)
	cr := g.flow.m.Rules[r.id]
	// The runtime-owned frame has the slots of the Node rule's scope.
	if !ok || !flow.FramedBody(cr.Body) || !slices.Equal(core.SlotNames(cr.Scope), g.nodeScopes[r]) {
		return ""
	}
	if g.flow.opts.Node {
		panic("internal error: a typed rule is lowered with the options of the Node tables")
	}
	f, err := flow.LowerFrame(g.flow.m, cr, cr.Body, cr.Scope, cr.Want, r.name, g.flow.opts)
	if err != nil {
		panic(fmt.Errorf("rule %s of table %s: %w", r.name, g.table, err)) // FramedBody holds
	}
	g.cur, g.proj = r, g.projections(r)
	defer func() { g.proj = nil }()
	return g.flowFramed(f, cr.Scope, scope, r.name+" ("+comment+")")
}

// flowNodeFrameBody prints the body of the rule r of the Node table from Flow IR as the matcher of
// its frame body, or returns "" if r takes another route. The rule is eligible (flowNodeFrameOK) when
// its Core body is a framed body and its action reads only captures of the frame; the runtime owns depth,
// memo, frame, action and finish. With work controls (Options.Work) the body charges the steps Flow IR
// makes explicit; the limits of a parse hold at Flow's own charges.
func (g *generator) flowNodeFrameBody(r *rule, scope *scope) string {
	cr, ok := g.flowNodeFrameOK(r)
	if !ok {
		return ""
	}
	if !g.flow.opts.Node {
		panic("internal error: a Node rule is lowered with the options of the typed table")
	}
	f, err := flow.LowerFrame(g.flow.m, cr, cr.Body, cr.Scope, !r.lean, r.name, g.flow.opts)
	if err != nil {
		panic(fmt.Errorf("rule %s of table %s: %w", r.name, g.table, err)) // FramedBody holds
	}
	g.cur, g.proj = r, g.projections(r)
	defer func() { g.proj = nil }()
	return g.flowFramed(f, cr.Scope, scope, r.name+" (Node body inlined)")
}

// flowStreamBody prints the body of the start rule r of ParseStream from Flow IR as the matcher of its
// frame body, with the repetition of its #stream delivering the elements (flow.Options.Stream), or
// returns "" if the rule is not printed that way: a rule is eligible as for flowNodeFrameBody. The
// capture layout of the printed body must be the batch body's, which the action addresses by slot.
func (g *generator) flowStreamBody(r *rule, scope *scope) string {
	cr, ok := g.flowNodeFrameOK(r)
	if !ok || !g.flow.opts.Node {
		return ""
	}
	opts := g.flow.opts
	opts.Stream = true
	f, err := flow.LowerFrame(g.flow.m, cr, cr.Body, cr.Scope, !r.lean, r.name, opts)
	if err != nil {
		panic(fmt.Errorf("stream body of rule %s: %w", r.name, err)) // FramedBody holds
	}
	g.cur, g.proj = r, g.projections(r)
	defer func() { g.proj = nil }()
	return g.flowFramed(f, cr.Scope, scope, r.name+" (stream body)")
}

// flowFrameExprOK reports whether the body of the rule r is one that Flow IR prints as a framed body of the
// Node or recognizer table: as directOK, except that a body may cut or recover, and that the rule may be a
// left-recursion leader. Flow IR lowers a cut as the typed table does (a flag per backtracking point,
// published to the runtime's cut flag on return), so the
// runtime's frame, memo and finish ownership are the same. The runtime owns the growth of a leader's seed
// around its body, so the body is the matcher of any other rule; its left-recursive alternatives have no
// first set and are never guarded. The runtime's RecoverAttr sequence is lowered inline, with NewErrorNode
// building the error node.
func flowFrameExprOK(r *rule) bool {
	return directFrameExprOK(r.bodyExpr(), true, true)
}

// flowNodeFrameOK reports whether the rule r of the Node or recognizer table is printed from Flow IR as
// a framed body, with its Core rule.
//
// Core IR gathers the values of a projected repetition (Project), and the actions that read it expect
// that shape. The Node table writes its actions with the projections of the rule (generator.projections),
// which are the ones Core IR makes, so a Core-lowered body agrees with them. The recognizer's table writes
// its actions without projections, so its Core IR is built without them (buildCoreProjecting) and agrees with
// them as well; either table's body is eligible when its action reads only captures of the frame.
func (g *generator) flowNodeFrameOK(r *rule) (*core.Rule, bool) {
	if g.flow == nil || g.table == "trules" || !flowFrameExprOK(r) {
		return nil, false
	}
	cr := g.flow.m.Rules[r.id]
	if !flow.FramedBody(cr.Body) {
		return nil, false
	}
	if r.action != nil {
		known := newScope()
		for _, sl := range cr.Scope {
			known.slot(sl.Name)
		}
		if !frameRefsKnown(r.action, known) {
			return nil, false
		}
	}
	return cr, true
}

// flowOperatorsRuleBody prints the line l of the Operators Rule g.cur, whose expression is e, from Flow
// IR, or returns "" if Flow IR does not print the line that way.
//
// In the typed table every line is printed from Flow IR, as in the Node and recognizer tables (a line with
// recovery builds its error node with NewErrorNode there). Nothing needs a check. The lines of an Operators
// Rule have no projected repetitions (ruleProjections gives none for one), so Core IR and the tables agree
// on the shape of every capture, and the checker lets an action name only captures of its line. With work
// controls (Options.Work) the runtime charges the step of entering an operand or operator line of the Node
// and recognizer tables (entered), and the function charges the steps of the nodes below the line's root,
// as Flow IR makes them explicit. The skip of a rule is called without that charge (entered is false), so
// its function charges all of its steps.
func (g *generator) flowOperatorsRuleBody(l *core.OpLine, e grammar.Expr, scope *scope, build, entered bool) string {
	if g.flow == nil || l == nil || !flowLineOK(e) || !flow.FramedBody(l.Body) {
		return ""
	}
	if g.table != "trules" && !directFrameExprOK(e, true, true) {
		return ""
	}
	lower := flow.LowerFrame
	if g.table != "trules" && entered {
		lower = flow.LowerLine // the Node runtime charges the entry of an operand or operator line (see flow.LowerLine)
	}
	f, err := lower(g.flow.m, g.flow.m.Rules[g.cur.id], l.Body, l.Scope, build, g.cur.name, g.flow.opts)
	if err != nil {
		panic(fmt.Errorf("line of Operators Rule %s in table %s: %w", g.cur.name, g.table, err)) // FramedBody holds
	}
	prev := g.proj
	g.proj = nil // the actions of the lines use no projections of the rule
	defer func() { g.proj = prev }()
	if g.table != "trules" {
		if operatorsRouteHook != nil && g.collect == nil {
			operatorsRouteHook(g.table, g.cur)
		}
		return g.flowFramed(f, l.Scope, scope, g.cur.name+" (OperatorsRule body inlined)")
	}
	return g.flowFramed(f, l.Scope, scope, g.cur.name+" (typed OperatorsRule body inlined)")
}

// flowOperatorsRule returns the Core IR of the Operators Rule r, or nil if its lines are not
// printed from Flow IR: the typed table prints them all from it, the Node and recognizer tables
// when their Flow route is on (flowOperatorsRuleBody decides each line).
func (g *generator) flowOperatorsRule(r *rule) *core.OperatorsRule {
	if g.flow == nil || g.table != "trules" && !g.flow.opts.Node {
		return nil
	}
	o, _ := g.flow.m.Rules[r.id].Body.(*core.OperatorsRule)
	return o
}

// flowFramed prints the framed function f, whose capture scope has the slots, as a matcher method
// with the comment, as finishFrameBody does, and sets scope to its capture scope.
func (g *generator) flowFramed(f *flow.Func, slots []core.Slot, scope *scope, comment string) string {
	p := newFlowPrinter(g, true)
	p.document = f.Document
	p.stmts(f.Body)
	// The function publishes the cut flag of its outermost scope on return.
	publish := func() {
		if f.Cut != nil {
			p.markRead(f.Cut)
			p.write(fmt.Sprintf("p.Cut = %s\n", f.Cut.Name))
		}
	}
	if !p.dead {
		publish()
		p.read(f.Value)
		p.write(fmt.Sprintf("return %s, true\n", p.expr(f.Value)))
	}
	if p.used[f.Fail] {
		p.b.WriteString(p.g.label("fail") + ":\n")
		p.labelEnd = p.b.Len()
		p.dead = false
		publish()
		p.write("return nil, false\n")
	}
	var b strings.Builder
	res := p.vars.resolve(f.Vars, p.b.String())
	g.flowVarsErr(res)
	var vars []dvar
	dead, quiet := map[string]bool{}, map[string]bool{}
	var unread []string
	for _, v := range f.Vars {
		vars = append(vars, dvar{v.Name, p.goType(v.Type)})
		switch {
		case res.dead(v):
			dead[v.Name] = true
		case !p.reads[v]:
			unread = append(unread, v.Name)
		case !res.read(v):
			quiet[v.Name] = true
		}
	}
	b.WriteString(varLinesExcept(vars, dead, quiet))
	if len(unread) > 0 {
		fmt.Fprintf(&b, "%s_ = %s\n", strings.Repeat("_, ", len(unread)-1), strings.Join(unread, ", "))
	}
	b.WriteString(res.body)
	*scope = *newScope()
	for _, sl := range slots {
		scope.slot(sl.Name)
	}
	g.flowFrames++
	return g.method(comment, b.String())
}

func goType(t flow.Type) string {
	return [...]string{flow.TypeInt: "int", flow.TypeBool: "bool", flow.TypeRune: "rune", flow.TypeValue: "any",
		flow.TypeEnv: "*Env", flow.TypeExpMark: "ExpMark", flow.TypeExpIDs: "[]ExpID", flow.TypeVals: "[]any",
		flow.TypeCtx: "*TActCtx", flow.TypeMark: "Mark", flow.TypeFrame: "*TCaptureFrame", flow.TypeError: "*SyntaxError"}[t]
}

// goType returns the Go type of a variable of the function printed: the types of the Node runtime
// where the printer writes its code, otherwise those of the typed runtime.
func (p *flowPrinter) goType(t flow.Type) string {
	if p.untyped() {
		switch t {
		case flow.TypeValue:
			return "*Node"
		case flow.TypeVals:
			return "[]*Node"
		case flow.TypeFrame:
			return "*CaptureFrame"
		}
	}
	return goType(t)
}

// flowPrinter prints the body of a Flow IR function as the statements of a direct rule or, if
// frame, of a matcher method.
type flowPrinter struct {
	g     *generator
	frame bool
	// document reports that the function is lowered for Documents (flow.Func.Document): its in-place reads
	// are guarded by p.Document == nil, so that a Document parse reads through the recording primitives.
	document bool
	b        strings.Builder
	// labelEnd is the length of b after the last label written.
	labelEnd int
	// dead reports that the current point is unreachable. used holds the labels exited to from
	// reachable points, reads the variables read there, and loops the labels of loops.
	dead  bool
	used  map[*flow.Label]bool
	reads map[*flow.Var]bool
	loops map[*flow.Label]bool
	// vars holds the assignments of the function that are left out when their variables are unread.
	vars flowVars
	// names holds the package variables of the capture names of scopes, declared on first use
	// unless a DeclareNames statement declares them.
	names map[*flow.Names]string
}

func newFlowPrinter(g *generator, frame bool) *flowPrinter {
	return &flowPrinter{g: g, frame: frame, used: map[*flow.Label]bool{}, reads: map[*flow.Var]bool{},
		labelEnd: -1, loops: map[*flow.Label]bool{}, names: map[*flow.Names]string{}, vars: newFlowVars()}
}

// scopeNames returns the package variable of the capture names n, naming it on first use.
func (p *flowPrinter) scopeNames(n *flow.Names) string {
	if name, ok := p.names[n]; ok {
		return name
	}
	name := p.g.name("scope")
	p.names[n] = name
	if !n.Declared {
		p.declare(n)
	}
	return name
}

func (p *flowPrinter) declare(n *flow.Names) {
	fmt.Fprintf(&p.g.vars, "var %s = %s\n", p.names[n], goStrings(n.List))
}

// untyped reports whether the printer writes the Node runtime's code (the Node and recognizer tables).
func (p *flowPrinter) untyped() bool { return p.g.table == "recRules" || p.g.table == "rules" }

// matcher returns the parser whose methods match literals: the embedded Node parser of a typed
// parser, or the Node parser itself.
func (p *flowPrinter) matcher() string {
	if p.untyped() {
		return "p"
	}
	return "p.Parser"
}

// helper names the runtime method that the printed code calls on the parser the body runs on: the
// typed parser's embedded parser is not renamed.
func (p *flowPrinter) helper(name string) string {
	if !p.untyped() {
		return name
	}
	return p.g.helper(name)
}

// write writes s unless the current point is unreachable.
func (p *flowPrinter) write(s string) {
	if !p.dead {
		p.b.WriteString(s)
	}
}

// read records the variables that the expressions read, at a reachable point.
func (p *flowPrinter) read(xs ...flow.Expr) {
	if p.dead {
		return
	}
	for _, x := range xs {
		flow.Uses(x, p.markRead)
	}
}

// markRead records that the variable v is read at a reachable point: by the assignment being formed if
// there is one that is removed when its targets are unread, otherwise for sure.
func (p *flowPrinter) markRead(v *flow.Var) {
	p.reads[v] = true
	p.vars.read(v)
}

func (p *flowPrinter) stmts(ss []flow.Stmt) {
	for _, s := range ss {
		p.stmt(s)
	}
}

func (p *flowPrinter) stmt(s flow.Stmt) {
	switch s := s.(type) {
	case *flow.Block:
		p.stmts(s.Body)
		if p.used[s.Label] {
			fmt.Fprintf(&p.b, "%s:\n", p.g.label(s.Label.Name))
			p.labelEnd = p.b.Len()
			p.dead = false
		}
	case *flow.Loop:
		p.loops[s.Label] = true
		// The braces of a loop are written whatever the reachability.
		switch {
		case s.Counter != nil:
			n := s.Counter.Name
			p.markRead(s.Counter) // the header is printed at a dead point as well
			fmt.Fprintf(&p.b, "for %s = 0; %s < %d; %s++ {\n", n, n, s.Max, n)
		case s.While != nil:
			flow.Uses(s.While, p.markRead) // the header is printed at a dead point as well
			fmt.Fprintf(&p.b, "for %s {\n", p.expr(s.While))
		default:
			p.b.WriteString("for {\n")
		}
		p.stmts(s.Body)
		p.b.WriteString("}\n")
		p.dead = false
	case *flow.If:
		p.ifStmt(s, "if ")
	case *flow.Exit:
		if p.dead {
			return
		}
		p.used[s.To] = true
		if p.loops[s.To] {
			p.b.WriteString("break\n")
		} else {
			fmt.Fprintf(&p.b, "goto %s\n", p.g.label(s.To.Name))
		}
		p.dead = true
	case *flow.Continue:
		p.write("continue\n")
		p.dead = true
	case *flow.Set:
		p.setStmt(s)
	case *flow.Inc:
		p.incStmt(s.V)
	case *flow.Step:
		// A charge directly after a label stays in the ordinary form, like a peek there is not compacted.
		ordinary := ""
		if p.b.Len() == p.labelEnd {
			ordinary = "p.StepWork()\n"
		}
		p.write(p.g.formRest("p.StepWork()\n", ordinary, ordinary))
	case *flow.Silent:
		if s.Delta > 0 {
			p.write("p.Silent++\n")
		} else {
			p.write("p.Silent--\n")
		}
	case *flow.Peek:
		p.peekStmt(s)
	case *flow.Advance:
		p.read(s.N)
		if n, ok := s.N.(*flow.Int); ok {
			p.write(advance(n.N) + "\n")
		} else {
			p.write("p.Pos += " + p.expr(s.N) + "\n")
		}
	case *flow.Expect:
		p.read(s.At)
		if s.Message {
			p.write(fmt.Sprintf("p.%s(%s, MsgBit|%d)\n", p.g.helper("Expect"), p.expr(s.At), p.desc(s.Exp)))
		} else {
			p.write(fmt.Sprintf("p.%s(%s, %s)\n", p.g.helper("Expect"), p.expr(s.At), p.exp(s.Exp)))
		}
	case *flow.Restore:
		p.read(s.Pos, s.Rec)
		p.write(fmt.Sprintf("p.Restore(%s, %s)\n", p.expr(s.Pos), p.expr(s.Rec)))
	case *flow.MergeExpected:
		p.read(s.Far, s.Exps)
		p.write(fmt.Sprintf("p.MergeExpected(%s, %s)\n", p.expr(s.Far), p.expr(s.Exps)))
	case *flow.MergeRenamed:
		p.read(s.Far, s.Exps)
		p.write(fmt.Sprintf("p.MergeExpected(%s, RenamedExpected(%s, %s, %d))\n", p.expr(s.Far), p.matcher(), p.expr(s.Exps), p.desc(s.Exp)))
	case *flow.PushKid:
		p.read(s.X)
		if p.frame {
			p.write(fmt.Sprintf("p.KidStack = append(p.KidStack, %s)\n", p.expr(s.X)))
		} else {
			p.write(fmt.Sprintf("p.PushKid(%s)\n", p.expr(s.X)))
		}
	case *flow.StreamStart:
		p.write("p.StreamStart()\n")
	case *flow.EmitStream:
		p.read(s.X)
		p.write(fmt.Sprintf("p.EmitStream(%s, p.Pos)\n", p.expr(s.X)))
	case *flow.DropKids:
		p.read(s.Base)
		p.write(fmt.Sprintf("p.DropKids(%s)\n", p.expr(s.Base)))
	case *flow.Append:
		p.read(&flow.Use{V: s.Vals}, s.X)
		x := p.expr(s.X)
		if s.Spread && p.untyped() {
			x = x + ".RawChildren..."
		} else if s.Spread {
			x = fmt.Sprintf("AsTval(%s).TKids()...", x)
		}
		p.write(fmt.Sprintf("%s = append(%s, %s)\n", s.Vals.Name, s.Vals.Name, x))
	case *flow.SetWhere:
		p.write(fmt.Sprintf("p.Where = %q\n", s.Name))
	case *flow.DropCreated:
		p.read(s.C)
		p.write(fmt.Sprintf("p.DropCreated(%s.Cbase)\n", p.expr(s.C)))
	case *flow.Unfresh:
		p.read(s.X)
		p.write(fmt.Sprintf("if n := AsTval(%s); n != nil {\nn.TSetFresh(false)\n}\n", p.expr(s.X)))
	case *flow.Reset:
		p.read(s.M)
		p.write(fmt.Sprintf("p.Reset(%s)\n", p.expr(s.M)))
	case *flow.SetCapture:
		p.read(s.X)
		p.write(fmt.Sprintf("p.%s(%d, %s)\n", p.g.helper("SetCapture"), s.Slot, p.expr(s.X)))
	case *flow.ClearVals:
		p.read(&flow.Use{V: s.F})
		p.write(fmt.Sprintf("clear(%s.Vals)\n", s.F.Name))
	case *flow.DropTrail:
		p.read(s.M)
		p.write(fmt.Sprintf("p.DropTrail(%s.Trail)\n", p.expr(s.M)))
	case *flow.FreeFrame:
		p.read(&flow.Use{V: s.F})
		p.write(fmt.Sprintf("p.FreeFrame(%s)\n", s.F.Name))
	case *flow.AppendError:
		p.read(s.X)
		p.write(fmt.Sprintf("p.Recovered = append(p.Recovered, %s)\n", p.expr(s.X)))
	case *flow.DeclareNames:
		// Declared whatever the reachability.
		p.scopeNames(s.N)
		p.declare(s.N)
	case *flow.Comment:
		p.write("// " + s.Text + "\n")
	case *flow.ObserveSkip:
		// The alternative's expression begins with the call it skips, if any; a blank line follows
		// the report, except in the ordinary form, which has none.
		if trace := p.g.traceSkip(p.g.flow.origin[s.Alt]); trace != "" {
			trace = strings.TrimPrefix(trace, "\n")
			p.write(p.g.form(trace+"\n", trace))
		}
	default:
		panic(fmt.Sprintf("flow printer: statement %T", s))
	}
}

// ifStmt writes the If s after the keyword kw ("if " or "} else if "). The point after it is
// reachable if the point before it is.
func (p *flowPrinter) ifStmt(s *flow.If, kw string) {
	var head string
	if s.Init != nil {
		head = p.set(s.Init) + "; "
		if !p.dead {
			for _, d := range s.Init.Dst {
				if v, ok := d.(*flow.Var); ok {
					p.vars.stuck[v] = true
				}
			}
		}
	}
	p.read(s.Cond)
	head += p.expr(s.Cond)
	dead := p.dead
	p.write(kw + head + " {\n")
	p.stmts(s.Then)
	p.dead = dead
	if len(s.Else) == 1 {
		if e, ok := s.Else[0].(*flow.If); ok && e.Else == nil {
			p.ifStmt(e, "} else if ")
			return
		}
	}
	if len(s.Else) > 0 {
		p.write("} else {\n")
		p.stmts(s.Else)
		p.dead = dead
	}
	p.write("}\n")
}

// set returns the assignment s, whose reads are certain.
func (p *flowPrinter) set(s *flow.Set) string {
	dst, xs := p.setParts(s)
	return strings.Join(dst, ", ") + " = " + strings.Join(xs, ", ")
}

// setParts returns the places and the values of the assignment s, recording the reads of the values.
func (p *flowPrinter) setParts(s *flow.Set) (dst, xs []string) {
	dst = make([]string, len(s.Dst))
	for i, d := range s.Dst {
		switch d := d.(type) {
		case *flow.Var:
			dst[i] = d.Name
		case flow.Reg:
			dst[i] = p.expr(&flow.Read{R: d})
		case *flow.Discard:
			dst[i] = "_"
		}
	}
	xs = make([]string, len(s.X))
	for i, x := range s.X {
		p.read(x)
		xs[i] = p.expr(x)
	}
	return dst, xs
}

// setStmt writes the assignment s as a statement of a list, which is left out or has the blank
// identifier for its places if they are variables nothing reads (see flowVars).
func (p *flowPrinter) setStmt(s *flow.Set) {
	if p.dead {
		p.setParts(s)
		return
	}
	uses := p.vars.begin()
	dst, xs := p.setParts(s)
	p.vars.end()
	st := &varStmt{uses: uses, pure: len(dst) == len(xs) && pureValues(xs)}
	for i, d := range s.Dst {
		v, _ := d.(*flow.Var)
		st.targets = append(st.targets, v)
		if _, isNil := s.X[min(i, len(s.X)-1)].(*flow.Nil); isNil && len(s.Dst) > 1 && len(s.Dst) == len(s.X) && v != nil {
			p.vars.pinned[v] = true
		}
	}
	st.text = func(dead func(*flow.Var) bool) string {
		out := slices.Clone(dst)
		for i, v := range st.targets {
			if dead(v) {
				out[i] = "_"
			}
		}
		return strings.Join(out, ", ") + " = " + strings.Join(xs, ", ") + "\n"
	}
	p.b.WriteString(p.vars.add(st))
}

// incStmt writes the increment of the variable v, which is left out if nothing reads v.
func (p *flowPrinter) incStmt(v *flow.Var) {
	if p.dead {
		return
	}
	p.b.WriteString(p.vars.add(&varStmt{targets: []*flow.Var{v}, pure: true, text: func(func(*flow.Var) bool) string {
		return v.Name + "++\n"
	}}))
}

// peekStmt writes the read of the next character into the variables of s, with the blank identifier
// for those nothing reads.
func (p *flowPrinter) peekStmt(s *flow.Peek) {
	guard := "p.Pos < len(p.In)"
	if p.document {
		guard = "p.Document == nil && " + guard
	}
	// The inlined read charges its input work and leaves a Document's examined range to Peek.
	work := "p.InputWork(1)\n"
	if !p.g.flow.opts.Work {
		work = ""
	}
	if p.dead {
		return
	}
	// The statement that a label stands before is not compacted.
	compact := p.b.Len() != p.labelEnd
	st := &varStmt{targets: []*flow.Var{s.Ch, s.Size, s.OK}}
	st.text = func(dead func(*flow.Var) bool) string {
		name := func(v *flow.Var) string {
			if v == nil || dead(v) {
				return "_"
			}
			return v.Name
		}
		ch, size, ok := name(s.Ch), name(s.Size), name(s.OK)
		controlled := fmt.Sprintf("if %s {\n%s%s, %s, %s = p.In[p.Pos], 1, true\n} else {\n%[3]s, %[4]s, %[5]s = p.Peek()\n}\n", guard, work, ch, size, ok)
		if compact {
			// The inlined read and its fallback are one call of the runtime, which the compiler inlines.
			return p.g.form(controlled, fmt.Sprintf("%s, %s, %s = p.NextOrdinary()\n", ch, size, ok))
		}
		return p.g.form(controlled, fmt.Sprintf("if %s {\n%s, %s, %s = p.In[p.Pos], 1, true\n} else {\n%[2]s, %[3]s, %[4]s = p.PeekOrdinary()\n}\n", guard, ch, size, ok))
	}
	p.b.WriteString(p.vars.add(st))
}

// fixedExpNames names the expectations of the runtime that every parser has, by ID.
var fixedExpNames = []string{"IdAny", "IdBeginInput", "IdEndInput", "IdBeginLine", "IdEndLine"}

// exp returns the Go expression of the expectation id: the name of a fixed one, otherwise its ID
// in the parser's table.
func (p *flowPrinter) exp(id core.ExpID) string {
	if int(id) < len(fixedExpNames) {
		return fixedExpNames[id]
	}
	return strconv.Itoa(int(p.desc(id)))
}

// desc returns the ID in the parser's table of the expectation id of the module.
func (p *flowPrinter) desc(id core.ExpID) expID { return p.g.desc(p.g.flow.m.Exps[id]) }

// grammarClass returns the character class expression of the class c.
func grammarClass(c *core.Class) *grammar.CharClass {
	e := &grammar.CharClass{Negated: c.Negated}
	for _, r := range c.Ranges {
		e.Ranges = append(e.Ranges, grammar.CharRange{Lo: r.Lo, Hi: r.Hi})
	}
	return e
}

// reject returns the condition under which the code point in the variable ch is not in the class.
func (p *flowPrinter) reject(c *core.Class, ch *flow.Var) string {
	cond := p.g.controlledClassReject(grammarClass(c))
	if ch.Name != "ch" {
		cond = chIdent.ReplaceAllString(cond, ch.Name)
	}
	return cond
}

// shaper returns the package variable that holds the shaper of the shape.
func (p *flowPrinter) shaper(sh core.Shape) string {
	return p.g.shaperOf(fmt.Sprintf("Shaper{Elems: %v, K: %d, Mode: %d, Tuple: %v, Shared: %v}", sh.Elems, sh.K, sh.Mode, sh.Tuple, sh.Shared))
}

// caps sets the capture variables of the generator's terms to the captures c and returns the
// function that restores them.
func (p *flowPrinter) caps(c flow.Captures) func() {
	caps := map[string]string{}
	for i, v := range c.Vars {
		if v != nil {
			caps[c.Names[i]] = v.Name
		}
	}
	prev := p.g.caps
	p.g.caps = caps
	return func() { p.g.caps = prev }
}

func (p *flowPrinter) expr(x flow.Expr) string {
	g := p.g
	switch x := x.(type) {
	case *flow.Use:
		return x.V.Name
	case *flow.Read:
		switch x.R {
		case flow.RegEnv:
			return "p.Env"
		case flow.RegFrame:
			return "p.Frame"
		case flow.RegCut:
			return "p.Cut"
		}
		return "p.Pos"
	case *flow.RecLen:
		return "len(p.Recovered)"
	case *flow.KidsLen:
		return "len(p.KidStack)"
	case *flow.StreamOn:
		return "p.Stream != nil && p.Stream.Emit != nil && p.Depth == 1"
	case *flow.Nil:
		return "nil"
	case *flow.Int:
		return strconv.Itoa(x.N)
	case *flow.Bool:
		return strconv.FormatBool(x.B)
	case *flow.Not:
		return p.not(x.X)
	case *flow.Or:
		return p.expr(x.X) + " || " + p.expr(x.Y)
	case *flow.Cmp:
		return p.expr(x.L) + " " + x.Op + " " + p.expr(x.R)
	case *flow.HasLit:
		rs := []rune(x.Lit.Text)
		cond := fmt.Sprintf("p.Pos+%d <= len(p.In)", len(rs))
		if len(rs) == 1 {
			cond = "p.Pos < len(p.In)"
		}
		for i, r := range rs {
			if i == 0 {
				cond += fmt.Sprintf(" && p.In[p.Pos] == %d", r)
			} else {
				cond += fmt.Sprintf(" && p.In[p.Pos+%d] == %d", i, r)
			}
		}
		if p.document {
			return g.formRest("p.Document == nil && (!WorkControlsEnabled || p.Control == nil) && ("+cond+")", "p.Document == nil && ("+cond+")", "p.Document == nil && ("+cond+")")
		}
		if p.untyped() && !g.flow.opts.Work {
			// The Node runtime's generated code of a parser without work controls has no such check.
			return cond
		}
		return g.formRest("(!WorkControlsEnabled || p.Control == nil) && ("+cond+")", cond, cond)
	case *flow.MatchLit:
		if x.Lit.Fold {
			return fmt.Sprintf("%s.%s(%s, %d, false)", p.matcher(), p.helper("MatchLiteralFold"), g.literal(string(foldedRunes([]rune(x.Lit.Text)))), p.desc(x.Lit.Exp))
		}
		lit := g.literal(x.Lit.Text)
		var desc expID
		if x.Lit.Text == "" {
			desc = g.desc(strconv.Quote(""))
		} else {
			desc = p.desc(x.Lit.Exp)
		}
		return fmt.Sprintf("%s.%s(%s, %q, %d, false)", p.matcher(), p.helper("MatchLiteral"), lit, x.Lit.Text, desc)
	case *flow.GuardFails:
		var cond string
		if x.Guard.Lit {
			cond = fmt.Sprintf("ch == %d", x.Guard.Rune)
			if x.Guard.Fold {
				cond = fmt.Sprintf("ch == %d || ch == %d", x.Guard.Rune, x.Guard.Rune-('a'-'A'))
			}
		} else {
			cond = negateCondition(g.controlledClassReject(grammarClass(x.Guard.Class)))
		}
		return fmt.Sprintf("!(%s && (%s)) && %s", x.More.Name, chIdent.ReplaceAllString(cond, x.Ch.Name), depthLimit(x.Guard.Depth))
	case *flow.CallRule:
		return g.ruleCall(g.flow.rules[x.Rule], x.Level)
	case *flow.Isolate:
		return "p.Isolate(p.Pos)"
	case *flow.Unisolate:
		return fmt.Sprintf("p.Unisolate(%s)", p.expr(x.Mark))
	case *flow.Text:
		return fmt.Sprintf("p.Text(%s, p.Pos)", p.expr(x.Start))
	case *flow.LiteralText:
		return fmt.Sprintf("p.LiteralText(%s, %q)", p.expr(x.Start), x.Lit)
	case *flow.Str:
		return strconv.Quote(x.S)
	case *flow.NewMatch:
		return fmt.Sprintf("p.%s(%s, p.Pos, %s, true)", p.g.helper("NewMatch"), p.expr(x.Start), p.expr(x.Text))
	case *flow.NewTerminalNode:
		return fmt.Sprintf("p.%s(%s, %s)", p.g.helper("NewTerminal"), p.expr(x.Start), p.expr(x.Text))
	case *flow.NewBranchNode:
		kind := "RawKindSeq"
		if x.List {
			kind = "RawKindList"
		}
		return fmt.Sprintf("p.%s(%s, %s, %s)", p.g.helper("NewBranch"), kind, p.expr(x.Start), p.expr(x.Kids))
	case *flow.NewNode:
		typ := "tuple"
		if x.List {
			typ = TypeList
		}
		return fmt.Sprintf("p.%s(%q, %s, p.Pos, %s)", p.g.helper("NewNode"), typ, p.expr(x.Start), p.expr(x.Kids))
	case *flow.NewVals:
		if p.untyped() {
			return fmt.Sprintf("p.Nodes(%d)[:0]", x.N)
		}
		return fmt.Sprintf("p.NewVals(%d)[:0]", x.N)
	case *flow.Kids:
		return fmt.Sprintf("p.Kids(%s)", p.expr(x.Base))
	case *flow.ShapeSingle:
		return fmt.Sprintf("p.ShapeSingle(%s, %s, %s)", p.shaper(x.Shape), p.expr(x.X), p.expr(x.Start))
	case *flow.ShapeCapture:
		return fmt.Sprintf("p.ShapeCapture(%s, %s, %s)", p.shaper(x.Shape), p.expr(x.X), p.expr(x.Start))
	case *flow.ScopeValue:
		return fmt.Sprintf("p.ScopeValue(%s, %s, %s)", p.shaper(x.Shape), p.expr(x.X), p.expr(x.Start))
	case *flow.AttachCaptures:
		v := p.expr(x.X)
		name := p.scopeNames(x.Names)
		if x.Frame != nil && p.untyped() {
			// The Node runtime's AttachCaptures takes the frame.
			return fmt.Sprintf("p.AttachCaptures(%s, %s, %s, %s, p.Pos)", v, name, x.Frame.Name, p.expr(x.Start))
		}
		if x.Frame != nil {
			return fmt.Sprintf("p.AttachCaptures(%s, %s, %s.Vals, %s, p.Pos)", v, name, x.Frame.Name, p.expr(x.Start))
		}
		vals := make([]string, len(x.Vals))
		for i, v := range x.Vals {
			vals[i] = v.Name
		}
		return fmt.Sprintf("p.AttachCaptures(%s, %s, []any{%s}, %s, p.Pos)", v, name, strings.Join(vals, ", "), p.expr(x.Start))
	case *flow.MarkState:
		return "p.Mark()"
	case *flow.MarkPos:
		return x.M.Name + ".Pos"
	case *flow.NewFrame:
		return fmt.Sprintf("p.NewFrame(len(%s))", p.scopeNames(x.N))
	case *flow.FrameVal:
		return fmt.Sprintf("%s.Vals[%d]", x.F.Name, x.Slot)
	case *flow.Keep:
		return fmt.Sprintf("p.Keep(%s)", p.expr(x.Exps))
	case *flow.MakeError:
		return fmt.Sprintf("p.MakeError(%s, %s)", p.expr(x.Far), p.expr(x.Exps))
	case *flow.ErrorValue:
		start := p.expr(x.Start)
		return fmt.Sprintf("NewTError(%s, p.Pos, p.Text(%s, p.Pos), p.RecoveryErrorText(%s))", start, start, p.expr(x.X))
	case *flow.NewErrorNode:
		return fmt.Sprintf("p.NewErrorNode(%s, %s)", p.expr(x.Start), p.expr(x.X))
	case *flow.FreshNode:
		return fmt.Sprintf("TFreshNode(%s)", p.expr(x.X))
	case *flow.ConvertResult:
		if x.Owned != nil {
			return fmt.Sprintf("p.TConvertResultOwned(trules[%d], %s, %s)", x.Rule, p.expr(x.X), p.expr(x.Owned))
		}
		return fmt.Sprintf("p.TConvertResult(trules[%d], %s)", x.Rule, p.expr(x.X))
	case *flow.UseCtx:
		return fmt.Sprintf("p.UseCtx(TActCtx{P: p, Start: %s, End: p.Pos, Cbase: len(p.Created)})", p.expr(x.Start))
	case *flow.Eval:
		defer p.caps(x.Caps)()
		g.final = g.cur.action
		defer func() { g.final = nil }()
		return g.term(g.cur.action, newScope(), nil)
	case *flow.CtxFinish:
		return fmt.Sprintf("%s.Finish(%s)", x.C.Name, p.expr(x.X))
	}
	panic(fmt.Sprintf("flow printer: expression %T", x))
}

// not returns the negation of the condition x.
func (p *flowPrinter) not(x flow.Expr) string {
	g := p.g
	switch x := x.(type) {
	case *flow.Use:
		return "!" + x.V.Name
	case *flow.AtAnchor:
		switch x.Kind {
		case core.BeginInput:
			return "p.Pos != 0"
		case core.EndInput:
			return "!p." + g.atEnd() + "()"
		case core.BeginLine:
			return "!p." + g.atLineStart() + "()"
		default:
			return "!p." + g.helper("AtLineEnd") + "()"
		}
	case *flow.MatchClass:
		table := g.classTable(grammarClass(x.Class))
		id := p.desc(x.Class.Exp)
		return fmt.Sprintf("!p.ClassFast(&%s) && !p.ClassSkip(&%s, %d)", table, table, id)
	case *flow.InClass:
		return p.reject(x.Class, x.Ch)
	case *flow.SkipTerminal:
		table, ok := g.terminalTable(g.flow.origin[x.T.Body])
		if !ok {
			panic("flow printer: terminal region without a terminal program")
		}
		return fmt.Sprintf("!p.SkipTerminal(%s)", table)
	case *flow.SkipClasses:
		items := make([]grammar.Expr, len(x.Run.Classes))
		for i, c := range x.Run.Classes {
			items[i] = grammarClass(c)
		}
		return fmt.Sprintf("!p.ClassesSkip(%s)", g.classSteps(items))
	case *flow.Pred:
		if x.Caps.Vars == nil {
			// The term reads the current frame.
			return fmt.Sprintf("!p.Predicate(func(c *%s) any { return %s })", g.ctxType(), g.predicateTerm(p.term(x.Node), frameScope(x.Caps), nil))
		}
		defer p.caps(x.Caps)()
		return fmt.Sprintf("!p.Predicate(func(c *TActCtx) any { return %s })", g.predicateTerm(p.term(x.Node), newScope(), nil))
	case *flow.Bind:
		b := p.term(x.Node).(*grammar.Bind)
		if x.Caps.Vars == nil {
			return fmt.Sprintf("!p.Bind(%q, func(c *%s) any { return %s })", b.Name, g.ctxType(), g.term(b.Value, frameScope(x.Caps), nil))
		}
		defer p.caps(x.Caps)()
		return fmt.Sprintf("!p.Bind(%q, func(c *TActCtx) any { return %s })", b.Name, g.term(b.Value, newScope(), nil))
	}
	return "!" + p.expr(x)
}

// term returns the term of the grammar predicate that the node n is built from.
func (p *flowPrinter) term(n core.Node) grammar.Term {
	e, ok := p.g.flow.origin[n].(*grammar.Predicate)
	if !ok {
		panic(fmt.Sprintf("flow printer: no grammar predicate for %T", n))
	}
	return e.Term
}

// frameScope returns the scope of the frame whose slots the captures c name.
func frameScope(c flow.Captures) *scope {
	s := newScope()
	for _, name := range c.Names {
		s.slot(name)
	}
	return s
}
