package postgresql

import "strings"

// Split splits a script into its statements without parsing them, as PostgreSQL's scanner would: a semicolon
// ends a statement unless it is in a string, an identifier, a comment, in parentheses (a rule can have a list
// of actions) or in the body of a CREATE FUNCTION or CREATE PROCEDURE with BEGIN ATOMIC ... END. The spans
// are in bytes, run from the first token to the last token of a statement (not including the semicolon, the
// white space and the comments around it), and statements that are empty are left out. Unlike ParseAST,
// Split accepts any input, including statements with syntax errors, so that one can be reported without
// losing the others; the text after an unterminated string or comment is the rest of the last statement.
func Split(src string) []Span {
	var out []Span
	n := len(src)
	start, end := -1, -1 // the first and the end of the last token of the current statement
	depth := 0           // parentheses
	atomic := 0          // the depth of BEGIN ATOMIC ... END (CASE ... END nests)
	var words []string   // the first words of the statement
	var last string
	flush := func() {
		if start >= 0 {
			out = append(out, Span{start, end})
		}
		start, end, depth, atomic = -1, -1, 0, 0
		words, last = words[:0], ""
	}
	tok := func(i, j int) {
		if start < 0 {
			start = i
		}
		end = j
	}
	for i := 0; i < n; {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v':
			i++
		case c == '-' && strings.HasPrefix(src[i:], "--"):
			for i < n && src[i] != '\n' && src[i] != '\r' {
				i++
			}
		case c == '/' && strings.HasPrefix(src[i:], "/*"):
			d := 0
			for i < n {
				if strings.HasPrefix(src[i:], "/*") {
					d++
					i += 2
				} else if strings.HasPrefix(src[i:], "*/") {
					d--
					i += 2
					if d == 0 {
						break
					}
				} else {
					i++
				}
			}
		case c == ';' && depth == 0 && atomic == 0:
			flush()
			i++
		case c == '\'' || ((c == 'e' || c == 'E') && i+1 < n && src[i+1] == '\'' && !identBefore(src, i)):
			j := i + 1
			esc := c != '\''
			if esc {
				j++
			}
			j = skipQuoted(src, j, '\'', esc)
			tok(i, j)
			i = j
			last = ""
		case c == '"':
			j := skipQuoted(src, i+1, '"', false)
			tok(i, j)
			i = j
			last = ""
		case c == '$' && dollarTag(src, i) > 0:
			tag := src[i : i+dollarTag(src, i)]
			j := strings.Index(src[i+len(tag):], tag)
			if j < 0 {
				j = n
			} else {
				j += i + 2*len(tag)
			}
			tok(i, j)
			i = j
			last = ""
		case isIdentStart(c):
			j := i + 1
			for j < n && isIdentCont(src[j]) {
				j++
			}
			w := strings.ToLower(src[i:j])
			tok(i, j)
			if len(words) < 8 {
				words = append(words, w)
			}
			if atomic == 0 && w == "atomic" && last == "begin" && len(words) > 1 && words[0] == "create" {
				atomic = 1
			} else if atomic > 0 {
				switch w {
				case "case":
					atomic++
				case "end":
					atomic--
				}
			}
			last = w
			i = j
		default:
			switch c {
			case '(':
				depth++
			case ')':
				if depth > 0 {
					depth--
				}
			}
			tok(i, i+1)
			i++
			last = ""
		}
	}
	flush()
	return out
}

func isIdentStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c >= 0x80
}

func isIdentCont(c byte) bool {
	return isIdentStart(c) || c >= '0' && c <= '9' || c == '$'
}

// identBefore tells whether src[i] continues an identifier.
func identBefore(src string, i int) bool {
	return i > 0 && isIdentCont(src[i-1])
}

// skipQuoted returns the index after the closing quote of a quoted string or identifier whose body starts at
// i; a doubled quote is part of the body, and in an E string so is a backslash and the character after it.
func skipQuoted(src string, i int, q byte, backslash bool) int {
	for i < len(src) {
		switch {
		case backslash && src[i] == '\\':
			i += 2
		case src[i] == q:
			if i+1 < len(src) && src[i+1] == q {
				i += 2
				continue
			}
			return i + 1
		default:
			i++
		}
	}
	return len(src)
}

// dollarTag returns the length of the opening delimiter $tag$ of a dollar-quoted string at src[i], or 0.
func dollarTag(src string, i int) int {
	j := i + 1
	if j < len(src) && isIdentStart(src[j]) && src[j] < 0x80 || j < len(src) && src[j] >= 0x80 {
		for j < len(src) && (isIdentStart(src[j]) || src[j] >= '0' && src[j] <= '9') {
			j++
		}
	}
	if j < len(src) && src[j] == '$' {
		return j + 1 - i
	}
	return 0
}
