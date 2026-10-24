package syntax

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
)

// commentBoundary is lexer-owned syntax trivia. The parser claims each side
// when constructing its node; no semantic-analysis attachment pass is needed.
type commentBoundary struct {
	leading  string
	trailing []string
	detached []string
	// run is the number of comments on the line of the previous token that are all trailing
	// comments of it, because a line break or the end of a scope follows them.
	run           int
	hasTrailing   bool
	hasNext       bool
	leadingOwner  *grammar.SourceInfo
	trailingOwner *grammar.SourceInfo
}

func collectComments(previous grammar.Pos, opens bool, next token, comments []*grammar.Comment) *commentBoundary {
	return collectCommentsWithCheck(previous, opens, next, comments, nil)
}

// collectCommentsWithCheck groups the comments between two tokens. opens
// reports that the token before them is an opening delimiter or a comma.
func collectCommentsWithCheck(previous grammar.Pos, opens bool, next token, comments []*grammar.Comment, check func()) *commentBoundary {
	b := &commentBoundary{}
	canPrevious := previous.IsValid()
	inlineEnd := 0
	groups := 0
	var pending []*grammar.Comment
	inRun := false // flushing the comments of the line of the previous token
	flush := func() {
		if pending == nil {
			return
		}
		content := joinCommentContent(pending, check)
		if canPrevious || inRun {
			b.trailing = append(b.trailing, content)
			b.hasTrailing = true
			canPrevious = false
		} else {
			b.detached = append(b.detached, content)
		}
		pending = nil
		groups++
	}
	last := previous
	scopeEnd := next.endsScope()
	if canPrevious && comments[0].Pos.Line == previous.Line {
		// The comments on the line of the previous token are all trailing when a line break or
		// the end of a scope follows them; otherwise only the first is a group of its own.
		n := 1
		for n < len(comments) && comments[n].Pos.Line == comments[n-1].End.Line && !strings.HasPrefix(comments[n-1].Text, "//") {
			n++
		}
		if !scopeEnd && next.pos.Line <= comments[n-1].End.Line {
			n = 1
		}
		inRun = true
		for _, c := range comments[:n] {
			pending = []*grammar.Comment{c}
			flush() // Inline comments never absorb following comment-only lines.
		}
		inRun = false
		b.run = n
		last = comments[n-1].End
		inlineEnd = last.Line
		canPrevious = false
		comments = comments[n:]
	}
	for i, c := range comments {
		if check != nil && i&255 == 0 {
			check()
		}
		if last.IsValid() && c.Pos.Line-last.Line >= 2 {
			flush()
			canPrevious = false
		}
		if len(pending) != 0 && (!strings.HasPrefix(pending[0].Text, "//") || !strings.HasPrefix(c.Text, "//")) {
			flush()
		}
		pending = append(pending, c)
		last = c.End
	}
	if next.pos.Line-last.Line >= 2 {
		flush()
		canPrevious = false
	}
	b.hasNext = !scopeEnd
	if scopeEnd {
		flush()
	}
	count := groups
	if pending != nil {
		count++
	}
	// A lone block comment between two tokens of one line is neither the trailing
	// comment of the first nor the leading comment of the second, unless the first
	// opens a list or separates its elements, and then it leads the second. Before a
	// closing delimiter it trails the token before it.
	if !scopeEnd && (previous.Line == next.pos.Line || inlineEnd == next.pos.Line) && count == 1 {
		if b.hasTrailing {
			if opens {
				b.leading = b.trailing[0]
			} else {
				b.detached = append([]string{b.trailing[0]}, b.detached...)
			}
			b.trailing, b.hasTrailing, b.run = nil, false, 0
		}
		canPrevious = false
		flush()
	}
	if !scopeEnd && pending != nil {
		b.leading = joinCommentContent(pending, check)
	}
	return b
}

func joinCommentContent(comments []*grammar.Comment, check func()) string {
	if len(comments) == 1 {
		if check != nil {
			check()
		}
		return comments[0].Content
	}
	length := 0
	for i, c := range comments {
		if check != nil && i&255 == 0 {
			check()
		}
		length += len(c.Content)
	}
	var out strings.Builder
	out.Grow(length)
	for i, c := range comments {
		if check != nil && i&255 == 0 {
			check()
		}
		writeCommentContent(&out, c.Content, check)
	}
	return out.String()
}

func commentContent(raw, ending string) string {
	return commentContentWithCheck(raw, ending, nil)
}

func commentContentWithCheck(raw, ending string, check func()) string {
	if strings.HasPrefix(raw, "//") {
		if check == nil {
			return raw[2:] + ending
		}
		var out strings.Builder
		out.Grow(len(raw) - 2 + len(ending))
		writeCommentContent(&out, raw[2:], check)
		out.WriteString(ending)
		return out.String()
	}
	content := strings.TrimPrefix(raw, "/*")
	content = strings.TrimSuffix(content, "*/")
	var out strings.Builder
	first := true
	for {
		i := commentLineBreak(content, check)
		line, newline := content, ""
		if i >= 0 {
			line, newline = content[:i], content[i:i+1]
			if newline == "\r" && i+1 < len(content) && content[i+1] == '\n' {
				newline = "\r\n"
			}
		}
		if !first {
			line = trimCommentLeft(line, check)
			if !strings.HasPrefix(line, "*/") {
				line = strings.TrimPrefix(line, "*")
			}
		}
		if check != nil {
			out.Grow(len(line) + len(newline))
		}
		writeCommentContent(&out, line, check)
		out.WriteString(newline)
		if i < 0 {
			break
		}
		content = content[i+len(newline):]
		first = false
	}
	return out.String()
}

func commentLineBreak(s string, check func()) int {
	if check == nil {
		return strings.IndexAny(s, "\r\n")
	}
	for i := 0; i < len(s); i += 256 {
		check()
		end := min(i+256, len(s))
		if j := strings.IndexAny(s[i:end], "\r\n"); j >= 0 {
			return i + j
		}
	}
	return -1
}

func trimCommentLeft(s string, check func()) string {
	if check == nil {
		return strings.TrimLeftFunc(s, unicode.IsSpace)
	}
	for i, n := 0, 0; i < len(s); n++ {
		if n&255 == 0 {
			check()
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if !unicode.IsSpace(r) {
			return s[i:]
		}
		i += size
		if i == len(s) {
			return ""
		}
	}
	return s
}

func writeCommentContent(out *strings.Builder, s string, check func()) {
	if check == nil {
		out.WriteString(s)
		return
	}
	for len(s) > 0 {
		check()
		n := min(len(s), 256)
		out.WriteString(s[:n])
		s = s[n:]
	}
}

// source records the tokens consumed by a production and transfers ownership
// to the enclosing production when source boundaries are shared. Diagnostic
// token positions remain independent of the complete syntax span.
func (p *parser) sourceNode(node any, start int) {
	if n, ok := node.(grammar.SourceNode); ok {
		p.source(n, start)
	}
}

func (p *parser) source(node grammar.SourceNode, start int) {
	s := node.Source()
	pos := p.toks[start].pos
	old := s.Diagnostic()
	if old.IsValid() && old != pos {
		s.DiagnosticPos = old
	} else {
		s.DiagnosticPos = grammar.Pos{}
	}
	s.Pos = pos
	s.End = pos
	if p.i > start {
		s.End = p.toks[p.i-1].end
	}
	if b := p.toks[start].source; b != nil && b.hasNext {
		if b.leadingOwner != s {
			if old := b.leadingOwner; old != nil {
				old.LeadingComments, old.LeadingDetachedComments = "", nil
			}
			s.LeadingComments = b.leading
			s.LeadingDetachedComments = b.detached
			b.leadingOwner = s
		}
	}
	p.sourceTrailing(s)
}

func (p *parser) sourceTrailing(s *grammar.SourceInfo) {
	if b := p.toks[p.i].source; b != nil && b.hasTrailing && b.trailingOwner != s {
		if old := b.trailingOwner; old != nil {
			old.TrailingComments = nil
		}
		// The comments after a semicolon follow the ones before it, when the node owns them.
		if p.i > 0 && p.toks[p.i-1].kind == tPunct && p.toks[p.i-1].text == ";" {
			if before := p.toks[p.i-1].source; before != nil && before.trailingOwner == s {
				s.TrailingComments = append(s.TrailingComments, b.trailing...)
				b.trailingOwner = s
				return
			}
		}
		s.TrailingComments = append([]string(nil), b.trailing...)
		b.trailingOwner = s
	}
}

// takeInlineGap takes the comments between the previous token and the current
// one when they are all block comments and no line break separates the two
// tokens and the comments but the lines inside the comments. The formatter
// writes them in the same place, so their owners do not change.
func (p *parser) takeInlineGap() []*grammar.Comment {
	if !p.inlineGap() {
		return nil
	}
	t := &p.toks[p.i]
	cs := t.comments
	if t.lineComment != nil {
		cs = p.appendComments([]*grammar.Comment{t.lineComment}, cs)
	}
	t.lineComment, t.comments = nil, nil
	p.inlined = p.i + 1
	return cs
}

// inlineGap reports whether the comments before the current token form a gap
// takeInlineGap takes.
func (p *parser) inlineGap() bool {
	if p.header || p.i == 0 {
		return false
	}
	t := &p.toks[p.i]
	if t.lineComment == nil && len(t.comments) == 0 {
		return false
	}
	line := p.toks[p.i-1].end.Line
	step := func(c *grammar.Comment) bool {
		p.poll()
		if !strings.HasPrefix(c.Text, "/*") || c.Pos.Line != line {
			return false
		}
		line = c.End.Line
		return true
	}
	if t.lineComment != nil && !step(t.lineComment) {
		return false
	}
	for _, c := range t.comments {
		if !step(c) {
			return false
		}
	}
	return line == t.pos.Line
}

// takeCommentGap takes the comments between the previous token and the current one,
// whatever their kind and place. It is for places inside delimiters or after an
// operator, where a comment may stand on a line of its own: the Gap keeps the
// lines of the tokens and the comments for the formatter.
func (p *parser) takeCommentGap() *grammar.Gap {
	if p.header || p.i == 0 {
		return nil
	}
	t := &p.toks[p.i]
	if t.lineComment == nil && len(t.comments) == 0 {
		return nil
	}
	cs := t.comments
	if t.lineComment != nil {
		cs = p.appendComments([]*grammar.Comment{t.lineComment}, cs)
	}
	t.lineComment, t.comments = nil, nil
	return &grammar.Gap{Comments: cs, PreviousLine: p.toks[p.i-1].end.Line, NextLine: t.pos.Line}
}

// leadOf keeps the gap before the first token of e, if there is one.
func (p *parser) leadOf(e grammar.Expr, gap *grammar.Gap) {
	if gap != nil {
		layoutOf(e).Lead = gap
	}
}

// atLineStart reports whether the current token starts a line that the layout
// of a rule body records: a line break before it, other than the lines inside
// the block comments that stand between it and the token before.
func (p *parser) atLineStart() bool {
	return p.tok().lineBefore && !p.inlineGap() && p.inlined != p.i+1
}

// afterGap keeps the inline comments before the current token, "/" or "=>", as
// the comments after e.
func (p *parser) afterGap(e grammar.Expr) {
	if gap := p.takeInlineGap(); gap != nil {
		layoutOf(e).After = gap
	}
}

// layoutOf returns the layout of the rule expression e, which it creates.
func layoutOf(e any) *grammar.SourceLayout {
	s := e.(grammar.SourceNode).Source()
	if s.Layout == nil {
		s.Layout = &grammar.SourceLayout{}
	}
	return s.Layout
}

// takeGap takes the comments before the current token, which lie between it
// and the token before it, for a #repeat pragma.
func (p *parser) takeGap() grammar.RepeatGap {
	t := &p.toks[p.i]
	cs := t.comments
	if t.lineComment != nil {
		cs = p.appendComments([]*grammar.Comment{t.lineComment}, cs)
	}
	g := grammar.RepeatGap{Comments: cs, PrevLine: p.toks[p.i-1].end.Line, NextLine: t.pos.Line}
	t.lineComment, t.comments = nil, nil
	return g
}

// takeTermLayout keeps trivia at an expression boundary rather than promoting
// it to the current statement's line break. Normalized ownership is independent.
func (p *parser) takeTermLayout() *grammar.SourceLayout {
	if p.header {
		return nil
	}
	t := &p.toks[p.i]
	if t.lineComment == nil && len(t.comments) == 0 {
		return nil
	}
	cs := t.comments
	if t.lineComment != nil {
		cs = p.appendComments([]*grammar.Comment{t.lineComment}, cs)
	}
	previous := 0
	if p.i > 0 {
		previous = p.toks[p.i-1].end.Line
	}
	layout := &grammar.SourceLayout{Before: cs, PreviousLine: previous, NextLine: t.pos.Line}
	t.lineComment, t.comments = nil, nil
	return layout
}
