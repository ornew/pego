package lsp

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/engine"
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
	// kwStart is the byte offset of the keyword (def or type).
	kwStart int
	rule    *grammar.RuleDef
	typ     *grammar.TypeDef
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
	tokAt map[int]int
	// g is the grammar, partial if there are syntax errors (see syntax.ParsePartial).
	g          *grammar.Grammar
	syntaxErrs syntax.ErrorList
	// prog is the compiled grammar, nil if the source has errors.
	prog  *engine.Program
	diags []Diagnostic
	defs  []*definition
	// rules and types map names to their first definition.
	rules, types map[string]*definition
	// occs holds the occurrences in source order.
	occs []occurrence
	// sem holds the semantic token class of identifiers other than occurrences, by the byte offset
	// of the token.
	sem map[int]semClass
	// vars are the names of the variables that predicates assign.
	vars map[string]bool
}

// analyzeHook, if not nil, is called at the start of every analysis. Tests use it to make an
// analysis fail.
var analyzeHook func(text string)

// analyzeSafely analyzes text, turning a panic into an error.
func analyzeSafely(text string) (a *analysis, err error) {
	defer func() {
		if x := recover(); x != nil {
			a, err = nil, fmt.Errorf("%v", x)
		}
	}()
	if analyzeHook != nil {
		analyzeHook(text)
	}
	return analyze(text), nil
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
	a.syntaxErrs = syntax.ErrorList{&syntax.Error{Msg: msg}}
	a.diags = []Diagnostic{{Range: Range{}, Severity: severityError, Source: "pego", Message: msg}}
	return &a
}

func newAnalysis(text string) *analysis {
	return &analysis{text: text, idx: newTextIndex(text), tokAt: map[int]int{},
		rules: map[string]*definition{}, types: map[string]*definition{},
		sem: map[int]semClass{}, vars: map[string]bool{}}
}

func analyze(text string) *analysis {
	a := newAnalysis(text)
	for _, t := range syntax.Tokenize(text) {
		start, end := a.idx.pegoOffset(t.Pos), a.idx.pegoOffset(t.End)
		a.tokAt[start] = len(a.toks)
		a.toks = append(a.toks, token{t, start, end})
	}
	a.g, a.syntaxErrs = syntax.ParsePartial(text)
	a.collect()
	for _, e := range a.syntaxErrs {
		a.diag(a.errorRange(e.Pos, false), e.Msg)
	}
	if len(a.syntaxErrs) == 0 {
		a.compile()
	}
	sort.SliceStable(a.diags, func(i, j int) bool {
		pi, pj := a.diags[i].Range.Start, a.diags[j].Range.Start
		return pi.Line < pj.Line || pi.Line == pj.Line && pi.Character < pj.Character
	})
	return a
}

// maxCompileErrors is the number of compile errors published at most for a document; a last
// diagnostic says how many more there are. (The parser limits the number of syntax errors.)
const maxCompileErrors = 100

func (a *analysis) diag(r Range, msg string) {
	a.diags = append(a.diags, Diagnostic{Range: r, Severity: severityError, Source: "pego", Message: msg})
}

// compile compiles and type checks the grammar and records its errors.
func (a *analysis) compile() {
	prog, err := compileSafely(a.g)
	if err == nil {
		a.prog = prog
		return
	}
	var list engine.ErrorList
	if !errors.As(err, &list) {
		a.diag(a.idx.rangeOf(0, 0), err.Error())
		return
	}
	for i, e := range list {
		if i == maxCompileErrors {
			a.diag(a.errorRange(e.Pos, true), fmt.Sprintf("%d more errors", len(list)-i))
			break
		}
		a.diag(a.errorRange(e.Pos, true), e.Msg)
	}
}

// compileSafely compiles g, turning a panic of the compiler into an error so that a bug in it
// cannot stop the server.
func compileSafely(g *grammar.Grammar) (prog *engine.Program, err error) {
	defer func() {
		if x := recover(); x != nil {
			prog, err = nil, fmt.Errorf("internal error while compiling the grammar: %v", x)
		}
	}()
	return engine.Compile(g, engine.Options{})
}

// errorRange returns the range to report an error at pos at: the token at pos, or else the
// character at pos. The compiler reports errors about a definition at its keyword; with
// definitions set, they are reported at its name instead.
func (a *analysis) errorRange(pos grammar.Pos, definitions bool) Range {
	off := a.idx.pegoOffset(pos)
	if definitions {
		for _, d := range a.defs {
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

// collect records the definitions and the occurrences of names in the grammar.
func (a *analysis) collect() {
	for _, s := range a.g.Statements {
		switch s := s.(type) {
		case *grammar.RuleDef:
			d := a.define(kindRule, s.Name, s.Pos)
			if d == nil {
				continue
			}
			d.rule = s
			if a.rules[s.Name] == nil {
				a.rules[s.Name] = d
			}
			if s.Type != nil {
				a.walkType(s.Type)
			}
			a.walkExpr(s.Expr)
			if s.Action != nil {
				a.walkTerm(s.Action)
			}
		case *grammar.TypeDef:
			d := a.define(kindType, s.Name, s.Pos)
			if d == nil {
				continue
			}
			d.typ = s
			if a.types[s.Name] == nil {
				a.types[s.Name] = d
			}
			switch spec := s.Spec.(type) {
			case *grammar.StructSpec:
				for _, f := range spec.Fields {
					a.mark(f.Pos, semProperty, modDeclaration)
					a.walkType(f.Type)
				}
			case *grammar.AliasSpec:
				a.walkType(spec.Type)
			}
		}
	}
	sort.SliceStable(a.occs, func(i, j int) bool { return a.occs[i].start < a.occs[j].start })
}

// mark records the semantic token class of the token at pos.
func (a *analysis) mark(pos grammar.Pos, typ semTokenType, mods int) {
	if i, ok := a.tokenIndex(pos); ok {
		a.markToken(i, typ, mods)
	}
}

func (a *analysis) markToken(i int, typ semTokenType, mods int) {
	if i < len(a.toks) && a.toks[i].Kind == syntax.TokenIdent {
		a.sem[a.toks[i].start] = semClass{typ, mods}
	}
}

// define records the definition whose keyword is at pos.
func (a *analysis) define(kind symKind, name string, pos grammar.Pos) *definition {
	i, ok := a.tokenIndex(pos)
	if !ok || i+1 >= len(a.toks) || a.toks[i+1].Kind != syntax.TokenIdent || a.toks[i+1].Text != name {
		return nil
	}
	kw, n := a.toks[i], a.toks[i+1]
	d := &definition{kind: kind, name: name, kwStart: kw.start, nameStart: n.start, nameEnd: n.end, start: kw.start}
	// The definition ends with the last token before the next definition.
	last := i + 1
	for j := i + 2; j < len(a.toks); j++ {
		t := a.toks[j]
		if t.Kind == syntax.TokenIdent && (t.Text == "def" || t.Text == "type") {
			break
		}
		if t.Kind != syntax.TokenComment {
			last = j
		}
	}
	d.end = a.toks[last].end
	d.doc = a.docComment(i, last)
	a.defs = append(a.defs, d)
	a.occs = append(a.occs, occurrence{kind: kind, name: name, start: n.start, end: n.end, def: d})
	return d
}

// docComment returns the documentation of the definition from token first to token last: the
// comments on the lines directly above it (each the first token of its line), or else a comment
// at the end of its line if it takes one line.
func (a *analysis) docComment(first, last int) string {
	var lines []string
	line := a.toks[first].Pos.Line
	for k := first - 1; k >= 0; k-- {
		t := a.toks[k]
		if t.Kind != syntax.TokenComment || t.Pos.Line != line-1 || k > 0 && a.toks[k-1].End.Line == t.Pos.Line {
			break
		}
		lines = append(lines, commentText(t.Text))
		line--
	}
	if len(lines) == 0 && a.toks[last].Pos.Line == a.toks[first].Pos.Line && last+1 < len(a.toks) {
		if t := a.toks[last+1]; t.Kind == syntax.TokenComment && t.Pos.Line == a.toks[last].End.Line {
			return commentText(t.Text)
		}
	}
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	return strings.Join(lines, "\n")
}

func commentText(c string) string {
	c = strings.TrimPrefix(c, "//")
	return strings.TrimPrefix(c, " ")
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

func (a *analysis) walkType(t grammar.TypeExpr) {
	switch t := t.(type) {
	case *grammar.TypeRef:
		a.ref(kindType, t.Name, t.Pos)
	case *grammar.ListType:
		a.walkType(t.Elem)
	case *grammar.OptionalType:
		a.walkType(t.Elem)
	case *grammar.UnionType:
		for _, u := range t.Types {
			a.walkType(u)
		}
	}
}

func (a *analysis) walkExpr(e grammar.Expr) {
	switch e := e.(type) {
	case *grammar.Ref:
		a.ref(kindRule, e.Name, e.Pos)
	case *grammar.Seq:
		for _, x := range e.Items {
			a.walkExpr(x)
		}
	case *grammar.Choice:
		for _, x := range e.Alts {
			a.walkExpr(x)
		}
	case *grammar.Repeat:
		a.walkExpr(e.Expr)
	case *grammar.Optional:
		a.walkExpr(e.Expr)
	case *grammar.And:
		a.walkExpr(e.Expr)
	case *grammar.Not:
		a.walkExpr(e.Expr)
	case *grammar.Atomic:
		a.walkExpr(e.Expr)
	case *grammar.Discard:
		a.walkExpr(e.Expr)
	case *grammar.Capture:
		a.mark(e.Pos, semVariable, modDeclaration)
		a.walkExpr(e.Expr)
	case *grammar.Predicate:
		a.walkTerm(e.Term)
	case *grammar.Attributed:
		a.walkExpr(e.Expr)
		for _, at := range e.Attrs {
			if i, ok := a.tokenIndex(at.Pos); ok {
				a.markToken(i+1, semDecorator, 0)
			}
			for _, arg := range at.Args {
				a.walkExpr(arg.Value)
			}
		}
	case *grammar.Pratt:
		if e.Skip != nil {
			a.walkExpr(e.Skip)
		}
		for _, o := range e.Operands {
			a.walkExpr(o.Expr)
			if o.Action != nil {
				a.walkTerm(o.Action)
			}
		}
		for _, l := range e.Levels {
			for _, op := range l.Operators {
				a.walkExpr(op.Expr)
				if op.Action != nil {
					a.walkTerm(op.Action)
				}
			}
		}
	}
}

func (a *analysis) walkTerm(t grammar.Term) {
	switch t := t.(type) {
	case *grammar.Member:
		a.walkTerm(t.X)
		mods := 0
		if findBuiltin(builtinMembers, t.Name) != nil {
			mods = modDefaultLibrary
		}
		a.mark(t.Pos, semProperty, mods)
	case *grammar.New:
		// The type name follows the keyword new.
		if i, ok := a.tokenIndex(t.Pos); ok {
			a.refToken(kindType, t.Type, i+1)
		}
		for _, f := range t.Fields {
			a.mark(f.Pos, semProperty, 0)
			a.walkTerm(f.Value)
		}
	case *grammar.Call:
		a.mark(t.Pos, semFunction, modDefaultLibrary)
		for _, x := range t.Args {
			a.walkTerm(x)
		}
	case *grammar.Lambda:
		a.walkTerm(t.Body)
	case *grammar.Binary:
		a.walkTerm(t.L)
		a.walkTerm(t.R)
	case *grammar.Unary:
		a.walkTerm(t.X)
	case *grammar.VarRef:
		a.mark(t.Pos, semVariable, 0)
	case *grammar.Assign:
		a.vars[t.Name] = true
		a.mark(t.Pos, semVariable, modDeclaration)
		a.walkTerm(t.Value)
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
func (a *analysis) ruleType(d *definition) (typ string, inferred, known bool) {
	if a.prog != nil {
		if t, ok := a.prog.RuleType(d.name); ok {
			return t, d.rule.Type == nil, true
		}
	}
	if d.rule.Type != nil {
		return grammar.FormatType(d.rule.Type), false, true
	}
	return "", true, false
}
