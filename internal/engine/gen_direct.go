package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ornew/pego/grammar"
)

// The direct rule of the Node and recognizer tables, as the Flow IR printer shapes it (flowRecDirect,
// flowDirectRule): the declarations and statements of the one method that calls the rule's body, up to
// the rule's value, with the methods that wrap it (directMethod, directLeanBody), and the predicates
// that select the rules whose bodies take that shape (directLeanOK, directFrameExprOK).
//
// The runtime runs a rule like this: a method per expression, captures in a frame with a trail
// that backtracking undoes, and an action evaluated in a context that reads the frame. A rule
// whose body uses only the constructs of a direct rule is instead printed into the one method that
// calls it:
//
//   - The body is inlined: an expression jumps to a label when it fails. Every variable is
//     declared at the top of the method, so each goto is legal.
//   - Captures are variables of the method; those of a repetition element with captures of its
//     own are cleared at the start of each iteration.
//   - A backtracking point (choice, optional, repetition element, !) saves the position, the
//     number of recovered errors, the variable environment if the expression binds variables,
//     and the captures the expression may set, and restores them when it fails.
//   - The action is a Go expression over the capture variables, evaluated in place.
//
// A direct rule never reads or sets p.cut (its flags are local and callees restore it), p.frame
// (callees set their own) or p.trail (callees truncate it to its length at the call), so it
// leaves them alone.
//
// Left-recursion leaders and rules that cut or recover keep runtime growth and finalization, and only
// their expression bodies are inlined (flowNodeFrameBody).

// directOK reports whether the rule r can be compiled as a direct rule.
func directOK(r *rule) bool {
	return directEligible(r)
}

// directEligible reports whether r is a direct rule candidate. Left-recursion leaders and bodies
// that directExprOK rejects, such as those with a cut, stay on the runtime path.
func directEligible(r *rule) bool {
	if r.leader {
		return false
	}
	return directExprEligible(r)
}

// Unfinished left-recursion bodies can use the structural expression emitter
// while retaining the runtime's seed, frame and finalization ownership.
func directExprEligible(r *rule) bool {
	return directExprOK(r.bodyExpr())
}

func directExprOK(expr grammar.Expr) bool {
	return directFrameExprOK(expr, false, false)
}

func directFrameExprOK(expr grammar.Expr, cuts, recovery bool) bool {
	ok := true
	walkExpr(expr, func(e grammar.Expr) {
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

// directLeanOK selects value-free plain rules of the Node runtime. Captures
// unused by recognition are omitted; predicates keep the general code because
// they can require values, capture frames and variable bindings.
func directLeanOK(r *rule) bool {
	if !r.plain || !r.lean || !r.novalue || !directOK(r) {
		return false
	}
	ok := true
	walkExpr(r.bodyExpr(), func(e grammar.Expr) {
		if _, predicate := e.(*grammar.Predicate); predicate {
			ok = false
		}
	})
	return ok
}

type dvar struct{ name, typ string }

// varLines declares the variables vs, those of one type together (var x1, x2 int) in the order of
// the first of each type. Separate statements need no alignment, unlike the lines of a var block.
func varLines(vs []dvar) string {
	var types []string
	names := map[string][]string{}
	for _, v := range vs {
		if _, ok := names[v.typ]; !ok {
			types = append(types, v.typ)
		}
		names[v.typ] = append(names[v.typ], v.name)
	}
	var b strings.Builder
	for _, typ := range types {
		fmt.Fprintf(&b, "var %s %s\n", strings.Join(names[typ], ", "), typ)
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

// direct is a direct rule's compiled body: the declarations and statements of the method that
// calls it, up to the rule's value in v (success falls through; failure jumps to fail).
type direct struct {
	vars       []dvar
	body       string
	fail       bool // the body can fail (fail is jumped to)
	succeed    bool // the body can succeed
	binds      bool // the body binds variables (the environment is restored)
	start, ctx bool // the value uses start, the context c
}

// directMethod writes the method name that calls the direct rule r: as invokePlain does if plain
// (restoring the position and the recovered errors when it fails), otherwise as invoke does.
func (g *generator) directMethod(r *rule, d *direct, name, comment string, plain bool) {
	observeBody := g.observability && g.table == "trules" && (!r.plain || g.directTypedDocument)
	if g.observability && !observeBody {
		name = g.tracePlainWrapper(r, name)
	}
	m := &g.methods
	fmt.Fprintf(m, "// %s, %s (body inlined)\nfunc %s(p *%s) (%s, bool) {\n%s", r.name, comment, name, g.recv(), g.valType(), g.step("\tp"))
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
	m.WriteString(varLines(append(vs, d.vars...)))
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
		m.WriteString("fail:\n\tp.Depth--\n")
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

// directLeanBody provides the rule-table body for external entry and generic
// fallback calls. Its caller owns depth and failure rollback, so it adds neither.
func (g *generator) directLeanBody(r *rule, d *direct) string {
	return g.method(r.name+" (value-free body inlined)", g.directLeanText(d))
}

// directLeanText returns the text of the method body of the value-free direct rule d.
func (g *generator) directLeanText(d *direct) string {
	var b strings.Builder
	var vs []dvar
	if d.succeed {
		vs = append(vs, dvar{"v", "*Node"})
	}
	b.WriteString(varLines(append(vs, d.vars...)))
	b.WriteString(d.body)
	if d.succeed {
		b.WriteString("return v, true\n")
	}
	if d.fail {
		b.WriteString("fail:\nreturn nil, false\n")
	}
	return b.String()
}

func directHasRecovery(e grammar.Expr) bool {
	found := false
	walkExpr(e, func(e grammar.Expr) {
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
