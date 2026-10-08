package python

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"
)

// EllipsisType is the type of Ellipsis.
type EllipsisType struct{}

// Ellipsis is the value of the constant `...`.
var Ellipsis = EllipsisType{}

// Value returns the value of the constant, as Python would compute it:
//
//   - an integer literal: *big.Int
//   - a floating-point literal: float64 (+Inf for a literal too large, as in Python)
//   - an imaginary literal: complex128
//   - string literals: string; adjacent literals are concatenated, escape sequences decoded, and
//     the newlines of the source (\r\n, \r) read as \n. A lone surrogate (\ud800) becomes U+FFFD,
//     since a Go string cannot hold it (StringValue returns the code points)
//   - bytes literals: []byte
//   - True, False: bool; None: nil; `...`: Ellipsis
//
// The error reports escape sequences that CPython rejects although the grammar accepts them (an
// unknown name in \N{...}).
func (c *Constant) Value() (any, error) {
	v, err := constValue(c.Text)
	if err != nil {
		return nil, err
	}
	switch v.kind {
	case kStr:
		return runesString(v.str), nil
	case kBytes:
		return v.bytes, nil
	case kInt:
		return v.i, nil
	case kFloat:
		return v.f, nil
	case kComplex:
		return complex(0, v.f), nil
	case kTrue:
		return true, nil
	case kFalse:
		return false, nil
	case kNone:
		return nil, nil
	}
	return Ellipsis, nil
}

// StringValue returns the value of string literals as code points, which may include lone
// surrogates (\ud800), and whether the first literal has the prefix u in lower case (Python's
// Constant.kind "u"). It is an error for a constant that is not a string.
func (c *Constant) StringValue() (value []rune, u bool, err error) {
	v, err := constValue(c.Text)
	if err != nil {
		return nil, false, err
	}
	if v.kind != kStr {
		return nil, false, fmt.Errorf("python: %.20q is not a string", c.Text)
	}
	return v.str, v.u, nil
}

// Value returns the text the part stands for: escape sequences decoded, doubled braces made
// single, and newlines read as \n.
func (m *FStringMiddle) Value() (string, error) {
	rs, err := decodeFStringText(m.Text, false)
	return runesString(rs), err
}

// Value returns the text the part stands for: doubled braces made single, and newlines read as
// \n (backslashes are kept).
func (m *FStringRawMiddle) Value() string {
	rs, _ := decodeFStringText(m.Text, true)
	return runesString(rs)
}

// ConversionCode returns the conversion as Python's FormattedValue.conversion: -1 without one,
// otherwise the code of the character ('r' if there is a '=' but neither a conversion nor a
// format spec).
func (v *FormattedValue) ConversionCode() int {
	return conversionCode(v.Conversion, v.Debug, v.FormatSpec)
}

// ConversionCode returns the conversion as Python's Interpolation.conversion (see
// FormattedValue.ConversionCode).
func (v *Interpolation) ConversionCode() int {
	return conversionCode(v.Conversion, v.Debug, v.FormatSpec)
}

func conversionCode(conv, debug string, spec *JoinedStr) int {
	switch {
	case conv != "":
		return int(conv[0])
	case debug != "" && spec == nil:
		return 'r'
	}
	return -1
}

func runesString(rs []rune) string {
	var b strings.Builder
	for _, r := range rs {
		b.WriteRune(r) // surrogates become U+FFFD
	}
	return b.String()
}

type constKind int

const (
	kInt constKind = iota
	kFloat
	kComplex
	kStr
	kBytes
	kTrue
	kFalse
	kNone
	kEllipsis
)

// cval is a decoded constant.
type cval struct {
	kind  constKind
	i     *big.Int
	f     float64 // the float, or the imaginary part
	str   []rune
	bytes []byte
	u     bool // the first string literal has the prefix u
}

func constValue(text string) (cval, error) {
	switch text {
	case "True":
		return cval{kind: kTrue}, nil
	case "False":
		return cval{kind: kFalse}, nil
	case "None":
		return cval{kind: kNone}, nil
	case "...":
		return cval{kind: kEllipsis}, nil
	}
	if text == "" {
		return cval{}, errors.New("python: empty constant")
	}
	if c := text[0]; c >= '0' && c <= '9' || c == '.' {
		return numberValue(text)
	}
	return stringsValue(text)
}

// numberValue converts a number literal.
func numberValue(text string) (cval, error) {
	t := strings.ReplaceAll(text, "_", "")
	if len(t) > 1 && t[0] == '0' {
		base := 0
		switch t[1] {
		case 'x', 'X':
			base = 16
		case 'o', 'O':
			base = 8
		case 'b', 'B':
			base = 2
		}
		if base != 0 {
			i, ok := new(big.Int).SetString(t[2:], base)
			if !ok {
				return cval{}, fmt.Errorf("python: invalid number %q", text)
			}
			return cval{kind: kInt, i: i}, nil
		}
	}
	last := t[len(t)-1]
	if last == 'j' || last == 'J' {
		f, err := parseFloat(t[:len(t)-1])
		return cval{kind: kComplex, f: f}, err
	}
	if strings.ContainsAny(t, ".eE") {
		f, err := parseFloat(t)
		return cval{kind: kFloat, f: f}, err
	}
	i, ok := new(big.Int).SetString(t, 10)
	if !ok {
		return cval{}, fmt.Errorf("python: invalid number %q", text)
	}
	return cval{kind: kInt, i: i}, nil
}

func parseFloat(s string) (float64, error) {
	f, err := strconv.ParseFloat(s, 64)
	var ne *strconv.NumError
	if errors.As(err, &ne) && ne.Err == strconv.ErrRange {
		// Too large: ±Inf; too small: 0 or a subnormal, as in Python.
		return f, nil
	}
	if math.IsNaN(f) {
		return 0, fmt.Errorf("python: invalid number %q", s)
	}
	return f, err
}

// stringsValue decodes adjacent string or bytes literals, with the whitespace, comments and
// continuations between them.
func stringsValue(text string) (cval, error) {
	v := cval{kind: kStr}
	first := true
	for i := 0; i < len(text); {
		switch c := text[i]; c {
		case ' ', '\t', '\f', '\n', '\r', '\\':
			i++
			continue
		case '#':
			for i < len(text) && text[i] != '\n' && text[i] != '\r' {
				i++
			}
			continue
		}
		raw, isBytes, u := false, false, false
		for ; i < len(text) && text[i] != '\'' && text[i] != '"'; i++ {
			switch text[i] {
			case 'r', 'R':
				raw = true
			case 'b', 'B':
				isBytes = true
			case 'u':
				u = true // CPython sets the kind for a lower-case u only
			case 'U':
			default:
				return cval{}, fmt.Errorf("python: invalid string prefix in %.40q", text)
			}
		}
		if i >= len(text) {
			return cval{}, fmt.Errorf("python: invalid string literal %.40q", text)
		}
		body, n, err := literalBody(text[i:])
		if err != nil {
			return cval{}, err
		}
		i += n
		if first {
			v.u = u
			if isBytes {
				v.kind = kBytes
				v.bytes = []byte{}
			}
			first = false
		} else if isBytes != (v.kind == kBytes) {
			return cval{}, errors.New("python: cannot mix bytes and nonbytes literals")
		}
		if isBytes {
			b, err := decodeBytes(body, raw)
			if err != nil {
				return cval{}, err
			}
			v.bytes = append(v.bytes, b...)
		} else {
			rs, err := decodeStr(body, raw, false)
			if err != nil {
				return cval{}, err
			}
			v.str = append(v.str, rs...)
		}
	}
	if first {
		return cval{}, fmt.Errorf("python: invalid string literal %.40q", text)
	}
	return v, nil
}

// literalBody returns the text between the quotes of the literal at the start of s (which starts
// with its quote) and the length of the literal.
func literalBody(s string) (body string, n int, err error) {
	q := s[:1]
	if strings.HasPrefix(s, q+q+q) {
		q = q + q + q
	}
	for i := len(q); i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if strings.HasPrefix(s[i:], q) {
			return s[len(q):i], i + len(q), nil
		}
	}
	return "", 0, fmt.Errorf("python: unterminated string literal %.40q", s)
}

// decodeStr decodes the body of a str literal (fstring: an f-string middle, where doubled braces
// stand for braces).
func decodeStr(s string, raw, fstring bool) ([]rune, error) {
	out := make([]rune, 0, len(s))
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '\r':
			// Newlines in the source are \n.
			out = append(out, '\n')
			i++
			if i < len(s) && s[i] == '\n' {
				i++
			}
			continue
		case fstring && (c == '{' || c == '}') && i+1 < len(s) && s[i+1] == c:
			out = append(out, rune(c))
			i += 2
			continue
		case c != '\\' || raw:
			if raw && c == '\\' && i+1 < len(s) {
				// A raw backslash and the character after it (which may be a quote or a brace).
				out = append(out, '\\')
				i++
				continue
			}
			r, n := utf8.DecodeRuneInString(s[i:])
			out = append(out, r)
			i += n
			continue
		}
		// An escape sequence.
		if i+1 >= len(s) {
			out = append(out, '\\')
			i++
			continue
		}
		e := s[i+1]
		i += 2
		switch e {
		case '\n':
		case '\r':
			if i < len(s) && s[i] == '\n' {
				i++
			}
		case '\\', '\'', '"':
			out = append(out, rune(e))
		case 'a':
			out = append(out, '\a')
		case 'b':
			out = append(out, '\b')
		case 'f':
			out = append(out, '\f')
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 't':
			out = append(out, '\t')
		case 'v':
			out = append(out, '\v')
		case '0', '1', '2', '3', '4', '5', '6', '7':
			v := rune(e - '0')
			for k := 0; k < 2 && i < len(s) && s[i] >= '0' && s[i] <= '7'; k++ {
				v = v*8 + rune(s[i]-'0')
				i++
			}
			out = append(out, v)
		case 'x', 'u', 'U':
			n := map[byte]int{'x': 2, 'u': 4, 'U': 8}[e]
			if i+n > len(s) {
				return nil, fmt.Errorf("python: truncated \\%c escape", e)
			}
			v, err := strconv.ParseUint(s[i:i+n], 16, 32)
			if err != nil || v > utf8.MaxRune {
				return nil, fmt.Errorf("python: invalid \\%c escape", e)
			}
			out = append(out, rune(v))
			i += n
		case 'N':
			end := strings.IndexByte(s[i:], '}')
			if i >= len(s) || s[i] != '{' || end < 0 {
				return nil, errors.New("python: malformed \\N character escape")
			}
			name := s[i+1 : i+end]
			r, ok := lookupName(name)
			if !ok {
				return nil, fmt.Errorf("python: unknown Unicode character name %q", name)
			}
			out = append(out, r)
			i += end + 1
		default:
			// An unknown escape is kept (CPython warns).
			out = append(out, '\\')
			i--
		}
	}
	return out, nil
}

// decodeFStringText decodes the literal text of an f-string or t-string.
func decodeFStringText(s string, raw bool) ([]rune, error) {
	return decodeStr(s, raw, true)
}

// decodeBytes decodes the body of a bytes literal.
func decodeBytes(s string, raw bool) ([]byte, error) {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '\r':
			out = append(out, '\n')
			i++
			if i < len(s) && s[i] == '\n' {
				i++
			}
			continue
		case c != '\\' || raw:
			if raw && c == '\\' && i+1 < len(s) {
				out = append(out, '\\')
				i++
				continue
			}
			out = append(out, c)
			i++
			continue
		}
		if i+1 >= len(s) {
			out = append(out, '\\')
			i++
			continue
		}
		e := s[i+1]
		i += 2
		switch e {
		case '\n':
		case '\r':
			if i < len(s) && s[i] == '\n' {
				i++
			}
		case '\\', '\'', '"':
			out = append(out, e)
		case 'a':
			out = append(out, '\a')
		case 'b':
			out = append(out, '\b')
		case 'f':
			out = append(out, '\f')
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 't':
			out = append(out, '\t')
		case 'v':
			out = append(out, '\v')
		case '0', '1', '2', '3', '4', '5', '6', '7':
			v := int(e - '0')
			for k := 0; k < 2 && i < len(s) && s[i] >= '0' && s[i] <= '7'; k++ {
				v = v*8 + int(s[i]-'0')
				i++
			}
			out = append(out, byte(v))
		case 'x':
			if i+2 > len(s) {
				return nil, errors.New("python: invalid \\x escape")
			}
			v, err := strconv.ParseUint(s[i:i+2], 16, 8)
			if err != nil {
				return nil, errors.New("python: invalid \\x escape")
			}
			out = append(out, byte(v))
			i += 2
		default:
			out = append(out, '\\')
			i--
		}
	}
	return out, nil
}
