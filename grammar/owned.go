package grammar

import "strings"

// A grammar decoded from JSON, or built by hand, has no LineBreak values, but
// its nodes may own comments (SourceInfo.LeadingComments, TrailingComments and
// LeadingDetachedComments). Format writes those comments for the nodes that
// stand on lines of their own: the package clause, the declarations, the fields
// of a struct, and the lines of an operators rule. A node that has a LineBreak
// keeps its layout comment tokens instead, so the comments of parsed source
// are never written twice.

// ownedComments returns the comment tokens of text, a normalized comment: the
// lines of a text that ends in a line break are line comments, and any other
// text is one block comment.
func ownedComments(text string) []*Comment {
	if text == "" {
		return nil
	}
	if !strings.HasSuffix(text, "\n") {
		return []*Comment{{Text: "/*" + strings.ReplaceAll(text, "*/", "* /") + "*/"}}
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	cs := make([]*Comment, len(lines))
	for i, line := range lines {
		cs[i] = &Comment{Text: "//" + strings.TrimRight(line, " \t\r")}
	}
	return cs
}

// ownedLead returns b, or when b is nil the line break that writes the leading
// and detached comments s owns. blank says whether a blank line precedes the
// first comment, as it does between declarations.
func ownedLead(s *SourceInfo, b *LineBreak, blank bool) *LineBreak {
	if b != nil || s.LeadingComments == "" && len(s.LeadingDetachedComments) == 0 {
		return b
	}
	return ownedLeadFrom(s, blank)
}

// ownedLeadFrom builds the break of ownedLead: the detached paragraphs, each
// after a blank line, then the leading comments directly before the node.
func ownedLeadFrom(s *SourceInfo, blank bool) *LineBreak {
	b := &LineBreak{}
	for i, paragraph := range s.LeadingDetachedComments {
		cs := ownedComments(paragraph)
		if len(cs) == 0 {
			continue
		}
		cs[0].Blank = i > 0 || blank
		b.Comments = append(b.Comments, cs...)
	}
	leading := ownedComments(s.LeadingComments)
	if len(leading) > 0 {
		leading[0].Blank = len(b.Comments) > 0 || blank
		b.Comments = append(b.Comments, leading...)
	} else {
		b.Blank = len(b.Comments) > 0
	}
	return b
}

// ownedTrailing returns the break that writes the trailing comment s owns at
// the end of its line, or nil. A comment over several lines is one block
// comment.
func ownedTrailing(s *SourceInfo, b *LineBreak) *LineBreak {
	if b != nil || s.TrailingComments == "" {
		return nil
	}
	text := s.TrailingComments
	cs := ownedComments(text)
	c := cs[0]
	if len(cs) > 1 {
		c = &Comment{Text: "/*" + strings.ReplaceAll(text, "*/", "* /") + "*/"}
	}
	return &LineBreak{Trailing: c, TrailingEnds: strings.HasPrefix(c.Text, "//")}
}

// ownsComments reports whether Format writes comments that s owns, because b
// is nil.
func ownsComments(s *SourceInfo, b *LineBreak) bool {
	return b == nil && (s.LeadingComments != "" || s.TrailingComments != "" || len(s.LeadingDetachedComments) > 0)
}
