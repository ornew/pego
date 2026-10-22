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
	hasMissing bool // immutable syntax metadata; bytecode/generation are unsupported
	Grammar    *grammar.Grammar
	// source is the grammar as written when Grammar is its lowering (inline actions replaced by
	// hidden rules, inline_actions.go); nil when Grammar is the grammar itself.
	source *grammar.Grammar
	// unsupportedSeen holds the type syntax reported as not supported yet while compiling, so that
	// an alias resolved from several places, by several checkers, is reported once.
	unsupportedSeen map[grammar.TypeExpr]bool
	rules           []*rule
	descTable       []string  // expectation table (starting with fixedDescs)
	twins           []*rule   // value-free twins (rule.twin); ids start at len(rules)
	nseen           int       // number of rules with rule.seen set
	typed           *typeInfo // types found by the type checker (nil if it did not run)
	// projected holds the map calls of actions that read a projected repetition (project.go).
	projected     map[*grammar.Call]bool
	noProjections bool
	// sourceInfo reports that nodes record source information (Options.SourceInfo).
	sourceInfo bool
	// defaultValue reports that value actions and declared rule types were dropped
	// (Options.DefaultValue).
	defaultValue bool
	byName       map[string]*rule
	// starts holds the start rules in declaration order and defaultStart the default one
	// (resolveStarts).
	starts       []string
	defaultStart string
	types        map[string]grammar.TypeDecl
	// vars holds the declared variables (nil if there are none).
	vars *varTable
	// fieldTypes caches the closure backend's field checks by constructor (constructorChecks)
	// and the types of casts (castType).
	fieldTypes sync.Map

	workOnce      sync.Once
	workProgram   *Program
	workErr       error
	workRecognize bool

	moduleOnce         sync.Once
	module             *Module
	recOnce            sync.Once
	rec                *Program // program that builds no tree (recognizer)
	recErr             error
	recognitionModule  *Module
	recognitionEntries []recognitionEntry
	recognitionStarts  []recognitionEntry
	// Freshly compiled recognizers are owned by this program and do not change
	// after recOnce. Loaded recognition modules remain mutable and are not cached.
	recognitionBytesOnce sync.Once
	recognitionBytes     []byte
	vmOnce               sync.Once
	vm                   *vmProgram
	ivmOnce              sync.Once
	ivm                  *vmProgram
	pkg                  string // package name when loaded from a file without the AST
	// typeKinds holds the node kinds of the grammar's types (typeKind), made with the program.
	typeKinds map[string]*nodeKind
	// nodeKinds holds the kinds of the grammar's named node types, also under the names of the
	// aliases of them.
	nodeKinds map[string]*nodeTypeKinds
	// nodeTypeCache holds the results of nodeTypeOf by type name: a nodeTy, or false.
	nodeTypeCache sync.Map
	// scratch holds the buffers of finished parses for later ones (*scratch).
	scratch sync.Pool
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
	// noProjections compiles repetitions as written (project.go), for tests: with projections in
	// every backend, only a program without them can check what they produce.
	noProjections bool
	// eagerAttributeCopies disables copy-on-change normalization for measurements.
	// It affects compilation only; parser execution has no corresponding branch.
	eagerAttributeCopies bool
}

// matcher tries to match at the current position and returns the value and whether it
// succeeded. On failure the position is unspecified; the caller restores it.
type matcher func(p *parser) (*Node, bool)

type rule struct {
	// where names the rule in the message of a failed action, if not name (rule.whereName).
	where string
	// splice reports that the rule makes no node of its own: a hidden rule without an action, whose
	// elements are those of the sequence that calls it.
	splice bool
	// kinds are the node kinds the rule makes (newRuleKinds).
	kinds     ruleKinds
	id        int
	name      string
	def       *grammar.RuleDef
	scope     *scope
	body      func(p *parser, min int) (*Node, bool)
	action    grammar.Term
	bodyIsSeq bool
	// nodeType, if not "", is the named node type the rule's value is converted to
	// (Program.ruleNodeType), whose kinds are nodeKinds.
	nodeType  string
	nodeKinds *nodeTypeKinds
	// opNodeType, if not "", is the named node type every operator node of an Operators Rule is
	// converted to (Program.operatorsNodeType), whose kinds are opKinds.
	opNodeType    string
	opKinds       *nodeTypeKinds
	memo          bool
	missing       bool // this rule or a callee contains #missing
	leader        bool
	operatorsRule *operatorsRule
	// stream reports that the top level of the rule body has a #stream repetition.
	stream bool
	// positional reports that the result may depend on position values (analysis.positional).
	positional bool
	// act evaluates the action (nil if there is no action).
	act evaluator
	// value makes the rule's value from the value of its body, for a rule without an action.
	value func(p *parser, v *Node, start int) *Node
	// entry is the start of the body in bytecode (only for rules of the bytecode backend).
	entry int
	// twin is the value-free twin used for calls from places where the value is discarded (#atomic, -, !,
	// value-free bodies). It is created for rules that return a CST (no action) and are not part of a left-recursion cycle (twinOK), when such a call exists. It matches the
	// same input in the same way, but keeps a separate memo (ids from len(rules) on).
	twin   *rule
	twinOK bool
	// vars are the variables that the rule or the rules it calls read, sorted. The rule's result is
	// memoized per combination of their values at the call (memoKey.env).
	vars []string
	// initVals are the initial values of vars, in the same order (nil when vars is empty).
	initVals []any
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
	"Match": true, "Seq": true, "List": true, "Operator": true, "Error": true, "Missing": true, TypeMissing: true,
}

type compiler struct {
	control        *compileControl
	prog           *Program
	proj           map[string]string // projected captures of the rule being compiled (ruleProjections)
	an             *analysis
	opts           Options
	errs           ErrorList
	rule           *rule
	descIDs        map[string]expID
	missingSites   int
	missingSiteIDs map[*grammar.Attribute]int
	// runSites numbers the repetitions that a Document can resume (resume.go).
	runSites      int
	plainDispatch bool
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
	return buildControlled(g, opts, flags, nil)
}

func buildControlled(g *grammar.Grammar, opts Options, flags []ruleFlags, control *compileControl) (*Program, error) {
	names := inlineNames{}
	prog, err := buildNamed(g, opts, flags, names, control)
	if err != nil {
		return nil, names.rewrite(err)
	}
	return prog, nil
}

// buildNamed is buildControlled; names receives the hidden rules that the lowering of inline actions
// generates.
func buildNamed(g *grammar.Grammar, opts Options, flags []ruleFlags, names inlineNames, control *compileControl) (*Program, error) {
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
		if _, ok := root.(*grammar.OperatorsRule); ok && rd.Action != nil {
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
	prog := &Program{Grammar: g, source: source, byName: map[string]*rule{}, types: map[string]grammar.TypeDecl{},
		descTable: append([]string(nil), fixedDescs...)}
	c := &compiler{control: control, prog: prog, opts: opts, plainDispatch: !opts.controlledDispatch}
	if control != nil {
		defer func() {
			c.control = nil
			if c.an != nil {
				c.an.control = nil
			}
		}()
	}
	prog.starts, prog.defaultStart = starts, defaultStart
	prog.workRecognize = opts.recognize
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
		r := &rule{id: i, name: rd.Name, def: rd, scope: newScope(), action: rd.Action, splice: spliceRule(rd)}
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
	transient := transientRulesControlled(rules, control)
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
		r.initVals = c.prog.vars.valuesOf(r.vars)
		if flags == nil {
			_, isOperatorsRule := r.def.Expr.(*grammar.OperatorsRule)
			twin = r.action == nil && !isOperatorsRule && !c.an.cyclic[r.name] && !hasStreamControlled(r.def.Expr, control)
		}
		r.twinOK = twin
	}
	if opts.recognize {
		// Only rules whose values are referenced by predicates, and the rules needed to build those
		// values, are evaluated as usual.
		needed := valueNeededRulesControlled(rules, control)
		for _, r := range prog.rules {
			control.poll()
			if !needed[r.name] {
				r.novalue, r.action, r.twinOK = true, nil, false
				r.predCaps = predicateCapturesControlled(r.def.Expr, control)
			}
		}
	}
	for _, r := range prog.rules {
		control.poll()
		c.compileRule(r)
	}
	// Value-free twins can be assembled the same way if the original rules have no errors. New twins
	// may be added during assembly.
	for i := 0; i < len(prog.twins) && len(c.errs) == 0; i++ {
		control.poll()
		c.compileRule(prog.twins[i])
	}
	control.check()
	if len(c.errs) > 0 {
		return nil, c.errs
	}
	prog.nseen = numberSeen(prog.rules, prog.twins)
	prog.typeKinds = map[string]*nodeKind{}
	prog.nodeKinds = map[string]*nodeTypeKinds{}
	for name, spec := range prog.types {
		control.poll()
		if n, ok := prog.nodeTypeOf(name); ok {
			if prog.nodeKinds[n.name] == nil {
				prog.nodeKinds[n.name] = newNodeTypeKinds(n.name)
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
			prog.typeKinds[name] = kindOf(canonical, "")
		} else {
			prog.typeKinds[name] = kindOf(name, "")
		}
	}
	for _, rs := range [][]*rule{prog.rules, prog.twins} {
		control.poll()
		for _, r := range rs {
			control.poll()
			r.plain = !r.leader && (!r.memo || r.transient) && len(r.scope.names) == 0
			if n, ok := prog.ruleNodeType(r.def); ok && !r.novalue {
				r.nodeType, r.nodeKinds = n.name, newNodeTypeKinds(n.name)
			}
			if n, ok := prog.operatorsNodeType(r.def); ok && !r.novalue {
				r.opNodeType, r.opKinds = n.name, newNodeTypeKinds(n.name)
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

func (c *compiler) compileRule(r *rule) {
	control := c.control
	c.control.poll()
	c.rule = r
	r.kinds = newRuleKinds(r.name)
	if p, ok := r.def.Expr.(*grammar.OperatorsRule); ok {
		// The lines build the rule's value.
		r.value = func(_ *parser, v *Node, _ int) *Node { return v }
		c.compileOperatorsRule(r, p)
		return
	}
	body := r.bodyExpr()
	_, r.bodyIsSeq = body.(*grammar.Seq)
	r.stream = c.checkStream(body)
	r.lean = r.novalue || leanBody(r)
	c.proj = ruleProjectionsControlled(c.prog, r, control)
	m := c.expr(body, r.scope, !r.lean)
	if r.splice {
		r.value = func(_ *parser, v *Node, _ int) *Node { return v }
	} else {
		r.value = scopeValueOf(c.prog, body)
	}
	if c.proj != nil {
		if c.prog.projected == nil {
			c.prog.projected = map[*grammar.Call]bool{}
		}
		markProjectedMapsControlled(r.action, c.proj, c.prog.projected, control)
	}
	c.proj = nil
	r.body = func(p *parser, _ int) (*Node, bool) { return m(p) }
	if r.action != nil {
		c.checkTerm(r.action, r.scope, nil)
		r.act = termEvaluator(r.action)
	}
}

// bodyExpr returns the expression the rule's body matches.
func (r *rule) bodyExpr() grammar.Expr {
	return r.def.Expr
}

// leanBody reports whether the value of the rule body is unused. It is unused when there is an
// action, which reads captures only, provided the rule has no #stream. Every backend and the
// code generator use this test to compile the body value-free.
func leanBody(r *rule) bool {
	if r.stream {
		return false
	}
	return r.action != nil
}

// twinOf returns the value-free twin of rule r (creating it if needed; it is assembled later).
func (c *compiler) twinOf(r *rule) *rule {
	if r.twin == nil {
		prog := c.prog
		r.twin = &rule{id: len(prog.rules) + len(prog.twins), name: r.name, def: r.def, scope: newScope(),
			memo: r.memo, missing: r.missing, transient: r.transient, vars: r.vars, initVals: r.initVals, positional: r.positional, novalue: true}
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
	return transientRulesControlled(rules, nil)
}

func transientRulesControlled(rules []*grammar.RuleDef, control *compileControl) map[string]bool {
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
func predicateCapturesControlled(e grammar.Expr, control *compileControl) map[string]bool {
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
// tree: the rules called within captures referenced by predicates, and every rule they call.
func valueNeededRulesControlled(rules []*grammar.RuleDef, control *compileControl) map[string]bool {
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
		pc := predicateCapturesControlled(rd.Expr, control)
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
func hasStreamControlled(e grammar.Expr, control *compileControl) bool {
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
	return hasCapturesControlled(e, control) || predicatesReadCapturesControlled(e, control)
}

// predicatesReadCaptures reports whether a predicate in e refers to a capture.
func predicatesReadCapturesControlled(e grammar.Expr, control *compileControl) bool {
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

// expr compiles the expression. If build is false, it only matches without building values.
// expr adds the closure expression-dispatch charge around the compiled matcher.
func (c *compiler) expr(e grammar.Expr, s *scope, build bool) matcher {
	c.control.poll()
	m := c.exprMatcher(e, s, build)
	if !workControlsEnabled || c.plainDispatch {
		return m
	}
	return func(p *parser) (*Node, bool) { p.stepWork(); return m(p) }
}

func (c *compiler) exprMatcher(e grammar.Expr, s *scope, build bool) matcher {
	control := c.control
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
			return p.newNode(Node{kind: kindMatch, start: int32(start), end: int32(p.pos), text: p.literalText(start, value), terminal: true, fresh: true}), true
		}

	case *grammar.CharClass:
		desc := c.desc(charClassStringControlled(e, c.control))
		return c.single(build, desc, classAcceptControlled(e, c.control))

	case *grammar.Any:
		return c.single(build, idAny, func(*parser, rune) bool { return true })

	case *grammar.Ref:
		r := c.prog.byName[e.Name]
		if r == nil {
			c.errorf(e.Diagnostic(), "undefined rule %s", e.Name)
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
		// The value is the tuple node of the elements, flattened through nested sequences.
		ms := make([]matcher, len(e.Items))
		flat := make([]bool, len(e.Items))
		valued := make([]bool, len(e.Items))
		for i, it := range e.Items {
			ms[i] = c.expr(it, s, build)
			flat[i] = isElems(c.prog, it)
			valued[i] = !flat[i] && valuedCount(c.prog, it) == 1
		}
		n := valuedCount(c.prog, e)
		return func(p *parser) (*Node, bool) {
			start := p.pos
			var kids []*Node
			if build {
				kids = p.nodes(n)[:0]
			}
			for i, m := range ms {
				v, ok := m(p)
				if !ok {
					return nil, false
				}
				if !build {
					continue
				}
				switch {
				case flat[i]:
					kids = append(kids, v.children...)
				case valued[i]:
					kids = append(kids, v)
				}
			}
			if !build {
				return nil, true
			}
			return p.newNode(Node{kind: kindSeq, start: int32(start), end: int32(p.pos), children: kids, fresh: true}), true
		}

	case *grammar.Choice:
		ms := make([]matcher, len(e.Alts))
		guards := make([]firstGuard, len(e.Alts))
		guarded := false
		for i, alt := range e.Alts {
			ms[i] = c.expr(alt, s, build)
			if build {
				ms[i] = singleMatcher(c.prog, alt, ms[i])
			}
			guards[i] = c.first(alt)
			guarded = guarded || guards[i].accept != nil
		}
		if guarded {
			return func(p *parser) (*Node, bool) {
				m0 := p.mark()
				var ch rune
				more, peeked := false, false
				for i, m := range ms {
					if g := &guards[i]; g.accept != nil && p.missing == nil {
						if !peeked {
							ch, _, more = p.peek()
							peeked = true
						}
						if !(more && g.accept(p, ch)) && p.depth+g.depth <= p.maxDepth {
							p.stepWork()            // a skipped matcher still consumes dispatch work
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
		if build {
			m = singleMatcher(c.prog, e.Expr, m)
		}
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
		m := c.expr(e.Expr, s, build && hasCapturesControlled(e.Expr, control))
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
			return p.newNode(Node{kind: kindMatch, start: int32(start), end: int32(p.pos), text: p.text(start, p.pos), terminal: true, fresh: true}), true
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
		slot := s.slot(e.Name)
		var m matcher
		if field := c.proj[e.Name]; field != "" {
			m = c.projectRepeat(e.Expr.(*grammar.Repeat), field)
		} else {
			m = captureMatcher(c.prog, e.Expr, c.expr(e.Expr, s, true))
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
		// _ matches the empty input and has no value.
		return func(p *parser) (*Node, bool) { return nil, true }

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

	case *grammar.OperatorsRule:
		c.errorf(e.Diagnostic(), "operatorsRule is only allowed as the whole body of a rule")
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
// behind sequences, captures, @, - and calls of rules that are neither left-recursive nor OperatorsRule.
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
		if _, operatorsRule := r.def.Expr.(*grammar.OperatorsRule); operatorsRule {
			break
		}
		seen[e.Name] = true
		return firstTerminal(prog, r.def.Expr, depth+1, seen)
	}
	return nil, 0
}

// firstGuard tells when an alternative of a choice can be skipped (see firstTerminal).
type firstGuard struct {
	accept func(*parser, rune) bool // nil if it cannot be
	desc   expID
	depth  int
}

func (c *compiler) first(e grammar.Expr) firstGuard {
	switch t, depth := firstTerminalControlled(c.prog, e, 0, map[string]bool{}, c.control); t := t.(type) {
	case *grammar.Literal:
		r0, _ := utf8.DecodeRuneInString(t.Value)
		return firstGuard{func(_ *parser, r rune) bool { return r == r0 }, c.desc(quote(t.Value)), depth}
	case *grammar.CharClass:
		return firstGuard{classAcceptControlled(t, c.control), c.desc(charClassStringControlled(t, c.control)), depth}
	}
	return firstGuard{}
}

func (c *compiler) single(build bool, desc expID, accept func(*parser, rune) bool) matcher {
	return func(p *parser) (*Node, bool) {
		ch, size, ok := p.peek()
		if !ok || !accept(p, ch) {
			p.expect(p.pos, desc)
			return nil, false
		}
		start := p.pos
		p.pos += size
		if !build {
			return nil, true
		}
		return p.newNode(Node{kind: kindMatch, start: int32(start), end: int32(p.pos), text: p.text(start, p.pos), terminal: true, fresh: true}), true
	}
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

// classAccept returns the membership test of a character class. Classes of one or two ranges,
// which are most of them, compare against constants; others test ASCII characters with a bitmap.
func classAccept(e *grammar.CharClass) func(*parser, rune) bool {
	neg := e.Negated
	switch len(e.Ranges) {
	case 1:
		lo, hi := e.Ranges[0].Lo, e.Ranges[0].Hi
		return func(_ *parser, r rune) bool { return (lo <= r && r <= hi) != neg }
	case 2:
		lo1, hi1, lo2, hi2 := e.Ranges[0].Lo, e.Ranges[0].Hi, e.Ranges[1].Lo, e.Ranges[1].Hi
		return func(_ *parser, r rune) bool { return (lo1 <= r && r <= hi1 || lo2 <= r && r <= hi2) != neg }
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
	return func(p *parser, r rune) bool {
		if uint32(r) < 128 {
			return ascii[r>>6]&(1<<(r&63)) != 0
		}
		if !workControlsEnabled || p.control == nil {
			return slow(r)
		}
		for _, rg := range ranges {
			p.stepWork()
			if rg.Lo <= r && r <= rg.Hi {
				return !neg
			}
		}
		return neg
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
	var accept func(*parser, rune) bool
	var desc expID
	switch x := e.Expr.(type) {
	case *grammar.CharClass:
		accept, desc = classAcceptControlled(x, c.control), c.desc(charClassStringControlled(x, c.control))
	case *grammar.Any:
		accept, desc = func(*parser, rune) bool { return true }, idAny
	default:
		return nil
	}
	min, max := e.Min, e.Max
	return func(p *parser) (*Node, bool) {
		count := 0
		for max < 0 || count < max {
			ch, size, ok := p.peek()
			if !ok || !accept(p, ch) {
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
	control := c.control
	if !stream {
		if m := c.scanRepeat(e, build); m != nil {
			return m
		}
	}
	elemScope := s
	if elementScopedControlled(e.Expr, control) {
		elemScope = newScope()
		elemScope.outer = s
	}
	m := c.expr(e.Expr, elemScope, build)
	ownScope := elemScope != s
	min, max := e.Min, e.Max
	elemValue := scopeValueOf(c.prog, e.Expr)
	var rs *runSite
	if c.resumable(e.Expr) { // a Document parse is not a stream: #stream has no effect there
		c.runSites++
		rs = &runSite{id: c.runSites, m: m, scope: elemScope, ownScope: ownScope, build: build, min: min, max: max, shiftable: !c.positional(e.Expr), value: elemValue}
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
			if build {
				v = elemValue(p, v, m0.pos)
				if ownScope {
					v = p.attachCaptures(v, elemScope, f, m0.pos, p.pos)
				}
			}
			count++
			if stream && p.emit != nil && p.depth == 1 {
				// Elements of the start rule's #stream are passed on without being retained, and committed.
				if v != nil {
					v.fresh = false
				}
				if err := p.emitStream(v); err != nil {
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
		return p.newNode(Node{kind: kindList, start: int32(start), end: int32(p.pos), children: p.kids(base), fresh: true}), true
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
			clearWork(p, f.vals)
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
		return p.newNode(Node{kind: kindList, start: int32(start), end: int32(p.pos), children: p.kids(base), fresh: true}), true
	}
}

// levelMin returns the minimum binding level of a call with a binding level.
func (c *compiler) levelMin(e *grammar.Ref, r *rule) int {
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

func predicateCaptures(e grammar.Expr) map[string]bool { return predicateCapturesControlled(e, nil) }

func valueNeededRules(rules []*grammar.RuleDef) map[string]bool {
	return valueNeededRulesControlled(rules, nil)
}

func hasStream(e grammar.Expr) bool { return hasStreamControlled(e, nil) }

func elementScoped(e grammar.Expr) bool { return elementScopedControlled(e, nil) }

func predicatesReadCaptures(e grammar.Expr) bool { return predicatesReadCapturesControlled(e, nil) }

func hasCaptures(e grammar.Expr) bool { return hasCapturesControlled(e, nil) }

// SourceInfo reports whether the program records source information in its results
// (Options.SourceInfo).
func (p *Program) SourceInfo() bool { return p.sourceInfo }

// DefaultValue reports whether the program was compiled to make default values (Options.DefaultValue).
func (p *Program) DefaultValue() bool { return p.defaultValue }

// SourceGrammar returns the grammar the program was compiled from: the grammar as written, without
// the hidden rules that inline actions are lowered to.
func (p *Program) SourceGrammar() *grammar.Grammar {
	if p.source != nil {
		return p.source
	}
	return p.Grammar
}

// whereName returns the text that names the rule in the message of a failed action.
func (r *rule) whereName() string {
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
