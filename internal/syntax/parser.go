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
func ParseExpr(src string) (grammar.Expr, error) {
	errs := &errorList{}
	p := &parser{toks: lex(src, errs), errs: errs}
	e := p.choice()
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
	p.takeComments(p.i)
	if p.i < len(p.toks)-1 {
		p.i++
	}
	return t
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
	if p.tok().kind != tIdent {
		p.errorf("expected identifier, found %s", p.tok())
		panic(bail{})
	}
	return p.next()
}

var keywords = map[string]bool{
	"package": true, "type": true, "def": true, "struct": true, "terminal": true,
	"pratt": true, "skip": true, "operand": true, "level": true, "prefix": true,
	"postfix": true, "infix": true, "new": true,
}

// stops holds the identifiers that end a sequence of expressions
// because they start the next definition or item.
var stops = map[string]bool{
	"package": true, "type": true, "def": true, "skip": true, "operand": true,
	"level": true, "prefix": true, "postfix": true, "infix": true,
}

func (p *parser) file() *grammar.Grammar {
	g := &grammar.Grammar{}
	if p.is("package") {
		g.PackageBreak = p.lineBreak()
		p.next()
		func() {
			defer p.recover() // a missing name skips to the first definition
			if p.is("def") || p.is("type") {
				p.errorf("expected identifier, found %s", p.tok())
				panic(bail{})
			}
			g.Package = p.ident().text
		}()
	}
	for p.tok().kind != tEOF {
		start := p.i
		func() {
			defer p.recover()
			switch {
			case p.is("type"):
				g.Statements = append(g.Statements, p.typeDef())
			case p.is("def"):
				g.Statements = append(g.Statements, p.ruleDef())
			default:
				p.errorf("expected 'def' or 'type', found %s", p.tok())
				panic(bail{})
			}
		}()
		if p.i == start {
			p.next()
		}
	}
	g.EndBreak = p.lineBreak()
	return g
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
	for p.tok().kind != tEOF && !p.is("def") && !p.is("type") {
		p.next()
	}
}

func (p *parser) typeDef() *grammar.TypeDef {
	br := p.lineBreak()
	pos := p.expect("type").pos
	name := p.ident()
	td := &grammar.TypeDef{Pos: pos, Name: name.text, Break: br}
	switch {
	case p.accept("struct"):
		open := p.i
		p.expect("{")
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
			fname := p.ident()
			st.Fields = append(st.Fields, &grammar.Field{Pos: fname.pos, Name: fname.text, Type: p.typeExpr(), Break: fb})
			p.accept(",")
		}
		st.OneLine = !p.multiline(open, p.i)
		p.expect("}")
		td.Spec = st
	case p.accept("terminal"):
		td.Spec = &grammar.TerminalSpec{}
	case p.accept("="):
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
	pos := p.expect("def").pos
	name := p.ident()
	rd := &grammar.RuleDef{Pos: pos, Name: name.text, Break: br}
	if p.accept(":") {
		rd.Type = p.typeExpr()
	}
	p.expect("=")
	if p.tok().lineBefore {
		rd.BodyBreak = p.lineBreak()
	}
	if p.is("pratt") {
		rd.Expr = p.pratt()
		return rd
	}
	rd.Expr = p.bodyChoice()
	if p.is("->") {
		if p.tok().lineBefore {
			rd.ActionBreak = p.lineBreak()
		}
		p.next()
		rd.Action = p.term()
	}
	return rd
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
	for p.is("/") {
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

func (p *parser) pratt() *grammar.Pratt {
	pr := &grammar.Pratt{Pos: p.expect("pratt").pos}
	p.expect("{")
	for {
		b := p.lineBreak()
		if p.accept("}") {
			pr.CloseBreak = b
			break
		}
		switch {
		case p.accept("skip"):
			if pr.Skip != nil {
				p.errorf("duplicate skip")
			}
			pr.Skip = p.choice()
			pr.SkipBreak = b
		case p.accept("operand"):
			o := &grammar.PrattOperand{Expr: p.choice(), Break: b}
			if p.accept("->") {
				o.Action = p.term()
			}
			pr.Operands = append(pr.Operands, o)
		case p.accept("level"):
			l := &grammar.PrattLevel{Break: b}
			if p.tok().kind == tIdent {
				l.Name = p.next().text
			}
			open := p.i
			p.expect("{")
			for {
				ob := p.lineBreak()
				if p.is("}") {
					l.CloseBreak = ob
					l.OneLine = !p.multiline(open, p.i)
					p.next()
					break
				}
				op := p.prattOperator()
				op.Break = ob
				l.Operators = append(l.Operators, op)
			}
			pr.Levels = append(pr.Levels, l)
		default:
			p.errorf("expected 'skip', 'operand', 'level', or '}', found %s", p.tok())
			panic(bail{})
		}
	}
	return pr
}

func (p *parser) prattOperator() *grammar.PrattOperator {
	t := p.tok()
	op := &grammar.PrattOperator{Pos: t.pos}
	switch {
	case p.accept("prefix"):
		op.Kind = grammar.Prefix
	case p.accept("postfix"):
		op.Kind = grammar.Postfix
	case p.accept("infix"):
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
	default:
		p.errorf("expected 'prefix', 'postfix', 'infix', or '}', found %s", t)
		panic(bail{})
	}
	op.Expr = p.choice()
	if p.accept("->") {
		op.Action = p.term()
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
	for p.accept("/") {
		c.Alts = append(c.Alts, p.sequence())
	}
	return c
}

// startsPrimary reports whether the current token can start an element
// of a sequence.
func (p *parser) startsPrimary() bool {
	t := p.tok()
	switch t.kind {
	case tIdent:
		return !stops[t.text]
	case tString, tCharClass:
		return true
	case tPunct:
		switch t.text {
		case "(", "[", "&", "!", "@", "-", "--", "^^", "^", "$$", "$", "_|_", ".":
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
		p.next()
		p.next()
		return &grammar.Capture{Pos: t.pos, Name: t.text, Expr: p.prefixed()}
	}
	switch {
	case p.accept("&"):
		return &grammar.And{Pos: t.pos, Expr: p.prefixed()}
	case p.accept("!"):
		return &grammar.Not{Pos: t.pos, Expr: p.prefixed()}
	case p.accept("@"):
		return &grammar.Atomic{Pos: t.pos, Expr: p.prefixed()}
	case p.is("-"):
		p.next()
		return &grammar.Discard{Pos: t.pos, Expr: p.prefixed()}
	}
	return p.suffixed()
}

func (p *parser) suffixed() grammar.Expr {
	start := p.tok().pos
	e := p.primary()
	for {
		t := p.tok()
		switch {
		case p.accept("*"):
			e = &grammar.Repeat{Pos: start, Expr: e, Min: 0, Max: -1}
		case p.accept("+"):
			e = &grammar.Repeat{Pos: start, Expr: e, Min: 1, Max: -1}
		case p.accept("?"):
			e = &grammar.Optional{Pos: start, Expr: e}
		case p.is("{") && !t.spaceBefore:
			p.next()
			r := &grammar.Repeat{Pos: start, Expr: e}
			if p.tok().kind == tInt {
				r.Min = p.next().num
			}
			if p.accept(",") {
				r.Max = -1
				if p.tok().kind == tInt {
					r.Max = p.next().num
				}
			} else {
				r.Max = r.Min
			}
			if r.Max >= 0 && r.Max < r.Min {
				p.errorf("invalid repetition {%d,%d}", r.Min, r.Max)
			}
			p.expect("}")
			e = r
		case p.is("#"):
			a, ok := e.(*grammar.Attributed)
			if !ok {
				a = &grammar.Attributed{Expr: e, Attrs: []*grammar.Attribute{}}
				e = a
			}
			for p.is("#") {
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
}

func (p *parser) attribute() *grammar.Attribute {
	pos := p.expect("#").pos
	a := &grammar.Attribute{Pos: pos, Name: p.ident().text}
	if p.is("(") && !p.tok().spaceBefore {
		p.next()
		for !p.accept(")") {
			name := p.ident().text
			p.expect("=")
			a.Args = append(a.Args, &grammar.AttrArg{Name: name, Value: p.choice()})
			if !p.is(")") {
				p.expect(",")
			}
		}
	}
	return a
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
		if keywords[t.text] {
			p.errorf("unexpected keyword %s", t.text)
			panic(bail{})
		}
		p.next()
		if t.text == "_" {
			return &grammar.Top{Pos: t.pos}
		}
		ref := &grammar.Ref{Pos: t.pos, Name: t.text}
		if p.is("(") && !p.tok().spaceBefore {
			p.next()
			ref.Level = p.ident().text
			p.expect(")")
		}
		return ref
	case tPunct:
		switch t.text {
		case "(":
			p.next()
			e := p.choice()
			p.expect(")")
			return e
		case "[":
			p.next()
			pred := &grammar.Predicate{Pos: t.pos}
			if p.tok().kind == tIdent && p.peek(1).kind == tPunct && p.peek(1).text == "=" {
				name := p.next()
				p.next()
				pred.Term = &grammar.Assign{Pos: name.pos, Name: name.text, Value: p.term()}
			} else {
				pred.Term = p.term()
			}
			p.expect("]")
			return pred
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
		if t.kind != tPunct || !contains(binaryLevels[level], t.text) {
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
	for p.is(".") {
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
		case "new":
			return p.newTerm()
		}
		p.next()
		if p.is("(") && !p.tok().spaceBefore {
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
	pos := p.expect("new").pos
	n := &grammar.New{Pos: pos, Type: p.ident().text}
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
