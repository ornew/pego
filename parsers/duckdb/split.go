package duckdb

import (
	"strings"
	"unicode/utf8"
)

// A StatementText is a statement with the text DuckDB gives it when it splits a script.
type StatementText struct {
	Statement Statement
	// Text is what DuckDB records as the query of the statement: from the semicolon that precedes the statement (the
	// start of the script for the first) to the semicolon that ends it, or to the end of the script for the last
	// statement. Comments and white space around the statement belong to it.
	Text string
	// Start and End are the byte offsets of Text in the script.
	Start, End int
}

// Split parses a script and returns its statements with their texts, as DuckDB splits it. The error is the
// syntax error of the script (the script is not split if it has one: DuckDB parses all of it first).
func Split(input string) ([]StatementText, error) {
	script, err := ParseAST(input, Bytes)
	if err != nil {
		return nil, err
	}
	return SplitScript(input, script), nil
}

// SplitScript is Split for a script that ParseAST parsed with the position unit Bytes.
func SplitScript(input string, script *Script) []StatementText {
	out := make([]StatementText, 0, len(script.Statements))
	// the last semicolon before the next statement starts the text of the statement
	semi := -1
	q := 0
	for i, st := range script.Statements {
		sp, _ := SpanOf(st)
		for {
			q = skipSpace(input, q)
			if q < sp.Start && q < len(input) && input[q] == ';' {
				semi = q
				q++
				continue
			}
			break
		}
		start := semi + 1
		end := len(input)
		if i+1 < len(script.Statements) {
			p := skipSpace(input, sp.End)
			if p < len(input) && input[p] == ';' {
				end = p
			}
			semi = end
			q = end + 1
		}
		out = append(out, StatementText{Statement: st, Text: replaceUnicodeSpaces(input[start:end], end == len(input)), Start: start, End: end})
	}
	return out
}

// replaceUnicodeSpaces replaces the Unicode white space characters (see isUnicodeSpace) that stand outside strings,
// quoted identifiers and comments by a space each, as DuckDB does to the text of a statement. DuckDB leaves a
// two-byte character (U+00A0) alone when it is the last character of the script; atEnd tells that s ends the script.
func replaceUnicodeSpaces(s string, atEnd bool) string {
	hasWide := false
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			hasWide = true
			break
		}
	}
	if !hasWide {
		return s
	}
	var b []byte
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '\'' || c == '"':
			j := i + 1
			for j < len(s) {
				if s[j] == c {
					if j+1 < len(s) && s[j+1] == c {
						j += 2
						continue
					}
					break
				}
				j++
			}
			b = append(b, s[i:min(j+1, len(s))]...)
			i = min(j+1, len(s))
		case c == '-' && i+1 < len(s) && s[i+1] == '-':
			j := i
			for j < len(s) && s[j] != '\n' && s[j] != '\r' {
				j++
			}
			b = append(b, s[i:j]...)
			i = j
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			j := skipSpace(s, i)
			b = append(b, s[i:j]...)
			i = j
		case c == '$':
			// $tag$ ... $tag$
			j := i + 1
			for j < len(s) && (s[j] == '_' || s[j] >= 'a' && s[j] <= 'z' || s[j] >= 'A' && s[j] <= 'Z' || s[j] >= '0' && s[j] <= '9' && j > i+1 || s[j] >= utf8.RuneSelf) {
				j++
			}
			if j < len(s) && s[j] == '$' {
				tag := s[i : j+1]
				k := strings.Index(s[j+1:], tag)
				if k >= 0 {
					end := j + 1 + k + len(tag)
					b = append(b, s[i:end]...)
					i = end
					continue
				}
			}
			b = append(b, c)
			i++
		case c >= utf8.RuneSelf:
			r, n := utf8.DecodeRuneInString(s[i:])
			if isUnicodeSpace(r) && !(atEnd && n == 2 && i+n == len(s)) {
				b = append(b, ' ')
			} else {
				b = append(b, s[i:i+n]...)
			}
			i += n
		default:
			b = append(b, c)
			i++
		}
	}
	return string(b)
}

// skipSpace returns the offset of the first byte at or after p that is not white space or a comment.
func skipSpace(s string, p int) int {
	for p < len(s) {
		switch c := s[p]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f':
			p++
		case c == '-' && p+1 < len(s) && s[p+1] == '-':
			for p < len(s) && s[p] != '\n' && s[p] != '\r' {
				p++
			}
		case c == '/' && p+1 < len(s) && s[p+1] == '*':
			depth := 0
			for p < len(s) {
				if s[p] == '/' && p+1 < len(s) && s[p+1] == '*' {
					depth++
					p += 2
				} else if s[p] == '*' && p+1 < len(s) && s[p+1] == '/' {
					depth--
					p += 2
					if depth == 0 {
						break
					}
				} else {
					p++
				}
			}
		case c >= utf8.RuneSelf:
			r, n := utf8.DecodeRuneInString(s[p:])
			if !isUnicodeSpace(r) {
				return p
			}
			p += n
		default:
			return p
		}
	}
	return p
}

// isUnicodeSpace reports the characters that DuckDB reads as white space when they stand where an identifier
// could be.
func isUnicodeSpace(r rune) bool {
	switch {
	case r == 0xa0, r >= 0x2000 && r <= 0x200b, r == 0x202f, r == 0x205f, r == 0x2060, r == 0x3000, r == 0xfeff:
		return true
	}
	return false
}
