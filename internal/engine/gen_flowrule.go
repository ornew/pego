package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
)

// Flat and framed rules of the Node and recognizer tables, as the Flow IR printer shapes them.
//
// A flat rule is printed from Flow IR as one function (flowRecRule, flowTypedRule): the declarations
// and statements of the one method that calls the rule's body, up to the rule's value, wrapped by
// flatMethod or leanRuleBody. A framed rule keeps a runtime frame: its body (flowNodeFrameBody) is
// printed from Flow IR into the matcher of the rule, but the runtime drives the frame, the growth of
// a left-recursion seed or the finalization of a cut or a recovery. The predicates flowBodyOK
// and leanRuleOK select which rules take which shape.
//
// The runtime runs a rule with a method per expression, captures in a frame with a trail that
// backtracking undoes, and an action evaluated in a context that reads the frame. A flat rule
// uses only the constructs that need none of that and is printed into the one method that calls it:
//
//   - The body is printed in place: an expression jumps to a label when it fails. Every variable is
//     declared at the top of the method, so each goto is legal.
//   - Captures are variables of the method; those of a repetition element with captures of its
//     own are cleared at the start of each iteration.
//   - A backtracking point (choice, optional, repetition element, !) saves the position, the
//     number of recovered errors, the variable environment if the expression binds variables,
//     and the captures the expression may set, and restores them when it fails.
//   - The action is a Go expression over the capture variables, evaluated in place.
//
// A flat rule never reads or sets p.cut (its flags are local and callees restore it), p.frame
// (callees set their own) or p.trail (callees truncate it to its length at the call), so it
// leaves them alone.
//
// Left-recursion leaders and rules that cut or recover are framed: they keep runtime growth and
// finalization, and only their expression bodies are printed in place.

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

type dvar struct{ name, typ string }

// markVar records, of the variable name of d.vars, that it is not declared (dead) or that it stays
// declared with nothing to read it (quiet).
func (d *flatRule) markVar(name string, dead bool) {
	m := &d.quiet
	if dead {
		m = &d.dead
	}
	if *m == nil {
		*m = map[string]bool{}
	}
	(*m)[name] = true
}

// varLinesExcept declares the variables vs, those of one type together (var x1, x2 int) in the order
// of the first of each type, without the variables in dead, which still give the declarations their
// order: the first variable of each type, dead or not, places the type. Separate statements need no
// alignment, unlike the lines of a var block. A declaration that declares
// a variable of quiet is followed by the statement that assigns the quiet variables of it to the blank
// identifier.
func varLinesExcept(vs []dvar, dead, quiet map[string]bool) string {
	var types []string
	names := map[string][]string{}
	seen := map[string]bool{}
	for _, v := range vs {
		if !seen[v.typ] {
			seen[v.typ] = true
			types = append(types, v.typ)
		}
		if !dead[v.name] {
			names[v.typ] = append(names[v.typ], v.name)
		}
	}
	var b strings.Builder
	for _, typ := range types {
		if len(names[typ]) > 0 {
			fmt.Fprintf(&b, "var %s %s\n", strings.Join(names[typ], ", "), typ)
			var q []string
			for _, n := range names[typ] {
				if quiet[n] {
					q = append(q, n)
				}
			}
			if len(q) > 0 {
				fmt.Fprintf(&b, "%s_ = %s\n", strings.Repeat("_, ", len(q)-1), strings.Join(q, ", "))
			}
		}
	}
	return b.String()
}

var chIdent = regexp.MustCompile(`\bch\b`)

// advance returns the statement that moves the position n units forward.
func advance(n int) string {
	if n == 1 {
		return "p.Pos++"
	}
	return fmt.Sprintf("p.Pos += %d", n)
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

// flatRule is a flat rule's printed body: the declarations and statements of the method that
// calls it, up to the rule's value in v (success falls through; failure jumps to fail).
type flatRule struct {
	vars       []dvar
	dead       map[string]bool // the variables of vars that are not declared (varLinesExcept)
	quiet      map[string]bool // the variables of vars that are declared, unread and silenced after their declaration
	body       string
	fail       bool // the body can fail (fail is jumped to)
	succeed    bool // the body can succeed
	binds      bool // the body binds variables (the environment is restored)
	start, ctx bool // the value uses start, the context c
}

// flatMethod writes the method name that calls the flat rule r: as invokePlain does if plain
// (restoring the position and the recovered errors when it fails), otherwise as invoke does.
func (g *generator) flatMethod(r *compile.Rule, d *flatRule, name, comment string, plain bool) {
	observeBody := g.observability && g.table == "trules" && (!r.Plain || g.directTypedDocument)
	if g.observability && !observeBody {
		name = g.tracePlainWrapper(r, name)
	}
	m := &g.methods
	fmt.Fprintf(m, "// %s, %s (body inlined)\nfunc %s(p *%s) (%s, bool) {\n%s", r.Name, comment, name, g.recv(), g.valType(), g.step("\tp"))
	if observeBody {
		m.WriteString("p.Observation.Evaluated()\n")
	}
	restore := plain && d.fail
	start, rec := d.start || restore, restore && g.recovers()
	var vs []dvar
	if start {
		vs = append(vs, dvar{"start", "int"})
	}
	if d.succeed {
		vs = append(vs, dvar{"v", g.valType()})
	}
	if d.ctx {
		vs = append(vs, dvar{"c", "*TActCtx"})
	}
	if rec {
		vs = append(vs, dvar{"rec", "int"})
	}
	if d.binds {
		vs = append(vs, dvar{"prevEnv", "*Env"})
	}
	m.WriteString(varLinesExcept(append(vs, d.vars...), d.dead, d.quiet))
	switch {
	case rec:
		m.WriteString("\tstart, rec = p.Pos, len(p.Recovered)\n")
	case start:
		m.WriteString("\tstart = p.Pos\n")
	}
	if d.binds {
		m.WriteString("\tprevEnv = p.Env\n")
	}
	m.WriteString("\tp.Enter()\n")
	m.WriteString(d.body)
	if d.succeed {
		m.WriteString("\tp.Depth--\n")
		if d.binds {
			m.WriteString("\tp.Env = prevEnv\n")
		}
		m.WriteString("\treturn v, true\n")
	}
	if d.fail {
		m.WriteString("fail" + ":\n\tp.Depth--\n")
		if rec {
			m.WriteString("\tp.Restore(start, rec)\n")
		} else if restore {
			m.WriteString("\tp.Pos = start\n")
		}
		if d.binds {
			m.WriteString("\tp.Env = prevEnv\n")
		}
		m.WriteString("\treturn nil, false\n")
	}
	m.WriteString("}\n\n")
}

// leanRuleBody provides the rule-table body for external entry and generic
// fallback calls. Its caller owns depth and failure rollback, so it adds neither.
func (g *generator) leanRuleBody(r *compile.Rule, d *flatRule) string {
	return g.bodyMethod(r.Name+" (value-free body inlined)", g.leanRuleText(d))
}

// leanRuleText returns the text of the method body of the value-free flat rule d.
func (g *generator) leanRuleText(d *flatRule) string {
	var b strings.Builder
	var vs []dvar
	if d.succeed {
		vs = append(vs, dvar{"v", "*Node"})
	}
	b.WriteString(varLinesExcept(append(vs, d.vars...), d.dead, d.quiet))
	b.WriteString(d.body)
	if d.succeed {
		b.WriteString("return v, true\n")
	}
	if d.fail {
		b.WriteString("fail" + ":\nreturn nil, false\n")
	}
	return b.String()
}

func exprRecovers(e grammar.Expr) bool {
	found := false
	compile.WalkExpr(e, func(e grammar.Expr) {
		if e, ok := e.(*grammar.Attributed); ok {
			for _, a := range e.Attrs {
				if a.Name == "recover" {
					found = true
				}
			}
		}
	})
	return found
}
