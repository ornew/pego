package engine

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/engine/flow"
)

// Typed Go rules printed from Flow IR.
//
// With GenOptions.flowTyped, in the parsers whose options flowSupported accepts, the typed rules
// that flow.LocalRule accepts are built as Core IR (typedCore), lowered to Flow IR (flow.Lower)
// and printed here into the direct rule that typedCall and directMethod wrap. The other rules
// keep the direct rules of dgen and the general code.
//
// The printer follows reachability as dgen does: a statement at an unreachable point is left out,
// but its text is still formed, so that the package variables it names (literals, expectations,
// class tables, shapers, scopes) are declared in the same order. The variables of the function
// that no printed statement reads are assigned to the blank identifier on the first line.

// flowState holds the Core IR of the typed rules while they are printed from Flow IR.
type flowState struct {
	m      *core.Module
	origin map[core.Node]grammar.Expr
	opts   flow.Options
	// rules holds the rules of the program by ID, value-free twins included.
	rules []*rule
}

// flowSupported reports whether flowTyped covers the generator's options: lean output without
// observability, with the typed cuts and local capture layouts that dgen uses.
func (g *generator) flowSupported() bool {
	return g.lean && !g.observability && !g.document && !g.directTypedDocument &&
		!g.disableTypedCuts && !g.disableTypedLocalLayouts
}

// startFlow builds the Core IR of the typed rules for printing them from Flow IR.
func (g *generator) startFlow() error {
	origin := map[core.Node]grammar.Expr{}
	m, err := g.typedCore(origin)
	if err != nil {
		return err
	}
	g.flow = &flowState{m: m, origin: origin, rules: append(append([]*rule(nil), g.prog.rules...), g.prog.twins...),
		opts: flow.Options{Work: !g.noWork, Recovers: flow.Recovers(m), ClassTables: g.leanTyped(),
			GuardedClasses: g.lean && !g.disableGuardedClasses, InlineLiterals: maxInlineLiteral}}
	return nil
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
		panic(err) // LocalRule holds
	}
	g.cur, g.proj = r, g.projections(r)
	defer func() { g.proj = nil }()
	p := &flowPrinter{g: g, used: map[*flow.Label]bool{}, reads: map[*flow.Var]bool{}, loops: map[*flow.Label]bool{}}
	p.stmts(f.Body)
	out := &direct{fail: p.used[f.Fail], succeed: !p.dead, binds: f.Binds, start: p.reads[f.Start], ctx: p.reads[f.Ctx]}
	var unread []string
	for _, v := range f.Vars {
		out.vars = append(out.vars, dvar{v.Name, goType(v.Type)})
		if !p.reads[v] {
			unread = append(unread, v.Name)
		}
	}
	out.body = p.b.String()
	if len(unread) > 0 {
		out.body = "\t" + strings.Repeat("_, ", len(unread)-1) + "_ = " + strings.Join(unread, ", ") + "\n" + out.body
	}
	g.flowRules++
	return out
}

func goType(t flow.Type) string {
	return [...]string{flow.TypeInt: "int", flow.TypeBool: "bool", flow.TypeRune: "rune", flow.TypeValue: "any",
		flow.TypeEnv: "*Env", flow.TypeExpMark: "ExpMark", flow.TypeExpIDs: "[]ExpID", flow.TypeVals: "[]any",
		flow.TypeCtx: "*TActCtx"}[t]
}

// flowPrinter prints the body of a Flow IR function as the statements of a direct rule.
type flowPrinter struct {
	g *generator
	b strings.Builder
	// dead reports that the current point is unreachable. used holds the labels exited to from
	// reachable points, reads the variables read there, and loops the labels of loops.
	dead  bool
	used  map[*flow.Label]bool
	reads map[*flow.Var]bool
	loops map[*flow.Label]bool
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
		flow.Uses(x, func(v *flow.Var) { p.reads[v] = true })
	}
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
			fmt.Fprintf(&p.b, "%s:\n", s.Label.Name)
			p.dead = false
		}
	case *flow.Loop:
		p.loops[s.Label] = true
		// The braces of a loop are written whatever the reachability.
		switch {
		case s.Counter != nil:
			n := s.Counter.Name
			if !p.dead {
				p.reads[s.Counter] = true
			}
			fmt.Fprintf(&p.b, "for %s = 0; %s < %d; %s++ {\n", n, n, s.Max, n)
		case s.While != nil:
			p.read(s.While)
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
			fmt.Fprintf(&p.b, "goto %s\n", s.To.Name)
		}
		p.dead = true
	case *flow.Continue:
		p.write("continue\n")
		p.dead = true
	case *flow.Set:
		p.write(p.set(s) + "\n")
	case *flow.Inc:
		p.write(s.V.Name + "++\n")
	case *flow.Step:
		p.write("p.StepWork()\n")
	case *flow.Silent:
		if s.Delta > 0 {
			p.write("p.Silent++\n")
		} else {
			p.write("p.Silent--\n")
		}
	case *flow.Peek:
		ch, size := "_", "_"
		if s.Ch != nil {
			ch = s.Ch.Name
		}
		if s.Size != nil {
			size = s.Size.Name
		}
		guard := "p.Pos < len(p.In)"
		if p.g.document {
			guard = "p.Document == nil && " + guard
		}
		work := "p.InputWork(1)\n"
		if p.g.noWork {
			work = ""
		}
		p.write(fmt.Sprintf("if %s {\n%s%s, %s, %s = p.In[p.Pos], 1, true\n} else {\n%[3]s, %[4]s, %[5]s = p.Peek()\n}\n", guard, work, ch, size, s.OK.Name))
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
			p.write(fmt.Sprintf("p.Expect(%s, MsgBit|%d)\n", p.expr(s.At), p.desc(s.Exp)))
		} else {
			p.write(fmt.Sprintf("p.Expect(%s, %s)\n", p.expr(s.At), p.exp(s.Exp)))
		}
	case *flow.Restore:
		p.read(s.Pos, s.Rec)
		p.write(fmt.Sprintf("p.Restore(%s, %s)\n", p.expr(s.Pos), p.expr(s.Rec)))
	case *flow.MergeExpected:
		p.read(s.Far, s.Exps)
		p.write(fmt.Sprintf("p.MergeExpected(%s, %s)\n", p.expr(s.Far), p.expr(s.Exps)))
	case *flow.MergeRenamed:
		p.read(s.Far, s.Exps)
		p.write(fmt.Sprintf("p.MergeExpected(%s, RenamedExpected(p.Parser, %s, %d))\n", p.expr(s.Far), p.expr(s.Exps), p.desc(s.Exp)))
	case *flow.PushKid:
		p.read(s.X)
		p.write(fmt.Sprintf("p.PushKid(%s)\n", p.expr(s.X)))
	case *flow.DropKids:
		p.read(s.Base)
		p.write(fmt.Sprintf("p.DropKids(%s)\n", p.expr(s.Base)))
	case *flow.Append:
		p.read(&flow.Use{V: s.Vals}, s.X)
		x := p.expr(s.X)
		if s.Spread {
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

// set returns the assignment s.
func (p *flowPrinter) set(s *flow.Set) string {
	dst := make([]string, len(s.Dst))
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
	xs := make([]string, len(s.X))
	for i, x := range s.X {
		p.read(x)
		xs[i] = p.expr(x)
	}
	return strings.Join(dst, ", ") + " = " + strings.Join(xs, ", ")
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
		if x.R == flow.RegEnv {
			return "p.Env"
		}
		return "p.Pos"
	case *flow.RecLen:
		return "len(p.Recovered)"
	case *flow.KidsLen:
		return "len(p.KidStack)"
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
		if g.document {
			return "p.Document == nil && (!WorkControlsEnabled || p.Control == nil) && (" + cond + ")"
		}
		return "(!WorkControlsEnabled || p.Control == nil) && (" + cond + ")"
	case *flow.MatchLit:
		lit := g.literal(x.Lit.Text)
		var desc expID
		if x.Lit.Text == "" {
			desc = g.desc(strconv.Quote(""))
		} else {
			desc = p.desc(x.Lit.Exp)
		}
		return fmt.Sprintf("p.Parser.MatchLiteral(%s, %q, %d, false)", lit, x.Lit.Text, desc)
	case *flow.GuardFails:
		var cond string
		if x.Guard.Lit {
			cond = fmt.Sprintf("ch == %d", x.Guard.Rune)
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
		return fmt.Sprintf("p.NewMatch(%s, p.Pos, %s, true)", p.expr(x.Start), p.expr(x.Text))
	case *flow.NewNode:
		typ := "tuple"
		if x.List {
			typ = TypeList
		}
		return fmt.Sprintf("p.NewNode(%q, %s, p.Pos, %s)", typ, p.expr(x.Start), p.expr(x.Kids))
	case *flow.NewVals:
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
		name := g.name("scope")
		fmt.Fprintf(&g.vars, "var %s = %s\n", name, goStrings(x.Names))
		vals := make([]string, len(x.Vals))
		for i, v := range x.Vals {
			vals[i] = v.Name
		}
		return fmt.Sprintf("p.AttachCaptures(%s, %s, []any{%s}, %s, p.Pos)", v, name, strings.Join(vals, ", "), p.expr(x.Start))
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
			return "!p.AtLineEnd()"
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
		defer p.caps(x.Caps)()
		return fmt.Sprintf("!p.Predicate(func(c *TActCtx) any { return %s })", g.predicateTerm(p.term(x.Node), newScope(), nil))
	case *flow.Bind:
		defer p.caps(x.Caps)()
		b := p.term(x.Node).(*grammar.Bind)
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
