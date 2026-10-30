package gogen

import (
	"fmt"
	goparser "go/parser"
	"go/token"
	"strings"

	goast "go/ast"

	"github.com/ornew/pego/internal/compile"
	irc "github.com/ornew/pego/internal/ir/core"
	"github.com/ornew/pego/internal/ir/exec"
	"github.com/ornew/pego/internal/ir/exec/core"
	"github.com/ornew/pego/internal/ir/exec/execgo"
	"github.com/ornew/pego/internal/ir/exec/lower"
	"github.com/ornew/pego/internal/ir/flow"
)

// Typed Go rules printed from Exec IR.
//
// With the Exec IR switch on (core.Enabled), the rules of the typed table in the ordinary form are
// lowered from Flow IR to Exec IR (lower.Flow) and printed by the Go backend of Exec IR
// (execgo.Body), which prints the compositions of the runtime too. The primitives of Flow IR are
// transitional operations there, which the flow printer prints as it prints them from Flow IR. The
// printed code is the same as from Flow IR.

// execRoute reports whether the functions of the current table print from Exec IR.
func (g *Generator) execRoute() bool {
	return (core.Enabled || g.hooks.ExecRoute) && g.Ordinary && g.Table == "trules"
}

// execBody prints the body of the Flow IR function f from Exec IR with the flow printer p.
func (p *flowPrinter) execBody(f *flow.Func) execgo.BodyResult {
	l, err := lower.Flow(f)
	if err != nil {
		panic(fmt.Errorf("rule %s of table %s: %w", f.Name, p.g.Table, err))
	}
	res := execgo.Body(l.Module, l.Func, p.execTarget(), l.Vars)
	p.g.ExecFuncs++
	if p.g.hooks.FlowVarsMutant {
		// The locals that the body names stay undeclared.
		for _, v := range l.Vars {
			res.Stores.Dead[v] = true
		}
		return res
	}
	if p.g.FlowErr == nil {
		for _, id := range freeIdents(res.Text) {
			if res.Stores.Dead[id] {
				p.g.FlowErr = fmt.Errorf("internal error: the printed body names the variable %s, which is not declared", id)
				break
			}
		}
	}
	return res
}

// execTarget returns the Go implementation of the operations of lowered grammar code: the flow
// printer prints each primitive as it does from Flow IR.
func (p *flowPrinter) execTarget() *execgo.Target {
	return &execgo.Target{
		Reg: func(name string) string {
			return map[string]string{lower.Pos: "p.Pos", lower.Cut: "p.Cut"}[name]
		},
		Handle: func(name string) string { return name },
		Op: func(c *exec.Call, args []string) string {
			switch x := c.Imm.(type) {
			case flow.Reg:
				return p.expr(&flow.Read{R: x}) + " = " + args[0]
			case flow.Expr:
				return p.expr(x)
			case flow.Stmt:
				return p.leafStmt(x)
			}
			panic(fmt.Sprintf("gogen: operation %s without a Flow IR primitive", c.Callee))
		},
		Stmt: func(c *exec.Call, _ []string, dst []string, _ bool) (string, bool) {
			switch x := c.Imm.(type) {
			case *flow.Peek:
				return strings.TrimSuffix(p.peekText(dst[0], dst[1], dst[2]), "\n"), true
			case *flow.Append:
				return p.leafStmt(x), true
			case *flow.Eval:
				v := p.expr(x)
				pre := p.pre
				p.pre = ""
				assign := strings.Join(dst, ", ") + " = " + v
				if pre != "" {
					return "{ " + pre + assign + "\n}", true
				}
				return assign, true
			}
			return "", false
		},
		Negate: func(c *exec.Call, _ []string) (string, bool) {
			if x, ok := c.Imm.(flow.Expr); ok {
				return p.not(x), true
			}
			return "", false
		},
		Prec: goPrec,
		Pure: func(c *exec.Call) bool {
			switch c.Imm.(type) {
			case *flow.Str, *flow.MarkPos, *flow.Read:
				return true
			}
			return false
		},
	}
}

// leafStmt returns the text of the primitive statement s as the flow printer writes it at a
// reachable point, without its final newline.
func (p *flowPrinter) leafStmt(s flow.Stmt) string {
	prev, dead := p.b, p.dead
	p.b, p.dead = strings.Builder{}, false
	p.stmt(s)
	text := p.b.String()
	p.b, p.dead = prev, dead
	return strings.TrimSuffix(text, "\n")
}

// goPrec returns the precedence of the loosest binary operator of the Go expression text outside
// parentheses, or 6 for an operand.
func goPrec(text string) int {
	e, err := goparser.ParseExpr(text)
	if err != nil {
		return 0
	}
	if b, ok := e.(*goast.BinaryExpr); ok {
		return b.Op.Precedence()
	}
	return token.UnaryPrec
}

// execFlatRule prints the local function f from Exec IR as a flat rule (flowTypedRule).
func (g *Generator) execFlatRule(p *flowPrinter, f *flow.Func) *flatRule {
	res := p.execBody(f)
	st := res.Stores
	read := func(v *flow.Var) bool { return v != nil && st.Read[v.Name] }
	out := &flatRule{fail: res.Exited, succeed: res.End, binds: f.Binds, start: read(f.Start), ctx: read(f.Ctx)}
	var unread []string
	for _, v := range f.Vars {
		out.vars = append(out.vars, dvar{v.Name, goType(v.Type)})
		switch {
		case st.Dead[v.Name]:
			out.markVar(v.Name, true)
		case !st.Named[v.Name]:
			unread = append(unread, v.Name)
		case !st.Read[v.Name]:
			out.markVar(v.Name, false)
		}
	}
	out.body = res.Text
	if len(unread) > 0 {
		out.body = "\t" + strings.Repeat("_, ", len(unread)-1) + "_ = " + strings.Join(unread, ", ") + "\n" + out.body
	}
	g.FlowRules++
	return out
}

// execFramed prints the framed function f from Exec IR as flowFramed does.
func (g *Generator) execFramed(p *flowPrinter, f *flow.Func, slots []irc.Slot, scope *compile.Scope, comment string, ruleBody bool) string {
	res := p.execBody(f)
	st := res.Stores
	var vars []dvar
	dead, quiet := map[string]bool{}, map[string]bool{}
	var unread []string
	for _, v := range f.Vars {
		vars = append(vars, dvar{v.Name, p.goType(v.Type)})
		switch {
		case st.Dead[v.Name]:
			dead[v.Name] = true
		case !st.Named[v.Name]:
			unread = append(unread, v.Name)
		case !st.Read[v.Name]:
			quiet[v.Name] = true
		}
	}
	var b strings.Builder
	b.WriteString(varLinesExcept(vars, dead, quiet))
	if len(unread) > 0 {
		fmt.Fprintf(&b, "%s_ = %s\n", strings.Repeat("_, ", len(unread)-1), strings.Join(unread, ", "))
	}
	b.WriteString(res.Text)
	*scope = *compile.NewScope()
	for _, sl := range slots {
		scope.Slot(sl.Name)
	}
	g.FlowFrames++
	if ruleBody {
		return g.bodyMethod(comment, b.String())
	}
	return g.Method(comment, b.String())
}
