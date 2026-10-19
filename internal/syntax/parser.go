package syntax

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
)

// Error is a syntax error.
type Error struct {
	Pos grammar.Pos
	// End is the position just after the text the error is about, when the lexer knows it (an
	// escape sequence, a run of invalid characters), and the zero Pos otherwise.
	End grammar.Pos
	Msg string
}

func (e *Error) Error() string { return fmt.Sprintf("%d:%d: %s", e.Pos.Line, e.Pos.Col, e.Msg) }

// ErrorList is a list of syntax errors.
type ErrorList []*Error

func (l ErrorList) Error() string {
	msgs := make([]string, len(l))
	for i, e := range l {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "\n")
}

// maxErrors is the number of errors reported at most; a last error says that there are more.
const maxErrors = 100

// maxNesting bounds the nesting of the parser's recursive calls (see enter), about two for each
// level of parentheses: deeper nesting of expressions, values and types is an error, so that a
// malformed or hostile source cannot overflow the stack.
const maxNesting = 1000

type errorList struct{ list ErrorList }

func (e *errorList) add(pos grammar.Pos, msg string) { e.addRange(pos, grammar.Pos{}, msg) }

func (e *errorList) addRange(pos, end grammar.Pos, msg string) {
	// Report only the first error at a given position.
	if n := len(e.list); n > 0 && e.list[n-1].Pos == pos {
		return
	}
	if len(e.list) >= maxErrors {
		if len(e.list) == maxErrors {
			e.list = append(e.list, &Error{Pos: pos, Msg: "too many errors"})
		}
		return
	}
	e.list = append(e.list, &Error{Pos: pos, End: end, Msg: msg})
}

// Parse parses the PEGO source code src.
func Parse(src string) (*grammar.Grammar, error) {
	errs := &errorList{}
	p := &parser{toks: lex(src, errs), errs: errs}
	g := p.file()
	if len(errs.list) > 0 {
		return nil, errs.list
	}
	return g, nil
}

// legacyForms locates the removed forms that parseLegacy represents in the current syntax tree.
type legacyForms struct {
	// terminals are the definitions written type T terminal, parsed as T = { Text string }. The
	// source of a definition's spec is the word terminal.
	terminals map[*grammar.TypeDef]bool
	// positional are the constructors written T{x}, parsed as T{Text: x}.
	positional map[*grammar.New]bool
}

// parseLegacy parses src like Parse but accepts the removed syntax that pego fix rewrites:
// positional captures, lambda parameters written $x, terminal types, positional constructors
// and the retired names of concrete syntax tree types. Terminal types and positional
// constructors are parsed as the text structs that replace them, and located in the returned
// legacyForms.
func parseLegacy(src string) (*grammar.Grammar, *legacyForms, error) {
	errs := &errorList{}
	forms := &legacyForms{terminals: map[*grammar.TypeDef]bool{}, positional: map[*grammar.New]bool{}}
	p := &parser{toks: lex(src, errs), errs: errs, legacy: forms}
	g := p.file()
	if len(errs.list) > 0 {
		return nil, nil, errs.list
	}
	return g, forms, nil
}

// ParseExpr parses a single parser expression. It is intended for tests.
func ParseExpr(src string) (expr grammar.Expr, err error) {
	errs := &errorList{}
	p := &parser{toks: lex(src, errs), errs: errs, layout: true}
	defer func() {
		if value := recover(); value != nil {
			if _, ok := value.(bail); !ok {
				panic(value)
			}
			expr, err = nil, errs.list
		}
	}()
	e := p.inlineChoice()
	if p.tok().kind != tEOF {
		p.errorf("unexpected %s", p.tok())
	}
	if len(errs.list) > 0 {
		return nil, errs.list
	}
	return e, nil
}

type parser struct {
	toks []token
	i    int
	errs *errorList
	// legacy, if not nil, accepts the removed syntax that pego fix rewrites (see parseLegacy) and
	// records where it is.
	legacy *legacyForms
	// params holds the parameters of the lambdas whose bodies are being parsed, innermost last.
	params []string
	ctx    context.Context
	work   uint
	// cur is the line break most recently recorded. Comments that do not
	// precede a recorded line break are added to it, because the formatter
	// writes the tokens they follow on the line after it.
	cur *grammar.LineBreak
	// nest counts the choices being parsed with choice. Line breaks are
	// recorded only outside them, in the top level of a rule body.
	nest int
	// depth counts the nested calls of the functions that call deeper (see enter).
	depth int
	// layout restricts rule-body continuation outside open delimiters.
	layout         bool
	layoutDepth    int
	layoutStart    int
	header         bool
	headerComments []*grammar.Comment
	headerPos      grammar.Pos
	recoveryStart  int
}

// bail is the panic value used to abort parsing on an unrecoverable
// error.
type bail struct{}

// parseCancelled unwinds the parser without treating cancellation as a
// recoverable syntax error. The caller discards the incomplete grammar.
type parseCancelled struct{ err error }

// poll charges token consumption and token-array scans alike. Ordinary parses
// have no context; contextual parses check after at most 256 such steps.
func (p *parser) poll() {
	p.charge(1)
}

func (p *parser) charge(steps uint) {
	if p.ctx == nil {
		return
	}
	old := p.work
	p.work += steps
	if old/256 != p.work/256 {
		if err := p.ctx.Err(); err != nil {
			panic(parseCancelled{err})
		}
	}
}

func (p *parser) tok() token { return p.toks[p.i] }
func (p *parser) peek(k int) token {
	p.poll()
	if p.i+k < len(p.toks) {
		return p.toks[p.i+k]
	}
	return p.toks[len(p.toks)-1]
}
func (p *parser) next() token {
	p.poll()
	t := p.toks[p.i]
	if p.header {
		if t.lineComment != nil {
			t.lineComment.Header = p.headerPos
			p.headerComments = append(p.headerComments, t.lineComment)
		}
		for _, comment := range t.comments {
			p.poll()
			comment.Header = p.headerPos
		}
		p.headerComments = p.appendComments(p.headerComments, t.comments)
	}
	if p.layout {
		if p.boundary() {
			p.errorf("multiline expressions require their own open delimiter")
			panic(bail{})
		}
		if t.kind == tPunct {
			switch t.text {
			case "(", "{", "[":
				p.layoutDepth++
			case ")", "}", "]":
				if p.layoutDepth > 0 {
					p.layoutDepth--
				}
			}
		}
	}
	p.takeComments(p.i)
	if p.i < len(p.toks)-1 {
		p.i++
	}
	return t
}

func (p *parser) appendComments(dst, comments []*grammar.Comment) []*grammar.Comment {
	if p.ctx == nil {
		return append(dst, comments...)
	}
	for len(comments) > 0 {
		n := min(len(comments), 256)
		p.charge(uint(n))
		dst = append(dst, comments[:n]...)
		comments = comments[n:]
	}
	return dst
}

// boundary marks a new statement outside the current expression's delimiters.
func (p *parser) boundary() bool {
	return p.layout && p.layoutDepth == 0 && p.i > p.layoutStart && p.tok().lineBefore
}

func (p *parser) statement() func() {
	layout, depth, start := p.layout, p.layoutDepth, p.layoutStart
	p.layout, p.layoutDepth, p.layoutStart = true, 0, p.i
	return func() { p.layout, p.layoutDepth, p.layoutStart = layout, depth, start }
}

// takeComments moves the comments before token i to the current line
// break.
func (p *parser) takeComments(i int) {
	t := &p.toks[i]
	if p.cur != nil {
		if t.lineComment != nil {
			p.cur.Comments = append(p.cur.Comments, t.lineComment)
		}
		p.cur.Comments = p.appendComments(p.cur.Comments, t.comments)
	}
	t.lineComment, t.comments = nil, nil
}

// lineBreak records the start of a line at the current token, where the
// formatter may start a new line, with the comments before the token. A
// comment at the end of the previous line becomes the trailing comment of
// the line break recorded before.
func (p *parser) lineBreak() *grammar.LineBreak {
	t := &p.toks[p.i]
	b := &grammar.LineBreak{Comments: t.comments, Blank: t.blankBefore}
	if c := t.lineComment; c != nil {
		if p.cur != nil && p.cur.Trailing == nil {
			p.cur.Trailing = c
		} else {
			b.Comments = p.appendComments([]*grammar.Comment{c}, b.Comments)
		}
	}
	t.lineComment, t.comments = nil, nil
	p.cur = b
	return b
}

// multiline reports whether a line break precedes any of the tokens after
// token i up to token j.
func (p *parser) multiline(i, j int) bool {
	for k := i + 1; k <= j && k < len(p.toks); k++ {
		p.poll()
		if p.toks[k].lineBefore {
			return true
		}
	}
	return false
}

func (p *parser) is(text string) bool {
	t := p.tok()
	return (t.kind == tPunct || t.kind == tIdent) && t.text == text
}

func (p *parser) accept(text string) bool {
	if p.is(text) {
		p.next()
		return true
	}
	return false
}

func (p *parser) errorf(format string, args ...any) {
	p.errs.add(p.tok().pos, fmt.Sprintf(format, args...))
}

func (p *parser) expect(text string) token {
	if !p.is(text) {
		p.errorf("expected '%s', found %s", text, p.tok())
		panic(bail{})
	}
	return p.next()
}

func (p *parser) ident() token {
	if p.boundary() {
		p.errorf("expected identifier on the same statement, found %s", p.tok())
		panic(bail{})
	}
	if p.tok().kind != tIdent {
		p.errorf("expected identifier, found %s", p.tok())
		panic(bail{})
	}
	return p.next()
}

var keywords = map[string]bool{
	"package": true, "type": true, "rule": true, "struct": true, "terminal": true,
	"operators": true, "skip": true, "operand": true, "level": true, "prefix": true,
	"postfix": true, "infix": true,
}

func (p *parser) file() *grammar.Grammar {
	g := &grammar.Grammar{}

	if p.is("package") {
		packageStart := p.i
		g.File.Pkg.Break = p.lineBreak()
		restore := p.statement()
		g.File.Pkg.SourceInfo.Pos = p.next().pos
		p.header = true
		p.headerPos = g.File.Pkg.SourceInfo.Pos
		func() {
			defer func() { p.header = false }()
			defer p.recover() // a missing name skips to the first definition
			g.File.Pkg.Name = p.ident().text
		}()
		restore()
		p.source(&g.File.Pkg.SourceInfo, packageStart)
		p.declarationEnd()
		p.sourceTrailing(&g.File.Pkg.SourceInfo)
	}
	for p.tok().kind != tEOF {
		start := p.i
		var statement grammar.SourceNode
		p.recoveryStart = start
		func() {
			defer p.recover()
			switch {
			case p.is("type"):
				t := p.typeDecl()
				statement = t.(grammar.SourceNode)
				g.File.Decls = append(g.File.Decls, t)
			case p.is("rule"):
				r := p.ruleDef()
				statement = r
				g.File.Decls = append(g.File.Decls, r)
			default:
				p.errorf("expected 'rule' or 'type', found %s", p.tok())
				panic(bail{})
			}
		}()
		if p.i == start {
			p.next()
		}
		p.declarationEnd()
		if statement != nil {
			p.sourceTrailing(statement.Source())
		}
	}
	g.File.EndBreak = p.lineBreak()
	g.File.HeaderComments = p.headerComments
	g.File.Pos = p.toks[0].pos
	g.File.End = g.File.Pos
	if p.i > 0 {
		g.File.End = p.toks[p.i-1].end
	}
	return g
}

// declarationEnd consumes explicit separators, otherwise requiring a new
// declaration after a newline.
func (p *parser) declarationEnd() {
	if p.accept(";") {
		for p.accept(";") {
		}
		return
	}
	if p.tok().kind != tEOF && !p.tok().lineBefore {
		p.errorf("expected a semicolon or newline between declarations")
	}
}

// recover recovers from a bail and skips to the next definition.
func (p *parser) recover() {
	x := recover()
	if x == nil {
		return
	}
	if _, ok := x.(bail); !ok {
		panic(x)
	}
	p.layout, p.layoutDepth, p.header = false, 0, false
	// An unclosed group can consume a contextual declaration introducer as a
	// reference before its '=' exposes the error. Recover its statement header
	// rather than losing the following declaration from partial tooling.
	for j := p.i - 1; j > p.recoveryStart; j-- {
		p.poll()
		t := p.toks[j]
		if t.lineBefore && t.kind == tIdent && (t.text == "rule" || t.text == "type") && j+1 < len(p.toks) && p.toks[j+1].kind == tIdent {
			p.i = j
			return
		}
	}
	for p.tok().kind != tEOF && !p.is("rule") && !p.is("type") {
		p.next()
	}
}

func (p *parser) typeDecl() (sourceResult grammar.TypeDecl) {
	sourceStart := p.i
	defer func() {
		if sourceResult != nil {
			p.sourceNode(sourceResult, sourceStart)
		}
	}()

	defer p.statement()()
	br := p.lineBreak()
	pos := p.expect("type").pos
	p.header = true
	p.headerPos = pos
	defer func() { p.header = false }()
	name := p.ident()
	info := grammar.SourceInfo{Pos: pos}
	def := func(t grammar.TypeExpr) *grammar.TypeDef {
		return &grammar.TypeDef{SourceInfo: info, Name: name.text, Type: t, Break: br}
	}
	specStart := p.i
	switch {
	case p.is("struct"):
		p.errorf("a struct type is defined as type %s = {...}; run pego fix to rewrite type %s struct", name.text, name.text)
		panic(bail{})
	case p.is("=") && p.peek(1).kind == tPunct && p.peek(1).text == "{":
		p.next()
		p.header = false
		p.layout, p.layoutDepth = false, 0
		st := &grammar.StructType{}
		st.Fields, st.CloseBreak, st.OneLine = p.structFields()
		p.sourceNode(st, specStart)
		return def(st)
	case p.is("terminal"):
		if p.legacy == nil {
			p.errorf("a terminal type is the struct type %s = { Text string }; run pego fix to rewrite type %s terminal", name.text, name.text)
			panic(bail{})
		}
		p.next()
		p.header = false
		st := &grammar.StructType{Fields: []*grammar.Field{{Name: "Text", Type: &grammar.TypeRef{Name: "string"}}}}
		p.sourceNode(st, specStart)
		td := def(st)
		p.legacy.terminals[td] = true
		return td
	case p.is("=") && p.peek(1).kind == tIdent && p.peek(1).text == "node" && p.peek(2).text == "[":
		// A named node type: type N = node[T].
		p.next()
		p.header = false
		app, _ := p.typeTerm().(*grammar.TypeApp)
		if app == nil || len(app.Args) != 1 {
			p.errorf("a node type takes one type argument: type %s = node[T]", name.text)
			panic(bail{})
		}
		return def(app)
	case p.is("="):
		p.errorf("expected '{' or 'node[' after '='; write type %s ~ <type> for an alias (pego fix rewrites type %s = <type>)", name.text, name.text)
		panic(bail{})
	case p.accept("~"):
		p.header = false
		return &grammar.TypeAlias{SourceInfo: info, Name: name.text, Type: p.typeExpr(), Break: br}
	default:
		p.errorf("expected '=' or '~', found %s", p.tok())
		panic(bail{})
	}
}

func (p *parser) typeExpr() (sourceResult grammar.TypeExpr) {
	sourceStart := p.i
	defer func() {
		if sourceResult != nil {
			p.sourceNode(sourceResult, sourceStart)
		}
	}()

	t := p.typeMeet()
	if !p.is("|") {
		return t
	}
	u := &grammar.UnionType{Types: []grammar.TypeExpr{t}}
	for p.accept("|") {
		u.Types = append(u.Types, p.typeMeet())
	}
	return u
}

// typeMeet parses T & U, which binds tighter than |.
func (p *parser) typeMeet() (sourceResult grammar.TypeExpr) {
	sourceStart := p.i
	defer func() {
		if sourceResult != nil {
			p.sourceNode(sourceResult, sourceStart)
		}
	}()

	t := p.typeTerm()
	if !p.is("&") {
		return t
	}
	m := &grammar.IntersectionType{Types: []grammar.TypeExpr{t}}
	for p.accept("&") {
		m.Types = append(m.Types, p.typeTerm())
	}
	return m
}

// enter counts one more level of nesting and fails beyond maxNesting. The caller defers
// p.leave().
func (p *parser) enter() {
	p.depth++
	if p.depth > maxNesting {
		p.errorf("nesting too deep")
		panic(bail{})
	}
}

func (p *parser) leave() { p.depth-- }

func (p *parser) typeTerm() (sourceResult grammar.TypeExpr) {
	sourceStart := p.i
	defer func() {
		if sourceResult != nil {
			p.sourceNode(sourceResult, sourceStart)
		}
	}()

	p.enter()
	defer p.leave()
	switch {
	case p.accept("[]"):
		return &grammar.ListType{Elem: p.typeTerm()}
	case p.accept("*"):
		return &grammar.OptionalType{Elem: p.typeTerm()}
	case p.accept("("):
		open := p.i - 1
		outer := p.cur
		defer func() { p.cur = outer }()
		g := &grammar.TypeGroup{OpenBreak: &grammar.LineBreak{}}
		p.cur = g.OpenBreak
		g.BodyBreak = p.lineBreak()
		if p.is(")") {
			return p.tupleType(open, g, nil) // the unit ()
		}
		g.Type = p.typeExpr()
		if p.is(",") {
			return p.tupleType(open, g, g.Type)
		}
		g.CloseBreak = p.lineBreak()
		g.OneLine = !p.multiline(open, p.i)
		p.expect(")")
		return g
	case p.is("{"):
		// An unnamed struct type. Its fields are statements of their own, as in a definition.
		outer, header := p.cur, p.header
		defer func() { p.cur, p.header = outer, header }()
		p.header = false
		s := &grammar.StructType{}
		s.Fields, s.CloseBreak, s.OneLine = p.structFields()
		return s
	case p.is("_|_"):
		t := p.next()
		return &grammar.TypeRef{SourceInfo: grammar.SourceInfo{Pos: t.pos}, Name: t.text}
	}
	name := p.ident()
	if replacement, ok := valueTypeNames[name.text]; ok && p.legacy == nil {
		p.errs.add(name.pos, fmt.Sprintf("%s is not a type; write %s (pego fix rewrites it)", name.text, replacement))
	} else if name.text == "node" && !p.is("[") && p.legacy == nil {
		p.errs.add(name.pos, bareNode+" (pego fix rewrites it)")
	}
	if p.is("[") {
		// The lexer makes "[" a bracket only directly after the name: T[U].
		p.next()
		a := &grammar.TypeApp{SourceInfo: grammar.SourceInfo{Pos: name.pos}, Name: name.text}
		for {
			a.Args = append(a.Args, p.typeExpr())
			if !p.accept(",") || p.is("]") {
				break
			}
		}
		p.expect("]")
		return a
	}
	return &grammar.TypeRef{SourceInfo: grammar.SourceInfo{Pos: name.pos}, Name: name.text}
}

// structFields parses the braces and fields of a struct type: { A T; B U }.
func (p *parser) structFields() (fields []*grammar.Field, closeBreak *grammar.LineBreak, oneLine bool) {
	open := p.i
	p.expect("{")
	for {
		fb := p.lineBreak()
		if p.is("}") {
			closeBreak = fb
			break
		}
		if p.tok().kind == tEOF {
			p.errorf("unterminated struct")
			panic(bail{})
		}
		restore := p.statement()
		fieldStart := p.i
		fname := p.ident()
		field := &grammar.Field{SourceInfo: grammar.SourceInfo{Pos: fname.pos}, Name: fname.text, Type: p.typeExpr(), Break: fb}
		p.source(field, fieldStart)
		fields = append(fields, field)
		restore()
		if !p.accept(";") && !p.is("}") && !p.tok().lineBefore {
			p.errorf("expected a semicolon or newline between struct fields")
			panic(bail{})
		}
		p.sourceTrailing(field.Source())
	}
	oneLine = !p.multiline(open, p.i)
	p.expect("}")
	return fields, closeBreak, oneLine
}

// tupleType finishes a tuple type whose "(" and first element, if any, were read as a group's.
// A trailing comma is optional, and required after a single element.
func (p *parser) tupleType(open int, g *grammar.TypeGroup, first grammar.TypeExpr) grammar.TypeExpr {
	t := &grammar.TupleType{OpenBreak: g.OpenBreak, BodyBreak: g.BodyBreak}
	if first != nil {
		t.Types = append(t.Types, first)
		for p.accept(",") && !p.is(")") {
			t.Types = append(t.Types, p.typeExpr())
		}
	}
	t.CloseBreak = p.lineBreak()
	t.OneLine = !p.multiline(open, p.i)
	p.expect(")")
	return t
}

func (p *parser) ruleDef() (sourceResult *grammar.RuleDef) {
	sourceStart := p.i
	defer func() {
		if sourceResult != nil {
			p.sourceNode(sourceResult, sourceStart)
		}
	}()

	br := p.lineBreak()
	pos := p.expect("rule").pos
	p.header, p.layout, p.layoutDepth, p.layoutStart = true, true, 0, p.i-1
	p.headerPos = pos
	defer func() { p.header, p.layout, p.layoutDepth = false, false, 0 }()
	name := p.ident()
	rd := &grammar.RuleDef{SourceInfo: grammar.SourceInfo{Pos: pos}, Name: name.text, Break: br}
	if !p.is("=") {
		rd.Type = p.typeExpr()
	}
	p.expect("=")
	p.header = false
	if !p.boundary() && p.is("operators") && p.peek(1).kind == tPunct && p.peek(1).text == "{" {
		p.next()
		// Items inside an operators block have their own separators.
		p.layout = false
		rd.Expr = p.operatorsRule()
		p.layout = true
		if !p.boundary() && p.is("=>") {
			if p.tok().lineBefore {
				rd.ActionBreak = p.lineBreak()
			}
			p.next()
			rd.Action, rd.DefaultAction = p.actionTerm()
		}
		return rd
	}
	if p.tok().lineBefore {
		rd.BodyBreak = p.lineBreak()
	}
	rd.Expr = p.bodyChoice()
	if !p.boundary() && p.is("=>") {
		if p.tok().lineBefore {
			rd.ActionBreak = p.lineBreak()
		}
		p.next()
		rd.Action, rd.DefaultAction = p.actionTerm()
	}
	return rd
}

func (p *parser) actionTerm() (grammar.Term, bool) {
	var term grammar.Term
	defaultAction := p.accept("...")
	if !defaultAction {
		if p.is("{") {
			term = p.actionBlock()
		} else {
			term = p.term()
		}
	}
	if !p.boundary() && p.is("=>") {
		p.errorf("actions do not chain; parenthesize an inline action")
		panic(bail{})
	}
	return term, defaultAction
}

func (p *parser) inlineChoice() (sourceResult grammar.Expr) {
	sourceStart := p.i
	defer func() {
		if sourceResult != nil {
			p.sourceNode(sourceResult, sourceStart)
		}
	}()

	e := p.choice()
	if !p.boundary() && p.is("=>") {
		pos := p.next().pos
		term, defaultAction := p.actionTerm()
		return &grammar.InlineAction{SourceInfo: grammar.SourceInfo{Pos: pos}, Expr: e, Action: term, DefaultAction: defaultAction}
	}
	return e
}

// bodyChoice parses the top-level choice of a rule body like choice, and
// records the line breaks between its alternatives and sequence items.
func (p *parser) bodyChoice() (sourceResult grammar.Expr) {
	sourceStart := p.i
	defer func() {
		if sourceResult != nil {
			p.sourceNode(sourceResult, sourceStart)
		}
	}()

	first := p.bodySequence()
	if !p.is("/") {
		return first
	}
	c := &grammar.Choice{Alts: []grammar.Expr{first}}
	breaks := []*grammar.LineBreak{nil}
	broken := false
	for !p.boundary() && p.is("/") {
		var b *grammar.LineBreak
		if p.tok().lineBefore || p.peek(1).lineBefore {
			b = p.lineBreak()
			broken = true
		}
		p.next()
		c.Alts = append(c.Alts, p.bodySequence())
		breaks = append(breaks, b)
	}
	if broken {
		c.Breaks = breaks
	}
	return c
}

// bodySequence parses a sequence like sequence, and records the line breaks
// between its items.
func (p *parser) bodySequence() (sourceResult grammar.Expr) {
	sourceStart := p.i
	defer func() {
		if sourceResult != nil {
			p.sourceNode(sourceResult, sourceStart)
		}
	}()

	var items []grammar.Expr
	var breaks []*grammar.LineBreak
	broken := false
	for p.startsPrimary() {
		var b *grammar.LineBreak
		if len(items) > 0 && p.tok().lineBefore {
			b = p.lineBreak()
			broken = true
		}
		items = append(items, p.prefixed())
		breaks = append(breaks, b)
	}
	switch len(items) {
	case 0:
		p.errorf("expected an expression, found %s", p.tok())
		panic(bail{})
	case 1:
		return items[0]
	}
	s := &grammar.Seq{Items: items}
	if broken {
		s.Breaks = breaks
	}
	return s
}

func (p *parser) operatorsRule() (sourceResult *grammar.OperatorsRule) {
	sourceStart := p.i - 1 // The caller consumed the operators keyword.
	defer func() {
		if sourceResult != nil {
			p.sourceNode(sourceResult, sourceStart)
		}
	}()

	pr := &grammar.OperatorsRule{SourceInfo: grammar.SourceInfo{Pos: p.expect("{").pos}}
	for {
		b := p.lineBreak()
		if p.accept("}") {
			pr.CloseBreak = b
			break
		}
		itemStart := p.i
		var itemSource *grammar.SourceInfo
		restore := p.statement()
		switch {
		case p.accept("skip"):
			pr.SkipSource.Pos = p.toks[p.i-1].pos
			if pr.Skip != nil {
				p.errorf("duplicate skip")
			}
			pr.Skip = p.choice()
			pr.SkipBreak = b
			p.source(&pr.SkipSource, itemStart)
			itemSource = &pr.SkipSource
		case p.accept("operand"):
			operandPos := p.toks[p.i-1].pos
			o := &grammar.OperatorsRuleOperand{SourceInfo: grammar.SourceInfo{Pos: operandPos}, Expr: p.choice(), Break: b}
			if p.accept("=>") {
				o.Action, o.DefaultAction = p.actionTerm()
			}
			pr.Operands = append(pr.Operands, o)
			p.source(o, itemStart)
			itemSource = o.Source()
		case p.accept("level"):
			p.header = true
			p.headerPos = p.toks[p.i-1].pos
			l := &grammar.OperatorsRuleLevel{SourceInfo: grammar.SourceInfo{Pos: p.headerPos}, Break: b}
			if p.tok().kind == tIdent {
				l.Name = p.ident().text
			}
			open := p.i
			p.expect("{")
			p.header = false
			p.layout, p.layoutDepth = false, 0
			for {
				ob := p.lineBreak()
				if p.is("}") {
					l.CloseBreak = ob
					l.OneLine = !p.multiline(open, p.i)
					p.next()
					break
				}
				op := p.operatorsRuleOperator()
				op.Break = ob
				l.Operators = append(l.Operators, op)
				p.itemEnd()
				p.sourceTrailing(op.Source())
			}
			pr.Levels = append(pr.Levels, l)
			p.source(l, itemStart)
			itemSource = l.Source()
		default:
			p.errorf("expected 'skip', 'operand', 'level', or '}', found %s", p.tok())
			panic(bail{})
		}
		restore()
		p.itemEnd()
		if itemSource != nil {
			p.sourceTrailing(itemSource)
		}
	}
	return pr
}

func (p *parser) itemEnd() {
	if p.accept(";") {
		for p.accept(";") {
		}
		return
	}
	if !p.is("}") && !p.tok().lineBefore {
		p.errorf("expected a semicolon or newline between operators items")
		panic(bail{})
	}
}

func (p *parser) operatorsRuleOperator() (sourceResult *grammar.OperatorsRuleOperator) {
	sourceStart := p.i
	defer func() {
		if sourceResult != nil {
			p.sourceNode(sourceResult, sourceStart)
		}
	}()

	defer p.statement()()
	t := p.tok()
	op := &grammar.OperatorsRuleOperator{SourceInfo: grammar.SourceInfo{Pos: t.pos}}
	switch {
	case p.accept("prefix"):
		op.Kind = grammar.Prefix
	case p.accept("postfix"):
		op.Kind = grammar.Postfix
	case p.accept("infix"):
		p.header = true
		p.headerPos = op.Pos
		op.Kind = grammar.Infix
		switch {
		case p.accept("left"):
			op.Assoc = grammar.AssocLeft
		case p.accept("right"):
			op.Assoc = grammar.AssocRight
		case p.accept("none"):
			op.Assoc = grammar.AssocNone
		default:
			p.errorf("expected 'left', 'right', or 'none', found %s", p.tok())
			panic(bail{})
		}
		p.header = false
	default:
		p.errorf("expected 'prefix', 'postfix', 'infix', or '}', found %s", t)
		panic(bail{})
	}
	op.Expr = p.choice()
	if p.accept("=>") {
		op.Action, op.DefaultAction = p.actionTerm()
	}
	return op
}

// --- Parser expressions ---

func (p *parser) choice() (sourceResult grammar.Expr) {
	sourceStart := p.i
	defer func() {
		if sourceResult != nil {
			p.sourceNode(sourceResult, sourceStart)
		}
	}()

	p.enter()
	defer p.leave()
	p.nest++
	defer func() { p.nest-- }()
	first := p.sequence()
	if !p.is("/") {
		return first
	}
	c := &grammar.Choice{Alts: []grammar.Expr{first}}
	for !p.boundary() && p.accept("/") {
		c.Alts = append(c.Alts, p.sequence())
	}
	return c
}

// startsPrimary reports whether the current token can start an element
// of a sequence.
func (p *parser) startsPrimary() bool {
	if p.boundary() {
		return false
	}
	t := p.tok()
	switch t.kind {
	case tIdent:
		return true
	case tString, tCharClass:
		return true
	case tPunct:
		switch t.text {
		case "(", "{", "&", "!", "-", "--", "^^", "^", "$$", "$", "_|_", ".", "[":
			// "[" is a character class written without a space before it, an error.
			return true
		case "#":
			return p.isCut()
		}
	}
	return false
}

func (p *parser) sequence() (sourceResult grammar.Expr) {
	sourceStart := p.i
	defer func() {
		if sourceResult != nil {
			p.sourceNode(sourceResult, sourceStart)
		}
	}()

	var items []grammar.Expr
	for p.startsPrimary() {
		items = append(items, p.prefixed())
	}
	switch len(items) {
	case 0:
		p.errorf("expected an expression, found %s", p.tok())
		panic(bail{})
	case 1:
		return items[0]
	}
	return &grammar.Seq{Items: items}
}

func (p *parser) prefixed() (sourceResult grammar.Expr) {
	sourceStart := p.i
	defer func() {
		if sourceResult != nil {
			p.sourceNode(sourceResult, sourceStart)
		}
	}()

	p.enter()
	defer p.leave()
	t := p.tok()
	if t.kind == tIdent && p.peek(1).kind == tPunct && p.peek(1).text == ":" && !p.peek(1).spaceBefore {
		p.ident()
		p.next()
		return &grammar.Capture{SourceInfo: grammar.SourceInfo{Pos: t.pos}, Name: t.text, Expr: p.prefixed()}
	}
	switch {
	case p.accept("&"):
		return &grammar.And{SourceInfo: grammar.SourceInfo{Pos: t.pos}, Expr: p.prefixed()}
	case p.accept("!"):
		return &grammar.Not{SourceInfo: grammar.SourceInfo{Pos: t.pos}, Expr: p.prefixed()}
	case p.is("-"):
		p.next()
		return &grammar.Discard{SourceInfo: grammar.SourceInfo{Pos: t.pos}, Expr: p.prefixed()}
	}
	return p.suffixed()
}

func (p *parser) suffixed() (sourceResult grammar.Expr) {
	sourceStart := p.i
	defer func() {
		if sourceResult != nil {
			p.sourceNode(sourceResult, sourceStart)
		}
	}()

	start := p.tok().pos
	e := p.primary()
	for !p.boundary() {
		switch {
		case p.accept("*"):
			e = &grammar.Repeat{SourceInfo: grammar.SourceInfo{Pos: start}, Expr: e, Min: 0, Max: -1}
		case p.accept("+"):
			e = &grammar.Repeat{SourceInfo: grammar.SourceInfo{Pos: start}, Expr: e, Min: 1, Max: -1}
		case p.accept("?"):
			e = &grammar.Optional{SourceInfo: grammar.SourceInfo{Pos: start}, Expr: e}
		case p.is("#") && !p.isCut():
			e = p.pragma(e, start)
		case p.is("@"):
			a, ok := e.(*grammar.Attributed)
			if !ok {
				a = &grammar.Attributed{Expr: e, Attrs: []*grammar.Attribute{}}
				e = a
			}
			for p.is("@") {
				var b *grammar.LineBreak
				if p.nest == 0 && p.tok().lineBefore {
					b = p.lineBreak()
				}
				if b != nil || a.Breaks != nil {
					p.appendAttributeBreak(a, b)
				}
				a.Attrs = append(a.Attrs, p.attribute())
			}
		default:
			return e
		}
		p.sourceNode(e, sourceStart)
	}
	return e
}

// appendAttributeBreak fills the absent breaks before a newly broken
// attribute. A long same-line attribute run can make this gap large.
func (p *parser) appendAttributeBreak(a *grammar.Attributed, b *grammar.LineBreak) {
	gap := len(a.Attrs) - len(a.Breaks)
	if p.ctx == nil {
		a.Breaks = append(a.Breaks, make([]*grammar.LineBreak, gap)...)
	} else {
		var blanks [256]*grammar.LineBreak
		for gap > 0 {
			n := min(gap, len(blanks))
			p.charge(uint(n))
			a.Breaks = append(a.Breaks, blanks[:n]...)
			gap -= n
		}
	}
	a.Breaks = append(a.Breaks, b)
}

// pragma dispatches built-in grammar syntax. Its payload is not an attribute
// argument list: each directive defines its own grammar and target.
func (p *parser) pragma(e grammar.Expr, start grammar.Pos) (sourceResult grammar.Expr) {
	sourceStart := p.i
	defer func() {
		if sourceResult != nil {
			p.sourceNode(sourceResult, sourceStart)
		}
	}()

	var br *grammar.LineBreak
	if p.nest == 0 && p.tok().lineBefore {
		br = p.lineBreak()
	}
	pos := p.expect("#").pos
	if p.tok().spaceBefore {
		p.errorf("pragma name must immediately follow '#'")
		panic(bail{})
	}
	name := p.ident().text
	if (name == "recover" || name == "repeat") && p.is("(") && p.tok().spaceBefore {
		p.errorf("pragma arguments must immediately follow '#%s'", name)
		panic(bail{})
	}
	switch name {
	case "atomic":
		if !p.boundary() && p.is("(") && !p.tok().spaceBefore {
			p.next()
			p.expect(")")
		}
		return &grammar.Atomic{SourceInfo: grammar.SourceInfo{Pos: pos}, Expr: e, Break: br}
	case "stream":
		if !p.boundary() && p.is("(") && !p.tok().spaceBefore {
			p.next()
			p.expect(")")
		}
		return &grammar.Stream{SourceInfo: grammar.SourceInfo{Pos: pos}, Expr: e, Break: br}
	case "missing":
		if !p.boundary() && p.is("(") && !p.tok().spaceBefore {
			p.errorf("#missing takes no arguments")
			panic(bail{})
		}
		result, ok := e.(*grammar.Attributed)
		if !ok {
			result = &grammar.Attributed{Expr: e}
		}
		if br != nil || result.Breaks != nil {
			p.appendAttributeBreak(result, br)
		}
		result.Attrs = append(result.Attrs, &grammar.Attribute{SourceInfo: grammar.SourceInfo{Pos: pos}, Name: name, Pragma: true})
		return result
	case "recover":
		p.expect("(")
		p.expect("skip")
		p.expect("=")
		skip := p.choice()
		p.expect(")")
		return &grammar.Recovery{SourceInfo: grammar.SourceInfo{Pos: pos}, Expr: e, Skip: skip, Break: br}
	case "repeat":
		p.expect("(")
		first := p.tok().kind == tInt || p.is("-")
		min := 0
		if first {
			min = p.repeatCount()
		}
		max := min
		if p.accept(",") {
			max = -1
			if !p.is(")") {
				max = p.repeatCount()
			} else if !first {
				p.errorf("#repeat requires at least one bound")
			}
		} else if !first {
			p.errorf("#repeat requires a count")
		}
		p.expect(")")
		return &grammar.Repeat{SourceInfo: grammar.SourceInfo{Pos: start}, Expr: e, Min: min, Max: max, PragmaBreak: br}
	default:
		p.errorf("unknown pragma #%s", name)
		panic(bail{})
	}
}

// isCut reports whether the current tokens are the cut #cut, which stands alone instead of
// applying to the preceding expression like other pragmas.
func (p *parser) isCut() bool {
	if !p.is("#") {
		return false
	}
	n := p.peek(1)
	return n.kind == tIdent && n.text == "cut" && !n.spaceBefore
}

func (p *parser) repeatCount() int {
	negative := p.accept("-")
	if p.tok().kind != tInt {
		p.errorf("expected integer repetition bound, found %s", p.tok())
		panic(bail{})
	}
	value := p.next().num
	if negative {
		value = -value
	}
	if value < 0 {
		p.errorf("repetition bound must be nonnegative")
		panic(bail{})
	}
	return value
}

func (p *parser) attribute() (sourceResult *grammar.Attribute) {
	sourceStart := p.i
	defer func() {
		if sourceResult != nil {
			p.sourceNode(sourceResult, sourceStart)
		}
	}()

	pos := p.expect("@").pos
	if p.tok().spaceBefore {
		p.errorf("attribute name must immediately follow '@'")
		panic(bail{})
	}
	a := &grammar.Attribute{SourceInfo: grammar.SourceInfo{Pos: pos}, Name: p.ident().text}
	if !p.is("(") || p.tok().spaceBefore {
		return a
	}
	p.next()
	named := false
	if p.accept(")") {
		return a
	}
	for {
		if p.tok().kind == tIdent && p.peek(1).kind == tPunct && p.peek(1).text == "=" {
			named = true
			name := p.ident().text
			p.expect("=")
			value := p.attributeValue()
			if a.KwArgs == nil {
				a.KwArgs = map[string]any{}
			}
			if _, exists := a.KwArgs[name]; exists {
				p.errorf("duplicate attribute argument %s", name)
			}
			a.KwArgs[name] = value
			a.KeywordOrder = append(a.KeywordOrder, name)
		} else {
			if named {
				p.errorf("positional attribute arguments must precede keyword arguments")
				panic(bail{})
			}
			var value any
			if !p.is(",") && !p.is(")") {
				value = p.attributeValue()
			}
			a.Args = append(a.Args, value)
		}
		if p.accept(")") {
			return a
		}
		p.expect(",")
		if named && p.accept(")") {
			return a
		}
	}
}

func (p *parser) attributeValue() any {
	if p.accept("nil") {
		return nil
	}
	if p.accept("true") {
		return true
	}
	if p.accept("false") {
		return false
	}
	if p.tok().kind == tInt {
		return p.next().num
	}
	if p.is("-") && p.peek(1).kind == tInt {
		p.next()
		return -p.next().num
	}
	e := p.choice()
	if literal, ok := e.(*grammar.Literal); ok {
		return literal.Value
	}
	return e
}

func (p *parser) primary() (sourceResult grammar.Expr) {
	sourceStart := p.i
	defer func() {
		if sourceResult != nil {
			p.sourceNode(sourceResult, sourceStart)
		}
	}()

	t := p.tok()
	switch t.kind {
	case tString:
		p.next()
		return &grammar.Literal{SourceInfo: grammar.SourceInfo{Pos: t.pos}, Value: t.text}
	case tCharClass:
		p.next()
		return t.cc
	case tIdent:

		p.next()
		if t.text == "_" {
			return &grammar.Top{SourceInfo: grammar.SourceInfo{Pos: t.pos}}
		}
		if t.text == "level" && !p.boundary() && p.is("(") && !p.tok().spaceBefore {
			p.next()
			target := p.ident()
			p.expect(",")
			level := p.ident()
			p.expect(")")
			return &grammar.Ref{SourceInfo: grammar.SourceInfo{Pos: target.pos}, Name: target.text, Level: level.text}
		}
		if !p.boundary() && p.is("(") && !p.tok().spaceBefore {
			p.errorf("level calls use level(rule, name); separate a rule and a group with whitespace")
			panic(bail{})
		}
		return &grammar.Ref{SourceInfo: grammar.SourceInfo{Pos: t.pos}, Name: t.text}
	case tPunct:
		switch t.text {
		case "(":
			open := p.i
			p.next()
			group := &grammar.Group{SourceInfo: grammar.SourceInfo{Pos: t.pos}, BodyBreak: p.lineBreak()}
			group.Expr = p.bodyChoice()
			if !p.boundary() && p.is("=>") {
				pos := p.next().pos
				action, defaultAction := p.actionTerm()
				group.Expr = &grammar.InlineAction{SourceInfo: grammar.SourceInfo{Pos: pos}, Expr: group.Expr, Action: action, DefaultAction: defaultAction}
				p.sourceNode(group.Expr, open+1)
			}
			group.CloseBreak = p.lineBreak()
			group.OneLine = !p.multiline(open, p.i)
			p.expect(")")
			return group
		case "{":
			block := p.actionBlock()
			var term grammar.Term = block
			if len(block.Statements) == 1 {
				term = block.Statements[0]
			}
			return &grammar.Predicate{SourceInfo: grammar.SourceInfo{Pos: t.pos}, Term: term, Breaks: block.Breaks, CloseBreak: block.CloseBreak, OneLine: block.OneLine}
		case "#":
			if p.isCut() {
				p.next()
				p.next()
				if p.is("(") && !p.tok().spaceBefore {
					p.errorf("#cut takes no arguments")
					panic(bail{})
				}
				return &grammar.Cut{SourceInfo: grammar.SourceInfo{Pos: t.pos}}
			}
		case "--":
			p.errorf("the cut is written #cut; run pego fix to rewrite --")
			panic(bail{})
		case "^^":
			p.next()
			return &grammar.BeginInput{SourceInfo: grammar.SourceInfo{Pos: t.pos}}
		case "^":
			p.next()
			return &grammar.BeginLine{SourceInfo: grammar.SourceInfo{Pos: t.pos}}
		case "$$":
			p.next()
			return &grammar.EndInput{SourceInfo: grammar.SourceInfo{Pos: t.pos}}
		case "$":
			p.next()
			return &grammar.EndLine{SourceInfo: grammar.SourceInfo{Pos: t.pos}}
		case "_|_":
			p.next()
			return &grammar.Bottom{SourceInfo: grammar.SourceInfo{Pos: t.pos}}
		case ".":
			p.next()
			return &grammar.Any{SourceInfo: grammar.SourceInfo{Pos: t.pos}}
		case "[":
			p.errorf("a character class directly after a name, a capture, ')' or ']' is read as an index; write a space before it")
			panic(bail{})
		}
	}
	p.errorf("expected an expression, found %s", t)
	panic(bail{})
}

// actionBlock preserves future action statements without executing them.
func (p *parser) actionBlock() (sourceResult *grammar.Block) {
	sourceStart := p.i
	defer func() {
		if sourceResult != nil {
			p.sourceNode(sourceResult, sourceStart)
		}
	}()

	p.enter()
	defer p.leave()
	open := p.i
	outerBreak := p.cur
	defer func() { p.cur = outerBreak }()
	outerLayout, outerDepth, outerStart := p.layout, p.layoutDepth, p.layoutStart
	defer func() { p.layout, p.layoutDepth, p.layoutStart = outerLayout, outerDepth, outerStart }()
	p.layout = false
	block := &grammar.Block{SourceInfo: grammar.SourceInfo{Pos: p.expect("{").pos}, Statements: []grammar.Term{}}
	for {
		br := p.lineBreak()
		if p.is("}") {
			block.CloseBreak = br
			block.OneLine = !p.multiline(open, p.i)
			p.next()
			return block
		}
		restore := p.statement()
		statementStart := p.i
		var term grammar.Term
		if p.tok().kind == tIdent && p.peek(1).text == ":=" {
			name := p.ident()
			p.expect(":=")
			term = &grammar.Assign{SourceInfo: grammar.SourceInfo{Pos: name.pos}, Name: name.text, Value: p.term()}
		} else {
			term = p.term()
		}
		p.sourceNode(term, statementStart)
		restore()
		block.Statements = append(block.Statements, term)
		block.Breaks = append(block.Breaks, br)
		if !p.accept(";") && !p.is("}") && !p.tok().lineBefore {
			p.errorf("expected a semicolon or newline between action statements")
			panic(bail{})
		}
		p.sourceTrailing(term.(grammar.SourceNode).Source())
	}
}

// --- Value expressions ---

func (p *parser) term() (sourceResult grammar.Term) {
	sourceStart := p.i
	defer func() {
		if sourceResult != nil {
			p.sourceNode(sourceResult, sourceStart)
		}
	}()

	p.enter()
	defer p.leave()
	if l := p.tryLambda(); l != nil {
		return l
	}
	return p.binary(0)
}

// tryLambda parses a lambda if the input has the form (a, b) => body.
func (p *parser) tryLambda() (sourceResult grammar.Term) {
	sourceStart := p.i
	var layout *grammar.SourceLayout
	defer func() {
		if sourceResult != nil {
			p.sourceNode(sourceResult, sourceStart)
			if layout != nil {
				sourceResult.(grammar.SourceNode).Source().Layout = layout
			}
		}
	}()

	if !p.is("(") {
		return nil
	}
	k := 1
	var params []string
	for {
		t := p.peek(k)
		if t.kind == tPunct && t.text == ")" && len(params) == 0 {
			break
		}
		if t.kind != tIdent {
			return nil
		}
		params = append(params, t.text)
		k++
		if s := p.peek(k); s.kind == tPunct && s.text == "," {
			k++
			continue
		}
		break
	}
	if t := p.peek(k); t.kind != tPunct || t.text != ")" {
		return nil
	}
	if t := p.peek(k + 1); t.kind != tPunct || t.text != "=>" {
		return nil
	}
	layout = p.takeTermLayout()
	for range k + 2 {
		if t := p.tok(); p.legacy == nil && t.kind == tIdent && (t.text == "true" || t.text == "false" || t.text == "nil") {
			p.errs.addRange(t.pos, t.end, fmt.Sprintf("%s cannot name a lambda parameter", t.text))
		}
		p.next()
	}
	outer := len(p.params)
	p.params = append(p.params, params...)
	defer func() { p.params = p.params[:outer] }()
	return &grammar.Lambda{Params: params, Body: p.term()}
}

var binaryLevels = [][]string{
	{"||"},
	{"&&"},
	{"==", "!=", "<", "<=", ">", ">="},
	{"+", "-"},
	{"*", "/", "%"},
}

func (p *parser) binary(level int) (sourceResult grammar.Term) {
	sourceStart := p.i
	defer func() {
		if sourceResult != nil {
			p.sourceNode(sourceResult, sourceStart)
		}
	}()

	if level == len(binaryLevels) {
		return p.unary()
	}
	l := p.binary(level + 1)
	for {
		t := p.tok()
		if p.boundary() || t.kind != tPunct || !contains(binaryLevels[level], t.text) {
			return l
		}
		p.next()
		r := p.binary(level + 1)
		l = &grammar.Binary{SourceInfo: grammar.SourceInfo{Pos: t.pos}, Op: t.text, L: l, R: r}
		p.sourceNode(l, sourceStart)
		if level == 2 {
			// Comparison operators do not chain.
			return l
		}
	}
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if x == y {
			return true
		}
	}
	return false
}

func (p *parser) unary() (sourceResult grammar.Term) {
	sourceStart := p.i
	layout := p.takeTermLayout()
	defer func() {
		if sourceResult != nil {
			p.sourceNode(sourceResult, sourceStart)
			if layout != nil {
				sourceResult.(grammar.SourceNode).Source().Layout = layout
			}
		}
	}()

	p.enter()
	defer p.leave()
	t := p.tok()
	if p.accept("-") || p.accept("!") {
		return &grammar.Unary{SourceInfo: grammar.SourceInfo{Pos: t.pos}, Op: t.text, X: p.unary()}
	}
	x := p.termPrimary()
	for !p.boundary() && (p.is(".") || p.is("[")) {
		if t := p.next(); t.text == "[" {
			// The lexer reads "[" as a bracket only directly after the indexed value.
			i := p.term()
			p.expect("]")
			x = &grammar.Binary{SourceInfo: grammar.SourceInfo{Pos: t.pos}, Op: "[]", L: x, R: i}
			p.sourceNode(x, sourceStart)
			continue
		}
		if t := p.tok(); t.text == "(" && t.kind == tPunct && !p.boundary() {
			// x.(T) asserts that x is a T.
			p.next()
			typ := p.typeExpr()
			p.expect(")")
			x = &grammar.Cast{SourceInfo: grammar.SourceInfo{Pos: t.pos}, X: x, Type: typ}
			p.sourceNode(x, sourceStart)
			continue
		}
		if t := p.tok(); t.kind == tCapture && !p.boundary() {
			// x.$y reads the capture y of the node x.
			p.next()
			x = &grammar.Member{SourceInfo: grammar.SourceInfo{Pos: t.pos}, X: x, Name: "$" + t.text}
			p.sourceNode(x, sourceStart)
			continue
		}
		name := p.ident()
		if p.legacy == nil && captureMember(name.text) {
			p.errs.addRange(name.pos, name.end, fmt.Sprintf("a capture is read with .$%s; run pego fix to rewrite .%s", name.text, name.text))
		}
		x = &grammar.Member{SourceInfo: grammar.SourceInfo{Pos: name.pos}, X: x, Name: name.text}
		p.sourceNode(x, sourceStart)
	}
	return x
}

// captureMember reports whether x.name read the capture name in earlier syntax: field names
// start with an uppercase letter, and startPos, endPos, children, expected and message are
// members of every node or of Missing.
func captureMember(name string) bool {
	switch name {
	case "startPos", "endPos", "children", "expected", "message", "":
		return false
	}
	r, _ := utf8.DecodeRuneInString(name)
	return r != '$' && !unicode.IsUpper(r) // x.$y is parsed as the member $y
}

func (p *parser) termPrimary() (sourceResult grammar.Term) {
	sourceStart := p.i
	defer func() {
		if sourceResult != nil {
			p.sourceNode(sourceResult, sourceStart)
		}
	}()

	t := p.tok()
	switch t.kind {
	case tInt:
		p.next()
		return &grammar.IntLit{Value: t.num}
	case tString:
		p.next()
		return &grammar.StringLit{Value: t.text}
	case tCapture:
		p.next()
		if p.legacy == nil && slices.Contains(p.params, t.text) {
			p.errs.addRange(t.pos, t.end, fmt.Sprintf("lambda parameters are written without $: write %s; run pego fix to rewrite $%s", t.text, t.text))
		}
		return &grammar.CaptureRef{SourceInfo: grammar.SourceInfo{Pos: t.pos}, Name: t.text}
	case tIndex:
		p.next()
		if p.legacy == nil {
			p.errs.addRange(t.pos, t.end, fmt.Sprintf("positional captures are removed: label the element (x:...) and write $x; run pego fix to rewrite $%d", t.num))
		}
		return &grammar.IndexRef{SourceInfo: grammar.SourceInfo{Pos: t.pos}, Index: t.num}
	case tIdent:
		switch t.text {
		case "true", "false":
			p.next()
			return &grammar.BoolLit{Value: t.text == "true"}
		case "nil":
			p.next()
			return &grammar.NilLit{}
		}
		if (!p.layout || p.layoutDepth > 0 || !p.peek(1).lineBefore) && p.peek(1).kind == tPunct && p.peek(1).text == "{" {
			return p.newTerm()
		}
		p.next()
		if !p.boundary() && p.is("(") && !p.tok().spaceBefore {
			p.next()
			call := &grammar.Call{SourceInfo: grammar.SourceInfo{Pos: t.pos}, Func: t.text, Args: []grammar.Term{}}
			for !p.accept(")") {
				call.Args = append(call.Args, p.term())
				if !p.is(")") {
					p.expect(",")
				}
			}
			return call
		}
		if slices.Contains(p.params, t.text) {
			// A name in a call or a constructor is a function or a type, never a parameter.
			return &grammar.CaptureRef{SourceInfo: grammar.SourceInfo{Pos: t.pos}, Name: t.text, Param: true}
		}
		return &grammar.VarRef{SourceInfo: grammar.SourceInfo{Pos: t.pos}, Name: t.text}
	case tPunct:
		if t.text == "[]" {
			p.next()
			n := &grammar.ListConstructor{Elem: p.typeTerm()}
			p.expect("{")
			n.Items = p.constructorArgs()
			return n
		}
		if t.text == "(" {
			open := p.i
			p.next()
			outer := p.cur
			defer func() { p.cur = outer }()
			g := &grammar.TermGroup{OpenBreak: &grammar.LineBreak{}}
			p.cur = g.OpenBreak
			g.BodyBreak = p.lineBreak()
			if p.is(")") {
				return p.tupleLit(open, &grammar.TupleLit{OpenBreak: g.OpenBreak, BodyBreak: g.BodyBreak}) // the unit ()
			}
			g.Term = p.term()
			if p.is(",") {
				return p.tupleLit(open, &grammar.TupleLit{OpenBreak: g.OpenBreak, BodyBreak: g.BodyBreak, Elems: []grammar.Term{g.Term}})
			}
			g.CloseBreak = p.lineBreak()
			g.OneLine = !p.multiline(open, p.i)
			p.expect(")")
			return g
		}
	}
	p.errorf("expected a value, found %s", t)
	panic(bail{})
}

// tupleLit finishes a tuple value whose "(" and first element, if any, were read as a group's.
// A trailing comma is optional, and required after a single element.
func (p *parser) tupleLit(open int, t *grammar.TupleLit) grammar.Term {
	if len(t.Elems) > 0 {
		for p.accept(",") && !p.is(")") {
			t.Elems = append(t.Elems, p.term())
		}
	} else {
		t.Elems = []grammar.Term{}
	}
	t.CloseBreak = p.lineBreak()
	t.OneLine = !p.multiline(open, p.i)
	p.expect(")")
	return t
}

func (p *parser) newTerm() (sourceResult grammar.Term) {
	sourceStart := p.i
	defer func() {
		if sourceResult != nil {
			p.sourceNode(sourceResult, sourceStart)
		}
	}()

	nameToken := p.ident()
	p.expect("{")
	if !p.is("}") && !(p.tok().kind == tIdent && p.peek(1).text == ":") {
		args := p.constructorArgs()
		n := &grammar.New{SourceInfo: grammar.SourceInfo{Pos: nameToken.pos}, Type: nameToken.text}
		if p.legacy == nil {
			// A positional constructor converts its value to a named node type.
			n.Args = args
			return n
		}
		// Every argument is kept, as a Text field, so that Fix still rewrites the terms inside
		// them; only a constructor of one argument is a terminal constructor that Fix rewrites, and
		// the compiler reports any other (pego fix leaves it as it is).
		for _, arg := range args {
			n.Fields = append(n.Fields, &grammar.FieldInit{Name: "Text", Value: arg})
		}
		if p.legacy != nil && len(args) == 1 {
			p.legacy.positional[n] = true
		}
		return n
	}
	n := &grammar.New{SourceInfo: grammar.SourceInfo{Pos: nameToken.pos}, Type: nameToken.text}
	for !p.accept("}") {
		fieldStart := p.i
		layout := p.takeTermLayout()
		name := p.ident()
		p.expect(":")
		field := &grammar.FieldInit{SourceInfo: grammar.SourceInfo{Pos: name.pos}, Name: name.text, Value: p.term()}
		p.source(field, fieldStart)
		field.Layout = layout
		n.Fields = append(n.Fields, field)
		if !p.is("}") {
			p.expect(",")
		}
		p.sourceTrailing(field.Source())
	}
	return n
}

// constructorArgs reads positional expressions through the closing brace.
func (p *parser) constructorArgs() []grammar.Term {
	args := []grammar.Term{}
	for !p.accept("}") {
		args = append(args, p.term())
		if !p.is("}") {
			p.expect(",")
		}
	}
	return args
}

// ParseLegacy parses src like Parse but accepts the removed syntax that pego fix rewrites, with
// the meaning Fix gives it. Grammar ASTs decoded from JSON may still hold some of these forms,
// such as positional captures, which the engine reports; ParseLegacy builds such ASTs from source.
func ParseLegacy(src string) (*grammar.Grammar, error) {
	g, _, err := parseLegacy(src)
	return g, err
}
