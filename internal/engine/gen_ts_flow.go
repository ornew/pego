package engine

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/ir/core"
	"github.com/ornew/pego/internal/ir/flow"
	"github.com/ornew/pego/internal/value"
)

// Value-free rules of the TypeScript runtime printed from Flow IR.
//
// The rules are lowered by flow.Lower (with the options of the untyped tables, except for the
// Go-only forms: in-place literal reads and class tables) and printed by tsFlowPrinter as one
// function each. A block becomes a labeled block and a loop a labeled
// for statement, so that an exit leaves the construct it names with break or continue. A rule
// that Flow IR or the printer does not cover fails the generation.

// tsFlowUnsupported is the panic value of the printer for a construct it does not print; the
// generation then fails.
type tsFlowUnsupported string

// startTSFlow builds the Core IR of the table being generated for printing its rules from Flow IR. It
// reports whether the Core IR was built; if not, the generation fails with the error it records.
func (g *tsGen) startTSFlow() bool {
	g.flow = nil
	if err := g.startRecFlow(g.prog); err != nil {
		if g.flowErr == nil {
			g.flowErr = err
		}
		return false
	}
	// The generated TypeScript reads input through the runtime's peek and matches literals through
	// matchLiteral: it has no in-place reads and no class tables.
	g.flow.opts.GuardedClasses = false
	g.flow.opts.InlineLiterals = 0
	g.flow.opts.ClassTables = false
	if chosen := outlineNodes(g.flow.m, g.table == "rules"); len(chosen) > 0 {
		g.flow.opts.Outline = func(n core.Node) bool { return chosen[n] }
	}
	return true
}

// flowRule prints the rule r from Flow IR as a function of its own and returns the call
// expression that runs it, or false if r is not printed from Flow IR. A lean value-free rule that
// flow.LocalRule accepts prints as a flat function; a rule whose body is a framed body prints under the
// frame the runtime owns, which also runs its action and finish. On success it sets scope to the
// capture scope of the printed body.
func (g *tsGen) flowRule(r *rule, scope *scope) (call string, ok bool) {
	if g.flow == nil {
		return "", false
	}
	var f *flow.Func
	var slots []core.Slot
	cr := g.flow.m.Rules[r.id]
	if recFlowEligible(r) && flow.LocalRule(cr) {
		var err error
		if f, err = flow.Lower(g.flow.m, cr, r.name, g.flow.opts); err != nil {
			panic(fmt.Errorf("rule %s of table %s: %w", r.name, g.table, err)) // LocalRule holds
		}
		if f.Binds {
			f = nil // the runtime owns the frame the binds write
		}
	}
	if f == nil {
		if _, eligible := g.flowNodeFrameOK(r); !eligible {
			return "", false
		}
		var err error
		if f, err = flow.LowerFrame(g.flow.m, cr, cr.Body, cr.Scope, !r.lean, r.name, g.flow.opts); err != nil {
			panic(fmt.Errorf("rule %s of table %s: %w", r.name, g.table, err)) // FramedBody holds
		}
		slots = cr.Scope
	}
	return g.flowFunc(f, slots, scope, gen.Describe(r.bodyExpr()))
}

// flowFunc prints the function f, whose capture scope has the slots, as a function of its own and
// returns the call expression that runs it, or false if the printer does not print f. On success it
// sets scope to the capture scope of a framed f. The comment describes the function.
func (g *tsGen) flowFunc(f *flow.Func, slots []core.Slot, scope *scope, comment string) (call string, ok bool) {
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
		b.WriteString(v)
	}
	b.WriteString(indent(body))
	if f.Frame {
		*scope = *newScope()
		for _, sl := range slots {
			scope.slot(sl.Name)
		}
	}
	name, shared := g.methodName("Parser/R", tsSharedKey(b.String(), decls))
	if !shared {
		for _, d := range decls {
			if tsNamesIdentifier(b.String(), d.name) {
				g.vars.WriteString(d.text)
			}
		}
		param := "p"
		if !strings.Contains(b.String(), "p.") && !strings.Contains(b.String(), "(p)") {
			param = "_p"
		}
		fmt.Fprintf(&g.methods, "// %s\nfunction %s(%s: Parser): R {\n%s}\n\n", comment, name, param, b.String())
	}
	return name + "(p)", true
}

var (
	tsMarkParts = [4]string{"Pos", "Env", "Trail", "Rec"}
	tsMarkZero  = [4]string{"0", "null as Env | null", "0", "0"}
)

func tsFlowType(t flow.Type) string {
	switch t {
	case flow.TypeInt, flow.TypeRune:
		return "number"
	case flow.TypeBool:
		return "boolean"
	case flow.TypeValue:
		return "Node | null"
	case flow.TypeVals:
		return "(Node | null)[]"
	case flow.TypeFrame:
		return "Frame"
	case flow.TypeExpMark:
		return "number"
	case flow.TypeExpIDs:
		return "number[]"
	case flow.TypeError:
		return "SyntaxError | null"
	}
	panic(tsFlowUnsupported("variable of type " + t.String()))
}

func tsFlowZero(t flow.Type) string {
	switch t {
	case flow.TypeBool:
		return "false"
	case flow.TypeValue:
		return "null"
	case flow.TypeVals:
		return "[]"
	case flow.TypeFrame:
		return "emptyFrame"
	case flow.TypeExpIDs:
		return "noExp"
	case flow.TypeError:
		return "null"
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
	names map[*flow.Names]string
	// marks holds the marks of the whole parser state by variable, with the parts they save.
	marks map[*flow.Var]*flow.MarkState
	// outlined holds the calls of the functions printed for the outlined nodes.
	outlined map[*flow.CallOutline]string
	// parts holds, for each mark variable, the variables that stand for its position, environment, trail
	// length and number of recovered errors, so that a mark is declared as far as it is read.
	parts map[*flow.Var]*[4]*flow.Var
}

// part returns the variable that stands for the component i of the mark m.
func (p *tsFlowPrinter) part(m *flow.Var, i int) *flow.Var {
	ps := p.parts[m]
	if ps == nil {
		ps = &[4]*flow.Var{}
		for j := range ps {
			ps[j] = &flow.Var{Name: m.Name + tsMarkParts[j]}
		}
		p.parts[m] = ps
	}
	return ps[i]
}

// tsSharedKey returns the text by which printed functions share a body: the text with each constant
// it names replaced by the declaration of the constant, so that equal functions share whatever the
// names of their constants (the literal tables of the TypeScript generation are not shared) are.
func tsSharedKey(code string, decls []tsDecl) string {
	for _, d := range decls {
		if !tsNamesIdentifier(code, d.name) {
			continue
		}
		id := regexp.MustCompile(`\b` + regexp.QuoteMeta(d.name) + `\b`)
		code = id.ReplaceAllLiteralString(code, "{"+id.ReplaceAllLiteralString(d.text, "$")+"}")
	}
	return code
}

// tsDecl is a package-level constant of a printed function.
type tsDecl struct{ name, text string }

// printTSFlow returns the statements of the function f, ending with its return, and the variables
// the statements declare.
func (g *tsGen) printTSFlow(f *flow.Func) (string, []string, []tsDecl) {
	parts := map[*flow.Var]*[4]*flow.Var{}
	var reads map[*flow.Var]bool
	for i := 0; ; i++ {
		p := &tsFlowPrinter{g: g, parts: parts, dry: true, reads: reads, next: map[*flow.Var]bool{}, assigned: map[*flow.Var]bool{}, used: map[*flow.Label]bool{}, labels: map[*flow.Label]string{}}
		p.function(f)
		if len(p.next) == len(reads) && reads != nil || i > 8 {
			break
		}
		reads = p.next
	}
	if reads == nil {
		reads = map[*flow.Var]bool{}
	}
	p := &tsFlowPrinter{g: g, parts: parts, reads: reads, next: map[*flow.Var]bool{}, assigned: map[*flow.Var]bool{}, used: map[*flow.Label]bool{}, labels: map[*flow.Label]string{}}
	p.function(f)
	if p.next[f.Start] || p.next[f.Ctx] {
		panic(tsFlowUnsupported("the start or the context of a value-free rule"))
	}
	var vars []string
	for _, v := range f.Vars {
		if v.Entry {
			continue
		}
		if v.Type == flow.TypeMark {
			// A mark of the whole parser state is four variables, declared as far as they are read.
			if ps := parts[v]; ps != nil {
				for i, part := range ps {
					if p.next[part] {
						vars = append(vars, fmt.Sprintf("  let %s%s = %s;\n", v.Name, tsMarkParts[i], tsMarkZero[i]))
					}
				}
			}
			continue
		}
		if p.next[v] {
			if typ := tsFlowType(v.Type); typ == "number" || typ == "boolean" {
				// The initial value gives the type.
				vars = append(vars, fmt.Sprintf("  let %s = %s;\n", v.Name, tsFlowZero(v.Type)))
			} else {
				vars = append(vars, fmt.Sprintf("  let %s: %s = %s;\n", v.Name, typ, tsFlowZero(v.Type)))
			}
		}
	}
	return p.b.String(), vars, p.decls
}

func (p *tsFlowPrinter) function(f *flow.Func) {
	p.stmts(f.Body)
	publish := func(b *strings.Builder) {
		if f.Cut != nil {
			p.next[f.Cut] = true
			b.WriteString("p.cut = " + f.Cut.Name + ";\n")
		}
	}
	var inner strings.Builder
	if !p.dead {
		switch {
		case f.Frame:
			p.read(f.Value)
			publish(&inner)
			inner.WriteString("return " + p.expr(f.Value) + ";\n")
		default:
			inner.WriteString("return null;\n")
		}
	}
	if p.used[f.Fail] {
		body := p.b.String()
		p.b.Reset()
		var fail strings.Builder
		if f.Frame {
			publish(&fail)
		}
		fail.WriteString("return undefined;\n")
		fmt.Fprintf(&p.b, "%s: {\n%s%s}\n%s", p.label(f.Fail), indent(body), indent(inner.String()), fail.String())
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
	for i := 0; i < len(ss); i++ {
		if _, step := ss[i].(*flow.Step); step {
			// Consecutive charges are one.
			n := 1
			for i+n < len(ss) {
				if _, next := ss[i+n].(*flow.Step); !next {
					break
				}
				n++
			}
			p.write(tsStepWork(n))
			i += n - 1
			continue
		}
		if i+1 < len(ss) && !p.dead && p.unisolateMerge(ss[i], ss[i+1]) {
			i++
			continue
		}
		p.stmt(ss[i])
	}
}

// unisolateMerge prints the end of an expectation record whose body succeeded, an Unisolate followed
// by the merge of its expectations into the enclosing record, as one call that merges them in place.
// The two variables of the Unisolate are written again where the failed body ends the record, before
// they are read, so skipping their assignment here is not observable. It reports whether it printed.
func (p *tsFlowPrinter) unisolateMerge(a, b flow.Stmt) bool {
	set, ok := a.(*flow.Set)
	if !ok {
		return false
	}
	u, ok := tsUnisolate(set)
	if !ok {
		return false
	}
	m, ok := b.(*flow.MergeExpected)
	if !ok {
		return false
	}
	far, _ := set.Dst[0].(*flow.Var)
	ids, _ := set.Dst[1].(*flow.Var)
	f, fok := m.Far.(*flow.Use)
	e, eok := m.Exps.(*flow.Use)
	if !fok || !eok || far == nil || ids == nil || f.V != far || e.V != ids {
		return false
	}
	p.read(u.Mark)
	p.write(fmt.Sprintf("p.unisolateMerge(%s);\n", p.expr(u.Mark)))
	return true
}

// tsStepWork returns the statement that charges n steps.
func tsStepWork(n int) string {
	if n == 1 {
		return "p.stepWork();\n"
	}
	return fmt.Sprintf("p.stepWork(%d);\n", n)
}

// isRead reports whether v is read, by the previous pass.
func (p *tsFlowPrinter) isRead(v *flow.Var) bool {
	return v != nil && (p.reads == nil || p.reads[v])
}

// pure reports whether evaluating x has no effect, so that an assignment of it to nothing read is
// left out.
func tsFlowPure(x flow.Expr) bool {
	switch x := x.(type) {
	case *flow.Use, *flow.Read, *flow.Int, *flow.Bool, *flow.Nil, *flow.RecLen, *flow.KidsLen, *flow.NewVals, *flow.Str, *flow.MarkPos, *flow.FrameVal, *flow.InClass, *flow.GuardFails:
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
		p.write(tsStepWork(1))
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
	case *flow.MergeExpected:
		p.read(s.Far, s.Exps)
		p.write(fmt.Sprintf("p.mergeKept(%s, %s);\n", p.expr(s.Far), p.expr(s.Exps)))
	case *flow.MergeRenamed:
		p.read(s.Far, s.Exps)
		p.write(fmt.Sprintf("p.mergeKept(%s, p.renameExpected(%s, %d));\n", p.expr(s.Far), p.expr(s.Exps), p.desc(s.Exp)))
	case *flow.AppendError:
		p.read(s.X)
		p.write(fmt.Sprintf("p.recovered.push(%s!);\n", p.expr(s.X)))
	case *flow.PushKid:
		p.read(s.X)
		p.write(fmt.Sprintf("p.kidStack.push(%s);\n", p.expr(s.X)))
	case *flow.DropKids:
		p.read(s.Base)
		p.write(fmt.Sprintf("p.dropKids(%s);\n", p.expr(s.Base)))
	case *flow.Append:
		p.read(&flow.Use{V: s.Vals}, s.X)
		if s.Spread {
			p.write(fmt.Sprintf("for (const k of (%s as Node).children) {\n  %s.push(k);\n}\n", p.expr(s.X), s.Vals.Name))
		} else {
			p.write(fmt.Sprintf("%s.push(%s);\n", s.Vals.Name, p.expr(s.X)))
		}
	case *flow.SetCapture:
		p.read(s.X)
		p.write(fmt.Sprintf("p.setCapture(%d, %s);\n", s.Slot, p.expr(s.X)))
	case *flow.ClearVals:
		p.read(&flow.Use{V: s.F})
		p.write(s.F.Name + ".fill(null);\n")
	case *flow.FreeFrame:
		// Frames are not pooled.
	case *flow.DeclareNames:
		// The names are declared where they are first used.
	case *flow.Reset:
		m := p.markVar(s.M)
		p.write(p.reset(m))
	case *flow.DropTrail:
		m := p.markVar(s.M)
		if !p.marks[m].Trail {
			panic(tsFlowUnsupported("a mark without a trail dropped"))
		}
		p.readVar(p.part(m, 2))
		p.write(fmt.Sprintf("p.trail.length = %sTrail;\n", m.Name))
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
	var init, cond string
	if call, v, ok := tsTestedCall(s); ok {
		// The call and its test in one condition: nothing else reads the flag it sets.
		p.read(call)
		cond = p.expr(call) + " === undefined"
		if v != nil && p.isRead(v) {
			p.note(v)
			cond = fmt.Sprintf("(%s = %s as Node | null) === undefined", v.Name, p.expr(call))
		}
	} else {
		if s.Init != nil {
			init = p.setText(s.Init, true)
		}
		p.read(s.Cond)
		cond = p.expr(s.Cond)
	}
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
	if els == "" && strings.Count(then, "\n") == 1 {
		p.b.WriteString("if (" + cond + ") " + then) // one statement
		return
	}
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
	if len(s.Dst) == 1 && len(s.X) == 1 {
		// The expectations are a copy already, so keeping them leaves the variable as it is.
		if k, ok := s.X[0].(*flow.Keep); ok {
			if u, ok := k.Exps.(*flow.Use); ok && u.V == s.Dst[0] {
				return
			}
		}
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
	if len(s.X) == 1 && len(s.Dst) == 1 {
		if st, isMark := s.X[0].(*flow.MarkState); isMark {
			m, _ := s.Dst[0].(*flow.Var)
			if p.marks == nil {
				p.marks = map[*flow.Var]*flow.MarkState{}
			}
			p.marks[m] = st
			srcs := [4]string{"p.pos", "p.env", "p.trail.length", "p.recovered.length"}
			var parts []string
			for i, src := range srcs {
				if p.isRead(p.part(m, i)) {
					parts = append(parts, m.Name+tsMarkParts[i]+" = "+src)
				}
			}
			if len(parts) == 0 {
				return ""
			}
			return strings.Join(parts, stmtSep(stmt)) + end
		}
	}
	if len(s.X) == 1 && len(s.Dst) == 2 {
		if _, single := s.X[0].(*flow.ShapeSingle); single {
			// The second result, whether the value is a single element, is not used.
			return p.setText(&flow.Set{Dst: s.Dst[:1], X: s.X}, stmt)
		}
	}
	if u, ok := tsUnisolate(s); ok {
		// The farthest failure of the record, and a copy of its expectations in p.isoIds.
		p.read(u.Mark)
		far, _ := s.Dst[0].(*flow.Var)
		ids, _ := s.Dst[1].(*flow.Var)
		farRead, idsRead := p.isRead(far), p.isRead(ids)
		p.note(far)
		p.note(ids)
		call := fmt.Sprintf("p.unisolate(%s)", p.expr(u.Mark))
		switch {
		case farRead && idsRead:
			return fmt.Sprintf("%s = %s%s%s = p.isoIds%s", far.Name, call, stmtSep(stmt), ids.Name, end)
		case farRead:
			return fmt.Sprintf("%s = %s%s", far.Name, call, end)
		}
		// Only the expectations are read, or none: the record still ends.
		if idsRead {
			return fmt.Sprintf("%s%s%s = p.isoIds%s", call, stmtSep(stmt), ids.Name, end)
		}
		return call + end
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
			return fmt.Sprintf("%s = %s as Node | null%s%s = %s !== undefined%s", vv.Name, x, stmtSep(stmt), ov.Name, vv.Name, end)
		case valRead:
			return fmt.Sprintf("%s = %s as Node | null%s", vv.Name, x, end)
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
		val := value.LiteralValue(x.Lit.Text)
		v, shared := g.literalName(val)
		if !shared {
			p.decl(v, fmt.Sprintf("const %s: Lit = { text: %s, cps: %s, bytes: %s };\n", v, tsString(val), tsInts([]rune(val)), tsInts([]byte(val))))
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
	case *flow.Isolate:
		return "p.isolate()"
	case *flow.Keep:
		// The expectations are a copy of the record already.
		return p.expr(x.Exps)
	case *flow.MakeError:
		return fmt.Sprintf("p.recoveredError(%s, %s)", p.expr(x.Far), p.expr(x.Exps))
	case *flow.NewErrorNode:
		start := p.expr(x.Start)
		return fmt.Sprintf("new Node(\"error\", \"\", %s, p.pos, p.text(%s, p.pos), noNodes, [{ name: \"message\", value: %s!.message }], true, true)", start, start, p.expr(x.X))
	case *flow.Text:
		p.read(x.Start)
		return fmt.Sprintf("p.text(%s, p.pos)", p.expr(x.Start))
	case *flow.LiteralText:
		// The input matched: the literal holds the replacement character, which stands for invalid input.
		p.read(x.Start)
		return fmt.Sprintf("p.text(%s, p.pos)", p.expr(x.Start))
	case *flow.Str:
		return tsString(x.S)
	case *flow.NewTerminalNode:
		return fmt.Sprintf("new Node(\"string\", \"\", %s, p.pos, %s, noNodes, noFields, true, true)", p.expr(x.Start), p.expr(x.Text))
	case *flow.NewBranchNode:
		kind := "tuple"
		if x.List {
			kind = "list"
		}
		return fmt.Sprintf("new Node(\"%s\", \"\", %s, p.pos, \"\", %s, noFields, true, false)", kind, p.expr(x.Start), p.expr(x.Kids))
	case *flow.NewVals:
		return "[]"
	case *flow.Kids:
		return fmt.Sprintf("p.kids(%s)", p.expr(x.Base))
	case *flow.ShapeSingle:
		return fmt.Sprintf("p.shapeSingle(%d, %s, %s)", tsShape(x.Shape), p.expr(x.X), p.expr(x.Start))
	case *flow.ShapeCapture:
		return fmt.Sprintf("p.shapeCapture(%d, %s, %s)", tsShape(x.Shape), p.expr(x.X), p.expr(x.Start))
	case *flow.ScopeValue:
		return fmt.Sprintf("p.scopeValue(%d, %s, %s)", tsShape(x.Shape), p.expr(x.X), p.expr(x.Start))
	case *flow.AttachCaptures:
		if x.Frame == nil {
			panic(tsFlowUnsupported("captures attached from variables"))
		}
		return fmt.Sprintf("p.attachCaptures(%s, %s, %s, %s, p.pos)", p.expr(x.X), p.scopeNames(x.Names), x.Frame.Name, p.expr(x.Start))
	case *flow.Pred:
		if x.Caps.Vars != nil {
			panic(tsFlowUnsupported("a predicate that reads variables"))
		}
		// The term reads the current frame.
		if p.dry {
			return "p.predicate(a)"
		}
		body := p.g.predicateTerm(p.grammarTerm(x.Node), frameScope(x.Caps))
		return fmt.Sprintf("p.predicate((%s) => %s)", tsActxParam(body), body)
	case *flow.Bind:
		if x.Caps.Vars != nil {
			panic(tsFlowUnsupported("a bind that reads variables"))
		}
		b, ok := p.grammarTerm(x.Node).(*grammar.Bind)
		if !ok {
			panic(tsFlowUnsupported("a bind without a grammar bind"))
		}
		if p.dry {
			return "p.bind(a)"
		}
		// The function is declared only if the rule keeps the printed text.
		name := p.g.name("a")
		val := p.g.term(b.Value, frameScope(x.Caps), nil)
		p.decl(name, fmt.Sprintf("function %s(%s: Actx): Value {\n  return %s;\n}\n\n", name, tsActxParam(val), val))
		return fmt.Sprintf("p.bind(%s, %s)", tsString(b.Name), name)
	case *flow.CallOutline:
		return p.outline(x)
	case *flow.MarkPos:
		p.readVar(p.part(x.M, 0))
		return x.M.Name + "Pos"
	case *flow.NewFrame:
		return fmt.Sprintf("p.newFrame(%d)", len(x.N.List))
	case *flow.FrameVal:
		return fmt.Sprintf("%s[%d]!", x.F.Name, x.Slot)
	}
	panic(tsFlowUnsupported(fmt.Sprintf("expression %T", x)))
}

// decl declares a package-level constant of the printed function, once per pass that prints.
func (p *tsFlowPrinter) decl(name, s string) {
	p.decls = append(p.decls, tsDecl{name, s})
}

// call returns the call of the rule r from the Operators Rule level min.
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

// tsShape packs a shape as the runtime's Shaper (tsShaper).
func tsShape(sh core.Shape) int {
	s := int(sh.Mode)<<2 | sh.K<<5
	if sh.Elems {
		s |= 1
	}
	if sh.Tuple {
		s |= 2
	}
	if sh.Shared {
		s |= 16
	}
	return s
}

// markName returns the name of the mark variable that x reads.
func (p *tsFlowPrinter) markVar(x flow.Expr) *flow.Var {
	u, ok := x.(*flow.Use)
	if !ok {
		panic(tsFlowUnsupported("a mark that is not a variable"))
	}
	return u.V
}

// readVar records that v is read at a reachable point.
func (p *tsFlowPrinter) readVar(v *flow.Var) {
	if !p.dead {
		p.next[v] = true
	}
}

// scopeNames returns the constant that holds the capture names n, declaring it on first use.
func (p *tsFlowPrinter) scopeNames(n *flow.Names) string {
	if p.dry {
		return "scope"
	}
	if name, ok := p.names[n]; ok {
		return name
	}
	name := p.g.name("scope")
	if p.names == nil {
		p.names = map[*flow.Names]string{}
	}
	p.names[n] = name
	p.decl(name, fmt.Sprintf("const %s: string[] = %s;\n", name, tsStrings(n.List)))
	return name
}

// grammarTerm returns the term of the grammar predicate that the node n is built from, which the
// term printer prints.
func (p *tsFlowPrinter) grammarTerm(n core.Node) grammar.Term {
	e, ok := p.g.flow.origin[n].(*grammar.Predicate)
	if !ok {
		panic(tsFlowUnsupported(fmt.Sprintf("no grammar predicate for %T", n)))
	}
	return e.Term
}

// tsUnisolate returns the Unisolate that the assignment s makes to two variables.
func tsUnisolate(s *flow.Set) (*flow.Unisolate, bool) {
	if len(s.X) != 1 || len(s.Dst) != 2 {
		return nil, false
	}
	u, ok := s.X[0].(*flow.Unisolate)
	return u, ok
}

// flowLine prints the line l of the Operators Rule g.cur, whose expression is e, from Flow IR as a
// function and returns the call expression that runs it, or false if the printer does not print
// the line. It sets scope to the capture scope of the line. With work controls the runtime loop that
// runs the line charges its entry and the function charges each iteration of a repetition.
func (g *tsGen) flowLine(l *core.OpLine, e grammar.Expr, scope *scope, build bool) (string, bool) {
	if g.flow == nil || l == nil || !flowLineOK(e) || !flow.FramedBody(l.Body) {
		return "", false
	}
	f, err := flow.LowerFrame(g.flow.m, g.flow.m.Rules[g.cur.id], l.Body, l.Scope, build, g.cur.name, g.flow.opts)
	if err != nil {
		panic(fmt.Errorf("line of Operators Rule %s in table %s: %w", g.cur.name, g.table, err)) // FramedBody holds
	}
	prev := g.proj
	g.proj = nil // the actions of the lines use no projections of the rule
	defer func() { g.proj = prev }()
	return g.flowFunc(f, l.Scope, scope, g.cur.name+" (OperatorsRule line)")
}

// outline prints the function of the outlined node x and returns the call of it. The passes that only
// collect reads print no function.
func (p *tsFlowPrinter) outline(x *flow.CallOutline) string {
	if p.dry {
		return "e(p)"
	}
	if call, ok := p.outlined[x]; ok {
		return call
	}
	g := p.g
	f, err := flow.LowerOutline(g.flow.m, g.flow.m.Rules[g.cur.id], x.Node, x.Slots, x.Build, g.cur.name, g.flow.opts)
	if err != nil {
		panic(fmt.Errorf("outlined node of rule %s in table %s: %w", g.cur.name, g.table, err)) // Outlinable holds
	}
	call, ok := g.flowFunc(f, x.Slots, newScope(), g.cur.name+" (outlined)")
	if !ok {
		panic(tsFlowUnsupported("an outlined node"))
	}
	if p.outlined == nil {
		p.outlined = map[*flow.CallOutline]string{}
	}
	p.outlined[x] = call
	return call
}

// tsTestedCall returns, for an If that tests the success flag of a call with two results (the value and
// whether it matched) that its initialization makes, the call and the variable of its value (nil if the
// value is dropped).
func tsTestedCall(s *flow.If) (call flow.Expr, v *flow.Var, ok bool) {
	if s.Init == nil || len(s.Init.X) != 1 || len(s.Init.Dst) != 2 || flow.Results(s.Init.X[0]) != 2 {
		return nil, nil, false
	}
	switch s.Init.X[0].(type) {
	case *flow.MatchLit, *flow.CallRule, *flow.CallOutline:
	default:
		return nil, nil, false
	}
	flag, isVar := s.Init.Dst[1].(*flow.Var)
	if not, isNot := s.Cond.(*flow.Not); !isVar || !isNot {
		return nil, nil, false
	} else if u, isUse := not.X.(*flow.Use); !isUse || u.V != flag {
		return nil, nil, false
	}
	v, _ = s.Init.Dst[0].(*flow.Var)
	return s.Init.X[0], v, true
}

// reset returns the statement that goes back to the mark m: the position, and the parts of the parser
// state that the code since the mark may change.
func (p *tsFlowPrinter) reset(m *flow.Var) string {
	st := p.marks[m]
	if st == nil {
		panic(tsFlowUnsupported("a reset of a mark that is not set"))
	}
	p.readVar(p.part(m, 0))
	if !st.Env && !st.Trail && !st.Rec {
		return "p.pos = " + m.Name + "Pos;\n"
	}
	args := []string{m.Name + "Pos", "p.env", "p.trail.length", "p.recovered.length"}
	for i, needed := range [4]bool{true, st.Env, st.Trail, st.Rec} {
		if needed {
			p.readVar(p.part(m, i))
			args[i] = m.Name + tsMarkParts[i]
		}
	}
	return "p.reset(" + strings.Join(args, ", ") + ");\n"
}
