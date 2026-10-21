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
	leading, trailing string
	detached          []string
	hasTrailing       bool
	hasNext           bool
	leadingOwner      *grammar.SourceInfo
	trailingOwner     *grammar.SourceInfo
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
	flush := func() {
		if pending == nil {
			return
		}
		content := joinCommentContent(pending, check)
		if canPrevious {
			b.trailing = content
			b.hasTrailing = true
			canPrevious = false
		} else {
			b.detached = append(b.detached, content)
		}
		pending = nil
		groups++
	}
	last := previous
	if canPrevious && comments[0].Pos.Line == previous.Line {
		pending = comments[:1]
		last = comments[0].End
		inlineEnd = last.Line
		flush() // Inline comments never absorb following comment-only lines.
		comments = comments[1:]
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
	scopeEnd := next.kind == tEOF || next.kind == tPunct && (next.text == ")" || next.text == "]" || next.text == "}")
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
				b.leading = b.trailing
			} else {
				b.detached = append([]string{b.trailing}, b.detached...)
			}
			b.trailing, b.hasTrailing = "", false
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
	if b := p.toks[p.i].source; b != nil && b.hasTrailing {
		if old := b.trailingOwner; old != nil && old != s {
			old.TrailingComments = ""
		}
		s.TrailingComments = b.trailing
		b.trailingOwner = s
	}
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
