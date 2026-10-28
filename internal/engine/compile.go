package engine

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/bytecode"
	"github.com/ornew/pego/internal/value"
)

// staticProgram is what the compiler found out about a grammar: its rules with their static facts,
// its types and expectations, and its bytecode module. Every backend and generator reads it; a
// Program adds what the parser runs.
type staticProgram struct {
	hasMissing bool // immutable syntax metadata; bytecode/generation are unsupported
	// shapeControl is the compilation's cancellation control while the program is being built, so that
	// the shape analysis of rule bodies (shapeElems) polls it; nil otherwise.
	shapeControl *compileControl
	Grammar      *grammar.Grammar
	// source is the grammar as written when Grammar is its lowering (inline actions replaced by
	// hidden rules, inline_actions.go); nil when Grammar is the grammar itself.
	source *grammar.Grammar
	// unsupportedSeen holds the type syntax reported as not supported yet while compiling, so that
	// an alias resolved from several places, by several checkers, is reported once.
	unsupportedSeen map[grammar.TypeExpr]bool
	rules           []*staticRule
	descTable       []string      // expectation table (starting with fixedDescs)
	twins           []*staticRule // value-free twins (staticRule.twin); ids start at len(rules)
	nseen           int           // number of rules with staticRule.seen set
	typed           *typeInfo     // types found by the type checker (nil if it did not run)
	// projected holds the map calls of actions that read a projected repetition (project.go).
	projected     map[*grammar.Call]bool
	noProjections bool
	// sourceInfo reports that nodes record source information (Options.SourceInfo).
	sourceInfo bool
	// defaultValue reports that value actions and declared rule types were dropped
	// (Options.DefaultValue).
	defaultValue bool
	byName       map[string]*staticRule
	// starts holds the start rules in declaration order and defaultStart the default one
	// (resolveStarts).
	starts       []string
	defaultStart string
	types        map[string]grammar.TypeDecl
	// vars holds the declared variables (nil if there are none).
	vars *varTable
	// fieldTypes caches the types of casts (castType).
	fieldTypes sync.Map
	// workRecognize and workRecognizer are Options.recognize and Options.recognizer.
	workRecognize, workRecognizer bool

	moduleOnce sync.Once
	module     *bytecode.Module
	// typeKinds holds the node kinds of the grammar's types (typeKind), made with the program.
	typeKinds map[string]*value.NodeKind
	// nodeKinds holds the kinds of the grammar's named node types, also under the names of the
	// aliases of them.
	nodeKinds map[string]*value.NodeTypeKinds
	// nodeTypeCache holds the results of nodeTypeOf by type name: a nodeTy, or false.
	nodeTypeCache sync.Map
}

// Options configures compilation.
type Options struct {
	// DisableMemo disables memoization (except for left-recursion leaders).
	DisableMemo bool
	// SourceInfo records source information in the values: the rule that produced each node and
	// where its captures are.
	SourceInfo bool
	// DefaultValue makes every rule produce the default value of its expression: value actions
	// (=>) and the declared types of rules are dropped before compilation, so the parser returns
	// the default node tree even for grammars with actions. Predicates still run, because they
	// decide the match, and the rules that make the values predicates read keep their actions and
	// types (defaultValueGrammar).
	DefaultValue bool
	// controlledDispatch builds only the metered closure graph for lazy work setup.
	controlledDispatch bool
	// recognize builds a program that only checks whether the input conforms to the grammar, without
	// building a tree (Program.recognizer).
	recognize bool
	// recognizer marks the program Program.recognizer builds, which only runs with Recognize: it
	// builds no stream elements either.
	recognizer bool
	// noProjections compiles repetitions as written (project.go), for tests: with projections in
	// every backend, only a program without them can check what they produce.
	noProjections bool
	// eagerAttributeCopies disables copy-on-change normalization for measurements.
	// It affects compilation only; parser execution has no corresponding branch.
	eagerAttributeCopies bool
	// leftRecursionBindings accepts a binding that may run before a left call into a cycle that
	// reads its variable (checkLeftRecursionBindings), for tests of the runtime's left-recursion
	// guard, which such a binding reaches with another variable environment.
	leftRecursionBindings bool
}

// staticRule is a rule of a compiled program: what the compiler found out about it. Every backend and
// generator reads it; the parser runs a rule (runtime.go) made from it.
type staticRule struct {
	// where names the rule in the message of a failed action, if not name (staticRule.whereName).
	where string
	// splice reports that the rule makes no node of its own: a hidden rule without an action, whose
	// elements are those of the sequence that calls it.
	splice bool
	id     int
	name   string
	def    *grammar.RuleDef
	scope  *scope
	action grammar.Term
	// nodeType, if not "", is the named node type the rule's value is converted to
	// (staticProgram.ruleNodeType).
	nodeType string
	// opNodeType, if not "", is the named node type every operator node of an Operators Rule is
	// converted to (staticProgram.operatorsNodeType).
	opNodeType string
	memo       bool
	missing    bool // this rule or a callee contains #missing
	leader     bool
	// stream reports that the top level of the rule body has a #stream repetition.
	stream bool
	// positional reports that the result may depend on position values (analysis.positional).
	positional bool
	// twin is the value-free twin used for calls from places where the value is discarded (#atomic, -, !,
	// value-free bodies). It is created for rules that return a CST (no action) and are not part of a left-recursion cycle (twinOK), when such a call exists. It matches the
	// same input in the same way, but keeps a separate memo (ids from len(rules) on).
	twin   *staticRule
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
	// lean reports that the body's value is unused (an action, which reads captures only), so the
	// body runs value-free. Captures still build values.
	lean bool
}

// scope maps capture names to slots. Each rule body, repetition element, and Operators Rule line has its
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
	"Match": true, "Seq": true, "List": true, "Operator": true, "Error": true, "Missing": true, value.TypeMissing: true,
}

type compiler struct {
	control        *compileControl
	prog           *staticProgram
	proj           map[string]string // projected captures of the rule being compiled (ruleProjections)
	an             *analysis
	opts           Options
	errs           ErrorList
	rule           *staticRule
	descIDs        map[string]expID
	missingSites   int
	missingSiteIDs map[*grammar.Attribute]int
	// runSites numbers the repetitions that a Document can resume (resumable.go).
	runSites int
}

// desc returns the index of an expectation string (adding it to staticProgram.descTable).
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

// ruleFlags are rule properties determined by static analysis. They are saved in compiled
// grammars so loading can skip the analysis.
type ruleFlags struct {
	memo, leader, positional bool
	// twin reports that the rule has a value-free twin (staticRule.twin).
	twin bool
}

// compileProgram compiles the grammar to its static program. The walk of the rule bodies calls
// the builder that builder returns for the compiler, which builds the bodies in a backend's form.
// names receives the hidden rules that the lowering of inline actions generates. If flags is not
// nil, it skips static analysis and type checking and uses flags instead.
func compileProgram[M any](g *grammar.Grammar, opts Options, flags []ruleFlags, names inlineNames, control *compileControl, builder func(*compiler) exprBuilder[M]) (*staticProgram, error) {
	control.check()
	if !opts.DefaultValue {
		if err := validateGrammar(g, control); err != nil {
			return nil, err
		}
	}
	// The inline actions are checked as written, then lowered to hidden rules. A program keeps
	// the grammar as written, whatever its options, for Program.SourceGrammar.
	source := g
	if hasInlineAction(g, control) {
		if errs := undeclaredRules(g, control); errs != nil {
			return nil, errs
		}
		lowered, errs := lowerInlineActions(g, names, control)
		if errs != nil {
			return nil, errs
		}
		g = lowered
		control.check()
	}
	if opts.DefaultValue {
		lowered := g
		g = defaultValueGrammar(g, control)
		control.check()
		// A defect is reported where the grammar as written has it.
		valid := g
		if lowered != source {
			valid = sourceDefaultValueGrammar(source, lowered, g, names, control)
		}
		if err := validateGrammar(valid, control); err != nil {
			return nil, err
		}
	}
	keepSource := source != g
	var unsupported ErrorList
	for _, comment := range g.File.HeaderComments {
		control.poll()
		unsupported = append(unsupported, &Error{Pos: comment.Pos, Msg: "comments are not allowed inside statement headers"})
	}
	checkBlocks := func(term grammar.Term) {
		walkTerm(term, func(t grammar.Term) {
			control.poll()
			if block, ok := t.(*grammar.Block); ok {
				unsupported = append(unsupported, &Error{Pos: block.Diagnostic(), Msg: "action statement blocks are not implemented"})
			}
		})
	}
	for _, rd := range control.rules(g) {
		control.poll()
		checkBlocks(rd.Action)
		root := rd.Expr
		for {
			group, ok := root.(*grammar.Group)
			if !ok {
				break
			}
			root = group.Expr
		}
		if pr, _ := grammar.RuleOperators(root); pr != nil && rd.Action != nil {
			unsupported = append(unsupported, &Error{Pos: termPos(rd.Action, rd.Diagnostic()), Msg: "whole-rule actions are not supported for Operators Rules; use => ... or omit the action"})
		}
		walkExprControlled(rd.Expr, func(e grammar.Expr) {
			control.poll()
			switch e := e.(type) {
			case *grammar.Predicate:
				if block, ok := e.Term.(*grammar.Block); ok && len(block.Statements) > 0 {
					for _, statement := range block.Statements {
						control.poll()
						checkBlocks(statement)
					}
				} else {
					checkBlocks(e.Term)
				}
			case *grammar.InlineAction:
				checkBlocks(e.Action)
			case *grammar.OperatorsRule:
				for _, operand := range e.Operands {
					control.poll()
					checkBlocks(operand.Action)
				}
				for _, level := range e.Levels {
					control.poll()
					for _, operator := range level.Operators {
						control.poll()
						checkBlocks(operator.Action)
					}
				}
			}
		}, control)
	}
	starts, defaultStart, startErrors := resolveStarts(control.rules(g), control)
	control.check()
	if len(unsupported) != 0 {
		return nil, unsupported
	}
	var normalizationError error
	g, normalizationError = normalizeAttributesControlled(g, opts.eagerAttributeCopies, control)
	control.check()
	if normalizationError != nil {
		return nil, normalizationError
	}
	if !keepSource {
		source = nil // the grammar as written is the one the program runs
	} else {
		// The grammar as written is kept beside the one the program runs.
		if source, normalizationError = normalizeAttributesControlled(source, opts.eagerAttributeCopies, control); normalizationError != nil {
			return nil, normalizationError
		}
	}
	prog := &staticProgram{Grammar: g, source: source, byName: map[string]*staticRule{}, types: map[string]grammar.TypeDecl{},
		descTable: append([]string(nil), fixedDescs...)}
	c := &compiler{control: control, prog: prog, opts: opts}
	if control != nil {
		prog.shapeControl = control
		defer func() {
			prog.shapeControl = nil
			c.control = nil
			if c.an != nil {
				c.an.control = nil
			}
		}()
	}
	prog.starts, prog.defaultStart = starts, defaultStart
	prog.workRecognize, prog.workRecognizer = opts.recognize, opts.recognizer
	prog.noProjections = opts.noProjections
	prog.sourceInfo = opts.SourceInfo
	prog.defaultValue = opts.DefaultValue

	for _, td := range control.types(g) {
		control.poll()
		switch {
		case builtinTypes[td.DeclName()]:
			c.errorf(td.Diagnostic(), "type %s is reserved", td.DeclName())
		case !startsUpper(td.DeclName()):
			c.errorf(td.Diagnostic(), "type %s must start with an uppercase letter", td.DeclName())
		case prog.types[td.DeclName()] != nil:
			c.errorf(td.Diagnostic(), "type %s is already defined", td.DeclName())
		default:
			prog.types[td.DeclName()] = td
		}
	}
	rules := control.rules(g)
	for i, rd := range rules {
		control.poll()
		if prog.byName[rd.Name] != nil {
			c.errorf(rd.Diagnostic(), "rule %s is already defined", rd.Name)
			continue
		}
		r := &staticRule{id: i, name: rd.Name, def: rd, scope: newScope(), action: rd.Action, splice: spliceRule(rd)}
		if r.splice && rd.Type != nil {
			c.errorf(rd.Diagnostic(), "rule %s makes no node of its own, so it cannot have a result type", rd.Name)
		}
		if w := names.where(rd.Name); w != rd.Name {
			r.where = w
		}
		prog.rules = append(prog.rules, r)
		prog.byName[rd.Name] = r
	}

	c.checkSpliceCycles(rules)
	c.declareVars(g)
	prog.markMissingRules(control)
	if flags == nil {
		c.an = analyzeControlled(rules, control)
	} else if len(flags) != len(prog.rules) {
		return nil, fmt.Errorf("compiled grammar has %d rule flags for %d rules", len(flags), len(prog.rules))
	}
	transient := transientRules(rules, control)
	vars := ruleVariablesControlled(rules, control)
	for i, r := range prog.rules {
		control.poll()
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
		if flags == nil {
			_, isOperatorsRule := r.def.Expr.(*grammar.OperatorsRule)
			twin = r.action == nil && !isOperatorsRule && !c.an.cyclic[r.name] && !hasStream(r.def.Expr, control)
		}
		r.twinOK = twin
	}
	if flags == nil && !opts.leftRecursionBindings {
		c.checkLeftRecursionBindings(rules, vars)
	}
	if opts.recognize {
		// Only rules whose values are referenced by predicates, and the rules needed to build those
		// values, are evaluated as usual; so are the rules with a #stream repetition, whose elements
		// a stream hands to its caller, unless the program is the recognizer, which only recognizes.
		needed := valueNeededRules(rules, control, !opts.recognizer)
		for _, r := range prog.rules {
			control.poll()
			if !needed[r.name] {
				r.novalue, r.action, r.twinOK = true, nil, false
				r.predCaps = predicateCaptures(r.def.Expr, control)
			}
		}
	}
	w := &walker[M]{compiler: c, b: builder(c)}
	for _, r := range prog.rules {
		control.poll()
		w.compileRule(r)
	}
	// Value-free twins can be assembled the same way if the original rules have no errors. New twins
	// may be added during assembly.
	for i := 0; i < len(prog.twins) && len(c.errs) == 0; i++ {
		control.poll()
		w.compileRule(prog.twins[i])
	}
	control.check()
	if len(c.errs) > 0 {
		return nil, c.errs
	}
	prog.nseen = numberSeen(prog.rules, prog.twins)
	prog.typeKinds = map[string]*value.NodeKind{}
	prog.nodeKinds = map[string]*value.NodeTypeKinds{}
	for name, spec := range prog.types {
		control.poll()
		if n, ok := prog.nodeTypeOf(name); ok {
			if prog.nodeKinds[n.name] == nil {
				prog.nodeKinds[n.name] = value.NewNodeTypeKinds(n.name)
			}
			prog.nodeKinds[name] = prog.nodeKinds[n.name]
			continue
		}
		canonical := name
		if _, alias := spec.(*grammar.TypeAlias); alias {
			if resolved, concrete := prog.concreteType(&grammar.TypeRef{Name: name}); concrete != nil {
				canonical = resolved
			}
		}
		if canonical != name {
			prog.typeKinds[name] = value.KindOf(canonical, "")
		} else {
			prog.typeKinds[name] = value.KindOf(name, "")
		}
	}
	for _, rs := range [][]*staticRule{prog.rules, prog.twins} {
		control.poll()
		for _, r := range rs {
			control.poll()
			r.plain = !r.leader && (!r.memo || r.transient) && len(r.scope.names) == 0
			if n, ok := prog.ruleNodeType(r.def); ok && !r.novalue {
				r.nodeType = n.name
			}
			if n, ok := prog.operatorsNodeType(r.def); ok && !r.novalue {
				r.opNodeType = n.name
			}
		}
	}
	if flags == nil {
		checkTypesControlled(prog, &c.errs, control)
		control.check()
		if len(c.errs) > 0 {
			return nil, c.errs
		}
	}
	// The start rules are reported once the grammar is otherwise valid.
	if len(startErrors) > 0 {
		return nil, startErrors
	}

	control.check()
	return prog, nil
}

// validateGrammar returns the validation error of g as a compile error.
func validateGrammar(g *grammar.Grammar, control *compileControl) error {
	var validationError error
	if control == nil {
		validationError = grammar.Validate(g)
	} else {
		validationError = grammar.ValidateContext(control.ctx, g)
	}
	control.check()
	if validationError != nil {
		e := validationError.(*grammar.ValidationError)
		return ErrorList{&Error{Pos: e.Pos, Msg: e.Path + ": " + e.Msg}}
	}
	return nil
}

// bodyExpr returns the expression the rule's body matches.
func (r *staticRule) bodyExpr() grammar.Expr {
	return r.def.Expr
}

// leanBody reports whether the value of the rule body is unused. It is unused when there is an
// action, which reads captures only, provided the rule has no #stream. Every backend and the
// code generator use this test to compile the body value-free.
func leanBody(r *staticRule) bool {
	if r.stream {
		return false
	}
	return r.action != nil
}

// numberSeen numbers the rules whose memoization firstCall may defer (staticRule.seen) and returns
// their count.
func numberSeen(lists ...[]*staticRule) int {
	n := 0
	for _, rules := range lists {
		for _, r := range rules {
			r.seen = -1
			if r.memo && !r.transient && !r.leader {
				r.seen = n
				n++
			}
		}
	}
	return n
}

// twinOf returns the value-free twin of rule r (creating it if needed; it is assembled later).
func (c *compiler) twinOf(r *staticRule) *staticRule {
	if r.twin == nil {
		prog := c.prog
		r.twin = &staticRule{id: len(prog.rules) + len(prog.twins), name: r.name, def: r.def, scope: newScope(),
			memo: r.memo, missing: r.missing, transient: r.transient, vars: r.vars, positional: r.positional, novalue: true}
		prog.twins = append(prog.twins, r.twin)
	}
	return r.twin
}

func transientRules(rules []*grammar.RuleDef, control *compileControl) map[string]bool {
	refs := map[string]int{}
	for _, rd := range rules {
		control.poll()
		for _, name := range allCallsControlled(rd.Expr, nil, control) {
			refs[name]++
		}
	}
	out := map[string]bool{}
	for _, rd := range rules {
		control.poll()
		out[rd.Name] = refs[rd.Name] < 2 || len(allCallsControlled(rd.Expr, nil, control)) == 0
	}
	return out
}

// predicateCaptures returns the names of the captures referenced by predicates in the expression.
func predicateCaptures(e grammar.Expr, control *compileControl) map[string]bool {
	control.poll()
	names := map[string]bool{}
	walkExprControlled(e, func(x grammar.Expr) {
		if pr, ok := x.(*grammar.Predicate); ok {
			walkTerm(pr.Term, func(t grammar.Term) {
				control.poll()
				if cr, ok := t.(*grammar.CaptureRef); ok {
					names[cr.Name] = true
				}
			})
		}
	}, control)
	return names
}

// valueNeededRules returns the rules that must build values even in a program that builds no
// tree: the rules called within captures referenced by predicates, and every rule they call. With
// streams, also the rules with a #stream repetition (see Program.ParseStream) and every rule they call.
func valueNeededRules(rules []*grammar.RuleDef, control *compileControl, streams bool) map[string]bool {
	control.poll()
	byName := map[string]*grammar.RuleDef{}
	for _, rd := range rules {
		control.poll()
		byName[rd.Name] = rd
	}
	needed := map[string]bool{}
	var mark func(name string)
	mark = func(name string) {
		if needed[name] || byName[name] == nil {
			return
		}
		needed[name] = true
		for _, callee := range allCallsControlled(byName[name].Expr, nil, control) {
			control.poll()
			mark(callee)
		}
	}
	for _, rd := range rules {
		control.poll()
		if streams && hasStream(rd.Expr, control) {
			mark(rd.Name)
		}
		pc := predicateCaptures(rd.Expr, control)
		walkExprControlled(rd.Expr, func(x grammar.Expr) {
			if cp, ok := x.(*grammar.Capture); ok && pc[cp.Name] {
				for _, callee := range allCallsControlled(cp.Expr, nil, control) {
					control.poll()
					mark(callee)
				}
			}
		}, control)
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
func hasStream(e grammar.Expr, control *compileControl) bool {
	control.poll()
	found := false
	walkExprControlled(e, func(x grammar.Expr) {
		if isStream(x) {
			found = true
		}
	}, control)
	return found
}

// noCaptures reports captures inside value-free expressions (#atomic, -, !, skip) as errors.
func (c *compiler) noCaptures(e grammar.Expr) {
	control := c.control
	c.control.poll()
	walkExprControlled(e, func(x grammar.Expr) {
		if cp, ok := x.(*grammar.Capture); ok {
			c.errorf(cp.Diagnostic(), "capture %s inside #atomic, -, or ! has no effect", cp.Name)
		}
	}, control)
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

// hasCaptures reports whether the expression contains captures (excluding nested repetitions),
// also inside positive lookaheads.
// elementScoped reports whether the repetition element e gets a capture scope of its own per
// iteration: when it contains captures (hasCaptures), or its predicates read captures, which can
// then only be its own (made inside &) or, an error, those of the enclosing scope. Every backend
// and generator must decide the same way, since the scope decides where captures are written.
func elementScopedControlled(e grammar.Expr, control *compileControl) bool {
	control.poll()
	return hasCapturesControlled(e, control) || predicatesReadCaptures(e, control)
}

// predicatesReadCaptures reports whether a predicate in e refers to a capture.
func predicatesReadCaptures(e grammar.Expr, control *compileControl) bool {
	control.poll()
	found := false
	walkExprControlled(e, func(x grammar.Expr) {
		if p, ok := x.(*grammar.Predicate); ok {
			walkTerm(p.Term, func(t grammar.Term) {
				control.poll()
				if _, ok := t.(*grammar.CaptureRef); ok {
					found = true
				}
			})
		}
	}, control)
	return found
}

func hasCapturesControlled(e grammar.Expr, control *compileControl) bool {
	control.poll()
	switch e := e.(type) {
	case *grammar.Capture:
		return true
	case *grammar.Seq:
		for _, it := range e.Items {
			control.poll()
			if hasCapturesControlled(it, control) {
				return true
			}
		}
	case *grammar.Choice:
		for _, alt := range e.Alts {
			control.poll()
			if hasCapturesControlled(alt, control) {
				return true
			}
		}
	case *grammar.Optional:
		return hasCapturesControlled(e.Expr, control)
	case *grammar.Attributed:
		return hasCapturesControlled(e.Expr, control)
	case *grammar.Group:
		return hasCapturesControlled(e.Expr, control)
	case *grammar.And:
		// The captures of a positive lookahead are kept beside the element's value.
		return hasCapturesControlled(e.Expr, control)
	}
	return false
}

func quote(s string) string { return strconv.Quote(s) }

// literalDesc is the expectation text of a literal, as written: a case-insensitive literal keeps its
// suffix i.
func literalDesc(e *grammar.Literal) string {
	if e.IgnoreCase {
		return quote(e.Value) + "i"
	}
	return quote(e.Value)
}

// foldASCIIRune maps A-Z to a-z and leaves every other rune as it is: the folding of #ignorecase
// and of the suffix i, which is ASCII only.
func foldASCIIRune(r rune) rune {
	if 'A' <= r && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}

// foldedRunes returns rs with foldASCIIRune applied to each rune.
func foldedRunes(rs []rune) []rune {
	out := make([]rune, len(rs))
	for i, r := range rs {
		out[i] = foldASCIIRune(r)
	}
	return out
}

// firstTerminal returns the terminal (a *grammar.Literal or *grammar.CharClass) that the expression
// e must begin with, if any, and the number of rule calls on the way: e is the terminal, possibly
// behind sequences, captures, @, - and calls of rules that are neither left-recursive nor OperatorsRule.
// When the next character cannot start the terminal, e fails at once, recording only the
// terminal's expectation and examining only that character (a call memoizes the same failure, and
// memoization only affects speed), so a choice can skip e: first-character dispatch, done by the
// closure backend, the VMs (GUARD) and generated parsers, unless the calls on the way would exceed
// the nesting limit.
func firstTerminal(prog *staticProgram, e grammar.Expr, depth int, seen map[string]bool) (grammar.Expr, int) {
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
		if _, operatorsRule := r.def.Expr.(*grammar.OperatorsRule); operatorsRule {
			break
		}
		seen[e.Name] = true
		return firstTerminal(prog, r.def.Expr, depth+1, seen)
	}
	return nil, 0
}

func charClassString(e *grammar.CharClass) string {
	var b strings.Builder
	b.WriteString("[")
	if e.Negated {
		b.WriteString("^")
	}
	esc := func(r rune) string {
		switch r {
		case '-', '[', ']', '^', '\\':
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
	b.WriteString("]")
	return b.String()
}

// levelMin returns the minimum binding level of a call with a binding level.
func (c *compiler) levelMin(e *grammar.Ref, r *staticRule) int {
	pr, ok := r.def.Expr.(*grammar.OperatorsRule)
	if !ok {
		c.errorf(e.Diagnostic(), "rule %s has no Operators Rule levels", e.Name)
		return 0
	}
	for i, l := range pr.Levels {
		if l.Name == e.Level {
			return i // parse binding levels i+1 and above
		}
	}
	c.errorf(e.Diagnostic(), "rule %s has no level %s", e.Name, e.Level)
	return 0
}

// isStream reports whether the expression is an expression with the #stream pragma.
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
	control := c.control
	top := streamTops(body)
	n := 0
	walkExprControlled(body, func(e grammar.Expr) {
		if !isStream(e) {
			return
		}
		n++
		a := e.(*grammar.Attributed)
		for _, attr := range a.Attrs {
			if attr.Name == "recover" {
				c.errorf(attr.Diagnostic(), "#recover cannot wrap a #stream repetition; recover inside each element instead")
			}
		}
		if !top[e] {
			c.errorf(a.Attrs[0].Diagnostic(), "#stream is only allowed at the top level of a rule body")
		} else if n > 1 {
			c.errorf(a.Attrs[0].Diagnostic(), "a rule body can have only one #stream")
		}
	}, control)
	return n > 0
}

// streamTops returns the expressions of a rule body where #stream is allowed: the body, the items
// of its top-level sequence (also those of a sequence grouped in an item) and the expression a
// capture of such an item holds, each with the groups and the attributes other than #recover and
// #missing around it.
func streamTops(body grammar.Expr) map[grammar.Expr]bool {
	top := map[grammar.Expr]bool{}
	var mark func(e grammar.Expr)
	mark = func(e grammar.Expr) {
		for e != nil {
			top[e] = true
			switch x := e.(type) {
			case *grammar.Group:
				e = x.Expr
			case *grammar.Attributed:
				if recovers(x) {
					return
				}
				e = x.Expr
			case *grammar.Capture:
				mark(x.Expr)
				return
			default:
				return
			}
		}
	}
	var items func(e grammar.Expr)
	items = func(e grammar.Expr) {
		if seq, ok := seeThrough(e).(*grammar.Seq); ok {
			for _, it := range seq.Items {
				mark(it)
				items(it)
			}
		}
	}
	mark(body)
	items(body)
	return top
}

// seeThrough returns e without the groups and the attributes other than #recover and #missing
// around it.
func seeThrough(e grammar.Expr) grammar.Expr {
	for {
		switch x := e.(type) {
		case *grammar.Group:
			e = x.Expr
		case *grammar.Attributed:
			if recovers(x) {
				return e
			}
			e = x.Expr
		default:
			return e
		}
	}
}

func elementScoped(e grammar.Expr) bool { return elementScopedControlled(e, nil) }

func hasCaptures(e grammar.Expr) bool { return hasCapturesControlled(e, nil) }

// whereName returns the text that names the rule in the message of a failed action.
func (r *staticRule) whereName() string {
	if r.where != "" {
		return r.where
	}
	return r.name
}

// spliceRule reports whether the rule rd makes no node of its own: it is hidden and has no action
// (not even => ...), and its body is not an Operators Rule. A call of it contributes the elements
// of its body, as a group does.
func spliceRule(rd *grammar.RuleDef) bool {
	if !hiddenRule(rd.Name) || rd.Action != nil || rd.DefaultAction {
		return false
	}
	_, operators := rd.Expr.(*grammar.OperatorsRule)
	return !operators
}

// checkSpliceCycles reports a splice rule whose elements include its own: it calls itself in the
// sequence of its elements, so that its elements would be infinite in number. A call in a choice,
// an option, a repetition or a capture is one element of its own, and may recur.
func (c *compiler) checkSpliceCycles(rules []*grammar.RuleDef) {
	flat := map[string][]string{}
	for _, rd := range rules {
		if r := c.prog.byName[rd.Name]; r != nil && r.splice && r.def == rd {
			flat[rd.Name] = c.flatRefs(rd.Expr, nil)
		}
	}
	state := map[string]int{} // 1 visiting, 2 done
	var visit func(name string) bool
	visit = func(name string) bool {
		switch state[name] {
		case 1:
			return true
		case 2:
			return false
		}
		state[name] = 1
		for _, to := range flat[name] {
			if _, ok := flat[to]; ok && visit(to) {
				return true
			}
		}
		state[name] = 2
		return false
	}
	for _, rd := range rules {
		c.control.poll()
		if _, ok := flat[rd.Name]; ok && state[rd.Name] == 0 && visit(rd.Name) {
			c.errorf(rd.Diagnostic(), "rule %s contains itself in its sequence of elements, so it has no fixed number of them", rd.Name)
			return
		}
	}
}

// flatRefs appends to out the rules e calls where their elements are elements of e.
func (c *compiler) flatRefs(e grammar.Expr, out []string) []string {
	switch e := e.(type) {
	case *grammar.Group:
		return c.flatRefs(e.Expr, out)
	case *grammar.Seq:
		for _, it := range e.Items {
			out = c.flatRefs(it, out)
		}
	case *grammar.Attributed:
		if !recovers(e) {
			out = c.flatRefs(e.Expr, out)
		}
	case *grammar.Ref:
		out = append(out, e.Name)
	}
	return out
}

// typeKind returns the kind of nodes of the grammar's type typ without a rule.
func (prog *staticProgram) typeKind(typ string) *value.NodeKind {
	if k := prog.typeKinds[typ]; k != nil {
		return k
	}
	return value.KindOf(typ, "")
}
