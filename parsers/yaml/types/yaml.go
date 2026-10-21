package types

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Name returns the name of the anchor, without "&".
func (a *Anchor) Name() string { return a.Value[1:] }

// Name returns the name of the anchor the alias refers to, without "*".
func (a *Alias) Name() string { return a.Value[1:] }

// Value returns the content of the scalar: for a plain or quoted scalar, its text with the lines folded
// and, in a double-quoted scalar, the escape sequences decoded; for a block scalar, its lines without
// the indentation, folded in a folded scalar and with the final line breaks chomped as the header says.
// An escape of a surrogate or of a value outside Unicode decodes to U+FFFD.
func (s *Scalar) Value() string {
	switch s.Style {
	case SingleQuoted:
		return foldFlow(s.Text[1:len(s.Text)-1], '\'')
	case DoubleQuoted:
		return foldFlow(s.Text[1:len(s.Text)-1], '"')
	case Literal, Folded:
		return blockValue(s.Text, s.Indent, s.Style == Folded)
	}
	if !strings.ContainsAny(s.Text, "\r\n") {
		return s.Text
	}
	return foldFlow(s.Text, 0)
}

func isWhite(c byte) bool { return c == ' ' || c == '\t' }

// foldFlow decodes the text of a flow scalar (7.3): a line break and the white space around it fold to a
// space, or to a line feed per empty line that follows it. The quote is a single quote to decode the
// doubled quotes of a single-quoted scalar, a double quote to decode the escapes of a double-quoted one,
// or 0.
func foldFlow(t string, quote byte) string {
	var b strings.Builder
	b.Grow(len(t))
	pending := -1 // the start of white space not yet written, which a line break discards; -1 if none
	flush := func(i int) {
		if pending >= 0 {
			b.WriteString(t[pending:i])
			pending = -1
		}
	}
	i := 0
	// skipBreak skips the line break at i, the empty lines after it and the indentation of the next line,
	// and returns the number of empty lines.
	skipBreak := func() int {
		empty := 0
		i = skipNewline(t, i)
		for {
			j := i
			for j < len(t) && isWhite(t[j]) {
				j++
			}
			if j < len(t) && (t[j] == '\n' || t[j] == '\r') {
				empty++
				i = skipNewline(t, j)
				continue
			}
			i = j
			return empty
		}
	}
	for i < len(t) {
		c := t[i]
		switch {
		case isWhite(c):
			if pending < 0 {
				pending = i
			}
			i++
		case c == '\n' || c == '\r':
			pending = -1
			if empty := skipBreak(); empty == 0 {
				b.WriteByte(' ')
			} else {
				b.WriteString(strings.Repeat("\n", empty))
			}
		case c == '\'' && quote == '\'':
			flush(i)
			b.WriteByte('\'')
			i += 2
		case c == '\\' && quote == '"':
			flush(i)
			i++
			if t[i] == '\n' || t[i] == '\r' {
				// An escaped line break: the break and the indentation of the next line are discarded,
				// the empty lines after it are line feeds.
				b.WriteString(strings.Repeat("\n", skipBreak()))
				continue
			}
			i = decodeEscape(&b, t, i)
		default:
			flush(i)
			j := i + 1
			for j < len(t) && !isWhite(t[j]) && t[j] != '\n' && t[j] != '\r' && t[j] != '\\' && t[j] != '\'' {
				j++
			}
			b.WriteString(t[i:j])
			i = j
		}
	}
	flush(len(t))
	return b.String()
}

func skipNewline(t string, i int) int {
	if t[i] == '\r' && i+1 < len(t) && t[i+1] == '\n' {
		return i + 2
	}
	return i + 1
}

// decodeEscape writes the character of the escape sequence whose letter is at t[i] and returns the
// index after the sequence.
func decodeEscape(b *strings.Builder, t string, i int) int {
	c := t[i]
	switch c {
	case '0':
		b.WriteByte(0)
	case 'a':
		b.WriteByte('\a')
	case 'b':
		b.WriteByte('\b')
	case 't', '\t':
		b.WriteByte('\t')
	case 'n':
		b.WriteByte('\n')
	case 'v':
		b.WriteByte('\v')
	case 'f':
		b.WriteByte('\f')
	case 'r':
		b.WriteByte('\r')
	case 'e':
		b.WriteByte(0x1b)
	case 'N':
		b.WriteRune(0x85)
	case '_':
		b.WriteRune(0xa0)
	case 'L':
		b.WriteRune(0x2028)
	case 'P':
		b.WriteRune(0x2029)
	case 'x', 'u', 'U':
		n := map[byte]int{'x': 2, 'u': 4, 'U': 8}[c]
		v, _ := strconv.ParseUint(t[i+1:i+1+n], 16, 32)
		r := rune(v)
		if v > utf8.MaxRune || !utf8.ValidRune(r) {
			r = utf8.RuneError
		}
		b.WriteRune(r)
		return i + 1 + n
	default: // ' ', '"', '/', '\\'
		b.WriteByte(c)
	}
	return i + 1
}

// blockValue decodes a block scalar (8.1) from its text, which starts with the indicator, and the
// indentation of its content.
func blockValue(t string, indent int, folded bool) string {
	// The header: indicators up to white space, a comment or the end of the line.
	chomp := byte(0)
	i := 1
	for ; i < len(t) && !isWhite(t[i]) && t[i] != '#' && t[i] != '\n' && t[i] != '\r'; i++ {
		if t[i] == '-' || t[i] == '+' {
			chomp = t[i]
		}
	}
	for i < len(t) && t[i] != '\n' && t[i] != '\r' {
		i++
	}
	if i < len(t) {
		i = skipNewline(t, i)
	}
	// The lines. A last line without a line break counts as one with a line break, as in the reference
	// parser of the YAML test suite.
	type line struct {
		text    string // without the indentation; "" for an empty line
		content bool   // a content line, rather than an empty one
		more    bool   // a more-indented line: its text starts with white space
	}
	var lines []line
	for i < len(t) {
		j := i
		for j < len(t) && t[j] != '\n' && t[j] != '\r' {
			j++
		}
		var l line
		if raw := t[i:j]; len(raw) > indent {
			l.text, l.content = raw[indent:], true
			l.more = isWhite(l.text[0])
		}
		lines = append(lines, l)
		if j < len(t) {
			j = skipNewline(t, j)
		}
		i = j
	}
	last := -1
	for k, l := range lines {
		if l.content {
			last = k
		}
	}
	var b strings.Builder
	prev := -1 // the last content line written
	for k := 0; k <= last; k++ {
		l := lines[k]
		if !l.content {
			continue
		}
		empty := k - prev - 1
		switch {
		case prev < 0:
			b.WriteString(strings.Repeat("\n", empty))
		case folded && !l.more && !lines[prev].more:
			if empty == 0 {
				b.WriteByte(' ')
			} else {
				b.WriteString(strings.Repeat("\n", empty))
			}
		default:
			b.WriteString(strings.Repeat("\n", empty+1))
		}
		b.WriteString(l.text)
		prev = k
	}
	switch chomp {
	case '-':
	case '+':
		n := len(lines) - last - 1 // the empty lines after the last content line
		if last >= 0 {
			n++ // and the line break of the last content line
		}
		b.WriteString(strings.Repeat("\n", n))
	default:
		if last >= 0 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// The styles of a Scalar (Scalar.Style).
const (
	Plain        = 0 // a plain scalar, or an empty node
	SingleQuoted = 1
	DoubleQuoted = 2
	Literal      = 3 // a literal block scalar (|)
	Folded       = 4 // a folded block scalar (>)
)
