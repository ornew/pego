package lsp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/lint"
	"github.com/ornew/pego/internal/syntax"
)

// symKind is the namespace of a name: rules and types have separate namespaces.
type symKind int

const (
	kindRule symKind = iota + 1
	kindType
)

func (k symKind) String() string {
	if k == kindType {
		return "type"
	}
	return "rule"
}

// definition is a rule or type definition.
type definition struct {
	kind symKind
	name string
	// nameStart and nameEnd are the byte span of the name, and start and end that of the whole
	// definition (from the keyword to its last token).
	nameStart, nameEnd int
	start, end         int
	// kwStart is the byte offset of the keyword (rule or type).
	kwStart int
	rule    *grammar.RuleDef
	typ     grammar.TypeDecl
	// doc is the documentation comment: the comment lines directly above the definition, or the
	// comment at the end of a one-line definition.
	doc string
}

// occurrence is a name of a rule or a type in the source: in its definition or in a reference.
type occurrence struct {
	kind       symKind
	name       string
	start, end int
	// def is the definition whose name this occurrence is, or nil for a reference.
	def *definition
}

// token is a syntax.Token with its byte span.
type token struct {
	syntax.Token
	start, end int
}

// analysis is what the server knows about one version of a document.
type analysis struct {
	text string
	idx  *textIndex
	toks []token
	// tokAt maps the byte offset at which a token starts to its index in toks.
	tokAt             map[int]int
	declarationStarts []bool
	// g is the grammar, partial if there are syntax errors (see syntax.ParsePartial).
	g          *grammar.Grammar
	syntaxErrs syntax.ErrorList
	// prog is the compiled grammar, nil if the source has errors.
	prog       *engine.Program
	diags      []Diagnostic
	lintCache  []lint.Finding
	lintCached bool
	// publicationDiags is built by the responsive worker for its immutable snapshot.
	publicationDiags []Diagnostic
	defs             []*definition
	// rules and types map names to their first definition.
	rules, types map[string]*definition
	// occs holds the occurrences in source order.
	occs []occurrence
	// sem holds the semantic token class of identifiers other than occurrences, by the byte offset
	// of the token.
	sem map[int]semClass
	// vars are the names of the variables that predicates bind.
	vars map[string]bool
}

// analyzeHook, if not nil, is called at the start of every analysis. Tests use it to make an
// analysis fail.
var analyzeHook func(text string)

// analyzeSafely analyzes text, turning a panic into an error.
func analyzeSafely(text string) (*analysis, error) {
	return analyzeContextSafely(context.Background(), text)
}

func analyzeContextSafely(ctx context.Context, text string) (a *analysis, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	defer func() {
		if x := recover(); x != nil {
			a, err = nil, fmt.Errorf("%v", x)
		}
	}()
	if analyzeHook != nil {
		analyzeHook(text)
	}
	return analyzeContext(ctx, text)
}

// failedAnalysis returns the analysis to use for text when analyzing it failed with err: good, the
// analysis of an earlier version, if there is one, or an empty analysis of text. Its only
// diagnostic reports the failure, and it counts as having a syntax error, so that formatting and
// rename, which would edit the text it describes, refuse.
func failedAnalysis(text string, good *analysis, err error) *analysis {
	msg := "internal error while analyzing the document: " + err.Error()
	var a analysis
	if good != nil {
		a = *good
	} else {
		a = *newAnalysis(text)
		a.g = &grammar.Grammar{}
	}
	a.publicationDiags = nil
	a.syntaxErrs = syntax.ErrorList{&syntax.Error{Msg: msg}}
	a.diags = []Diagnostic{{Range: Range{}, Severity: severityError, Source: "pego", Message: msg}}
	return &a
}

func newAnalysis(text string) *analysis {
	return newAnalysisWithIndex(text, newTextIndex(text))
}

func newAnalysisWithIndex(text string, idx *textIndex) *analysis {
	return &analysis{text: text, idx: idx, tokAt: map[int]int{},
		rules: map[string]*definition{}, types: map[string]*definition{},
		sem: map[int]semClass{}, vars: map[string]bool{}}
}

func analyze(text string) *analysis {
	a, _ := analyzeContext(context.Background(), text)
	return a
}

// analyzeContext polls during text indexing, tokenization, parsing, selected
// compiler traversals and local metadata construction. Individual primitives
// and other shared compiler operations remain indivisible.
func analyzeContext(ctx context.Context, text string) (*analysis, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	idx, err := newTextIndexContext(ctx, text)
	if err != nil {
		return nil, err
	}
	a := newAnalysisWithIndex(text, idx)
	tokens, err := syntax.TokenizeContext(ctx, text)
	if err != nil {
		return nil, err
	}
	for i, t := range tokens {
		if i%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		start, end := a.idx.pegoOffset(t.Pos), a.idx.pegoOffset(t.End)
		a.tokAt[start] = len(a.toks)
		a.toks = append(a.toks, token{t, start, end})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.g, a.syntaxErrs, err = syntax.ParsePartialContext(ctx, text)
	if err != nil {
		return nil, err
	}
	if err := a.finishMetadataContext(ctx); err != nil {
		return nil, err
	}
	return a, nil
}

// maxCompileErrors is the number of compile errors published at most for a document; a last
// diagnostic says how many more there are. (The parser limits the number of syntax errors.)
const maxCompileErrors = 100

func (a *analysis) diag(r Range, msg string) {
	a.diags = append(a.diags, Diagnostic{Range: r, Severity: severityError, Source: "pego", Message: msg})
}

// compileContext records ordinary compile errors; cancellation publishes neither
// a program nor diagnostics for the unfinished compilation.
func (a *analysis) compileContextControlled(ctx context.Context, control *renderControl) error {
	control.poll()
	prog, err := compileContextSafely(ctx, a.g)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if err == nil {
		a.prog = prog
		return nil
	}
	var list engine.ErrorList
	if !errors.As(err, &list) {
		a.diag(a.idx.rangeOf(0, 0), err.Error())
		return nil
	}
	a.mapCompileDiagnostics(list, control)
	return nil
}

// compileSafely compiles g, turning a panic of the compiler into an error so that a bug in it
// cannot stop the server.
func compileSafely(g *grammar.Grammar) (*engine.Program, error) {
	return compileContextSafely(context.Background(), g)
}

func compileContextSafely(ctx context.Context, g *grammar.Grammar) (prog *engine.Program, err error) {
	defer func() {
		if x := recover(); x != nil {
			prog, err = nil, fmt.Errorf("internal error while compiling the grammar: %v", x)
		}
	}()
	return engine.CompileContext(ctx, g, engine.Options{})
}

// errorRange returns the range to report an error at pos at: the token at pos, or else the
// character at pos. The compiler reports errors about a definition at its keyword; with
// definitions set, they are reported at its name instead.
func (a *analysis) errorRangeControlled(pos grammar.Pos, definitions bool, control *renderControl) Range {
	control.poll()
	off := a.idx.pegoOffset(pos)
	if definitions {
		for _, d := range a.defs {
			control.poll()
			if d.kwStart == off {
				return a.idx.rangeOf(d.nameStart, d.nameEnd)
			}
		}
	}
	if i, ok := a.tokAt[off]; ok {
		return a.idx.rangeOf(a.toks[i].start, a.toks[i].end)
	}
	end := off
	if off < len(a.text) && a.text[off] != '\n' && a.text[off] != '\r' {
		_, size := utf8.DecodeRuneInString(a.text[off:])
		end += size
	}
	return a.idx.rangeOf(off, end)
}

// tokenIndex returns the index of the token that starts at the PEGO position pos.
func (a *analysis) tokenIndex(pos grammar.Pos) (int, bool) {
	i, ok := a.tokAt[a.idx.pegoOffset(pos)]
	return i, ok
}

// significantToken skips comments from i, returning len(toks) at end of input.
// Other tokens, including punctuation, retain their place in the syntax.
func (a *analysis) significantTokenControlled(i int, control *renderControl) int {
	control.poll()
	for i < len(a.toks) && a.toks[i].Kind == syntax.TokenComment {
		control.poll()
		i++
	}
	return i
}

// previousSignificantToken skips comments backwards; -1 means the start of input.
func (a *analysis) previousSignificantTokenControlled(i int, control *renderControl) int {
	control.poll()
	for i >= 0 && a.toks[i].Kind == syntax.TokenComment {
		control.poll()
		i--
	}
	return i
}

// markAttributeName marks the name of the attribute at as a decorator.
func (a *analysis) markAttributeName(at *grammar.Attribute) {
	if i, ok := a.tokenIndex(at.Diagnostic()); ok {
		// Attribute names and markers must be adjacent; comments are
		// not permitted between them by the lexical contract.
		if i+1 < len(a.toks) && a.toks[i].end == a.toks[i+1].start {
			a.markToken(i+1, semDecorator, 0)
		}
	}
}

// collect records the definitions and the occurrences of names in the grammar.
func (a *analysis) collectControlled(control *renderControl) {
	control.poll()
	a.mark(a.g.File.Pkg.SourceInfo.Diagnostic(), semKeyword, 0)
	for _, s := range a.g.File.Decls {
		control.poll()
		switch s := s.(type) {
		case *grammar.RuleDef:
			for _, at := range s.Attrs {
				control.poll()
				// The arguments of a declaration attribute name its options,
				// not rules.
				a.markAttributeName(at)
			}
			a.mark(s.Diagnostic(), semKeyword, 0)
			d := a.define(kindRule, s.Name, s.Pos, s.Diagnostic(), control)
			if d == nil {
				continue
			}
			d.rule = s
			if a.rules[s.Name] == nil {
				a.rules[s.Name] = d
			}
			if s.Type != nil {
				a.walkType(s.Type, control)
			}
			a.walkExpr(s.Expr, control)
			if s.Action != nil {
				a.walkTerm(s.Action, control)
			}
		case *grammar.VarDef:
			a.mark(s.Diagnostic(), semKeyword, 0)
			a.vars[s.Name] = true
			if i, ok := a.tokenIndex(s.Diagnostic()); ok {
				a.markToken(a.significantTokenControlled(i+1, control), semVariable, modDeclaration)
			}
			if s.Type != nil {
				a.walkType(s.Type, control)
			}
			if s.Value != nil {
				a.walkTerm(s.Value, control)
			}
		case grammar.TypeDecl:
			a.mark(s.Diagnostic(), semKeyword, 0)
			d := a.define(kindType, s.DeclName(), s.Diagnostic(), s.Diagnostic(), control)
			if d == nil {
				continue
			}
			d.typ = s
			if a.types[s.DeclName()] == nil {
				a.types[s.DeclName()] = d
			}
			if i, ok := a.tokenIndex(s.Diagnostic()); ok {
				name := a.significantTokenControlled(i+1, control)
				spec := a.significantTokenControlled(name+1, control)
				if a.isIdent(spec, "terminal") {
					a.markToken(spec, semKeyword, 0)
				}
			}
			switch spec := s.(type) {
			case *grammar.TypeDef:
				if spec.Struct() == nil && spec.Type != nil {
					a.walkType(spec.Type, control)
				}
				for _, f := range spec.Fields() {
					control.poll()
					a.mark(f.Diagnostic(), semProperty, modDeclaration)
					a.walkType(f.Type, control)
				}
			case *grammar.TypeAlias:
				a.walkType(spec.Type, control)
			}
		}
	}
	a.sortOccurrences(control)
}

// mark records the semantic token class of the token at pos.
func (a *analysis) mark(pos grammar.Pos, typ semTokenType, mods int) {
	if i, ok := a.tokenIndex(pos); ok {
		a.markToken(i, typ, mods)
	}
}

func (a *analysis) markToken(i int, typ semTokenType, mods int) {
	if i >= 0 && i < len(a.toks) && a.toks[i].Kind == syntax.TokenIdent {
		a.sem[a.toks[i].start] = semClass{typ, mods}
	}
}

// define records the definition that starts at start, with its first declaration attribute if it
// has any, and whose keyword is at pos.
func (a *analysis) define(kind symKind, name string, start, pos grammar.Pos, control *renderControl) *definition {
	control.poll()
	i, ok := a.tokenIndex(pos)
	if !ok {
		return nil
	}
	first, ok := a.tokenIndex(start)
	if !ok || first > i {
		first = i
	}
	next := a.significantTokenControlled(i+1, control)
	if next >= len(a.toks) || a.toks[next].Kind != syntax.TokenIdent || a.toks[next].Text != name {
		return nil
	}
	kw, n := a.toks[i], a.toks[next]
	d := &definition{kind: kind, name: name, kwStart: kw.start, nameStart: n.start, nameEnd: n.end, start: a.toks[first].start}
	// Declarations end at a top-level separator or the next declaration, which
	// may start with declaration attributes. A constructor or operators block
	// closing brace can still precede an action.
	last, depth := next, 0
	for j := next + 1; j < len(a.toks); j++ {
		control.poll()
		t := a.toks[j]
		if depth == 0 && (a.isDefKeyword(j) || a.declarationStart(j) && (a.isIdent(j, "package") || a.isPunct(j, "@")) || a.isPunct(j, ";")) {
			break
		}
		if t.Kind != syntax.TokenComment {
			last = j
		}
		if t.Kind == syntax.TokenPunct {
			switch t.Text {
			case "(", "(?", "{", "[":
				depth++
			case ")", "}", "]":
				depth = max(0, depth-1)
			}
		}
	}

	d.end = a.toks[last].end
	d.doc = a.docComment(first, last, control)
	a.defs = append(a.defs, d)
	a.occs = append(a.occs, occurrence{kind: kind, name: name, start: n.start, end: n.end, def: d})
	return d
}

// docComment returns the documentation of the definition from token first to token last: the
// comments on the lines directly above it (each the first token of its line), or else a comment
// at the end of its line if it takes one line.
func (a *analysis) docComment(first, last int, control *renderControl) string {
	control.poll()
	var lines []string
	line := a.toks[first].Pos.Line
	for k := first - 1; k >= 0; k-- {
		control.poll()
		t := a.toks[k]
		if t.Kind != syntax.TokenComment || t.End.Line != line-1 || k > 0 && a.toks[k-1].End.Line == t.Pos.Line {
			break
		}
		lines = append(lines, commentText(t.Text, control))
		line = t.Pos.Line
	}
	if len(lines) == 0 && a.toks[last].Pos.Line == a.toks[first].Pos.Line && last+1 < len(a.toks) {
		if t := a.toks[last+1]; t.Kind == syntax.TokenComment && t.Pos.Line == a.toks[last].End.Line {
			return commentText(t.Text, control)
		}
	}
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		control.poll()
		lines[i], lines[j] = lines[j], lines[i]
	}
	return control.join(lines, "\n")
}

func commentText(text string, control *renderControl) string {
	control.poll()
	if strings.HasPrefix(text, "/*") {
		return control.trimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "/*"), "*/"))
	}
	text = strings.TrimPrefix(text, "//")
	return strings.TrimPrefix(text, " ")
}

// ref records a reference to the name at pos.
func (a *analysis) ref(kind symKind, name string, pos grammar.Pos) {
	if i, ok := a.tokenIndex(pos); ok {
		a.refToken(kind, name, i)
	}
}

func (a *analysis) refToken(kind symKind, name string, i int) {
	if i >= len(a.toks) {
		return
	}
	t := a.toks[i]
	if t.Kind != syntax.TokenIdent || t.Text != name {
		return
	}
	a.occs = append(a.occs, occurrence{kind: kind, name: name, start: t.start, end: t.end})
}

func (a *analysis) walkType(t grammar.TypeExpr, control *renderControl) {
	control.poll()
	switch t := t.(type) {
	case *grammar.TypeGroup:
		a.walkType(t.Type, control)
	case *grammar.TypeRef:
		a.ref(kindType, t.Name, t.Diagnostic())
	case *grammar.ListType:
		a.walkType(t.Elem, control)
	case *grammar.OptionalType:
		a.walkType(t.Elem, control)
	case *grammar.JoinType:
		for _, u := range t.Types {
			control.poll()
			a.walkType(u, control)
		}
	case *grammar.MeetType:
		for _, u := range t.Types {
			control.poll()
			a.walkType(u, control)
		}
	case *grammar.TupleType:
		for _, u := range t.Types {
			control.poll()
			a.walkType(u, control)
		}
	case *grammar.TypeApp:
		a.ref(kindType, t.Name, t.Diagnostic())
		for _, u := range t.Args {
			control.poll()
			a.walkType(u, control)
		}
	case *grammar.StructType:
		for _, f := range t.Fields {
			control.poll()
			a.mark(f.Diagnostic(), semProperty, modDeclaration)
			a.walkType(f.Type, control)
		}
	}
}

func (a *analysis) walkExpr(e grammar.Expr, control *renderControl) {
	control.poll()
	switch e := e.(type) {
	case *grammar.Group:
		a.walkExpr(e.Expr, control)
	case *grammar.InlineAction:
		a.walkExpr(e.Expr, control)
		a.walkTerm(e.Action, control)
	case *grammar.Ref:
		if e.Level != "" {
			if i, ok := a.tokenIndex(e.Diagnostic()); ok {
				open := a.previousSignificantTokenControlled(i-1, control)
				keyword := a.previousSignificantTokenControlled(open-1, control)
				if a.isPunct(open, "(") && a.isIdent(keyword, "level") {
					a.markToken(keyword, semKeyword, 0)
				}
			}
		}
		a.ref(kindRule, e.Name, e.Diagnostic())
	case *grammar.Seq:
		for _, x := range e.Items {
			control.poll()
			a.walkExpr(x, control)
		}
	case *grammar.Choice:
		for _, x := range e.Alts {
			control.poll()
			a.walkExpr(x, control)
		}
	case *grammar.Repeat:
		a.walkExpr(e.Expr, control)
	case *grammar.Optional:
		a.walkExpr(e.Expr, control)
	case *grammar.And:
		a.walkExpr(e.Expr, control)
	case *grammar.Not:
		a.walkExpr(e.Expr, control)
	case *grammar.Atomic:
		a.walkExpr(e.Expr, control)
	case *grammar.Stream:
		a.walkExpr(e.Expr, control)
	case *grammar.Recovery:
		a.walkExpr(e.Expr, control)
		a.walkExpr(e.Skip, control)
	case *grammar.Discard:
		a.walkExpr(e.Expr, control)
	case *grammar.Capture:
		a.mark(e.Diagnostic(), semVariable, modDeclaration)
		a.walkExpr(e.Expr, control)
	case *grammar.Predicate:
		a.walkTerm(e.Term, control)
	case *grammar.Attributed:
		a.walkExpr(e.Expr, control)
		for _, at := range e.Attrs {
			control.poll()
			a.markAttributeName(at)
			for _, arg := range control.expressionArgs(at) {
				control.poll()
				a.walkExpr(arg, control)
			}
		}
	case *grammar.OperatorsRule:
		if i, ok := a.tokenIndex(e.Diagnostic()); ok {
			a.markToken(a.previousSignificantTokenControlled(i-1, control), semKeyword, 0)
		}
		a.mark(e.SkipSource.Diagnostic(), semKeyword, 0)
		if e.Skip != nil {
			a.walkExpr(e.Skip, control)
		}
		for _, o := range e.Operands {
			control.poll()
			a.mark(o.Diagnostic(), semKeyword, 0)
			a.walkExpr(o.Expr, control)
			if o.Action != nil {
				a.walkTerm(o.Action, control)
			}
		}
		for _, l := range e.Levels {
			control.poll()
			a.mark(l.Diagnostic(), semKeyword, 0)
			for _, op := range l.Operators {
				control.poll()
				a.mark(op.Diagnostic(), semKeyword, 0)
				if op.Kind == grammar.Infix {
					if i, ok := a.tokenIndex(op.Diagnostic()); ok {
						a.markToken(a.significantTokenControlled(i+1, control), semKeyword, 0)
					}
				}
				a.walkExpr(op.Expr, control)
				if op.Action != nil {
					a.walkTerm(op.Action, control)
				}
			}
		}
	}
}

func (a *analysis) walkTerm(t grammar.Term, control *renderControl) {
	control.poll()
	switch t := t.(type) {
	case *grammar.TermGroup:
		a.walkTerm(t.Term, control)
	case *grammar.Block:
		for _, stmt := range t.Statements {
			control.poll()
			a.walkTerm(stmt, control)
		}
	case *grammar.Member:
		a.walkTerm(t.X, control)
		mods := 0
		if findBuiltin(builtinMembers, t.Name) != nil {
			mods = modDefaultLibrary
		}
		a.mark(t.Diagnostic(), semProperty, mods)
	case *grammar.New:
		// The constructor starts at its type name.
		if i, ok := a.tokenIndex(t.Diagnostic()); ok {
			a.refToken(kindType, t.Type, i)
		}
		for _, f := range t.Fields {
			control.poll()
			a.mark(f.Diagnostic(), semProperty, 0)
			a.walkTerm(f.Value, control)
		}
		for _, x := range t.Args {
			control.poll()
			a.walkTerm(x, control)
		}
	case *grammar.Call:
		a.mark(t.Diagnostic(), semFunction, modDefaultLibrary)
		for _, x := range t.Args {
			control.poll()
			a.walkTerm(x, control)
		}
	case *grammar.ListConstructor:
		a.walkType(t.Elem, control)
		for _, item := range t.Items {
			control.poll()
			a.walkTerm(item, control)
		}
	case *grammar.TupleLit:
		for _, x := range t.Elems {
			control.poll()
			a.walkTerm(x, control)
		}
	case *grammar.Lambda:
		a.walkTerm(t.Body, control)
	case *grammar.Binary:
		a.walkTerm(t.L, control)
		a.walkTerm(t.R, control)
	case *grammar.Unary:
		a.walkTerm(t.X, control)
	case *grammar.Cast:
		a.walkTerm(t.X, control)
		a.walkType(t.Type, control)
	case *grammar.VarRef:
		a.mark(t.Diagnostic(), semVariable, 0)
	case *grammar.CaptureRef:
		if t.Param {
			a.mark(t.Diagnostic(), semParameter, 0)
		}
	case *grammar.Bind:
		a.vars[t.Name] = true
		a.mark(t.Diagnostic(), semVariable, modDeclaration)
		a.walkTerm(t.Value, control)
	}
}

// occurrenceAt returns the occurrence at the byte offset off, including one that ends at off
// (the cursor just after a name).
func (a *analysis) occurrenceAt(off int) (occurrence, bool) {
	i := sort.Search(len(a.occs), func(i int) bool { return a.occs[i].end >= off })
	if i < len(a.occs) && a.occs[i].start <= off {
		return a.occs[i], true
	}
	return occurrence{}, false
}

// lookup returns the first definition of name in the namespace kind.
func (a *analysis) lookup(kind symKind, name string) *definition {
	if kind == kindType {
		return a.types[name]
	}
	return a.rules[name]
}

// tokenAt returns the index of the token that contains the byte offset off or ends at it, or -1.
// A token that starts at off is preferred to one that ends there.
func (a *analysis) tokenAt(off int) int {
	i := sort.Search(len(a.toks), func(i int) bool { return a.toks[i].end >= off })
	if i < len(a.toks) && a.toks[i].start <= off {
		if a.toks[i].end == off && i+1 < len(a.toks) && a.toks[i+1].start == off {
			return i + 1
		}
		return i
	}
	return -1
}

// ruleType returns the type of the rule d as written, whether it was inferred, and whether it is
// known.
func (a *analysis) ruleTypeControlled(d *definition, control *renderControl) (typ string, inferred, known bool) {
	control.poll()
	if a.prog != nil {
		if t, ok := control.ruleType(a.prog, d.name); ok {
			return t, d.rule.Type == nil, true
		}
	}
	if d.rule.Type != nil {
		return control.formatType(d.rule.Type), false, true
	}
	return "", true, false
}
