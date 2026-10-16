package engine

import (
	"fmt"
	"strings"

	"github.com/ornew/pego/grammar"
)

// The ordinary runtime is kept byte-for-byte unchanged. Only opt-in output
// receives observation fields, wrappers, options and evaluation/memo hooks.
func generatedTraceRuntime(rt string) (string, error) {
	for _, change := range [][2]string{
		{"type parseOptions struct {", "type parseOptions struct {\ntrace func(TraceEvent)\nprofile *Profile\ntraceRules []string\ntraceMaxDepth int"},
		{"type parser struct {", "type parser struct {\nobservation *observation"},
		{"p.control = o.control", `p.control = o.control
if o.trace != nil || o.profile != nil {
 p.observation = &observation{trace: o.trace, maxDepth: o.traceMaxDepth, profile: newProfileObservation(o.profile)}
 if len(o.traceRules) != 0 {
  p.observation.rules = make(map[string]bool, len(o.traceRules))
  for _, name := range o.traceRules { p.observation.rules[name] = true }
 }
}`},
		{"case contextPanic:", "case tracePanic:\npanic(x.value)\ncase contextPanic:"},
		{"func (p *parser) call(r *rule, min int)", "func (p *parser) callObservedBody(r *rule, min int)"},
		{"return p.invokePlain(r, min)", "return p.invokePlainObservedBody(r, min)"},
		{"func (p *parser) invokePlain(r *rule, min int)", "func (p *parser) invokePlainObservedBody(r *rule, min int)"},
		{"\t\tif !e.growing {\n\t\t\tp.mergeExpected", "\t\tp.observation.memoHit()\n\t\tif !e.growing {\n\t\t\tp.mergeExpected"},
	} {
		if strings.Count(rt, change[0]) != 1 {
			return "", fmt.Errorf("generated trace runtime anchor changed: %q", change[0])
		}
		rt = strings.Replace(rt, change[0], change[1], 1)
	}
	const eval = "v, ok := r.body(p, min)"
	if strings.Count(rt, eval) != 2 {
		return "", fmt.Errorf("generated trace evaluation anchors changed")
	}
	rt = strings.ReplaceAll(rt, eval, "p.observation.evaluated()\n"+eval)
	return rt, nil
}

func (g *generator) tracePlainWrapper(r *rule, name string) string {
	return g.traceCallWrapper(r, name, true)
}

func (g *generator) traceCallWrapper(r *rule, name string, evaluated bool) string {
	body := name + "ObservedBody"
	eval := ""
	if evaluated {
		eval = "p.observation.evaluated()\n"
	}
	fmt.Fprintf(&g.methods, `// %s observes one actual direct rule call.
func (p *%s) %s() (%s, bool) {
 if p.observation == nil { return p.%s() }
 p.observation.enter(%q, p.pos, 0)
 h := p.examinedBegin()
 %sv, ok := p.%s()
 x, known := p.examinedEnd(h)
 p.observation.exit(ok, p.pos, x, known)
 return v, ok
}

`, name, g.recv(), name, g.valType(), body, r.name, eval, body)
	return body
}

// A guard may suppress a whole sequence or a chain of rule references. Report
// only its first identifiable rule call site, without inventing nested calls.
func (g *generator) traceSkip(expr grammar.Expr) string {
	if !g.observability {
		return ""
	}
	for {
		switch e := expr.(type) {
		case *grammar.Seq:
			if len(e.Items) == 0 {
				return ""
			}
			expr = e.Items[0]
		case *grammar.Capture:
			expr = e.Expr
		case *grammar.Atomic:
			expr = e.Expr
		case *grammar.Discard:
			expr = e.Expr
		case *grammar.Ref:
			return fmt.Sprintf("\np.observation.skipped(%q, p.pos, 0)\n", e.Name)
		default:
			return ""
		}
	}
}

const generatedTraceWrappers = `
// WithTrace observes actual generated calls synchronously. Callback panics
// propagate unchanged after cleanup; an aborted parse can omit exit events.
// Events do not expose input or parser state. ExaminedKnown is true only for
// Document parses, where Examined is the frontier used for edit reuse.
func WithTrace(trace func(TraceEvent)) ParseOption {
 return func(o *parseOptions) { o.trace = trace }
}

// WithProfile accumulates complete generated rule-call metrics. It is independent
// of trace delivery filters. A Profile must not be shared by concurrent parses.
func WithProfile(profile *Profile) ParseOption {
 return func(o *parseOptions) { o.profile = profile }
}

// WithTraceFilter selects named rules and their nested calls. Unknown names
// match nothing. With no names, every entered rule is selected.
func WithTraceFilter(rules ...string) ParseOption {
 names := append([]string(nil), rules...)
 return func(o *parseOptions) { o.traceRules = names }
}

// WithTraceMaxDepth limits delivered events, counting the start rule as one.
// Zero and negative values leave delivery unlimited; parsing is unaffected.
func WithTraceMaxDepth(n int) ParseOption {
 return func(o *parseOptions) { o.traceMaxDepth = n }
}

func (p *parser) call(r *rule, min int) (*Node, bool) {
 if p.observation == nil { return p.callObservedBody(r, min) }
 p.observation.enter(r.name, p.pos, min)
 h := p.examinedBegin()
 v, ok := p.callObservedBody(r, min)
 x, known := p.examinedEnd(h)
 p.observation.exit(ok, p.pos, x, known)
 return v, ok
}

func (p *parser) invokePlain(r *rule, min int) (*Node, bool) {
 if p.observation == nil { return p.invokePlainObservedBody(r, min) }
 p.observation.enter(r.name, p.pos, min)
 h := p.examinedBegin()
 v, ok := p.invokePlainObservedBody(r, min)
 x, known := p.examinedEnd(h)
 p.observation.exit(ok, p.pos, x, known)
 return v, ok
}
`

// Typed AST entry points can bypass both the Node runtime and the generic typed
// call path. The generated s/u entry wrappers cover those calls; runtime hooks
// account for general calls and memo/growth work without adding another frame.
func generatedTypedTraceRuntime(rt string) (string, error) {
	for _, change := range [][2]string{
		{"p.control = o.control", `p.control = o.control
if o.trace != nil || o.profile != nil {
 p.observation = &observation{trace: o.trace, maxDepth: o.traceMaxDepth, profile: newProfileObservation(o.profile)}
 if len(o.traceRules) != 0 {
  p.observation.rules = make(map[string]bool, len(o.traceRules))
  for _, name := range o.traceRules { p.observation.rules[name] = true }
 }
}`},
		{"func (p *tparser) call(r *trule, min int)", "func (p *tparser) callObservedBody(r *trule, min int)"},
		{"return p.invokePlain(r, min)", "return p.invokePlainObservedBody(r, min)"},
		{"func (p *tparser) invokePlain(r *trule, min int)", "func (p *tparser) invokePlainObservedBody(r *trule, min int)"},
	} {
		if strings.Count(rt, change[0]) != 1 {
			return "", fmt.Errorf("generated typed trace runtime anchor changed: %q", change[0])
		}
		rt = strings.Replace(rt, change[0], change[1], 1)
	}
	const memo = "\t\tif !e.growing {\n\t\t\tp.mergeExpected"
	const eval = "v, ok := r.body(p, min)"
	if strings.Count(rt, memo) != 2 || strings.Count(rt, eval) != 2 {
		return "", fmt.Errorf("generated typed trace memo/evaluation anchors changed")
	}
	rt = strings.ReplaceAll(rt, memo, "\t\tp.observation.memoHit()\n"+memo)
	rt = strings.ReplaceAll(rt, eval, "p.observation.evaluated()\n"+eval)
	return rt + generatedTypedTraceWrappers, nil
}

const generatedTypedTraceWrappers = `
func (p *tparser) call(r *trule, min int) (any, bool) {
 if p.observation == nil { return p.callObservedBody(r, min) }
 p.observation.enter(r.name, p.pos, min)
 h := p.examinedBegin()
 v, ok := p.callObservedBody(r, min)
 x, known := p.examinedEnd(h)
 p.observation.exit(ok, p.pos, x, known)
 return v, ok
}

func (p *tparser) invokePlain(r *trule, min int) (any, bool) {
 if p.observation == nil { return p.invokePlainObservedBody(r, min) }
 p.observation.enter(r.name, p.pos, min)
 h := p.examinedBegin()
 v, ok := p.invokePlainObservedBody(r, min)
 x, known := p.examinedEnd(h)
 p.observation.exit(ok, p.pos, x, known)
 return v, ok
}
`
