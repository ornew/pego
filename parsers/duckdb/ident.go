package duckdb

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Quoted reports whether the identifier is written between double quotes (or is a string used as a name).
func (i *Ident) Quoted() bool {
	t := i.Text
	return t != "" && (t[0] == '"' || t[0] == '\'' || t[0] == '$' || isPrefixedQuote(t) ||
		len(t) > 2 && (t[0] == 'U' || t[0] == 'u') && t[1] == '&' && t[2] == '"')
}

// Name returns the name the identifier denotes: a word as written, a quoted identifier without its quotes and
// with "" decoded, a string (the ColIdOrString of the grammar) with its escapes decoded.
//
// DuckDB keeps the case of an identifier. Names are compared without regard to the case of ASCII letters:
// compare Fold of two names, or use EqualFold.
func (i *Ident) Name() string {
	t := i.Text
	switch {
	case t == "":
		return ""
	case t[0] == '"':
		return unquoteIdent(t[1:], '\\', false)
	case len(t) > 2 && (t[0] == 'U' || t[0] == 'u') && t[1] == '&' && t[2] == '"':
		return unquoteIdent(t[3:], '\\', true)
	case t[0] == '\'' || t[0] == '$' || isPrefixedQuote(t):
		return stringValue(t)
	}
	return t
}

// Fold returns the name in lower case (ASCII letters only), the key by which DuckDB compares names.
func (i *Ident) Fold() string { return foldASCII(i.Name()) }

// EqualFold reports whether two identifiers denote the same name.
func (i *Ident) EqualFold(o *Ident) bool { return i.Fold() == o.Fold() }

func isPrefixedQuote(t string) bool {
	if len(t) < 2 {
		return false
	}
	switch t[0] {
	case 'e', 'E':
		return t[1] == '\''
	case 'u', 'U':
		return len(t) > 2 && t[1] == '&' && t[2] == '\''
	}
	return false
}

func foldASCII(s string) string {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 'A' && c <= 'Z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				if b[j] >= 'A' && b[j] <= 'Z' {
					b[j] += 'a' - 'A'
				}
			}
			return string(b)
		}
	}
	return s
}

// unquoteIdent decodes the rest of a quoted identifier after its opening quote: "" is a quote. With unicode,
// \XXXX and \+XXXXXX are code points, and the escape character can be changed after the closing quote
// (UESCAPE 'c').
func unquoteIdent(s string, esc byte, unicode bool) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		c := s[i]
		if c == '"' {
			if i+1 < len(s) && s[i+1] == '"' {
				b.WriteByte('"')
				i += 2
				continue
			}
			rest := s[i+1:]
			if unicode {
				if j := strings.Index(rest, "'"); j >= 0 && len(rest) >= j+3 {
					esc = rest[j+1]
				}
				return decodeUnicodeEscapes(b.String(), esc)
			}
			return b.String()
		}
		b.WriteByte(c)
		i++
	}
	if unicode {
		return decodeUnicodeEscapes(b.String(), esc)
	}
	return b.String()
}

// decodeUnicodeEscapes decodes the escapes of U&"..." and U&'...': esc esc is esc, esc and four hexadecimal
// digits is a code point, esc + and six digits too; pairs of UTF-16 surrogates are combined.
func decodeUnicodeEscapes(s string, esc byte) string {
	var b strings.Builder
	var hi rune
	for i := 0; i < len(s); {
		if s[i] != esc {
			b.WriteByte(s[i])
			i++
			continue
		}
		if i+1 < len(s) && s[i+1] == esc {
			b.WriteByte(esc)
			i += 2
			continue
		}
		n := 4
		start := i + 1
		if i+1 < len(s) && s[i+1] == '+' {
			n = 6
			start = i + 2
		}
		if start+n > len(s) {
			b.WriteRune(utf8.RuneError)
			break
		}
		v, err := strconv.ParseUint(s[start:start+n], 16, 32)
		i = start + n
		if err != nil {
			b.WriteRune(utf8.RuneError)
			continue
		}
		r := rune(v)
		switch {
		case r >= 0xd800 && r < 0xdc00:
			hi = r
		case r >= 0xdc00 && r < 0xe000 && hi != 0:
			b.WriteRune(0x10000 + (hi-0xd800)<<10 + (r - 0xdc00))
			hi = 0
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// String returns the name as written, with its quotes: schema.table.
func (n *Name) String() string {
	var b strings.Builder
	for i, p := range n.Parts {
		if i > 0 {
			b.WriteByte('.')
		}
		b.WriteString(p.Text)
	}
	return b.String()
}

// Names returns the decoded parts of the name.
func (n *Name) Names() []string {
	out := make([]string, len(n.Parts))
	for i := range n.Parts {
		out[i] = n.Parts[i].Name()
	}
	return out
}

// Name returns the name the identifier of an alias denotes, or "".
func (a *TableAlias) Alias() string {
	if a == nil || a.Name == nil {
		return ""
	}
	return a.Name.Name()
}
