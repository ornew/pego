package syntax

import (
	"strings"
	"unicode"

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

func collectComments(previous grammar.Pos, next token, comments []*grammar.Comment) *commentBoundary {
	b := &commentBoundary{}
	canPrevious := previous.IsValid()
	inlineEnd := 0
	groups := 0
	var pending []*grammar.Comment
	flush := func() {
		if pending == nil {
			return
		}
		var content strings.Builder
		for _, comment := range pending {
			content.WriteString(comment.Content)
		}
		if canPrevious {
			b.trailing = content.String()
			b.hasTrailing = true
			canPrevious = false
		} else {
			b.detached = append(b.detached, content.String())
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
	for _, c := range comments {
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
	if next.kind != tEOF && (previous.Line == next.pos.Line || inlineEnd == next.pos.Line) && count == 1 {
		if b.hasTrailing {
			b.detached = append([]string{b.trailing}, b.detached...)
			b.trailing, b.hasTrailing = "", false
		}
		canPrevious = false
		flush()
	}
	if !scopeEnd && pending != nil {
		var content strings.Builder
		for _, c := range pending {
			content.WriteString(c.Content)
		}
		b.leading = content.String()
	}
	return b
}

func commentContent(raw, ending string) string {
	if strings.HasPrefix(raw, "//") {
		return raw[2:] + ending
	}
	content := strings.TrimPrefix(raw, "/*")
	content = strings.TrimSuffix(content, "*/")
	var out strings.Builder
	first := true
	for {
		i := strings.IndexAny(content, "\r\n")
		line, newline := content, ""
		if i >= 0 {
			line, newline = content[:i], content[i:i+1]
			if newline == "\r" && i+1 < len(content) && content[i+1] == '\n' {
				newline = "\r\n"
			}
		}
		if !first {
			line = strings.TrimLeftFunc(line, unicode.IsSpace)
			if !strings.HasPrefix(line, "*/") {
				line = strings.TrimPrefix(line, "*")
			}
		}
		out.WriteString(line)
		out.WriteString(newline)
		if i < 0 {
			break
		}
		content = content[i+len(newline):]
		first = false
	}
	return out.String()
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
