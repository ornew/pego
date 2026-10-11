package engine

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
)

// Program is a compiled grammar. It can be used by multiple parses concurrently.
type Program struct {
	Grammar   *grammar.Grammar
	rules     []*rule
	descTable []string  // expectation table (starting with fixedDescs)
	twins     []*rule   // value-free twins (rule.twin); ids start at len(rules)
	nseen     int       // number of rules with rule.seen set
	typed     *typeInfo // types found by the type checker (nil if it did not run)
	// projected holds the map calls of actions that read a projected repetition (project.go).
	projected     map[*grammar.Call]bool
	noProjections bool
	byName        map[string]*rule
	types         map[string]grammar.TypeSpec

	moduleOnce sync.Once
	module     *Module
	recOnce    sync.Once
	rec        *Program // program that builds no tree (recognizer)
	recErr     error
	vmOnce     sync.Once
	vm         *vmProgram
	ivmOnce    sync.Once
	ivm        *vmProgram
	pkg        string // package name when loaded from a file without the AST
	// typeKinds holds the node kinds of the grammar's types (typeKind), made with the program.
	typeKinds map[string]*nodeKind
	// scratch holds the buffers of finished parses for later ones (*scratch).
	scratch sync.Pool
}

// Options configures compilation.
type Options struct {
	// DisableMemo disables memoization (except for left-recursion leaders).
	DisableMemo bool
	// NoTypeCheck skips type checking.
	NoTypeCheck bool
	// recognize builds a program that only checks whether the input conforms to the grammar, without
	// building a tree (Program.recognizer).
	recognize bool
	// noProjections compiles repetitions as written (project.go), for tests: with projections in
	// every backend, only a program without them can check what they produce.
	noProjections bool
}

// matcher tries to match at the current position and returns the value and whether it
// succeeded. On failure the position is unspecified; the caller restores it.
type matcher func(p *parser) (*Node, bool)

type rule struct {
	// kinds are the node kinds the rule makes (newRuleKinds).
	kinds        ruleKinds
	id           int
	name         string
	def          *grammar.RuleDef
	scope        *scope
	body         func(p *parser, min int) (*Node, bool)
	action       grammar.Term
	bodyIsSeq    bool
	terminalType string
	memo         bool
	leader       bool
	pratt        *pratt
	// stream reports that the top level of the rule body has a #stream repetition.
	stream bool
	// positional reports that the result may depend on position values (analysis.positional).
	positional bool
	// act evaluates the action (nil if there is no action).
	act evaluator
	// entry is the start of the body in bytecode (only for rules of the bytecode backend).
	entry int
	// twin is the value-free twin used for calls from places where the value is discarded (@, -, !,
	// value-free bodies). It is created for rules that return a CST (no action and no terminal type)
	// and are not part of a left-recursion cycle (twinOK), when such a call exists. It matches the
	// same input in the same way, but keeps a separate memo (ids from len(rules) on).
	twin   *rule
	twinOK bool
	// vars are the variables that the rule or the rules it calls read, sorted. The rule's result is
	// memoized per combination of their values at the call (memoKey.env).
	vars []string
	// transient reports that the rule is not memoized in ordinary parses (it is memoized only by
	// the incremental Document) (transientRules).
	transient bool
	// seen is the rule's index among the rules whose memoization is deferred to the second call at a
	// position (parser.firstCall), or -1.
	seen int
	// plain reports that the rule is never memoized except by Document and has no captures, so
	// call runs it with invokePlain (the closure backend calls invokePlain directly).
	plain bool
	// novalue reports that this is a value-free twin (its value is always nil).
	novalue bool
	// predCaps holds, for value-free rules in a program that builds no tree, the names of the
	// captures referenced by predicates (other captures build no values). It is nil for ordinary
	// rules.
	predCaps map[string]bool
	// lean reports that the body's value is unused (a terminal type, or an action that does not use
	// $n), so the body runs value-free. Captures still build values.
	lean bool
}

// scope maps capture names to slots. Each rule body, repetition element, and Pratt line has its
// own scope.
type scope struct {
	names []string
	slots map[string]int
	// outer is the scope of the rule body that contains a repetition element's scope, for error
	// messages: its captures are not visible in the element.
	outer *scope
}

func newScope() *scope { return &scope{slots: map[string]int{}} }

func (s *scope) slot(name string) int {
	if i, ok := s.slots[name]; ok {
		return i
	}
	s.slots[name] = len(s.names)
	s.names = append(s.names, name)
	return s.slots[name]
}

// emptyFrame is the frame for scopes without captures (shared because it is never written to).
var emptyFrame = &frame{}

func (s *scope) newFrame() *frame {
	if len(s.names) == 0 {
		return emptyFrame
	}
	return &frame{vals: make([]*Node, len(s.names))}
}

// Error is a compile error.
type Error struct {
	Pos grammar.Pos
	Msg string
}

func (e *Error) Error() string {
	if e.Pos.IsValid() {
		return fmt.Sprintf("%d:%d: %s", e.Pos.Line, e.Pos.Col, e.Msg)
	}
	return e.Msg
}

// ErrorList is a list of compile errors.
type ErrorList []*Error

func (l ErrorList) Error() string {
	msgs := make([]string, len(l))
	for i, e := range l {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "\n")
}

// Reserved type names.
var builtinTypes = map[string]bool{
	"int": true, "string": true, "bool": true, "node": true, "terminal": true,
	TypeMatch: true, TypeSeq: true, TypeList: true, TypeOperator: true, TypeError: true,
}

type compiler struct {
	prog *Program
	proj map[string]string // projected captures of the rule being compiled (ruleProjections)
	an   *analysis
	opts Options
	errs ErrorList
	rule *rule
	// noIndex reports that a place where $n is not allowed (a predicate or a Pratt operator action)
	// is being checked.
	noIndex string
	descIDs map[string]expID
	// runSites numbers the repetitions that a Document can resume (resume.go).
	runSites int
}

// desc returns the index of an expectation string (adding it to Program.descs).
func (c *compiler) desc(s string) expID {
	if id, ok := c.descIDs[s]; ok {
		return id
	}
	if c.descIDs == nil {
		c.descIDs = map[string]expID{}
		for i, d := range fixedDescs {
			c.descIDs[d] = expID(i)
		}
		if id, ok := c.descIDs[s]; ok {
			return id
		}
	}
	id := expID(len(c.prog.descTable))
	c.prog.descTable = append(c.prog.descTable, s)
	c.descIDs[s] = id
	return id
}

func (c *compiler) errorf(pos grammar.Pos, format string, args ...any) {
	c.errs = append(c.errs, &Error{Pos: pos, Msg: fmt.Sprintf(format, args...)})
}

// Compile compiles the grammar.
func Compile(g *grammar.Grammar, opts Options) (*Program, error) {
	return build(g, opts, nil)
}

// ruleFlags are rule properties determined by static analysis. They are saved in compiled
// grammars so loading can skip the analysis.
type ruleFlags struct {
	memo, leader, positional bool
	// twin reports that the rule has a value-free twin (rule.twin).
	twin bool
}

// build builds a program from the grammar. If flags is not nil, it skips static analysis and
// type checking and uses flags instead.
func build(g *grammar.Grammar, opts Options, flags []ruleFlags) (*Program, error) {
	if err := grammar.Validate(g); err != nil {
		e := err.(*grammar.ValidationError)
		return nil, ErrorList{&Error{Pos: e.Pos, Msg: e.Path + ": " + e.Msg}}
	}
	prog := &Program{Grammar: g, byName: map[string]*rule{}, types: map[string]grammar.TypeSpec{},
		descTable: append([]string(nil), fixedDescs...)}
	c := &compiler{prog: prog, opts: opts}
	prog.noProjections = opts.noProjections

	for _, td := range g.Types() {
		switch {
		case builtinTypes[td.Name]:
			c.errorf(td.Pos, "type %s is reserved", td.Name)
		case !startsUpper(td.Name):
			c.errorf(td.Pos, "type %s must start with an uppercase letter", td.Name)
		case prog.types[td.Name] != nil:
			c.errorf(td.Pos, "type %s is already defined", td.Name)
		default:
			prog.types[td.Name] = td.Spec
		}
	}
	rules := g.Rules()
	for i, rd := range rules {
		if prog.byName[rd.Name] != nil {
			c.errorf(rd.Pos, "rule %s is already defined", rd.Name)
			continue
		}
		r := &rule{id: i, name: rd.Name, def: rd, scope: newScope(), action: rd.Action}
		prog.rules = append(prog.rules, r)
		prog.byName[rd.Name] = r
	}

	if flags == nil {
		c.an = analyze(rules)
	} else if len(flags) != len(prog.rules) {
		return nil, fmt.Errorf("compiled grammar has %d rule flags for %d rules", len(flags), len(prog.rules))
	}
	transient := transientRules(rules)
	vars := ruleVariables(rules)
	for i, r := range prog.rules {
		twin := false
		if flags != nil {
			r.memo, r.leader, r.positional, twin = flags[i].memo, flags[i].leader, flags[i].positional, flags[i].twin
		} else {
			r.leader = c.an.leaders[r.name]
			r.positional = c.an.positional[r.name]
			r.memo = !opts.DisableMemo && !c.an.cyclic[r.name]
		}
		r.transient = transient[r.name]
		r.vars = vars[r.name]
		if name, spec := prog.concreteType(r.def.Type); spec != nil {
			if _, isTerm := spec.(*grammar.TerminalSpec); isTerm {
				r.terminalType = name
			}
		}
		if flags == nil {
			_, isPratt := r.def.Expr.(*grammar.Pratt)
			twin = r.action == nil && r.terminalType == "" && !isPratt && !c.an.cyclic[r.name] && !hasStream(r.def.Expr)
		}
		r.twinOK = twin
	}
	if opts.recognize {
		// Only rules whose values are referenced by predicates, and the rules needed to build those
		// values, are evaluated as usual.
		needed := valueNeededRules(rules)
		for _, r := range prog.rules {
			if !needed[r.name] {
				r.novalue, r.action, r.terminalType, r.twinOK = true, nil, "", false
				r.predCaps = predicateCaptures(r.def.Expr)
			}
		}
	}
	for _, r := range prog.rules {
		c.compileRule(r)
	}
	// Value-free twins can be assembled the same way if the original rules have no errors. New twins
	// may be added during assembly.
	for i := 0; i < len(prog.twins) && len(c.errs) == 0; i++ {
		c.compileRule(prog.twins[i])
	}
	if len(c.errs) > 0 {
		return nil, c.errs
	}
	prog.nseen = numberSeen(prog.rules, prog.twins)
	prog.typeKinds = map[string]*nodeKind{}
	for name := range prog.types {
		canonical, st := prog.structType(name)
		if st != nil {
			prog.typeKinds[name] = kindOf(canonical, "")
		} else {
			prog.typeKinds[name] = kindOf(name, "")
		}
	}
	for _, rs := range [][]*rule{prog.rules, prog.twins} {
		for _, r := range rs {
			r.plain = !r.leader && (!r.memo || r.transient) && len(r.scope.names) == 0
		}
	}
	if !opts.NoTypeCheck && flags == nil {
		checkTypes(prog, &c.errs)
		if len(c.errs) > 0 {
			return nil, c.errs
		}
	}
	return prog, nil
}

func (c *compiler) compileRule(r *rule) {
	c.rule = r
	r.kinds = newRuleKinds(r.name, r.terminalType)
	if p, ok := r.def.Expr.(*grammar.Pratt); ok {
		c.compilePratt(r, p)
		return
	}
	_, r.bodyIsSeq = r.def.Expr.(*grammar.Seq)
	r.stream = c.checkStream(r.def.Expr)
	r.lean = r.novalue || leanBody(r)
	c.proj = ruleProjections(c.prog, r)
	m := c.expr(r.def.Expr, r.scope, !r.lean)
	if c.proj != nil {
		if c.prog.projected == nil {
			c.prog.projected = map[*grammar.Call]bool{}
		}
		markProjectedMaps(r.action, c.proj, c.prog.projected)
	}
	c.proj = nil
	r.body = func(p *parser, _ int) (*Node, bool) { return m(p) }
	if r.action != nil {
		c.checkTerm(r.action, r.scope, nil)
		r.act = termEvaluator(r.action)
	}
}

// leanBody reports whether the value of the rule body is unused. It is unused when there is an
// action that does not use the body's value (does not refer to $n), or when the rule has no
// action and a terminal type, provided it has no #stream. Every backend and the code generator
// use this test to compile the body value-free.
func leanBody(r *rule) bool {
	if r.stream {
		return false
	}
	if r.action != nil {
		uses := false
		walkTerm(r.action, func(t grammar.Term) {
			if _, ok := t.(*grammar.IndexRef); ok {
				uses = true
			}
		})
		return !uses
	}
	return r.terminalType != ""
}

// twinOf returns the value-free twin of rule r (creating it if needed; it is assembled later).
func (c *compiler) twinOf(r *rule) *rule {
	if r.twin == nil {
		prog := c.prog
		r.twin = &rule{id: len(prog.rules) + len(prog.twins), name: r.name, def: r.def, scope: newScope(),
			memo: r.memo, transient: r.transient, vars: r.vars, positional: r.positional, novalue: true}
		prog.twins = append(prog.twins, r.twin)
	}
	return r.twin
}

// transientRules returns the rules that are never re-evaluated at the same position even without
// memoization.
//   - Rules that call no other rules: re-evaluating them just rescans the input once.
//   - Rules referenced only once in the grammar: they are re-evaluated at the same position only
//     when their single caller is re-evaluated at the same position. Following the callers
//     eventually reaches a memoized rule or the start rule.
//
// When the caller is memoized, memoizing such a rule never yields reuse, so it only adds the
// cost of the memo.
func transientRules(rules []*grammar.RuleDef) map[string]bool {
	refs := map[string]int{}
	for _, rd := range rules {
		for _, name := range allCalls(rd.Expr, nil) {
			refs[name]++
		}
	}
	out := map[string]bool{}
	for _, rd := range rules {
		out[rd.Name] = refs[rd.Name] < 2 || len(allCalls(rd.Expr, nil)) == 0
	}
	return out
}

// predicateCaptures returns the names of the captures referenced by predicates in the expression.
func predicateCaptures(e grammar.Expr) map[string]bool {
	names := map[string]bool{}
	walkExpr(e, func(x grammar.Expr) {
		if pr, ok := x.(*grammar.Predicate); ok {
			walkTerm(pr.Term, func(t grammar.Term) {
				if cr, ok := t.(*grammar.CaptureRef); ok {
					names[cr.Name] = true
				}
			})
		}
	})
	return names
}

// valueNeededRules returns the rules that must build values even in a program that builds no
// tree: the rules called within captures referenced by predicates, and every rule they call.
func valueNeededRules(rules []*grammar.RuleDef) map[string]bool {
	byName := map[string]*grammar.RuleDef{}
	for _, rd := range rules {
		byName[rd.Name] = rd
	}
	needed := map[string]bool{}
	var mark func(name string)
	mark = func(name string) {
		if needed[name] || byName[name] == nil {
			return
		}
		needed[name] = true
		for _, callee := range allCalls(byName[name].Expr, nil) {
			mark(callee)
		}
	}
	for _, rd := range rules {
		pc := predicateCaptures(rd.Expr)
		walkExpr(rd.Expr, func(x grammar.Expr) {
			if cp, ok := x.(*grammar.Capture); ok && pc[cp.Name] {
				for _, callee := range allCalls(cp.Expr, nil) {
					mark(callee)
				}
			}
		})
	}
	return needed
}

// startsUpper reports whether name starts with an uppercase letter, as user-defined type and field
// names must.
func startsUpper(name string) bool {
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(r)
}

// hasStream reports whether the expression contains #stream.
func hasStream(e grammar.Expr) bool {
	found := false
	walkExpr(e, func(x grammar.Expr) {
		if isStream(x) {
			found = true
		}
	})
	return found
}

// noCaptures reports captures inside value-free expressions (@, -, !, skip) as errors.
func (c *compiler) noCaptures(e grammar.Expr) {
	walkExpr(e, func(x grammar.Expr) {
		if cp, ok := x.(*grammar.Capture); ok {
			c.errorf(cp.Pos, "capture %s inside @, -, or ! has no effect", cp.Name)
		}
	})
}

// visible reports whether the expression has a value (a CST child).
func visible(e grammar.Expr) bool {
	switch e := e.(type) {
	case *grammar.And, *grammar.Not, *grammar.Discard, *grammar.Cut, *grammar.Bottom,
		*grammar.BeginInput, *grammar.EndInput, *grammar.BeginLine, *grammar.EndLine,
		*grammar.Predicate:
		return false
	case *grammar.Capture:
		return visible(e.Expr)
	case *grammar.Attributed:
		return visible(e.Expr)
	}
	return true
}

// hasCaptures reports whether the expression contains captures (excluding nested repetitions).
// elementScoped reports whether the repetition element e gets a capture scope of its own per
// iteration: when it contains captures (hasCaptures), or its predicates read captures, which can
// then only be its own (made inside &) or, an error, those of the enclosing scope. Every backend
// and generator must decide the same way, since the scope decides where captures are written.
func elementScoped(e grammar.Expr) bool {
	return hasCaptures(e) || predicatesReadCaptures(e)
}

// predicatesReadCaptures reports whether a predicate in e refers to a capture.
func predicatesReadCaptures(e grammar.Expr) bool {
	found := false
	walkExpr(e, func(x grammar.Expr) {
		if p, ok := x.(*grammar.Predicate); ok {
			walkTerm(p.Term, func(t grammar.Term) {
				if _, ok := t.(*grammar.CaptureRef); ok {
					found = true
				}
			})
		}
	})
	return found
}

func hasCaptures(e grammar.Expr) bool {
	switch e := e.(type) {
	case *grammar.Capture:
		return true
	case *grammar.Seq:
		for _, it := range e.Items {
			if hasCaptures(it) {
				return true
			}
		}
	case *grammar.Choice:
		for _, alt := range e.Alts {
			if hasCaptures(alt) {
				return true
			}
		}
	case *grammar.Optional:
		return hasCaptures(e.Expr)
	case *grammar.Attributed:
		return hasCaptures(e.Expr)
	}
	return false
}

func quote(s string) string { return strconv.Quote(s) }

// expr compiles the expression. If build is false, it only matches without building values.
func (c *compiler) expr(e grammar.Expr, s *scope, build bool) matcher {
	switch e := e.(type) {
	case *grammar.Literal:
		value := literalValue(e.Value)
		rs, bs := []rune(value), []byte(value)
		desc := c.desc(quote(e.Value))
		return func(p *parser) (*Node, bool) {
			start := p.pos
			if !p.matchLiteral(rs, bs) {
				p.expect(start, desc)
				return nil, false
			}
			if !build {
				return nil, true
			}
			return p.newNode(Node{kind: kindMatch, Start: int32(start), End: int32(p.pos), Text: p.literalText(start, value), terminal: true, fresh: true}), true
		}

	case *grammar.CharClass:
		desc := c.desc(charClassString(e))
		return c.single(build, desc, classAccept(e))

	case *grammar.Any:
		return c.single(build, idAny, func(rune) bool { return true })

	case *grammar.Ref:
		r := c.prog.byName[e.Name]
		if r == nil {
			c.errorf(e.Pos, "undefined rule %s", e.Name)
			return nil
		}
		min := 0
		if e.Level != "" {
			min = c.levelMin(e, r)
		}
		if !build && r.twinOK {
			r = c.twinOf(r)
		}
		return func(p *parser) (*Node, bool) {
			if r.plain && !p.noPlain {
				return p.invokePlain(r, min) // what call does for such a rule, without its checks
			}
			return p.call(r, min)
		}

	case *grammar.Seq:
		ms := make([]matcher, len(e.Items))
		vis := make([]bool, len(e.Items))
		nvis := 0
		for i, it := range e.Items {
			ms[i] = c.expr(it, s, build)
			vis[i] = visible(it)
			if vis[i] {
				nvis++
			}
		}
		return func(p *parser) (*Node, bool) {
			start := p.pos
			var kids []*Node
			if build {
				kids = p.nodes(nvis)[:0]
			}
			for i, m := range ms {
				v, ok := m(p)
				if !ok {
					return nil, false
				}
				if build && vis[i] {
					kids = append(kids, v)
				}
			}
			if !build {
				return nil, true
			}
			return p.newNode(Node{kind: kindSeq, Start: int32(start), End: int32(p.pos), Children: kids, fresh: true}), true
		}

	case *grammar.Choice:
		ms := make([]matcher, len(e.Alts))
		guards := make([]firstGuard, len(e.Alts))
		guarded := false
		for i, alt := range e.Alts {
			ms[i] = c.expr(alt, s, build)
			guards[i] = c.first(alt)
			guarded = guarded || guards[i].accept != nil
		}
		if guarded {
			return func(p *parser) (*Node, bool) {
				m0 := p.mark()
				var ch rune
				more, peeked := false, false
				for i, m := range ms {
					if g := &guards[i]; g.accept != nil {
						if !peeked {
							ch, _, more = p.peek()
							peeked = true
						}
						if !(more && g.accept(ch)) && p.depth+g.depth <= p.maxDepth {
							p.expect(p.pos, g.desc) // what the alternative would record
							continue
						}
					}
					prevCut := p.cut
					p.cut = false
					v, ok := m(p)
					cut := p.cut
					p.cut = prevCut
					if ok {
						return v, true
					}
					p.reset(m0)
					if cut {
						return nil, false
					}
				}
				return nil, false
			}
		}
		return func(p *parser) (*Node, bool) {
			m0 := p.mark()
			for _, m := range ms {
				prevCut := p.cut
				p.cut = false
				v, ok := m(p)
				cut := p.cut
				p.cut = prevCut
				if ok {
					return v, true
				}
				p.reset(m0)
				if cut {
					return nil, false
				}
			}
			return nil, false
		}

	case *grammar.Repeat:
		return c.repeat(e, s, build, false)

	case *grammar.Optional:
		m := c.expr(e.Expr, s, build)
		return func(p *parser) (*Node, bool) {
			m0 := p.mark()
			prevCut := p.cut
			p.cut = false
			v, ok := m(p)
			cut := p.cut
			p.cut = prevCut
			if ok {
				return v, true
			}
			p.reset(m0)
			if cut {
				return nil, false
			}
			return nil, true
		}

	case *grammar.And:
		// Captures and variable definitions inside a positive lookahead remain valid after it.
		m := c.expr(e.Expr, s, build && hasCaptures(e.Expr))
		return func(p *parser) (*Node, bool) {
			start, prevCut := p.pos, p.cut
			p.silent++
			_, ok := m(p)
			p.silent--
			p.cut = prevCut
			p.pos = start
			return nil, ok
		}

	case *grammar.Not:
		c.noCaptures(e.Expr)
		m := c.expr(e.Expr, s, false)
		return func(p *parser) (*Node, bool) {
			m0 := p.mark()
			prevCut := p.cut
			p.silent++
			_, ok := m(p)
			p.silent--
			p.cut = prevCut
			p.reset(m0)
			return nil, !ok
		}

	case *grammar.Atomic:
		c.noCaptures(e.Expr)
		m := c.expr(e.Expr, s, false)
		return func(p *parser) (*Node, bool) {
			start := p.pos
			if _, ok := m(p); !ok {
				return nil, false
			}
			if !build {
				return nil, true
			}
			return p.newNode(Node{kind: kindMatch, Start: int32(start), End: int32(p.pos), Text: p.text(start, p.pos), terminal: true, fresh: true}), true
		}

	case *grammar.Discard:
		c.noCaptures(e.Expr)
		m := c.expr(e.Expr, s, false)
		return func(p *parser) (*Node, bool) {
			_, ok := m(p)
			return nil, ok
		}

	case *grammar.Capture:
		if !build && c.rule != nil && c.rule.predCaps != nil && !c.rule.predCaps[e.Name] {
			// In a program that builds no tree, captures not referenced by predicates are not recorded.
			return c.expr(e.Expr, s, false)
		}
		// Even inside a value-free body (leanBody), captures build and record their values.
		if !visible(e.Expr) {
			c.errorf(e.Pos, "capture %s of an expression without a value", e.Name)
			return nil
		}
		slot := s.slot(e.Name)
		var m matcher
		if field := c.proj[e.Name]; field != "" {
			m = c.projectRepeat(e.Expr.(*grammar.Repeat), field)
		} else {
			m = c.expr(e.Expr, s, true)
		}
		return func(p *parser) (*Node, bool) {
			v, ok := m(p)
			if ok {
				p.setCapture(slot, v)
			}
			if !build {
				return nil, ok
			}
			return v, ok
		}

	case *grammar.Cut:
		return func(p *parser) (*Node, bool) {
			p.cut = true
			return nil, true
		}

	case *grammar.Top:
		return func(p *parser) (*Node, bool) {
			if !build {
				return nil, true
			}
			return p.newNode(Node{kind: kindMatch, Start: int32(p.pos), End: int32(p.pos), terminal: true, fresh: true}), true
		}

	case *grammar.Bottom:
		return func(p *parser) (*Node, bool) { return nil, false }

	case *grammar.BeginInput:
		return anchor(func(p *parser) bool { return p.atInputStart() }, idBeginInput)
	case *grammar.EndInput:
		return anchor(func(p *parser) bool { return p.atEOF() }, idEndInput)
	case *grammar.BeginLine:
		return anchor(func(p *parser) bool { return p.atLineStart() }, idBeginLine)
	case *grammar.EndLine:
		return anchor(func(p *parser) bool {
			ch, _, ok := p.peek()
			return !ok || ch == '\n' || ch == '\r'
		}, idEndLine)

	case *grammar.Predicate:
		return c.predicate(e, s)

	case *grammar.Attributed:
		return c.attributed(e, s, build)

	case *grammar.Pratt:
		c.errorf(e.Pos, "pratt is only allowed as the whole body of a rule")
		return nil
	}
	c.errorf(grammar.Pos{}, "unsupported expression %T", e)
	return nil
}

func anchor(cond func(p *parser) bool, desc expID) matcher {
	return func(p *parser) (*Node, bool) {
		if cond(p) {
			return nil, true
		}
		p.expect(p.pos, desc)
		return nil, false
	}
}

// single compiles an expression that matches one character.
// firstTerminal returns the terminal (a *grammar.Literal or *grammar.CharClass) that the expression
// e must begin with, if any, and the number of rule calls on the way: e is the terminal, possibly
// behind sequences, captures, @, - and calls of rules that are neither left-recursive nor Pratt.
// When the next character cannot start the terminal, e fails at once, recording only the
// terminal's expectation and examining only that character (a call memoizes the same failure, and
// memoization only affects speed), so a choice can skip e: first-character dispatch, done by the
// closure backend, the VMs (GUARD) and generated parsers, unless the calls on the way would exceed
// the nesting limit.
func firstTerminal(prog *Program, e grammar.Expr, depth int, seen map[string]bool) (grammar.Expr, int) {
	switch e := e.(type) {
	case *grammar.Literal:
		if e.Value != "" {
			return e, depth
		}
	case *grammar.CharClass:
		return e, depth
	case *grammar.Seq:
		if len(e.Items) > 0 {
			return firstTerminal(prog, e.Items[0], depth, seen)
		}
	case *grammar.Capture:
		return firstTerminal(prog, e.Expr, depth, seen)
	case *grammar.Atomic:
		return firstTerminal(prog, e.Expr, depth, seen)
	case *grammar.Discard:
		return firstTerminal(prog, e.Expr, depth, seen)
	case *grammar.Ref:
		r := prog.byName[e.Name]
		if r == nil || r.leader || seen[e.Name] || r.def == nil {
			break
		}
		if _, pratt := r.def.Expr.(*grammar.Pratt); pratt {
			break
		}
		seen[e.Name] = true
		return firstTerminal(prog, r.def.Expr, depth+1, seen)
	}
	return nil, 0
}

// firstGuard tells when an alternative of a choice can be skipped (see firstTerminal).
type firstGuard struct {
	accept func(rune) bool // nil if it cannot be
	desc   expID
	depth  int
}

func (c *compiler) first(e grammar.Expr) firstGuard {
	switch t, depth := firstTerminal(c.prog, e, 0, map[string]bool{}); t := t.(type) {
	case *grammar.Literal:
		r0, _ := utf8.DecodeRuneInString(t.Value)
		return firstGuard{func(r rune) bool { return r == r0 }, c.desc(quote(t.Value)), depth}
	case *grammar.CharClass:
		return firstGuard{classAccept(t), c.desc(charClassString(t)), depth}
	}
	return firstGuard{}
}

func (c *compiler) single(build bool, desc expID, accept func(rune) bool) matcher {
	return func(p *parser) (*Node, bool) {
		ch, size, ok := p.peek()
		if !ok || !accept(ch) {
			p.expect(p.pos, desc)
			return nil, false
		}
		start := p.pos
		p.pos += size
		if !build {
			return nil, true
		}
		return p.newNode(Node{kind: kindMatch, Start: int32(start), End: int32(p.pos), Text: p.text(start, p.pos), terminal: true, fresh: true}), true
	}
}

func charClassString(e *grammar.CharClass) string {
	var b strings.Builder
	b.WriteString("(?")
	if e.Negated {
		b.WriteString("^")
	}
	esc := func(r rune) string {
		switch r {
		case '-', '(', ')', '?', '^', '\\':
			return `\` + string(r)
		}
		s := strconv.QuoteRune(r)
		return s[1 : len(s)-1]
	}
	for _, rg := range e.Ranges {
		b.WriteString(esc(rg.Lo))
		if rg.Hi != rg.Lo {
			b.WriteString("-" + esc(rg.Hi))
		}
	}
	b.WriteString(")")
	return b.String()
}

// classAccept returns the membership test function of a character class.
// classAccept returns the membership test of a character class. Classes of one or two ranges,
// which are most of them, compare against constants; others test ASCII characters with a bitmap.
func classAccept(e *grammar.CharClass) func(rune) bool {
	neg := e.Negated
	switch len(e.Ranges) {
	case 1:
		lo, hi := e.Ranges[0].Lo, e.Ranges[0].Hi
		return func(r rune) bool { return (lo <= r && r <= hi) != neg }
	case 2:
		lo1, hi1, lo2, hi2 := e.Ranges[0].Lo, e.Ranges[0].Hi, e.Ranges[1].Lo, e.Ranges[1].Hi
		return func(r rune) bool { return (lo1 <= r && r <= hi1 || lo2 <= r && r <= hi2) != neg }
	}
	ranges := append([]grammar.CharRange(nil), e.Ranges...)
	slow := func(r rune) bool {
		for _, rg := range ranges {
			if rg.Lo <= r && r <= rg.Hi {
				return !neg
			}
		}
		return neg
	}
	var ascii [2]uint64
	for r := rune(0); r < 128; r++ {
		if slow(r) {
			ascii[r>>6] |= 1 << (r & 63)
		}
	}
	return func(r rune) bool {
		if uint32(r) < 128 {
			return ascii[r>>6]&(1<<(r&63)) != 0
		}
		return slow(r)
	}
}

// scanRepeat returns a matcher that advances over a repetition of a value-free single-character
// expression (a character class or .) without saving and restoring state per element (nil if not
// applicable). Expectations are recorded as for a general repetition (once, at the position
// that did not match last).
func (c *compiler) scanRepeat(e *grammar.Repeat, build bool) matcher {
	if build {
		return nil
	}
	var accept func(rune) bool
	var desc expID
	switch x := e.Expr.(type) {
	case *grammar.CharClass:
		accept, desc = classAccept(x), c.desc(charClassString(x))
	case *grammar.Any:
		accept, desc = func(rune) bool { return true }, idAny
	default:
		return nil
	}
	min, max := e.Min, e.Max
	return func(p *parser) (*Node, bool) {
		count := 0
		for max < 0 || count < max {
			ch, size, ok := p.peek()
			if !ok || !accept(ch) {
				p.expect(p.pos, desc)
				break
			}
			p.pos += size
			count++
		}
		return nil, count >= min
	}
}

// repeat compiles a repetition. If stream is true, elements are passed to emit when parsing a
// stream.
func (c *compiler) repeat(e *grammar.Repeat, s *scope, build, stream bool) matcher {
	if !stream {
		if m := c.scanRepeat(e, build); m != nil {
			return m
		}
	}
	elemScope := s
	if elementScoped(e.Expr) {
		elemScope = newScope()
		elemScope.outer = s
	}
	m := c.expr(e.Expr, elemScope, build)
	ownScope := elemScope != s
	min, max := e.Min, e.Max
	var rs *runSite
	if c.resumable(e.Expr) { // a Document parse is not a stream: #stream has no effect there
		c.runSites++
		rs = &runSite{id: c.runSites, m: m, scope: elemScope, ownScope: ownScope, build: build, min: min, max: max, shiftable: !c.positional(e.Expr)}
	}
	return func(p *parser) (*Node, bool) {
		if rs != nil && p.runs != nil && p.silent == 0 {
			return p.resumeRepeat(rs)
		}
		start := p.pos
		base := len(p.kidStack) // collect the element values
		if streamPrefixIsolation && stream && p.emit != nil && p.depth == 1 {
			p.startStreamChunks()
		}
		count := 0
		for n := 0; max < 0 || n < max; n++ {
			m0 := p.mark()
			prevCut, prevFrame := p.cut, p.frame
			p.cut = false
			var f *frame
			if ownScope {
				f = p.newFrame(len(elemScope.names))
				p.frame = f
			}
			v, ok := m(p)
			cut := p.cut
			p.cut, p.frame = prevCut, prevFrame
			if !ok {
				p.reset(m0)
				if cut {
					p.kids(base)
					return nil, false
				}
				break
			}
			if ownScope && build {
				v = p.attachCaptures(v, elemScope, f, m0.pos, p.pos)
			}
			count++
			if stream && p.emit != nil && p.depth == 1 {
				// Elements of the start rule's #stream are passed on without being retained, and committed.
				if v != nil {
					v.fresh = false
				}
				if err := p.emit(v); err != nil {
					panic(fatal{err})
				}
				p.commit(p.pos)
				if p.pos == m0.pos {
					break
				}
				continue
			}
			if build {
				p.kidStack = append(p.kidStack, v)
			}
			if p.pos == m0.pos {
				// Stop a repetition that consumes no input.
				break
			}
		}
		if count < min {
			p.kids(base)
			return nil, false
		}
		if !build {
			return nil, true
		}
		return p.newNode(Node{kind: kindList, Start: int32(start), End: int32(p.pos), Children: p.kids(base), fresh: true}), true
	}
}

// projectRepeat compiles a repetition that gathers the values of the capture field of its elements
// (project.go). The elements build no values of their own, and share one capture frame: nothing
// refers to an element's frame once its value is taken.
func (c *compiler) projectRepeat(e *grammar.Repeat, field string) matcher {
	elemScope := newScope()
	m := c.expr(e.Expr, elemScope, false)
	slot, n := elemScope.slots[field], len(elemScope.names)
	min, max := e.Min, e.Max
	return func(p *parser) (*Node, bool) {
		start := p.pos
		base := len(p.kidStack)
		f := p.newFrame(n)
		count := 0
		for i := 0; max < 0 || i < max; i++ {
			clear(f.vals)
			m0 := p.mark()
			prevCut, prevFrame := p.cut, p.frame
			p.cut, p.frame = false, f
			_, ok := m(p)
			cut := p.cut
			p.cut, p.frame = prevCut, prevFrame
			if !ok {
				p.reset(m0)
				if cut {
					p.kids(base)
					return nil, false
				}
				break
			}
			count++
			p.kidStack = append(p.kidStack, f.vals[slot])
			if p.pos == m0.pos {
				break
			}
		}
		if count < min {
			p.kids(base)
			return nil, false
		}
		return p.newNode(Node{kind: kindList, Start: int32(start), End: int32(p.pos), Children: p.kids(base), fresh: true}), true
	}
}

// levelMin returns the minimum binding level of a call with a binding level.
func (c *compiler) levelMin(e *grammar.Ref, r *rule) int {
	pr, ok := r.def.Expr.(*grammar.Pratt)
	if !ok {
		c.errorf(e.Pos, "rule %s has no pratt levels", e.Name)
		return 0
	}
	for i, l := range pr.Levels {
		if l.Name == e.Level {
			return i // parse binding levels i+1 and above
		}
	}
	c.errorf(e.Pos, "rule %s has no level %s", e.Name, e.Level)
	return 0
}

// isStream reports whether the expression is an expression with the #stream attribute.
func isStream(e grammar.Expr) bool {
	a, ok := e.(*grammar.Attributed)
	if !ok {
		return false
	}
	for _, at := range a.Attrs {
		if at.Name == "stream" {
			return true
		}
	}
	return false
}

// checkStream checks that #stream appears only at the top level of a rule body (the body itself
// or an element of a sequence) and reports whether it is present.
func (c *compiler) checkStream(body grammar.Expr) bool {
	top := map[grammar.Expr]bool{}
	items := []grammar.Expr{body}
	if seq, ok := body.(*grammar.Seq); ok {
		items = seq.Items
	}
	for _, it := range items {
		top[it] = true
		if c, ok := it.(*grammar.Capture); ok {
			top[c.Expr] = true
		}
	}
	n := 0
	walkExpr(body, func(e grammar.Expr) {
		if !isStream(e) {
			return
		}
		n++
		a := e.(*grammar.Attributed)
		for _, attr := range a.Attrs {
			if attr.Name == "recover" {
				c.errorf(attr.Pos, "#recover cannot wrap a #stream repetition; recover inside each element instead")
			}
		}
		if !top[e] {
			c.errorf(a.Attrs[0].Pos, "#stream is only allowed at the top level of a rule body")
		} else if n > 1 {
			c.errorf(a.Attrs[0].Pos, "a rule body can have only one #stream")
		}
	})
	return n > 0
}
