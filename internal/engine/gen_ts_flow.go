package engine

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/engine/flow"
)

// Value-free rules of the TypeScript runtime printed from Flow IR.
//
// The lean rules that recFlowEligible accepts are lowered by flow.Lower (with the options of the
// untyped tables, except for the Go-only forms: in-place literal reads and class tables) and
// printed by tsFlowPrinter as one function each. A block becomes a labeled block and a loop a labeled
// for statement, so that an exit leaves the construct it names with break or continue. A rule
// that Flow IR or the printer does not cover keeps the lowering of tsGen.expr.

// tsFlowUnsupported is the panic value of the printer for a construct it does not print; the rule
// then keeps its other lowering.
type tsFlowUnsupported string

// startTSFlow builds the Core IR of the table being generated for printing its eligible rules from
// Flow IR. It reports whether Flow IR is in use for the table.
func (g *tsGen) startTSFlow() bool {
	g.flow = nil
	if !g.tsFlow {
		return false
	}
	if err := g.startRecFlow(g.prog); err != nil {
		return false
	}
	// The generated TypeScript reads input through the runtime's peek and matches literals through
	// matchLiteral: it has no in-place reads and no class tables.
	g.flow.opts.GuardedClasses = false
	g.flow.opts.InlineLiterals = 0
	g.flow.opts.ClassTables = false
	return true
}

// flowRule prints the lean rule r from Flow IR as a function of its own and returns the call
// expression that runs it, or false if r is not printed from Flow IR.
func (g *tsGen) flowRule(r *rule) (call string, ok bool) {
	if g.flow == nil || !recFlowEligible(r) {
		return "", false
	}
	cr := g.flow.m.Rules[r.id]
	if !flow.LocalRule(cr) {
		return "", false
	}
	f, err := flow.Lower(g.flow.m, cr, r.name, g.flow.opts)
	if err != nil {
		panic(fmt.Errorf("rule %s of table %s: %w", r.name, g.table, err)) // LocalRule holds
	}
	if f.Binds {
		return "", false
	}
	defer func() {
		if e := recover(); e != nil {
			if _, unsupported := e.(tsFlowUnsupported); !unsupported {
				panic(e)
			}
			call, ok = "", false
		}
	}()
	body, vars, decls := g.printTSFlow(f)
	var b strings.Builder
	for _, v := range vars {
		fmt.Fprintf(&b, "  let %s: %s = %s;\n", v.Name, tsFlowType(v.Type), tsFlowZero(v.Type))
	}
	b.WriteString(indent(body))
	for _, d := range decls {
		if tsNamesIdentifier(b.String(), d.name) {
			g.vars.WriteString(d.text)
		}
	}
	name, shared := g.methodName("Parser/R", b.String())
	if !shared {
		param := "p"
		if !strings.Contains(b.String(), "p.") && !strings.Contains(b.String(), "(p)") {
			param = "_p"
		}
		fmt.Fprintf(&g.methods, "// %s\nfunction %s(%s: Parser): R {\n%s}\n\n", describe(r.bodyExpr()), name, param, b.String())
	}
	return name + "(p)", true
}

func tsFlowType(t flow.Type) string {
	switch t {
	case flow.TypeInt, flow.TypeRune:
		return "number"
	case flow.TypeBool:
		return "boolean"
	}
	panic(tsFlowUnsupported("variable of type " + t.String()))
}

func tsFlowZero(t flow.Type) string {
	if t == flow.TypeBool {
		return "false"
	}
	return "0"
}

// tsFlowPrinter prints the body of a Flow IR function.
type tsFlowPrinter struct {
	g *tsGen
	b strings.Builder
	// dead reports that the current point is unreachable. used holds the labels exited to from
	// reachable points.
	dead bool
	used map[*flow.Label]bool
	// reads holds the variables read at reachable points (nil before the first pass: all are read).
	reads map[*flow.Var]bool
	// next collects the reads of the pass being printed, and assigned the variables it assigns.
	next, assigned map[*flow.Var]bool
	// dry reports a pass that only collects reads: it writes no declarations.
	dry    bool
	labels map[*flow.Label]string
	failed bool
	// decls holds the constants the printed text names, which the caller declares if it keeps the text.
	decls []tsDecl
}

// tsDecl is a package-level constant of a printed function.
type tsDecl struct{ name, text string }

// printTSFlow returns the statements of the function f, ending with its return, and the variables
// the statements declare.
func (g *tsGen) printTSFlow(f *flow.Func) (string, []*flow.Var, []tsDecl) {
	var reads map[*flow.Var]bool
	for i := 0; ; i++ {
		p := &tsFlowPrinter{g: g, dry: true, reads: reads, next: map[*flow.Var]bool{}, assigned: map[*flow.Var]bool{}, used: map[*flow.Label]bool{}, labels: map[*flow.Label]string{}}
		p.function(f)
		if len(p.next) == len(reads) && reads != nil || i > 8 {
			break
		}
		reads = p.next
	}
	if reads == nil {
		reads = map[*flow.Var]bool{}
	}
	p := &tsFlowPrinter{g: g, reads: reads, next: map[*flow.Var]bool{}, assigned: map[*flow.Var]bool{}, used: map[*flow.Label]bool{}, labels: map[*flow.Label]string{}}
	p.function(f)
	if p.next[f.Start] || p.next[f.Ctx] {
		panic(tsFlowUnsupported("the start or the context of a value-free rule"))
	}
	var vars []*flow.Var
	for _, v := range f.Vars {
		if (p.next[v] || p.assigned[v]) && p.next[v] && !v.Entry {
			vars = append(vars, v)
		}
	}
	return p.b.String(), vars, p.decls
}

func (p *tsFlowPrinter) function(f *flow.Func) {
	p.stmts(f.Body)
	var inner strings.Builder
	if !p.dead {
		inner.WriteString("return null;\n")
	}
	if p.used[f.Fail] {
		body := p.b.String()
		p.b.Reset()
		fmt.Fprintf(&p.b, "%s: {\n%s%s}\nreturn undefined;\n", p.label(f.Fail), indent(body), indent(inner.String()))
	} else {
		p.b.WriteString(inner.String())
	}
}

func (p *tsFlowPrinter) label(l *flow.Label) string {
	if n, ok := p.labels[l]; ok {
		return n
	}
	n := "L" + strconv.Itoa(len(p.labels)+1)
	if l.Name != "" {
		n = "L_" + l.Name
	}
	for _, other := range p.labels {
		if other == n {
			n += "_" + strconv.Itoa(len(p.labels)+1)
		}
	}
	p.labels[l] = n
	return n
}

func (p *tsFlowPrinter) write(s string) {
	if !p.dead && !p.dry {
		p.b.WriteString(s)
	}
}

// read records the variables that x reads at a reachable point.
func (p *tsFlowPrinter) read(xs ...flow.Expr) {
	if p.dead {
		return
	}
	for _, x := range xs {
		if x != nil {
			flow.Uses(x, func(v *flow.Var) { p.next[v] = true })
		}
	}
}

func (p *tsFlowPrinter) stmts(ss []flow.Stmt) {
	for _, s := range ss {
		p.stmt(s)
	}
}

// isRead reports whether v is read, by the previous pass.
func (p *tsFlowPrinter) isRead(v *flow.Var) bool {
	return v != nil && (p.reads == nil || p.reads[v])
}

// pure reports whether evaluating x has no effect, so that an assignment of it to nothing read is
// left out.
func tsFlowPure(x flow.Expr) bool {
	switch x := x.(type) {
	case *flow.Use, *flow.Read, *flow.Int, *flow.Bool, *flow.Nil, *flow.RecLen, *flow.KidsLen, *flow.InClass, *flow.GuardFails:
		return true
	case *flow.Not:
		return tsFlowPure(x.X)
	case *flow.Or:
		return tsFlowPure(x.X) && tsFlowPure(x.Y)
	case *flow.Cmp:
		return tsFlowPure(x.L) && tsFlowPure(x.R)
	}
	return false
}

// scoped prints the statements of a construct with the label l into a buffer of their own and
// reports whether l was exited to.
func (p *tsFlowPrinter) scoped(body func()) string {
	outer := p.b.String()
	p.b.Reset()
	body()
	out := p.b.String()
	p.b.Reset()
	p.b.WriteString(outer)
	return out
}

func indent(s string) string {
	if s == "" {
		return ""
	}
	lines := strings.SplitAfter(s, "\n")
	var b strings.Builder
	for _, l := range lines {
		if l != "" {
			b.WriteString("  ")
			b.WriteString(l)
		}
	}
	return b.String()
}

func (p *tsFlowPrinter) stmt(s flow.Stmt) {
	switch s := s.(type) {
	case *flow.Block:
		enter := p.dead
		body := p.scoped(func() { p.stmts(s.Body) })
		end := p.dead
		p.dead = enter
		if p.used[s.Label] {
			p.write(fmt.Sprintf("%s: {\n%s}\n", p.label(s.Label), indent(body)))
			p.dead = false
		} else {
			p.write(body)
			p.dead = end
		}
	case *flow.Loop:
		var head string
		switch {
		case s.Counter != nil:
			n := s.Counter.Name
			p.next[s.Counter] = true
			head = fmt.Sprintf("for (%s = 0; %s < %d; %s++)", n, n, s.Max, n)
		case s.While != nil:
			p.read(s.While)
			head = "while (" + p.expr(s.While) + ")"
		default:
			head = "for (;;)"
		}
		enter := p.dead
		body := p.scoped(func() { p.stmts(s.Body) })
		label := ""
		if p.used[s.Label] {
			label = p.label(s.Label) + ": "
		}
		if !enter && !p.dry {
			p.b.WriteString(label + head + " {\n" + indent(body) + "}\n")
		}
		p.dead = enter || (s.While == nil && s.Counter == nil && !p.used[s.Label])
	case *flow.If:
		p.ifStmt(s)
	case *flow.Exit:
		if p.dead {
			return
		}
		p.used[s.To] = true
		p.write("break " + p.label(s.To) + ";\n")
		p.dead = true
	case *flow.Continue:
		p.used[s.To] = true
		p.write("continue " + p.label(s.To) + ";\n")
		p.dead = true
	case *flow.Set:
		p.set(s)
	case *flow.Inc:
		if p.isRead(s.V) {
			p.next[s.V] = true
			p.write(s.V.Name + "++;\n")
		}
	case *flow.Step:
		p.write("p.stepWork();\n")
	case *flow.Silent:
		if s.Delta > 0 {
			p.write("p.silent++;\n")
		} else {
			p.write("p.silent--;\n")
		}
	case *flow.Peek:
		p.peek(s)
	case *flow.Advance:
		p.read(s.N)
		p.write("p.pos += " + p.expr(s.N) + ";\n")
	case *flow.Expect:
		p.read(s.At)
		if s.Message {
			p.write(fmt.Sprintf("p.expect(%s, msgBit | %d);\n", p.expr(s.At), p.desc(s.Exp)))
		} else {
			p.write(fmt.Sprintf("p.expect(%s, %s);\n", p.expr(s.At), p.exp(s.Exp)))
		}
	case *flow.Restore:
		p.read(s.Pos, s.Rec)
		p.write(fmt.Sprintf("p.pos = %s;\np.recovered.length = %s;\n", p.expr(s.Pos), p.expr(s.Rec)))
	case *flow.Comment:
		p.write("// " + s.Text + "\n")
	case *flow.ObserveSkip:
		if p.dead || p.dry {
			return
		}
		trace := p.g.traceSkipped(p.g.flow.origin[s.Alt])
		p.b.WriteString(strings.TrimLeft(trace, " "))
	default:
		panic(tsFlowUnsupported(fmt.Sprintf("statement %T", s)))
	}
}

func (p *tsFlowPrinter) ifStmt(s *flow.If) {
	dead := p.dead
	var init string
	if s.Init != nil {
		init = p.setText(s.Init, true)
	}
	p.read(s.Cond)
	cond := p.expr(s.Cond)
	then := p.scoped(func() { p.stmts(s.Then) })
	p.dead = dead
	var els string
	if len(s.Else) > 0 {
		els = p.scoped(func() { p.stmts(s.Else) })
		p.dead = dead
	}
	if dead || p.dry {
		return
	}
	p.b.WriteString(init)
	p.b.WriteString("if (" + cond + ") {\n" + indent(then) + "}")
	if els != "" {
		p.b.WriteString(" else {\n" + indent(els) + "}")
	}
	p.b.WriteString("\n")
}

// set writes the assignment s.
func (p *tsFlowPrinter) set(s *flow.Set) {
	if p.dead {
		return
	}
	if text := p.setText(s, true); text != "" {
		p.write(text)
	}
}

// setText returns the assignment s as TypeScript, or "" if it is left out. As a statement it ends
// with a semicolon and a newline.
func (p *tsFlowPrinter) setText(s *flow.Set, stmt bool) string {
	end := ""
	if stmt {
		end = ";\n"
	}
	used := false
	for _, d := range s.Dst {
		switch d := d.(type) {
		case *flow.Var:
			used = used || p.isRead(d)
		default:
			used = true
		}
	}
	if len(s.X) == 1 && len(s.Dst) == 2 && flow.Results(s.X[0]) == 2 {
		// A call with two results: the value and whether it matched.
		p.read(s.X[0])
		x := p.expr(s.X[0])
		vv, _ := s.Dst[0].(*flow.Var)
		ov, _ := s.Dst[1].(*flow.Var)
		valRead, okRead := p.isRead(vv), p.isRead(ov)
		p.note(vv)
		p.note(ov)
		switch {
		case valRead && okRead:
			return fmt.Sprintf("%s = %s%s%s = %s !== undefined%s", vv.Name, x, stmtSep(stmt), ov.Name, vv.Name, end)
		case valRead:
			return fmt.Sprintf("%s = %s%s", vv.Name, x, end)
		case okRead:
			return fmt.Sprintf("%s = %s !== undefined%s", ov.Name, x, end)
		}
		return x + end
	}
	if !used {
		allPure := true
		for _, x := range s.X {
			allPure = allPure && tsFlowPure(x)
		}
		if allPure {
			return ""
		}
	}
	if len(s.Dst) != len(s.X) {
		panic(tsFlowUnsupported("an assignment of a call with several results"))
	}
	var parts []string
	for i, d := range s.Dst {
		switch d := d.(type) {
		case *flow.Var:
			if !p.isRead(d) && tsFlowPure(s.X[i]) {
				continue
			}
			p.read(s.X[i])
			x := p.expr(s.X[i])
			if p.isRead(d) {
				p.note(d)
				parts = append(parts, d.Name+" = "+x)
			} else {
				parts = append(parts, x)
			}
		case flow.Reg:
			p.read(s.X[i])
			parts = append(parts, p.reg(d)+" = "+p.expr(s.X[i]))
		case *flow.Discard:
			if !tsFlowPure(s.X[i]) {
				p.read(s.X[i])
				parts = append(parts, p.expr(s.X[i]))
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, stmtSep(stmt)) + end
}

func stmtSep(stmt bool) string {
	if stmt {
		return ";\n"
	}
	return ", "
}

// note records that v is assigned at a reachable point: it is declared if something reads it.
func (p *tsFlowPrinter) note(v *flow.Var) {
	if v != nil && p.isRead(v) {
		p.assigned[v] = true
	}
}

func (p *tsFlowPrinter) reg(r flow.Reg) string {
	switch r {
	case flow.RegPos:
		return "p.pos"
	case flow.RegEnv:
		return "p.env"
	case flow.RegFrame:
		return "p.frame"
	case flow.RegCut:
		return "p.cut"
	}
	panic(tsFlowUnsupported("register"))
}

// peek writes the read of the next character.
func (p *tsFlowPrinter) peek(s *flow.Peek) {
	if p.dead {
		return
	}
	ch, size, ok := s.Ch, s.Size, s.OK
	wantCh, wantSize, wantOK := p.isRead(ch), p.isRead(size), p.isRead(ok)
	var b strings.Builder
	switch {
	case wantCh:
		fmt.Fprintf(&b, "%s = p.peek();\n", ch.Name)
		p.note(ch)
		if wantOK {
			fmt.Fprintf(&b, "%s = %s >= 0;\n", ok.Name, ch.Name)
			p.note(ok)
		}
	case wantOK:
		fmt.Fprintf(&b, "%s = p.peek() >= 0;\n", ok.Name)
		p.note(ok)
	default:
		b.WriteString("p.peek();\n")
	}
	if wantSize {
		fmt.Fprintf(&b, "%s = p.psize;\n", size.Name)
		p.note(size)
	}
	p.write(b.String())
}

var tsFixedExpNames = []string{"idAny", "idBeginInput", "idEndInput", "idBeginLine", "idEndLine"}

// exp returns the TypeScript expression of the expectation id: the name of a fixed one, otherwise its
// ID in the parser's table.
func (p *tsFlowPrinter) exp(id core.ExpID) string {
	if int(id) < len(tsFixedExpNames) {
		return tsFixedExpNames[id]
	}
	return strconv.Itoa(int(p.desc(id)))
}

func (p *tsFlowPrinter) desc(id core.ExpID) expID { return p.g.desc(p.g.flow.m.Exps[id]) }

// classIn returns the condition that the code point in ch is in the class c (false at the end of the
// input is the caller's check).
func tsClassIn(c *core.Class, ch string) string {
	var in []string
	for _, r := range c.Ranges {
		if r.Lo == r.Hi {
			in = append(in, fmt.Sprintf("%s === %d", ch, r.Lo))
		} else {
			in = append(in, fmt.Sprintf("(%s >= %d && %s <= %d)", ch, r.Lo, ch, r.Hi))
		}
	}
	if len(in) == 0 {
		in = []string{ch + " < 0"} // never true; mentions ch so that it is used
	}
	cond := tsJoinCondition(in, " || ")
	if c.Negated {
		return "!" + wrap(cond)
	}
	return cond
}

func wrap(cond string) string {
	if strings.HasPrefix(cond, "(") && strings.HasSuffix(cond, ")") {
		return cond
	}
	return "(" + cond + ")"
}

func (p *tsFlowPrinter) expr(x flow.Expr) string {
	g := p.g
	switch x := x.(type) {
	case *flow.Use:
		return x.V.Name
	case *flow.Read:
		return p.reg(x.R)
	case *flow.RecLen:
		return "p.recovered.length"
	case *flow.KidsLen:
		return "p.kidStack.length"
	case *flow.Nil:
		return "null"
	case *flow.Int:
		return strconv.Itoa(x.N)
	case *flow.Bool:
		return strconv.FormatBool(x.B)
	case *flow.Not:
		inner := p.expr(x.X)
		if _, ok := x.X.(*flow.Use); ok {
			return "!" + inner
		}
		return "!" + wrap(inner)
	case *flow.Or:
		return "(" + p.expr(x.X) + " || " + p.expr(x.Y) + ")"
	case *flow.Cmp:
		op := x.Op
		switch op {
		case "==":
			op = "==="
		case "!=":
			op = "!=="
		}
		return p.expr(x.L) + " " + op + " " + p.expr(x.R)
	case *flow.MatchLit:
		if x.Lit.Fold {
			folded := string(foldedRunes([]rune(x.Lit.Text)))
			if p.dry {
				return ""
			}
			v, shared := g.literalName(folded)
			if !shared {
				p.decl(v, fmt.Sprintf("const %s: Lit = { text: %s, cps: %s, bytes: %s };\n", v, tsString(folded), tsInts([]rune(folded)), tsInts([]byte(folded))))
			}
			return fmt.Sprintf("p.matchLiteralFold(%s, %d, false)", v, p.desc(x.Lit.Exp))
		}
		if p.dry {
			return ""
		}
		value := literalValue(x.Lit.Text)
		v, shared := g.literalName(value)
		if !shared {
			p.decl(v, fmt.Sprintf("const %s: Lit = { text: %s, cps: %s, bytes: %s };\n", v, tsString(value), tsInts([]rune(value)), tsInts([]byte(value))))
		}
		var desc expID
		if x.Lit.Text == "" {
			desc = g.desc(strconv.Quote(""))
		} else {
			desc = p.desc(x.Lit.Exp)
		}
		return fmt.Sprintf("p.matchLiteral(%s, %d, false)", v, desc)
	case *flow.GuardFails:
		var cond string
		if x.Guard.Lit {
			cond = fmt.Sprintf("%s === %d", x.Ch.Name, x.Guard.Rune)
			if x.Guard.Fold {
				cond = fmt.Sprintf("%s === %d || %s === %d", x.Ch.Name, x.Guard.Rune, x.Ch.Name, x.Guard.Rune-('a'-'A'))
			}
		} else {
			cond = tsClassIn(x.Guard.Class, x.Ch.Name)
		}
		depth := "p.depth <= p.maxDepth"
		if x.Guard.Depth != 0 {
			depth = fmt.Sprintf("p.depth + %d <= p.maxDepth", x.Guard.Depth)
		}
		return fmt.Sprintf("!(%s && (%s)) && %s", x.More.Name, cond, depth)
	case *flow.InClass:
		return tsClassIn(x.Class, x.Ch.Name)
	case *flow.AtAnchor:
		switch x.Kind {
		case core.BeginInput:
			return "p.pos === 0"
		case core.EndInput:
			return "p.atEnd()"
		case core.BeginLine:
			return "p.atLineStart()"
		default:
			return "p.atLineEnd()"
		}
	case *flow.CallRule:
		return p.call(g.flow.rules[x.Rule], x.Level)
	}
	panic(tsFlowUnsupported(fmt.Sprintf("expression %T", x)))
}

// decl declares a package-level constant of the printed function, once per pass that prints.
func (p *tsFlowPrinter) decl(name, s string) {
	p.decls = append(p.decls, tsDecl{name, s})
}

// call returns the call of the rule r from the Operators Rule level min, as tsGen.expr writes a
// reference.
func (p *tsFlowPrinter) call(r *rule, min int) string {
	g := p.g
	_, operatorsRule := r.def.Expr.(*grammar.OperatorsRule)
	switch {
	case r.plain && !operatorsRule:
		g.plainUsed[r] = true
		return fmt.Sprintf("%s%d(p)", g.plainPrefix(), r.id)
	case r.plain:
		return fmt.Sprintf("p.invokePlain(%s, %d)", g.ruleVar(r), min)
	}
	return fmt.Sprintf("p.call(%s, %d)", g.ruleVar(r), min)
}

// tsNamesIdentifier reports whether the code uses the identifier name.
func tsNamesIdentifier(code, name string) bool {
	for i := 0; ; {
		j := strings.Index(code[i:], name)
		if j < 0 {
			return false
		}
		j += i
		end := j + len(name)
		if end >= len(code) || !(code[end] >= '0' && code[end] <= '9' || code[end] >= 'a' && code[end] <= 'z' || code[end] >= 'A' && code[end] <= 'Z' || code[end] == '_') {
			return true
		}
		i = j + 1
	}
}
