package cue

import "strings"

// A Comment is a // comment: its position and its text, from the slashes to the end of the line (carriage
// returns removed).
type Comment struct {
	Span
	Text string
}

// Comments returns the comments of a source that parses, in order. Comments are not part of the syntax tree
// (the parser skips them like white space); this function finds them with a scan of the source that knows
// strings, interpolations and attributes. Positions are in code points, or in bytes with Bytes, as in
// ParseAST. The result for a source that does not parse is unspecified.
func Comments(src string, unit ...Unit) []Comment {
	s := &commentScanner{src: src}
	s.code(0, false)
	if len(unit) > 0 && unit[0] == Bytes {
		return s.out
	}
	// Convert byte offsets to code points.
	n, last := 0, 0
	for i := range s.out {
		c := &s.out[i]
		n += runeCount(src[last:c.Start])
		last = c.Start
		c.End = n + runeCount(src[c.Start:c.End])
		c.Start = n
	}
	return s.out
}

func runeCount(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

type commentScanner struct {
	src string
	i   int
	out []Comment
}

// code scans code until the end of the source, or, inside the parentheses of an interpolation (depth > 0 on
// entry), until the closing parenthesis; record tells whether comments are recorded (they are not inside
// an attribute).
func (s *commentScanner) code(depth int, attr bool) {
	src := s.src
	for s.i < len(src) {
		c := src[s.i]
		switch {
		case c == '/' && s.i+1 < len(src) && src[s.i+1] == '/':
			start := s.i
			for s.i < len(src) && src[s.i] != '\n' {
				s.i++
			}
			if !attr {
				text := src[start:s.i]
				if strings.IndexByte(text, '\r') >= 0 {
					text = strings.ReplaceAll(text, "\r", "")
				}
				s.out = append(s.out, Comment{Span{start, start + len(src[start:s.i])}, text})
			}
		case c == '"' || c == '\'' || c == '#':
			j := s.i
			for j < len(src) && src[j] == '#' {
				j++
			}
			if j < len(src) && (src[j] == '"' || src[j] == '\'') {
				s.str(j-s.i, src[j], attr)
			} else {
				s.i = j
			}
		case c == '@' && !attr:
			s.i++
			for s.i < len(src) && (isIdentByte(src[s.i]) || src[s.i] >= 0x80) {
				s.i++
			}
			for s.i < len(src) && (src[s.i] == ' ' || src[s.i] == '\t' || src[s.i] == '\r' || src[s.i] == '\n') {
				s.i++
			}
			if s.i < len(src) && src[s.i] == '(' {
				s.i++
				s.attrBody()
			}
		case c == '(' && depth > 0:
			depth++
			s.i++
		case c == ')' && depth > 0:
			depth--
			s.i++
			if depth == 0 {
				return
			}
		default:
			s.i++
		}
	}
}

// attrBody scans the body of an attribute up to the parenthesis that closes it (the brackets nest).
func (s *commentScanner) attrBody() {
	src := s.src
	var stack []byte
	for s.i < len(src) {
		c := src[s.i]
		switch c {
		case '(', '[', '{':
			stack = append(stack, c)
			s.i++
		case ')', ']', '}':
			s.i++
			if len(stack) == 0 {
				return
			}
			stack = stack[:len(stack)-1]
		case '/', '"', '\'', '#', '@':
			// Strings, comments and nested attributes: let code scan one token's worth.
			if c == '/' && !(s.i+1 < len(src) && src[s.i+1] == '/') {
				s.i++
				continue
			}
			if c == '@' {
				s.i++
				continue
			}
			j := s.i
			for j < len(src) && src[j] == '#' {
				j++
			}
			if c == '/' || (j < len(src) && (src[j] == '"' || src[j] == '\'')) {
				s.token(c, j)
			} else {
				s.i = j
			}
		default:
			s.i++
		}
	}
}

// token scans a comment or a string inside an attribute.
func (s *commentScanner) token(c byte, j int) {
	src := s.src
	if c == '/' {
		for s.i < len(src) && src[s.i] != '\n' {
			s.i++
		}
		return
	}
	s.str(j-s.i, src[j], true)
}

func isIdentByte(c byte) bool {
	return c == '_' || c == '$' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// str scans a string that starts at s.i with hashes hashes and the quote q at s.i+hashes.
func (s *commentScanner) str(hashes int, q byte, attr bool) {
	src := s.src
	s.i += hashes + 1
	triple := s.i+1 < len(src) && src[s.i] == q && src[s.i+1] == q
	multi := false
	if triple {
		// #"""# is a string of one quote; otherwise """ opens a multi-line string.
		k := s.i + 2
		n := 0
		for k < len(src) && src[k] == '#' && n < hashes {
			k++
			n++
		}
		if hashes > 0 && n == hashes {
			s.i = k
			return
		}
		multi = true
		s.i += 2
		// the line break after the opening quotes
		if s.i < len(src) && src[s.i] == '\r' {
			s.i++
		}
		if s.i < len(src) && src[s.i] == '\n' {
			s.i++
		}
	}
	closeOK := multi // a multi-line string closes at the start of a line, after blanks
	for s.i < len(src) {
		c := src[s.i]
		switch {
		case c == q && s.closes(q, multi, closeOK, hashes):
			s.i += 1 + hashes
			if multi {
				s.i += 2
			}
			return
		case c == '\\' && s.escHashes(hashes):
			s.i += 1 + hashes
			if s.i < len(src) && src[s.i] == '(' {
				s.i++
				s.code(1, attr)
				closeOK = false
			} else if s.i < len(src) {
				s.i++ // the escaped character
				closeOK = false
			}
		case c == '\n':
			if !multi {
				return // unterminated
			}
			closeOK = true
			s.i++
		case multi && closeOK && (c == ' ' || c == '\t' || c == '\r'):
			s.i++
		default:
			closeOK = false
			s.i++
		}
	}
}

// escHashes reports whether the backslash at s.i is followed by exactly hashes hashes.
func (s *commentScanner) escHashes(hashes int) bool {
	src := s.src
	for k := 1; k <= hashes; k++ {
		if s.i+k >= len(src) || src[s.i+k] != '#' {
			return false
		}
	}
	return true
}

// closes reports whether the quote at s.i closes the string.
func (s *commentScanner) closes(q byte, multi, closeOK bool, hashes int) bool {
	src := s.src
	i := s.i + 1
	if multi {
		if !closeOK || i+1 >= len(src) || src[i] != q || src[i+1] != q {
			return false
		}
		i += 2
	}
	for k := 0; k < hashes; k++ {
		if i+k >= len(src) || src[i+k] != '#' {
			return false
		}
	}
	return true
}
