package syntax

import (
	"fmt"
	"strings"

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

func (p *parser) tok() token { return p.toks[p.i] }
func (p *parser) peek(k int) token {
	if p.i+k < len(p.toks) {
		return p.toks[p.i+k]
	}
	return p.toks[len(p.toks)-1]
}
func (p *parser) next() token {
	t := p.toks[p.i]
	if p.header {
		if t.lineComment != nil {
			t.lineComment.Header = p.headerPos
			p.headerComments = append(p.headerComments, t.lineComment)
		}
		for _, comment := range t.comments {
			comment.Header = p.headerPos
		}
		p.headerComments = append(p.headerComments, t.comments...)
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
		p.cur.Comments = append(p.cur.Comments, t.comments...)
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
			b.Comments = append([]*grammar.Comment{c}, b.Comments...)
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
		g.PackageBreak = p.lineBreak()
		restore := p.statement()
		g.PackagePos = p.next().pos
		p.header = true
		p.headerPos = g.PackagePos
		func() {
			defer func() { p.header = false }()
			defer p.recover() // a missing name skips to the first definition
			g.Package = p.ident().text
		}()
		restore()
		p.declarationEnd()
	}
	for p.tok().kind != tEOF {
		start := p.i
		p.recoveryStart = start
		func() {
			defer p.recover()
			switch {
			case p.is("type"):
				g.Statements = append(g.Statements, p.typeDef())
			case p.is("rule"):
				g.Statements = append(g.Statements, p.ruleDef())
			default:
				p.errorf("expected 'rule' or 'type', found %s", p.tok())
				panic(bail{})
			}
		}()
		if p.i == start {
			p.next()
		}
		p.declarationEnd()
	}
	g.EndBreak = p.lineBreak()
	g.HeaderComments = p.headerComments
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

func (p *parser) typeDef() *grammar.TypeDef {
	defer p.statement()()
	br := p.lineBreak()
	pos := p.expect("type").pos
	p.header = true
	p.headerPos = pos
	defer func() { p.header = false }()
	name := p.ident()
	td := &grammar.TypeDef{Pos: pos, Name: name.text, Break: br}
	switch {
	case p.accept("struct"):
		open := p.i
		p.expect("{")
		p.header = false
		p.layout, p.layoutDepth = false, 0
		st := &grammar.StructSpec{}
		for {
			fb := p.lineBreak()
			if p.is("}") {
				st.CloseBreak = fb
				break
			}
			if p.tok().kind == tEOF {
				p.errorf("unterminated struct")
				panic(bail{})
			}
			restore := p.statement()
			fname := p.ident()
			st.Fields = append(st.Fields, &grammar.Field{Pos: fname.pos, Name: fname.text, Type: p.typeExpr(), Break: fb})
			restore()
			if !p.accept(",") && !p.is("}") && !p.tok().lineBefore {
				p.errorf("expected a comma or newline between struct fields")
				panic(bail{})
			}
		}
		st.OneLine = !p.multiline(open, p.i)
		p.expect("}")
		td.Spec = st
	case p.accept("terminal"):
		p.header = false
		td.Spec = &grammar.TerminalSpec{}
	case p.accept("="):
		p.header = false
		td.Spec = &grammar.AliasSpec{Type: p.typeExpr()}
	default:
		p.errorf("expected 'struct', 'terminal', or '=', found %s", p.tok())
		panic(bail{})
	}
	return td
}

func (p *parser) typeExpr() grammar.TypeExpr {
	t := p.typeTerm()
	if !p.is("|") {
		return t
	}
	u := &grammar.UnionType{Types: []grammar.TypeExpr{t}}
	for p.accept("|") {
		u.Types = append(u.Types, p.typeTerm())
	}
	return u
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

func (p *parser) typeTerm() grammar.TypeExpr {
	p.enter()
	defer p.leave()
	switch {
	case p.accept("[]"):
		return &grammar.ListType{Elem: p.typeTerm()}
	case p.accept("*"):
		return &grammar.OptionalType{Elem: p.typeTerm()}
	case p.accept("("):
		t := p.typeExpr()
		p.expect(")")
		return t
	}
	name := p.ident()
	return &grammar.TypeRef{Pos: name.pos, Name: name.text}
}

func (p *parser) ruleDef() *grammar.RuleDef {
	br := p.lineBreak()
	pos := p.expect("rule").pos
	p.header, p.layout, p.layoutDepth, p.layoutStart = true, true, 0, p.i-1
	p.headerPos = pos
	defer func() { p.header, p.layout, p.layoutDepth = false, false, 0 }()
	name := p.ident()
	rd := &grammar.RuleDef{Pos: pos, Name: name.text, Break: br}
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

func (p *parser) inlineChoice() grammar.Expr {
	e := p.choice()
	if !p.boundary() && p.is("=>") {
		pos := p.next().pos
		term, defaultAction := p.actionTerm()
		return &grammar.InlineAction{Pos: pos, Expr: e, Action: term, DefaultAction: defaultAction}
	}
	return e
}

// bodyChoice parses the top-level choice of a rule body like choice, and
// records the line breaks between its alternatives and sequence items.
func (p *parser) bodyChoice() grammar.Expr {
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
func (p *parser) bodySequence() grammar.Expr {
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

func (p *parser) operatorsRule() *grammar.OperatorsRule {
	pr := &grammar.OperatorsRule{Pos: p.expect("{").pos}
	for {
		b := p.lineBreak()
		if p.accept("}") {
			pr.CloseBreak = b
			break
		}
		restore := p.statement()
		switch {
		case p.accept("skip"):
			pr.SkipPos = p.toks[p.i-1].pos
			if pr.Skip != nil {
				p.errorf("duplicate skip")
			}
			pr.Skip = p.choice()
			pr.SkipBreak = b
		case p.accept("operand"):
			operandPos := p.toks[p.i-1].pos
			o := &grammar.OperatorsRuleOperand{Pos: operandPos, Expr: p.choice(), Break: b}
			if p.accept("=>") {
				o.Action, o.DefaultAction = p.actionTerm()
			}
			pr.Operands = append(pr.Operands, o)
		case p.accept("level"):
			p.header = true
			p.headerPos = p.toks[p.i-1].pos
			l := &grammar.OperatorsRuleLevel{Pos: p.headerPos, Break: b}
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
			}
			pr.Levels = append(pr.Levels, l)
		default:
			p.errorf("expected 'skip', 'operand', 'level', or '}', found %s", p.tok())
			panic(bail{})
		}
		restore()
		p.itemEnd()
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

func (p *parser) operatorsRuleOperator() *grammar.OperatorsRuleOperator {
	defer p.statement()()
	t := p.tok()
	op := &grammar.OperatorsRuleOperator{Pos: t.pos}
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

func (p *parser) choice() grammar.Expr {
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
		case "(", "{", "&", "!", "-", "--", "^^", "^", "$$", "$", "_|_", ".":
			return true
		}
	}
	return false
}

func (p *parser) sequence() grammar.Expr {
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

func (p *parser) prefixed() grammar.Expr {
	p.enter()
	defer p.leave()
	t := p.tok()
	if t.kind == tIdent && p.peek(1).kind == tPunct && p.peek(1).text == ":" && !p.peek(1).spaceBefore {
		p.ident()
		p.next()
		return &grammar.Capture{Pos: t.pos, Name: t.text, Expr: p.prefixed()}
	}
	switch {
	case p.accept("&"):
		return &grammar.And{Pos: t.pos, Expr: p.prefixed()}
	case p.accept("!"):
		return &grammar.Not{Pos: t.pos, Expr: p.prefixed()}
	case p.is("-"):
		p.next()
		return &grammar.Discard{Pos: t.pos, Expr: p.prefixed()}
	}
	return p.suffixed()
}

func (p *parser) suffixed() grammar.Expr {
	start := p.tok().pos
	e := p.primary()
	for !p.boundary() {
		switch {
		case p.accept("*"):
			e = &grammar.Repeat{Pos: start, Expr: e, Min: 0, Max: -1}
		case p.accept("+"):
			e = &grammar.Repeat{Pos: start, Expr: e, Min: 1, Max: -1}
		case p.accept("?"):
			e = &grammar.Optional{Pos: start, Expr: e}
		case p.is("#"):
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
					a.Breaks = append(a.Breaks, make([]*grammar.LineBreak, len(a.Attrs)-len(a.Breaks))...)
					a.Breaks = append(a.Breaks, b)
				}
				a.Attrs = append(a.Attrs, p.attribute())
			}
		default:
			return e
		}
	}
	return e
}

// pragma dispatches built-in grammar syntax. Its payload is not an attribute
// argument list: each directive defines its own grammar and target.
func (p *parser) pragma(e grammar.Expr, start grammar.Pos) grammar.Expr {
	var br *grammar.LineBreak
	if p.nest == 0 && p.tok().lineBefore {
		br = p.lineBreak()
	}
	pos := p.expect("#").pos
	name := p.ident().text
	switch name {
	case "atomic":
		if !p.boundary() && p.is("(") && !p.tok().spaceBefore {
			p.next()
			p.expect(")")
		}
		return &grammar.Atomic{Pos: pos, Expr: e, Break: br}
	case "stream":
		if !p.boundary() && p.is("(") && !p.tok().spaceBefore {
			p.next()
			p.expect(")")
		}
		return &grammar.Stream{Pos: pos, Expr: e, Break: br}
	case "recover":
		p.expect("(")
		p.expect("skip")
		p.expect("=")
		skip := p.choice()
		p.expect(")")
		return &grammar.Recovery{Pos: pos, Expr: e, Skip: skip, Break: br}
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
		return &grammar.Repeat{Pos: start, Expr: e, Min: min, Max: max, PragmaBreak: br}
	default:
		p.errorf("unknown pragma #%s", name)
		panic(bail{})
	}
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

func (p *parser) attribute() *grammar.Attribute {
	pos := p.expect("@").pos
	if p.tok().spaceBefore {
		p.errorf("attribute name must immediately follow '@'")
		panic(bail{})
	}
	a := &grammar.Attribute{Pos: pos, Name: p.ident().text}
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

func (p *parser) primary() grammar.Expr {
	t := p.tok()
	switch t.kind {
	case tString:
		p.next()
		return &grammar.Literal{Pos: t.pos, Value: t.text}
	case tCharClass:
		p.next()
		return t.cc
	case tIdent:

		p.next()
		if t.text == "_" {
			return &grammar.Top{Pos: t.pos}
		}
		if t.text == "level" && !p.boundary() && p.is("(") && !p.tok().spaceBefore {
			p.next()
			target := p.ident()
			p.expect(",")
			level := p.ident()
			p.expect(")")
			return &grammar.Ref{Pos: target.pos, Name: target.text, Level: level.text}
		}
		if !p.boundary() && p.is("(") && !p.tok().spaceBefore {
			p.errorf("level calls use level(rule, name); separate a rule and a group with whitespace")
			panic(bail{})
		}
		return &grammar.Ref{Pos: t.pos, Name: t.text}
	case tPunct:
		switch t.text {
		case "(":
			open := p.i
			p.next()
			group := &grammar.Group{Pos: t.pos, BodyBreak: p.lineBreak()}
			group.Expr = p.bodyChoice()
			if !p.boundary() && p.is("=>") {
				pos := p.next().pos
				action, defaultAction := p.actionTerm()
				group.Expr = &grammar.InlineAction{Pos: pos, Expr: group.Expr, Action: action, DefaultAction: defaultAction}
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
			return &grammar.Predicate{Pos: t.pos, Term: term, Breaks: block.Breaks, CloseBreak: block.CloseBreak, OneLine: block.OneLine}
		case "--":
			p.next()
			return &grammar.Cut{Pos: t.pos}
		case "^^":
			p.next()
			return &grammar.BeginInput{Pos: t.pos}
		case "^":
			p.next()
			return &grammar.BeginLine{Pos: t.pos}
		case "$$":
			p.next()
			return &grammar.EndInput{Pos: t.pos}
		case "$":
			p.next()
			return &grammar.EndLine{Pos: t.pos}
		case "_|_":
			p.next()
			return &grammar.Bottom{Pos: t.pos}
		case ".":
			p.next()
			return &grammar.Any{Pos: t.pos}
		}
	}
	p.errorf("expected an expression, found %s", t)
	panic(bail{})
}

// actionBlock preserves future action statements without executing them.
func (p *parser) actionBlock() *grammar.Block {
	p.enter()
	defer p.leave()
	open := p.i
	outerBreak := p.cur
	defer func() { p.cur = outerBreak }()
	outerLayout, outerDepth, outerStart := p.layout, p.layoutDepth, p.layoutStart
	defer func() { p.layout, p.layoutDepth, p.layoutStart = outerLayout, outerDepth, outerStart }()
	p.layout = false
	block := &grammar.Block{Pos: p.expect("{").pos, Statements: []grammar.Term{}}
	for {
		br := p.lineBreak()
		if p.is("}") {
			block.CloseBreak = br
			block.OneLine = !p.multiline(open, p.i)
			p.next()
			return block
		}
		restore := p.statement()
		var term grammar.Term
		if p.tok().kind == tIdent && p.peek(1).text == ":=" {
			name := p.ident()
			p.expect(":=")
			term = &grammar.Assign{Pos: name.pos, Name: name.text, Value: p.term()}
		} else {
			term = p.term()
		}
		restore()
		block.Statements = append(block.Statements, term)
		block.Breaks = append(block.Breaks, br)
		if !p.accept(";") && !p.is("}") && !p.tok().lineBefore {
			p.errorf("expected a semicolon or newline between action statements")
			panic(bail{})
		}
	}
}

// --- Value expressions ---

func (p *parser) term() grammar.Term {
	p.enter()
	defer p.leave()
	if l := p.tryLambda(); l != nil {
		return l
	}
	return p.binary(0)
}

// tryLambda parses a lambda if the input has the form (a, b) => body.
func (p *parser) tryLambda() grammar.Term {
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
	for range k + 2 {
		p.next()
	}
	return &grammar.Lambda{Params: params, Body: p.term()}
}

var binaryLevels = [][]string{
	{"||"},
	{"&&"},
	{"==", "!=", "<", "<=", ">", ">="},
	{"+", "-"},
	{"*", "/", "%"},
}

func (p *parser) binary(level int) grammar.Term {
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
		l = &grammar.Binary{Pos: t.pos, Op: t.text, L: l, R: r}
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

func (p *parser) unary() grammar.Term {
	p.enter()
	defer p.leave()
	t := p.tok()
	if p.accept("-") || p.accept("!") {
		return &grammar.Unary{Pos: t.pos, Op: t.text, X: p.unary()}
	}
	x := p.termPrimary()
	for !p.boundary() && p.is(".") {
		p.next()
		name := p.ident()
		x = &grammar.Member{Pos: name.pos, X: x, Name: name.text}
	}
	return x
}

func (p *parser) termPrimary() grammar.Term {
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
		return &grammar.CaptureRef{Pos: t.pos, Name: t.text}
	case tIndex:
		p.next()
		return &grammar.IndexRef{Pos: t.pos, Index: t.num}
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
			call := &grammar.Call{Pos: t.pos, Func: t.text, Args: []grammar.Term{}}
			for !p.accept(")") {
				call.Args = append(call.Args, p.term())
				if !p.is(")") {
					p.expect(",")
				}
			}
			return call
		}
		return &grammar.VarRef{Pos: t.pos, Name: t.text}
	case tPunct:
		if t.text == "(" {
			p.next()
			x := p.term()
			p.expect(")")
			return x
		}
	}
	p.errorf("expected a value, found %s", t)
	panic(bail{})
}

func (p *parser) newTerm() grammar.Term {
	nameToken := p.ident()
	n := &grammar.New{Pos: nameToken.pos, Type: nameToken.text}
	p.expect("{")
	for !p.accept("}") {
		name := p.ident()
		p.expect(":")
		n.Fields = append(n.Fields, &grammar.FieldInit{Pos: name.pos, Name: name.text, Value: p.term()})
		if !p.is("}") {
			p.expect(",")
		}
	}
	return n
}
