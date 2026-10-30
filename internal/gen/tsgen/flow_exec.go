package tsgen

import (
	"fmt"
	"strings"

	"github.com/ornew/pego/internal/ir/exec"
	"github.com/ornew/pego/internal/ir/exec/core"
	"github.com/ornew/pego/internal/ir/exec/exects"
	"github.com/ornew/pego/internal/ir/exec/lower"
	"github.com/ornew/pego/internal/ir/exec/pass"
	"github.com/ornew/pego/internal/ir/flow"
)

// Rules printed from Exec IR.
//
// With the Exec IR switch on (core.Enabled), the functions printed from Flow IR are lowered to Exec
// IR (lower.Flow) and printed by exects.Body, which prints their structure and decides which
// assignments stay (pass.DeadStores). The primitives of Flow IR are transitional operations there,
// which tsFlowPrinter prints as it prints them from Flow IR. Before printing (tsRuns), charges in a
// row become one spend, the end of an expectation record followed by the merge of its expectations
// one unisolateMerge, and the test of the call an If's Init makes the If's condition, as the flow
// printer prints them, so that the locals they no longer read are not declared. The printed code
// is the same as from Flow IR.

// execRoute reports whether the functions printed from Flow IR print from Exec IR.
func (g *Generator) execRoute() bool { return core.Enabled || g.execHook }

// tsSpendRun is the immediate operand of a call of tsSpendOp: the number of charges in a row.
type tsSpendRun struct{ n int }

// tsMergeIsolated is the immediate operand of a call of tsMergeOp: the end of the expectation record
// whose expectations merge into the enclosing record.
type tsMergeIsolated struct{ u *flow.Unisolate }

// tsFailed is the immediate operand of a call of tsFailedOp: the variable of the bool that the Init
// of the If sets.
type tsFailed struct{ flag string }

const (
	tsSpendOp  = "ts.spend"
	tsMergeOp  = "ts.unisolateMerge"
	tsFailedOp = "ts.failed"
)

// printTSFlowExec is printTSFlow from Exec IR.
func (g *Generator) printTSFlowExec(f *flow.Func) (string, []string, []tsDecl) {
	l, err := lower.Flow(f)
	if err != nil {
		panic(fmt.Errorf("rule %s of table %s: %w", f.Name, g.Table, err))
	}
	g.ExecFuncs++
	tsRuns(l)
	vars := map[string]*flow.Var{}
	for _, v := range append(append([]*flow.Var(nil), f.Vars...), f.Start, f.Result, f.Ctx) {
		if v != nil {
			vars[v.Name] = v
		}
	}
	pure := func(c *exec.Call) bool {
		x, ok := c.Imm.(flow.Expr)
		return ok && tsFlowPure(x)
	}
	st := pass.DeadStores(l.Func, l.Vars, func(x exec.Expr) bool { return exects.Pure(x, pure) })
	reads := map[*flow.Var]bool{}
	for name, v := range vars {
		if st.Read[name] {
			reads[v] = true
		}
	}
	// The parts of the marks that are read: the reads of a pass do not depend on them, but which
	// parts a mark saves does.
	parts := map[*flow.Var]*[4]*flow.Var{}
	partReads := map[*flow.Var]bool{}
	newPrinter := func(dry bool) *tsFlowPrinter {
		r := map[*flow.Var]bool{}
		for v := range reads {
			r[v] = true
		}
		for v := range partReads {
			r[v] = true
		}
		return &tsFlowPrinter{g: g, parts: parts, dry: dry, reads: r, next: map[*flow.Var]bool{}, assigned: map[*flow.Var]bool{}, used: map[*flow.Label]bool{}, labels: map[*flow.Label]string{}}
	}
	partsRead := func(p *tsFlowPrinter) map[*flow.Var]bool {
		out := map[*flow.Var]bool{}
		for _, ps := range parts {
			for _, part := range ps {
				if p.next[part] {
					out[part] = true
				}
			}
		}
		return out
	}
	for i := 0; ; i++ {
		p := newPrinter(true)
		exects.Body(l.Module, l.Func, p.bodyTarget(l, vars), st)
		next := partsRead(p)
		same := len(next) == len(partReads)
		for v := range next {
			same = same && partReads[v]
		}
		if same || i > 8 {
			break
		}
		partReads = next
	}
	p := newPrinter(false)
	res := exects.Body(l.Module, l.Func, p.bodyTarget(l, vars), st)
	for _, v := range []*flow.Var{f.Start, f.Ctx} {
		if v != nil && (st.Read[v.Name] || p.next[v]) {
			panic(tsFlowUnsupported("the start or the context of a value-free rule"))
		}
	}
	body := res.Text
	if !f.Frame {
		inner := ""
		if res.End {
			inner = "return null;\n"
		}
		if res.Exited {
			body = fmt.Sprintf("%s: {\n%s%s}\nreturn undefined;\n", p.label(f.Fail), indent(body), indent(inner))
		} else {
			body += inner
		}
	}
	var decl []string
	for _, v := range f.Vars {
		if v.Entry {
			continue
		}
		if v.Type == flow.TypeMark {
			// A mark of the whole parser state is four variables, declared as far as they are read.
			if ps := parts[v]; ps != nil {
				for i, part := range ps {
					if p.next[part] {
						decl = append(decl, fmt.Sprintf("  let %s%s = %s;\n", v.Name, tsMarkParts[i], tsMarkZero[i]))
					}
				}
			}
			continue
		}
		if st.Read[v.Name] {
			if typ := tsFlowType(v.Type); typ == "number" || typ == "boolean" {
				// The initial value gives the type.
				decl = append(decl, fmt.Sprintf("  let %s = %s;\n", v.Name, tsFlowZero(v.Type)))
			} else {
				decl = append(decl, fmt.Sprintf("  let %s: %s = %s;\n", v.Name, typ, tsFlowZero(v.Type)))
			}
		}
	}
	return body, decl, p.decls
}

// tsRuns replaces, in the lowered function l, each run of charges with one call of tsSpendOp, each
// end of an expectation record that a merge of its expectations follows with one call of tsMergeOp,
// and the condition of an If that tests the call of its Init with a call of tsFailedOp.
func tsRuns(l *lower.Lowered) {
	m := l.Module
	m.AddOp(&exec.Op{Name: tsFailedOp, Component: lower.Component, Results: []exec.Type{exec.TBool}, Reads: []string{lower.Component},
		Transitional: true, Imm: "the bool the Init sets",
		Contract: "Reports that the call of the Init of the If failed: that the bool it sets is false."})
	m.AddOp(&exec.Op{Name: tsSpendOp, Component: lower.Component, Writes: []string{lower.Component}, NoReturn: true,
		Transitional: true, Imm: "the number of charges", Contract: "Charges a step as many times as Imm says."})
	// The merge reads what the end of the record reads: one operation for each operation of the end.
	merges := map[string]string{}
	merge := func(end *exec.Op) string {
		if name, ok := merges[end.Name]; ok {
			return name
		}
		name := tsMergeOp
		if len(merges) > 0 {
			name = fmt.Sprintf("%s.%d", tsMergeOp, len(merges)+1)
		}
		merges[end.Name] = name
		m.AddOp(&exec.Op{Name: name, Component: lower.Component, Params: end.Params,
			Reads: []string{lower.Component}, Writes: []string{lower.Component}, NoReturn: true, Transitional: true,
			Imm: "the end of the expectation record", Contract: "Ends the expectation record and merges its expectations into the enclosing record."})
		return name
	}
	var block func(ss []exec.Stmt) []exec.Stmt
	block = func(ss []exec.Stmt) []exec.Stmt {
		var out []exec.Stmt
		for i := 0; i < len(ss); i++ {
			if tsStep(ss[i]) {
				n := 1
				for i+n < len(ss) && tsStep(ss[i+n]) {
					n++
				}
				out = append(out, &exec.Do{Call: &exec.Call{Callee: tsSpendOp, Imm: tsSpendRun{n}}})
				i += n - 1
				continue
			}
			if i+1 < len(ss) {
				if end, ok := tsMergedRecord(ss[i], ss[i+1]); ok {
					out = append(out, &exec.Do{Call: &exec.Call{Callee: merge(m.Op(end.Callee)), Args: end.Args, Imm: tsMergeIsolated{end.Imm.(*flow.Unisolate)}}})
					i++
					continue
				}
			}
			switch s := ss[i].(type) {
			case *exec.Block:
				s.Stmts = block(s.Stmts)
			case *exec.Loop:
				s.Body.Stmts = block(s.Body.Stmts)
			case *exec.If:
				if flag, ok := tsTestedFlag(s); ok {
					// The condition tests the call of the Init, so that nothing else reads the bool.
					s.Cond = &exec.Call{Callee: tsFailedOp, Imm: tsFailed{flag}}
				}
				s.Then.Stmts = block(s.Then.Stmts)
				if s.Else != nil {
					s.Else.Stmts = block(s.Else.Stmts)
				}
			}
			out = append(out, ss[i])
		}
		return out
	}
	l.Func.Body.Stmts = block(l.Func.Body.Stmts)
}

// tsTestedFlag reports whether the If s tests that the call of its Init fails, which prints as one
// condition (tsTestedCall), and returns the variable of the bool that the Init sets.
func tsTestedFlag(s *exec.If) (string, bool) {
	if s.Init == nil || len(s.Init.Dst) != 2 || len(s.Init.Values) != 1 {
		return "", false
	}
	c, ok := s.Init.Values[0].(*exec.Call)
	if !ok {
		return "", false
	}
	switch c.Imm.(type) {
	case *flow.MatchLit, *flow.CallRule, *flow.CallOutline:
	default:
		return "", false
	}
	flag := s.Init.Dst[1]
	not, ok := s.Cond.(*exec.Unary)
	if !ok || not.Op != "!" || flag.Reg || flag.Name == "" {
		return "", false
	}
	if l, ok := not.X.(*exec.Local); !ok || l.Name != flag.Name {
		return "", false
	}
	return flag.Name, true
}

// tsStep reports whether s charges a step.
func tsStep(s exec.Stmt) bool {
	d, ok := s.(*exec.Do)
	if !ok {
		return false
	}
	_, ok = d.Call.Imm.(*flow.Step)
	return ok
}

// tsMergedRecord reports whether a and b end an expectation record and merge its expectations into
// the enclosing record, as unisolateMerge prints them, and returns the call that ends the record.
func tsMergedRecord(a, b exec.Stmt) (*exec.Call, bool) {
	set, ok := a.(*exec.Set)
	if !ok || len(set.Dst) != 2 || len(set.Values) != 1 {
		return nil, false
	}
	c, ok := set.Values[0].(*exec.Call)
	if !ok {
		return nil, false
	}
	if _, ok := c.Imm.(*flow.Unisolate); !ok {
		return nil, false
	}
	do, ok := b.(*exec.Do)
	if !ok {
		return nil, false
	}
	m, ok := do.Call.Imm.(*flow.MergeExpected)
	if !ok {
		return nil, false
	}
	far, ids := set.Dst[0], set.Dst[1]
	f, fok := m.Far.(*flow.Use)
	e, eok := m.Exps.(*flow.Use)
	if !fok || !eok || far.Reg || ids.Reg || far.Name == "" || ids.Name == "" || f.V.Name != far.Name || e.V.Name != ids.Name {
		return nil, false
	}
	return c, true
}

// bodyTarget returns the printing of the operations of the lowered function l, whose variables by
// name are vars, by the printer p.
func (p *tsFlowPrinter) bodyTarget(l *lower.Lowered, vars map[string]*flow.Var) *exects.BodyTarget {
	// at runs print at the point that dead tells and returns what it writes.
	at := func(dead bool, print func()) string {
		prev, prevDead := p.b, p.dead
		p.b, p.dead = strings.Builder{}, dead
		print()
		text := p.b.String()
		p.b, p.dead = prev, prevDead
		return text
	}
	set := func(s *exec.Set) (*flow.Set, bool) {
		if len(s.Values) != 1 {
			return nil, false
		}
		c, ok := s.Values[0].(*exec.Call)
		if !ok {
			return nil, false
		}
		x, ok := c.Imm.(flow.Expr)
		if !ok {
			return nil, false
		}
		fs := &flow.Set{X: []flow.Expr{x}}
		for _, d := range s.Dst {
			switch {
			case d.Reg && d.Name == lower.Pos:
				fs.Dst = append(fs.Dst, flow.RegPos)
			case d.Reg:
				fs.Dst = append(fs.Dst, flow.RegCut)
			case d.Name == "":
				fs.Dst = append(fs.Dst, &flow.Discard{})
			default:
				fs.Dst = append(fs.Dst, vars[d.Name])
			}
		}
		return fs, true
	}
	return &exects.BodyTarget{
		Reg: func(name string) string {
			if name == lower.Cut {
				return "p.cut"
			}
			return "p.pos"
		},
		Label: func(name string) string { return p.label(l.Labels[name]) },
		Op: func(c *exec.Call, dead bool) string {
			x, ok := c.Imm.(flow.Expr)
			if !ok {
				panic(fmt.Sprintf("tsgen: operation %s without a Flow IR expression", c.Callee))
			}
			var s string
			at(dead, func() { s = p.expr(x) })
			return s
		},
		Do: func(c *exec.Call, args func() []string, dead bool) string {
			switch x := c.Imm.(type) {
			case tsSpendRun:
				return at(dead, func() { p.write(tsSpend(x.n)) })
			case tsMergeIsolated:
				return at(dead, func() {
					p.read(x.u.Mark)
					p.write(fmt.Sprintf("p.unisolateMerge(%s);\n", p.expr(x.u.Mark)))
				})
			case flow.Reg:
				// The assignment of a register that holds a handle.
				if dead {
					return ""
				}
				return p.reg(x) + " = " + args()[0] + ";\n"
			case flow.Stmt:
				return at(dead, func() { p.stmt(x) })
			}
			panic(fmt.Sprintf("tsgen: operation %s without a Flow IR statement", c.Callee))
		},
		Set: func(s *exec.Set, init, dead bool) (string, bool) {
			if len(s.Values) == 1 {
				if c, ok := s.Values[0].(*exec.Call); ok {
					if x, ok := c.Imm.(flow.Stmt); ok {
						// A primitive statement that sets variables (Peek, Append).
						return at(dead, func() { p.stmt(x) }), true
					}
				}
			}
			fs, ok := set(s)
			if !ok {
				return "", false
			}
			if init {
				var text string
				at(dead, func() { text = p.setText(fs, true) })
				return text, true
			}
			return at(dead, func() { p.set(fs) }), true
		},
		Test: func(s *exec.If, dead bool) (string, bool) {
			fs, ok := set(s.Init)
			if !ok {
				return "", false
			}
			c, ok := s.Cond.(*exec.Call)
			if !ok {
				return "", false
			}
			failed, ok := c.Imm.(tsFailed)
			if !ok {
				return "", false
			}
			call, v, ok := tsTestedCall(&flow.If{Init: fs, Cond: &flow.Not{X: &flow.Use{V: vars[failed.flag]}}})
			if !ok {
				panic("tsgen: an If whose test does not print as one condition")
			}
			var cond string
			at(dead, func() { cond = p.testedCond(call, v) })
			return cond, true
		},
		Pure: func(c *exec.Call) bool {
			x, ok := c.Imm.(flow.Expr)
			return ok && tsFlowPure(x)
		},
	}
}
